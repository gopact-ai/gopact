package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestReplayHandlerInvokesToolFromRecordedEffectArgs(t *testing.T) {
	ctx := context.Background()
	var seenCallID string
	registry := NewRegistry(WithToolMiddleware(func(c *gopact.ToolContext) error {
		seenCallID = c.IDs.CallID
		return c.Next()
	}))
	err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return gopact.ToolResult{}, err
			}
			return gopact.ToolResult{Content: input.Text}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					Applied:        true,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
					Metadata: map[string]any{
						EffectMetadataToolArgs: json.RawMessage(`{"text":"replayed"}`),
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(registry))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	if seenCallID != "call-1" {
		t.Fatalf("tool call id = %q, want call-1", seenCallID)
	}
	toolResult, ok := results[0].Metadata[EffectReplayMetadataToolResult].(gopact.ToolResult)
	if !ok {
		t.Fatalf("tool result metadata = %#v, want ToolResult", results[0].Metadata[EffectReplayMetadataToolResult])
	}
	if toolResult.Content != "replayed" {
		t.Fatalf("tool result content = %q, want replayed", toolResult.Content)
	}
}

func TestReplayHandlerRejectsMissingRecordedArgs(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(registry))
	if !errors.Is(err, ErrReplayArgsMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayArgsMissing", err)
	}
}

func TestReplayHandlerRejectsNonToolCallEffect(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "effect-1",
					Type:           "artifact_write",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
					Metadata: map[string]any{
						EffectMetadataToolArgs: json.RawMessage(`{}`),
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(registry))
	if !errors.Is(err, ErrReplayEffectType) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayEffectType", err)
	}
}

func TestReplayHandlerCanBeRegisteredInEffectReplayRegistry(t *testing.T) {
	ctx := context.Background()
	toolRegistry := NewRegistry()
	err := toolRegistry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{Content: string(args)}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	replayRegistry := gopact.NewEffectReplayRegistry()
	if err := replayRegistry.Register(EffectTypeToolCall, NewReplayHandler(toolRegistry)); err != nil {
		t.Fatalf("Register(replay handler) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
					Metadata: map[string]any{
						EffectMetadataToolArgs: map[string]any{"text": "via registry"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, replayRegistry)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	toolResult := results[0].Metadata[EffectReplayMetadataToolResult].(gopact.ToolResult)
	if toolResult.Content != `{"text":"via registry"}` {
		t.Fatalf("tool result content = %q, want encoded map args", toolResult.Content)
	}
}
