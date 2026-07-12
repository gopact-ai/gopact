package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowCallerCancellationCommitsCanceledTerminal(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	started := make(chan struct{})
	wf := New[int, int]("caller-cancel", WithCheckpointer(&contextCheckpointer{recordingCheckpointer: store}))
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
	if len(events) == 0 || events[len(events)-1].Type != EventWorkflowCanceled {
		t.Fatalf("last event = %v, want workflow canceled", events)
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
