package devagent

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gopact-ai/gopact"
)

var ErrReleaseGateStatusRequired = errors.New("devagent: release gate status is required")

const (
	// VerificationCheckReleaseGate is the standard check ID for Dev Agent release gate results.
	VerificationCheckReleaseGate = "release-gate"

	// VerificationEvidenceTypeReleaseGate is the evidence type for Dev Agent release gate decisions.
	VerificationEvidenceTypeReleaseGate = "release_gate"
)

// RecordReleaseGateCheck records an already-evaluated release gate result as verification evidence.
func RecordReleaseGateCheck(recorder *gopact.VerificationRecorder, result GateResult) error {
	if recorder == nil {
		return errors.New("devagent: verification recorder is nil")
	}
	if !result.Status.valid() {
		return ErrReleaseGateStatusRequired
	}

	check := releaseGateCheck(result)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		return ErrReleaseGateRejected
	}
	return nil
}

func releaseGateCheck(result GateResult) gopact.VerificationCheck {
	status := gopact.VerificationStatusPassed
	switch result.Status {
	case GateSkipped, GatePending:
		status = gopact.VerificationStatusSkipped
	case GateRejected:
		status = gopact.VerificationStatusFailed
	}

	return gopact.VerificationCheck{
		ID:      VerificationCheckReleaseGate,
		Name:    "release gate",
		Status:  status,
		Summary: releaseGateSummary(result),
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeReleaseGate,
				Ref:      releaseGateRef(result),
				Summary:  releaseGateEvidenceSummary(result),
				Metadata: releaseGateEvidenceMetadata(result),
			},
		},
		Metadata: releaseGateCheckMetadata(result),
	}
}

func (s GateStatus) valid() bool {
	switch s {
	case GatePassed, GateRejected, GateSkipped, GatePending:
		return true
	default:
		return false
	}
}

func releaseGateSummary(result GateResult) string {
	switch result.Status {
	case GatePassed:
		return "release gate passed"
	case GateSkipped:
		if len(result.Reasons) > 0 {
			return "release gate skipped: " + strings.Join(result.Reasons, "; ")
		}
		return "release gate skipped"
	case GatePending:
		if len(result.Reasons) > 0 {
			return "release gate pending: " + strings.Join(result.Reasons, "; ")
		}
		return "release gate pending"
	default:
		if len(result.Reasons) > 0 {
			return "release gate rejected: " + strings.Join(result.Reasons, "; ")
		}
		return "release gate rejected"
	}
}

func releaseGateEvidenceSummary(result GateResult) string {
	if len(result.Reasons) > 0 {
		return strings.Join(result.Reasons, "; ")
	}
	return string(result.Status)
}

func releaseGateCheckMetadata(result GateResult) map[string]any {
	metadata := releaseGateBaseMetadata(result)
	if keys := releaseGateSupplementalMetadataKeys(result.Metadata); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeReleaseGateSupplementalMetadata(metadata, result.Metadata)
	return metadata
}

func releaseGateEvidenceMetadata(result GateResult) map[string]any {
	metadata := releaseGateBaseMetadata(result)
	if keys := releaseGateSupplementalMetadataKeys(result.Metadata); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeReleaseGateSupplementalMetadata(metadata, result.Metadata)
	return metadata
}

func releaseGateBaseMetadata(result GateResult) map[string]any {
	metadata := map[string]any{
		"gate_status": string(result.Status),
		"ref":         releaseGateRef(result),
	}
	if result.Mode != "" {
		metadata["mode"] = string(result.Mode)
	}
	if result.ReportStatus != "" {
		metadata["report_status"] = string(result.ReportStatus)
	}
	if result.MaxEntropySeverity != "" {
		metadata["max_entropy_severity"] = string(result.MaxEntropySeverity)
	}
	if result.ReviewStatus != "" {
		metadata["review_status"] = string(result.ReviewStatus)
	}
	if len(result.Reasons) > 0 {
		metadata["reasons"] = append([]string(nil), result.Reasons...)
	}
	return metadata
}

func releaseGateReservedMetadataKey(key string) bool {
	switch key {
	case "gate_status",
		"ref",
		"mode",
		"report_status",
		"max_entropy_severity",
		"review_status",
		"reasons",
		"metadata_keys":
		return true
	default:
		return false
	}
}

func releaseGateSupplementalMetadataKeys(supplemental map[string]any) []string {
	if len(supplemental) == 0 {
		return nil
	}
	keys := make([]string, 0, len(supplemental))
	for key := range supplemental {
		if releaseGateReservedMetadataKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mergeReleaseGateSupplementalMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if releaseGateReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func releaseGateRef(result GateResult) string {
	if result.Mode != "" {
		return fmt.Sprintf("%s:%s", VerificationCheckReleaseGate, result.Mode)
	}
	return VerificationCheckReleaseGate
}
