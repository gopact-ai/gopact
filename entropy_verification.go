package gopact

import (
	"errors"
	"fmt"
	"time"
)

var ErrEntropyAuditFailed = errors.New("gopact: entropy audit failed")

const (
	// VerificationCheckEntropyAudit is the standard check ID prefix for entropy audits.
	VerificationCheckEntropyAudit = "entropy-audit"

	// VerificationEvidenceTypeEntropyAudit is the evidence type for entropy audit records.
	VerificationEvidenceTypeEntropyAudit = "entropy_audit"
)

// RecordEntropyAuditCheck records an already-observed entropy audit as verification evidence.
func RecordEntropyAuditCheck(recorder *VerificationRecorder, audit EntropyAudit) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if err := audit.Validate(); err != nil {
		return err
	}

	check := entropyAuditCheck(audit)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		return ErrEntropyAuditFailed
	}
	return nil
}

func entropyAuditCheck(audit EntropyAudit) VerificationCheck {
	status := entropyAuditCheckStatus(audit.Status)
	return VerificationCheck{
		ID:       VerificationCheckEntropyAudit + ":" + audit.ID,
		Name:     "entropy audit",
		Status:   status,
		Summary:  entropyAuditCheckSummary(status, audit),
		Evidence: entropyAuditEvidence(status, audit),
		Metadata: entropyAuditCheckMetadata(audit),
	}
}

func entropyAuditCheckStatus(status VerificationStatus) VerificationStatus {
	switch status {
	case VerificationStatusFailed:
		return VerificationStatusFailed
	case VerificationStatusSkipped:
		return VerificationStatusSkipped
	default:
		return VerificationStatusPassed
	}
}

func entropyAuditCheckSummary(status VerificationStatus, audit EntropyAudit) string {
	switch status {
	case VerificationStatusSkipped:
		return "entropy audit skipped"
	case VerificationStatusFailed:
		return "entropy audit failed"
	default:
		if len(audit.Findings) == 1 {
			return "entropy audit completed with 1 finding"
		}
		if len(audit.Findings) > 1 {
			return fmt.Sprintf("entropy audit completed with %d findings", len(audit.Findings))
		}
		return "entropy audit passed"
	}
}

func entropyAuditEvidence(status VerificationStatus, audit EntropyAudit) []VerificationEvidence {
	if status == VerificationStatusSkipped {
		return nil
	}
	return []VerificationEvidence{
		{
			Type:     VerificationEvidenceTypeEntropyAudit,
			Ref:      audit.ID,
			Summary:  entropyAuditEvidenceSummary(audit),
			Metadata: entropyAuditEvidenceMetadata(audit),
		},
	}
}

func entropyAuditEvidenceSummary(audit EntropyAudit) string {
	if len(audit.Findings) == 1 {
		return "1 entropy finding"
	}
	if len(audit.Findings) > 1 {
		return fmt.Sprintf("%d entropy findings", len(audit.Findings))
	}
	return string(audit.Status)
}

func entropyAuditCheckMetadata(audit EntropyAudit) map[string]any {
	metadata := entropyAuditBaseMetadata(audit)
	if keys := sortedSupplementalVerificationMetadataKeys(audit.Metadata, entropyAuditReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeSupplementalVerificationMetadata(metadata, audit.Metadata, entropyAuditReservedMetadataKey)
	return metadata
}

func entropyAuditEvidenceMetadata(audit EntropyAudit) map[string]any {
	return entropyAuditCheckMetadata(audit)
}

func entropyAuditBaseMetadata(audit EntropyAudit) map[string]any {
	metadata := map[string]any{
		"ref":           audit.ID,
		"audit_id":      audit.ID,
		"audit_status":  string(audit.Status),
		"finding_count": len(audit.Findings),
	}
	addEntropyRuntimeIDMetadata(metadata, audit.IDs)
	if !audit.CreatedAt.IsZero() {
		metadata["created_at"] = audit.CreatedAt.Format(time.RFC3339Nano)
	}
	if maxSeverity := maxEntropySeverity(audit.Findings); maxSeverity != "" {
		metadata["max_entropy_severity"] = string(maxSeverity)
	}
	if len(audit.Findings) > 0 {
		metadata["findings"] = entropyFindingMetadata(audit.Findings)
	}
	return metadata
}

func entropyAuditReservedMetadataKey(key string) bool {
	if runtimeIDVerificationMetadataKey(key) {
		return true
	}
	switch key {
	case "ref",
		"audit_id",
		"audit_status",
		"finding_count",
		"created_at",
		"max_entropy_severity",
		"findings",
		"metadata_keys":
		return true
	default:
		return false
	}
}

func addEntropyRuntimeIDMetadata(metadata map[string]any, ids RuntimeIDs) {
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
	if ids.TraceID != "" {
		metadata["trace_id"] = ids.TraceID
	}
}

func maxEntropySeverity(findings []EntropyFinding) EntropySeverity {
	var maxSeverity EntropySeverity
	for _, finding := range findings {
		if entropySeverityRank(finding.Severity) > entropySeverityRank(maxSeverity) {
			maxSeverity = finding.Severity
		}
	}
	return maxSeverity
}

func entropySeverityRank(severity EntropySeverity) int {
	switch severity {
	case EntropySeverityLow:
		return 1
	case EntropySeverityMedium:
		return 2
	case EntropySeverityHigh:
		return 3
	case EntropySeverityCritical:
		return 4
	default:
		return 0
	}
}

func entropyFindingMetadata(findings []EntropyFinding) []map[string]any {
	out := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		metadata := map[string]any{
			"id":       finding.ID,
			"category": string(finding.Category),
			"severity": string(finding.Severity),
		}
		if finding.Summary != "" {
			metadata["summary"] = finding.Summary
		}
		if !finding.CreatedAt.IsZero() {
			metadata["created_at"] = finding.CreatedAt.Format(time.RFC3339Nano)
		}
		if len(finding.Evidence) > 0 {
			metadata["evidence_count"] = len(finding.Evidence)
		}
		out = append(out, metadata)
	}
	return out
}
