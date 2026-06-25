package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopact-ai/gopact/graph"
)

var _ graph.CheckpointStore[struct{}] = (*FileStore[struct{}])(nil)

// FileStore persists checkpoints into one local JSON file.
type FileStore[S any] struct {
	mu            sync.Mutex
	path          string
	codec         StateCodec[S]
	configVersion string
	driftPolicy   ConfigDriftPolicy
	migrations    map[string]RecordMigrator
}

// FileStoreOption configures a FileStore checkpoint store.
type FileStoreOption[S any] interface {
	applyFileStore(*FileStore[S])
}

// WithFileCodec sets the state codec used by the file checkpoint store.
//
// Deprecated: use WithCodec.
func WithFileCodec[S any](codec StateCodec[S]) FileStoreOption[S] {
	return WithCodec(codec)
}

// NewFileStore creates a checkpoint store backed by a local JSON file.
func NewFileStore[S any](path string, opts ...FileStoreOption[S]) (*FileStore[S], error) {
	if path == "" {
		return nil, errors.New("checkpoint: file store path is required")
	}
	store := &FileStore[S]{
		path:  path,
		codec: JSONCodec[S]{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyFileStore(store)
		}
	}
	return store, nil
}

// Put stores or replaces one checkpoint record.
func (s *FileStore[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
	if err := checkContext(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readDataLocked()
	if err != nil {
		return err
	}
	checkpoint = s.prepareCheckpoint(checkpoint, data.Records)
	record, err := EncodeCheckpoint(checkpoint, s.codec)
	if err != nil {
		return err
	}

	for i, existing := range data.Records {
		if existing.ID == record.ID {
			data.Records[i] = record
			return s.writeDataLocked(data)
		}
	}
	data.Records = append(data.Records, record)
	return s.writeDataLocked(data)
}

// Get returns one checkpoint by id.
func (s *FileStore[S]) Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error) {
	if err := checkContext(ctx); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readDataLocked()
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	for _, record := range data.Records {
		if record.ID != id {
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

// List returns checkpoint copies for a thread in insertion order.
func (s *FileStore[S]) List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readDataLocked()
	if err != nil {
		return nil, err
	}
	out := make([]graph.Checkpoint[S], 0)
	for _, record := range data.Records {
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
func (s *FileStore[S]) Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool, error) {
	if err := checkContext(ctx); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readDataLocked()
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	for i := len(data.Records) - 1; i >= 0; i-- {
		record := data.Records[i]
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

type fileStoreData struct {
	SchemaVersion string   `json:"schema_version,omitempty"`
	Records       []Record `json:"records,omitempty"`
}

func (s *FileStore[S]) readDataLocked() (fileStoreData, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return fileStoreData{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return fileStoreData{}, fmt.Errorf("checkpoint: read file store: %w", err)
	}
	if len(raw) == 0 {
		return fileStoreData{SchemaVersion: SchemaVersion}, nil
	}

	var data fileStoreData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fileStoreData{}, fmt.Errorf("checkpoint: decode file store: %w", err)
	}
	if data.SchemaVersion != "" && data.SchemaVersion != SchemaVersion {
		return fileStoreData{}, fmt.Errorf("%w: got %q want %q", ErrSchemaMismatch, data.SchemaVersion, SchemaVersion)
	}
	if data.SchemaVersion == "" {
		data.SchemaVersion = SchemaVersion
	}
	return data, nil
}

func (s *FileStore[S]) writeDataLocked(data fileStoreData) error {
	data.SchemaVersion = SchemaVersion
	raw, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return fmt.Errorf("checkpoint: encode file store: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("checkpoint: create file store directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("checkpoint: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(raw); err != nil {
		return fmt.Errorf("checkpoint: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("checkpoint: close temp file: %w", err)
	}
	closed = true
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("checkpoint: replace file store: %w", err)
	}
	return nil
}

func (s *FileStore[S]) prepareCheckpoint(checkpoint graph.Checkpoint[S], records []Record) graph.Checkpoint[S] {
	if checkpoint.ThreadID == "" {
		checkpoint.ThreadID = checkpoint.IDs.ThreadID
	}
	if checkpoint.ID == "" {
		checkpoint.ID = fmt.Sprintf("%s:%d:%d", checkpoint.ThreadID, checkpoint.Step, countThreadRecords(records, checkpoint.ThreadID)+1)
	}
	if checkpoint.ConfigVersion == "" {
		checkpoint.ConfigVersion = s.configVersion
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now()
	}
	return checkpoint
}

func (s *FileStore[S]) decodeConfig() decodeConfig {
	return decodeConfig{
		currentConfigVersion: s.configVersion,
		driftPolicy:          s.driftPolicy,
		migrations:           copyMigrations(s.migrations),
	}
}

func countThreadRecords(records []Record, threadID string) int {
	count := 0
	for _, record := range records {
		if record.ThreadID == threadID {
			count++
		}
	}
	return count
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("checkpoint: context is required")
	}
	return ctx.Err()
}
