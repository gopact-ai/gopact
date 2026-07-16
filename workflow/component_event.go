package workflow

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
)

type componentEventEmitterContextKey struct{}

type componentEventEmitter struct {
	sinks []gopact.EventSink
}

// EmitModelEvent sends an observer-only model event through the current run's
// EventSink values that also implement gopact.ModelEventSink.
func EmitModelEvent(ctx context.Context, event gopact.ModelEvent) error {
	if ctx == nil {
		return nil
	}
	emitter, ok := ctx.Value(componentEventEmitterContextKey{}).(componentEventEmitter)
	if !ok {
		return nil
	}
	return emitter.emitModel(ctx, event)
}

// EmitToolEvent sends an observer-only tool event through the current run's
// EventSink values that also implement gopact.ToolEventSink.
func EmitToolEvent(ctx context.Context, event gopact.ToolEvent) error {
	if ctx == nil {
		return nil
	}
	emitter, ok := ctx.Value(componentEventEmitterContextKey{}).(componentEventEmitter)
	if !ok {
		return nil
	}
	return emitter.emitTool(ctx, event)
}

func (e componentEventEmitter) emitModel(ctx context.Context, event gopact.ModelEvent) error {
	for _, sink := range e.sinks {
		target, ok := sink.(gopact.ModelEventSink)
		if !ok {
			continue
		}
		if err := emitModelEventSink(ctx, target, event); err != nil && gopact.IsStrictEventSink(sink) {
			return fmt.Errorf("workflow: emit model event: %w", err)
		}
	}
	return nil
}

func (e componentEventEmitter) emitTool(ctx context.Context, event gopact.ToolEvent) error {
	for _, sink := range e.sinks {
		target, ok := sink.(gopact.ToolEventSink)
		if !ok {
			continue
		}
		if err := emitToolEventSink(ctx, target, event); err != nil && gopact.IsStrictEventSink(sink) {
			return fmt.Errorf("workflow: emit tool event: %w", err)
		}
	}
	return nil
}

func emitModelEventSink(ctx context.Context, sink gopact.ModelEventSink, event gopact.ModelEvent) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("model event sink panic: %v", recovered)
		}
	}()
	return sink.EmitModelEvent(eventSinkContext{Context: ctx}, event)
}

func emitToolEventSink(ctx context.Context, sink gopact.ToolEventSink, event gopact.ToolEvent) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool event sink panic: %v", recovered)
		}
	}()
	return sink.EmitToolEvent(eventSinkContext{Context: ctx}, event)
}
