// Package agui provides an AG-UI-oriented transfer adapter.
package agui

import (
	"context"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// Target is the channel target name used by the AG-UI adapter.
	Target gopact.ChannelTarget = "agui"

	// MIMEType is the media type used when AG-UI events are carried by a generic channel.
	MIMEType = "application/ag-ui+json"
)

// EventType identifies one AG-UI event.
type EventType string

const (
	EventTextMessageStart   EventType = "TEXT_MESSAGE_START"
	EventTextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd     EventType = "TEXT_MESSAGE_END"
	EventToolCallStart      EventType = "TOOL_CALL_START"
	EventToolCallArgs       EventType = "TOOL_CALL_ARGS"
	EventToolCallEnd        EventType = "TOOL_CALL_END"
	EventToolCallResult     EventType = "TOOL_CALL_RESULT"
	EventStateSnapshot      EventType = "STATE_SNAPSHOT"
	EventStateDelta         EventType = "STATE_DELTA"
	EventMessagesSnapshot   EventType = "MESSAGES_SNAPSHOT"
	EventActivitySnapshot   EventType = "ACTIVITY_SNAPSHOT"
	EventActivityDelta      EventType = "ACTIVITY_DELTA"
	EventRaw                EventType = "RAW"
	EventCustom             EventType = "CUSTOM"
	EventRunStarted         EventType = "RUN_STARTED"
	EventRunFinished        EventType = "RUN_FINISHED"
	EventRunError           EventType = "RUN_ERROR"
)

// Payload is the AG-UI representation of a surface message.
type Payload struct {
	MIMEType    string            `json:"mime_type,omitempty"`
	SurfaceID   string            `json:"surface_id,omitempty"`
	IDs         gopact.RuntimeIDs `json:"ids,omitempty"`
	Events      []Event           `json:"events,omitempty"`
	SourceEvent string            `json:"source_event,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
}

// Event is a compact AG-UI event shape. Fields use AG-UI JSON names so the
// payload can be sent directly by an injected SSE, WebSocket, webhook, or HTTP channel.
type Event struct {
	Type            EventType `json:"type"`
	Timestamp       int64     `json:"timestamp,omitempty"`
	RawEvent        any       `json:"rawEvent,omitempty"`
	ThreadID        string    `json:"threadId,omitempty"`
	RunID           string    `json:"runId,omitempty"`
	ParentRunID     string    `json:"parentRunId,omitempty"`
	MessageID       string    `json:"messageId,omitempty"`
	Role            string    `json:"role,omitempty"`
	Delta           string    `json:"delta,omitempty"`
	ToolCallID      string    `json:"toolCallId,omitempty"`
	ToolCallName    string    `json:"toolCallName,omitempty"`
	ParentMessageID string    `json:"parentMessageId,omitempty"`
	Content         string    `json:"content,omitempty"`
	Message         string    `json:"message,omitempty"`
	Code            string    `json:"code,omitempty"`
	Name            string    `json:"name,omitempty"`
	Value           any       `json:"value,omitempty"`
	Result          any       `json:"result,omitempty"`
}

// Transfer converts gopact surface messages into AG-UI event payloads.
type Transfer struct{}

var _ gopact.Transfer = (*Transfer)(nil)

// NewTransfer creates an AG-UI transfer.
func NewTransfer() *Transfer {
	return &Transfer{}
}

// Name returns the transfer name.
func (t *Transfer) Name() string {
	return "agui"
}

// Supports reports whether target is the AG-UI channel target.
func (t *Transfer) Supports(target gopact.ChannelTarget) bool {
	return target == "" || target == Target
}

// Convert converts one surface message into a typed AG-UI payload.
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
		"mime_type":    MIMEType,
	}
	payload := Payload{
		MIMEType:    MIMEType,
		SurfaceID:   msg.ID,
		IDs:         msg.IDs,
		Events:      eventsFromSurfaceMessage(msg),
		SourceEvent: msg.SourceEvent,
		Metadata:    copyAnyMap(msg.Metadata),
		CreatedAt:   msg.CreatedAt,
	}
	return gopact.ChannelPayload{
		Target:   Target,
		Data:     payload,
		Metadata: metadata,
	}, nil
}

func eventsFromSurfaceMessage(msg gopact.SurfaceMessage) []Event {
	started := runStartedEvent(msg)
	switch msg.Type {
	case gopact.SurfaceMessageMessage:
		return append([]Event{started}, textMessageEvents(msg)...)
	case gopact.SurfaceMessageTextDelta:
		return []Event{textDeltaEvent(msg)}
	case gopact.SurfaceMessageError:
		return []Event{started, runErrorEvent(msg)}
	default:
		return []Event{started, customSurfaceEvent(msg), runFinishedEvent(msg)}
	}
}

func textMessageEvents(msg gopact.SurfaceMessage) []Event {
	messageID := messageID(msg)
	timestamp := timestampMillis(msg.CreatedAt)
	return []Event{
		{
			Type:      EventTextMessageStart,
			Timestamp: timestamp,
			MessageID: messageID,
			Role:      "assistant",
		},
		{
			Type:      EventTextMessageContent,
			Timestamp: timestamp,
			MessageID: messageID,
			Delta:     renderParts(msg.Parts),
		},
		{
			Type:      EventTextMessageEnd,
			Timestamp: timestamp,
			MessageID: messageID,
		},
		runFinishedEvent(msg),
	}
}

func textDeltaEvent(msg gopact.SurfaceMessage) Event {
	return Event{
		Type:      EventTextMessageContent,
		Timestamp: timestampMillis(msg.CreatedAt),
		MessageID: messageID(msg),
		Delta:     renderParts(msg.Parts),
	}
}

func runStartedEvent(msg gopact.SurfaceMessage) Event {
	return Event{
		Type:      EventRunStarted,
		Timestamp: timestampMillis(msg.CreatedAt),
		ThreadID:  threadID(msg),
		RunID:     runID(msg),
	}
}

func runFinishedEvent(msg gopact.SurfaceMessage) Event {
	return Event{
		Type:      EventRunFinished,
		Timestamp: timestampMillis(msg.CreatedAt),
		ThreadID:  threadID(msg),
		RunID:     runID(msg),
		Result: map[string]any{
			"surface_id":   msg.ID,
			"surface_type": string(msg.Type),
		},
	}
}

func runErrorEvent(msg gopact.SurfaceMessage) Event {
	return Event{
		Type:      EventRunError,
		Timestamp: timestampMillis(msg.CreatedAt),
		Message:   renderParts(msg.Parts),
		Code:      "surface_error",
	}
}

func customSurfaceEvent(msg gopact.SurfaceMessage) Event {
	return Event{
		Type:      EventCustom,
		Timestamp: timestampMillis(msg.CreatedAt),
		Name:      "gopact.surface." + string(msg.Type),
		Value: map[string]any{
			"surface_id":   msg.ID,
			"surface_type": string(msg.Type),
			"ids":          msg.IDs,
			"text":         renderParts(msg.Parts),
			"actions":      copyActions(msg.Actions),
			"artifacts":    copyArtifacts(msg.Artifacts),
			"metadata":     copyAnyMap(msg.Metadata),
			"source_event": msg.SourceEvent,
		},
	}
}

func renderParts(parts []gopact.SurfacePart) string {
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part.Text)
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func threadID(msg gopact.SurfaceMessage) string {
	if msg.IDs.ThreadID != "" {
		return msg.IDs.ThreadID
	}
	if msg.Target.ThreadID != "" {
		return msg.Target.ThreadID
	}
	return msg.ID
}

func runID(msg gopact.SurfaceMessage) string {
	if msg.IDs.RunID != "" {
		return msg.IDs.RunID
	}
	return msg.ID
}

func messageID(msg gopact.SurfaceMessage) string {
	if msg.ID != "" {
		return msg.ID
	}
	return runID(msg)
}

func timestampMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyActions(in []gopact.SurfaceAction) []gopact.SurfaceAction {
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
