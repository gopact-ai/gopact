package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicySamplingHandlerDenySkipsNext(t *testing.T) {
	ctx := context.Background()
	var nextCalls int
	handler, err := NewPolicySamplingHandler(
		SamplingHandlerFunc(func(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
			nextCalls++
			return SamplingResponse{Role: gopact.RoleAssistant, Content: []gopact.ContentPart{gopact.TextPart("ignored")}}, nil
		}),
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Boundary != gopact.PolicyBoundaryMCP {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryMCP)
			}
			if req.Action != gopact.PolicyActionGenerate {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionGenerate)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindSampling {
				t.Fatalf("kind = %q, want %q", input.Kind, PolicyKindSampling)
			}
			if input.Sampling == nil {
				t.Fatalf("sampling input = nil, want request")
			}
			if input.Sampling.MaxTokens != 128 || input.Sampling.SystemPrompt != "stay concise" {
				t.Fatalf("sampling input = %+v, want max tokens and system prompt", input.Sampling)
			}
			if len(input.Sampling.Messages) != 1 || input.Sampling.Messages[0].Text() != "hello" {
				t.Fatalf("messages = %+v, want hello", input.Sampling.Messages)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "sampling blocked"}, nil
		}),
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-sampling"}),
	)
	if err != nil {
		t.Fatalf("NewPolicySamplingHandler() error = %v", err)
	}

	_, err = handler.CreateMessage(ctx, SamplingRequest{
		Messages:     []gopact.Message{{Role: gopact.RoleUser, Content: "hello"}},
		SystemPrompt: "stay concise",
		MaxTokens:    128,
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("CreateMessage() error = %v, want ErrPolicyDenied", err)
	}
	if nextCalls != 0 {
		t.Fatalf("next calls = %d, want 0", nextCalls)
	}
}

func TestPolicyElicitationHandlerReviewReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	var nextCalls int
	handler, err := NewPolicyElicitationHandler(
		ElicitationHandlerFunc(func(ctx context.Context, request ElicitationRequest) (ElicitationResponse, error) {
			nextCalls++
			return ElicitationResponse{Action: ElicitationAccept}, nil
		}),
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionReceive {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionReceive)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindElicitation {
				t.Fatalf("kind = %q, want %q", input.Kind, PolicyKindElicitation)
			}
			if input.Elicitation == nil {
				t.Fatalf("elicitation input = nil, want request")
			}
			if input.Elicitation.Mode != ElicitationURL || input.Elicitation.ElicitationID != "elicit-1" {
				t.Fatalf("elicitation input = %+v, want url elicit-1", input.Elicitation)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review elicitation"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyElicitationHandler() error = %v", err)
	}

	_, err = handler.Elicit(ctx, ElicitationRequest{
		Mode:            ElicitationURL,
		Message:         "connect external account",
		URL:             "https://mcp.example.com/connect",
		ElicitationID:   "elicit-1",
		RequestedSchema: gopact.JSONSchema{"type": "object"},
	})
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Elicit() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Elicit() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryMCP) {
		t.Fatalf("RequiredBy = %q, want mcp", interruptErr.Record.RequiredBy)
	}
	if nextCalls != 0 {
		t.Fatalf("next calls = %d, want 0", nextCalls)
	}
}

func TestPolicySamplingHandlerAllowsAndPublishesEvents(t *testing.T) {
	ctx := context.Background()
	var events []gopact.Event
	handler, err := NewPolicySamplingHandler(
		SamplingHandlerFunc(func(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
			return SamplingResponse{
				Role:       gopact.RoleAssistant,
				Content:    []gopact.ContentPart{gopact.TextPart("pong")},
				Model:      "test-model",
				StopReason: "endTurn",
			}, nil
		}),
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
		}),
		WithPolicyEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicySamplingHandler() error = %v", err)
	}

	response, err := handler.CreateMessage(ctx, SamplingRequest{Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "ping"}}})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	if response.Model != "test-model" || response.StopReason != "endTurn" || response.Content[0].Text != "pong" {
		t.Fatalf("response = %+v, want test-model pong", response)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 events", events)
	}
	if events[0].Type != gopact.EventPolicyRequested || events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %q, %q", events[0].Type, events[1].Type)
	}
}
