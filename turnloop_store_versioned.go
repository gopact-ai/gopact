package gopact

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrTurnLoopVersionedBackendRequired is returned when a versioned TurnLoop store is created without a backend.
	ErrTurnLoopVersionedBackendRequired = errors.New("gopact: turn loop versioned backend is required")
	// ErrTurnLoopStoreConflict is returned when a TurnLoop state save loses an optimistic concurrency race.
	ErrTurnLoopStoreConflict = errors.New("gopact: turn loop store conflict")
)

// TurnLoopVersionedRecord is the stable CAS payload for a TurnLoop store.
type TurnLoopVersionedRecord struct {
	Key           string         `json:"key"`
	Version       string         `json:"version,omitempty"`
	SchemaVersion string         `json:"schema_version,omitempty"`
	State         TurnLoopState  `json:"state,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// TurnLoopVersionedBackend is the minimal optimistic-concurrency storage port
// required by VersionedTurnLoopStore.
//
// SQL, KV, Redis, object metadata, and internal control-plane adapters can map
// Version to their native revision, etag, update timestamp, or CAS token.
type TurnLoopVersionedBackend interface {
	GetTurnLoopVersionedState(ctx context.Context, key string) (TurnLoopVersionedRecord, bool, error)
	CompareAndSwapTurnLoopState(ctx context.Context, record TurnLoopVersionedRecord, expectedVersion string) (string, error)
}

// VersionedTurnLoopStore persists TurnLoop queue state with optimistic CAS.
type VersionedTurnLoopStore struct {
	mu             sync.Mutex
	backend        TurnLoopVersionedBackend
	key            string
	currentVersion string
}

// NewVersionedTurnLoopStore creates a TurnLoop store backed by a CAS backend.
func NewVersionedTurnLoopStore(backend TurnLoopVersionedBackend, key string) (*VersionedTurnLoopStore, error) {
	if backend == nil {
		return nil, ErrTurnLoopVersionedBackendRequired
	}
	if key == "" {
		return nil, errors.New("gopact: turn loop versioned key is required")
	}
	return &VersionedTurnLoopStore{backend: backend, key: key}, nil
}

// Load returns the stored TurnLoop state and remembers the backend version.
func (s *VersionedTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
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

	record, ok, err := s.backend.GetTurnLoopVersionedState(ctx, s.key)
	if err != nil {
		return TurnLoopState{}, false, fmt.Errorf("gopact: read turn loop versioned store: %w", err)
	}
	if !ok {
		s.currentVersion = ""
		return TurnLoopState{}, false, nil
	}
	if record.SchemaVersion != "" && record.SchemaVersion != turnLoopStoreSchemaVersion {
		return TurnLoopState{}, false, fmt.Errorf("%w: got %q want %q", ErrTurnLoopStoreSchemaMismatch, record.SchemaVersion, turnLoopStoreSchemaVersion)
	}
	s.currentVersion = record.Version
	return copyTurnLoopState(record.State), true, nil
}

// Save writes the TurnLoop state if the backend version has not changed since
// this store last loaded or saved state.
func (s *VersionedTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
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

	record := TurnLoopVersionedRecord{
		Key:           s.key,
		SchemaVersion: turnLoopStoreSchemaVersion,
		State:         copyTurnLoopState(state),
		UpdatedAt:     time.Now(),
	}
	version, err := s.backend.CompareAndSwapTurnLoopState(ctx, record, s.currentVersion)
	if err != nil {
		return fmt.Errorf("gopact: write turn loop versioned store: %w", err)
	}
	s.currentVersion = version
	return nil
}

// MemoryTurnLoopVersionedBackend is an in-process CAS backend for tests and local development.
type MemoryTurnLoopVersionedBackend struct {
	mu         sync.RWMutex
	versionSeq uint64
	records    map[string]TurnLoopVersionedRecord
}

// NewMemoryTurnLoopVersionedBackend creates an empty in-process CAS backend.
func NewMemoryTurnLoopVersionedBackend() *MemoryTurnLoopVersionedBackend {
	return &MemoryTurnLoopVersionedBackend{
		records: make(map[string]TurnLoopVersionedRecord),
	}
}

// GetTurnLoopVersionedState returns a copy of one versioned TurnLoop record.
func (b *MemoryTurnLoopVersionedBackend) GetTurnLoopVersionedState(ctx context.Context, key string) (TurnLoopVersionedRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnLoopVersionedRecord{}, false, err
	}
	if b == nil {
		return TurnLoopVersionedRecord{}, false, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	record, ok := b.records[key]
	if !ok {
		return TurnLoopVersionedRecord{}, false, nil
	}
	return copyTurnLoopVersionedRecord(record), true, nil
}

// CompareAndSwapTurnLoopState stores record if expectedVersion matches the
// currently stored backend version, and returns the new version.
func (b *MemoryTurnLoopVersionedBackend) CompareAndSwapTurnLoopState(ctx context.Context, record TurnLoopVersionedRecord, expectedVersion string) (string, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Key == "" {
		return "", errors.New("gopact: turn loop versioned key is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.records == nil {
		b.records = make(map[string]TurnLoopVersionedRecord)
	}
	current, ok := b.records[record.Key]
	if !ok {
		if expectedVersion != "" {
			return "", ErrTurnLoopStoreConflict
		}
	} else if current.Version != expectedVersion {
		return "", ErrTurnLoopStoreConflict
	}

	b.versionSeq++
	record = copyTurnLoopVersionedRecord(record)
	record.Version = fmt.Sprintf("%d", b.versionSeq)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now()
	}
	b.records[record.Key] = record
	return record.Version, nil
}

func copyTurnLoopVersionedRecord(record TurnLoopVersionedRecord) TurnLoopVersionedRecord {
	record.State = copyTurnLoopState(record.State)
	record.Metadata = copyAnyMap(record.Metadata)
	return record
}
