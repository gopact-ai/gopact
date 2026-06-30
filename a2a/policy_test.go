package a2a

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyAgentDenySkipsSendAndReturnsPolicyEvidence(t *testing.T) {
	var gotRequest gopact.PolicyRequest
	var sinkEvents []gopact.Event
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		gotRequest = req
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if input.AgentName != "reviewer" || input.Task == nil || input.Task.Input != "diff" {
			t.Fatalf("policy input = %+v, want reviewer task", input)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "a2a blocked"}, nil
	})
	called := false
	agent, err := NewPolicyAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "reviewer"},
			SendFunc: func(context.Context, Task) (Result, error) {
				called = true
				return Result{}, nil
			},
		},
		policy,
		WithPolicyMetadata(map[string]any{"scope": "mesh"}),
		WithPolicyEventSink(func(ctx context.Context, event gopact.Event) error {
			sinkEvents = append(sinkEvents, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyAgent() error = %v", err)
	}

	result, err := agent.Send(context.Background(), Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "run-1"},
		Input: "diff",
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Send() error = %v, want ErrPolicyDenied", err)
	}
	if called {
		t.Fatal("underlying agent should not be called after policy denial")
	}
	if gotRequest.Boundary != gopact.PolicyBoundaryA2A ||
		gotRequest.Action != gopact.PolicyActionSend ||
		gotRequest.IDs.RunID != "run-1" ||
		gotRequest.Metadata["scope"] != "mesh" {
		t.Fatalf("policy request = %+v, want a2a send request", gotRequest)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("Send() events = %+v, want policy evidence", result.Events)
	}
	if len(sinkEvents) != 2 ||
		sinkEvents[0].Type != gopact.EventPolicyRequested ||
		sinkEvents[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("sink events = %+v, want policy evidence", sinkEvents)
	}
}

func TestPolicyAgentReviewInterruptsStream(t *testing.T) {
	var sinkEvents []gopact.Event
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Boundary != gopact.PolicyBoundaryA2A || req.Action != gopact.PolicyActionStream {
			t.Fatalf("policy request = %+v, want a2a stream", req)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review remote stream"}, nil
	})
	called := false
	agent, err := NewPolicyAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "researcher"},
			StreamFunc: func(context.Context, Task) iter.Seq2[TaskEvent, error] {
				called = true
				return func(yield func(TaskEvent, error) bool) {
					yield(TaskEvent{Status: TaskStatusRunning}, nil)
				}
			},
		},
		policy,
		WithPolicyEventSink(func(ctx context.Context, event gopact.Event) error {
			sinkEvents = append(sinkEvents, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyAgent() error = %v", err)
	}

	events, err := collectTaskEvents(agent.Stream(context.Background(), Task{ID: "task-1"}))
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Stream() error = %v, want ErrInterrupted", err)
	}
	if called {
		t.Fatal("underlying stream should not be called before policy review")
	}
	if len(events) != 0 {
		t.Fatalf("Stream() events = %+v, want none before review approval", events)
	}
	if len(sinkEvents) != 2 ||
		sinkEvents[0].Type != gopact.EventPolicyRequested ||
		sinkEvents[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("sink events = %+v, want policy evidence", sinkEvents)
	}
}

func TestPolicyAgentAuthorizesCancel(t *testing.T) {
	wantIDs := gopact.RuntimeIDs{RunID: "ctx-run", AgentID: "policy-agent", TraceID: "trace-1"}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{RunID: "ctx-run", TraceID: "trace-1"})
	var canceled string
	var cancelContextIDs gopact.RuntimeIDs
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if req.Boundary != gopact.PolicyBoundaryA2A ||
			req.Action != gopact.PolicyActionCancel ||
			input.TaskID != "task-1" {
			t.Fatalf("policy request = %+v input = %+v, want a2a cancel", req, input)
		}
		if req.IDs != wantIDs {
			t.Fatalf("policy request IDs = %+v, want %+v", req.IDs, wantIDs)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
	})
	agent, err := NewPolicyAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "planner"},
			CancelFunc: func(ctx context.Context, taskID string) error {
				canceled = taskID
				cancelContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
				return nil
			},
		},
		policy,
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "policy-run", AgentID: "policy-agent"}),
	)
	if err != nil {
		t.Fatalf("NewPolicyAgent() error = %v", err)
	}

	if err := agent.Cancel(ctx, "task-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceled != "task-1" {
		t.Fatalf("canceled task = %q, want task-1", canceled)
	}
	if cancelContextIDs != wantIDs {
		t.Fatalf("cancel context IDs = %+v, want %+v", cancelContextIDs, wantIDs)
	}
}

func TestNewPolicyAgentRequiresDependencies(t *testing.T) {
	policy := gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})
	if _, err := NewPolicyAgent(nil, policy); !errors.Is(err, ErrAgentRequired) {
		t.Fatalf("NewPolicyAgent(nil, policy) error = %v, want ErrAgentRequired", err)
	}
	if _, err := NewPolicyAgent(FakeAgent{CardValue: AgentCard{Name: "planner"}}, nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyAgent(agent, nil) error = %v, want ErrPolicyRequired", err)
	}
}
