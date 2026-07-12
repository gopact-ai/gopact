package gopact

import (
	"context"
	"iter"
)

// Invokable is the shared typed invocation protocol.
type Invokable[I, O any] interface {
	Invoke(context.Context, I, ...RunOption) (O, error)
}

// InvokableFunc adapts a function into an Invokable.
type InvokableFunc[I, O any] func(context.Context, I, ...RunOption) (O, error)

// Invoke implements Invokable.
func (f InvokableFunc[I, O]) Invoke(ctx context.Context, input I, opts ...RunOption) (O, error) {
	return f(ctx, input, opts...)
}

// StreamingInvokable is the optional typed output stream protocol.
type StreamingInvokable[I, C any] interface {
	InvokeStream(context.Context, I, ...RunOption) iter.Seq2[C, error]
}
