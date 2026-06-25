package checkpoint

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gopact-ai/gopact/graph"
)

var _ graph.CheckpointStore[struct{}] = (*ObjectStore[struct{}])(nil)

var (
	// ErrObjectBackendRequired is returned when an object checkpoint store is created without a backend.
	ErrObjectBackendRequired = errors.New("checkpoint: object backend is required")
	// ErrObjectNotFound tells ObjectStore that a backend key has no object.
	ErrObjectNotFound = errors.New("checkpoint: object not found")
)

// ObjectBackend is the minimal object storage port required by ObjectStore.
//
// Cloud adapters can map this interface to S3, GCS, R2, OSS, or another blob
// store without forcing gopact to own provider configuration.
type ObjectBackend interface {
	PutObject(ctx context.Context, key string, data []byte) error
	GetObject(ctx context.Context, key string) ([]byte, error)
	ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error)
}

// ObjectInfo describes one object visible to an ObjectBackend list operation.
type ObjectInfo struct {
	Key       string
	UpdatedAt time.Time
	Metadata  map[string]string
}

// ObjectStore persists each checkpoint record as one object.
type ObjectStore[S any] struct {
	mu            sync.Mutex
	backend       ObjectBackend
	prefix        string
	codec         StateCodec[S]
	configVersion string
	driftPolicy   ConfigDriftPolicy
	migrations    map[string]RecordMigrator
}

// ObjectStoreOption configures an ObjectStore checkpoint store.
type ObjectStoreOption[S any] interface {
	applyObjectStore(*ObjectStore[S])
}

// NewObjectStore creates a checkpoint store backed by an injected object backend.
func NewObjectStore[S any](backend ObjectBackend, opts ...ObjectStoreOption[S]) (*ObjectStore[S], error) {
	if backend == nil {
		return nil, ErrObjectBackendRequired
	}
	store := &ObjectStore[S]{
		backend: backend,
		codec:   JSONCodec[S]{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyObjectStore(store)
		}
	}
	return store, nil
}

// Put stores or replaces one checkpoint record.
func (s *ObjectStore[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
	if err := checkContext(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	prepared, err := s.prepareCheckpointLocked(ctx, checkpoint)
	if err != nil {
		return err
	}
	record, err := EncodeCheckpoint(prepared, s.codec)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("checkpoint: encode object record: %w", err)
	}
	if err := s.backend.PutObject(ctx, s.recordKey(record.ID), raw); err != nil {
		return fmt.Errorf("checkpoint: write object record: %w", err)
	}
	return nil
}

// Get returns one checkpoint by id.
func (s *ObjectStore[S]) Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error) {
	if err := checkContext(ctx); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok, err := s.getRecordLocked(ctx, id)
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	if !ok {
		var zero graph.Checkpoint[S]
		return zero, false, nil
	}
	checkpoint, err := decodeCheckpointWithConfig[S](record, s.codec, s.decodeConfig())
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	return checkpoint, true, nil
}

// List returns checkpoint copies for a thread ordered by record creation time.
func (s *ObjectStore[S]) List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.listRecordsLocked(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]graph.Checkpoint[S], 0)
	for _, record := range records {
		if record.ThreadID != threadID {
			continue
		}
		checkpoint, err := decodeCheckpointWithConfig[S](record, s.codec, s.decodeConfig())
		if err != nil {
			return nil, err
		}
		out = append(out, checkpoint)
	}
	return out, nil
}

// Latest returns the latest checkpoint for a thread.
func (s *ObjectStore[S]) Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool, error) {
	if err := checkContext(ctx); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.listRecordsLocked(ctx)
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if record.ThreadID != threadID {
			continue
		}
		checkpoint, err := decodeCheckpointWithConfig[S](record, s.codec, s.decodeConfig())
		if err != nil {
			var zero graph.Checkpoint[S]
			return zero, false, err
		}
		return checkpoint, true, nil
	}
	var zero graph.Checkpoint[S]
	return zero, false, nil
}

func (s *ObjectStore[S]) prepareCheckpointLocked(ctx context.Context, checkpoint graph.Checkpoint[S]) (graph.Checkpoint[S], error) {
	if checkpoint.ThreadID == "" {
		checkpoint.ThreadID = checkpoint.IDs.ThreadID
	}
	if checkpoint.ID == "" {
		records, err := s.listRecordsLocked(ctx)
		if err != nil {
			return graph.Checkpoint[S]{}, err
		}
		checkpoint.ID = fmt.Sprintf("%s:%d:%d", checkpoint.ThreadID, checkpoint.Step, countThreadRecords(records, checkpoint.ThreadID)+1)
	}
	if checkpoint.ConfigVersion == "" {
		checkpoint.ConfigVersion = s.configVersion
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now()
	}
	return checkpoint, nil
}

func (s *ObjectStore[S]) getRecordLocked(ctx context.Context, id string) (Record, bool, error) {
	raw, err := s.backend.GetObject(ctx, s.recordKey(id))
	if errors.Is(err, ErrObjectNotFound) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("checkpoint: read object record: %w", err)
	}
	record, err := decodeObjectRecord(raw)
	if err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

func (s *ObjectStore[S]) listRecordsLocked(ctx context.Context) ([]Record, error) {
	infos, err := s.backend.ListObjects(ctx, s.recordsPrefix())
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list object records: %w", err)
	}
	records := make([]Record, 0, len(infos))
	for _, info := range infos {
		raw, err := s.backend.GetObject(ctx, info.Key)
		if errors.Is(err, ErrObjectNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("checkpoint: read object record %q: %w", info.Key, err)
		}
		record, err := decodeObjectRecord(raw)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sortRecords(records)
	return records, nil
}

func decodeObjectRecord(raw []byte) (Record, error) {
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return Record{}, fmt.Errorf("checkpoint: decode object record: %w", err)
	}
	return record, nil
}

func (s *ObjectStore[S]) decodeConfig() decodeConfig {
	return decodeConfig{
		currentConfigVersion: s.configVersion,
		driftPolicy:          s.driftPolicy,
		migrations:           copyMigrations(s.migrations),
	}
}

func (s *ObjectStore[S]) recordsPrefix() string {
	return joinObjectKey(s.prefix, "records") + "/"
}

func (s *ObjectStore[S]) recordKey(id string) string {
	return s.recordsPrefix() + encodeObjectKeyPart(id) + ".json"
}

func sortRecords(records []Record) {
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].CreatedAt.Before(records[j].CreatedAt)
		}
		if records[i].Step != records[j].Step {
			return records[i].Step < records[j].Step
		}
		return records[i].ID < records[j].ID
	})
}

func normalizeObjectPrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), "/")
}

func joinObjectKey(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "/")
}

func encodeObjectKeyPart(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

// MemoryObjectBackend is an in-process object backend for tests and local development.
type MemoryObjectBackend struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
}

type memoryObject struct {
	data      []byte
	updatedAt time.Time
	metadata  map[string]string
}

// NewMemoryObjectBackend creates an empty in-process object backend.
func NewMemoryObjectBackend() *MemoryObjectBackend {
	return &MemoryObjectBackend{
		objects: make(map[string]memoryObject),
	}
}

// PutObject stores or replaces one object.
func (b *MemoryObjectBackend) PutObject(ctx context.Context, key string, data []byte) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if key == "" {
		return errors.New("checkpoint: object key is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.objects == nil {
		b.objects = make(map[string]memoryObject)
	}
	b.objects[key] = memoryObject{
		data:      append([]byte(nil), data...),
		updatedAt: time.Now(),
	}
	return nil
}

// GetObject returns a copy of one object payload.
func (b *MemoryObjectBackend) GetObject(ctx context.Context, key string) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	object, ok := b.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return append([]byte(nil), object.data...), nil
}

// ListObjects returns objects whose keys have the given prefix, ordered by key.
func (b *MemoryObjectBackend) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]ObjectInfo, 0)
	for key, object := range b.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out = append(out, ObjectInfo{
			Key:       key,
			UpdatedAt: object.updatedAt,
			Metadata:  copyStringMap(object.metadata),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
