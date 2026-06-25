package checkpoint

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordVerificationCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC)
	queue := []string{"call_model", "call_tool"}
	recordMetadata := map[string]any{"source": "memory"}
	customMetadata := map[string]any{"mode": "write"}

	if err := RecordVerificationCheck(recorder, VerificationSnapshot{
		ID:   "checkpoint-check-1",
		Name: "checkpoint persisted",
		Record: Record{
			ID:            "thread-1:2:1",
			SchemaVersion: SchemaVersion,
			IDs: gopact.RuntimeIDs{
				UserID:    "user-1",
				SessionID: "session-1",
				ThreadID:  "thread-1",
				RunID:     "run-1",
				AgentID:   "agent-1",
				CallID:    "call-1",
				TraceID:   "trace-1",
			},
			ThreadID:      "thread-1",
			Step:          2,
			Node:          "call_tool",
			Phase:         gopact.StepCompleted,
			State:         []byte(`{"answer":"ok"}`),
			StateCodec:    "json",
			StateHash:     "sha256:abc123",
			Queue:         queue,
			Pending:       &gopact.InterruptRecord{ID: "interrupt-1", Type: gopact.InterruptApproval},
			Effects:       []gopact.EffectRecord{{ID: "effect-1", Artifacts: []gopact.ArtifactRef{{ID: "artifact-1"}}}},
			ConfigVersion: "cfg-1",
			CreatedAt:     createdAt,
			Metadata:      recordMetadata,
		},
		Metadata: customMetadata,
	}); err != nil {
		t.Fatalf("RecordVerificationCheck() error = %v", err)
	}
	queue[0] = "mutated"
	recordMetadata["source"] = "mutated"
	customMetadata["mode"] = "mutated"

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "checkpoint-check-1" || check.Name != "checkpoint persisted" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed checkpoint check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeCheckpoint || check.Evidence[0].Ref != "thread-1:2:1" {
		t.Fatalf("evidence = %+v, want checkpoint evidence", check.Evidence)
	}
	if check.Metadata["checkpoint_id"] != "thread-1:2:1" ||
		check.Metadata["schema_version"] != SchemaVersion ||
		check.Metadata["thread_id"] != "thread-1" ||
		check.Metadata["run_id"] != "run-1" ||
		check.Metadata["step"] != 2 ||
		check.Metadata["node"] != "call_tool" ||
		check.Metadata["phase"] != string(gopact.StepCompleted) ||
		check.Metadata["state_codec"] != "json" ||
		check.Metadata["state_hash"] != "sha256:abc123" ||
		check.Metadata["state_size_bytes"] != 15 ||
		check.Metadata["queue_count"] != 2 ||
		check.Metadata["effect_count"] != 1 ||
		check.Metadata["artifact_count"] != 1 ||
		check.Metadata["pending_interrupt_id"] != "interrupt-1" ||
		check.Metadata["pending_interrupt_type"] != string(gopact.InterruptApproval) ||
		check.Metadata["config_version"] != "cfg-1" ||
		check.Metadata["created_at"] != createdAt.Format(time.RFC3339Nano) ||
		check.Metadata["mode"] != "write" {
		t.Fatalf("metadata = %+v, want checkpoint and custom metadata", check.Metadata)
	}
	gotQueue, ok := check.Metadata["queue"].([]string)
	if !ok || !reflect.DeepEqual(gotQueue, []string{"call_model", "call_tool"}) {
		t.Fatalf("metadata queue = %#v, want copied queue", check.Metadata["queue"])
	}
	gotRecordMetadata, ok := check.Metadata["checkpoint_metadata"].(map[string]any)
	if !ok || gotRecordMetadata["source"] != "memory" {
		t.Fatalf("checkpoint metadata = %#v, want copied record metadata", check.Metadata["checkpoint_metadata"])
	}
}

func TestRecordVerificationCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC)
	record := Record{
		ID:            "thread-1:2:1",
		SchemaVersion: SchemaVersion,
		IDs: gopact.RuntimeIDs{
			ThreadID: "thread-1",
			RunID:    "run-1",
			CallID:   "call-1",
		},
		ThreadID:      "thread-1",
		Step:          2,
		Node:          "call_tool",
		Phase:         gopact.StepCompleted,
		State:         []byte(`{"answer":"ok"}`),
		StateCodec:    "json",
		StateHash:     "sha256:abc123",
		Queue:         []string{"call_model", "call_tool"},
		Pending:       &gopact.InterruptRecord{ID: "interrupt-1", Type: gopact.InterruptApproval},
		Effects:       []gopact.EffectRecord{{ID: "effect-1", Artifacts: []gopact.ArtifactRef{{ID: "artifact-1"}}}},
		ConfigVersion: "cfg-1",
		CreatedAt:     createdAt,
	}

	if err := RecordVerificationCheck(recorder, VerificationSnapshot{
		Record: record,
		Metadata: map[string]any{
			"ref":                    "forged-ref",
			"checkpoint_id":          "forged-checkpoint",
			"schema_version":         "forged-schema",
			"thread_id":              "forged-thread",
			"run_id":                 "forged-run",
			"call_id":                "forged-call",
			"step":                   999,
			"node":                   "forged-node",
			"phase":                  "forged-phase",
			"state_codec":            "forged-codec",
			"state_hash":             "forged-hash",
			"state_size_bytes":       999,
			"queue":                  []string{"forged-queue"},
			"queue_count":            999,
			"pending_interrupt_id":   "forged-interrupt",
			"pending_interrupt_type": "forged-type",
			"effect_count":           999,
			"artifact_count":         999,
			"config_version":         "forged-config",
			"created_at":             "forged-created-at",
			"mode":                   "write",
		},
	}); err != nil {
		t.Fatalf("RecordVerificationCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "thread-1:2:1" ||
		metadata["checkpoint_id"] != "thread-1:2:1" ||
		metadata["schema_version"] != SchemaVersion ||
		metadata["thread_id"] != "thread-1" ||
		metadata["run_id"] != "run-1" ||
		metadata["call_id"] != "call-1" ||
		metadata["step"] != 2 ||
		metadata["node"] != "call_tool" ||
		metadata["phase"] != string(gopact.StepCompleted) ||
		metadata["state_codec"] != "json" ||
		metadata["state_hash"] != "sha256:abc123" ||
		metadata["state_size_bytes"] != len(record.State) ||
		metadata["queue_count"] != 2 ||
		metadata["pending_interrupt_id"] != "interrupt-1" ||
		metadata["pending_interrupt_type"] != string(gopact.InterruptApproval) ||
		metadata["effect_count"] != 1 ||
		metadata["artifact_count"] != 1 ||
		metadata["config_version"] != "cfg-1" ||
		metadata["created_at"] != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("metadata = %+v, want canonical checkpoint fields preserved", metadata)
	}
	if metadata["mode"] != "write" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
}

func TestRecordVerificationCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	checkpointErr := errors.New("checkpoint store unavailable")

	err := RecordVerificationCheck(recorder, VerificationSnapshot{
		Record: Record{ID: "thread-1:2:1"},
		Err:    checkpointErr,
	})
	if !errors.Is(err, ErrVerificationCheckFailed) || !errors.Is(err, checkpointErr) {
		t.Fatalf("RecordVerificationCheck() error = %v, want ErrVerificationCheckFailed and checkpoint error", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["error"] != "checkpoint store unavailable" {
		t.Fatalf("metadata = %+v, want error metadata", check.Metadata)
	}
}

func TestRecordVerificationCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordVerificationCheck(recorder, VerificationSnapshot{
		Ref:     "plan-mode:checkpoint",
		Skipped: true,
		Summary: "checkpoint not produced in plan mode",
	}); err != nil {
		t.Fatalf("RecordVerificationCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped checkpoint check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "plan-mode:checkpoint" {
		t.Fatalf("evidence = %+v, want skipped checkpoint evidence", checks[0].Evidence)
	}
}

func TestRecordVerificationCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordVerificationCheck(nil, VerificationSnapshot{Record: Record{ID: "thread-1:2:1"}}); err == nil {
		t.Fatal("RecordVerificationCheck(nil) error = nil, want error")
	}
	if err := RecordVerificationCheck(recorder, VerificationSnapshot{}); !errors.Is(err, ErrVerificationRecordRequired) {
		t.Fatalf("RecordVerificationCheck(empty checkpoint) error = %v, want ErrVerificationRecordRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
