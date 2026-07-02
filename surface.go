package gopact

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"
)

var (
	ErrTransferConvertRequired = errors.New("gopact: transfer convert function is required")
	ErrChannelSendRequired     = errors.New("gopact: channel send function is required")
)

// SurfaceMessageType identifies platform-neutral display intent.
type SurfaceMessageType string

const (
	SurfaceMessageTextDelta  SurfaceMessageType = "text_delta"
	SurfaceMessageMessage    SurfaceMessageType = "message"
	SurfaceMessageToolCall   SurfaceMessageType = "tool_call"
	SurfaceMessageToolResult SurfaceMessageType = "tool_result"
	SurfaceMessageArtifact   SurfaceMessageType = "artifact"
	SurfaceMessageApproval   SurfaceMessageType = "approval"
	SurfaceMessageSelection  SurfaceMessageType = "selection"
	SurfaceMessageStatus     SurfaceMessageType = "status"
	SurfaceMessageError      SurfaceMessageType = "error"
)

// SurfacePartType identifies one display part in a SurfaceMessage.
type SurfacePartType string

const (
	SurfacePartText   SurfacePartType = "text"
	SurfacePartStatus SurfacePartType = "status"
	SurfacePartMedia  SurfacePartType = "media"
)

// SurfaceActionType identifies an inbound action exposed by a surface message.
type SurfaceActionType string

const (
	SurfaceActionOpen   SurfaceActionType = "open"
	SurfaceActionResume SurfaceActionType = "resume"
	SurfaceActionCancel SurfaceActionType = "cancel"
	SurfaceActionSelect SurfaceActionType = "select"
	SurfaceActionSubmit SurfaceActionType = "submit"
)

// ChannelTarget names a transfer/channel destination such as tui, lark, a2ui, or agui.
type ChannelTarget string

// SurfaceTarget optionally scopes a surface message to a channel target.
type SurfaceTarget struct {
	Channel   ChannelTarget  `json:"channel,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	ThreadID  string         `json:"thread_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SurfacePart is one platform-neutral display part.
type SurfacePart struct {
	Type     SurfacePartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	URI      string          `json:"uri,omitempty"`
	MIMEType string          `json:"mime_type,omitempty"`
	Name     string          `json:"name,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// SurfaceAction describes a user action that must return through TurnLoop boundaries.
type SurfaceAction struct {
	ID          string            `json:"id,omitempty"`
	Type        SurfaceActionType `json:"type"`
	Label       string            `json:"label,omitempty"`
	IDs         RuntimeIDs        `json:"ids,omitempty"`
	InterruptID string            `json:"interrupt_id,omitempty"`
	CallID      string            `json:"call_id,omitempty"`
	Payload     any               `json:"payload,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

// SurfaceMessage is the platform-neutral output semantic consumed by transfer adapters.
type SurfaceMessage struct {
	ID          string             `json:"id,omitempty"`
	IDs         RuntimeIDs         `json:"ids,omitempty"`
	Type        SurfaceMessageType `json:"type"`
	Target      SurfaceTarget      `json:"target,omitempty"`
	Parts       []SurfacePart      `json:"parts,omitempty"`
	Actions     []SurfaceAction    `json:"actions,omitempty"`
	Artifacts   []ArtifactRef      `json:"artifacts,omitempty"`
	SourceEvent string             `json:"source_event,omitempty"`
	Metadata    map[string]any     `json:"metadata,omitempty"`
	CreatedAt   time.Time          `json:"created_at,omitempty"`
}

// ChannelPayload is a target-specific payload produced by a Transfer.
type ChannelPayload struct {
	Target   ChannelTarget  `json:"target,omitempty"`
	Data     any            `json:"data,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Channel sends channel payloads and receives platform-neutral channel events.
type Channel interface {
	Name() string
	Send(ctx context.Context, payload ChannelPayload) error
	Events(ctx context.Context) iter.Seq2[ChannelEvent, error]
	Close(ctx context.Context) error
}

// ChannelFunc adapts functions into a Channel.
type ChannelFunc struct {
	NameValue  string
	SendFunc   func(ctx context.Context, payload ChannelPayload) error
	EventsFunc func(ctx context.Context) iter.Seq2[ChannelEvent, error]
	CloseFunc  func(ctx context.Context) error
}

// Name returns the channel name.
func (c ChannelFunc) Name() string {
	if c.NameValue == "" {
		return "channel"
	}
	return c.NameValue
}

// Send calls the wrapped send function.
func (c ChannelFunc) Send(ctx context.Context, payload ChannelPayload) error {
	if c.SendFunc == nil {
		return ErrChannelSendRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.SendFunc(ctx, copyChannelPayload(payload))
}

// Events returns inbound channel events from the wrapped event function.
func (c ChannelFunc) Events(ctx context.Context) iter.Seq2[ChannelEvent, error] {
	return func(yield func(ChannelEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if err := ctx.Err(); err != nil {
			yield(ChannelEvent{}, err)
			return
		}
		if c.EventsFunc == nil {
			return
		}
		for event, err := range c.EventsFunc(ctx) {
			if !yield(copyChannelEvent(event), err) {
				return
			}
		}
	}
}

// Close calls the wrapped close function when one is present.
func (c ChannelFunc) Close(ctx context.Context) error {
	if c.CloseFunc == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.CloseFunc(ctx)
}

// Transfer converts a SurfaceMessage into a target channel payload.
type Transfer interface {
	Name() string
	Supports(target ChannelTarget) bool
	Convert(ctx context.Context, msg SurfaceMessage) (ChannelPayload, error)
}

// TransferFunc adapts functions into a Transfer.
type TransferFunc struct {
	NameValue   string
	Targets     []ChannelTarget
	ConvertFunc func(ctx context.Context, msg SurfaceMessage) (ChannelPayload, error)
}

// Name returns the transfer name.
func (t TransferFunc) Name() string {
	if t.NameValue == "" {
		return "transfer"
	}
	return t.NameValue
}

// Supports reports whether the transfer supports a target.
func (t TransferFunc) Supports(target ChannelTarget) bool {
	if len(t.Targets) == 0 {
		return true
	}
	for _, candidate := range t.Targets {
		if candidate == target {
			return true
		}
	}
	return false
}

// Convert calls the wrapped convert function.
func (t TransferFunc) Convert(ctx context.Context, msg SurfaceMessage) (ChannelPayload, error) {
	if t.ConvertFunc == nil {
		return ChannelPayload{}, ErrTransferConvertRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ChannelPayload{}, err
	}
	return t.ConvertFunc(ctx, copySurfaceMessage(msg))
}

// ChannelEventType identifies inbound channel events.
type ChannelEventType string

const (
	ChannelEventMessage ChannelEventType = "message"
	ChannelEventAction  ChannelEventType = "action"
	ChannelEventCancel  ChannelEventType = "cancel"
)

// ChannelEvent is platform-neutral inbound channel input.
type ChannelEvent struct {
	ID        string           `json:"id,omitempty"`
	Channel   ChannelTarget    `json:"channel,omitempty"`
	Type      ChannelEventType `json:"type"`
	IDs       RuntimeIDs       `json:"ids,omitempty"`
	Action    SurfaceAction    `json:"action,omitempty"`
	Text      string           `json:"text,omitempty"`
	Payload   any              `json:"payload,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
	CreatedAt time.Time        `json:"created_at,omitempty"`
}

// ResumeRequest converts a resume action into a runtime ResumeRequest.
func (e ChannelEvent) ResumeRequest() (ResumeRequest, bool) {
	if e.Type != ChannelEventAction || e.Action.Type != SurfaceActionResume || e.Action.InterruptID == "" {
		return ResumeRequest{}, false
	}
	return ResumeRequest{
		InterruptID: e.Action.InterruptID,
		IDs:         e.IDs.WithDefaults(e.Action.IDs),
		Payload:     e.Payload,
		CreatedAt:   e.CreatedAt,
		Metadata:    copyAnyMap(e.Metadata),
	}, true
}

// ProjectSurfaceMessages projects one runtime event into zero or more surface messages.
func ProjectSurfaceMessages(event Event) []SurfaceMessage {
	switch event.Type {
	case EventModelMessage:
		if event.Message == nil {
			return nil
		}
		return []SurfaceMessage{surfaceFromMessage(event)}
	case EventToolCall:
		if event.ToolCall == nil {
			return nil
		}
		return []SurfaceMessage{surfaceFromToolCall(event)}
	case EventToolResult:
		if event.Result == nil && len(event.Artifacts) == 0 {
			return nil
		}
		return []SurfaceMessage{surfaceFromToolResult(event)}
	case EventInterrupted, EventRunInterrupted, EventTurnInterrupted:
		if event.StepSnapshot == nil || event.StepSnapshot.Pending == nil {
			return nil
		}
		return []SurfaceMessage{surfaceFromInterrupt(event, *event.StepSnapshot.Pending)}
	case EventRunStarted, EventTurnStarted, EventNodeStarted, EventNodeResumed,
		EventCheckpointLoaded, EventStepImported, EventResumeReceived,
		EventRunCompleted, EventTurnCompleted, EventNodeCompleted,
		EventRunCanceled, EventTurnCanceled,
		EventA2AAgentRegistered, EventA2AAgentCardFetched, EventA2AAgentHeartbeat,
		EventA2ATaskSent, EventA2AMessageReceived, EventA2ATaskStatusUpdated, EventA2ATaskCompleted, EventA2ATaskCanceled:
		return []SurfaceMessage{surfaceStatus(event)}
	case EventA2AArtifactUpdated:
		if len(event.Artifacts) > 0 {
			return []SurfaceMessage{surfaceArtifact(event)}
		}
		return []SurfaceMessage{surfaceStatus(event)}
	case EventRunFailed, EventTurnFailed, EventNodeFailed, EventModelProviderAttemptFailed,
		EventSandboxExecFailed, EventA2ATaskFailed:
		return []SurfaceMessage{surfaceError(event)}
	default:
		if len(event.Artifacts) > 0 {
			return []SurfaceMessage{surfaceArtifact(event)}
		}
		return nil
	}
}

func surfaceFromMessage(event Event) SurfaceMessage {
	return baseSurfaceMessage(event, SurfaceMessageMessage, messageParts(*event.Message), nil)
}

func surfaceFromToolCall(event Event) SurfaceMessage {
	call := event.ToolCall
	msg := baseSurfaceMessage(event, SurfaceMessageToolCall, []SurfacePart{{
		Type: SurfacePartStatus,
		Text: call.Name,
	}}, nil)
	actionID := call.ID
	if actionID == "" {
		actionID = call.Name
	}
	msg.Actions = []SurfaceAction{{
		ID:     actionID,
		Type:   SurfaceActionOpen,
		Label:  call.Name,
		IDs:    event.RuntimeIDs(),
		CallID: call.ID,
	}}
	return msg
}

func surfaceFromToolResult(event Event) SurfaceMessage {
	parts := []SurfacePart(nil)
	metadata := map[string]any(nil)
	artifacts := copyArtifactRefs(event.Artifacts)
	if event.Result != nil {
		if event.Result.Content != "" {
			parts = append(parts, SurfacePart{Type: SurfacePartText, Text: event.Result.Content})
		}
		if len(artifacts) == 0 {
			artifacts = copyArtifactRefs(event.Result.Artifacts)
		}
		metadata = copyAnyMap(event.Result.Metadata)
	}
	msg := baseSurfaceMessage(event, SurfaceMessageToolResult, parts, metadata)
	msg.Artifacts = artifacts
	return msg
}

func surfaceFromInterrupt(event Event, record InterruptRecord) SurfaceMessage {
	msgType := SurfaceMessageApproval
	actionType := SurfaceActionResume
	switch record.Type {
	case InterruptSelection:
		msgType = SurfaceMessageSelection
		actionType = SurfaceActionSelect
	case InterruptInput, InterruptExternalWait:
		msgType = SurfaceMessageStatus
		actionType = SurfaceActionResume
	}
	label := record.Reason
	if label == "" {
		label = string(record.Type)
	}
	msg := baseSurfaceMessage(event, msgType, []SurfacePart{{Type: SurfacePartText, Text: label}}, nil)
	msg.Actions = []SurfaceAction{{
		ID:          record.ID,
		Type:        actionType,
		Label:       label,
		IDs:         event.RuntimeIDs(),
		InterruptID: record.ID,
		Metadata: map[string]any{
			"interrupt_type": string(record.Type),
			"required_by":    record.RequiredBy,
		},
	}}
	return msg
}

func surfaceStatus(event Event) SurfaceMessage {
	text := strings.ReplaceAll(string(event.Type), "_", " ")
	return baseSurfaceMessage(event, SurfaceMessageStatus, []SurfacePart{{Type: SurfacePartStatus, Text: text}}, copyAnyMap(event.Metadata))
}

func surfaceError(event Event) SurfaceMessage {
	text := event.Error()
	if text == "" {
		text = strings.ReplaceAll(string(event.Type), "_", " ")
	}
	return baseSurfaceMessage(event, SurfaceMessageError, []SurfacePart{{Type: SurfacePartText, Text: text}}, nil)
}

func surfaceArtifact(event Event) SurfaceMessage {
	msg := baseSurfaceMessage(event, SurfaceMessageArtifact, nil, copyAnyMap(event.Metadata))
	msg.Artifacts = copyArtifactRefs(event.Artifacts)
	return msg
}

func baseSurfaceMessage(event Event, typ SurfaceMessageType, parts []SurfacePart, metadata map[string]any) SurfaceMessage {
	return SurfaceMessage{
		ID:          surfaceMessageID(event),
		IDs:         event.RuntimeIDs(),
		Type:        typ,
		Parts:       copySurfaceParts(parts),
		Artifacts:   copyArtifactRefs(event.Artifacts),
		SourceEvent: string(event.Type),
		Metadata:    metadata,
		CreatedAt:   event.CreatedAt,
	}
}

func messageParts(message Message) []SurfacePart {
	if len(message.Parts) == 0 {
		if message.Content == "" {
			return nil
		}
		return []SurfacePart{{Type: SurfacePartText, Text: message.Content}}
	}
	parts := make([]SurfacePart, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch part.Type {
		case ContentPartText:
			parts = append(parts, SurfacePart{
				Type:     SurfacePartText,
				Text:     part.Text,
				Metadata: copyAnyMap(part.Metadata),
			})
		case ContentPartImage, ContentPartAudio, ContentPartFile:
			parts = append(parts, SurfacePart{
				Type:     SurfacePartMedia,
				URI:      part.URI,
				MIMEType: part.MIMEType,
				Name:     part.Name,
				Metadata: copyAnyMap(part.Metadata),
			})
		}
	}
	return parts
}

func surfaceMessageID(event Event) string {
	ids := event.RuntimeIDs()
	return fmt.Sprintf("surface:%s:%s:%s:%d", ids.RunID, event.Type, event.Node, event.Step)
}

func copySurfaceMessage(msg SurfaceMessage) SurfaceMessage {
	msg.Target.Metadata = copyAnyMap(msg.Target.Metadata)
	msg.Parts = copySurfaceParts(msg.Parts)
	msg.Actions = copySurfaceActions(msg.Actions)
	msg.Artifacts = copyArtifactRefs(msg.Artifacts)
	msg.Metadata = copyAnyMap(msg.Metadata)
	return msg
}

func copySurfaceParts(in []SurfacePart) []SurfacePart {
	if len(in) == 0 {
		return nil
	}
	out := make([]SurfacePart, len(in))
	for i, part := range in {
		out[i] = part
		out[i].Metadata = copyAnyMap(part.Metadata)
	}
	return out
}

func copySurfaceActions(in []SurfaceAction) []SurfaceAction {
	if len(in) == 0 {
		return nil
	}
	out := make([]SurfaceAction, len(in))
	for i, action := range in {
		out[i] = action
		out[i].Metadata = copyAnyMap(action.Metadata)
	}
	return out
}

func copyChannelPayload(payload ChannelPayload) ChannelPayload {
	payload.Metadata = copyAnyMap(payload.Metadata)
	return payload
}

func copyChannelEvent(event ChannelEvent) ChannelEvent {
	event.Action.Metadata = copyAnyMap(event.Action.Metadata)
	event.Metadata = copyAnyMap(event.Metadata)
	return event
}
