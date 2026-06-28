package gopact

import (
	"errors"
	"reflect"
	"testing"
)

func TestRecordModelCallCheckRecordsObservedModelCall(t *testing.T) {
	recorder := NewVerificationRecorder()
	request := ModelRequest{
		IDs: RuntimeIDs{
			ThreadID: "thread-1",
			RunID:    "run-1",
			CallID:   "call-model-1",
			TraceID:  "trace-1",
		},
		Model:     "gpt-5-mini",
		RouteHint: "coding-fast",
		Messages: []Message{
			{Role: RoleUser, Content: "do not persist raw prompt"},
		},
		Tools:        []ToolSpec{{Name: "search"}},
		Capabilities: []Capability{CapabilityToolCalling},
		Budget:       Budget{MaxOutputTokens: 256, MaxCostUSD: 0.05},
		Metadata:     map[string]any{"risk": "low", "tenant": "tenant-a"},
	}
	response := ModelResponse{
		Message: Message{
			Role:    RoleAssistant,
			Content: "do not persist raw response",
			ToolCalls: []ToolCall{
				{ID: "tool-call-1", Name: "search"},
			},
		},
		Route: ModelRoute{
			RouteName:     "coding-fast",
			Provider:      "openai",
			Model:         "gpt-5-mini",
			Attempt:       2,
			ConfigVersion: "routes:v1",
		},
		Usage: Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
			CostUSD:      0.01,
		},
		Metadata: map[string]any{"finish_reason": "tool_calls", "safety": "ok"},
	}

	if err := RecordModelCallCheck(recorder, ModelCallSnapshot{
		Request:  request,
		Response: response,
		Metadata: map[string]any{"phase": "call_model"},
	}); err != nil {
		t.Fatalf("RecordModelCallCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "model-call:call-model-1" || check.Name != "model call" || check.Status != VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed model-call:call-model-1", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeModelCall || check.Evidence[0].Ref != "call-model-1" {
		t.Fatalf("evidence = %+v, want model_call call-model-1", check.Evidence)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["message_count"] != 1 || metadata["tool_count"] != 1 || metadata["capability_count"] != 1 {
		t.Fatalf("metadata counts = %+v, want message/tool/capability counts", metadata)
	}
	if metadata["output_tool_call_count"] != 1 || metadata["input_tokens"] != 10 || metadata["total_tokens"] != 30 {
		t.Fatalf("metadata output/usage = %+v, want tool call and usage counts", metadata)
	}
	if metadata["request_text"] != nil || metadata["response_text"] != nil || metadata["messages"] != nil {
		t.Fatalf("metadata leaked raw text = %+v", metadata)
	}
	assertModelStringSliceMetadata(t, metadata, "request_metadata_keys", []string{"risk", "tenant"})
	assertModelStringSliceMetadata(t, metadata, "response_metadata_keys", []string{"finish_reason", "safety"})
	assertModelStringSliceMetadata(
		t,
		check.Metadata,
		"request_metadata_keys",
		[]string{"risk", "tenant"},
	)
	assertModelStringSliceMetadata(
		t,
		check.Metadata,
		"response_metadata_keys",
		[]string{"finish_reason", "safety"},
	)
	if check.Metadata["phase"] != "call_model" {
		t.Fatalf("check metadata = %+v, want custom metadata copied", check.Metadata)
	}
}

func assertModelStringSliceMetadata(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()
	if got := metadata[key]; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
	}
}

func TestRecordModelCallCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := NewVerificationRecorder()
	request := ModelRequest{
		IDs: RuntimeIDs{
			RunID:  "run-1",
			CallID: "model-call-1",
		},
		Model: "gpt-5-mini",
		Messages: []Message{
			{Role: RoleUser, Content: "summarize"},
		},
		Tools:        []ToolSpec{{Name: "search"}},
		Capabilities: []Capability{CapabilityToolCalling},
	}
	response := ModelResponse{
		Message: Message{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "tool-call-1", Name: "search"},
			},
		},
		Usage: Usage{InputTokens: 5, OutputTokens: 7, TotalTokens: 12},
	}

	err := RecordModelCallCheck(recorder, ModelCallSnapshot{
		Request:  request,
		Response: response,
		Metadata: map[string]any{
			"ref":                    "forged-ref",
			"message_count":          999,
			"tool_count":             999,
			"capability_count":       999,
			"run_id":                 "forged-run",
			"call_id":                "forged-call",
			"request_model":          "forged-model",
			"output_tool_call_count": 999,
			"input_tokens":           999,
			"total_tokens":           999,
			"phase":                  "call_model",
		},
	})
	if err != nil {
		t.Fatalf("RecordModelCallCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "model-call-1" ||
		metadata["message_count"] != 1 ||
		metadata["tool_count"] != 1 ||
		metadata["capability_count"] != 1 ||
		metadata["run_id"] != "run-1" ||
		metadata["call_id"] != "model-call-1" ||
		metadata["request_model"] != "gpt-5-mini" ||
		metadata["output_tool_call_count"] != 1 ||
		metadata["input_tokens"] != 5 ||
		metadata["total_tokens"] != 12 {
		t.Fatalf("metadata = %+v, want canonical model call fields preserved", metadata)
	}
	if metadata["phase"] != "call_model" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}

	evidenceMetadata := recorder.Checks()[0].Evidence[0].Metadata
	if evidenceMetadata["ref"] != "model-call-1" ||
		evidenceMetadata["message_count"] != 1 ||
		evidenceMetadata["tool_count"] != 1 ||
		evidenceMetadata["capability_count"] != 1 ||
		evidenceMetadata["run_id"] != "run-1" ||
		evidenceMetadata["call_id"] != "model-call-1" ||
		evidenceMetadata["request_model"] != "gpt-5-mini" ||
		evidenceMetadata["output_tool_call_count"] != 1 ||
		evidenceMetadata["input_tokens"] != 5 ||
		evidenceMetadata["total_tokens"] != 12 {
		t.Fatalf("evidence metadata = %+v, want canonical model call fields preserved", evidenceMetadata)
	}
	if evidenceMetadata["phase"] != "call_model" {
		t.Fatalf("evidence metadata = %+v, want supplemental metadata preserved", evidenceMetadata)
	}
}

func TestRecordModelCallCheckRecordsFailure(t *testing.T) {
	recorder := NewVerificationRecorder()
	boom := errors.New("rate limited")

	err := RecordModelCallCheck(recorder, ModelCallSnapshot{
		Request: ModelRequest{
			IDs:   RuntimeIDs{RunID: "run-1", CallID: "call-model-2"},
			Model: "gpt-5-mini",
		},
		Err: boom,
	})
	if !errors.Is(err, ErrModelCallFailed) || !errors.Is(err, boom) {
		t.Fatalf("RecordModelCallCheck(failed) error = %v, want ErrModelCallFailed wrapping boom", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}
	if checks[0].Evidence[0].Metadata["error"] != boom.Error() {
		t.Fatalf("metadata = %+v, want error string", checks[0].Evidence[0].Metadata)
	}
}

func TestRecordModelCallCheckRejectsMissingEvidence(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordModelCallCheck(nil, ModelCallSnapshot{Skipped: true}); err == nil {
		t.Fatal("RecordModelCallCheck(nil recorder) error = nil, want error")
	}
	if err := RecordModelCallCheck(recorder, ModelCallSnapshot{}); !errors.Is(err, ErrModelCallRequired) {
		t.Fatalf("RecordModelCallCheck(empty) error = %v, want ErrModelCallRequired", err)
	}
	if err := RecordModelCallCheck(recorder, ModelCallSnapshot{Skipped: true}); err != nil {
		t.Fatalf("RecordModelCallCheck(skipped) error = %v", err)
	}
}
