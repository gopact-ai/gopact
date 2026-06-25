package sse

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

func TestTransferConvertsSurfaceMessageToSSEPayload(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	msg := gopact.SurfaceMessage{
		ID:   "surface-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type: gopact.SurfaceMessageApproval,
		Target: gopact.SurfaceTarget{
			SessionID: "session-1",
			Metadata:  map[string]any{"target": "web"},
		},
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "approve tool call", Metadata: map[string]any{"part": "text"}},
		},
		Actions: []gopact.SurfaceAction{
			{
				ID:          "resume-1",
				Type:        gopact.SurfaceActionResume,
				Label:       "Approve",
				InterruptID: "interrupt-1",
				Metadata:    map[string]any{"scope": "approval"},
			},
		},
		Artifacts: []gopact.ArtifactRef{
			{ID: "artifact-1", Name: "patch.diff", URI: "file://patch.diff", Metadata: map[string]any{"kind": "patch"}},
		},
		SourceEvent: "interrupted",
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
		t.Fatalf("payload.Data type = %T, want sse.Payload", payload.Data)
	}
	want := Payload{
		ID:      "surface-1",
		Event:   "approval",
		Message: msg,
		Metadata: map[string]any{
			"surface_id":   "surface-1",
			"surface_type": "approval",
			"source_event": "interrupted",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload.Data = %+v, want %+v", got, want)
	}

	msg.Metadata["source"] = "mutated"
	got.Message.Metadata["source"] = "payload-mutated"
	payload2, err := NewTransfer().Convert(context.Background(), msg)
	if err != nil {
		t.Fatalf("Convert(second) error = %v", err)
	}
	got2 := payload2.Data.(Payload)
	if got2.Message.Metadata["source"] != "mutated" {
		t.Fatalf("second payload metadata = %v, want fresh copy from input", got2.Message.Metadata)
	}
}

func TestChannelStreamsSentPayloadsAsServerSentEvents(t *testing.T) {
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
	initial, err := readSSEFrame(reader)
	if err != nil {
		t.Fatalf("read initial stream frame error = %v", err)
	}
	if !strings.HasPrefix(initial, ":") {
		t.Fatalf("initial frame = %q, want SSE comment", initial)
	}

	err = channel.Send(context.Background(), gopact.ChannelPayload{
		Target: Target,
		Data: Payload{
			ID:    "surface-1",
			Event: "message",
			Message: gopact.SurfaceMessage{
				ID:   "surface-1",
				Type: gopact.SurfaceMessageMessage,
				Parts: []gopact.SurfacePart{
					{Type: gopact.SurfacePartText, Text: "hello"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	frame, err := readSSEFrame(reader)
	if err != nil {
		t.Fatalf("readSSEFrame() error = %v", err)
	}
	if !strings.Contains(frame, "id: surface-1\n") {
		t.Fatalf("frame = %q, want id line", frame)
	}
	if !strings.Contains(frame, "event: message\n") {
		t.Fatalf("frame = %q, want event line", frame)
	}
	if !strings.Contains(frame, `"text":"hello"`) {
		t.Fatalf("frame = %q, want JSON message payload", frame)
	}
}

func TestActionHandlerPublishesInboundChannelEvents(t *testing.T) {
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
		Metadata: map[string]any{"source": "button"},
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
	event, err := nextEvent(ctx, channel)
	if err != nil {
		t.Fatalf("nextEvent() error = %v", err)
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

func TestChannelCloseStopsSendAndEvents(t *testing.T) {
	channel := NewChannel()
	if err := channel.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := channel.Send(context.Background(), gopact.ChannelPayload{Target: Target, Data: Payload{ID: "surface-1"}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send(closed) error = %v, want ErrClosed", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for event, err := range channel.Events(ctx) {
		t.Fatalf("Events() yielded event=%+v err=%v after close", event, err)
	}
}

func readSSEFrame(reader *bufio.Reader) (string, error) {
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

func nextEvent(ctx context.Context, channel gopact.Channel) (gopact.ChannelEvent, error) {
	for event, err := range channel.Events(ctx) {
		if err != nil {
			return gopact.ChannelEvent{}, err
		}
		return event, nil
	}
	return gopact.ChannelEvent{}, errors.New("event stream closed")
}
