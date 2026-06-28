package gopact

import (
	"errors"
	"fmt"
)

var (
	ErrToolCallFailed   = errors.New("gopact: tool call failed")
	ErrToolCallRequired = errors.New("gopact: tool call evidence is required")
)

const (
	// VerificationCheckToolCall is the standard check ID prefix for observed tool calls.
	VerificationCheckToolCall = "tool-call"

	// VerificationEvidenceTypeToolCall is the evidence type for observed tool calls.
	VerificationEvidenceTypeToolCall = "tool_call"
)

// ToolCallSnapshot is an already-observed tool call summary. It records call
// identity, payload shape, result shape, and error metadata, not raw arguments
// or raw result content.
type ToolCallSnapshot struct {
	ID       string
	Name     string
	Ref      string
	IDs      RuntimeIDs
	Call     ToolCall
	Result   ToolResult
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordToolCallCheck records an already-observed tool call as verification evidence.
func RecordToolCallCheck(recorder *VerificationRecorder, snapshot ToolCallSnapshot) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if !snapshot.Skipped && !toolCallHasRef(snapshot) {
		return ErrToolCallRequired
	}

	check := toolCallCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrToolCallFailed, snapshot.Err)
		}
		return ErrToolCallFailed
	}
	return nil
}

func toolCallCheck(snapshot ToolCallSnapshot) VerificationCheck {
	ref := toolCallRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckToolCall + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "tool call"
	}
	status := toolCallStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = toolCallSummary(status, snapshot)
	}
	return VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeToolCall,
				Ref:      ref,
				Summary:  toolCallEvidenceSummary(status, snapshot),
				Metadata: toolCallEvidenceMetadata(snapshot),
			},
		},
		Metadata: toolCallCheckMetadata(snapshot),
	}
}

func toolCallStatus(snapshot ToolCallSnapshot) VerificationStatus {
	if snapshot.Skipped {
		return VerificationStatusSkipped
	}
	if snapshot.Err != nil {
		return VerificationStatusFailed
	}
	return VerificationStatusPassed
}

func toolCallSummary(status VerificationStatus, snapshot ToolCallSnapshot) string {
	switch status {
	case VerificationStatusSkipped:
		return "tool call skipped"
	case VerificationStatusFailed:
		if snapshot.Err != nil {
			return "tool call failed: " + snapshot.Err.Error()
		}
		return "tool call failed"
	default:
		if snapshot.Call.Name != "" {
			return "tool call completed: " + snapshot.Call.Name
		}
		return "tool call completed"
	}
}

func toolCallEvidenceSummary(status VerificationStatus, snapshot ToolCallSnapshot) string {
	if status == VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if snapshot.Call.Name != "" {
		return fmt.Sprintf("%s returned %d bytes", snapshot.Call.Name, len(snapshot.Result.Content))
	}
	return "tool call captured"
}

func toolCallCheckMetadata(snapshot ToolCallSnapshot) map[string]any {
	metadata := toolCallBaseMetadata(snapshot)
	mergeSupplementalVerificationMetadata(metadata, snapshot.Metadata, toolCallReservedMetadataKey)
	return metadata
}

func toolCallEvidenceMetadata(snapshot ToolCallSnapshot) map[string]any {
	return toolCallCheckMetadata(snapshot)
}

func toolCallBaseMetadata(snapshot ToolCallSnapshot) map[string]any {
	metadata := map[string]any{
		"ref":                  toolCallRef(snapshot),
		"argument_bytes":       len(snapshot.Call.Arguments),
		"result_content_bytes": len(snapshot.Result.Content),
		"artifact_count":       len(snapshot.Result.Artifacts),
		"effect_count":         len(snapshot.Result.Effects),
		"event_count":          len(snapshot.Result.Events),
	}
	addRunExportRuntimeIDMetadata(metadata, snapshot.IDs)
	if snapshot.Call.ID != "" {
		metadata["tool_call_id"] = snapshot.Call.ID
	}
	if snapshot.Call.Name != "" {
		metadata["tool_name"] = snapshot.Call.Name
	}
	if len(snapshot.Result.Metadata) > 0 {
		metadata["result_metadata"] = copyAnyMap(snapshot.Result.Metadata)
		metadata["result_metadata_keys"] = sortedAnyMapKeys(snapshot.Result.Metadata)
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func toolCallReservedMetadataKey(key string) bool {
	if runtimeIDVerificationMetadataKey(key) {
		return true
	}
	switch key {
	case "ref",
		"argument_bytes",
		"result_content_bytes",
		"artifact_count",
		"effect_count",
		"event_count",
		"tool_call_id",
		"tool_name",
		"result_metadata",
		"result_metadata_keys",
		"error",
		"skipped":
		return true
	default:
		return false
	}
}

func toolCallHasRef(snapshot ToolCallSnapshot) bool {
	if snapshot.Ref != "" || snapshot.ID != "" {
		return true
	}
	if snapshot.Call.ID != "" || snapshot.Call.Name != "" {
		return true
	}
	return snapshot.IDs.CallID != "" || snapshot.IDs.RunID != ""
}

func toolCallRef(snapshot ToolCallSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.Call.ID != "" {
		return snapshot.Call.ID
	}
	if snapshot.IDs.CallID != "" {
		return snapshot.IDs.CallID
	}
	if snapshot.Call.Name != "" {
		return snapshot.Call.Name
	}
	if snapshot.IDs.RunID != "" {
		return snapshot.IDs.RunID + ":tool"
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckToolCall
}
