package a2a

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestAuthAgentInjectsSendAuth(t *testing.T) {
	var gotRequest AuthRequest
	var gotTask Task
	var gotAuth Auth
	agent, err := NewAuthAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "reviewer"},
			SendFunc: func(ctx context.Context, task Task) (Result, error) {
				gotTask = task
				auth, ok := AuthFromContext(ctx)
				if !ok {
					t.Fatal("Send() context missing auth")
				}
				gotAuth = auth
				return Result{TaskID: task.ID, Output: "reviewed"}, nil
			},
		},
		AuthenticatorFunc(func(ctx context.Context, req AuthRequest) (Auth, error) {
			gotRequest = req
			if req.Task == nil || req.Task.Input != "diff" {
				t.Fatalf("auth request task = %+v, want diff task", req.Task)
			}
			return Auth{
				Scheme:        "bearer",
				Principal:     "svc-reviewer",
				CredentialRef: "secret://a2a/reviewer",
			}, nil
		}),
		WithAuthMetadata(map[string]any{"scope": "mesh"}),
	)
	if err != nil {
		t.Fatalf("NewAuthAgent() error = %v", err)
	}

	result, err := agent.Send(context.Background(), Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "run-1"},
		Input: "diff",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.Output != "reviewed" {
		t.Fatalf("Send() = %+v, want reviewed result", result)
	}
	if gotRequest.AgentName != "reviewer" ||
		gotRequest.Action != gopact.PolicyActionSend ||
		gotRequest.IDs.RunID != "run-1" ||
		gotRequest.Metadata["scope"] != "mesh" {
		t.Fatalf("auth request = %+v, want reviewer send request", gotRequest)
	}
	if gotTask.Auth == nil ||
		gotTask.Auth.Principal != "svc-reviewer" ||
		gotAuth.Principal != "svc-reviewer" {
		t.Fatalf("sent task auth = %+v context auth = %+v, want injected auth", gotTask.Auth, gotAuth)
	}
}

func TestAuthAgentInjectsStreamAuth(t *testing.T) {
	var gotTask Task
	agent, err := NewAuthAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "researcher"},
			StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
				gotTask = task
				return func(yield func(TaskEvent, error) bool) {
					yield(TaskEvent{TaskID: task.ID, Status: TaskStatusCompleted}, nil)
				}
			},
		},
		AuthenticatorFunc(func(context.Context, AuthRequest) (Auth, error) {
			return Auth{Scheme: "bearer", Principal: "svc-researcher"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewAuthAgent() error = %v", err)
	}

	events, err := collectTaskEvents(agent.Stream(context.Background(), Task{ID: "task-1"}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) != 1 || events[0].Status != TaskStatusCompleted {
		t.Fatalf("Stream() events = %+v, want completed event", events)
	}
	if gotTask.Auth == nil || gotTask.Auth.Principal != "svc-researcher" {
		t.Fatalf("stream task auth = %+v, want injected auth", gotTask.Auth)
	}
}

func TestAuthAgentInjectsCancelAuthIntoContext(t *testing.T) {
	wantIDs := gopact.RuntimeIDs{RunID: "ctx-run", AgentID: "auth-agent", TraceID: "trace-1"}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{RunID: "ctx-run", TraceID: "trace-1"})
	var gotRequest AuthRequest
	var gotAuth Auth
	var gotContextIDs gopact.RuntimeIDs
	agent, err := NewAuthAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "planner"},
			CancelFunc: func(ctx context.Context, taskID string) error {
				auth, ok := AuthFromContext(ctx)
				if !ok {
					t.Fatal("Cancel() context missing auth")
				}
				gotAuth = auth
				gotContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
				return nil
			},
		},
		AuthenticatorFunc(func(ctx context.Context, req AuthRequest) (Auth, error) {
			gotRequest = req
			return Auth{Scheme: "bearer", Principal: "svc-planner"}, nil
		}),
		WithAuthIDs(gopact.RuntimeIDs{RunID: "auth-run", AgentID: "auth-agent"}),
	)
	if err != nil {
		t.Fatalf("NewAuthAgent() error = %v", err)
	}

	if err := agent.Cancel(ctx, "task-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if gotRequest.Action != gopact.PolicyActionCancel ||
		gotRequest.TaskID != "task-1" ||
		gotRequest.IDs != wantIDs {
		t.Fatalf("auth request = %+v, want cancel request", gotRequest)
	}
	if gotAuth.Principal != "svc-planner" {
		t.Fatalf("cancel auth = %+v, want injected auth", gotAuth)
	}
	if gotContextIDs != wantIDs {
		t.Fatalf("cancel context IDs = %+v, want %+v", gotContextIDs, wantIDs)
	}
}

func TestAuthAgentPreservesExplicitTaskAuth(t *testing.T) {
	called := false
	agent, err := NewAuthAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "reviewer"},
			SendFunc: func(ctx context.Context, task Task) (Result, error) {
				if task.Auth == nil || task.Auth.Principal != "explicit" {
					t.Fatalf("task auth = %+v, want explicit auth", task.Auth)
				}
				return Result{TaskID: task.ID}, nil
			},
		},
		AuthenticatorFunc(func(context.Context, AuthRequest) (Auth, error) {
			called = true
			return Auth{Principal: "generated"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewAuthAgent() error = %v", err)
	}

	_, err = agent.Send(context.Background(), Task{
		ID:   "task-1",
		Auth: &Auth{Principal: "explicit"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if called {
		t.Fatal("authenticator should not run when task already has auth")
	}
}

func TestNewAuthAgentRequiresDependencies(t *testing.T) {
	auth := AuthenticatorFunc(func(context.Context, AuthRequest) (Auth, error) {
		return Auth{}, nil
	})
	if _, err := NewAuthAgent(nil, auth); !errors.Is(err, ErrAgentRequired) {
		t.Fatalf("NewAuthAgent(nil, auth) error = %v, want ErrAgentRequired", err)
	}
	if _, err := NewAuthAgent(FakeAgent{CardValue: AgentCard{Name: "planner"}}, nil); !errors.Is(err, ErrAuthenticatorRequired) {
		t.Fatalf("NewAuthAgent(agent, nil) error = %v, want ErrAuthenticatorRequired", err)
	}
}
