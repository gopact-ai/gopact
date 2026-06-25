package gopact

import "context"

type runtimeIDsContextKey struct{}

// ContextWithRuntimeIDs returns a child context carrying runtime identity for lower-level adapters.
func ContextWithRuntimeIDs(ctx context.Context, ids RuntimeIDs) context.Context {
	if ctx == nil {
		ctx = context.TODO()
	}
	return context.WithValue(ctx, runtimeIDsContextKey{}, ids)
}

// RuntimeIDsFromContext returns runtime identity previously attached to ctx.
func RuntimeIDsFromContext(ctx context.Context) (RuntimeIDs, bool) {
	if ctx == nil {
		return RuntimeIDs{}, false
	}
	ids, ok := ctx.Value(runtimeIDsContextKey{}).(RuntimeIDs)
	return ids, ok
}
