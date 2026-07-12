package workflow

import (
	"context"
	"errors"
	"sync"

	"github.com/gopact-ai/gopact/runlog"
)

type mirroredCheckpointer struct {
	primary  *MemoryStore
	mirror   Checkpointer
	strict   bool
	mu       sync.Mutex
	disabled map[string]struct{}
}

func (store *mirroredCheckpointer) Create(ctx context.Context, record CheckpointRecord) error {
	if err := store.primary.Create(ctx, record); err != nil {
		return err
	}
	return store.writeMirror(record.RunID, func() error { return store.mirror.Create(ctx, record) })
}

func (store *mirroredCheckpointer) Load(ctx context.Context, runID string) (CheckpointRecord, error) {
	record, err := store.primary.Load(ctx, runID)
	if err == nil || !errors.Is(err, ErrCheckpointNotFound) {
		return record, err
	}
	record, err = store.mirror.Load(ctx, runID)
	if err != nil {
		return CheckpointRecord{}, err
	}
	store.primary.restore(record)
	return record, nil
}

func (store *mirroredCheckpointer) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.primary.Save(ctx, record, version); err != nil {
		return err
	}
	return store.writeMirror(record.RunID, func() error { return store.mirror.Save(ctx, record, version) })
}

func (store *mirroredCheckpointer) Finish(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.primary.Finish(ctx, record, version); err != nil {
		return err
	}
	return store.writeMirror(record.RunID, func() error { return store.mirror.Finish(ctx, record, version) })
}

func (store *mirroredCheckpointer) Reopen(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.primary.Reopen(ctx, record, version); err != nil {
		return err
	}
	controller, ok := store.mirror.(CheckpointController)
	if !ok {
		return store.writeMirror(record.RunID, func() error {
			return errors.New("workflow: checkpointer does not support control")
		})
	}
	return store.writeMirror(record.RunID, func() error { return controller.Reopen(ctx, record, version) })
}

func (store *mirroredCheckpointer) ListCheckpoints(ctx context.Context, request CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	records, err := store.primary.ListCheckpoints(ctx, request)
	if err == nil || !errors.Is(err, ErrCheckpointNotFound) {
		return records, err
	}
	history, ok := store.mirror.(CheckpointHistory)
	if !ok {
		return nil, err
	}
	return history.ListCheckpoints(ctx, request)
}

func (store *mirroredCheckpointer) writeMirror(runID string, write func() error) error {
	store.mu.Lock()
	_, disabled := store.disabled[runID]
	store.mu.Unlock()
	if disabled {
		return nil
	}
	if err := write(); err != nil {
		if store.strict {
			return err
		}
		store.mu.Lock()
		if store.disabled == nil {
			store.disabled = make(map[string]struct{})
		}
		store.disabled[runID] = struct{}{}
		store.mu.Unlock()
	}
	return nil
}

type mirroredLog struct {
	primary *MemoryStore
	mirror  runlog.Log
	strict  bool
}

func (log mirroredLog) Append(ctx context.Context, record runlog.Record) error {
	if err := log.primary.Append(ctx, record); err != nil {
		return err
	}
	if err := log.mirror.Append(ctx, record); err != nil && log.strict {
		return err
	}
	return nil
}

func (log mirroredLog) List(ctx context.Context, query runlog.Query) ([]runlog.Record, error) {
	if query.RunID == "" {
		return log.primary.List(ctx, query)
	}
	existing, err := log.primary.List(ctx, runlog.Query{RunID: query.RunID, Limit: 1})
	if err != nil || len(existing) > 0 {
		if err != nil {
			return nil, err
		}
		return log.primary.List(ctx, query)
	}
	return log.mirror.List(ctx, query)
}

var (
	_ Checkpointer         = (*mirroredCheckpointer)(nil)
	_ CheckpointHistory    = (*mirroredCheckpointer)(nil)
	_ CheckpointController = (*mirroredCheckpointer)(nil)
	_ runlog.Log           = mirroredLog{}
)
