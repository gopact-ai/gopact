package gopact

import (
	"errors"
	"testing"
	"time"
)

func TestRecordFailureAttributionCheckRecordsFailedCheck(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	attribution := FailureAttribution{
		ID:   "failure-1",
		Kind: FailureTool,
		IDs: RuntimeIDs{
			ThreadID: "thread-1",
			RunID:    "run-1",
			CallID:   "tool-call-1",
			TraceID:  "trace-1",
		},
		Node:    "call_tool",
		Step:    3,
		Summary: "tool failed",
		Error:   "exit status 1",
		Evidence: []VerificationEvidence{
			{Type: VerificationEvidenceTypeToolCall, Ref: "tool-call-1", Summary: "tool crashed"},
		},
		CreatedAt: createdAt,
		Metadata:  map[string]any{"owner": "tools"},
	}

	err := RecordFailureAttributionCheck(recorder, attribution)
	if !errors.Is(err, ErrFailureAttributionFailed) {
		t.Fatalf("RecordFailureAttributionCheck() error = %v, want ErrFailureAttributionFailed", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "failure-attribution:failure-1" || check.Name != "failure attribution" || check.Status != VerificationStatusFailed {
		t.Fatalf("check = %+v, want failed failure-attribution:failure-1", check)
	}
	if len(check.Evidence) != 2 ||
		check.Evidence[0].Type != VerificationEvidenceTypeFailureAttribution ||
		check.Evidence[0].Ref != "failure-1" ||
		check.Evidence[1].Type != VerificationEvidenceTypeToolCall {
		t.Fatalf("evidence = %+v, want failure attribution plus original evidence", check.Evidence)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["kind"] != string(FailureTool) || metadata["node"] != "call_tool" || metadata["step"] != 3 {
		t.Fatalf("metadata = %+v, want kind/node/step", metadata)
	}
	if metadata["run_id"] != "run-1" || metadata["call_id"] != "tool-call-1" || metadata["evidence_count"] != 1 {
		t.Fatalf("metadata = %+v, want runtime ids and evidence count", metadata)
	}
	if metadata["owner"] != "tools" {
		t.Fatalf("metadata = %+v, want attribution metadata copied into evidence metadata", metadata)
	}
	if check.Metadata["owner"] != "tools" {
		t.Fatalf("check metadata = %+v, want attribution metadata copied", check.Metadata)
	}
	attribution.Metadata["owner"] = "mutated"
	if checks := recorder.Checks(); checks[0].Metadata["owner"] != "tools" ||
		checks[0].Evidence[0].Metadata["owner"] != "tools" {
		t.Fatalf("recorded metadata mutated = %+v", checks[0])
	}
}

func TestRecordFailureAttributionCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	err := RecordFailureAttributionCheck(recorder, FailureAttribution{
		ID:   "failure-1",
		Kind: FailureTool,
		IDs: RuntimeIDs{
			RunID:  "run-1",
			CallID: "tool-call-1",
		},
		Node:      "call_tool",
		Step:      3,
		Summary:   "tool failed",
		Error:     "exit status 1",
		CreatedAt: createdAt,
		Metadata: map[string]any{
			"kind":           string(FailureRuntime),
			"evidence_count": 99,
			"run_id":         "forged-run",
			"call_id":        "forged-call",
			"node":           "runtime",
			"step":           99,
			"summary":        "forged summary",
			"error":          "forged error",
			"created_at":     "forged-time",
			"owner":          "tools",
		},
	})
	if !errors.Is(err, ErrFailureAttributionFailed) {
		t.Fatalf("RecordFailureAttributionCheck() error = %v, want ErrFailureAttributionFailed", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["kind"] != string(FailureTool) ||
		metadata["evidence_count"] != 0 ||
		metadata["run_id"] != "run-1" ||
		metadata["call_id"] != "tool-call-1" ||
		metadata["node"] != "call_tool" ||
		metadata["step"] != 3 ||
		metadata["summary"] != "tool failed" ||
		metadata["error"] != "exit status 1" ||
		metadata["created_at"] != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("metadata = %+v, want canonical failure attribution fields preserved", metadata)
	}
	if metadata["owner"] != "tools" {
		t.Fatalf("metadata = %+v, want non-conflicting caller metadata preserved", metadata)
	}
}

func TestRecordFailureAttributionCheckRejectsInvalidInput(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordFailureAttributionCheck(nil, FailureAttribution{
		ID:      "failure-1",
		Kind:    FailureRuntime,
		Summary: "failed",
	}); err == nil {
		t.Fatal("RecordFailureAttributionCheck(nil recorder) error = nil, want error")
	}
	if err := RecordFailureAttributionCheck(recorder, FailureAttribution{
		ID:      "failure-1",
		Summary: "failed",
	}); err == nil {
		t.Fatal("RecordFailureAttributionCheck(invalid kind) error = nil, want validation error")
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("checks = %+v, want no check for invalid attribution", recorder.Checks())
	}
}
