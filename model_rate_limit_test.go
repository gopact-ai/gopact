package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestModelRateLimitMiddlewareWaitsBeforeNext(t *testing.T) {
	var order []string
	limiter := ModelRateLimiterFunc(func(ctx context.Context, request ModelRequest) error {
		order = append(order, "wait")
		if request.Model != "fast" {
			t.Fatalf("request.Model = %q, want fast", request.Model)
		}
		return nil
	})
	handler := ComposeModelHandler(func(c *ModelContext) error {
		order = append(order, "provider")
		c.Response = ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}
		return nil
	}, ModelRateLimitMiddleware(limiter))

	modelCtx := NewModelContext(context.Background(), ModelContextOptions{
		Request: ModelRequest{Model: "fast"},
	})
	if err := handler(modelCtx); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if len(order) != 2 || order[0] != "wait" || order[1] != "provider" {
		t.Fatalf("order = %v, want wait/provider", order)
	}
	if modelCtx.Response.Message.Content != "ok" {
		t.Fatalf("response = %+v, want ok", modelCtx.Response)
	}
}

func TestModelRateLimitMiddlewareStopsOnLimiterError(t *testing.T) {
	wantErr := errors.New("quota exhausted")
	called := false
	handler := ComposeModelHandler(func(_ *ModelContext) error {
		called = true
		return nil
	}, ModelRateLimitMiddleware(ModelRateLimiterFunc(func(ctx context.Context, request ModelRequest) error {
		return wantErr
	})))

	err := handler(NewModelContext(context.Background(), ModelContextOptions{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
	if called {
		t.Fatal("final model handler ran after rate limiter error")
	}
}

func TestModelRateLimitMiddlewareRequiresLimiter(t *testing.T) {
	handler := ComposeModelHandler(func(_ *ModelContext) error {
		t.Fatal("final model handler should not run without limiter")
		return nil
	}, ModelRateLimitMiddleware(nil))

	err := handler(NewModelContext(context.Background(), ModelContextOptions{}))
	if !errors.Is(err, ErrModelRateLimiterRequired) {
		t.Fatalf("handler error = %v, want ErrModelRateLimiterRequired", err)
	}
}

func TestModelRateLimiterFuncUsesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ModelRateLimiterFunc(func(ctx context.Context, request ModelRequest) error {
		t.Fatal("limiter function should not run after context cancellation")
		return nil
	}).WaitModel(ctx, ModelRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitModel() error = %v, want context.Canceled", err)
	}
}
