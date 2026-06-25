package gopact

import (
	"context"
	"fmt"
)

const (
	// EventMetadataEventSinkError records a non-fatal event sink error on fallback.
	EventMetadataEventSinkError = "event_sink_error"
)

// EventHandler handles one event emission boundary.
type EventHandler func(*EventContext) error

// EventSinkFailurePolicy controls whether sink failures stop event emission.
type EventSinkFailurePolicy string

const (
	EventSinkFailureStrict   EventSinkFailurePolicy = "strict"
	EventSinkFailureFallback EventSinkFailurePolicy = "fallback"
)

type eventSinkConfig struct {
	failurePolicy EventSinkFailurePolicy
}

// EventSinkOption configures EventSinkMiddleware.
type EventSinkOption func(*eventSinkConfig)

// WithEventSinkFailurePolicy sets the sink failure policy.
func WithEventSinkFailurePolicy(policy EventSinkFailurePolicy) EventSinkOption {
	return func(cfg *eventSinkConfig) {
		switch policy {
		case EventSinkFailureStrict, EventSinkFailureFallback:
			cfg.failurePolicy = policy
		}
	}
}

// WithEventSinkStrict makes sink failures stop event emission.
func WithEventSinkStrict() EventSinkOption {
	return WithEventSinkFailurePolicy(EventSinkFailureStrict)
}

// WithEventSinkFallback records sink failures on event metadata and keeps emitting.
func WithEventSinkFallback() EventSinkOption {
	return WithEventSinkFailurePolicy(EventSinkFailureFallback)
}

// EventContext carries event emission state through middleware.
type EventContext struct {
	Context context.Context
	Event   Event
	Err     error

	handlers []EventHandler
	index    int
}

// NewEventContext creates an event middleware context.
func NewEventContext(ctx context.Context, event Event) *EventContext {
	if ctx == nil {
		ctx = context.TODO()
	}
	return &EventContext{
		Context: ctx,
		Event:   event,
		index:   -1,
	}
}

// Next advances to the next event handler in the chain.
func (c *EventContext) Next() error {
	if c == nil {
		return nil
	}
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}
	err := c.handlers[c.index](c)
	if err != nil {
		c.Err = err
	}
	return err
}

// EventSinkMiddleware adapts an EventSubscriber into event middleware.
func EventSinkMiddleware(sink EventSubscriber, opts ...EventSinkOption) EventHandler {
	cfg := eventSinkConfig{failurePolicy: EventSinkFailureStrict}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return func(c *EventContext) error {
		if c == nil {
			return nil
		}
		if sink == nil {
			return c.Next()
		}
		if err := sink(c.Context, c.Event); err != nil {
			if cfg.failurePolicy == EventSinkFailureFallback {
				event := c.Event
				if event.Metadata == nil {
					event.Metadata = make(map[string]any)
				}
				event.Metadata[EventMetadataEventSinkError] = err.Error()
				c.Event = event
				return c.Next()
			}
			return fmt.Errorf("gopact: event sink: %w", err)
		}
		return c.Next()
	}
}

// ComposeEventHandler composes event middleware around a final event handler.
func ComposeEventHandler(final EventHandler, middlewares ...EventHandler) EventHandler {
	return func(c *EventContext) error {
		if c == nil {
			c = NewEventContext(context.TODO(), Event{})
		}
		if final == nil {
			final = func(_ *EventContext) error { return nil }
		}
		handlers := make([]EventHandler, 0, len(middlewares)+1)
		for _, middleware := range middlewares {
			if middleware != nil {
				handlers = append(handlers, middleware)
			}
		}
		handlers = append(handlers, final)
		c.handlers = handlers
		c.index = -1
		return c.Next()
	}
}
