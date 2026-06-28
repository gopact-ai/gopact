package gopact

import (
	"errors"
	"reflect"
	"testing"
)

func TestRecordToolCallCheckRecordsObservedToolCall(t *testing.T) {
	recorder := NewVerificationRecorder()
	call := ToolCall{
		ID:        "tool-call-1",
		Name:      "repo.search",
		Arguments: []byte(`{"query":"do not persist raw args"}`),
	}
	result := ToolResult{
		Content: "do not persist raw result",
		Artifacts: []ArtifactRef{
			{ID: "artifact-1", URI: "mem://artifact-1", Size: 42},
		},
		Effects: []EffectRecord{
			{ID: "effect-1", Type: "tool_call", Target: "repo.search"},
		},
		Events: []Event{
			{Type: EventToolResult, IDs: RuntimeIDs{RunID: "run-1", CallID: "tool-call-1"}},
		},
		Metadata: map[string]any{"source": "unit", "visibility": "internal"},
	}

	if err := RecordToolCallCheck(recorder, ToolCallSnapshot{
		IDs: RuntimeIDs{
			ThreadID: "thread-1",
			RunID:    "run-1",
			CallID:   "tool-call-1",
			TraceID:  "trace-1",
		},
		Call:     call,
		Result:   result,
		Metadata: map[string]any{"phase": "call_tool"},
	}); err != nil {
		t.Fatalf("RecordToolCallCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "tool-call:tool-call-1" || check.Name != "tool call" || check.Status != VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed tool-call:tool-call-1", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeToolCall || check.Evidence[0].Ref != "tool-call-1" {
		t.Fatalf("evidence = %+v, want tool_call tool-call-1", check.Evidence)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["tool_name"] != "repo.search" || metadata["argument_bytes"] != len(call.Arguments) || metadata["result_content_bytes"] != len(result.Content) {
		t.Fatalf("metadata shape = %+v, want tool name and byte counts", metadata)
	}
	if metadata["artifact_count"] != 1 || metadata["effect_count"] != 1 || metadata["event_count"] != 1 {
		t.Fatalf("metadata counts = %+v, want artifact/effect/event counts", metadata)
	}
	if metadata["arguments"] != nil || metadata["result_content"] != nil || metadata["content"] != nil {
		t.Fatalf("metadata leaked raw payload = %+v", metadata)
	}
	assertToolStringSliceMetadata(t, metadata, "result_metadata_keys", []string{"source", "visibility"})
	assertToolStringSliceMetadata(
		t,
		check.Metadata,
		"result_metadata_keys",
		[]string{"source", "visibility"},
	)
	if check.Metadata["phase"] != "call_tool" {
		t.Fatalf("check metadata = %+v, want custom metadata copied", check.Metadata)
	}
}

func assertToolStringSliceMetadata(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()
	if got := metadata[key]; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
	}
}

func TestRecordToolCallCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := NewVerificationRecorder()
	call := ToolCall{
		ID:        "tool-call-1",
		Name:      "repo.search",
		Arguments: []byte(`{"query":"gopact"}`),
	}
	result := ToolResult{
		Content: "result",
		Artifacts: []ArtifactRef{
			{ID: "artifact-1", URI: "mem://artifact-1"},
		},
		Effects: []EffectRecord{
			{ID: "effect-1", Type: "tool_call"},
		},
		Events: []Event{{Type: EventToolResult}},
	}

	err := RecordToolCallCheck(recorder, ToolCallSnapshot{
		IDs:    RuntimeIDs{RunID: "run-1", CallID: "call-1"},
		Call:   call,
		Result: result,
		Metadata: map[string]any{
			"ref":                  "forged-ref",
			"argument_bytes":       999,
			"result_content_bytes": 999,
			"artifact_count":       999,
			"effect_count":         999,
			"event_count":          999,
			"run_id":               "forged-run",
			"call_id":              "forged-call",
			"tool_call_id":         "forged-tool-call",
			"tool_name":            "forged.tool",
			"phase":                "call_tool",
		},
	})
	if err != nil {
		t.Fatalf("RecordToolCallCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "tool-call-1" ||
		metadata["argument_bytes"] != len(call.Arguments) ||
		metadata["result_content_bytes"] != len(result.Content) ||
		metadata["artifact_count"] != 1 ||
		metadata["effect_count"] != 1 ||
		metadata["event_count"] != 1 ||
		metadata["run_id"] != "run-1" ||
		metadata["call_id"] != "call-1" ||
		metadata["tool_call_id"] != "tool-call-1" ||
		metadata["tool_name"] != "repo.search" {
		t.Fatalf("metadata = %+v, want canonical tool call fields preserved", metadata)
	}
	if metadata["phase"] != "call_tool" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}

	evidenceMetadata := recorder.Checks()[0].Evidence[0].Metadata
	if evidenceMetadata["ref"] != "tool-call-1" ||
		evidenceMetadata["argument_bytes"] != len(call.Arguments) ||
		evidenceMetadata["result_content_bytes"] != len(result.Content) ||
		evidenceMetadata["artifact_count"] != 1 ||
		evidenceMetadata["effect_count"] != 1 ||
		evidenceMetadata["event_count"] != 1 ||
		evidenceMetadata["run_id"] != "run-1" ||
		evidenceMetadata["call_id"] != "call-1" ||
		evidenceMetadata["tool_call_id"] != "tool-call-1" ||
		evidenceMetadata["tool_name"] != "repo.search" {
		t.Fatalf("evidence metadata = %+v, want canonical tool call fields preserved", evidenceMetadata)
	}
	if evidenceMetadata["phase"] != "call_tool" {
		t.Fatalf("evidence metadata = %+v, want supplemental metadata preserved", evidenceMetadata)
	}
}

func TestRecordToolCallCheckRecordsFailure(t *testing.T) {
	recorder := NewVerificationRecorder()
	boom := errors.New("tool crashed")

	err := RecordToolCallCheck(recorder, ToolCallSnapshot{
		IDs:  RuntimeIDs{RunID: "run-1", CallID: "tool-call-2"},
		Call: ToolCall{ID: "tool-call-2", Name: "repo.apply"},
		Err:  boom,
	})
	if !errors.Is(err, ErrToolCallFailed) || !errors.Is(err, boom) {
		t.Fatalf("RecordToolCallCheck(failed) error = %v, want ErrToolCallFailed wrapping boom", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}
	if checks[0].Evidence[0].Metadata["error"] != boom.Error() {
		t.Fatalf("metadata = %+v, want error string", checks[0].Evidence[0].Metadata)
	}
}

func TestRecordToolCallCheckRejectsMissingEvidence(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordToolCallCheck(nil, ToolCallSnapshot{Skipped: true}); err == nil {
		t.Fatal("RecordToolCallCheck(nil recorder) error = nil, want error")
	}
	if err := RecordToolCallCheck(recorder, ToolCallSnapshot{}); !errors.Is(err, ErrToolCallRequired) {
		t.Fatalf("RecordToolCallCheck(empty) error = %v, want ErrToolCallRequired", err)
	}
	if err := RecordToolCallCheck(recorder, ToolCallSnapshot{Skipped: true}); err != nil {
		t.Fatalf("RecordToolCallCheck(skipped) error = %v", err)
	}
}
