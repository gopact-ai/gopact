// Package tui provides a local terminal-oriented transfer and channel adapter.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// Target is the channel target name used by the TUI adapter.
	Target gopact.ChannelTarget = "tui"
)

var (
	ErrWriterRequired     = errors.New("tui: writer is required")
	ErrUnsupportedPayload = errors.New("tui: unsupported payload")
)

// Payload is the terminal-oriented representation of a surface message.
type Payload struct {
	MessageID   string                    `json:"message_id,omitempty"`
	Type        gopact.SurfaceMessageType `json:"type,omitempty"`
	IDs         gopact.RuntimeIDs         `json:"ids,omitempty"`
	Text        string                    `json:"text,omitempty"`
	Actions     []gopact.SurfaceAction    `json:"actions,omitempty"`
	Artifacts   []gopact.ArtifactRef      `json:"artifacts,omitempty"`
	SourceEvent string                    `json:"source_event,omitempty"`
	Metadata    map[string]any            `json:"metadata,omitempty"`
	CreatedAt   time.Time                 `json:"created_at,omitempty"`
}

// Transfer converts gopact surface messages into TUI payloads.
type Transfer struct{}

var _ gopact.Transfer = (*Transfer)(nil)

// NewTransfer creates a TUI transfer.
func NewTransfer() *Transfer {
	return &Transfer{}
}

// Name returns the transfer name.
func (t *Transfer) Name() string {
	return "tui"
}

// Supports reports whether target is the TUI channel target.
func (t *Transfer) Supports(target gopact.ChannelTarget) bool {
	return target == "" || target == Target
}

// Convert converts one surface message into a typed TUI payload.
func (t *Transfer) Convert(ctx context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.ChannelPayload{}, err
	}
	payload := Payload{
		MessageID:   msg.ID,
		Type:        msg.Type,
		IDs:         msg.IDs,
		Text:        renderParts(msg.Parts),
		Actions:     copyActions(msg.Actions),
		Artifacts:   copyArtifacts(msg.Artifacts),
		SourceEvent: msg.SourceEvent,
		Metadata:    copyAnyMap(msg.Metadata),
		CreatedAt:   msg.CreatedAt,
	}
	return gopact.ChannelPayload{
		Target: Target,
		Data:   payload,
		Metadata: map[string]any{
			"surface_id":   msg.ID,
			"surface_type": string(msg.Type),
			"source_event": msg.SourceEvent,
		},
	}, nil
}

// Channel writes TUI payloads to an io.Writer and exposes optional inbound events.
type Channel struct {
	mu     sync.Mutex
	writer io.Writer
	events func(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error]
}

var _ gopact.Channel = (*Channel)(nil)

// ChannelOption configures a TUI channel.
type ChannelOption func(*Channel)

// WithEvents sets the inbound event stream for tests or host-owned terminal input.
func WithEvents(events func(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error]) ChannelOption {
	return func(c *Channel) {
		c.events = events
	}
}

// NewChannel creates a TUI channel that writes rendered payloads to writer.
func NewChannel(writer io.Writer, opts ...ChannelOption) (*Channel, error) {
	if writer == nil {
		return nil, ErrWriterRequired
	}
	channel := &Channel{writer: writer}
	for _, opt := range opts {
		if opt != nil {
			opt(channel)
		}
	}
	return channel, nil
}

// Name returns the channel name.
func (c *Channel) Name() string {
	return "tui"
}

// Send writes one payload to the TUI writer.
func (c *Channel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	text, err := RenderPayload(payload)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.writer, text); err != nil {
		return fmt.Errorf("tui: write payload: %w", err)
	}
	if !strings.HasSuffix(text, "\n") {
		if _, err := io.WriteString(c.writer, "\n"); err != nil {
			return fmt.Errorf("tui: write payload newline: %w", err)
		}
	}
	return nil
}

// Events returns inbound TUI events when an event source was configured.
func (c *Channel) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if err := ctx.Err(); err != nil {
			yield(gopact.ChannelEvent{}, err)
			return
		}
		if c.events == nil {
			return
		}
		for event, err := range c.events(ctx) {
			if event.Channel == "" {
				event.Channel = Target
			}
			if !yield(copyChannelEvent(event), err) {
				return
			}
		}
	}
}

// Close closes the channel. The injected writer lifecycle remains owned by the host.
func (c *Channel) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	return ctx.Err()
}

// RenderPayload renders a channel payload into terminal text.
func RenderPayload(payload gopact.ChannelPayload) (string, error) {
	switch data := payload.Data.(type) {
	case Payload:
		return renderPayload(data), nil
	case *Payload:
		if data == nil {
			return "", ErrUnsupportedPayload
		}
		return renderPayload(*data), nil
	case string:
		return data, nil
	case []byte:
		return string(data), nil
	default:
		return "", fmt.Errorf("%w: %T", ErrUnsupportedPayload, payload.Data)
	}
}

func renderPayload(payload Payload) string {
	lines := make([]string, 0, 3)
	if strings.TrimSpace(payload.Text) != "" {
		lines = append(lines, payload.Text)
	}
	if len(payload.Actions) > 0 {
		labels := make([]string, 0, len(payload.Actions))
		for _, action := range payload.Actions {
			label := action.Label
			if label == "" {
				label = string(action.Type)
			}
			if label != "" {
				labels = append(labels, label)
			}
		}
		if len(labels) > 0 {
			lines = append(lines, "actions: "+strings.Join(labels, ", "))
		}
	}
	if len(payload.Artifacts) > 0 {
		labels := make([]string, 0, len(payload.Artifacts))
		for _, artifact := range payload.Artifacts {
			label := artifact.Name
			if label == "" {
				label = artifact.ID
			}
			if artifact.URI != "" {
				label = fmt.Sprintf("%s (%s)", label, artifact.URI)
			}
			if label != "" {
				labels = append(labels, label)
			}
		}
		if len(labels) > 0 {
			lines = append(lines, "artifacts: "+strings.Join(labels, ", "))
		}
	}
	return strings.Join(lines, "\n")
}

func renderParts(parts []gopact.SurfacePart) string {
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		text := part.Text
		if text == "" {
			text = part.Name
		}
		if text == "" {
			text = part.URI
		}
		if strings.TrimSpace(text) != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func copyActions(in []gopact.SurfaceAction) []gopact.SurfaceAction {
	out := append([]gopact.SurfaceAction(nil), in...)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}

func copyArtifacts(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	out := append([]gopact.ArtifactRef(nil), in...)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
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
