package agui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestTransferConvertsMessageSurfaceToAGUIEvents(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 12, 0, 0, 123000000, time.UTC)
	msg := gopact.SurfaceMessage{
		ID:   "message-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type: gopact.SurfaceMessageMessage,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "hello"},
			{Type: gopact.SurfacePartStatus, Text: "working"},
		},
		SourceEvent: "model_message",
		Metadata:    map[string]any{"source": "test"},
		CreatedAt:   createdAt,
	}

	payload, err := NewTransfer().Convert(context.Background(), msg)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if payload.Target != Target {
		t.Fatalf("payload.Target = %q, want %q", payload.Target, Target)
	}
	got, ok := payload.Data.(Payload)
	if !ok {
		t.Fatalf("payload.Data type = %T, want agui.Payload", payload.Data)
	}
	if got.MIMEType != MIMEType || got.SurfaceID != "message-1" || got.IDs.RunID != "run-1" {
		t.Fatalf("payload identity = %+v, want AG-UI payload with copied identity", got)
	}
	if got.Metadata["source"] != "test" {
		t.Fatalf("payload metadata = %+v, want copied surface metadata", got.Metadata)
	}

	want := []Event{
		{Type: EventRunStarted, Timestamp: createdAt.UnixMilli(), ThreadID: "thread-1", RunID: "run-1"},
		{Type: EventTextMessageStart, Timestamp: createdAt.UnixMilli(), MessageID: "message-1", Role: "assistant"},
		{Type: EventTextMessageContent, Timestamp: createdAt.UnixMilli(), MessageID: "message-1", Delta: "hello\nworking"},
		{Type: EventTextMessageEnd, Timestamp: createdAt.UnixMilli(), MessageID: "message-1"},
		{
			Type:      EventRunFinished,
			Timestamp: createdAt.UnixMilli(),
			ThreadID:  "thread-1",
			RunID:     "run-1",
			Result: map[string]any{
				"surface_id":   "message-1",
				"surface_type": string(gopact.SurfaceMessageMessage),
			},
		},
	}
	if !reflect.DeepEqual(got.Events, want) {
		t.Fatalf("events = %+v, want %+v", got.Events, want)
	}

	msg.Metadata["source"] = "mutated"
	got.Metadata["source"] = "payload-mutated"
	payload2, err := NewTransfer().Convert(context.Background(), msg)
	if err != nil {
		t.Fatalf("Convert(second) error = %v", err)
	}
	got2 := payload2.Data.(Payload)
	if got2.Metadata["source"] != "mutated" {
		t.Fatalf("second payload metadata = %+v, want fresh copy from input", got2.Metadata)
	}
}

func TestTransferConvertsTextDeltaSurfaceToContentEvent(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 12, 0, 0, 456000000, time.UTC)
	payload, err := NewTransfer().Convert(context.Background(), gopact.SurfaceMessage{
		ID:        "message-1",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type:      gopact.SurfaceMessageTextDelta,
		Parts:     []gopact.SurfacePart{{Type: gopact.SurfacePartText, Text: "streaming token"}},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	want := []Event{
		{
			Type:      EventTextMessageContent,
			Timestamp: createdAt.UnixMilli(),
			MessageID: "message-1",
			Delta:     "streaming token",
		},
	}
	if !reflect.DeepEqual(got.Events, want) {
		t.Fatalf("events = %+v, want %+v", got.Events, want)
	}
}

func TestTransferConvertsErrorSurfaceToRunErrorEvent(t *testing.T) {
	payload, err := NewTransfer().Convert(context.Background(), gopact.SurfaceMessage{
		ID:          "error-1",
		IDs:         gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type:        gopact.SurfaceMessageError,
		Parts:       []gopact.SurfacePart{{Type: gopact.SurfacePartText, Text: "provider failed"}},
		SourceEvent: "run_failed",
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	want := []Event{
		{Type: EventRunStarted, ThreadID: "thread-1", RunID: "run-1"},
		{Type: EventRunError, Message: "provider failed", Code: "surface_error"},
	}
	if !reflect.DeepEqual(got.Events, want) {
		t.Fatalf("events = %+v, want %+v", got.Events, want)
	}
}

func TestTransferConvertsApprovalSurfaceToCustomEvent(t *testing.T) {
	payload, err := NewTransfer().Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "approval-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Type: gopact.SurfaceMessageApproval,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "approve write?"},
		},
		Actions: []gopact.SurfaceAction{{
			ID:          "resume-1",
			Type:        gopact.SurfaceActionResume,
			Label:       "Approve",
			InterruptID: "interrupt-1",
			Payload:     map[string]any{"approved": true},
		}},
		Artifacts: []gopact.ArtifactRef{{ID: "artifact-1", Name: "patch.diff", URI: "file://patch.diff"}},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	got := payload.Data.(Payload)
	if len(got.Events) != 3 {
		t.Fatalf("events count = %d, want run started, custom, run finished", len(got.Events))
	}
	if got.Events[1].Type != EventCustom || got.Events[1].Name != "gopact.surface.approval" {
		t.Fatalf("custom event = %+v, want gopact approval custom event", got.Events[1])
	}
	value, ok := got.Events[1].Value.(map[string]any)
	if !ok {
		t.Fatalf("custom value type = %T, want map", got.Events[1].Value)
	}
	if value["text"] != "approve write?" || value["surface_type"] != string(gopact.SurfaceMessageApproval) {
		t.Fatalf("custom value = %+v, want approval surface data", value)
	}
	actions, ok := value["actions"].([]gopact.SurfaceAction)
	if !ok || len(actions) != 1 || actions[0].InterruptID != "interrupt-1" {
		t.Fatalf("custom actions = %+v, want copied approval action", value["actions"])
	}
	artifacts, ok := value["artifacts"].([]gopact.ArtifactRef)
	if !ok || len(artifacts) != 1 || artifacts[0].ID != "artifact-1" {
		t.Fatalf("custom artifacts = %+v, want copied artifact refs", value["artifacts"])
	}
}

func TestChannelStreamsPayloadEventsAsServerSentEvents(t *testing.T) {
	channel := NewChannel()
	server := httptest.NewServer(channel.StreamHandler())
	defer server.Close()
	defer func() {
		if err := channel.Close(context.Background()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	reader := bufio.NewReader(resp.Body)
	initial, err := readAGUISSEFrame(reader)
	if err != nil {
		t.Fatalf("read initial stream frame error = %v", err)
	}
	if !strings.HasPrefix(initial, ":") {
		t.Fatalf("initial frame = %q, want SSE comment", initial)
	}

	err = channel.Send(context.Background(), gopact.ChannelPayload{
		Target: Target,
		Data: Payload{
			SurfaceID: "surface-1",
			Events: []Event{
				{Type: EventRunStarted, ThreadID: "thread-1", RunID: "run-1"},
				{Type: EventTextMessageContent, MessageID: "message-1", Delta: "hello"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	frame, err := readAGUISSEFrame(reader)
	if err != nil {
		t.Fatalf("read first event frame error = %v", err)
	}
	if !strings.Contains(frame, "id: surface-1\n") {
		t.Fatalf("frame = %q, want surface id line", frame)
	}
	if !strings.Contains(frame, "event: RUN_STARTED\n") {
		t.Fatalf("frame = %q, want AG-UI event type line", frame)
	}
	if !strings.Contains(frame, `"type":"RUN_STARTED"`) || !strings.Contains(frame, `"threadId":"thread-1"`) {
		t.Fatalf("frame = %q, want AG-UI run started JSON", frame)
	}

	frame, err = readAGUISSEFrame(reader)
	if err != nil {
		t.Fatalf("read second event frame error = %v", err)
	}
	if !strings.Contains(frame, "event: TEXT_MESSAGE_CONTENT\n") {
		t.Fatalf("frame = %q, want AG-UI content event type line", frame)
	}
	if !strings.Contains(frame, `"delta":"hello"`) {
		t.Fatalf("frame = %q, want AG-UI text delta JSON", frame)
	}
}

func TestChannelActionHandlerPublishesInboundChannelEvents(t *testing.T) {
	channel := NewChannel()
	server := httptest.NewServer(channel.ActionHandler())
	defer server.Close()
	defer func() {
		if err := channel.Close(context.Background()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	input := gopact.ChannelEvent{
		ID:      "event-1",
		Type:    gopact.ChannelEventAction,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Payload: map[string]any{"approved": true},
		Action: gopact.SurfaceAction{
			ID:          "resume-1",
			Type:        gopact.SurfaceActionResume,
			InterruptID: "interrupt-1",
		},
		Metadata: map[string]any{"source": "agui-client"},
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	resp, err := http.Post(server.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("action status = %d body=%q, want 202", resp.StatusCode, raw)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := nextAGUIEvent(ctx, channel)
	if err != nil {
		t.Fatalf("nextAGUIEvent() error = %v", err)
	}
	if event.Channel != Target {
		t.Fatalf("event.Channel = %q, want %q", event.Channel, Target)
	}
	if event.ID != "event-1" || event.Action.InterruptID != "interrupt-1" {
		t.Fatalf("event = %+v, want posted resume event", event)
	}
	resume, ok := event.ResumeRequest()
	if !ok {
		t.Fatalf("ResumeRequest() ok = false, want true")
	}
	if resume.InterruptID != "interrupt-1" || resume.IDs.RunID != "run-1" {
		t.Fatalf("ResumeRequest() = %+v, want interrupt/run ids", resume)
	}
}

func readAGUISSEFrame(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if line == "\n" || line == "\r\n" {
			return builder.String(), nil
		}
		builder.WriteString(line)
	}
}

func nextAGUIEvent(ctx context.Context, channel gopact.Channel) (gopact.ChannelEvent, error) {
	for event, err := range channel.Events(ctx) {
		if err != nil {
			return gopact.ChannelEvent{}, err
		}
		return event, nil
	}
	return gopact.ChannelEvent{}, errors.New("event stream closed")
}
