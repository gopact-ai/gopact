package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gopact-ai/gopact/graph"
)

var _ graph.CheckpointStore[struct{}] = (*RowStore[struct{}])(nil)

// ErrRowBackendRequired is returned when a row checkpoint store is created without a backend.
var ErrRowBackendRequired = errors.New("checkpoint: row backend is required")

// RowBackend is the minimal database-like storage port required by RowStore.
//
// SQL adapters should map one Record to one durable row and return ListRecords
// in any stable order; RowStore sorts records by checkpoint creation time before
// decoding them.
type RowBackend interface {
	UpsertRecord(ctx context.Context, record Record) error
	GetRecord(ctx context.Context, id string) (Record, bool, error)
	ListRecords(ctx context.Context, threadID string) ([]Record, error)
}

// RowStore persists encoded checkpoint records through an injected row backend.
type RowStore[S any] struct {
	mu            sync.Mutex
	backend       RowBackend
	codec         StateCodec[S]
	configVersion string
	driftPolicy   ConfigDriftPolicy
	migrations    map[string]RecordMigrator
}

// RowStoreOption configures a RowStore checkpoint store.
type RowStoreOption[S any] interface {
	applyRowStore(*RowStore[S])
}

// NewRowStore creates a checkpoint store backed by an injected row backend.
func NewRowStore[S any](backend RowBackend, opts ...RowStoreOption[S]) (*RowStore[S], error) {
	if backend == nil {
		return nil, ErrRowBackendRequired
	}
	store := &RowStore[S]{
		backend: backend,
		codec:   JSONCodec[S]{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyRowStore(store)
		}
	}
	return store, nil
}

// Put stores or replaces one checkpoint record.
func (s *RowStore[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
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
	if err := s.backend.UpsertRecord(ctx, record); err != nil {
		return fmt.Errorf("checkpoint: upsert row record: %w", err)
	}
	return nil
}

// Get returns one checkpoint by id.
func (s *RowStore[S]) Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error) {
	if err := checkContext(ctx); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok, err := s.backend.GetRecord(ctx, id)
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, fmt.Errorf("checkpoint: get row record: %w", err)
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
func (s *RowStore[S]) List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.listRecordsLocked(ctx, threadID)
	if err != nil {
		return nil, err
	}
	out := make([]graph.Checkpoint[S], 0, len(records))
	for _, record := range records {
		checkpoint, err := decodeCheckpointWithConfig[S](record, s.codec, s.decodeConfig())
		if err != nil {
			return nil, err
		}
		out = append(out, checkpoint)
	}
	return out, nil
}

// Latest returns the latest checkpoint for a thread.
func (s *RowStore[S]) Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool, error) {
	if err := checkContext(ctx); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.listRecordsLocked(ctx, threadID)
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	if len(records) == 0 {
		var zero graph.Checkpoint[S]
		return zero, false, nil
	}
	checkpoint, err := decodeCheckpointWithConfig[S](records[len(records)-1], s.codec, s.decodeConfig())
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	return checkpoint, true, nil
}

func (s *RowStore[S]) prepareCheckpointLocked(ctx context.Context, checkpoint graph.Checkpoint[S]) (graph.Checkpoint[S], error) {
	if checkpoint.ThreadID == "" {
		checkpoint.ThreadID = checkpoint.IDs.ThreadID
	}
	if checkpoint.ID == "" {
		records, err := s.listRecordsLocked(ctx, checkpoint.ThreadID)
		if err != nil {
			return graph.Checkpoint[S]{}, err
		}
		checkpoint.ID = fmt.Sprintf("%s:%d:%d", checkpoint.ThreadID, checkpoint.Step, len(records)+1)
	}
	if checkpoint.ConfigVersion == "" {
		checkpoint.ConfigVersion = s.configVersion
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now()
	}
	return checkpoint, nil
}

func (s *RowStore[S]) listRecordsLocked(ctx context.Context, threadID string) ([]Record, error) {
	records, err := s.backend.ListRecords(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list row records: %w", err)
	}
	records = copyRecords(records)
	sortRecords(records)
	return records, nil
}

func (s *RowStore[S]) decodeConfig() decodeConfig {
	return decodeConfig{
		currentConfigVersion: s.configVersion,
		driftPolicy:          s.driftPolicy,
		migrations:           copyMigrations(s.migrations),
	}
}

// MemoryRowBackend is an in-process row backend for tests and local development.
type MemoryRowBackend struct {
	mu       sync.RWMutex
	byThread map[string][]Record
	byID     map[string]Record
}

// NewMemoryRowBackend creates an empty in-process row backend.
func NewMemoryRowBackend() *MemoryRowBackend {
	return &MemoryRowBackend{
		byThread: make(map[string][]Record),
		byID:     make(map[string]Record),
	}
}

// UpsertRecord stores or replaces one checkpoint record row.
func (b *MemoryRowBackend) UpsertRecord(ctx context.Context, record Record) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if record.ID == "" {
		return errors.New("checkpoint: row record id is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.byThread == nil {
		b.byThread = make(map[string][]Record)
	}
	if b.byID == nil {
		b.byID = make(map[string]Record)
	}
	record = copyRecord(record)
	records := b.byThread[record.ThreadID]
	for i, existing := range records {
		if existing.ID == record.ID {
			records[i] = record
			b.byThread[record.ThreadID] = records
			b.byID[record.ID] = record
			return nil
		}
	}
	b.byThread[record.ThreadID] = append(records, record)
	b.byID[record.ID] = record
	return nil
}

// GetRecord returns one checkpoint record by id.
func (b *MemoryRowBackend) GetRecord(ctx context.Context, id string) (Record, bool, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, false, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	record, ok := b.byID[id]
	if !ok {
		return Record{}, false, nil
	}
	return copyRecord(record), true, nil
}

// ListRecords returns checkpoint records for one thread.
func (b *MemoryRowBackend) ListRecords(ctx context.Context, threadID string) ([]Record, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return copyRecords(b.byThread[threadID]), nil
}

func copyRecords(in []Record) []Record {
	if len(in) == 0 {
		return nil
	}
	out := make([]Record, len(in))
	for i, record := range in {
		out[i] = copyRecord(record)
	}
	return out
}
