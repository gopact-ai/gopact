package gopact

import (
	"context"
	"errors"
	"fmt"
)

// ErrModelRateLimiterRequired is returned when a model rate-limit middleware has no limiter.
var ErrModelRateLimiterRequired = errors.New("gopact: model rate limiter is required")

// ModelRateLimiter waits until a model request may proceed.
type ModelRateLimiter interface {
	WaitModel(ctx context.Context, request ModelRequest) error
}

// ModelRateLimiterFunc adapts a function into a ModelRateLimiter.
type ModelRateLimiterFunc func(context.Context, ModelRequest) error

// WaitModel calls the wrapped function.
func (f ModelRateLimiterFunc) WaitModel(ctx context.Context, request ModelRequest) error {
	if f == nil {
		return ErrModelRateLimiterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f(ctx, copyModelRequest(request))
}

// ModelRateLimitMiddleware waits on limiter before the model handler chain proceeds.
func ModelRateLimitMiddleware(limiter ModelRateLimiter) ModelHandler {
	return func(c *ModelContext) error {
		if limiter == nil {
			return ErrModelRateLimiterRequired
		}
		if c == nil {
			c = NewModelContext(context.TODO(), ModelContextOptions{})
		}
		if err := limiter.WaitModel(c.Context, c.Request); err != nil {
			return fmt.Errorf("gopact: model rate limit: %w", err)
		}
		return c.Next()
	}
}
