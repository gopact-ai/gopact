package gopact

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrTurnLoopRowBackendRequired is returned when a row TurnLoop store is created without a backend.
	ErrTurnLoopRowBackendRequired = errors.New("gopact: turn loop row backend is required")
)

// TurnLoopRowRecord is the stable row payload for a TurnLoop store.
type TurnLoopRowRecord struct {
	Key           string         `json:"key"`
	SchemaVersion string         `json:"schema_version,omitempty"`
	State         TurnLoopState  `json:"state,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// TurnLoopRowBackend is the minimal database-like storage port required by RowTurnLoopStore.
//
// SQL, KV, Redis hash, and internal control-plane adapters should implement this
// interface outside core, using their own schema, migrations, and transactions.
type TurnLoopRowBackend interface {
	UpsertTurnLoopState(ctx context.Context, record TurnLoopRowRecord) error
	GetTurnLoopState(ctx context.Context, key string) (TurnLoopRowRecord, bool, error)
}

// RowTurnLoopStore persists TurnLoop queue state as one row identified by key.
type RowTurnLoopStore struct {
	mu      sync.Mutex
	backend TurnLoopRowBackend
	key     string
}

// NewRowTurnLoopStore creates a TurnLoop store backed by an injected row backend.
func NewRowTurnLoopStore(backend TurnLoopRowBackend, key string) (*RowTurnLoopStore, error) {
	if backend == nil {
		return nil, ErrTurnLoopRowBackendRequired
	}
	if key == "" {
		return nil, errors.New("gopact: turn loop row key is required")
	}
	return &RowTurnLoopStore{
		backend: backend,
		key:     key,
	}, nil
}

// Load returns the stored TurnLoop state from the backend row.
func (s *RowTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnLoopState{}, false, err
	}
	if s == nil {
		return TurnLoopState{}, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok, err := s.backend.GetTurnLoopState(ctx, s.key)
	if err != nil {
		return TurnLoopState{}, false, fmt.Errorf("gopact: read turn loop row store: %w", err)
	}
	if !ok {
		return TurnLoopState{}, false, nil
	}
	if record.SchemaVersion != "" && record.SchemaVersion != turnLoopStoreSchemaVersion {
		return TurnLoopState{}, false, fmt.Errorf("%w: got %q want %q", ErrTurnLoopStoreSchemaMismatch, record.SchemaVersion, turnLoopStoreSchemaVersion)
	}
	return copyTurnLoopState(record.State), true, nil
}

// Save writes the TurnLoop state to the backend row.
func (s *RowTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record := TurnLoopRowRecord{
		Key:           s.key,
		SchemaVersion: turnLoopStoreSchemaVersion,
		State:         copyTurnLoopState(state),
		UpdatedAt:     time.Now(),
	}
	if err := s.backend.UpsertTurnLoopState(ctx, record); err != nil {
		return fmt.Errorf("gopact: write turn loop row store: %w", err)
	}
	return nil
}

// MemoryTurnLoopRowBackend is an in-process row backend for tests and local development.
type MemoryTurnLoopRowBackend struct {
	mu      sync.RWMutex
	records map[string]TurnLoopRowRecord
}

// NewMemoryTurnLoopRowBackend creates an empty in-process row backend.
func NewMemoryTurnLoopRowBackend() *MemoryTurnLoopRowBackend {
	return &MemoryTurnLoopRowBackend{
		records: make(map[string]TurnLoopRowRecord),
	}
}

// UpsertTurnLoopState stores or replaces one TurnLoop row.
func (b *MemoryTurnLoopRowBackend) UpsertTurnLoopState(ctx context.Context, record TurnLoopRowRecord) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Key == "" {
		return errors.New("gopact: turn loop row key is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.records == nil {
		b.records = make(map[string]TurnLoopRowRecord)
	}
	b.records[record.Key] = copyTurnLoopRowRecord(record)
	return nil
}

// GetTurnLoopState returns a copy of one TurnLoop row.
func (b *MemoryTurnLoopRowBackend) GetTurnLoopState(ctx context.Context, key string) (TurnLoopRowRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnLoopRowRecord{}, false, err
	}
	if b == nil {
		return TurnLoopRowRecord{}, false, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	record, ok := b.records[key]
	if !ok {
		return TurnLoopRowRecord{}, false, nil
	}
	return copyTurnLoopRowRecord(record), true, nil
}

func copyTurnLoopRowRecord(record TurnLoopRowRecord) TurnLoopRowRecord {
	record.State = copyTurnLoopState(record.State)
	record.Metadata = copyAnyMap(record.Metadata)
	return record
}
