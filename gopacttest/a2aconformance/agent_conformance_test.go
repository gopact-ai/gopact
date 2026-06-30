package a2aconformance

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
)

func TestRequireAgentConformanceAcceptsFakeStreamingAgent(t *testing.T) {
	agent := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "reviewer"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "reviewed"}, nil
		},
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			return func(yield func(a2a.TaskEvent, error) bool) {
				if err := ctx.Err(); err != nil {
					yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusFailed, Err: err}, err)
					return
				}
				yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusCompleted, Result: &a2a.Result{TaskID: task.ID, Output: "reviewed"}}, nil)
			}
		},
		CancelFunc: func(ctx context.Context, taskID string) error {
			return ctx.Err()
		},
	}

	RequireAgentConformance(t, AgentConformanceHarness{
		Agent:            agent,
		Task:             a2a.Task{ID: "task-1", Input: "diff"},
		RequireStreaming: true,
	})
}

func TestCheckAgentConformanceReportsFailures(t *testing.T) {
	results := CheckAgentConformance(context.Background(), AgentConformanceHarness{
		Agent:            badAgent{},
		Task:             a2a.Task{ID: "task-1", Input: "diff", Metadata: map[string]any{"original": true}},
		RequireStreaming: true,
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
			if !errors.Is(result.Err, ErrAgentConformanceFailed) {
				t.Fatalf("case %q error = %v, want ErrAgentConformanceFailed", result.Case, result.Err)
			}
		}
	}
	for _, want := range []string{
		"has-card-name",
		"send-respects-canceled-context",
		"send-returns-task-id",
		"send-does-not-mutate-task",
		"cancel-respects-canceled-context",
		"implements-streaming",
	} {
		if !failures[want] {
			t.Fatalf("failures = %v, want case %q to fail", failures, want)
		}
	}
}

func TestCheckAgentConformanceReportsNilStream(t *testing.T) {
	results := CheckAgentConformance(context.Background(), AgentConformanceHarness{
		Agent:            nilStreamAgent{},
		Task:             a2a.Task{ID: "task-1", Input: "diff"},
		RequireStreaming: true,
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
		}
	}
	if !failures["streams-events"] {
		t.Fatalf("failures = %v, want streams-events to fail", failures)
	}
	if !failures["stream-respects-canceled-context"] {
		t.Fatalf("failures = %v, want stream-respects-canceled-context to fail", failures)
	}
}

type badAgent struct{}

func (badAgent) Card() a2a.AgentCard {
	return a2a.AgentCard{}
}

func (badAgent) Send(_ context.Context, task a2a.Task) (a2a.Result, error) {
	task.Metadata["mutated"] = true
	return a2a.Result{}, nil
}

func (badAgent) Cancel(context.Context, string) error {
	return errors.New("ignored cancellation")
}

type nilStreamAgent struct{}

func (nilStreamAgent) Card() a2a.AgentCard {
	return a2a.AgentCard{Name: "nil-stream"}
}

func (nilStreamAgent) Send(ctx context.Context, task a2a.Task) (a2a.Result, error) {
	if err := ctx.Err(); err != nil {
		return a2a.Result{}, err
	}
	return a2a.Result{TaskID: task.ID}, nil
}

func (nilStreamAgent) Stream(context.Context, a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
	return nil
}

func (nilStreamAgent) Cancel(ctx context.Context, _ string) error {
	return ctx.Err()
}
