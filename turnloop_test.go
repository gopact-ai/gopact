package gopact

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTurnLoopRunEmitsTurnBoundaryEvents(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{
		events: []Event{
			{Type: EventRunStarted},
			{Type: EventRunCompleted},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "input", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := turnEventTypes(events)
	want := []EventType{EventTurnStarted, EventRunStarted, EventRunCompleted, EventTurnCompleted}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[0].IDs.RunID != "run-1" || events[3].IDs.RunID != "run-1" {
		t.Fatalf("turn event IDs = %+v / %+v", events[0].IDs, events[3].IDs)
	}
}

func TestTurnLoopCancelCancelsActiveRun(t *testing.T) {
	blocking := newBlockingTurnRunnable()
	runner, err := NewRunner(blocking)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	done := make(chan collectResult, 1)
	go func() {
		events, err := collectTurnEvents(loop.Run(context.Background(), "input"))
		done <- collectResult{events: events, err: err}
	}()

	<-blocking.started
	if !loop.Cancel("user stop") {
		t.Fatal("Cancel() = false, want true")
	}

	result := <-done
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", result.err)
	}
	got := turnEventTypes(result.events)
	want := []EventType{EventTurnStarted, EventRunStarted, EventRunCanceled, EventTurnCanceled}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if result.events[3].Metadata["reason"] != "user stop" {
		t.Fatalf("turn_canceled metadata = %+v, want reason", result.events[3].Metadata)
	}
}

func TestTurnLoopPreemptCancelsActiveRun(t *testing.T) {
	blocking := newBlockingTurnRunnable()
	runner, err := NewRunner(blocking)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	firstDone := make(chan collectResult, 1)
	go func() {
		events, err := collectTurnEvents(loop.Run(context.Background(), "first"))
		firstDone <- collectResult{events: events, err: err}
	}()
	<-blocking.started

	secondEvents, err := collectTurnEvents(loop.Run(context.Background(), "second", WithPreempt()))
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondEvents[0].Type != EventTurnPreempted {
		t.Fatalf("first second-run event = %s, want turn_preempted", secondEvents[0].Type)
	}

	first := <-firstDone
	if !errors.Is(first.err, context.Canceled) {
		t.Fatalf("first Run() error = %v, want context canceled", first.err)
	}
}

func TestTurnLoopRunEmitsResumeEvent(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "input", WithResume(ResumeRequest{
		InterruptID: "interrupt-1",
		Payload:     "approved",
	})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := turnEventTypes(events)
	want := []EventType{EventTurnStarted, EventTurnResumed, EventRunCompleted, EventTurnCompleted}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[1].Metadata["interrupt_id"] != "interrupt-1" {
		t.Fatalf("resume metadata = %+v", events[1].Metadata)
	}
}

func TestTurnLoopRunPassesResumeRequestToRunner(t *testing.T) {
	recorder := &recordingTurnRunnable{events: []Event{{Type: EventRunCompleted}}}
	runner, err := NewRunner(recorder)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	if _, err := collectTurnEvents(loop.Run(context.Background(), "input", WithResume(ResumeRequest{
		CheckpointID: "checkpoint-1",
		InterruptID:  "interrupt-1",
		Payload:      "approved",
	}))); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if recorder.lastConfig.ResumeRequest == nil || recorder.lastConfig.ResumeRequest.InterruptID != "interrupt-1" {
		t.Fatalf("runner resume request = %+v, want interrupt-1", recorder.lastConfig.ResumeRequest)
	}
	if recorder.lastConfig.ResumeRequest.CheckpointID != "checkpoint-1" || recorder.lastConfig.ResumeRequest.Payload != "approved" {
		t.Fatalf("runner resume request = %+v, want checkpoint and payload", recorder.lastConfig.ResumeRequest)
	}
}

func TestTurnLoopPushQueuesInputForNextRun(t *testing.T) {
	recorder := &recordingTurnRunnable{events: []Event{{Type: EventRunCompleted}}}
	runner, err := NewRunner(recorder)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	record, err := loop.Push(context.Background(), "queued", WithTurnRuntimeIDs(RuntimeIDs{ThreadID: "thread-1"}))
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if record.Kind != TurnInputUser || record.Input != "queued" {
		t.Fatalf("Push() record = %+v, want queued user input", record)
	}
	if got := loop.Pending(); len(got) != 1 || got[0].Input != "queued" {
		t.Fatalf("Pending() = %+v, want queued input", got)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "current", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnInputReceived,
		EventTurnStarted,
		EventTurnInputMerged,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[0].Metadata["kind"] != string(TurnInputUser) {
		t.Fatalf("input received metadata = %+v, want user kind", events[0].Metadata)
	}
	if events[2].Metadata["pending_count"] != 1 {
		t.Fatalf("input merged metadata = %+v, want pending_count 1", events[2].Metadata)
	}
	batch, ok := recorder.lastInput.(TurnInputBatch)
	if !ok {
		t.Fatalf("runner input type = %T, want TurnInputBatch", recorder.lastInput)
	}
	if batch.Current != "current" || len(batch.Pending) != 1 || batch.Pending[0].Input != "queued" {
		t.Fatalf("runner batch = %+v, want current plus queued", batch)
	}
	if pending := loop.Pending(); len(pending) != 0 {
		t.Fatalf("Pending() after Run = %+v, want empty", pending)
	}
}

func TestTurnLoopResumeQueuesResumeForNextRun(t *testing.T) {
	recorder := &recordingTurnRunnable{events: []Event{{Type: EventRunCompleted}}}
	runner, err := NewRunner(recorder)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	record, err := loop.Resume(context.Background(), ResumeRequest{
		CheckpointID: "checkpoint-1",
		InterruptID:  "interrupt-1",
		Payload:      "approved",
	}, WithTurnRuntimeIDs(RuntimeIDs{ThreadID: "thread-1"}))
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if record.Kind != TurnInputResume || record.Resume == nil || record.Resume.InterruptID != "interrupt-1" {
		t.Fatalf("Resume() record = %+v, want resume input", record)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "current", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnInputReceived,
		EventTurnStarted,
		EventTurnInputMerged,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[0].Metadata["kind"] != string(TurnInputResume) || events[0].Metadata["interrupt_id"] != "interrupt-1" {
		t.Fatalf("input received metadata = %+v, want resume interrupt", events[0].Metadata)
	}
	batch, ok := recorder.lastInput.(TurnInputBatch)
	if !ok {
		t.Fatalf("runner input type = %T, want TurnInputBatch", recorder.lastInput)
	}
	if len(batch.Pending) != 1 || batch.Pending[0].Resume == nil || batch.Pending[0].Resume.InterruptID != "interrupt-1" {
		t.Fatalf("runner batch = %+v, want queued resume", batch)
	}
}

func TestTurnLoopRunUsesCustomInputMergeStrategy(t *testing.T) {
	recorder := &recordingTurnRunnable{events: []Event{{Type: EventRunCompleted}}}
	runner, err := NewRunner(recorder)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	var gotRequest TurnInputMergeRequest
	loop, err := NewTurnLoop(runner, WithTurnInputMerge(func(ctx context.Context, req TurnInputMergeRequest) (TurnInputMergeResult, error) {
		gotRequest = req
		req.Pending[0].Input = "mutated"
		if req.Pending[0].Metadata == nil {
			req.Pending[0].Metadata = make(map[string]any)
		}
		req.Pending[0].Metadata["source"] = "mutated"
		return TurnInputMergeResult{
			Input: map[string]any{
				"current": req.Current,
				"pending": req.Pending[0].Input,
				"resume":  req.Resume.Payload,
			},
			Metadata: map[string]any{"strategy": "compact"},
		}, nil
	}))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := loop.Push(context.Background(), "queued", WithTurnRuntimeIDs(RuntimeIDs{ThreadID: "thread-1"})); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "current",
		WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}),
		WithResume(ResumeRequest{InterruptID: "interrupt-1", Payload: "approved"}),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnInputReceived,
		EventTurnStarted,
		EventTurnInputMerged,
		EventTurnResumed,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if gotRequest.Current != "current" || len(gotRequest.Pending) != 1 || gotRequest.Pending[0].Input != "mutated" {
		t.Fatalf("merge request = %+v, want current plus mutable request copy", gotRequest)
	}
	if events[2].Metadata["strategy"] != "compact" || events[2].Metadata["pending_count"] != 1 {
		t.Fatalf("input merged metadata = %+v, want strategy and pending_count", events[2].Metadata)
	}
	merged, ok := recorder.lastInput.(map[string]any)
	if !ok {
		t.Fatalf("runner input type = %T, want merged map", recorder.lastInput)
	}
	if merged["current"] != "current" || merged["pending"] != "mutated" || merged["resume"] != "approved" {
		t.Fatalf("runner input = %+v, want custom merged input", merged)
	}
	if pending := loop.Pending(); len(pending) != 0 {
		t.Fatalf("Pending() after Run = %+v, want empty", pending)
	}
}

func TestTurnLoopStoreRestoresPendingInput(t *testing.T) {
	store := NewMemoryTurnLoopStore()
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := loop.Push(context.Background(), "queued", WithTurnRuntimeIDs(RuntimeIDs{ThreadID: "thread-1"})); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	recorder := &recordingTurnRunnable{events: []Event{{Type: EventRunCompleted}}}
	restoredRunner, err := NewRunner(recorder)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	restored, err := NewTurnLoop(restoredRunner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop(restored) error = %v", err)
	}
	if pending := restored.Pending(); len(pending) != 1 || pending[0].Input != "queued" {
		t.Fatalf("restored Pending() = %+v, want queued input", pending)
	}

	events, err := collectTurnEvents(restored.Run(context.Background(), "current", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnInputReceived,
		EventTurnStarted,
		EventTurnInputMerged,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	batch, ok := recorder.lastInput.(TurnInputBatch)
	if !ok {
		t.Fatalf("runner input type = %T, want TurnInputBatch", recorder.lastInput)
	}
	if batch.Current != "current" || len(batch.Pending) != 1 || batch.Pending[0].Input != "queued" {
		t.Fatalf("runner batch = %+v, want current plus queued", batch)
	}

	again, err := NewTurnLoop(restoredRunner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop(again) error = %v", err)
	}
	if pending := again.Pending(); len(pending) != 0 {
		t.Fatalf("Pending() after successful run = %+v, want empty", pending)
	}
}

func TestTurnLoopStoreRestoresInterruptedInput(t *testing.T) {
	store := NewMemoryTurnLoopStore()
	interruptRunner, err := NewRunner(interruptingTurnRunnable{})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(interruptRunner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}))); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Run() error = %v, want ErrInterrupted", err)
	}

	runnable := &recordingTurnRunnable{events: []Event{{Type: EventRunCompleted}}}
	resumeRunner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	restored, err := NewTurnLoop(resumeRunner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop(restored) error = %v", err)
	}
	interrupted, ok := restored.Interrupted()
	if !ok || interrupted.Input != "question" {
		t.Fatalf("restored Interrupted() = %+v ok=%v, want question", interrupted, ok)
	}

	events, err := collectTurnEvents(restored.Run(context.Background(), "resume input",
		WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-2", ThreadID: "thread-1"}),
		WithResume(ResumeRequest{InterruptID: "interrupt-1", Payload: "approved"}),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnStarted,
		EventTurnInputMerged,
		EventTurnResumed,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	batch, ok := runnable.lastInput.(TurnInputBatch)
	if !ok {
		t.Fatalf("runner input type = %T, want TurnInputBatch", runnable.lastInput)
	}
	if batch.Interrupted == nil || batch.Interrupted.Input != "question" || batch.Current != "resume input" {
		t.Fatalf("runner batch = %+v, want interrupted question plus resume input", batch)
	}

	again, err := NewTurnLoop(resumeRunner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop(again) error = %v", err)
	}
	if _, ok := again.Interrupted(); ok {
		t.Fatal("Interrupted() ok = true after completed resume run, want false")
	}
}

func TestTurnLoopResumeRejectsInvalidResumeRequest(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	if _, err := loop.Resume(context.Background(), ResumeRequest{}); err == nil {
		t.Fatal("Resume() error = nil, want invalid resume error")
	}
	if pending := loop.Pending(); len(pending) != 0 {
		t.Fatalf("Pending() = %+v, want empty after invalid resume", pending)
	}
}

func TestTurnLoopPushRejectsInvalidResumeOption(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	if _, err := loop.Push(context.Background(), "input", WithResume(ResumeRequest{})); err == nil {
		t.Fatal("Push() error = nil, want invalid resume error")
	}
	if pending := loop.Pending(); len(pending) != 0 {
		t.Fatalf("Pending() = %+v, want empty after invalid resume", pending)
	}
}

func TestTurnLoopRunRemembersInterruptedInput(t *testing.T) {
	runner, err := NewRunner(interruptingTurnRunnable{})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "question", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Run() error = %v, want ErrInterrupted", err)
	}
	got := turnEventTypes(events)
	want := []EventType{EventTurnStarted, EventRunInterrupted, EventTurnInterrupted}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	interrupted, ok := loop.Interrupted()
	if !ok {
		t.Fatal("Interrupted() ok = false, want true")
	}
	if interrupted.Input != "question" || interrupted.Kind != TurnInputUser {
		t.Fatalf("Interrupted() = %+v, want question user input", interrupted)
	}
}

func TestTurnLoopResumeRunIncludesInterruptedInput(t *testing.T) {
	runnable := &interruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}))); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "resume input",
		WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-2", ThreadID: "thread-1"}),
		WithResume(ResumeRequest{InterruptID: "interrupt-1", Payload: "approved"}),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnStarted,
		EventTurnInputMerged,
		EventTurnResumed,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	batch, ok := runnable.lastInput.(TurnInputBatch)
	if !ok {
		t.Fatalf("runner input type = %T, want TurnInputBatch", runnable.lastInput)
	}
	if batch.Interrupted == nil || batch.Interrupted.Input != "question" || batch.Current != "resume input" {
		t.Fatalf("runner batch = %+v, want interrupted question plus resume input", batch)
	}
	if _, ok := loop.Interrupted(); ok {
		t.Fatal("Interrupted() ok = true after completed resume run, want false")
	}
}

func TestTurnLoopRunRejectsResumePayloadSchemaMismatchBeforeRunner(t *testing.T) {
	runnable := &schemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question")); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "resume input", WithResume(ResumeRequest{
		InterruptID: "interrupt-1",
		Payload: map[string]any{
			"answer": 42,
		},
	})))
	if !errors.Is(err, ErrResumePayloadInvalid) {
		t.Fatalf("resume Run() error = %v, want ErrResumePayloadInvalid", err)
	}
	if got := runnable.calls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want only initial interrupted run", got)
	}
	want := []EventType{EventTurnFailed}
	if got := turnEventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestTurnLoopResumeRejectsPayloadSchemaMismatch(t *testing.T) {
	runnable := &schemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question")); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	_, err = loop.Resume(context.Background(), ResumeRequest{
		InterruptID: "interrupt-1",
		Payload: map[string]any{
			"answer": 42,
		},
	})
	if !errors.Is(err, ErrResumePayloadInvalid) {
		t.Fatalf("Resume() error = %v, want ErrResumePayloadInvalid", err)
	}
	if pending := loop.Pending(); len(pending) != 0 {
		t.Fatalf("Pending() = %+v, want empty after rejected resume", pending)
	}
}

func TestTurnLoopRunAllowsResumePayloadMatchingSchema(t *testing.T) {
	runnable := &schemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question")); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "resume input", WithResume(ResumeRequest{
		InterruptID: "interrupt-1",
		Payload: map[string]any{
			"answer": "approved",
		},
	})))
	if err != nil {
		t.Fatalf("resume Run() error = %v", err)
	}
	if got := runnable.calls.Load(); got != 2 {
		t.Fatalf("runner calls = %d, want initial interrupt plus resume", got)
	}
	want := []EventType{
		EventTurnStarted,
		EventTurnInputMerged,
		EventTurnResumed,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if got := turnEventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestTurnLoopRunUsesInjectedSchemaValidatorForResumeGate(t *testing.T) {
	runnable := &refSchemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	called := false
	validator := JSONSchemaValidatorFunc(func(ctx context.Context, schema JSONSchema, value any) error {
		called = true
		if schema["$ref"] != "#/$defs/resume" {
			t.Fatalf("schema = %+v, want resume ref schema", schema)
		}
		return nil
	})
	loop, err := NewTurnLoop(runner, WithTurnJSONSchemaValidator(validator))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question")); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "resume input", WithResume(ResumeRequest{
		InterruptID: "interrupt-1",
		Payload: map[string]any{
			"answer": 42,
		},
	})))
	if err != nil {
		t.Fatalf("resume Run() error = %v", err)
	}
	if !called {
		t.Fatal("validator called = false, want true")
	}
	if got := runnable.calls.Load(); got != 2 {
		t.Fatalf("runner calls = %d, want initial interrupt plus resumed run", got)
	}
	if runnable.lastConfig.JSONSchemaValidator == nil {
		t.Fatal("runner schema validator = nil, want propagated validator")
	}
	want := []EventType{
		EventTurnStarted,
		EventTurnInputMerged,
		EventTurnResumed,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if got := turnEventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestTurnLoopRunAuthorizesResumeWithPolicy(t *testing.T) {
	runnable := &schemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	var gotRequest PolicyRequest
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		gotRequest = req
		if req.Boundary != PolicyBoundaryTurn {
			t.Fatalf("policy boundary = %q, want turn", req.Boundary)
		}
		if req.Action != PolicyActionResume {
			t.Fatalf("policy action = %q, want resume", req.Action)
		}
		input, ok := req.Input.(TurnLoopPolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want TurnLoopPolicyInput", req.Input)
		}
		if input.Resume.InterruptID != "interrupt-1" {
			t.Fatalf("policy resume = %+v, want interrupt-1", input.Resume)
		}
		if input.Interrupted == nil || input.Interrupted.Input != "question" {
			t.Fatalf("policy interrupted input = %+v, want question", input.Interrupted)
		}
		return PolicyDecision{Action: PolicyAllow, Reason: "resume allowed"}, nil
	})
	loop, err := NewTurnLoop(runner,
		WithTurnPolicy(policy),
		WithTurnPolicyMetadata(map[string]any{"scope": "resume"}),
	)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}))); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "resume input",
		WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-2", ThreadID: "thread-1"}),
		WithResume(ResumeRequest{
			InterruptID: "interrupt-1",
			Payload: map[string]any{
				"answer": "approved",
			},
		}),
	))
	if err != nil {
		t.Fatalf("resume Run() error = %v", err)
	}
	want := []EventType{
		EventPolicyRequested,
		EventPolicyDecided,
		EventTurnStarted,
		EventTurnInputMerged,
		EventTurnResumed,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if got := turnEventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[0].PolicyRequest == nil || events[0].PolicyRequest.Boundary != PolicyBoundaryTurn {
		t.Fatalf("policy requested event = %+v, want turn policy request", events[0])
	}
	if events[1].PolicyDecision == nil || events[1].PolicyDecision.Action != PolicyAllow {
		t.Fatalf("policy decided event = %+v, want allow", events[1])
	}
	if gotRequest.Metadata["scope"] != "resume" {
		t.Fatalf("policy metadata = %+v, want scope", gotRequest.Metadata)
	}
	if got := runnable.calls.Load(); got != 2 {
		t.Fatalf("runner calls = %d, want initial interrupt plus resume", got)
	}
}

func TestTurnLoopRunStopsResumeWhenPolicyDenies(t *testing.T) {
	runnable := &schemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{Action: PolicyDeny, Reason: "resume blocked"}, nil
	})
	loop, err := NewTurnLoop(runner, WithTurnPolicy(policy))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question")); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "resume input", WithResume(ResumeRequest{
		InterruptID: "interrupt-1",
		Payload: map[string]any{
			"answer": "approved",
		},
	})))
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("resume Run() error = %v, want ErrPolicyDenied", err)
	}
	want := []EventType{EventPolicyRequested, EventPolicyDecided, EventTurnFailed}
	if got := turnEventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if got := runnable.calls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want only initial interrupted run", got)
	}
}

func TestTurnLoopRunAuthorizesQueuedResumeWithPolicy(t *testing.T) {
	runnable := &schemaInterruptThenRecordRunnable{}
	runner, err := NewRunner(runnable)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		input, ok := req.Input.(TurnLoopPolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want TurnLoopPolicyInput", req.Input)
		}
		if input.Queued == nil || input.Queued.Kind != TurnInputResume {
			t.Fatalf("policy queued input = %+v, want resume record", input.Queued)
		}
		return PolicyDecision{Action: PolicyDeny, Reason: "queued resume blocked"}, nil
	})
	loop, err := NewTurnLoop(runner, WithTurnPolicy(policy))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := collectTurnEvents(loop.Run(context.Background(), "question")); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first Run() error = %v, want ErrInterrupted", err)
	}
	if _, err := loop.Resume(context.Background(), ResumeRequest{
		InterruptID: "interrupt-1",
		Payload: map[string]any{
			"answer": "approved",
		},
	}); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "current"))
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Run() error = %v, want ErrPolicyDenied", err)
	}
	want := []EventType{EventTurnInputReceived, EventPolicyRequested, EventPolicyDecided, EventTurnFailed}
	if got := turnEventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if got := runnable.calls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want only initial interrupted run", got)
	}
}

func TestTurnLoopPushWithPreemptCancelsActiveRunAndQueuesInput(t *testing.T) {
	blocking := newBlockingTurnRunnable()
	runner, err := NewRunner(blocking)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	firstDone := make(chan collectResult, 1)
	go func() {
		events, err := collectTurnEvents(loop.Run(context.Background(), "first", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-1"})))
		firstDone <- collectResult{events: events, err: err}
	}()
	<-blocking.started

	if _, err := loop.Push(context.Background(), "urgent", WithPreempt(), WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-2"})); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	first := <-firstDone
	if !errors.Is(first.err, context.Canceled) {
		t.Fatalf("first Run() error = %v, want context canceled", first.err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "current", WithTurnRuntimeIDs(RuntimeIDs{RunID: "run-2"})))
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	got := turnEventTypes(events)
	want := []EventType{
		EventTurnPreempted,
		EventTurnInputReceived,
		EventTurnStarted,
		EventTurnInputMerged,
		EventRunStarted,
		EventRunCompleted,
		EventTurnCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestTurnLoopPendingReturnsCopy(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := loop.Push(context.Background(), "queued"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	pending := loop.Pending()
	pending[0].Input = "mutated"

	again := loop.Pending()
	if again[0].Input != "queued" {
		t.Fatalf("Pending() leaked mutable backing storage: %+v", again)
	}
}

func TestTurnLoopCloseCancelsActiveRunAndClosesRunner(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var order []string
	if err := host.Install(ctx, &closePlugin{name: "audit", order: &order}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	blocking := newBlockingTurnRunnable()
	runner, err := NewRunner(blocking, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	done := make(chan collectResult, 1)
	go func() {
		events, err := collectTurnEvents(loop.Run(context.Background(), "input"))
		done <- collectResult{events: events, err: err}
	}()
	<-blocking.started

	if err := loop.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result := <-done
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", result.err)
	}

	expected := []string{"setup:audit", "close:audit"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
}

func TestTurnLoopCloseClosesStore(t *testing.T) {
	ctx := context.Background()
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	store := &closeTrackingTurnLoopStore{}
	loop, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, store))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	if err := loop.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !store.Closed() {
		t.Fatal("store.Closed() = false, want true")
	}
}

func TestTurnLoopCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	store := &closeCountingTurnLoopStore{}
	loop, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, store))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}

	if err := loop.Close(ctx); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := loop.Close(ctx); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := store.CloseCount(); got != 1 {
		t.Fatalf("store Close() count = %d, want 1", got)
	}
}

func TestTurnLoopCloseCanRetryAfterContextCancellation(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	store := &contextAwareCloseTurnLoopStore{}
	loop, err := NewTurnLoop(runner, WithTurnLoopStore(context.Background(), store))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := loop.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Close() error = %v, want %v", err, context.Canceled)
	}
	if err := loop.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := store.CloseCount(); got != 2 {
		t.Fatalf("store Close() count = %d, want 2", got)
	}
}

func TestTurnLoopRunAfterCloseFails(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if err := loop.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events, err := collectTurnEvents(loop.Run(context.Background(), "input"))
	if !errors.Is(err, ErrTurnLoopClosed) {
		t.Fatalf("Run() error = %v, want %v", err, ErrTurnLoopClosed)
	}
	got := turnEventTypes(events)
	want := []EventType{EventTurnFailed}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestTurnLoopPushAfterCloseFails(t *testing.T) {
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner)
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if err := loop.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := loop.Push(context.Background(), "queued"); !errors.Is(err, ErrTurnLoopClosed) {
		t.Fatalf("Push() error = %v, want %v", err, ErrTurnLoopClosed)
	}
}

func TestNewTurnLoopRejectsNilRunner(t *testing.T) {
	if _, err := NewTurnLoop(nil); err == nil {
		t.Fatal("NewTurnLoop() error = nil, want nil runner error")
	}
}

type fakeTurnRunnable struct {
	events []Event
	err    error
}

type closeTrackingTurnLoopStore struct {
	mu     sync.Mutex
	closed bool
}

func (s *closeTrackingTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
	return TurnLoopState{}, false, nil
}

func (s *closeTrackingTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
	return nil
}

func (s *closeTrackingTurnLoopStore) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *closeTrackingTurnLoopStore) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type closeCountingTurnLoopStore struct {
	mu     sync.Mutex
	closes int
}

func (s *closeCountingTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
	return TurnLoopState{}, false, nil
}

func (s *closeCountingTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
	return nil
}

func (s *closeCountingTurnLoopStore) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	if s.closes > 1 {
		return errors.New("store closed twice")
	}
	return nil
}

func (s *closeCountingTurnLoopStore) CloseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

type contextAwareCloseTurnLoopStore struct {
	mu     sync.Mutex
	closes int
}

func (s *contextAwareCloseTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
	return TurnLoopState{}, false, nil
}

func (s *contextAwareCloseTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
	return nil
}

func (s *contextAwareCloseTurnLoopStore) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return ctx.Err()
}

func (s *contextAwareCloseTurnLoopStore) CloseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func (f fakeTurnRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for _, event := range f.events {
			if !yield(event, nil) {
				return
			}
		}
		if f.err != nil {
			yield(Event{Type: EventRunFailed, Err: f.err}, f.err)
		}
	}
}

type recordingTurnRunnable struct {
	events     []Event
	lastInput  any
	lastConfig RunConfig
}

func (r *recordingTurnRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		r.lastInput = input
		r.lastConfig = ResolveRunOptions(opts...)
		for _, event := range r.events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

type interruptingTurnRunnable struct{}

func (interruptingTurnRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		err := Interrupt(InterruptRecord{
			ID:     "interrupt-1",
			Type:   InterruptInput,
			Reason: "need user input",
		})
		yield(Event{Type: EventRunInterrupted, Err: err}, err)
	}
}

type interruptThenRecordRunnable struct {
	calls     atomic.Int64
	lastInput any
}

func (r *interruptThenRecordRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if r.calls.Add(1) == 1 {
			err := Interrupt(InterruptRecord{
				ID:     "interrupt-1",
				Type:   InterruptInput,
				Reason: "need user input",
			})
			yield(Event{Type: EventRunInterrupted, Err: err}, err)
			return
		}
		r.lastInput = input
		yield(Event{Type: EventRunCompleted}, nil)
	}
}

type schemaInterruptThenRecordRunnable struct {
	calls     atomic.Int64
	lastInput any
}

func (r *schemaInterruptThenRecordRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if r.calls.Add(1) == 1 {
			record := InterruptRecord{
				ID:     "interrupt-1",
				Type:   InterruptInput,
				Reason: "need user input",
				ResumeSchema: JSONSchema{
					"type":     "object",
					"required": []any{"answer"},
					"properties": map[string]any{
						"answer": map[string]any{"type": "string"},
					},
				},
			}
			err := Interrupt(record)
			yield(Event{
				Type: EventRunInterrupted,
				StepSnapshot: &StepSnapshot{
					Pending: &record,
				},
				Err: err,
			}, err)
			return
		}
		r.lastInput = input
		yield(Event{Type: EventRunCompleted}, nil)
	}
}

type refSchemaInterruptThenRecordRunnable struct {
	calls      atomic.Int64
	lastInput  any
	lastConfig RunConfig
}

func (r *refSchemaInterruptThenRecordRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		r.lastConfig = ResolveRunOptions(opts...)
		if r.calls.Add(1) == 1 {
			record := InterruptRecord{
				ID:     "interrupt-1",
				Type:   InterruptInput,
				Reason: "need user input",
				ResumeSchema: JSONSchema{
					"$ref": "#/$defs/resume",
				},
			}
			err := Interrupt(record)
			yield(Event{
				Type: EventRunInterrupted,
				StepSnapshot: &StepSnapshot{
					Pending: &record,
				},
				Err: err,
			}, err)
			return
		}
		r.lastInput = input
		yield(Event{Type: EventRunCompleted}, nil)
	}
}

type blockingTurnRunnable struct {
	started chan struct{}
	calls   atomic.Int64
}

func newBlockingTurnRunnable() *blockingTurnRunnable {
	return &blockingTurnRunnable{started: make(chan struct{})}
}

func (b *blockingTurnRunnable) Run(ctx context.Context, _ any, _ ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if b.calls.Add(1) > 1 {
			if !yield(Event{Type: EventRunStarted}, nil) {
				return
			}
			yield(Event{Type: EventRunCompleted}, nil)
			return
		}
		if !yield(Event{Type: EventRunStarted}, nil) {
			return
		}
		close(b.started)
		<-ctx.Done()
		yield(Event{Type: EventRunCanceled, Err: ctx.Err()}, ctx.Err())
	}
}

type collectResult struct {
	events []Event
	err    error
}

func collectTurnEvents(seq iter.Seq2[Event, error]) ([]Event, error) {
	var events []Event
	for event, err := range seq {
		events = append(events, event)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}

func turnEventTypes(events []Event) []EventType {
	types := make([]EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}
