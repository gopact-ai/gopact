package gopacttest

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	ErrCIGateFailed   = errors.New("gopacttest: CI gate failed")
	ErrCIGateRequired = errors.New("gopacttest: CI gate is required")
)

const (
	// VerificationCheckCIGates is the standard check ID for aggregated CI gates.
	VerificationCheckCIGates = "ci-gates"

	// VerificationEvidenceTypeCIGate is the evidence type for one observed CI gate.
	VerificationEvidenceTypeCIGate = "ci_gate"
)

// CIGateResult is an already-observed CI gate command result.
type CIGateResult struct {
	Gate     string
	Result   CommandResult
	Metadata map[string]any
}

// CIGateSuite is a collection of already-observed CI gate results.
type CIGateSuite struct {
	ID            string
	Name          string
	RequiredGates []string
	Results       []CIGateResult
	Metadata      map[string]any
}

// RecordCIGateSuiteCheck records already-observed CI gate results as one verification check.
func RecordCIGateSuiteCheck(recorder *gopact.VerificationRecorder, suite CIGateSuite) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if err := validateCIGateSuite(suite); err != nil {
		return err
	}

	check := ciGateSuiteCheck(suite)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		return ErrCIGateFailed
	}
	return nil
}

func validateCIGateSuite(suite CIGateSuite) error {
	if len(suite.Results) == 0 {
		return fmt.Errorf("%w: results are required", ErrCIGateRequired)
	}
	resultByGate := make(map[string]struct{}, len(suite.Results))
	for i, result := range suite.Results {
		gate := strings.TrimSpace(result.Gate)
		if gate == "" {
			return fmt.Errorf("%w: result %d gate is required", ErrCIGateRequired, i)
		}
		if len(result.Result.Command) == 0 {
			return fmt.Errorf("%w: result %s command is required", ErrCIGateRequired, gate)
		}
		resultByGate[gate] = struct{}{}
	}
	for _, gate := range suite.RequiredGates {
		gate = strings.TrimSpace(gate)
		if gate == "" {
			return fmt.Errorf("%w: required gate is empty", ErrCIGateRequired)
		}
		if _, ok := resultByGate[gate]; !ok {
			return fmt.Errorf("%w: required gate %s is missing", ErrCIGateRequired, gate)
		}
	}
	return nil
}

func ciGateSuiteCheck(suite CIGateSuite) gopact.VerificationCheck {
	id := suite.ID
	if id == "" {
		id = VerificationCheckCIGates
	}
	name := suite.Name
	if name == "" {
		name = "CI gates"
	}
	status, passed, failed, skipped := ciGateSuiteStatus(suite.Results)
	return gopact.VerificationCheck{
		ID:       id,
		Name:     name,
		Status:   status,
		Summary:  ciGateSuiteSummary(status, passed, failed, skipped),
		Evidence: ciGateSuiteEvidence(suite.Results),
		Metadata: ciGateSuiteMetadata(suite, passed, failed, skipped),
	}
}

func ciGateSuiteStatus(results []CIGateResult) (gopact.VerificationStatus, int, int, int) {
	passed := 0
	failed := 0
	skipped := 0
	for _, result := range results {
		switch ciGateStatus(result.Result) {
		case gopact.VerificationStatusFailed:
			failed++
		case gopact.VerificationStatusSkipped:
			skipped++
		default:
			passed++
		}
	}
	if failed > 0 {
		return gopact.VerificationStatusFailed, passed, failed, skipped
	}
	if passed > 0 {
		return gopact.VerificationStatusPassed, passed, failed, skipped
	}
	return gopact.VerificationStatusSkipped, passed, failed, skipped
}

func ciGateStatus(result CommandResult) gopact.VerificationStatus {
	if result.Skipped {
		return gopact.VerificationStatusSkipped
	}
	if result.Err != nil || result.ExitCode != 0 {
		return gopact.VerificationStatusFailed
	}
	return gopact.VerificationStatusPassed
}

func ciGateSuiteSummary(status gopact.VerificationStatus, passed, failed, skipped int) string {
	switch status {
	case gopact.VerificationStatusFailed:
		return fmt.Sprintf("CI gates failed: %d passed, %d failed, %d skipped", passed, failed, skipped)
	case gopact.VerificationStatusSkipped:
		return fmt.Sprintf("CI gates skipped: %d skipped", skipped)
	default:
		return fmt.Sprintf("CI gates passed: %d passed", passed)
	}
}

func ciGateSuiteEvidence(results []CIGateResult) []gopact.VerificationEvidence {
	evidence := make([]gopact.VerificationEvidence, 0, len(results))
	for _, result := range results {
		status := ciGateStatus(result.Result)
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:     VerificationEvidenceTypeCIGate,
			Ref:      ciGateRef(result.Gate),
			Summary:  ciGateEvidenceSummary(result.Gate, status),
			Metadata: ciGateEvidenceMetadata(result, status),
		})
	}
	return evidence
}

func ciGateEvidenceSummary(gate string, status gopact.VerificationStatus) string {
	return fmt.Sprintf("%s gate %s", ciGateName(gate), status)
}

func ciGateSuiteMetadata(suite CIGateSuite, passed, failed, skipped int) map[string]any {
	metadata := map[string]any{
		"gate_count":         len(suite.Results),
		"passed_gate_count":  passed,
		"failed_gate_count":  failed,
		"skipped_gate_count": skipped,
	}
	if len(suite.RequiredGates) > 0 {
		metadata["required_gates"] = append([]string(nil), suite.RequiredGates...)
	}
	mergeSupplementalMetadata(metadata, suite.Metadata, ciGateSuiteReservedMetadataKey)
	return metadata
}

func ciGateEvidenceMetadata(result CIGateResult, status gopact.VerificationStatus) map[string]any {
	metadata := commandEvidenceMetadata(result.Result)
	metadata["gate"] = ciGateName(result.Gate)
	metadata["status"] = string(status)
	mergeSupplementalMetadata(metadata, result.Metadata, ciGateEvidenceReservedMetadataKey)
	return metadata
}

func ciGateSuiteReservedMetadataKey(key string) bool {
	switch key {
	case "gate_count",
		"passed_gate_count",
		"failed_gate_count",
		"skipped_gate_count",
		"required_gates":
		return true
	default:
		return false
	}
}

func ciGateEvidenceReservedMetadataKey(key string) bool {
	switch key {
	case "gate", "status":
		return true
	default:
		return commandReservedMetadataKey(key)
	}
}

func ciGateRef(gate string) string {
	return "ci-gate:" + ciGateName(gate)
}

func ciGateName(gate string) string {
	return strings.TrimSpace(gate)
}
