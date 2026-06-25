package gopact

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRedactToolResultRedactsContentAndMetadata(t *testing.T) {
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret", "[redacted]"), nil
	})
	result := ToolResult{
		Content: "secret content",
		Metadata: map[string]any{
			"token":  "secret",
			"labels": []string{"public", "secret"},
			"nested": map[string]any{"summary": "secret nested"},
			"count":  3,
		},
		Artifacts: []ArtifactRef{{ID: "artifact-1", Scope: ArtifactScopeRun}},
		Commit: &ToolCommit{
			IdempotencyKey: "safe-key",
			Metadata: map[string]any{
				"receipt": "secret receipt",
			},
		},
		Effects: []EffectRecord{{
			ID:           "effect-1",
			Type:         "tool_call",
			ReplayPolicy: EffectReplayRecordOnly,
		}},
		Events: []Event{{Type: EventToolResult}},
	}

	got, err := RedactToolResult(context.Background(), redactor, result)
	if err != nil {
		t.Fatalf("RedactToolResult() error = %v", err)
	}
	if got.Content != "[redacted] content" {
		t.Fatalf("Content = %q, want redacted content", got.Content)
	}
	if got.Metadata["token"] != "[redacted]" {
		t.Fatalf("token metadata = %+v", got.Metadata["token"])
	}
	labels, ok := got.Metadata["labels"].([]string)
	if !ok || labels[1] != "[redacted]" {
		t.Fatalf("labels metadata = %+v", got.Metadata["labels"])
	}
	nested, ok := got.Metadata["nested"].(map[string]any)
	if !ok || nested["summary"] != "[redacted] nested" {
		t.Fatalf("nested metadata = %+v", got.Metadata["nested"])
	}
	if got.Metadata["count"] != 3 {
		t.Fatalf("count metadata = %+v", got.Metadata["count"])
	}
	if got.Commit == nil || got.Commit.Metadata["receipt"] != "[redacted] receipt" {
		t.Fatalf("commit metadata = %+v, want redacted receipt", got.Commit)
	}
	if got.Artifacts[0].ID != "artifact-1" || got.Effects[0].ID != "effect-1" || got.Events[0].Type != EventToolResult {
		t.Fatalf("non-text fields changed: %+v", got)
	}
}

func TestToolResultRedactionMiddlewareRedactsAfterNext(t *testing.T) {
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret", "[redacted]"), nil
	})
	handler := ComposeToolHandler(func(c *ToolContext) error {
		c.Result = ToolResult{Content: "secret from tool", Metadata: map[string]any{"token": "secret"}}
		return nil
	}, ToolResultRedactionMiddleware(redactor))

	ctx := NewToolContext(context.Background(), ToolContextOptions{})
	if err := handler(ctx); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if ctx.Result.Content != "[redacted] from tool" {
		t.Fatalf("result content = %q, want redacted", ctx.Result.Content)
	}
	if ctx.Result.Metadata["token"] != "[redacted]" {
		t.Fatalf("result metadata = %+v", ctx.Result.Metadata)
	}
}

func TestToolResultRedactionMiddlewarePropagatesRedactorError(t *testing.T) {
	wantErr := errors.New("redactor unavailable")
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return "", wantErr
	})
	handler := ComposeToolHandler(func(c *ToolContext) error {
		c.Result = ToolResult{Content: "secret"}
		return nil
	}, ToolResultRedactionMiddleware(redactor))

	err := handler(NewToolContext(context.Background(), ToolContextOptions{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
}
