package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowSettleAllGatesDownstreamUntilEveryBranchCompletes(t *testing.T) {
	third := make(chan struct{})
	downstream := make(chan int, 3)
	compiled := compileSettledBranches(t, 2, SettleAll(), func(_ context.Context, input int) (int, error) {
		if input == 3 {
			<-third
		}
		return input, nil
	}, func(_ context.Context, input int) (int, error) {
		downstream <- input
		return input, nil
	})
	done := invokeParallel(compiled, []int{1, 2, 3})
	assertNoParallelValue(t, downstream)
	close(third)
	result := receiveParallelResult(t, done)
	if result.err != nil || result.output != 6 {
		t.Fatalf("Invoke() = %d, %v, want 6", result.output, result.err)
	}
}

func TestWorkflowSettleAnySelectsFirstSuccessAndCancelsRest(t *testing.T) {
	ready := make(chan int, 3)
	winner := make(chan struct{})
	canceled := make(chan int, 2)
	compiled := compileSettledExit(t, SettleAny(), func(ctx context.Context, input int) (int, error) {
		ready <- input
		if input == 2 {
			<-winner
			return input * 10, nil
		}
		<-ctx.Done()
		canceled <- input
		return 0, ctx.Err()
	})
	done := invokeParallel(compiled, []int{1, 2, 3})
	receiveParallelValue(t, ready)
	receiveParallelValue(t, ready)
	receiveParallelValue(t, ready)
	close(winner)
	result := receiveParallelResult(t, done)
	if result.err != nil || result.output != 20 {
		t.Fatalf("Invoke() = %d, %v, want 20", result.output, result.err)
	}
	receiveParallelValue(t, canceled)
	receiveParallelValue(t, canceled)
}

func TestWorkflowSettleQuorumSelectsEarliestSuccesses(t *testing.T) {
	ready := make(chan int, 4)
	first := make(chan struct{})
	second := make(chan struct{})
	firstDone := make(chan struct{}, 1)
	canceled := make(chan int, 2)
	compiled := compileSettledExit(t, SettleQuorum(2), func(ctx context.Context, input int) (int, error) {
		ready <- input
		switch input {
		case 2:
			return firstQuorumBranch(input, first, firstDone)
		case 3:
			return secondQuorumBranch(input, second)
		default:
			return canceledQuorumBranch(ctx, input, canceled)
		}
	})
	stream := invokeParallelStream(compiled, []int{1, 2, 3, 4})
	receiveParallelValue(t, ready)
	receiveParallelValue(t, ready)
	receiveParallelValue(t, ready)
	receiveParallelValue(t, ready)
	close(first)
	receiveParallelSignal(t, firstDone)
	close(second)
	result := receiveParallelStream(t, stream)
	if result.err != nil || len(result.values) != 2 || result.values[0] != 2 || result.values[1] != 3 {
		t.Fatalf("InvokeStream() = %v, %v, want [2 3]", result.values, result.err)
	}
	receiveParallelValue(t, canceled)
	receiveParallelValue(t, canceled)
}

func TestWorkflowSettleAnyFailsWhenNoBranchSucceeds(t *testing.T) {
	compiled := compileSettledExit(t, SettleAny(), func(_ context.Context, input int) (int, error) {
		return 0, errors.New("failed branch")
	})
	_, err := compiled.Invoke(context.Background(), []int{1, 2})
	if err == nil || !strings.Contains(err.Error(), "source set") {
		t.Fatalf("Invoke() error = %v, want source-set failure", err)
	}
}

func TestWorkflowSettleAllPreservesBranchFailureOverSiblingCancellation(t *testing.T) {
	firstStarted := make(chan struct{})
	boom := errors.New("branch failed")
	wf := New[int, int]("settle-failure", WithMaxParallelism(2))
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	first := wf.Node("first", func(ctx context.Context, input int) (int, error) {
		close(firstStarted)
		<-ctx.Done()
		return 0, ctx.Err()
	})
	second := wf.Node("second", func(context.Context, int) (int, error) {
		<-firstStarted
		return 0, boom
	})
	plan.Route(func(_ context.Context, input int) (Dispatch, error) {
		return plan.Once(first, input).And(plan.Once(second, input)).WithSettle(SettleAll()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, first)
	wf.Edge(plan, second)
	wf.Exit(first)
	wf.Exit(second)

	_, err := wf.Invoke(context.Background(), 1)
	if !errors.Is(err, boom) {
		t.Fatalf("Invoke() error = %v, want branch failure", err)
	}
}

func TestWorkflowSettleQuorumRejectsUnreachableThreshold(t *testing.T) {
	compiled := compileSettledExit(t, SettleQuorum(3), func(_ context.Context, input int) (int, error) { return input, nil })
	_, err := compiled.Invoke(context.Background(), []int{1, 2})
	if err == nil || !strings.Contains(err.Error(), "quorum") {
		t.Fatalf("Invoke() error = %v, want quorum error", err)
	}
}

func TestWorkflowResumeRestoresPendingSourceSet(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	runs := &branchRunCounts{values: map[int]int{}}
	wf := New[[]int, int]("settle-resume", WithMaxParallelism(2), WithStore(storeWithCheckpointer(store)))
	plan := wf.Node("plan", func(_ context.Context, input []int) ([]int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) {
		runs.add(input)
		return input, nil
	})
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
	plan.Route(func(_ context.Context, values []int) (Dispatch, error) {
		return plan.Each(branch, values...).WithSettle(SettleAll()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Edge(branch, total)
	wf.Exit(total)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	completed := 0
	_, err = compiled.Invoke(context.Background(), []int{1, 2, 3}, gopact.WithRunID("settle-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type != EventNodeCompleted || event.Summary != "branch" {
			return nil
		}
		completed++
		if completed == 2 {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	if runs.get(1) != 1 || runs.get(2) != 1 || runs.get(3) != 0 {
		t.Fatalf("branch runs before resume = %v, want map[1:1 2:1]", runs.snapshot())
	}
	expireRecordingLease(t, store, "settle-resume")
	output, err := compiled.Invoke(context.Background(), []int{1, 2, 3}, WithResume(ResumeRequest{RunID: "settle-resume"}))
	if err != nil || output != 6 {
		t.Fatalf("resumed Invoke() = %d, %v, want 6", output, err)
	}
	if runs.get(1) != 1 || runs.get(2) != 1 || runs.get(3) != 1 {
		t.Fatalf("branch runs after resume = %v, want one run each", runs.snapshot())
	}
}

func TestWorkflowResumeSettlesSatisfiedAnyBeforeRunningQueuedBranch(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	runs := &branchRunCounts{values: map[int]int{}}
	wf := New[[]int, int]("settle-any-resume", WithMaxParallelism(1), WithStore(storeWithCheckpointer(store)))
	plan := wf.Node("plan", func(_ context.Context, input []int) ([]int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) {
		runs.add(input)
		return input, nil
	})
	plan.Route(func(_ context.Context, values []int) (Dispatch, error) {
		return plan.Each(branch, values...).WithSettle(SettleAny()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), []int{1, 2}, gopact.WithRunID("settle-any-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeCompleted && event.Summary == "branch" {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	if runs.get(1) != 1 || runs.get(2) != 0 {
		t.Fatalf("branch runs before resume = %v, want map[1:1]", runs.snapshot())
	}
	expireRecordingLease(t, store, "settle-any-resume")
	output, err := compiled.Invoke(context.Background(), []int{1, 2}, WithResume(ResumeRequest{RunID: "settle-any-resume"}))
	if err != nil || output != 1 {
		t.Fatalf("resumed Invoke() = %d, %v, want 1", output, err)
	}
	if runs.get(1) != 1 || runs.get(2) != 0 {
		t.Fatalf("branch runs after resume = %v, want queued branch canceled", runs.snapshot())
	}
}

func TestWorkflowResumeContinuesInterruptedSourceSetRelease(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	runs := &branchRunCounts{values: map[int]int{}}
	wf := New[[]int, int]("settle-release-resume", WithMaxParallelism(1), WithStore(storeWithCheckpointer(store)))
	plan := wf.Node("plan", func(_ context.Context, input []int) ([]int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) {
		runs.add(input)
		return input, nil
	})
	plan.Route(func(_ context.Context, values []int) (Dispatch, error) {
		return plan.Each(branch, values...).WithSettle(SettleAny()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), []int{1, 2, 3}, gopact.WithRunID("settle-release-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeCanceled {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	if runs.get(1) != 1 || runs.get(2) != 0 || runs.get(3) != 0 {
		t.Fatalf("branch runs before resume = %v, want map[1:1]", runs.snapshot())
	}
	expireRecordingLease(t, store, "settle-release-resume")
	canceled := 0
	output, err := compiled.Invoke(context.Background(), []int{1, 2, 3}, WithResume(ResumeRequest{RunID: "settle-release-resume"}), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeCanceled {
			canceled++
		}
		return nil
	}))
	if err != nil || output != 1 {
		t.Fatalf("resumed Invoke() = %d, %v, want 1", output, err)
	}
	if runs.get(1) != 1 || runs.get(2) != 0 || runs.get(3) != 0 {
		t.Fatalf("branch runs after resume = %v, want no rerun", runs.snapshot())
	}
	if canceled != 2 {
		t.Fatalf("resumed canceled events = %d, want pending replay plus remaining loser", canceled)
	}
}

func compileSettledExit(t *testing.T, policy SettlePolicy, worker func(context.Context, int) (int, error)) *compiled[[]int, int] {
	t.Helper()
	wf := New[[]int, int]("settled-exit", WithMaxParallelism(4))
	plan := wf.Node("plan", func(_ context.Context, input []int) ([]int, error) { return input, nil })
	branch := wf.Node("branch", worker)
	plan.Route(func(_ context.Context, values []int) (Dispatch, error) {
		return plan.Each(branch, values...).WithSettle(policy), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func compileSettledBranches(t *testing.T, parallelism int, policy SettlePolicy, worker, downstream func(context.Context, int) (int, error)) *compiled[[]int, int] {
	t.Helper()
	wf := New[[]int, int]("settled-branches", WithMaxParallelism(parallelism))
	plan := wf.Node("plan", func(_ context.Context, input []int) ([]int, error) { return input, nil })
	branch := wf.Node("branch", worker)
	next := wf.Node("next", downstream)
	total := wf.Merge("total", func(_ context.Context, in Inputs) (int, error) {
		values, err := in.All(next)
		if err != nil {
			return 0, err
		}
		sum := 0
		for _, value := range values {
			sum += value
		}
		return sum, nil
	})
	plan.Route(func(_ context.Context, values []int) (Dispatch, error) {
		return plan.Each(branch, values...).WithSettle(policy), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Edge(branch, next)
	wf.Edge(next, total)
	wf.Exit(total)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

type parallelStreamResult struct {
	values []int
	err    error
}

func invokeParallelStream(compiled *compiled[[]int, int], input []int) <-chan parallelStreamResult {
	done := make(chan parallelStreamResult, 1)
	go func() {
		var result parallelStreamResult
		for value, err := range compiled.InvokeStream(context.Background(), input) {
			if err != nil {
				result.err = err
				break
			}
			result.values = append(result.values, value)
		}
		done <- result
	}()
	return done
}

func receiveParallelStream(t *testing.T, stream <-chan parallelStreamResult) parallelStreamResult {
	t.Helper()
	return receiveParallelValue(t, stream)
}

func firstQuorumBranch(input int, ready <-chan struct{}, done chan<- struct{}) (int, error) {
	<-ready
	done <- struct{}{}
	return input, nil
}

func secondQuorumBranch(input int, ready <-chan struct{}) (int, error) {
	<-ready
	return input, nil
}

func canceledQuorumBranch(ctx context.Context, input int, canceled chan<- int) (int, error) {
	<-ctx.Done()
	canceled <- input
	return 0, ctx.Err()
}

type branchRunCounts struct {
	mu     sync.Mutex
	values map[int]int
}

func (counts *branchRunCounts) add(input int) {
	counts.mu.Lock()
	defer counts.mu.Unlock()
	counts.values[input]++
}

func (counts *branchRunCounts) get(input int) int {
	counts.mu.Lock()
	defer counts.mu.Unlock()
	return counts.values[input]
}

func (counts *branchRunCounts) snapshot() map[int]int {
	counts.mu.Lock()
	defer counts.mu.Unlock()
	values := make(map[int]int, len(counts.values))
	for key, value := range counts.values {
		values[key] = value
	}
	return values
}

func expireRecordingLease(t *testing.T, store *recordingCheckpointer, runID string) {
	t.Helper()
	record := store.records[runID]
	payload, err := decodeCheckpointPayload[int](record.Payload)
	if err != nil {
		t.Fatalf("decodeCheckpointPayload() error = %v", err)
	}
	meta := payload.meta()
	meta.LeaseExpiresAt = time.Now().Add(-time.Second)
	record.Payload, err = encodeCheckpointPayloadWithMeta(payload.state(), payload.Outputs, payload.NextStep, meta)
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	store.records[runID] = record
}
