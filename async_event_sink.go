package gopact

import (
	"context"
	"errors"
	"sync"
)

const (
	// EventMetadataAsyncEventSinkDropped marks an event that was not queued by an async sink.
	EventMetadataAsyncEventSinkDropped = "async_event_sink_dropped"
)

// AsyncEventSinkOverflowPolicy controls what happens when the async sink queue is full.
type AsyncEventSinkOverflowPolicy string

const (
	AsyncEventSinkOverflowBlock      AsyncEventSinkOverflowPolicy = "block"
	AsyncEventSinkOverflowDropNewest AsyncEventSinkOverflowPolicy = "drop_newest"
)

type asyncEventSinkConfig struct {
	buffer         int
	overflowPolicy AsyncEventSinkOverflowPolicy
}

// AsyncEventSinkOption configures an async event sink.
type AsyncEventSinkOption func(*asyncEventSinkConfig)

// WithAsyncEventSinkBuffer sets the bounded queue size. Zero creates an unbuffered queue.
func WithAsyncEventSinkBuffer(size int) AsyncEventSinkOption {
	return func(cfg *asyncEventSinkConfig) {
		if size >= 0 {
			cfg.buffer = size
		}
	}
}

// WithAsyncEventSinkOverflowPolicy sets the bounded queue overflow behavior.
func WithAsyncEventSinkOverflowPolicy(policy AsyncEventSinkOverflowPolicy) AsyncEventSinkOption {
	return func(cfg *asyncEventSinkConfig) {
		switch policy {
		case AsyncEventSinkOverflowBlock, AsyncEventSinkOverflowDropNewest:
			cfg.overflowPolicy = policy
		}
	}
}

// WithAsyncEventSinkDropNewest drops the current event when the queue is full.
func WithAsyncEventSinkDropNewest() AsyncEventSinkOption {
	return WithAsyncEventSinkOverflowPolicy(AsyncEventSinkOverflowDropNewest)
}

// WithAsyncEventSinkBlock waits for queue capacity when the queue is full.
func WithAsyncEventSinkBlock() AsyncEventSinkOption {
	return WithAsyncEventSinkOverflowPolicy(AsyncEventSinkOverflowBlock)
}

// AsyncEventSink exports events from a bounded background queue.
type AsyncEventSink struct {
	sink           EventSubscriber
	overflowPolicy AsyncEventSinkOverflowPolicy
	queue          chan Event
	closing        chan struct{}
	done           chan struct{}
	closeOnce      sync.Once

	mu       sync.Mutex
	isClosed bool
	errs     []error
}

// NewAsyncEventSink creates an async event sink with a bounded queue.
func NewAsyncEventSink(sink EventSubscriber, opts ...AsyncEventSinkOption) *AsyncEventSink {
	cfg := asyncEventSinkConfig{
		buffer:         64,
		overflowPolicy: AsyncEventSinkOverflowBlock,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	s := &AsyncEventSink{
		sink:           sink,
		overflowPolicy: cfg.overflowPolicy,
		queue:          make(chan Event, cfg.buffer),
		closing:        make(chan struct{}),
		done:           make(chan struct{}),
	}
	if sink == nil {
		close(s.done)
		return s
	}
	go s.run()
	return s
}

// Middleware queues events for asynchronous export before continuing the event chain.
func (s *AsyncEventSink) Middleware() EventHandler {
	return func(c *EventContext) error {
		if c == nil {
			return nil
		}
		if s == nil || s.sink == nil {
			return c.Next()
		}
		dropped, err := s.enqueue(c.Context, c.Event)
		if err != nil {
			return err
		}
		if dropped {
			event := c.Event
			if event.Metadata == nil {
				event.Metadata = make(map[string]any)
			} else {
				metadata := make(map[string]any, len(event.Metadata)+1)
				for key, value := range event.Metadata {
					metadata[key] = value
				}
				event.Metadata = metadata
			}
			event.Metadata[EventMetadataAsyncEventSinkDropped] = true
			c.Event = event
		}
		return c.Next()
	}
}

// Close stops accepting new events, drains the queue, and returns exporter errors.
func (s *AsyncEventSink) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.isClosed = true
		s.mu.Unlock()
		close(s.closing)
	})
	select {
	case <-s.done:
		return s.closeErr()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *AsyncEventSink) enqueue(ctx context.Context, event Event) (bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	isClosed := s.isClosed
	s.mu.Unlock()
	if isClosed {
		return false, nil
	}
	switch s.overflowPolicy {
	case AsyncEventSinkOverflowDropNewest:
		select {
		case <-s.closing:
			return false, nil
		case s.queue <- event:
			return false, nil
		default:
			return true, nil
		}
	default:
		select {
		case <-s.closing:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case s.queue <- event:
			return false, nil
		}
	}
}

func (s *AsyncEventSink) run() {
	defer close(s.done)
	for {
		select {
		case event := <-s.queue:
			s.export(event)
		case <-s.closing:
			s.drain()
			return
		}
	}
}

func (s *AsyncEventSink) drain() {
	for {
		select {
		case event := <-s.queue:
			s.export(event)
		default:
			return
		}
	}
}

func (s *AsyncEventSink) export(event Event) {
	if err := s.sink(context.Background(), event); err != nil {
		s.mu.Lock()
		s.errs = append(s.errs, err)
		s.mu.Unlock()
	}
}

func (s *AsyncEventSink) closeErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.errs...)
}
