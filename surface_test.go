package gopact

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"
	"time"
)

func TestProjectSurfaceMessagesFromRuntimeEvents(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 11, 0, 0, 0, time.UTC)
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", SessionID: "session-1", CallID: "call-1"}
	events := []Event{
		{
			Type:      EventModelMessage,
			IDs:       ids,
			Node:      "call_model",
			Step:      1,
			Message:   &Message{Role: RoleAssistant, Content: "hello"},
			CreatedAt: createdAt,
		},
		{
			Type:      EventToolCall,
			IDs:       ids,
			Node:      "call_tool",
			Step:      2,
			ToolCall:  &ToolCall{ID: "tool-call-1", Name: "repo.read"},
			CreatedAt: createdAt.Add(time.Second),
		},
		{
			Type: EventToolResult,
			IDs:  ids,
			Node: "call_tool",
			Step: 2,
			Result: &ToolResult{
				Content:   "read ok",
				Artifacts: []ArtifactRef{{ID: "artifact-1", Name: "trace.json", MIMEType: "application/json"}},
			},
			CreatedAt: createdAt.Add(2 * time.Second),
		},
		{
			Type: EventInterrupted,
			IDs:  ids,
			StepSnapshot: &StepSnapshot{
				Pending: &InterruptRecord{
					ID:           "interrupt-1",
					Type:         InterruptApproval,
					Reason:       "approve tool",
					RequiredBy:   "tool",
					ResumeSchema: JSONSchema{"type": "object"},
				},
			},
			CreatedAt: createdAt.Add(3 * time.Second),
		},
		{
			Type: EventA2AAgentCardFetched,
			IDs:  ids,
			Metadata: map[string]any{
				"agent_name": "planner",
				"agent_url":  "https://agents.example/planner",
			},
			CreatedAt: createdAt.Add(4 * time.Second),
		},
		{
			Type: EventA2ATaskStatusUpdated,
			IDs:  ids,
			Metadata: map[string]any{
				"a2a_status":  "running",
				"a2a_message": "drafting",
				"progress":    0.5,
			},
			CreatedAt: createdAt.Add(5 * time.Second),
		},
		{
			Type: EventA2AMessageReceived,
			IDs:  ids,
			Metadata: map[string]any{
				"a2a_message": "outline ready",
				"phase":       "outline",
			},
			CreatedAt: createdAt.Add(6 * time.Second),
		},
		{
			Type:      EventA2AArtifactUpdated,
			IDs:       ids,
			Artifacts: []ArtifactRef{{ID: "artifact-2", Name: "plan.md", MIMEType: "text/markdown"}},
			Metadata: map[string]any{
				"phase": "draft",
			},
			CreatedAt: createdAt.Add(7 * time.Second),
		},
		{
			Type:      EventRunFailed,
			IDs:       ids,
			Err:       errors.New("boom"),
			CreatedAt: createdAt.Add(8 * time.Second),
		},
	}

	var got []SurfaceMessage
	for _, event := range events {
		got = append(got, ProjectSurfaceMessages(event)...)
	}

	want := []SurfaceMessage{
		{
			ID:          "surface:run-1:model_message:call_model:1",
			IDs:         ids,
			Type:        SurfaceMessageMessage,
			Parts:       []SurfacePart{{Type: SurfacePartText, Text: "hello"}},
			SourceEvent: string(EventModelMessage),
			CreatedAt:   createdAt,
		},
		{
			ID:          "surface:run-1:tool_call:call_tool:2",
			IDs:         ids,
			Type:        SurfaceMessageToolCall,
			Parts:       []SurfacePart{{Type: SurfacePartStatus, Text: "repo.read"}},
			Actions:     []SurfaceAction{{ID: "tool-call-1", Type: SurfaceActionOpen, Label: "repo.read", IDs: ids, CallID: "tool-call-1"}},
			SourceEvent: string(EventToolCall),
			CreatedAt:   createdAt.Add(time.Second),
		},
		{
			ID:          "surface:run-1:tool_result:call_tool:2",
			IDs:         ids,
			Type:        SurfaceMessageToolResult,
			Parts:       []SurfacePart{{Type: SurfacePartText, Text: "read ok"}},
			Artifacts:   []ArtifactRef{{ID: "artifact-1", Name: "trace.json", MIMEType: "application/json"}},
			SourceEvent: string(EventToolResult),
			CreatedAt:   createdAt.Add(2 * time.Second),
		},
		{
			ID:    "surface:run-1:interrupted::0",
			IDs:   ids,
			Type:  SurfaceMessageApproval,
			Parts: []SurfacePart{{Type: SurfacePartText, Text: "approve tool"}},
			Actions: []SurfaceAction{{
				ID:          "interrupt-1",
				Type:        SurfaceActionResume,
				Label:       "approve tool",
				IDs:         ids,
				InterruptID: "interrupt-1",
				Metadata: map[string]any{
					"required_by":    "tool",
					"interrupt_type": string(InterruptApproval),
				},
			}},
			SourceEvent: string(EventInterrupted),
			CreatedAt:   createdAt.Add(3 * time.Second),
		},
		{
			ID:          "surface:run-1:a2a_agent_card_fetched::0",
			IDs:         ids,
			Type:        SurfaceMessageStatus,
			Parts:       []SurfacePart{{Type: SurfacePartStatus, Text: "a2a agent card fetched"}},
			SourceEvent: string(EventA2AAgentCardFetched),
			Metadata: map[string]any{
				"agent_name": "planner",
				"agent_url":  "https://agents.example/planner",
			},
			CreatedAt: createdAt.Add(4 * time.Second),
		},
		{
			ID:          "surface:run-1:a2a_task_status_updated::0",
			IDs:         ids,
			Type:        SurfaceMessageStatus,
			Parts:       []SurfacePart{{Type: SurfacePartStatus, Text: "a2a task status updated"}},
			SourceEvent: string(EventA2ATaskStatusUpdated),
			Metadata: map[string]any{
				"a2a_status":  "running",
				"a2a_message": "drafting",
				"progress":    0.5,
			},
			CreatedAt: createdAt.Add(5 * time.Second),
		},
		{
			ID:          "surface:run-1:a2a_message_received::0",
			IDs:         ids,
			Type:        SurfaceMessageStatus,
			Parts:       []SurfacePart{{Type: SurfacePartStatus, Text: "a2a message received"}},
			SourceEvent: string(EventA2AMessageReceived),
			Metadata: map[string]any{
				"a2a_message": "outline ready",
				"phase":       "outline",
			},
			CreatedAt: createdAt.Add(6 * time.Second),
		},
		{
			ID:          "surface:run-1:a2a_artifact_updated::0",
			IDs:         ids,
			Type:        SurfaceMessageArtifact,
			Artifacts:   []ArtifactRef{{ID: "artifact-2", Name: "plan.md", MIMEType: "text/markdown"}},
			SourceEvent: string(EventA2AArtifactUpdated),
			Metadata: map[string]any{
				"phase": "draft",
			},
			CreatedAt: createdAt.Add(7 * time.Second),
		},
		{
			ID:          "surface:run-1:run_failed::0",
			IDs:         ids,
			Type:        SurfaceMessageError,
			Parts:       []SurfacePart{{Type: SurfacePartText, Text: "boom"}},
			SourceEvent: string(EventRunFailed),
			CreatedAt:   createdAt.Add(8 * time.Second),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectSurfaceMessages() = %+v, want %+v", got, want)
	}
}

func TestProjectSurfaceMessagesCopiesMutableFields(t *testing.T) {
	event := Event{
		Type: EventToolResult,
		IDs:  RuntimeIDs{RunID: "run-1"},
		Result: &ToolResult{
			Content:   "ok",
			Artifacts: []ArtifactRef{{ID: "artifact-1", Metadata: map[string]any{"scope": "run"}}},
			Metadata:  map[string]any{"detail": "before"},
		},
	}

	messages := ProjectSurfaceMessages(event)
	if len(messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(messages))
	}
	messages[0].Artifacts[0].Metadata["scope"] = "mutated"
	messages[0].Metadata["detail"] = "mutated"

	again := ProjectSurfaceMessages(event)
	if again[0].Artifacts[0].Metadata["scope"] != "run" {
		t.Fatalf("artifact metadata shared: %+v", again[0].Artifacts[0].Metadata)
	}
	if again[0].Metadata["detail"] != "before" {
		t.Fatalf("surface metadata shared: %+v", again[0].Metadata)
	}
}

func TestProjectSurfaceMessagesProjectsA2AAgentRegistered(t *testing.T) {
	event := Event{
		Type: EventA2AAgentRegistered,
		IDs:  RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Metadata: map[string]any{
			"agent_name":       "planner",
			"capability_count": 2,
		},
	}

	messages := ProjectSurfaceMessages(event)
	if len(messages) != 1 ||
		messages[0].Type != SurfaceMessageStatus ||
		messages[0].SourceEvent != string(EventA2AAgentRegistered) ||
		messages[0].Parts[0].Text != "a2a agent registered" ||
		messages[0].Metadata["agent_name"] != "planner" ||
		messages[0].Metadata["capability_count"] != 2 {
		t.Fatalf("ProjectSurfaceMessages() = %+v, want A2A registration status", messages)
	}
	messages[0].Metadata["agent_name"] = "mutated"
	again := ProjectSurfaceMessages(event)
	if again[0].Metadata["agent_name"] != "planner" {
		t.Fatalf("surface metadata shared: %+v", again[0].Metadata)
	}
}

func TestProjectSurfaceMessagesProjectsA2AAgentHeartbeat(t *testing.T) {
	event := Event{
		Type: EventA2AAgentHeartbeat,
		IDs:  RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Metadata: map[string]any{
			"agent_name":       "planner",
			"lease_expires_at": "2026-07-03T10:00:00Z",
		},
	}

	messages := ProjectSurfaceMessages(event)
	if len(messages) != 1 ||
		messages[0].Type != SurfaceMessageStatus ||
		messages[0].SourceEvent != string(EventA2AAgentHeartbeat) ||
		messages[0].Parts[0].Text != "a2a agent heartbeat" ||
		messages[0].Metadata["agent_name"] != "planner" ||
		messages[0].Metadata["lease_expires_at"] != "2026-07-03T10:00:00Z" {
		t.Fatalf("ProjectSurfaceMessages() = %+v, want A2A heartbeat status", messages)
	}
}

func TestTransferFuncConvertsSurfaceMessage(t *testing.T) {
	wantPayload := ChannelPayload{Target: ChannelTarget("tui"), Data: "hello"}
	transfer := TransferFunc{
		NameValue: "test-transfer",
		Targets:   []ChannelTarget{ChannelTarget("tui")},
		ConvertFunc: func(ctx context.Context, msg SurfaceMessage) (ChannelPayload, error) {
			if msg.Type != SurfaceMessageMessage || msg.Parts[0].Text != "hello" {
				t.Fatalf("SurfaceMessage = %+v, want hello message", msg)
			}
			return wantPayload, nil
		},
	}

	if transfer.Name() != "test-transfer" {
		t.Fatalf("Name() = %q, want test-transfer", transfer.Name())
	}
	if !transfer.Supports(ChannelTarget("tui")) || transfer.Supports(ChannelTarget("lark")) {
		t.Fatalf("Supports() did not honor target set")
	}
	payload, err := transfer.Convert(context.Background(), SurfaceMessage{
		Type:  SurfaceMessageMessage,
		Parts: []SurfacePart{{Type: SurfacePartText, Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if !reflect.DeepEqual(payload, wantPayload) {
		t.Fatalf("Convert() = %+v, want %+v", payload, wantPayload)
	}
}

func TestChannelFuncSendsPayloadAndEvents(t *testing.T) {
	sent := make(chan ChannelPayload, 1)
	closed := false
	channel := ChannelFunc{
		NameValue: "test-channel",
		SendFunc: func(ctx context.Context, payload ChannelPayload) error {
			payload.Metadata["mutated"] = true
			sent <- payload
			return nil
		},
		EventsFunc: func(ctx context.Context) iter.Seq2[ChannelEvent, error] {
			return func(yield func(ChannelEvent, error) bool) {
				yield(ChannelEvent{ID: "event-1", Channel: "lark", Type: ChannelEventMessage, Text: "hi"}, nil)
			}
		},
		CloseFunc: func(ctx context.Context) error {
			closed = true
			return nil
		},
	}

	if channel.Name() != "test-channel" {
		t.Fatalf("Name() = %q, want test-channel", channel.Name())
	}
	payload := ChannelPayload{
		Target:   "lark",
		Data:     "card",
		Metadata: map[string]any{"source": "surface"},
	}
	if err := channel.Send(context.Background(), payload); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	gotPayload := <-sent
	if gotPayload.Target != "lark" || gotPayload.Data != "card" || gotPayload.Metadata["source"] != "surface" {
		t.Fatalf("sent payload = %+v", gotPayload)
	}
	if payload.Metadata["mutated"] != nil {
		t.Fatalf("Send() shared payload metadata: %+v", payload.Metadata)
	}

	events, err := collectChannelEvents(channel.Events(context.Background()))
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-1" || events[0].Text != "hi" {
		t.Fatalf("Events() = %+v", events)
	}
	if err := channel.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closed {
		t.Fatal("CloseFunc was not called")
	}
}

func TestChannelFuncRequiresSendFunction(t *testing.T) {
	err := ChannelFunc{}.Send(context.Background(), ChannelPayload{})
	if !errors.Is(err, ErrChannelSendRequired) {
		t.Fatalf("Send() error = %v, want ErrChannelSendRequired", err)
	}
}

func TestChannelEventToResumeRequest(t *testing.T) {
	event := ChannelEvent{
		ID:        "channel-event-1",
		Channel:   "lark",
		Type:      ChannelEventAction,
		IDs:       RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Action:    SurfaceAction{ID: "interrupt-1", Type: SurfaceActionResume, InterruptID: "interrupt-1"},
		Payload:   map[string]any{"approved": true},
		CreatedAt: time.Date(2026, 6, 24, 11, 30, 0, 0, time.UTC),
	}

	req, ok := event.ResumeRequest()
	if !ok {
		t.Fatal("ResumeRequest() ok = false, want true")
	}
	if req.InterruptID != "interrupt-1" || req.IDs.RunID != "run-1" {
		t.Fatalf("ResumeRequest() = %+v", req)
	}
	if !reflect.DeepEqual(req.Payload, map[string]any{"approved": true}) {
		t.Fatalf("ResumeRequest payload = %+v", req.Payload)
	}
}

func collectChannelEvents(seq iter.Seq2[ChannelEvent, error]) ([]ChannelEvent, error) {
	var events []ChannelEvent
	for event, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}
