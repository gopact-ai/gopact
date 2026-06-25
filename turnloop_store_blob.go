package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrTurnLoopBlobBackendRequired is returned when a blob TurnLoop store is created without a backend.
	ErrTurnLoopBlobBackendRequired = errors.New("gopact: turn loop blob backend is required")
	// ErrTurnLoopBlobNotFound tells BlobTurnLoopStore that a backend key has no state blob.
	ErrTurnLoopBlobNotFound = errors.New("gopact: turn loop blob not found")
)

// TurnLoopBlobBackend is the minimal blob storage port required by BlobTurnLoopStore.
//
// Remote adapters can map this interface to object storage, Redis, SQL, or a
// project-specific control plane without making gopact own backend configuration.
type TurnLoopBlobBackend interface {
	GetBlob(ctx context.Context, key string) ([]byte, error)
	PutBlob(ctx context.Context, key string, data []byte) error
}

// BlobTurnLoopStore persists TurnLoop queue state as one backend blob.
type BlobTurnLoopStore struct {
	mu      sync.Mutex
	backend TurnLoopBlobBackend
	key     string
}

// NewBlobTurnLoopStore creates a TurnLoop store backed by an injected blob backend.
func NewBlobTurnLoopStore(backend TurnLoopBlobBackend, key string) (*BlobTurnLoopStore, error) {
	if backend == nil {
		return nil, ErrTurnLoopBlobBackendRequired
	}
	if key == "" {
		return nil, errors.New("gopact: turn loop blob key is required")
	}
	return &BlobTurnLoopStore{
		backend: backend,
		key:     key,
	}, nil
}

// Load returns the stored TurnLoop state from the backend blob.
func (s *BlobTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
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

	raw, err := s.backend.GetBlob(ctx, s.key)
	if errors.Is(err, ErrTurnLoopBlobNotFound) {
		return TurnLoopState{}, false, nil
	}
	if err != nil {
		return TurnLoopState{}, false, fmt.Errorf("gopact: read turn loop blob store: %w", err)
	}
	if len(raw) == 0 {
		return TurnLoopState{}, false, nil
	}

	data, err := decodeTurnLoopStoreData(raw)
	if err != nil {
		return TurnLoopState{}, false, err
	}
	return copyTurnLoopState(data.State), true, nil
}

// Save writes the TurnLoop state to the backend blob.
func (s *BlobTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
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

	raw, err := json.Marshal(fileTurnLoopStoreData{
		SchemaVersion: turnLoopStoreSchemaVersion,
		State:         copyTurnLoopState(state),
	})
	if err != nil {
		return fmt.Errorf("gopact: encode turn loop blob store: %w", err)
	}
	if err := s.backend.PutBlob(ctx, s.key, raw); err != nil {
		return fmt.Errorf("gopact: write turn loop blob store: %w", err)
	}
	return nil
}

func decodeTurnLoopStoreData(raw []byte) (fileTurnLoopStoreData, error) {
	var data fileTurnLoopStoreData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fileTurnLoopStoreData{}, fmt.Errorf("gopact: decode turn loop store: %w", err)
	}
	if data.SchemaVersion != "" && data.SchemaVersion != turnLoopStoreSchemaVersion {
		return fileTurnLoopStoreData{}, fmt.Errorf("%w: got %q want %q", ErrTurnLoopStoreSchemaMismatch, data.SchemaVersion, turnLoopStoreSchemaVersion)
	}
	if data.SchemaVersion == "" {
		data.SchemaVersion = turnLoopStoreSchemaVersion
	}
	return data, nil
}

// MemoryTurnLoopBlobBackend is an in-process blob backend for tests and local development.
type MemoryTurnLoopBlobBackend struct {
	mu    sync.RWMutex
	blobs map[string][]byte
}

// NewMemoryTurnLoopBlobBackend creates an empty in-process blob backend.
func NewMemoryTurnLoopBlobBackend() *MemoryTurnLoopBlobBackend {
	return &MemoryTurnLoopBlobBackend{
		blobs: make(map[string][]byte),
	}
}

// GetBlob returns a copy of one blob payload.
func (b *MemoryTurnLoopBlobBackend) GetBlob(ctx context.Context, key string) ([]byte, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	raw, ok := b.blobs[key]
	if !ok {
		return nil, ErrTurnLoopBlobNotFound
	}
	return append([]byte(nil), raw...), nil
}

// PutBlob stores or replaces one blob payload.
func (b *MemoryTurnLoopBlobBackend) PutBlob(ctx context.Context, key string, data []byte) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return errors.New("gopact: turn loop blob key is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.blobs == nil {
		b.blobs = make(map[string][]byte)
	}
	b.blobs[key] = append([]byte(nil), data...)
	return nil
}
