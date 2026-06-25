// Package lark provides a Lark-oriented transfer and channel adapter.
package lark

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// Target is the channel target name used by the Lark adapter.
	Target gopact.ChannelTarget = "lark"

	MsgTypeText        = "text"
	MsgTypeInteractive = "interactive"
)

var (
	ErrSenderRequired     = errors.New("lark: sender is required")
	ErrUnsupportedPayload = errors.New("lark: unsupported payload")
)

// Payload is the Lark custom-bot-style representation of a surface message.
type Payload struct {
	MessageID   string            `json:"message_id,omitempty"`
	MsgType     string            `json:"msg_type,omitempty"`
	Content     TextContent       `json:"content,omitempty"`
	Card        *Card             `json:"card,omitempty"`
	IDs         gopact.RuntimeIDs `json:"ids,omitempty"`
	SourceEvent string            `json:"source_event,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
}

// TextContent is the Lark text message content.
type TextContent struct {
	Text string `json:"text,omitempty"`
}

// Card is the minimal interactive card shape used by the transfer.
type Card struct {
	Config   map[string]any `json:"config,omitempty"`
	Header   *CardHeader    `json:"header,omitempty"`
	Elements []CardElement  `json:"elements,omitempty"`
}

// CardHeader is a Lark interactive card header.
type CardHeader struct {
	Title CardText `json:"title,omitempty"`
}

// CardElement is a minimal Lark card element.
type CardElement struct {
	Tag     string       `json:"tag,omitempty"`
	Text    *CardText    `json:"text,omitempty"`
	Actions []CardAction `json:"actions,omitempty"`
}

// CardText is text inside a Lark card element.
type CardText struct {
	Tag     string `json:"tag,omitempty"`
	Content string `json:"content,omitempty"`
}

// CardAction is a minimal Lark card action button.
type CardAction struct {
	Tag   string      `json:"tag,omitempty"`
	Text  CardText    `json:"text,omitempty"`
	Type  string      `json:"type,omitempty"`
	Value ActionValue `json:"value,omitempty"`
}

// ActionValue is carried by Lark card actions and can be turned back into a ChannelEvent.
type ActionValue struct {
	ActionID    string            `json:"action_id,omitempty"`
	ActionType  string            `json:"action_type,omitempty"`
	InterruptID string            `json:"interrupt_id,omitempty"`
	CallID      string            `json:"call_id,omitempty"`
	IDs         gopact.RuntimeIDs `json:"ids,omitempty"`
	Payload     any               `json:"payload,omitempty"`
	Signature   string            `json:"signature,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

// ActionSigner signs outbound card action values.
type ActionSigner interface {
	SignAction(ctx context.Context, msg gopact.SurfaceMessage, action gopact.SurfaceAction) (string, error)
}

// ActionSignerFunc adapts a function into an ActionSigner.
type ActionSignerFunc func(ctx context.Context, msg gopact.SurfaceMessage, action gopact.SurfaceAction) (string, error)

// SignAction calls f.
func (f ActionSignerFunc) SignAction(ctx context.Context, msg gopact.SurfaceMessage, action gopact.SurfaceAction) (string, error) {
	if f == nil {
		return "", nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f(ctx, copySurfaceMessage(msg), copySurfaceAction(action))
}

type transferConfig struct {
	signer ActionSigner
}

// TransferOption configures a Lark transfer.
type TransferOption func(*transferConfig)

// WithActionSigner signs card action values before they leave the SDK boundary.
func WithActionSigner(signer ActionSigner) TransferOption {
	return func(cfg *transferConfig) {
		cfg.signer = signer
	}
}

// Transfer converts gopact surface messages into Lark payloads.
type Transfer struct {
	signer ActionSigner
}

var _ gopact.Transfer = (*Transfer)(nil)

// NewTransfer creates a Lark transfer.
func NewTransfer(opts ...TransferOption) *Transfer {
	cfg := transferConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Transfer{signer: cfg.signer}
}

// Name returns the transfer name.
func (t *Transfer) Name() string {
	return "lark"
}

// Supports reports whether target is the Lark channel target.
func (t *Transfer) Supports(target gopact.ChannelTarget) bool {
	return target == "" || target == Target
}

// Convert converts one surface message into a typed Lark payload.
func (t *Transfer) Convert(ctx context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.ChannelPayload{}, err
	}
	msg = copySurfaceMessage(msg)
	payload := Payload{
		MessageID:   msg.ID,
		IDs:         msg.IDs,
		SourceEvent: msg.SourceEvent,
		Metadata:    copyAnyMap(msg.Metadata),
		CreatedAt:   msg.CreatedAt,
	}
	text := renderParts(msg.Parts)
	if needsInteractiveCard(msg) {
		card, err := t.card(ctx, msg, text)
		if err != nil {
			return gopact.ChannelPayload{}, err
		}
		payload.MsgType = MsgTypeInteractive
		payload.Card = card
	} else {
		payload.MsgType = MsgTypeText
		payload.Content = TextContent{Text: text}
	}
	metadata := map[string]any{
		"surface_id":   msg.ID,
		"surface_type": string(msg.Type),
		"source_event": msg.SourceEvent,
	}
	return gopact.ChannelPayload{
		Target:   Target,
		Data:     payload,
		Metadata: metadata,
	}, nil
}

func (t *Transfer) card(ctx context.Context, msg gopact.SurfaceMessage, text string) (*Card, error) {
	title := string(msg.Type)
	if title == "" {
		title = "gopact"
	}
	card := &Card{
		Config: map[string]any{"wide_screen_mode": true},
		Header: &CardHeader{
			Title: CardText{Tag: "plain_text", Content: title},
		},
	}
	if strings.TrimSpace(text) != "" {
		card.Elements = append(card.Elements, CardElement{
			Tag:  "div",
			Text: &CardText{Tag: "plain_text", Content: text},
		})
	}
	if len(msg.Artifacts) > 0 {
		card.Elements = append(card.Elements, CardElement{
			Tag:  "div",
			Text: &CardText{Tag: "plain_text", Content: renderArtifacts(msg.Artifacts)},
		})
	}
	if len(msg.Actions) > 0 {
		actions := make([]CardAction, 0, len(msg.Actions))
		for _, action := range msg.Actions {
			value, err := t.actionValue(ctx, msg, action)
			if err != nil {
				return nil, err
			}
			label := action.Label
			if label == "" {
				label = string(action.Type)
			}
			actions = append(actions, CardAction{
				Tag:   "button",
				Text:  CardText{Tag: "plain_text", Content: label},
				Type:  buttonType(action.Type),
				Value: value,
			})
		}
		card.Elements = append(card.Elements, CardElement{
			Tag:     "action",
			Actions: actions,
		})
	}
	return card, nil
}

func (t *Transfer) actionValue(ctx context.Context, msg gopact.SurfaceMessage, action gopact.SurfaceAction) (ActionValue, error) {
	value := ActionValue{
		ActionID:    action.ID,
		ActionType:  string(action.Type),
		InterruptID: action.InterruptID,
		CallID:      action.CallID,
		IDs:         action.IDs,
		Payload:     action.Payload,
		Metadata:    copyAnyMap(action.Metadata),
	}
	if t.signer != nil {
		signature, err := t.signer.SignAction(ctx, msg, action)
		if err != nil {
			return ActionValue{}, fmt.Errorf("lark: sign action: %w", err)
		}
		value.Signature = signature
	}
	return value, nil
}

// SendResult carries platform ids returned by a host-owned Lark sender.
type SendResult struct {
	MessageID string         `json:"message_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Sender sends one Lark payload through host-owned Lark credentials/client code.
type Sender interface {
	Send(ctx context.Context, payload Payload) (SendResult, error)
}

// SenderFunc adapts a function into a Sender.
type SenderFunc func(ctx context.Context, payload Payload) (SendResult, error)

// Send calls f.
func (f SenderFunc) Send(ctx context.Context, payload Payload) (SendResult, error) {
	if f == nil {
		return SendResult{}, ErrSenderRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return SendResult{}, err
	}
	return f(ctx, copyPayload(payload))
}

// Channel sends Lark payloads through an injected sender and exposes optional inbound events.
type Channel struct {
	sender Sender
	events func(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error]
}

var _ gopact.Channel = (*Channel)(nil)

// ChannelOption configures a Lark channel.
type ChannelOption func(*Channel)

// WithEvents sets the inbound event stream for host-owned Lark callback handling.
func WithEvents(events func(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error]) ChannelOption {
	return func(c *Channel) {
		c.events = events
	}
}

// NewChannel creates a Lark channel backed by a host-owned sender.
func NewChannel(sender Sender, opts ...ChannelOption) (*Channel, error) {
	if sender == nil {
		return nil, ErrSenderRequired
	}
	channel := &Channel{sender: sender}
	for _, opt := range opts {
		if opt != nil {
			opt(channel)
		}
	}
	return channel, nil
}

// Name returns the channel name.
func (c *Channel) Name() string {
	return "lark"
}

// Send sends one Lark payload through the injected sender.
func (c *Channel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if c == nil || c.sender == nil {
		return ErrSenderRequired
	}
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
	if _, err := c.sender.Send(ctx, normalized); err != nil {
		return fmt.Errorf("lark: send payload: %w", err)
	}
	return nil
}

// Events returns inbound Lark events when an event source was configured.
func (c *Channel) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if err := ctx.Err(); err != nil {
			yield(gopact.ChannelEvent{}, err)
			return
		}
		if c == nil || c.events == nil {
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

// Close closes the channel. The injected Lark client lifecycle remains owned by the host.
func (c *Channel) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	return ctx.Err()
}

// ChannelEventFromActionValue converts a Lark card action value into a gopact channel event.
func ChannelEventFromActionValue(id string, value ActionValue, createdAt time.Time) gopact.ChannelEvent {
	actionType := gopact.SurfaceActionType(value.ActionType)
	if actionType == "" {
		actionType = gopact.SurfaceActionSubmit
	}
	metadata := copyAnyMap(value.Metadata)
	if value.Signature != "" {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["lark_action_signature"] = value.Signature
	}
	return gopact.ChannelEvent{
		ID:      id,
		Channel: Target,
		Type:    gopact.ChannelEventAction,
		IDs:     value.IDs,
		Action: gopact.SurfaceAction{
			ID:          value.ActionID,
			Type:        actionType,
			IDs:         value.IDs,
			InterruptID: value.InterruptID,
			CallID:      value.CallID,
			Payload:     value.Payload,
			Metadata:    copyAnyMap(value.Metadata),
		},
		Payload:   value.Payload,
		Metadata:  metadata,
		CreatedAt: createdAt,
	}
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
	default:
		return Payload{}, fmt.Errorf("%w: %T", ErrUnsupportedPayload, payload.Data)
	}
}

func needsInteractiveCard(msg gopact.SurfaceMessage) bool {
	return len(msg.Actions) > 0 || len(msg.Artifacts) > 0 ||
		msg.Type == gopact.SurfaceMessageApproval ||
		msg.Type == gopact.SurfaceMessageSelection ||
		msg.Type == gopact.SurfaceMessageArtifact
}

func buttonType(actionType gopact.SurfaceActionType) string {
	switch actionType {
	case gopact.SurfaceActionCancel:
		return "danger"
	case gopact.SurfaceActionResume, gopact.SurfaceActionSubmit, gopact.SurfaceActionSelect:
		return "primary"
	default:
		return "default"
	}
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

func renderArtifacts(artifacts []gopact.ArtifactRef) string {
	lines := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		label := artifact.Name
		if label == "" {
			label = artifact.ID
		}
		if artifact.URI != "" {
			label = label + " (" + artifact.URI + ")"
		}
		if strings.TrimSpace(label) != "" {
			lines = append(lines, label)
		}
	}
	return strings.Join(lines, "\n")
}

func copyPayload(in Payload) Payload {
	in.Metadata = copyAnyMap(in.Metadata)
	if in.Card != nil {
		card := *in.Card
		card.Config = copyAnyMap(card.Config)
		card.Elements = copyCardElements(card.Elements)
		if card.Header != nil {
			header := *card.Header
			card.Header = &header
		}
		in.Card = &card
	}
	return in
}

func copyCardElements(in []CardElement) []CardElement {
	out := append([]CardElement(nil), in...)
	for i := range out {
		if out[i].Text != nil {
			text := *out[i].Text
			out[i].Text = &text
		}
		out[i].Actions = append([]CardAction(nil), out[i].Actions...)
		for j := range out[i].Actions {
			out[i].Actions[j].Value.Metadata = copyAnyMap(out[i].Actions[j].Value.Metadata)
		}
	}
	return out
}

func copySurfaceMessage(msg gopact.SurfaceMessage) gopact.SurfaceMessage {
	msg.Target.Metadata = copyAnyMap(msg.Target.Metadata)
	msg.Parts = append([]gopact.SurfacePart(nil), msg.Parts...)
	for i := range msg.Parts {
		msg.Parts[i].Metadata = copyAnyMap(msg.Parts[i].Metadata)
	}
	msg.Actions = append([]gopact.SurfaceAction(nil), msg.Actions...)
	for i := range msg.Actions {
		msg.Actions[i] = copySurfaceAction(msg.Actions[i])
	}
	msg.Artifacts = append([]gopact.ArtifactRef(nil), msg.Artifacts...)
	for i := range msg.Artifacts {
		msg.Artifacts[i].Metadata = copyAnyMap(msg.Artifacts[i].Metadata)
	}
	msg.Metadata = copyAnyMap(msg.Metadata)
	return msg
}

func copySurfaceAction(action gopact.SurfaceAction) gopact.SurfaceAction {
	action.Metadata = copyAnyMap(action.Metadata)
	return action
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
