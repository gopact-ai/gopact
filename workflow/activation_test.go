package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowResumeRetriesRunningActivationWithNextAttempt(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	bodyRuns := 0
	wf := New[int, int]("activation-resume", WithCheckpointer(store))
	wait := wf.Node("wait", func(_ context.Context, input int) (int, error) {
		bodyRuns++
		return input, nil
	})
	wf.Entry(wait)
	wf.Exit(wait)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), 1, gopact.WithRunID("activation-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeStarted {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs before resume = %d, want 0", bodyRuns)
	}
	payload, err := decodeCheckpointPayload[int](store.records["activation-resume"].Payload)
	if err != nil {
		t.Fatalf("decodeCheckpointPayload() error = %v", err)
	}
	record := payload.state().activations["act-1"]
	if record == nil || record.phase != activationRunning || record.attempt != 1 || record.nodeExecutionVersion != 1 {
		t.Fatalf("activation record = %+v, want running attempt 1 at node version 1", record)
	}
	expireRecordingLease(t, store, "activation-resume")
	output, err := compiled.Invoke(context.Background(), 1, WithResume(ResumeRequest{RunID: "activation-resume"}))
	if err != nil || output != 1 {
		t.Fatalf("resumed Invoke() = %d, %v, want 1", output, err)
	}
	if bodyRuns != 1 {
		t.Fatalf("body runs after resume = %d, want 1", bodyRuns)
	}
	finalPayload, err := decodeCheckpointPayload[int](store.records["activation-resume"].Payload)
	if err != nil {
		t.Fatalf("decode final checkpoint payload error = %v", err)
	}
	final := finalPayload.state().activations["act-1"]
	if final == nil || final.phase != activationCompleted || final.attempt != 2 || final.nodeExecutionVersion != 2 {
		t.Fatalf("final activation record = %+v, want completed attempt 2 at node version 2", final)
	}
}

func TestWorkflowInterruptPersistsActivationPhaseAndWorkflowEvent(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	wf := New[string, string]("activation-interrupt", WithCheckpointer(store))
	wait := wf.Node("wait", func(_ context.Context, input string) (string, error) { return input, nil })
	wait.Guard(BeforeRun("approval", GuardFunc[string, string](func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
		return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1", Subject: "approval"}}, nil
	})))
	wf.Entry(wait)
	wf.Exit(wait)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var events []string
	_, err = compiled.Invoke(context.Background(), "input", gopact.WithRunID("activation-interrupt"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		events = append(events, event.Type)
		return nil
	}))
	var interrupt InterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	if len(events) < 2 || events[len(events)-2] != EventGuardInterrupted || events[len(events)-1] != EventWorkflowInterrupted {
		t.Fatalf("terminal events = %v, want guard then workflow interrupted", events)
	}
	record := store.records["activation-interrupt"]
	if record.Status != CheckpointInterrupted {
		t.Fatalf("checkpoint status = %q, want %q", record.Status, CheckpointInterrupted)
	}
	payload, err := decodeCheckpointPayload[string](record.Payload)
	if err != nil {
		t.Fatalf("decodeCheckpointPayload() error = %v", err)
	}
	activation := payload.state().activations["act-1"]
	if activation == nil || activation.phase != activationInterrupted {
		t.Fatalf("activation record = %+v, want interrupted", activation)
	}
}
