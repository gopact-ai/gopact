package gopact

import (
	"context"
	"encoding/json"
)

// ToolHandler handles one tool invocation boundary.
type ToolHandler func(*ToolContext) error

// ToolContext carries one tool invocation through middleware.
type ToolContext struct {
	Context  context.Context
	Name     string
	Spec     ToolSpec
	IDs      RuntimeIDs
	Args     json.RawMessage
	Result   ToolResult
	Err      error
	Effects  []EffectRecord
	Events   []Event
	Metadata map[string]any

	handlers []ToolHandler
	index    int
}

// ToolContextOptions configures a ToolContext.
type ToolContextOptions struct {
	Name     string
	Spec     ToolSpec
	IDs      RuntimeIDs
	Args     json.RawMessage
	Result   ToolResult
	Effects  []EffectRecord
	Events   []Event
	Metadata map[string]any
}

// NewToolContext creates a tool middleware context.
func NewToolContext(ctx context.Context, opts ToolContextOptions) *ToolContext {
	if ctx == nil {
		ctx = context.TODO()
	}
	return &ToolContext{
		Context:  ctx,
		Name:     opts.Name,
		Spec:     opts.Spec,
		IDs:      opts.IDs,
		Args:     append(json.RawMessage(nil), opts.Args...),
		Result:   opts.Result,
		Effects:  copyEffectRecords(opts.Effects),
		Events:   copyEvents(opts.Events),
		Metadata: copyAnyMap(opts.Metadata),
		index:    -1,
	}
}

// AddEvent appends a runtime event observed while invoking the tool.
func (c *ToolContext) AddEvent(event Event) {
	if c == nil {
		return
	}
	c.Events = append(c.Events, copyEvent(event))
}

// AddEffect appends an external effect observed while invoking the tool.
func (c *ToolContext) AddEffect(effect EffectRecord) {
	if c == nil {
		return
	}
	c.Effects = append(c.Effects, copyEffectRecord(effect))
}

// Next advances to the next tool handler in the chain.
func (c *ToolContext) Next() error {
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

// ComposeToolHandler composes tool middleware around a final handler.
func ComposeToolHandler(final ToolHandler, middlewares ...ToolHandler) ToolHandler {
	return func(c *ToolContext) error {
		if c == nil {
			c = NewToolContext(context.TODO(), ToolContextOptions{})
		}
		if final == nil {
			final = func(_ *ToolContext) error { return nil }
		}
		handlers := make([]ToolHandler, 0, len(middlewares)+1)
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
