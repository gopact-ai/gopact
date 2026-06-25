package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestToolRetryMiddlewareRetriesFailedIdempotentAttempt(t *testing.T) {
	attempts := 0
	handler := ComposeToolHandler(func(c *ToolContext) error {
		attempts++
		if attempts == 1 {
			c.Name = "mutated"
			c.Spec.Name = "mutated"
			c.IDs.RunID = "mutated"
			c.Args = json.RawMessage(`{"changed":true}`)
			c.Metadata["scope"] = "mutated"
			c.AddEffect(EffectRecord{
				ID:             "failed-effect",
				Type:           "tool_call",
				Target:         c.Name,
				ReplayPolicy:   EffectReplayIdempotent,
				IdempotencyKey: "retry-key",
			})
			c.Result = ToolResult{Content: "failed content"}
			return errors.New("transient")
		}
		if c.Name != "local.search" || c.Spec.Name != "search" || c.IDs.RunID != "run-1" {
			t.Fatalf("retry identity = name %q spec %q run %q, want original", c.Name, c.Spec.Name, c.IDs.RunID)
		}
		if string(c.Args) != `{"q":"gopact"}` {
			t.Fatalf("retry args = %s, want original", c.Args)
		}
		if c.Metadata["scope"] != "root" {
			t.Fatalf("retry metadata scope = %v, want root", c.Metadata["scope"])
		}
		c.AddEffect(EffectRecord{
			ID:           "success-effect",
			Type:         "tool_call",
			Target:       c.Name,
			ReplayPolicy: EffectReplayRecordOnly,
		})
		c.Result = ToolResult{Content: "ok"}
		return nil
	}, ToolRetryMiddleware(ToolRetryPolicy{MaxAttempts: 2}))

	toolCtx := NewToolContext(context.Background(), ToolContextOptions{
		Name: "local.search",
		Spec: ToolSpec{Name: "search"},
		IDs:  RuntimeIDs{RunID: "run-1"},
		Args: json.RawMessage(`{"q":"gopact"}`),
		Metadata: map[string]any{
			"scope": "root",
		},
	})
	if err := handler(toolCtx); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if toolCtx.Result.Content != "ok" {
		t.Fatalf("result content = %q, want ok", toolCtx.Result.Content)
	}
	if len(toolCtx.Effects) != 1 || toolCtx.Effects[0].ID != "success-effect" {
		t.Fatalf("effects = %+v, want only success effect", toolCtx.Effects)
	}
	if toolCtx.Err != nil {
		t.Fatalf("context error = %v, want nil after successful retry", toolCtx.Err)
	}
}

func TestToolRetryMiddlewareStopsNonIdempotentAttemptByDefault(t *testing.T) {
	wantErr := errors.New("transient")
	attempts := 0
	handler := ComposeToolHandler(func(_ *ToolContext) error {
		attempts++
		return wantErr
	}, ToolRetryMiddleware(ToolRetryPolicy{MaxAttempts: 2}))

	err := handler(NewToolContext(context.Background(), ToolContextOptions{Name: "local.write"}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestToolRetryMiddlewarePropagatesDeciderError(t *testing.T) {
	wantErr := errors.New("decider failed")
	handler := ComposeToolHandler(func(c *ToolContext) error {
		c.AddEffect(EffectRecord{
			ID:             "effect",
			Type:           "tool_call",
			Target:         c.Name,
			ReplayPolicy:   EffectReplayIdempotent,
			IdempotencyKey: "retry-key",
		})
		return errors.New("transient")
	}, ToolRetryMiddleware(ToolRetryDeciderFunc(func(ctx context.Context, request ToolRetryRequest) (ToolRetryDecision, error) {
		return ToolRetryDecision{}, wantErr
	})))

	err := handler(NewToolContext(context.Background(), ToolContextOptions{Name: "local.search"}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
}

func TestToolRetryPolicyImplementsDeciderAndRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (ToolRetryPolicy{MaxAttempts: 2}).DecideToolRetry(ctx, ToolRetryRequest{
		ToolName: "local.search",
		Attempt:  1,
		Err:      errors.New("transient"),
		Effects: []EffectRecord{{
			ReplayPolicy:   EffectReplayIdempotent,
			IdempotencyKey: "retry-key",
		}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DecideToolRetry() error = %v, want context.Canceled", err)
	}
}
