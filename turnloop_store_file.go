package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const turnLoopStoreSchemaVersion = "turnloop.v1"

var ErrTurnLoopStoreSchemaMismatch = errors.New("gopact: turn loop store schema mismatch")

// FileTurnLoopStore persists TurnLoop queue state into one local JSON file.
type FileTurnLoopStore struct {
	mu   sync.Mutex
	path string
}

// NewFileTurnLoopStore creates a local file-backed TurnLoop store.
func NewFileTurnLoopStore(path string) (*FileTurnLoopStore, error) {
	if path == "" {
		return nil, errors.New("gopact: turn loop store path is required")
	}
	return &FileTurnLoopStore{path: path}, nil
}

// Load returns the stored TurnLoop state from disk.
func (s *FileTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
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

	data, ok, err := s.readDataLocked()
	if err != nil {
		return TurnLoopState{}, false, err
	}
	if !ok {
		return TurnLoopState{}, false, nil
	}
	return copyTurnLoopState(data.State), true, nil
}

// Save writes the TurnLoop state to disk using atomic rename.
func (s *FileTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
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

	return s.writeDataLocked(fileTurnLoopStoreData{
		SchemaVersion: turnLoopStoreSchemaVersion,
		State:         copyTurnLoopState(state),
	})
}

type fileTurnLoopStoreData struct {
	SchemaVersion string        `json:"schema_version,omitempty"`
	State         TurnLoopState `json:"state,omitempty"`
}

func (s *FileTurnLoopStore) readDataLocked() (fileTurnLoopStoreData, bool, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return fileTurnLoopStoreData{}, false, nil
	}
	if err != nil {
		return fileTurnLoopStoreData{}, false, fmt.Errorf("gopact: read turn loop store: %w", err)
	}
	if len(raw) == 0 {
		return fileTurnLoopStoreData{}, false, nil
	}

	var data fileTurnLoopStoreData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fileTurnLoopStoreData{}, false, fmt.Errorf("gopact: decode turn loop store: %w", err)
	}
	if data.SchemaVersion != "" && data.SchemaVersion != turnLoopStoreSchemaVersion {
		return fileTurnLoopStoreData{}, false, fmt.Errorf("%w: got %q want %q", ErrTurnLoopStoreSchemaMismatch, data.SchemaVersion, turnLoopStoreSchemaVersion)
	}
	if data.SchemaVersion == "" {
		data.SchemaVersion = turnLoopStoreSchemaVersion
	}
	return data, true, nil
}

func (s *FileTurnLoopStore) writeDataLocked(data fileTurnLoopStoreData) error {
	data.SchemaVersion = turnLoopStoreSchemaVersion
	raw, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return fmt.Errorf("gopact: encode turn loop store: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("gopact: create turn loop store directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("gopact: create turn loop store temp file: %w", err)
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
		return fmt.Errorf("gopact: write turn loop store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("gopact: close turn loop store temp file: %w", err)
	}
	closed = true
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("gopact: replace turn loop store: %w", err)
	}
	return nil
}
