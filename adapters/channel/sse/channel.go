// Package sse provides an HTTP Server-Sent Events transfer and channel adapter.
package sse

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

const (
	// Target is the channel target name used by the SSE adapter.
	Target gopact.ChannelTarget = "sse"
)

var (
	// ErrClosed is returned when an operation targets a closed SSE channel.
	ErrClosed = errors.New("sse: channel closed")
	// ErrEventQueueFull is returned when inbound SSE events exceed the configured buffer.
	ErrEventQueueFull = errors.New("sse: inbound event queue full")
	// ErrUnsupportedPayload is returned when a payload cannot be encoded as SSE.
	ErrUnsupportedPayload = errors.New("sse: unsupported payload")
)

const (
	defaultInboundBuffer    = 64
	defaultSubscriberBuffer = 16
)

// Payload is the SSE representation of a surface message.
type Payload struct {
	ID       string                 `json:"id,omitempty"`
	Event    string                 `json:"event,omitempty"`
	Message  gopact.SurfaceMessage  `json:"message"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Transfer converts gopact surface messages into SSE payloads.
type Transfer struct{}

var _ gopact.Transfer = (*Transfer)(nil)

// NewTransfer creates an SSE transfer.
func NewTransfer() *Transfer {
	return &Transfer{}
}

// Name returns the transfer name.
func (t *Transfer) Name() string {
	return "sse"
}

// Supports reports whether target is the SSE channel target.
func (t *Transfer) Supports(target gopact.ChannelTarget) bool {
	return target == "" || target == Target
}

// Convert converts one surface message into a typed SSE payload.
func (t *Transfer) Convert(ctx context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.ChannelPayload{}, err
	}
	metadata := map[string]any{
		"surface_id":   msg.ID,
		"surface_type": string(msg.Type),
		"source_event": msg.SourceEvent,
	}
	payload := Payload{
		ID:       msg.ID,
		Event:    string(msg.Type),
		Message:  copySurfaceMessage(msg),
		Metadata: copyAnyMap(metadata),
	}
	return gopact.ChannelPayload{
		Target:   Target,
		Data:     payload,
		Metadata: copyAnyMap(metadata),
	}, nil
}

// Channel broadcasts outbound payloads to SSE subscribers and accepts inbound
// channel events from host code or the HTTP action handler.
type Channel struct {
	mu               sync.Mutex
	inbound          chan gopact.ChannelEvent
	subscribers      map[chan Payload]struct{}
	inboundBuffer    int
	subscriberBuffer int
	closed           bool
}

var _ gopact.Channel = (*Channel)(nil)

// ChannelOption configures an SSE channel.
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

// NewChannel creates an in-memory SSE channel hub.
func NewChannel(opts ...ChannelOption) *Channel {
	channel := &Channel{
		inboundBuffer:    defaultInboundBuffer,
		subscriberBuffer: defaultSubscriberBuffer,
		subscribers:      make(map[chan Payload]struct{}),
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
	return "sse"
}

// Send broadcasts one payload to active SSE subscribers. Slow subscribers may
// miss frames once their per-subscriber buffer is full.
func (c *Channel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := normalizePayload(payload)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	for subscriber := range c.subscribers {
		select {
		case subscriber <- copyPayload(normalized):
		default:
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

// StreamHandler returns an HTTP handler that streams sent payloads as SSE.
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
		if _, err := io.WriteString(w, ": gopact-sse\n\n"); err != nil {
			return
		}
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case payload, ok := <-events:
				if !ok {
					return
				}
				if err := WriteEvent(w, payload); err != nil {
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

// WriteEvent writes one SSE frame to writer.
func WriteEvent(writer io.Writer, payload Payload) error {
	if payload.ID != "" {
		if _, err := fmt.Fprintf(writer, "id: %s\n", cleanSSEField(payload.ID)); err != nil {
			return fmt.Errorf("sse: write event id: %w", err)
		}
	}
	if payload.Event != "" {
		if _, err := fmt.Fprintf(writer, "event: %s\n", cleanSSEField(payload.Event)); err != nil {
			return fmt.Errorf("sse: write event type: %w", err)
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sse: encode event: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if _, err := fmt.Fprintf(writer, "data: %s\n", line); err != nil {
			return fmt.Errorf("sse: write event data: %w", err)
		}
	}
	if _, err := io.WriteString(writer, "\n"); err != nil {
		return fmt.Errorf("sse: write event terminator: %w", err)
	}
	return nil
}

func (c *Channel) subscribe() (<-chan Payload, func(), bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, false
	}
	events := make(chan Payload, c.subscriberBuffer)
	c.subscribers[events] = struct{}{}
	unsubscribe := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.subscribers, events)
	}
	return events, unsubscribe, true
}

func normalizePayload(payload gopact.ChannelPayload) (Payload, error) {
	switch data := payload.Data.(type) {
	case Payload:
		return copyPayload(data), nil
	case *Payload:
		if data == nil {
			return Payload{}, ErrUnsupportedPayload
		}
		return copyPayload(*data), nil
	case gopact.SurfaceMessage:
		return payloadFromSurface(data), nil
	case *gopact.SurfaceMessage:
		if data == nil {
			return Payload{}, ErrUnsupportedPayload
		}
		return payloadFromSurface(*data), nil
	default:
		return Payload{}, fmt.Errorf("%w: %T", ErrUnsupportedPayload, payload.Data)
	}
}

func payloadFromSurface(msg gopact.SurfaceMessage) Payload {
	return Payload{
		ID:      msg.ID,
		Event:   string(msg.Type),
		Message: copySurfaceMessage(msg),
		Metadata: map[string]any{
			"surface_id":   msg.ID,
			"surface_type": string(msg.Type),
			"source_event": msg.SourceEvent,
		},
	}
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

func copyPayload(in Payload) Payload {
	in.Message = copySurfaceMessage(in.Message)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copySurfaceMessage(msg gopact.SurfaceMessage) gopact.SurfaceMessage {
	msg.Target.Metadata = copyAnyMap(msg.Target.Metadata)
	msg.Parts = copySurfaceParts(msg.Parts)
	msg.Actions = copySurfaceActions(msg.Actions)
	msg.Artifacts = copyArtifacts(msg.Artifacts)
	msg.Metadata = copyAnyMap(msg.Metadata)
	return msg
}

func copySurfaceParts(in []gopact.SurfacePart) []gopact.SurfacePart {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.SurfacePart, len(in))
	for i, part := range in {
		out[i] = part
		out[i].Metadata = copyAnyMap(part.Metadata)
	}
	return out
}

func copySurfaceActions(in []gopact.SurfaceAction) []gopact.SurfaceAction {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.SurfaceAction, len(in))
	for i, action := range in {
		out[i] = action
		out[i].Metadata = copyAnyMap(action.Metadata)
	}
	return out
}

func copyArtifacts(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, artifact := range in {
		out[i] = artifact
		out[i].Metadata = copyAnyMap(artifact.Metadata)
	}
	return out
}

func copyChannelEvent(in gopact.ChannelEvent) gopact.ChannelEvent {
	in.Action.Metadata = copyAnyMap(in.Action.Metadata)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
