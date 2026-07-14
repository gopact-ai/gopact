package workflow

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestIterOptionHidesPrivateConfiguration(t *testing.T) {
	optionType := reflect.TypeFor[IterOption[int]]()
	if optionType.Kind() != reflect.Interface {
		t.Fatalf("IterOption kind = %s, want interface", optionType.Kind())
	}
}

func TestWorkflowEachIterConvertsCallbackPanicsToFailures(t *testing.T) {
	tests := []struct {
		name       string
		factory    func(context.Context) iter.Seq2[int, error]
		checkpoint func() int
		want       string
	}{
		{
			name: "factory panic",
			factory: func(context.Context) iter.Seq2[int, error] {
				panic("factory failed")
			},
			want: "iterator factory panic: factory failed",
		},
		{
			name: "generator panic",
			factory: func(context.Context) iter.Seq2[int, error] {
				return func(func(int, error) bool) { panic("generator failed") }
			},
			want: "iterator panic: generator failed",
		},
		{
			name: "checkpoint panic",
			factory: func(context.Context) iter.Seq2[int, error] {
				return func(yield func(int, error) bool) { yield(1, nil) }
			},
			checkpoint: func() int { panic("checkpoint failed") },
			want:       "iterator checkpoint panic: checkpoint failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, branchRuns := compileFailingIterator(t, tt.factory, tt.checkpoint)
			_, err := compiled.Invoke(context.Background(), 0)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Invoke() error = %v, want %q", err, tt.want)
			}
			if *branchRuns != 0 {
				t.Fatalf("branch runs = %d, want 0", *branchRuns)
			}
		})
	}
}

func TestIterSourceRestoredSequenceConvertsCallbackFailures(t *testing.T) {
	tests := []struct {
		name    string
		restore func(context.Context, any) iter.Seq2[any, error]
		want    string
	}{
		{
			name: "panic",
			restore: func(context.Context, any) iter.Seq2[any, error] {
				panic("restore failed")
			},
			want: "iterator restore panic: restore failed",
		},
		{
			name:    "nil sequence",
			restore: func(context.Context, any) iter.Seq2[any, error] { return nil },
			want:    "iterator restore returned nil sequence",
		},
	}
	source := iterSource{cursor: 1, hasCursor: true}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sequence, err := source.restoredSequence(context.Background(), delivery{iterRestore: tt.restore})
			if sequence != nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("restoredSequence() = %v, %v, want %q", sequence, err, tt.want)
			}
		})
	}
}

func compileFailingIterator(t *testing.T, factory func(context.Context) iter.Seq2[int, error], checkpoint func() int) (*compiled[int, int], *int) {
	t.Helper()
	branchRuns := 0
	wf := New[int, int]("iter-failure", WithMaxParallelism(1))
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) {
		branchRuns++
		return input, nil
	})
	plan.Route(func(_ context.Context, _ int) (Dispatch, error) {
		options := []IterOption[int](nil)
		if checkpoint != nil {
			options = append(options, WithIterReplay(checkpoint, func(context.Context, int) iter.Seq2[int, error] { return nil }))
		}
		return plan.EachIter(branch, factory, options...), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled, &branchRuns
}

func TestWorkflowEachIterResumeRestoresTypedCursorWithoutRerun(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	values := []int{1, 2, 3}
	cursor := 0
	runs := &branchRunCounts{values: map[int]int{}}
	sequence := func(start int) iter.Seq2[int, error] {
		return func(yield func(int, error) bool) {
			for index := start; index < len(values); index++ {
				cursor = index + 1
				if !yield(values[index], nil) {
					return
				}
			}
		}
	}
	wf := New[string, int]("iter-resume", WithMaxParallelism(1), WithStore(storeWithCheckpointer(store)))
	plan := wf.Node("plan", func(_ context.Context, input string) (string, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) {
		runs.add(input)
		return input, nil
	})
	plan.Route(func(_ context.Context, _ string) (Dispatch, error) {
		return plan.EachIter(branch, func(context.Context) iter.Seq2[int, error] { return sequence(0) }, WithIterReplay(
			func() int { return cursor },
			func(_ context.Context, saved int) iter.Seq2[int, error] {
				cursor = saved
				return sequence(saved)
			},
		)), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), "input", gopact.WithRunID("iter-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeCompleted && event.Summary == "branch" {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	if runs.get(1) != 1 || runs.get(2) != 0 || runs.get(3) != 0 {
		t.Fatalf("runs before resume = %v, want only first item", runs.snapshot())
	}
	expireRecordingLease(t, store, "iter-resume")
	var outputs []int
	for output, err := range compiled.InvokeStream(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "iter-resume"})) {
		if err != nil {
			t.Fatalf("resumed InvokeStream() error = %v", err)
		}
		outputs = append(outputs, output)
	}
	if len(outputs) != 3 || outputs[0] != 1 || outputs[1] != 2 || outputs[2] != 3 {
		t.Fatalf("resumed outputs = %v, want [1 2 3]", outputs)
	}
	if runs.get(1) != 1 || runs.get(2) != 1 || runs.get(3) != 1 {
		t.Fatalf("runs after resume = %v, want one run per item", runs.snapshot())
	}
}

func TestWorkflowEachIterSettleAnyClosesOpenSourceAfterFirstSuccess(t *testing.T) {
	pulled := 0
	wf := New[int, int]("iter-any", WithMaxParallelism(1))
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) { return input, nil })
	plan.Route(func(_ context.Context, _ int) (Dispatch, error) {
		return plan.EachIter(branch, func(context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				for value := 1; value <= 100; value++ {
					pulled++
					if !yield(value, nil) {
						return
					}
				}
			}
		}).WithSettle(SettleAny()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	output, err := compiled.Invoke(context.Background(), 0)
	if err != nil || output != 1 {
		t.Fatalf("Invoke() = %d, %v, want first item", output, err)
	}
	if pulled != 1 {
		t.Fatalf("iterator pulled = %d, want 1", pulled)
	}
}

func TestWorkflowEachIterStopsGeneratorWhenRunExitsEarly(t *testing.T) {
	stopped := make(chan struct{})
	wf := New[int, int]("iter-cleanup")
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) { return input, nil })
	plan.Route(func(_ context.Context, _ int) (Dispatch, error) {
		return plan.EachIter(branch, func(context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				defer close(stopped)
				for value := 1; ; value++ {
					if !yield(value, nil) {
						return
					}
				}
			}
		}), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), 0, gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventIterItemPulled {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	receiveParallelSignal(t, stopped)
}

func TestWorkflowEachIterResumeSkipsPulledDeterministicItems(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	runs := &branchRunCounts{values: map[int]int{}}
	wf := New[int, int]("iter-factory-resume", WithMaxParallelism(1), WithStore(storeWithCheckpointer(store)))
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) {
		runs.add(input)
		return input, nil
	})
	plan.Route(func(_ context.Context, _ int) (Dispatch, error) {
		return plan.EachIter(branch, func(context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				for _, value := range []int{1, 2} {
					if !yield(value, nil) {
						return
					}
				}
			}
		}), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), 0, gopact.WithRunID("iter-factory-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeCompleted && event.Summary == "branch" {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	expireRecordingLease(t, store, "iter-factory-resume")
	var outputs []int
	for output, err := range compiled.InvokeStream(context.Background(), 0, WithResume(ResumeRequest{RunID: "iter-factory-resume"})) {
		if err != nil {
			t.Fatalf("resumed InvokeStream() error = %v", err)
		}
		outputs = append(outputs, output)
	}
	if len(outputs) != 2 || outputs[0] != 1 || outputs[1] != 2 {
		t.Fatalf("resumed outputs = %v, want [1 2]", outputs)
	}
	if runs.get(1) != 1 || runs.get(2) != 1 {
		t.Fatalf("branch runs = %v, want one run per item", runs.snapshot())
	}
}
