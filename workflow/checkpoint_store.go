package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gopact-ai/gopact/runlog"
)

// MemoryStore combines in-memory workflow checkpoints and execution history.
type MemoryStore struct {
	MemoryCheckpointer
	runlog.MemoryLog
}

// NewMemoryStore creates an empty in-memory workflow store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

var (
	_ Checkpointer         = (*MemoryStore)(nil)
	_ CheckpointHistory    = (*MemoryStore)(nil)
	_ CheckpointController = (*MemoryStore)(nil)
	_ runlog.Log           = (*MemoryStore)(nil)
)

// MemoryCheckpointer is an in-memory historical Checkpointer for tests and local runs.
type MemoryCheckpointer struct {
	mu      sync.RWMutex
	records map[string]CheckpointRecord
	history map[string][]CheckpointRecord
}

// NewMemoryCheckpointer creates an empty workflow checkpointer.
func NewMemoryCheckpointer() *MemoryCheckpointer {
	return &MemoryCheckpointer{records: make(map[string]CheckpointRecord), history: make(map[string][]CheckpointRecord)}
}

// Create stores a new running checkpoint.
func (store *MemoryCheckpointer) Create(ctx context.Context, record CheckpointRecord) error {
	if store == nil {
		return errors.New("workflow: checkpointer is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Version != 1 || record.Status != CheckpointRunning {
		return fmt.Errorf("%w: new checkpoint must be running at version one", ErrInvalidCheckpoint)
	}
	if err := validateCheckpointRecord(record); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.initialize()
	if _, exists := store.records[record.RunID]; exists {
		return ErrCheckpointExists
	}
	record = cloneCheckpointRecord(record)
	store.records[record.RunID] = record
	store.history[record.RunID] = append(store.history[record.RunID], cloneCheckpointRecord(record))
	return nil
}

// Load returns the latest independent checkpoint record.
func (store *MemoryCheckpointer) Load(ctx context.Context, runID string) (CheckpointRecord, error) {
	if store == nil {
		return CheckpointRecord{}, errors.New("workflow: checkpointer is nil")
	}
	if err := ctx.Err(); err != nil {
		return CheckpointRecord{}, err
	}
	if runID == "" {
		return CheckpointRecord{}, fmt.Errorf("%w: run id is required", ErrInvalidCheckpoint)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	record, exists := store.records[runID]
	if !exists {
		return CheckpointRecord{}, ErrCheckpointNotFound
	}
	return cloneCheckpointRecord(record), nil
}

// RenewLease extends the current ownership claim without creating a history version.
func (store *MemoryCheckpointer) RenewLease(ctx context.Context, lease CheckpointLease) error {
	if store == nil {
		return errors.New("workflow: checkpointer is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if lease.RunID == "" || lease.OwnerID == "" || lease.ClaimSequence <= 0 || lease.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: invalid checkpoint lease", ErrInvalidCheckpoint)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.records[lease.RunID]
	if !exists || current.Status != CheckpointRunning || current.OwnerID != lease.OwnerID || current.ClaimSequence != lease.ClaimSequence {
		return ErrCheckpointLeaseLost
	}
	current.LeaseExpiresAt = lease.ExpiresAt
	store.records[lease.RunID] = cloneCheckpointRecord(current)
	return nil
}

// Save writes a non-terminal checkpoint using CAS.
func (store *MemoryCheckpointer) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	return store.write(ctx, record, version, false)
}

// Finish writes a terminal checkpoint using CAS.
func (store *MemoryCheckpointer) Finish(ctx context.Context, record CheckpointRecord, version int64) error {
	return store.write(ctx, record, version, true)
}

// Reopen starts an explicit control epoch from one terminal run head.
func (store *MemoryCheckpointer) Reopen(ctx context.Context, record CheckpointRecord, version int64) error {
	if store == nil {
		return errors.New("workflow: checkpointer is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Version != version || record.Status != CheckpointRunning {
		return fmt.Errorf("%w: invalid reopened checkpoint", ErrInvalidCheckpoint)
	}
	if err := validateCheckpointRecord(record); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.records[record.RunID]
	if !exists {
		return ErrCheckpointNotFound
	}
	if !checkpointTerminal(current.Status) || current.Version != version {
		return fmt.Errorf("%w: checkpoint cannot reopen", ErrCheckpointConflict)
	}
	if !sameCheckpointIdentity(current, record) {
		return ErrCheckpointMismatch
	}
	record = cloneCheckpointRecord(record)
	record.Version = version + 1
	store.records[record.RunID] = record
	store.history[record.RunID] = append(store.history[record.RunID], cloneCheckpointRecord(record))
	return nil
}

// ListCheckpoints returns immutable versions in ascending order.
func (store *MemoryCheckpointer) ListCheckpoints(ctx context.Context, request CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	if store == nil {
		return nil, errors.New("workflow: checkpointer is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.RunID == "" || request.AfterVersion < 0 || request.Limit < 0 {
		return nil, fmt.Errorf("%w: invalid checkpoint history request", ErrInvalidCheckpoint)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	records, exists := store.history[request.RunID]
	if !exists {
		return nil, ErrCheckpointNotFound
	}
	result := make([]CheckpointRecord, 0, min(request.Limit, len(records)))
	for _, record := range records {
		if record.Version <= request.AfterVersion {
			continue
		}
		result = append(result, cloneCheckpointRecord(record))
		if request.Limit > 0 && len(result) == request.Limit {
			break
		}
	}
	return result, nil
}

func (store *MemoryCheckpointer) write(ctx context.Context, record CheckpointRecord, version int64, terminal bool) error {
	if store == nil {
		return errors.New("workflow: checkpointer is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if version <= 0 || record.Version != version {
		return fmt.Errorf("%w: snapshot version %d, expected %d", ErrCheckpointConflict, record.Version, version)
	}
	if terminal != checkpointTerminal(record.Status) {
		return fmt.Errorf("%w: status %q does not match write operation", ErrInvalidCheckpoint, record.Status)
	}
	if err := validateCheckpointRecord(record); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.records[record.RunID]
	if !exists {
		return ErrCheckpointNotFound
	}
	if checkpointTerminal(current.Status) {
		return fmt.Errorf("%w: checkpoint is terminal", ErrCheckpointConflict)
	}
	if current.Version != version {
		return fmt.Errorf("%w: stored version %d, expected %d", ErrCheckpointConflict, current.Version, version)
	}
	if !sameCheckpointIdentity(current, record) {
		return ErrCheckpointMismatch
	}
	record = cloneCheckpointRecord(record)
	record.Version = version + 1
	store.records[record.RunID] = record
	store.history[record.RunID] = append(store.history[record.RunID], cloneCheckpointRecord(record))
	return nil
}

func (store *MemoryCheckpointer) initialize() {
	if store.records == nil {
		store.records = make(map[string]CheckpointRecord)
	}
	if store.history == nil {
		store.history = make(map[string][]CheckpointRecord)
	}
}

func (store *MemoryCheckpointer) restore(record CheckpointRecord) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.initialize()
	record = cloneCheckpointRecord(record)
	store.records[record.RunID] = record
	store.history[record.RunID] = append(store.history[record.RunID], cloneCheckpointRecord(record))
}

func validateCheckpointRecord(record CheckpointRecord) error {
	if record.ID == "" || record.SessionID == "" || record.RunID == "" || record.WorkflowName == "" || record.TopologyVersion == "" || record.SchemaVersion != checkpointSchemaVersion {
		return fmt.Errorf("%w: checkpoint identity is incomplete", ErrInvalidCheckpoint)
	}
	if record.ConfirmedSequence < 0 || record.PendingSequence < 0 {
		return fmt.Errorf("%w: checkpoint sequence must not be negative", ErrInvalidCheckpoint)
	}
	if record.ReplayStatus != ReplayUnknown && record.ReplayStatus != ReplaySafe && record.ReplayStatus != ReplayUnsafe {
		return fmt.Errorf("%w: replay status %q", ErrInvalidCheckpoint, record.ReplayStatus)
	}
	if len(record.Payload) == 0 || len(record.Payload) > maxWorkflowCheckpointPayloadBytes {
		return fmt.Errorf("%w: checkpoint payload size is invalid", ErrInvalidCheckpoint)
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: checkpoint timestamps are required", ErrInvalidCheckpoint)
	}
	return nil
}

func sameCheckpointIdentity(left, right CheckpointRecord) bool {
	return left.ID == right.ID && left.SessionID == right.SessionID && left.RunID == right.RunID && left.WorkflowName == right.WorkflowName && left.TopologyVersion == right.TopologyVersion && left.SchemaVersion == right.SchemaVersion
}

func cloneCheckpointRecord(record CheckpointRecord) CheckpointRecord {
	record.Payload = append([]byte(nil), record.Payload...)
	return record
}
