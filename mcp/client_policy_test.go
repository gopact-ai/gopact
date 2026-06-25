package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyClientDenySkipsToolCall(t *testing.T) {
	ctx := context.Background()
	base := &FakeClient{
		ToolResults: map[string]ToolResult{
			"git.status": {Content: []gopact.ContentPart{gopact.TextPart("clean")}},
		},
	}
	var calls int
	client, err := NewPolicyClient(
		base,
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			calls++
			if req.Boundary != gopact.PolicyBoundaryMCP {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryMCP)
			}
			if req.Action != gopact.PolicyActionInvoke {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionInvoke)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindTool || input.Name != "git.status" {
				t.Fatalf("policy input = %+v, want tool git.status", input)
			}
			if string(input.Args) != `{"short":true}` {
				t.Fatalf("args = %s, want short true", input.Args)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "tool blocked"}, nil
		}),
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-1"}),
	)
	if err != nil {
		t.Fatalf("NewPolicyClient() error = %v", err)
	}

	_, err = client.CallTool(ctx, "git.status", json.RawMessage(`{"short":true}`))
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("CallTool() error = %v, want ErrPolicyDenied", err)
	}
	if calls != 1 {
		t.Fatalf("policy calls = %d, want 1", calls)
	}
	if len(base.ToolCalls) != 0 {
		t.Fatalf("tool calls = %+v, want none", base.ToolCalls)
	}
}

func TestPolicyClientAllowsListAndPublishesEvents(t *testing.T) {
	ctx := context.Background()
	base := &FakeClient{
		ToolsValue: []ToolInfo{{Name: "git.status"}},
	}
	var events []gopact.Event
	client, err := NewPolicyClient(
		base,
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionList {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionList)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindTools {
				t.Fatalf("kind = %q, want %q", input.Kind, PolicyKindTools)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
		}),
		WithPolicyEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyClient() error = %v", err)
	}

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "git.status" {
		t.Fatalf("Tools() = %+v, want git.status", tools)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 events", events)
	}
	if events[0].Type != gopact.EventPolicyRequested || events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %q, %q", events[0].Type, events[1].Type)
	}
}

func TestPolicyClientReviewPromptGetReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	base := &FakeClient{
		PromptContents: map[string]PromptContent{
			"git.review": {Name: "git.review", Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "review"}}},
		},
	}
	client, err := NewPolicyClient(base, gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionGet {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionGet)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if input.Kind != PolicyKindPrompt || input.Name != "git.review" {
			t.Fatalf("policy input = %+v, want prompt git.review", input)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review prompt get"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyClient() error = %v", err)
	}

	_, err = client.GetPrompt(ctx, "git.review", map[string]any{"scope": "diff"})
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("GetPrompt() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("GetPrompt() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryMCP) {
		t.Fatalf("RequiredBy = %q, want mcp", interruptErr.Record.RequiredBy)
	}
	if len(base.PromptGets) != 0 {
		t.Fatalf("prompt gets = %+v, want none", base.PromptGets)
	}
}

func TestPolicyClientAuthorizesResourceRead(t *testing.T) {
	ctx := context.Background()
	base := &FakeClient{
		ResourceContents: map[string]ResourceContent{
			"repo://README.md": {URI: "repo://README.md", Text: "hello"},
		},
	}
	var gotInput PolicyInput
	client, err := NewPolicyClient(base, gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionRead {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionRead)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		gotInput = input
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyClient() error = %v", err)
	}

	content, err := client.ReadResource(ctx, "repo://README.md")
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if content.Text != "hello" {
		t.Fatalf("content = %+v, want hello", content)
	}
	if gotInput.Kind != PolicyKindResource || gotInput.URI != "repo://README.md" {
		t.Fatalf("policy input = %+v, want resource repo://README.md", gotInput)
	}
}

func TestNewPolicyClientRequiresDependencies(t *testing.T) {
	if _, err := NewPolicyClient(nil, gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})); !errors.Is(err, ErrClientRequired) {
		t.Fatalf("NewPolicyClient(nil, policy) error = %v, want ErrClientRequired", err)
	}
	if _, err := NewPolicyClient(&FakeClient{}, nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyClient(client, nil) error = %v, want ErrPolicyRequired", err)
	}
}
