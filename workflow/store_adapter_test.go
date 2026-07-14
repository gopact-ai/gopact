package workflow

import (
	"context"
	"time"

	"github.com/gopact-ai/gopact/runlog"
)

type checkpointerStore struct {
	Checkpointer
	history CheckpointHistory
	log     runlog.Log
}

// storeWithCheckpointer adapts legacy single-threaded checkpoint white-box tests.
// Concurrency and fencing tests use a real MemoryStore instead.
func storeWithCheckpointer(checkpointer Checkpointer) Store {
	return storeWithCheckpointerAndLog(checkpointer, new(runlog.MemoryLog))
}

func storeWithCheckpointerAndLog(checkpointer Checkpointer, log runlog.Log) Store {
	store := &checkpointerStore{Checkpointer: checkpointer, log: log}
	store.history, _ = checkpointer.(CheckpointHistory)
	return store
}

func newTestRunLogSnapshotStore(log runlog.Log, history CheckpointHistory) RunLogSnapshotStore {
	return RunLogSnapshotStore{log: log, checkpoints: history}
}

func (store *checkpointerStore) ListCheckpoints(ctx context.Context, request CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	if store.history != nil {
		return store.history.ListCheckpoints(ctx, request)
	}
	return nil, ErrCheckpointNotFound
}

func (store *checkpointerStore) Append(ctx context.Context, record runlog.Record) error {
	return store.log.Append(ctx, record)
}

func (store *checkpointerStore) List(ctx context.Context, query runlog.Query) ([]runlog.Record, error) {
	return store.log.List(ctx, query)
}

func (store *checkpointerStore) AppendFenced(ctx context.Context, record runlog.Record, fence runlog.Fence) error {
	checkpoint, err := store.Load(ctx, record.RunID)
	if err != nil {
		return err
	}
	if checkpoint.Status != CheckpointRunning || checkpoint.OwnerID != fence.OwnerID ||
		checkpoint.ClaimSequence != fence.ClaimSequence || !checkpoint.LeaseExpiresAt.After(time.Now()) {
		return ErrCheckpointLeaseLost
	}
	return store.log.Append(ctx, record)
}
