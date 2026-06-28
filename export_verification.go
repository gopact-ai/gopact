package gopact

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrRunExportFailed     = errors.New("gopact: run export failed")
	ErrRunExportIncomplete = errors.New("gopact: run export incomplete")
)

const (
	// VerificationCheckRunExport is the standard check ID prefix for run exports.
	VerificationCheckRunExport = "run-export"

	// VerificationEvidenceTypeRunExport is the evidence type for run export records.
	VerificationEvidenceTypeRunExport = "run_export"
)

// RecordRunExportCheck records an already-observed run export as verification evidence.
func RecordRunExportCheck(recorder *VerificationRecorder, export RunExport) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if err := export.Validate(); err != nil {
		return err
	}

	check := runExportCheck(export)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		if runExportIncomplete(export) {
			return ErrRunExportIncomplete
		}
		return ErrRunExportFailed
	}
	return nil
}

func runExportCheck(export RunExport) VerificationCheck {
	status := runExportCheckStatus(export)
	return VerificationCheck{
		ID:       VerificationCheckRunExport + ":" + export.IDs.RunID,
		Name:     "run export",
		Status:   status,
		Summary:  runExportCheckSummary(status, export),
		Evidence: runExportEvidence(export),
		Metadata: runExportCheckMetadata(export),
	}
}

func runExportCheckStatus(export RunExport) VerificationStatus {
	switch export.Outcome {
	case RunCompleted:
		if runExportIncomplete(export) {
			return VerificationStatusFailed
		}
		return VerificationStatusPassed
	case RunCanceled, RunInterrupted:
		return VerificationStatusSkipped
	default:
		return VerificationStatusFailed
	}
}

func runExportIncomplete(export RunExport) bool {
	return export.Outcome == RunCompleted && (len(export.Events) == 0 || len(export.Steps) == 0)
}

func runExportCheckSummary(status VerificationStatus, export RunExport) string {
	switch status {
	case VerificationStatusPassed:
		if len(export.Steps) == 1 {
			return "run export completed with 1 step"
		}
		return fmt.Sprintf("run export completed with %d steps", len(export.Steps))
	case VerificationStatusSkipped:
		return "run export " + string(export.Outcome)
	default:
		if runExportIncomplete(export) {
			return "run export incomplete"
		}
		return "run export failed"
	}
}

func runExportEvidence(export RunExport) []VerificationEvidence {
	return []VerificationEvidence{
		{
			Type:     VerificationEvidenceTypeRunExport,
			Ref:      export.IDs.RunID,
			Summary:  runExportEvidenceSummary(export),
			Metadata: runExportEvidenceMetadata(export),
		},
	}
}

func runExportEvidenceSummary(export RunExport) string {
	if len(export.Events) == 1 && len(export.Steps) == 1 {
		return "1 event, 1 step"
	}
	if len(export.Events) == 1 {
		return fmt.Sprintf("1 event, %d steps", len(export.Steps))
	}
	if len(export.Steps) == 1 {
		return fmt.Sprintf("%d events, 1 step", len(export.Events))
	}
	return fmt.Sprintf("%d events, %d steps", len(export.Events), len(export.Steps))
}

func runExportCheckMetadata(export RunExport) map[string]any {
	metadata := runExportBaseMetadata(export)
	if keys := sortedSupplementalVerificationMetadataKeys(export.Metadata, runExportReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeRunExportSupplementalMetadata(metadata, export.Metadata)
	return metadata
}

func runExportEvidenceMetadata(export RunExport) map[string]any {
	metadata := runExportBaseMetadata(export)
	if keys := sortedSupplementalVerificationMetadataKeys(export.Metadata, runExportReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeRunExportSupplementalMetadata(metadata, export.Metadata)
	return metadata
}

func mergeRunExportSupplementalMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if runExportReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func runExportBaseMetadata(export RunExport) map[string]any {
	metadata := map[string]any{
		"ref":                       export.IDs.RunID,
		"run_export_version":        export.Version,
		"outcome":                   string(export.Outcome),
		"event_count":               len(export.Events),
		"step_count":                len(export.Steps),
		"task_count":                len(export.Tasks),
		"input_count":               len(export.Inputs),
		"intervention_count":        len(export.Interventions),
		"failure_count":             len(export.Failures),
		"entropy_audit_count":       len(export.EntropyAudits),
		"verification_report_count": len(export.VerificationReports),
	}
	addRunExportRuntimeIDMetadata(metadata, export.IDs)
	if !export.CreatedAt.IsZero() {
		metadata["created_at"] = export.CreatedAt.Format(time.RFC3339Nano)
	}
	return metadata
}

func addRunExportRuntimeIDMetadata(metadata map[string]any, ids RuntimeIDs) {
	if ids.UserID != "" {
		metadata["user_id"] = ids.UserID
	}
	if ids.SessionID != "" {
		metadata["session_id"] = ids.SessionID
	}
	if ids.ThreadID != "" {
		metadata["thread_id"] = ids.ThreadID
	}
	if ids.RunID != "" {
		metadata["run_id"] = ids.RunID
	}
	if ids.AgentID != "" {
		metadata["agent_id"] = ids.AgentID
	}
	if ids.AppID != "" {
		metadata["app_id"] = ids.AppID
	}
	if ids.CallID != "" {
		metadata["call_id"] = ids.CallID
	}
	if ids.ParentCallID != "" {
		metadata["parent_call_id"] = ids.ParentCallID
	}
	if ids.TraceID != "" {
		metadata["trace_id"] = ids.TraceID
	}
}

func runExportReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"run_export_version",
		"outcome",
		"event_count",
		"step_count",
		"task_count",
		"input_count",
		"intervention_count",
		"failure_count",
		"entropy_audit_count",
		"verification_report_count",
		"metadata_keys",
		"created_at",
		"user_id",
		"session_id",
		"thread_id",
		"run_id",
		"agent_id",
		"app_id",
		"call_id",
		"parent_call_id",
		"trace_id":
		return true
	default:
		return false
	}
}
