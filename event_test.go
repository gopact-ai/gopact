package gopact

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEventWithRuntimeDefaultsFillsIDsAndCompatibilityFields(t *testing.T) {
	event := Event{
		Type: EventNodeCompleted,
		IDs:  RuntimeIDs{ThreadID: "thread-1"},
	}

	got := event.WithRuntimeDefaults(RuntimeIDs{RunID: "run-1", ThreadID: "default-thread", AgentID: "agent-1"})

	if got.IDs.RunID != "run-1" || got.IDs.ThreadID != "thread-1" || got.IDs.AgentID != "agent-1" {
		t.Fatalf("WithRuntimeDefaults() IDs = %+v", got.IDs)
	}
	if got.RunID != "run-1" || got.ThreadID != "thread-1" {
		t.Fatalf("compatibility fields = run %q thread %q", got.RunID, got.ThreadID)
	}
}

func TestEventRuntimeIDsUsesCompatibilityFieldsAsInputs(t *testing.T) {
	event := Event{RunID: "run-legacy", ThreadID: "thread-legacy"}

	got := event.RuntimeIDs()
	if got.RunID != "run-legacy" || got.ThreadID != "thread-legacy" {
		t.Fatalf("RuntimeIDs() = %+v", got)
	}
}

func TestEventErrorString(t *testing.T) {
	event := Event{Err: errors.New("boom")}
	if got := event.Error(); got != "boom" {
		t.Fatalf("Error() = %q, want boom", got)
	}

	if got := (Event{}).Error(); got != "" {
		t.Fatalf("zero Event.Error() = %q, want empty", got)
	}
}

func TestM3EventTypeStrings(t *testing.T) {
	tests := map[EventType]string{
		EventToolRegistered:           "tool_registered",
		EventToolPromoted:             "tool_promoted",
		EventTurnInputReceived:        "turn_input_received",
		EventTurnInputMerged:          "turn_input_merged",
		EventTurnInterrupted:          "turn_interrupted",
		EventPolicyRequested:          "policy_requested",
		EventPolicyDecided:            "policy_decided",
		EventSandboxCreated:           "sandbox_created",
		EventSandboxExecCompleted:     "sandbox_exec_completed",
		EventRunInterrupted:           "run_interrupted",
		EventCheckpointLoaded:         "checkpoint_loaded",
		EventStepImported:             "step_imported",
		EventResumeReceived:           "resume_received",
		EventNodeResumed:              "node_resumed",
		EventMemoryPut:                "memory_put",
		EventSkillActivated:           "skill_activated",
		EventMCPServerConnected:       "mcp_server_connected",
		EventA2AAgentHeartbeat:        "a2a_agent_heartbeat",
		EventA2AAgentCardFetched:      "a2a_agent_card_fetched",
		EventA2AMessageReceived:       "a2a_message_received",
		EventA2AArtifactUpdated:       "a2a_artifact_updated",
		EventA2ATaskStatusUpdated:     "a2a_task_status_updated",
		EventA2ATaskCompleted:         "a2a_task_completed",
		EventSurfaceMessageProjected:  "surface_message_projected",
		EventChannelTransferCompleted: "channel_transfer_completed",
		EventChannelSendCompleted:     "channel_send_completed",
		EventChannelActionReceived:    "channel_action_received",
	}

	for eventType, want := range tests {
		if string(eventType) != want {
			t.Fatalf("event type = %q, want %q", eventType, want)
		}
	}
}

func TestEventRedactionMiddlewareRedactsEventBeforeNext(t *testing.T) {
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "secret", "[redacted]"), nil
	})
	var got Event
	handler := ComposeEventHandler(func(c *EventContext) error {
		got = c.Event
		return nil
	}, EventRedactionMiddleware(redactor))

	err := handler(NewEventContext(context.Background(), Event{
		Type: EventToolResult,
		Message: &Message{
			Content: "secret message",
			Parts: []ContentPart{
				TextPart("secret text part"),
				ReasoningPart("secret reasoning"),
				ImagePart("secret://image", "image/png"),
			},
			ToolCalls: []ToolCall{{Name: "lookup", Arguments: []byte(`{"token":"secret"}`)}},
		},
		ToolCall: &ToolCall{Name: "lookup", Arguments: []byte(`{"token":"secret"}`)},
		Result:   &ToolResult{Content: "secret result", Metadata: map[string]any{"token": "secret"}},
		Metadata: map[string]any{"summary": "secret metadata", "count": 1},
	}))
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if got.Message.Content != "[redacted] message" {
		t.Fatalf("message content = %q", got.Message.Content)
	}
	if got.Message.Parts[0].Text != "[redacted] text part" {
		t.Fatalf("text part = %q", got.Message.Parts[0].Text)
	}
	if got.Message.Parts[1].Text != "[redacted] reasoning" {
		t.Fatalf("reasoning part = %q", got.Message.Parts[1].Text)
	}
	if got.Message.Parts[2].URI != "secret://image" {
		t.Fatalf("image uri = %q, want unchanged", got.Message.Parts[2].URI)
	}
	if string(got.Message.ToolCalls[0].Arguments) != `{"token":"[redacted]"}` {
		t.Fatalf("message tool args = %s", got.Message.ToolCalls[0].Arguments)
	}
	if string(got.ToolCall.Arguments) != `{"token":"[redacted]"}` {
		t.Fatalf("tool args = %s", got.ToolCall.Arguments)
	}
	if got.Result.Content != "[redacted] result" {
		t.Fatalf("result content = %q", got.Result.Content)
	}
	if got.Result.Metadata["token"] != "[redacted]" {
		t.Fatalf("result metadata = %+v", got.Result.Metadata)
	}
	if got.Metadata["summary"] != "[redacted] metadata" || got.Metadata["count"] != 1 {
		t.Fatalf("event metadata = %+v", got.Metadata)
	}
	if !got.Redaction.Applied {
		t.Fatalf("redaction state = %+v, want applied", got.Redaction)
	}
	if !containsString(got.Redaction.Fields, "message.content") ||
		!containsString(got.Redaction.Fields, "tool_call.arguments") ||
		!containsString(got.Redaction.Fields, "result.content") ||
		!containsString(got.Redaction.Fields, "metadata.summary") {
		t.Fatalf("redaction fields = %v, want key redacted fields", got.Redaction.Fields)
	}
}

func TestEventRedactionMiddlewarePropagatesRedactorError(t *testing.T) {
	wantErr := errors.New("redactor unavailable")
	redactor := TextRedactorFunc(func(ctx context.Context, text string) (string, error) {
		return "", wantErr
	})
	handler := ComposeEventHandler(func(_ *EventContext) error {
		t.Fatal("event final handler should not run after redaction error")
		return nil
	}, EventRedactionMiddleware(redactor))

	err := handler(NewEventContext(context.Background(), Event{
		Message: &Message{Content: "secret"},
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
