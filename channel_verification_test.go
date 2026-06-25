package gopact

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRecordChannelEventCheckRecordsObservedActionEvent(t *testing.T) {
	recorder := NewVerificationRecorder()
	event := ChannelEvent{
		ID:      "event-1",
		Channel: "agui",
		Type:    ChannelEventAction,
		IDs: RuntimeIDs{
			UserID:    "user-1",
			SessionID: "session-1",
			ThreadID:  "thread-1",
			RunID:     "run-1",
			AgentID:   "agent-1",
			CallID:    "call-1",
			TraceID:   "trace-1",
		},
		Action: SurfaceAction{
			ID:          "action-1",
			Type:        SurfaceActionResume,
			InterruptID: "interrupt-1",
			CallID:      "action-call-1",
			Payload:     map[string]any{"approved": true},
			Metadata:    map[string]any{"button": "approve"},
		},
		Text:      "do not persist raw text",
		Payload:   map[string]any{"approved": true},
		Metadata:  map[string]any{"platform": "web"},
		CreatedAt: time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC),
	}

	if err := RecordChannelEventCheck(recorder, ChannelEventSnapshot{
		Event:    event,
		Metadata: map[string]any{"phase": "wait_review"},
	}); err != nil {
		t.Fatalf("RecordChannelEventCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "channel-event:event-1" || check.Name != "channel event" || check.Status != VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed channel-event:event-1", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeChannelEvent || check.Evidence[0].Ref != "event-1" {
		t.Fatalf("evidence = %+v, want channel_event event-1", check.Evidence)
	}

	metadata := check.Evidence[0].Metadata
	if metadata["channel"] != "agui" || metadata["event_type"] != "action" || metadata["action_type"] != "resume" {
		t.Fatalf("metadata event identity = %+v, want channel/action identity", metadata)
	}
	if metadata["user_id"] != "user-1" || metadata["session_id"] != "session-1" || metadata["run_id"] != "run-1" || metadata["call_id"] != "call-1" {
		t.Fatalf("metadata runtime ids = %+v, want event runtime ids", metadata)
	}
	if metadata["text_bytes"] != len(event.Text) || metadata["payload_present"] != true {
		t.Fatalf("metadata payload shape = %+v, want text byte count and payload presence", metadata)
	}
	if got, want := metadata["metadata_keys"], []string{"platform"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata keys = %+v, want %+v", got, want)
	}
	if metadata["text"] != nil || metadata["payload"] != nil || metadata["action_payload"] != nil || metadata["event_metadata"] != nil {
		t.Fatalf("metadata leaked raw channel event data = %+v", metadata)
	}
	if check.Metadata["phase"] != "wait_review" {
		t.Fatalf("check metadata = %+v, want custom metadata copied", check.Metadata)
	}
}

func TestRecordChannelEventCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC)
	event := ChannelEvent{
		ID:      "event-1",
		Channel: "lark",
		Type:    ChannelEventAction,
		IDs:     RuntimeIDs{RunID: "run-1", CallID: "call-1"},
		Action: SurfaceAction{
			ID:          "action-1",
			Type:        SurfaceActionSubmit,
			InterruptID: "interrupt-1",
			CallID:      "action-call-1",
			Payload:     map[string]any{"approved": true},
		},
		Text:      "approve",
		Payload:   map[string]any{"approved": true},
		Metadata:  map[string]any{"platform": "lark"},
		CreatedAt: createdAt,
	}

	err := RecordChannelEventCheck(recorder, ChannelEventSnapshot{
		Event: event,
		Metadata: map[string]any{
			"ref":                    "forged-ref",
			"text_bytes":             999,
			"payload_present":        false,
			"action_present":         false,
			"metadata_key_cnt":       999,
			"run_id":                 "forged-run",
			"call_id":                "forged-call",
			"event_id":               "forged-event",
			"channel":                "forged-channel",
			"event_type":             "forged-event-type",
			"created_at":             "forged-created-at",
			"action_id":              "forged-action",
			"action_type":            "forged-action-type",
			"interrupt_id":           "forged-interrupt",
			"action_call_id":         "forged-action-call",
			"action_payload_present": false,
			"phase":                  "wait_review",
		},
	})
	if err != nil {
		t.Fatalf("RecordChannelEventCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "event-1" ||
		metadata["text_bytes"] != len(event.Text) ||
		metadata["payload_present"] != true ||
		metadata["action_present"] != true ||
		metadata["metadata_key_cnt"] != 1 ||
		metadata["run_id"] != "run-1" ||
		metadata["call_id"] != "call-1" ||
		metadata["event_id"] != "event-1" ||
		metadata["channel"] != "lark" ||
		metadata["event_type"] != "action" ||
		metadata["created_at"] != createdAt.Format(time.RFC3339Nano) ||
		metadata["action_id"] != "action-1" ||
		metadata["action_type"] != string(SurfaceActionSubmit) ||
		metadata["interrupt_id"] != "interrupt-1" ||
		metadata["action_call_id"] != "action-call-1" ||
		metadata["action_payload_present"] != true {
		t.Fatalf("metadata = %+v, want canonical channel event fields preserved", metadata)
	}
	if metadata["phase"] != "wait_review" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
}

func TestRecordChannelEventCheckRecordsFailure(t *testing.T) {
	recorder := NewVerificationRecorder()
	boom := errors.New("callback rejected")

	err := RecordChannelEventCheck(recorder, ChannelEventSnapshot{
		Event: ChannelEvent{
			ID:      "event-2",
			Channel: "lark",
			Type:    ChannelEventCancel,
			IDs:     RuntimeIDs{RunID: "run-1", CallID: "event-call-2"},
		},
		Err: boom,
	})
	if !errors.Is(err, ErrChannelEventFailed) || !errors.Is(err, boom) {
		t.Fatalf("RecordChannelEventCheck(failed) error = %v, want ErrChannelEventFailed wrapping boom", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}
	if checks[0].Evidence[0].Metadata["error"] != boom.Error() {
		t.Fatalf("metadata = %+v, want error string", checks[0].Evidence[0].Metadata)
	}
}

func TestRecordChannelEventCheckRejectsMissingEvidence(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordChannelEventCheck(nil, ChannelEventSnapshot{Skipped: true}); err == nil {
		t.Fatal("RecordChannelEventCheck(nil recorder) error = nil, want error")
	}
	if err := RecordChannelEventCheck(recorder, ChannelEventSnapshot{}); !errors.Is(err, ErrChannelEventRequired) {
		t.Fatalf("RecordChannelEventCheck(empty) error = %v, want ErrChannelEventRequired", err)
	}
	if err := RecordChannelEventCheck(recorder, ChannelEventSnapshot{Skipped: true}); err != nil {
		t.Fatalf("RecordChannelEventCheck(skipped) error = %v", err)
	}
}
