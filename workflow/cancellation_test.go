package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

func TestWorkflowCallerCancellationCommitsCanceledTerminal(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	started := make(chan struct{})
	wf := New[int, int]("caller-cancel", WithStore(storeWithCheckpointer(&contextCheckpointer{recordingCheckpointer: store})))
	wait := wf.Node("wait", func(ctx context.Context, input int) (int, error) {
		close(started)
		<-ctx.Done()
		return 0, ctx.Err()
	})
	wf.Entry(wait)
	wf.Exit(wait)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var events []gopact.Event
	done := make(chan error, 1)
	go func() {
		_, err := compiled.Invoke(ctx, 1, gopact.WithRunID("caller-cancel"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}))
		done <- err
	}()
	<-started
	cancel()
	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Invoke() error = %v, want context canceled", err)
	}
	record := store.records["caller-cancel"]
	if record.Status != CheckpointCanceled {
		t.Fatalf("checkpoint status = %q, want %q", record.Status, CheckpointCanceled)
	}
	if len(events) < 2 || events[len(events)-2].Type != EventNodeFailed ||
		events[len(events)-1].Type != EventWorkflowCanceled {
		t.Fatalf("last events = %v, want node failed then workflow canceled", events)
	}
}

func TestWorkflowCallerLeaseSentinelCauseCommitsCanceledTerminal(t *testing.T) {
	store := NewMemoryStore()
	started := make(chan struct{})
	wf := New[int, int](
		"caller-lease-sentinel",
		WithStore(store),
	)
	wait := wf.Node("wait", func(ctx context.Context, input int) (int, error) {
		close(started)
		<-ctx.Done()
		return input, ctx.Err()
	})
	wf.Entry(wait)
	wf.Exit(wait)
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := wf.Invoke(ctx, 1, gopact.WithRunID("caller-lease-sentinel"))
		done <- err
	}()
	<-started
	cancel(ErrCheckpointLeaseLost)
	if err := <-done; !errors.Is(err, ErrCheckpointLeaseLost) {
		t.Fatalf("Invoke() error = %v, want caller cancellation cause", err)
	}
	record, err := store.Load(t.Context(), "caller-lease-sentinel")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if record.Status != CheckpointCanceled {
		t.Fatalf("checkpoint status = %q, want %q", record.Status, CheckpointCanceled)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: "caller-lease-sentinel"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) == 0 || records[len(records)-1].EventType != EventWorkflowCanceled {
		t.Fatalf("records = %+v, want workflow.canceled terminal event", records)
	}
}

type contextCheckpointer struct {
	recordingCheckpointer *recordingCheckpointer
}

func (store *contextCheckpointer) Create(ctx context.Context, record CheckpointRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.recordingCheckpointer.Create(ctx, record)
}

func (store *contextCheckpointer) Load(ctx context.Context, runID string) (CheckpointRecord, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointRecord{}, err
	}
	return store.recordingCheckpointer.Load(ctx, runID)
}

func (store *contextCheckpointer) Claim(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.recordingCheckpointer.Claim(ctx, record, version)
}

func (store *contextCheckpointer) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.recordingCheckpointer.Save(ctx, record, version)
}

func (store *contextCheckpointer) Finish(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.recordingCheckpointer.Finish(ctx, record, version)
}

func (store *contextCheckpointer) RenewLease(ctx context.Context, lease CheckpointLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.recordingCheckpointer.RenewLease(ctx, lease)
}
