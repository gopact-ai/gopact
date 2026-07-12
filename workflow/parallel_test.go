package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

const parallelTestTimeout = 2 * time.Second

type parallelInvokeResult struct {
	output int
	err    error
}

func TestWorkflowRunsReadyActivationsAtConfiguredParallelism(t *testing.T) {
	started := make(chan int, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	compiled := compileParallelSum(t, 2, func(ctx context.Context, input int) (int, error) {
		started <- input
		select {
		case <-release:
			return input, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	})
	done := invokeParallel(compiled, []int{1, 2, 3})
	receiveParallelValue(t, started)
	receiveParallelValue(t, started)
	assertNoParallelValue(t, started)
	releaseAll()
	result := receiveParallelResult(t, done)
	if result.err != nil || result.output != 6 {
		t.Fatalf("Invoke() = %d, %v, want 6", result.output, result.err)
	}
}

func TestWorkflowParallelFailureCancelsSibling(t *testing.T) {
	ready := make(chan int, 2)
	fail := make(chan struct{})
	canceled := make(chan struct{}, 1)
	compiled := compileParallelSum(t, 2, func(ctx context.Context, input int) (int, error) {
		ready <- input
		if input == 1 {
			<-fail
			return 0, errors.New("branch failed")
		}
		<-ctx.Done()
		canceled <- struct{}{}
		return 0, ctx.Err()
	})
	done := invokeParallel(compiled, []int{1, 2})
	receiveParallelValue(t, ready)
	receiveParallelValue(t, ready)
	close(fail)
	result := receiveParallelResult(t, done)
	if result.err == nil || !strings.Contains(result.err.Error(), "branch failed") {
		t.Fatalf("Invoke() error = %v, want branch failure", result.err)
	}
	receiveParallelSignal(t, canceled)
}

func TestWorkflowParallelEventsKeepContinuousSequence(t *testing.T) {
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	compiled := compileParallelSum(t, 2, func(ctx context.Context, input int) (int, error) {
		ready <- struct{}{}
		<-release
		if err := Emit(ctx, gopact.Event{Type: "branch.custom"}); err != nil {
			return 0, err
		}
		return input, nil
	})
	var mu sync.Mutex
	var events []gopact.Event
	done := make(chan parallelInvokeResult, 1)
	go func() {
		output, err := compiled.Invoke(context.Background(), []int{1, 2}, gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
			return nil
		}))
		done <- parallelInvokeResult{output: output, err: err}
	}()
	receiveParallelSignal(t, ready)
	receiveParallelSignal(t, ready)
	close(release)
	result := receiveParallelResult(t, done)
	if result.err != nil {
		t.Fatalf("Invoke() error = %v", result.err)
	}
	mu.Lock()
	defer mu.Unlock()
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event[%d].Sequence = %d, want %d", index, event.Sequence, index+1)
		}
	}
}

func TestWorkflowParallelNodeEventsIdentifyEachActivation(t *testing.T) {
	compiled := compileParallelSum(t, 2, func(_ context.Context, input int) (int, error) { return input, nil })
	var payloads []NodeEventPayload
	_, err := compiled.Invoke(context.Background(), []int{1, 2}, gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type != EventNodeCompleted || event.Summary != "branch" {
			return nil
		}
		var payload NodeEventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		payloads = append(payloads, payload)
		return nil
	}))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("node payloads = %+v, want two branch activations", payloads)
	}
	first, second := payloads[0], payloads[1]
	if first.ActivationID == "" || first.ActivationID == second.ActivationID {
		t.Fatalf("activation ids = %q, %q, want distinct", first.ActivationID, second.ActivationID)
	}
	if first.SourceSetID == "" || first.SourceSetID != second.SourceSetID {
		t.Fatalf("source sets = %q, %q, want same non-empty set", first.SourceSetID, second.SourceSetID)
	}
	if first.BranchIndex == second.BranchIndex || first.Attempt != 1 || second.Attempt != 1 {
		t.Fatalf("branch payloads = %+v, %+v, want distinct indices at attempt 1", first, second)
	}
	if first.ActivationPhase != ActivationCompleted || first.BranchPhase != BranchCompleted {
		t.Fatalf("first phases = %q, %q, want completed", first.ActivationPhase, first.BranchPhase)
	}
}

func TestWorkflowEventSinkFailureCancelsRunningSibling(t *testing.T) {
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	canceled := make(chan struct{}, 1)
	sinkErr := errors.New("sink failed")
	failing := func(ctx context.Context, input int) (int, error) {
		ready <- struct{}{}
		<-release
		return 0, Emit(ctx, gopact.Event{Type: "branch.custom"})
	}
	sibling := func(ctx context.Context, input int) (int, error) {
		ready <- struct{}{}
		select {
		case <-ctx.Done():
			canceled <- struct{}{}
			return 0, ctx.Err()
		case <-time.After(parallelTestTimeout / 10):
			return 0, errors.New("sibling context was not canceled")
		}
	}
	compiled := compileIndependentParallelBranches(t, failing, sibling)
	done := make(chan parallelInvokeResult, 1)
	go func() {
		output, err := compiled.Invoke(context.Background(), 1, gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == "branch.custom" {
				return sinkErr
			}
			return nil
		}))
		done <- parallelInvokeResult{output: output, err: err}
	}()
	receiveParallelSignal(t, ready)
	receiveParallelSignal(t, ready)
	close(release)
	result := receiveParallelResult(t, done)
	if !errors.Is(result.err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", result.err)
	}
	receiveParallelSignal(t, canceled)
}

func compileIndependentParallelBranches(t *testing.T, first, second func(context.Context, int) (int, error)) *compiled[int, int] {
	t.Helper()
	wf := New[int, int]("independent-parallel", WithMaxParallelism(2))
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	left := wf.Node("left", func(_ context.Context, input int) (int, error) { return input, nil })
	right := wf.Node("right", func(_ context.Context, input int) (int, error) { return input, nil })
	firstBranch := wf.Node("first", first)
	secondBranch := wf.Node("second", second)
	plan.Route(func(_ context.Context, input int) (Dispatch, error) {
		return plan.Once(left, input).And(plan.Once(right, input)), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, left)
	wf.Edge(plan, right)
	wf.Edge(left, firstBranch)
	wf.Edge(right, secondBranch)
	wf.Exit(firstBranch)
	wf.Exit(secondBranch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func compileParallelSum(t *testing.T, parallelism int, worker func(context.Context, int) (int, error)) *compiled[[]int, int] {
	t.Helper()
	wf := New[[]int, int]("parallel", WithMaxParallelism(parallelism))
	plan := wf.Node("plan", func(_ context.Context, input []int) ([]int, error) { return input, nil })
	branch := wf.Node("branch", worker)
	total := wf.Merge("total", func(_ context.Context, in Inputs) (int, error) {
		values, err := in.All(branch)
		if err != nil {
			return 0, err
		}
		sum := 0
		for _, value := range values {
			sum += value
		}
		return sum, nil
	})
	plan.Route(func(_ context.Context, values []int) (Dispatch, error) { return plan.Each(branch, values...), nil })
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Edge(branch, total)
	wf.Exit(total)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func invokeParallel(compiled *compiled[[]int, int], input []int) <-chan parallelInvokeResult {
	done := make(chan parallelInvokeResult, 1)
	go func() {
		output, err := compiled.Invoke(context.Background(), input)
		done <- parallelInvokeResult{output: output, err: err}
	}()
	return done
}

func receiveParallelValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(parallelTestTimeout):
		t.Fatal("timed out waiting for parallel value")
		return *new(T)
	}
}

func receiveParallelSignal(t *testing.T, values <-chan struct{}) {
	t.Helper()
	receiveParallelValue(t, values)
}

func assertNoParallelValue[T any](t *testing.T, values <-chan T) {
	t.Helper()
	select {
	case value := <-values:
		t.Fatalf("unexpected parallel value: %v", value)
	case <-time.After(20 * time.Millisecond):
	}
}

func receiveParallelResult(t *testing.T, done <-chan parallelInvokeResult) parallelInvokeResult {
	t.Helper()
	return receiveParallelValue(t, done)
}
