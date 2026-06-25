package gopact

import "context"

// ModelHandler handles one model invocation boundary.
type ModelHandler func(*ModelContext) error

// ModelContext carries one model request through middleware.
type ModelContext struct {
	Context  context.Context
	Request  ModelRequest
	Response ModelResponse
	Route    ModelRoute
	Err      error
	Events   []Event
	Metadata map[string]any

	handlers []ModelHandler
	index    int
}

// ModelContextOptions configures a ModelContext.
type ModelContextOptions struct {
	Request  ModelRequest
	Response ModelResponse
	Route    ModelRoute
	Events   []Event
	Metadata map[string]any
}

// NewModelContext creates a model middleware context.
func NewModelContext(ctx context.Context, opts ModelContextOptions) *ModelContext {
	if ctx == nil {
		ctx = context.TODO()
	}
	return &ModelContext{
		Context:  ctx,
		Request:  opts.Request,
		Response: opts.Response,
		Route:    opts.Route,
		Events:   copyEvents(opts.Events),
		Metadata: copyAnyMap(opts.Metadata),
		index:    -1,
	}
}

// AddEvent appends a runtime event observed while invoking the model.
func (c *ModelContext) AddEvent(event Event) {
	if c == nil {
		return
	}
	c.Events = append(c.Events, copyEvent(event))
}

// Next advances to the next model handler in the chain.
func (c *ModelContext) Next() error {
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

// ComposeModelHandler composes model middleware around a final handler.
func ComposeModelHandler(final ModelHandler, middlewares ...ModelHandler) ModelHandler {
	return func(c *ModelContext) error {
		if c == nil {
			c = NewModelContext(context.TODO(), ModelContextOptions{})
		}
		if final == nil {
			final = func(_ *ModelContext) error { return nil }
		}
		handlers := make([]ModelHandler, 0, len(middlewares)+1)
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
