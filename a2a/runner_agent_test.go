package a2a

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRunnableAgentSendRunsLocalRunnableAndReturnsResult(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	var gotInput any
	var gotIDs gopact.RuntimeIDs
	runnable := runnableFunc(func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		cfg := gopact.ResolveRunOptions(opts...)
		gotInput = input
		gotIDs = cfg.IDs
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted, IDs: cfg.IDs}, nil)
			yield(gopact.Event{
				Type:    gopact.EventModelMessage,
				IDs:     cfg.IDs,
				Message: &gopact.Message{Role: gopact.RoleAssistant, Content: "planned"},
			}, nil)
			yield(gopact.Event{
				Type:      gopact.EventToolResult,
				IDs:       cfg.IDs,
				Artifacts: []gopact.ArtifactRef{artifact},
				Result:    &gopact.ToolResult{Content: "artifact ready", Artifacts: []gopact.ArtifactRef{artifact}},
			}, nil)
			yield(gopact.Event{Type: gopact.EventRunCompleted, IDs: cfg.IDs}, nil)
		}
	})
	agent, err := NewRunnableAgent(AgentCard{Name: "planner", Description: "plans tasks"}, runnable)
	if err != nil {
		t.Fatalf("NewRunnableAgent() error = %v", err)
	}

	result, err := agent.Send(ctx, Task{
		ID: "task-1",
		IDs: gopact.RuntimeIDs{
			RunID:    "run-1",
			ThreadID: "thread-1",
			CallID:   "call-1",
			UserID:   "user-1",
		},
		Input: "write tests",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	message, ok := gotInput.(gopact.Message)
	if !ok || message.Role != gopact.RoleUser || message.Text() != "write tests" {
		t.Fatalf("runnable input = %#v, want user message", gotInput)
	}
	if gotIDs.RunID != "run-1" ||
		gotIDs.ThreadID != "thread-1" ||
		gotIDs.CallID != "call-1" ||
		gotIDs.UserID != "user-1" {
		t.Fatalf("run ids = %+v, want task ids", gotIDs)
	}
	if result.TaskID != "task-1" || result.Output != "planned" {
		t.Fatalf("Send() result = %+v, want planned task result", result)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].ID != artifact.ID {
		t.Fatalf("result artifacts = %+v, want deduped artifact", result.Artifacts)
	}
	if result.Metadata["agent_name"] != "planner" || result.Metadata["child_event_count"] != 4 {
		t.Fatalf("result metadata = %+v, want runnable agent metadata", result.Metadata)
	}
}

func TestRunnableAgentSendPropagatesRuntimeIDsThroughContext(t *testing.T) {
	want := gopact.RuntimeIDs{RunID: "task-run", ThreadID: "thread-1", CallID: "call-1", TraceID: "trace-1"}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{
		RunID:   "ctx-run",
		TraceID: "trace-1",
	})
	var mapperIDs, mapperTaskIDs, runnableContextIDs, runnableOptionIDs, resultIDs, resultTaskIDs gopact.RuntimeIDs
	runnable := runnableFunc(func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		runnableContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
		runnableOptionIDs = gopact.ResolveRunOptions(opts...).IDs
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted}, nil)
			yield(gopact.Event{Type: gopact.EventRunCompleted}, nil)
		}
	})
	agent, err := NewRunnableAgent(AgentCard{Name: "planner"}, runnable,
		WithRunnableInputMapper(func(ctx context.Context, task Task) (any, error) {
			mapperIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			mapperTaskIDs = task.IDs
			return task.Input, nil
		}),
		WithRunnableResultMapper(func(ctx context.Context, task Task, events []gopact.Event) (Result, error) {
			resultIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			resultTaskIDs = task.IDs
			return Result{TaskID: task.ID}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewRunnableAgent() error = %v", err)
	}

	_, err = agent.Send(ctx, Task{
		ID:       "task-1",
		IDs:      gopact.RuntimeIDs{RunID: "task-run", ThreadID: "thread-1", CallID: "call-1"},
		Input:    "write tests",
		Metadata: map[string]any{"kind": "unit"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	for name, got := range map[string]gopact.RuntimeIDs{
		"mapper context":   mapperIDs,
		"mapper task":      mapperTaskIDs,
		"runnable context": runnableContextIDs,
		"runnable options": runnableOptionIDs,
		"result context":   resultIDs,
		"result task":      resultTaskIDs,
	} {
		if got != want {
			t.Fatalf("%s IDs = %+v, want %+v", name, got, want)
		}
	}
}

func TestRunnableAgentStreamProjectsRuntimeEvents(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	runnable := runnableFunc(func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		cfg := gopact.ResolveRunOptions(opts...)
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted, IDs: cfg.IDs}, nil)
			yield(gopact.Event{
				Type:    gopact.EventModelMessage,
				IDs:     cfg.IDs,
				Message: &gopact.Message{Role: gopact.RoleAssistant, Content: "outline ready"},
			}, nil)
			yield(gopact.Event{
				Type:      gopact.EventToolResult,
				IDs:       cfg.IDs,
				Artifacts: []gopact.ArtifactRef{artifact},
				Result:    &gopact.ToolResult{Content: "artifact ready", Artifacts: []gopact.ArtifactRef{artifact}},
			}, nil)
			yield(gopact.Event{Type: gopact.EventRunCompleted, IDs: cfg.IDs}, nil)
		}
	})
	agent, err := NewRunnableAgent(AgentCard{Name: "planner"}, runnable)
	if err != nil {
		t.Fatalf("NewRunnableAgent() error = %v", err)
	}

	events, err := collectTaskEvents(agent.Stream(ctx, Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"},
		Input: "write tests",
	}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("Stream() events = %+v, want message/artifact/completed", events)
	}
	if events[0].Message != "outline ready" || events[0].Status != "" {
		t.Fatalf("message event = %+v, want streamed message", events[0])
	}
	if len(events[1].Artifacts) != 1 || events[1].Artifacts[0].ID != artifact.ID || events[1].Status != "" {
		t.Fatalf("artifact event = %+v, want streamed artifact update", events[1])
	}
	if events[2].Status != TaskStatusCompleted ||
		events[2].Result == nil ||
		events[2].Result.Output != "outline ready" ||
		len(events[2].Result.Artifacts) != 1 {
		t.Fatalf("completed event = %+v, want completed result", events[2])
	}
	for _, event := range events {
		if event.TaskID != "task-1" ||
			event.IDs.RunID != "run-1" ||
			event.IDs.CallID != "call-1" {
			t.Fatalf("stream event ids = %+v task=%q, want task identity", event.IDs, event.TaskID)
		}
	}
}

func TestRunnableAgentStreamPropagatesRuntimeIDsThroughContext(t *testing.T) {
	want := gopact.RuntimeIDs{RunID: "ctx-run", CallID: "call-1", TraceID: "trace-1"}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{
		RunID:   "ctx-run",
		TraceID: "trace-1",
	})
	var mapperIDs, runnableContextIDs, resultIDs gopact.RuntimeIDs
	runnable := runnableFunc(func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		runnableContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted}, nil)
			yield(gopact.Event{Type: gopact.EventRunCompleted}, nil)
		}
	})
	agent, err := NewRunnableAgent(AgentCard{Name: "planner"}, runnable,
		WithRunnableInputMapper(func(ctx context.Context, task Task) (any, error) {
			mapperIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return task.Input, nil
		}),
		WithRunnableResultMapper(func(ctx context.Context, task Task, events []gopact.Event) (Result, error) {
			resultIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return Result{TaskID: task.ID}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewRunnableAgent() error = %v", err)
	}

	events, err := collectTaskEvents(agent.Stream(ctx, Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{CallID: "call-1"},
		Input: "write tests",
	}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Stream() events = 0, want completed event")
	}

	for name, got := range map[string]gopact.RuntimeIDs{
		"mapper context":   mapperIDs,
		"runnable context": runnableContextIDs,
		"result context":   resultIDs,
		"completed event":  events[len(events)-1].IDs,
	} {
		if got != want {
			t.Fatalf("%s IDs = %+v, want %+v", name, got, want)
		}
	}
}

func TestRunnableAgentSendReturnsRunFailure(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("run failed")
	runnable := runnableFunc(func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		cfg := gopact.ResolveRunOptions(opts...)
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted, IDs: cfg.IDs}, nil)
			yield(gopact.Event{Type: gopact.EventRunFailed, IDs: cfg.IDs, Err: wantErr}, wantErr)
		}
	})
	agent, err := NewRunnableAgent(AgentCard{Name: "planner"}, runnable)
	if err != nil {
		t.Fatalf("NewRunnableAgent() error = %v", err)
	}

	result, err := agent.Send(ctx, Task{ID: "task-1", Input: "write tests"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Send() error = %v, want %v", err, wantErr)
	}
	if result.TaskID != "task-1" || result.Metadata["child_event_count"] != 2 {
		t.Fatalf("failure result = %+v, want task id and event count", result)
	}
}

type runnableFunc func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error]

func (f runnableFunc) Run(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
	return f(ctx, input, opts...)
}
