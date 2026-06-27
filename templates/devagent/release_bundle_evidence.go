package devagent

import (
	"errors"
	"fmt"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// VerificationCheckReleaseBundle is the standard check ID prefix for Dev Agent release bundles.
	VerificationCheckReleaseBundle = "release-bundle"

	// VerificationEvidenceTypeReleaseBundle is the evidence type for Dev Agent release bundles.
	VerificationEvidenceTypeReleaseBundle = "release_bundle"
)

// RecordReleaseBundleCheck records a validated release bundle as verification evidence.
func RecordReleaseBundleCheck(recorder *gopact.VerificationRecorder, bundle ReleaseBundle) error {
	if recorder == nil {
		return errors.New("devagent: verification recorder is nil")
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	return recorder.Record(releaseBundleCheck(bundle))
}

func releaseBundleCheck(bundle ReleaseBundle) gopact.VerificationCheck {
	return gopact.VerificationCheck{
		ID:      releaseBundleRef(bundle),
		Name:    "release bundle",
		Status:  gopact.VerificationStatusPassed,
		Summary: "release bundle passed",
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeReleaseBundle,
				Ref:      releaseBundleRef(bundle),
				Summary:  releaseBundleEvidenceSummary(bundle),
				Metadata: releaseBundleEvidenceMetadata(bundle),
			},
		},
		Metadata: releaseBundleCheckMetadata(bundle),
	}
}

func releaseBundleEvidenceSummary(bundle ReleaseBundle) string {
	return fmt.Sprintf("%d checks, %d entropy audits", len(bundle.VerificationReport.Checks), len(bundle.EntropyAudits))
}

func releaseBundleCheckMetadata(bundle ReleaseBundle) map[string]any {
	metadata := releaseBundleBaseMetadata(bundle)
	mergeReleaseBundleSupplementalMetadata(metadata, bundle.Metadata)
	return metadata
}

func releaseBundleEvidenceMetadata(bundle ReleaseBundle) map[string]any {
	metadata := releaseBundleBaseMetadata(bundle)
	mergeReleaseBundleSupplementalMetadata(metadata, bundle.Metadata)
	return metadata
}

func releaseBundleBaseMetadata(bundle ReleaseBundle) map[string]any {
	metadata := map[string]any{
		"ref":                        releaseBundleRef(bundle),
		"version":                    bundle.Version,
		"mode":                       string(bundle.Mode),
		"outcome":                    string(bundle.Outcome),
		"action":                     string(bundle.Action.Action),
		"action_status":              string(bundle.Action.Status),
		"report_status":              string(bundle.VerificationReport.Status),
		"gate_status":                string(bundle.Gate.Status),
		"check_count":                len(bundle.VerificationReport.Checks),
		"entropy_audit_count":        len(bundle.EntropyAudits),
		"process_input_count":        len(bundle.Process.Inputs),
		"process_intervention_count": len(bundle.Process.Interventions),
	}
	addReleaseBundleRuntimeIDMetadata(metadata, bundle.IDs)
	if bundle.Review.Status != ReviewUnknown {
		metadata["review_status"] = string(bundle.Review.Status)
	}
	if bundle.Review.Reviewer != "" {
		metadata["reviewer"] = bundle.Review.Reviewer
	}
	if bundle.Gate.ReportStatus != "" {
		metadata["gate_report_status"] = string(bundle.Gate.ReportStatus)
	}
	if bundle.Gate.ReviewStatus != ReviewUnknown {
		metadata["gate_review_status"] = string(bundle.Gate.ReviewStatus)
	}
	if bundle.Gate.MaxEntropySeverity != "" {
		metadata["max_entropy_severity"] = string(bundle.Gate.MaxEntropySeverity)
	}
	if bundle.Process.Task.ID != "" {
		metadata["process_task_id"] = bundle.Process.Task.ID
	}
	if releaseGateInputID := releaseGateProcessInputID(bundle.Process.Inputs); releaseGateInputID != "" {
		metadata["release_gate_input_id"] = releaseGateInputID
	}
	if reviewInterventionID := reviewProcessInterventionID(bundle.Process.Interventions); reviewInterventionID != "" {
		metadata["review_intervention_id"] = reviewInterventionID
	}
	if len(bundle.RequiredCheckIDs) > 0 {
		metadata["required_check_ids"] = copyStringSlice(bundle.RequiredCheckIDs)
	}
	if len(bundle.RequiredEvidenceTypes) > 0 {
		metadata["required_evidence_types"] = copyStringSlice(bundle.RequiredEvidenceTypes)
	}
	if len(bundle.RequiredCIGates) > 0 {
		metadata["required_ci_gates"] = copyStringSlice(bundle.RequiredCIGates)
	}
	if !bundle.CreatedAt.IsZero() {
		metadata["created_at"] = bundle.CreatedAt.Format(time.RFC3339Nano)
	}
	return metadata
}

func releaseBundleReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"version",
		"mode",
		"outcome",
		"action",
		"action_status",
		"report_status",
		"gate_status",
		"check_count",
		"entropy_audit_count",
		"process_input_count",
		"process_intervention_count",
		"user_id",
		"session_id",
		"thread_id",
		"run_id",
		"agent_id",
		"app_id",
		"call_id",
		"parent_call_id",
		"trace_id",
		"review_status",
		"reviewer",
		"gate_report_status",
		"gate_review_status",
		"max_entropy_severity",
		"process_task_id",
		"release_gate_input_id",
		"review_intervention_id",
		"required_check_ids",
		"required_evidence_types",
		"required_ci_gates",
		"created_at":
		return true
	default:
		return false
	}
}

func mergeReleaseBundleSupplementalMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if releaseBundleReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func releaseBundleRef(bundle ReleaseBundle) string {
	return fmt.Sprintf("%s:%s", VerificationCheckReleaseBundle, bundle.IDs.RunID)
}

func releaseGateProcessInputID(inputs []gopact.InputRecord) string {
	for _, input := range inputs {
		if input.Source == "devagent.release_gate" {
			return input.ID
		}
	}
	return ""
}

func reviewProcessInterventionID(interventions []gopact.InterventionRecord) string {
	for _, intervention := range interventions {
		if intervention.Type == gopact.InterruptApproval {
			return intervention.ID
		}
	}
	return ""
}

func addReleaseBundleRuntimeIDMetadata(metadata map[string]any, ids gopact.RuntimeIDs) {
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
