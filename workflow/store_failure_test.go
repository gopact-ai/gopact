package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type operationFailStore struct {
	Store
	operation string
	err       error
}

func (store operationFailStore) fail(operation string) error {
	if store.operation == operation {
		return store.err
	}
	return nil
}

func (store operationFailStore) Create(ctx context.Context, record CheckpointRecord) error {
	if err := store.fail("Create"); err != nil {
		return err
	}
	return store.Store.Create(ctx, record)
}

func (store operationFailStore) Claim(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.fail("Claim"); err != nil {
		return err
	}
	return store.Store.Claim(ctx, record, version)
}

func (store operationFailStore) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.fail("Save"); err != nil {
		return err
	}
	return store.Store.Save(ctx, record, version)
}

func (store operationFailStore) Finish(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.fail("Finish"); err != nil {
		return err
	}
	return store.Store.Finish(ctx, record, version)
}

func (store operationFailStore) RenewLease(ctx context.Context, lease CheckpointLease) error {
	if err := store.fail("RenewLease"); err != nil {
		return err
	}
	return store.Store.RenewLease(ctx, lease)
}

func (store operationFailStore) ListCheckpoints(ctx context.Context, request CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	if err := store.fail("ListCheckpoints"); err != nil {
		return nil, err
	}
	return store.Store.ListCheckpoints(ctx, request)
}

func (store operationFailStore) AppendFenced(ctx context.Context, record runlog.Record, fence runlog.Fence) error {
	if err := store.fail("AppendFenced"); err != nil {
		return err
	}
	return store.Store.AppendFenced(ctx, record, fence)
}

func TestWorkflowStoreCreateAndJournalFailuresStopRun(t *testing.T) {
	for _, operation := range []string{"Create", "AppendFenced"} {
		t.Run(operation, func(t *testing.T) {
			storeErr := errors.New("store unavailable")
			store := operationFailStore{Store: NewMemoryStore(), operation: operation, err: storeErr}
			wf := New[string, string]("store-failure", WithStore(store))
			node := testNode(wf, "node", func(ctx context.Context, input string) (string, error) {
				if operation == "AppendFenced" {
					return "", Emit(ctx, gopact.Event{Type: "audit.custom"})
				}
				return input, nil
			})
			wf.Entry(node)
			wf.Exit(node)

			if _, err := wf.Invoke(t.Context(), "input"); !errors.Is(err, storeErr) {
				t.Fatalf("Invoke() error = %v, want %v", err, storeErr)
			}
		})
	}
}

func TestWorkflowStoreFinishFailureStopsCompletedRun(t *testing.T) {
	storeErr := errors.New("finish unavailable")
	store := operationFailStore{Store: NewMemoryStore(), operation: "Finish", err: storeErr}
	wf := New[string, string]("finish-failure", WithStore(store))
	node := testNode(wf, "node", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)

	if _, err := wf.Invoke(t.Context(), "input"); !errors.Is(err, storeErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, storeErr)
	}
}

func TestWorkflowInterruptAcknowledgementSaveFailureIsNotReportedAsInterrupted(t *testing.T) {
	storeErr := errors.New("checkpoint unavailable")
	store := operationFailStore{Store: NewMemoryStore(), operation: "Save", err: storeErr}
	wf := interruptFailureWorkflow(store)

	_, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("save-failure"))
	var interrupt InterruptError
	if !errors.Is(err, storeErr) || errors.As(err, &interrupt) {
		t.Fatalf("Invoke() error = %v, want store error and no InterruptError", err)
	}
}

func TestWorkflowStoreClaimFailureStopsResume(t *testing.T) {
	base := NewMemoryStore()
	wf := interruptFailureWorkflow(base)
	_, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("claim-failure"))
	var interrupt InterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}

	storeErr := errors.New("claim unavailable")
	wf = interruptFailureWorkflow(operationFailStore{Store: base, operation: "Claim", err: storeErr})
	_, err = wf.Invoke(t.Context(), "", WithResume(ResumeRequest{
		RunID: "claim-failure", CheckpointID: interrupt.CheckpointID,
		Resolutions: []InterruptResolution{{InterruptID: "approval", PayloadRef: "approved"}},
	}))
	if !errors.Is(err, storeErr) {
		t.Fatalf("Resume() error = %v, want %v", err, storeErr)
	}
}

func TestWorkflowStoreListCheckpointFailureStopsSnapshot(t *testing.T) {
	base := NewMemoryStore()
	wf := New[string, string]("list-failure", WithStore(base))
	node := testNode(wf, "node", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("list-failure")); err != nil {
		t.Fatal(err)
	}

	storeErr := errors.New("history unavailable")
	wf = New[string, string]("list-failure", WithStore(operationFailStore{
		Store: base, operation: "ListCheckpoints", err: storeErr,
	}))
	node = testNode(wf, "node", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Snapshot(t.Context(), SnapshotRequest{RunID: "list-failure"}); !errors.Is(err, storeErr) {
		t.Fatalf("Snapshot() error = %v, want %v", err, storeErr)
	}
}

func interruptFailureWorkflow(store Store) *Workflow[string, string] {
	wf := New[string, string]("interrupt-failure", WithStore(store))
	node := testNode(wf, "node", func(_ context.Context, input string) (string, error) { return input, nil })
	node.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval"}}, nil
		},
	)))
	wf.Entry(node)
	wf.Exit(node)
	return wf
}
