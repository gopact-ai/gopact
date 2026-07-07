package gopact

import "context"

// Runnable is the typed result-first contract for user-facing workflow components.
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, input I, opts ...RunOption) (O, error)
}

// RunnableFunc adapts a function into a typed Runnable.
type RunnableFunc[I, O any] func(context.Context, I, ...RunOption) (O, error)

// Invoke calls f.
func (f RunnableFunc[I, O]) Invoke(ctx context.Context, input I, opts ...RunOption) (O, error) {
	var zero O
	if f == nil {
		return zero, errNilRunnable
	}
	return f(ctx, input, opts...)
}

// StateRunnable is the same-state form of Runnable.
type StateRunnable[S any] interface {
	Runnable[S, S]
}
