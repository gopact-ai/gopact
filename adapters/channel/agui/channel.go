package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	ErrClosed             = errors.New("agui: channel closed")
	ErrEventQueueFull     = errors.New("agui: inbound event queue full")
	ErrUnsupportedPayload = errors.New("agui: unsupported payload")
)

const (
	defaultInboundBuffer    = 64
	defaultSubscriberBuffer = 16
)

type eventFrame struct {
	id    string
	event Event
}

// Channel broadcasts AG-UI events to SSE subscribers and accepts inbound
// channel events from host code or the HTTP action handler.
type Channel struct {
	mu               sync.Mutex
	inbound          chan gopact.ChannelEvent
	subscribers      map[chan eventFrame]struct{}
	inboundBuffer    int
	subscriberBuffer int
	closed           bool
}

var _ gopact.Channel = (*Channel)(nil)

// ChannelOption configures an AG-UI channel.
type ChannelOption func(*Channel)

// WithInboundBuffer sets the inbound action queue size.
func WithInboundBuffer(size int) ChannelOption {
	return func(c *Channel) {
		if size > 0 {
			c.inboundBuffer = size
		}
	}
}

// WithSubscriberBuffer sets each SSE subscriber queue size.
func WithSubscriberBuffer(size int) ChannelOption {
	return func(c *Channel) {
		if size > 0 {
			c.subscriberBuffer = size
		}
	}
}

// NewChannel creates an in-memory AG-UI SSE channel hub.
func NewChannel(opts ...ChannelOption) *Channel {
	channel := &Channel{
		inboundBuffer:    defaultInboundBuffer,
		subscriberBuffer: defaultSubscriberBuffer,
		subscribers:      make(map[chan eventFrame]struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(channel)
		}
	}
	channel.inbound = make(chan gopact.ChannelEvent, channel.inboundBuffer)
	return channel
}

// Name returns the channel name.
func (c *Channel) Name() string {
	return "agui"
}

// Send broadcasts one AG-UI payload to active SSE subscribers. Slow
// subscribers may miss frames once their per-subscriber buffer is full.
func (c *Channel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	frames, err := normalizePayload(payload)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	for _, frame := range frames {
		for subscriber := range c.subscribers {
			select {
			case subscriber <- copyFrame(frame):
			default:
			}
		}
	}
	return nil
}

// Receive publishes one inbound channel event into the Events stream.
func (c *Channel) Receive(ctx context.Context, event gopact.ChannelEvent) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	event = normalizeEvent(event)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	select {
	case c.inbound <- copyChannelEvent(event):
		return nil
	default:
		return ErrEventQueueFull
	}
}

// Events returns inbound events submitted through Receive or ActionHandler.
func (c *Channel) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		c.mu.Lock()
		inbound := c.inbound
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return
		}
		for {
			select {
			case <-ctx.Done():
				yield(gopact.ChannelEvent{}, ctx.Err())
				return
			case event, ok := <-inbound:
				if !ok {
					return
				}
				if !yield(copyChannelEvent(event), nil) {
					return
				}
			}
		}
	}
}

// Close closes the channel hub and all active SSE subscriber streams.
func (c *Channel) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.inbound)
	for subscriber := range c.subscribers {
		close(subscriber)
		delete(c.subscribers, subscriber)
	}
	return nil
}

// StreamHandler returns an HTTP handler that streams AG-UI events as SSE.
func (c *Channel) StreamHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		events, unsubscribe, ok := c.subscribe()
		if !ok {
			http.Error(w, ErrClosed.Error(), http.StatusServiceUnavailable)
			return
		}
		defer unsubscribe()

		header := w.Header()
		header.Set("Content-Type", "text/event-stream; charset=utf-8")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		if _, err := io.WriteString(w, ": gopact-agui\n\n"); err != nil {
			return
		}
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case frame, ok := <-events:
				if !ok {
					return
				}
				if err := WriteEvent(w, frame.id, frame.event); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// ActionHandler returns an HTTP handler that accepts ChannelEvent JSON via POST.
func (c *Channel) ActionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer func() {
			_ = r.Body.Close()
		}()
		var event gopact.ChannelEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, fmt.Sprintf("invalid channel event: %v", err), http.StatusBadRequest)
			return
		}
		if err := c.Receive(r.Context(), event); err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusRequestTimeout
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"accepted":true}`)
	})
}

// WriteEvent writes one AG-UI SSE frame to writer.
func WriteEvent(writer io.Writer, id string, event Event) error {
	if id != "" {
		if _, err := fmt.Fprintf(writer, "id: %s\n", cleanSSEField(id)); err != nil {
			return fmt.Errorf("agui: write event id: %w", err)
		}
	}
	if event.Type != "" {
		if _, err := fmt.Fprintf(writer, "event: %s\n", cleanSSEField(string(event.Type))); err != nil {
			return fmt.Errorf("agui: write event type: %w", err)
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("agui: encode event: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if _, err := fmt.Fprintf(writer, "data: %s\n", line); err != nil {
			return fmt.Errorf("agui: write event data: %w", err)
		}
	}
	if _, err := io.WriteString(writer, "\n"); err != nil {
		return fmt.Errorf("agui: write event terminator: %w", err)
	}
	return nil
}

func (c *Channel) subscribe() (<-chan eventFrame, func(), bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, false
	}
	events := make(chan eventFrame, c.subscriberBuffer)
	c.subscribers[events] = struct{}{}
	unsubscribe := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.subscribers, events)
	}
	return events, unsubscribe, true
}

func normalizePayload(payload gopact.ChannelPayload) ([]eventFrame, error) {
	switch data := payload.Data.(type) {
	case Payload:
		return framesFromPayload(data), nil
	case *Payload:
		if data == nil {
			return nil, ErrUnsupportedPayload
		}
		return framesFromPayload(*data), nil
	case Event:
		return []eventFrame{{event: copyEvent(data)}}, nil
	case *Event:
		if data == nil {
			return nil, ErrUnsupportedPayload
		}
		return []eventFrame{{event: copyEvent(*data)}}, nil
	case []Event:
		frames := make([]eventFrame, 0, len(data))
		for _, event := range data {
			frames = append(frames, eventFrame{event: copyEvent(event)})
		}
		return frames, nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedPayload, payload.Data)
	}
}

func framesFromPayload(payload Payload) []eventFrame {
	frames := make([]eventFrame, 0, len(payload.Events))
	for _, event := range payload.Events {
		frames = append(frames, eventFrame{
			id:    payload.SurfaceID,
			event: copyEvent(event),
		})
	}
	return frames
}

func normalizeEvent(event gopact.ChannelEvent) gopact.ChannelEvent {
	if event.Channel == "" {
		event.Channel = Target
	}
	if event.Type == "" {
		switch {
		case event.Action.Type != "":
			event.Type = gopact.ChannelEventAction
		case event.Text != "":
			event.Type = gopact.ChannelEventMessage
		}
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return copyChannelEvent(event)
}

func cleanSSEField(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}

func copyFrame(in eventFrame) eventFrame {
	return eventFrame{
		id:    in.id,
		event: copyEvent(in.event),
	}
}

func copyEvent(in Event) Event {
	in.RawEvent = copyAnyValue(in.RawEvent)
	in.Value = copyAnyValue(in.Value)
	in.Result = copyAnyValue(in.Result)
	return in
}

func copyChannelEvent(in gopact.ChannelEvent) gopact.ChannelEvent {
	in.Action.Metadata = copyAnyMap(in.Action.Metadata)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyAnyValue(in any) any {
	switch value := in.(type) {
	case map[string]any:
		return copyAnyMap(value)
	default:
		return value
	}
}
