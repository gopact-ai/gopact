package gopact

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRedactModelRequestRedactsMessagesAndMetadata(t *testing.T) {
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret", "[redacted]"), nil
	})
	request := ModelRequest{
		Messages: []Message{{
			Role:    RoleUser,
			Content: "secret prompt",
			Parts: []ContentPart{
				TextPart("secret part"),
				ReasoningPart("secret reasoning"),
				ImagePart("secret://image", "image/png"),
			},
			ToolCalls: []ToolCall{{Name: "lookup", Arguments: []byte(`{"token":"secret"}`)}},
		}},
		Metadata: map[string]any{
			"tenant": "secret tenant",
			"count":  1,
		},
	}

	got, err := RedactModelRequest(context.Background(), redactor, request)
	if err != nil {
		t.Fatalf("RedactModelRequest() error = %v", err)
	}
	if got.Messages[0].Content != "[redacted] prompt" {
		t.Fatalf("message content = %q", got.Messages[0].Content)
	}
	if got.Messages[0].Parts[0].Text != "[redacted] part" {
		t.Fatalf("text part = %q", got.Messages[0].Parts[0].Text)
	}
	if got.Messages[0].Parts[1].Text != "[redacted] reasoning" {
		t.Fatalf("reasoning part = %q", got.Messages[0].Parts[1].Text)
	}
	if got.Messages[0].Parts[2].URI != "secret://image" {
		t.Fatalf("image uri = %q, want unchanged", got.Messages[0].Parts[2].URI)
	}
	if string(got.Messages[0].ToolCalls[0].Arguments) != `{"token":"[redacted]"}` {
		t.Fatalf("tool call args = %s", got.Messages[0].ToolCalls[0].Arguments)
	}
	if got.Metadata["tenant"] != "[redacted] tenant" || got.Metadata["count"] != 1 {
		t.Fatalf("metadata = %+v", got.Metadata)
	}
	if request.Messages[0].Content != "secret prompt" || request.Metadata["tenant"] != "secret tenant" {
		t.Fatalf("original request mutated: %+v", request)
	}
}

func TestRedactModelResponseRedactsMessageRouteAndMetadata(t *testing.T) {
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret", "[redacted]"), nil
	})
	response := ModelResponse{
		Message:  Message{Role: RoleAssistant, Content: "secret answer"},
		Route:    ModelRoute{Provider: "primary", Model: "fast", Metadata: map[string]any{"note": "secret route"}},
		Events:   []Event{{Type: EventModelMessage}},
		Metadata: map[string]any{"summary": "secret response", "count": 2},
	}

	got, err := RedactModelResponse(context.Background(), redactor, response)
	if err != nil {
		t.Fatalf("RedactModelResponse() error = %v", err)
	}
	if got.Message.Content != "[redacted] answer" {
		t.Fatalf("message content = %q", got.Message.Content)
	}
	if got.Route.Provider != "primary" || got.Route.Model != "fast" {
		t.Fatalf("route identity changed: %+v", got.Route)
	}
	if got.Route.Metadata["note"] != "[redacted] route" {
		t.Fatalf("route metadata = %+v", got.Route.Metadata)
	}
	if got.Metadata["summary"] != "[redacted] response" || got.Metadata["count"] != 2 {
		t.Fatalf("metadata = %+v", got.Metadata)
	}
	if got.Events[0].Type != EventModelMessage {
		t.Fatalf("events changed: %+v", got.Events)
	}
}

func TestModelIORedactionMiddlewareRedactsRequestBeforeAndResponseAfterNext(t *testing.T) {
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret", "[redacted]"), nil
	})
	handler := ComposeModelHandler(func(c *ModelContext) error {
		if c.Request.Messages[0].Content != "[redacted] prompt" {
			t.Fatalf("request content before provider = %q", c.Request.Messages[0].Content)
		}
		c.Response = ModelResponse{
			Message:  Message{Role: RoleAssistant, Content: "secret answer"},
			Metadata: map[string]any{"summary": "secret metadata"},
		}
		return nil
	}, ModelIORedactionMiddleware(redactor))

	modelCtx := NewModelContext(context.Background(), ModelContextOptions{
		Request: ModelRequest{Messages: []Message{{Role: RoleUser, Content: "secret prompt"}}},
	})
	if err := handler(modelCtx); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if modelCtx.Response.Message.Content != "[redacted] answer" {
		t.Fatalf("response content = %q", modelCtx.Response.Message.Content)
	}
	if modelCtx.Response.Metadata["summary"] != "[redacted] metadata" {
		t.Fatalf("response metadata = %+v", modelCtx.Response.Metadata)
	}
}

func TestModelIORedactionMiddlewarePropagatesRequestRedactionError(t *testing.T) {
	wantErr := errors.New("redactor unavailable")
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return "", wantErr
	})
	called := false
	handler := ComposeModelHandler(func(_ *ModelContext) error {
		called = true
		return nil
	}, ModelIORedactionMiddleware(redactor))

	err := handler(NewModelContext(context.Background(), ModelContextOptions{
		Request: ModelRequest{Messages: []Message{{Role: RoleUser, Content: "secret prompt"}}},
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
	if called {
		t.Fatal("final model handler ran after request redaction error")
	}
}
