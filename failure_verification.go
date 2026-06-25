package gopact

import (
	"errors"
	"time"
)

var ErrFailureAttributionFailed = errors.New("gopact: failure attribution recorded")

const (
	// VerificationCheckFailureAttribution is the standard check ID prefix for failure attributions.
	VerificationCheckFailureAttribution = "failure-attribution"

	// VerificationEvidenceTypeFailureAttribution is the evidence type for failure attribution records.
	VerificationEvidenceTypeFailureAttribution = "failure_attribution"
)

// RecordFailureAttributionCheck records an already-observed failure attribution
// as failed verification evidence. It does not derive or infer attribution.
func RecordFailureAttributionCheck(recorder *VerificationRecorder, attribution FailureAttribution) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if err := attribution.Validate(); err != nil {
		return err
	}
	if err := recorder.Record(failureAttributionCheck(attribution)); err != nil {
		return err
	}
	return ErrFailureAttributionFailed
}

func failureAttributionCheck(attribution FailureAttribution) VerificationCheck {
	return VerificationCheck{
		ID:       VerificationCheckFailureAttribution + ":" + attribution.ID,
		Name:     "failure attribution",
		Status:   VerificationStatusFailed,
		Summary:  failureAttributionSummary(attribution),
		Evidence: failureAttributionEvidence(attribution),
		Metadata: failureAttributionCheckMetadata(attribution),
	}
}

func failureAttributionSummary(attribution FailureAttribution) string {
	if attribution.Summary != "" {
		return attribution.Summary
	}
	if attribution.Error != "" {
		return attribution.Error
	}
	return "failure attributed"
}

func failureAttributionEvidence(attribution FailureAttribution) []VerificationEvidence {
	evidence := []VerificationEvidence{
		{
			Type:     VerificationEvidenceTypeFailureAttribution,
			Ref:      attribution.ID,
			Summary:  failureAttributionEvidenceSummary(attribution),
			Metadata: failureAttributionEvidenceMetadata(attribution),
		},
	}
	evidence = append(evidence, copyVerificationEvidence(attribution.Evidence)...)
	return evidence
}

func failureAttributionEvidenceSummary(attribution FailureAttribution) string {
	if attribution.Error != "" {
		return attribution.Error
	}
	if attribution.Summary != "" {
		return attribution.Summary
	}
	return string(attribution.Kind)
}

func failureAttributionCheckMetadata(attribution FailureAttribution) map[string]any {
	return failureAttributionMergedMetadata(attribution)
}

func failureAttributionEvidenceMetadata(attribution FailureAttribution) map[string]any {
	return failureAttributionMergedMetadata(attribution)
}

func failureAttributionMergedMetadata(attribution FailureAttribution) map[string]any {
	metadata := map[string]any{
		"kind":           string(attribution.Kind),
		"evidence_count": len(attribution.Evidence),
	}
	addRunExportRuntimeIDMetadata(metadata, attribution.IDs)
	if attribution.Node != "" {
		metadata["node"] = attribution.Node
	}
	if attribution.Step > 0 {
		metadata["step"] = attribution.Step
	}
	if attribution.Summary != "" {
		metadata["summary"] = attribution.Summary
	}
	if attribution.Error != "" {
		metadata["error"] = attribution.Error
	}
	if !attribution.CreatedAt.IsZero() {
		metadata["created_at"] = attribution.CreatedAt.Format(time.RFC3339Nano)
	}
	mergeSupplementalVerificationMetadata(metadata, attribution.Metadata, failureAttributionReservedMetadataKey)
	return metadata
}

func failureAttributionReservedMetadataKey(key string) bool {
	switch key {
	case "kind",
		"evidence_count",
		"user_id",
		"session_id",
		"thread_id",
		"run_id",
		"agent_id",
		"app_id",
		"call_id",
		"parent_call_id",
		"trace_id",
		"node",
		"step",
		"summary",
		"error",
		"created_at":
		return true
	default:
		return false
	}
}
