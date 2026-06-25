package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyManagerDenySkipsConnect(t *testing.T) {
	ctx := context.Background()
	base := NewManager()
	var calls int
	manager, err := NewPolicyManager(
		base,
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			calls++
			if req.Boundary != gopact.PolicyBoundaryMCP {
				t.Fatalf("boundary = %q, want mcp", req.Boundary)
			}
			if req.Action != gopact.PolicyActionConnect {
				t.Fatalf("action = %q, want connect", req.Action)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("input type = %T, want PolicyInput", req.Input)
			}
			if input.Server != "git" || input.Kind != PolicyKindServer {
				t.Fatalf("policy input = %+v, want git server", input)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "mcp blocked"}, nil
		}),
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-1"}),
	)
	if err != nil {
		t.Fatalf("NewPolicyManager() error = %v", err)
	}

	err = manager.Connect(ctx, FakeServer{NameValue: "git"})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Connect() error = %v, want ErrPolicyDenied", err)
	}
	if calls != 1 {
		t.Fatalf("policy calls = %d, want 1", calls)
	}
	tools, err := base.Tools(ctx)
	if err != nil {
		t.Fatalf("base Tools() error = %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("base tools = %+v, want none", tools)
	}
}

func TestPolicyManagerPublishesEventsAndAllowsList(t *testing.T) {
	ctx := context.Background()
	base := NewManager()
	if err := base.Connect(ctx, FakeServer{
		NameValue:  "git",
		ToolsValue: []ToolInfo{{Name: "status"}},
	}); err != nil {
		t.Fatalf("seed Connect() error = %v", err)
	}
	var events []gopact.Event
	manager, err := NewPolicyManager(
		base,
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Boundary != gopact.PolicyBoundaryMCP {
				t.Fatalf("boundary = %q, want mcp", req.Boundary)
			}
			if req.Action != gopact.PolicyActionList {
				t.Fatalf("action = %q, want list", req.Action)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindTools {
				t.Fatalf("kind = %q, want tools", input.Kind)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
		}),
		WithPolicyEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyManager() error = %v", err)
	}

	tools, err := manager.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if got := toolNames(tools); len(got) != 1 || got[0] != "git.status" {
		t.Fatalf("Tools() names = %v, want [git.status]", got)
	}
	if len(events) != 2 || events[0].Type != gopact.EventPolicyRequested || events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("policy events = %+v", events)
	}
}

func TestPolicyManagerReviewPromptsReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	base := NewManager()
	if err := base.Connect(ctx, FakeServer{NameValue: "git", PromptsValue: []Prompt{{Name: "review"}}}); err != nil {
		t.Fatalf("seed Connect() error = %v", err)
	}
	manager, err := NewPolicyManager(base, gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionList {
			t.Fatalf("action = %q, want list", req.Action)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("input type = %T, want PolicyInput", req.Input)
		}
		if input.Kind != PolicyKindPrompts {
			t.Fatalf("kind = %q, want prompts", input.Kind)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review prompts"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyManager() error = %v", err)
	}

	_, err = manager.Prompts(ctx)
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Prompts() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Prompts() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryMCP) {
		t.Fatalf("RequiredBy = %q, want mcp", interruptErr.Record.RequiredBy)
	}
}

func TestNewPolicyManagerRequiresDependencies(t *testing.T) {
	if _, err := NewPolicyManager(nil, gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})); !errors.Is(err, ErrManagerRequired) {
		t.Fatalf("NewPolicyManager(nil, policy) error = %v, want ErrManagerRequired", err)
	}
	if _, err := NewPolicyManager(NewManager(), nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyManager(manager, nil) error = %v, want ErrPolicyRequired", err)
	}
}
