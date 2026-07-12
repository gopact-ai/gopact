package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowInvokeStreamYieldsCommittedOutputBeforeWorkflowCompletes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	wf := New[int, int]("incremental-stream")
	first := wf.Node("first", func(_ context.Context, input int) (int, error) { return input, nil })
	second := wf.Node("second", func(_ context.Context, input int) (int, error) {
		close(started)
		<-release
		return input + 1, nil
	})
	wf.Entry(first)
	wf.Edge(first, second)
	wf.Exit(first)
	wf.Exit(second)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	values := make(chan int, 2)
	errors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for value, err := range compiled.InvokeStream(context.Background(), 1) {
			if err != nil {
				errors <- err
				return
			}
			values <- value
		}
	}()
	firstOutput := receiveParallelValue(t, values)
	if firstOutput != 1 {
		t.Fatalf("first stream output = %d, want 1", firstOutput)
	}
	receiveParallelSignal(t, started)
	releaseAll()
	secondOutput := receiveParallelValue(t, values)
	if secondOutput != 2 {
		t.Fatalf("second stream output = %d, want 2", secondOutput)
	}
	receiveParallelSignal(t, done)
	select {
	case err := <-errors:
		t.Fatalf("InvokeStream() error = %v", err)
	default:
	}
}

func TestWorkflowInvokeStreamConsumerStopCancelsPendingWorkflow(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	secondRuns := 0
	wf := New[int, int]("stopped-stream", WithCheckpointer(store))
	first := wf.Node("first", func(_ context.Context, input int) (int, error) { return input, nil })
	second := wf.Node("second", func(_ context.Context, input int) (int, error) {
		secondRuns++
		return input + 1, nil
	})
	wf.Entry(first)
	wf.Edge(first, second)
	wf.Exit(first)
	wf.Exit(second)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for value, err := range compiled.InvokeStream(context.Background(), 1, gopact.WithRunID("stopped-stream")) {
		if err != nil || value != 1 {
			t.Fatalf("first stream item = %d, %v, want 1", value, err)
		}
		break
	}
	if secondRuns != 0 {
		t.Fatalf("second node runs = %d, want 0", secondRuns)
	}
	record := store.records["stopped-stream"]
	if record.Status != CheckpointCanceled {
		t.Fatalf("checkpoint status = %q, want %q", record.Status, CheckpointCanceled)
	}
}
