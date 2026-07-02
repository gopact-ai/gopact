package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

var ErrVerificationEvidenceConformanceFailed = errors.New("gopacttest: verification evidence conformance failed")

// VerificationEvidenceConformanceHarness describes one VerificationReport under test.
type VerificationEvidenceConformanceHarness struct {
	Report                gopact.VerificationReport
	RequiredCheckIDs      []string
	RequiredEvidenceTypes []string
	RequiredCIGates       []string
}

// VerificationEvidenceRequirement describes one named release/readiness gate requirement.
type VerificationEvidenceRequirement struct {
	Name                  string
	RequiredCheckIDs      []string
	RequiredEvidenceTypes []string
	RequiredCIGates       []string
}

// VerificationEvidenceConformanceResult is the observed result for one verification evidence contract case.
type VerificationEvidenceConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckVerificationEvidenceConformance runs reusable verification report/evidence contract cases.
func CheckVerificationEvidenceConformance(ctx context.Context, harness VerificationEvidenceConformanceHarness) []VerificationEvidenceConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []VerificationEvidenceConformanceResult{failedVerificationEvidenceConformance("context", err)}
	}

	return []VerificationEvidenceConformanceResult{
		checkVerificationReportValid(harness.Report),
		checkVerificationRequiredCheckIDs(harness.Report, harness.RequiredCheckIDs),
		checkVerificationRequiredEvidenceTypes(harness.Report, harness.RequiredEvidenceTypes),
		checkVerificationRequiredCIGates(harness.Report, harness.RequiredCIGates),
	}
}

// CheckVerificationEvidenceRequirements runs reusable verification conformance over a named requirement set.
func CheckVerificationEvidenceRequirements(
	ctx context.Context,
	report gopact.VerificationReport,
	requirements []VerificationEvidenceRequirement,
) []VerificationEvidenceConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []VerificationEvidenceConformanceResult{failedVerificationEvidenceConformance("context", err)}
	}
	if len(requirements) == 0 {
		return CheckVerificationEvidenceConformance(ctx, VerificationEvidenceConformanceHarness{Report: report})
	}

	var results []VerificationEvidenceConformanceResult
	for i, requirement := range requirements {
		name := verificationEvidenceRequirementName(i, requirement.Name)
		evidenceReport := verificationReportScopedToChecks(report, requirement.RequiredCheckIDs)
		requirementResults := []VerificationEvidenceConformanceResult{
			checkVerificationReportValid(report),
			checkVerificationRequiredCheckIDs(report, requirement.RequiredCheckIDs),
			checkVerificationRequiredEvidenceTypes(evidenceReport, requirement.RequiredEvidenceTypes),
			checkVerificationRequiredCIGates(evidenceReport, requirement.RequiredCIGates),
		}
		for _, result := range requirementResults {
			result.Case = name + "/" + result.Case
			results = append(results, result)
		}
	}
	return results
}

// RequireVerificationEvidenceRequirements fails the test unless report satisfies every named requirement.
func RequireVerificationEvidenceRequirements(
	t testing.TB,
	report gopact.VerificationReport,
	requirements []VerificationEvidenceRequirement,
) {
	t.Helper()

	for _, result := range CheckVerificationEvidenceRequirements(context.Background(), report, requirements) {
		if !result.Passed {
			t.Fatalf("verification evidence requirement case %q failed: %v", result.Case, result.Err)
		}
	}
}

// RequireVerificationEvidenceConformance fails the test unless report satisfies the verification evidence contract.
func RequireVerificationEvidenceConformance(t testing.TB, harness VerificationEvidenceConformanceHarness) {
	t.Helper()

	for _, result := range CheckVerificationEvidenceConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("verification evidence conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkVerificationReportValid(report gopact.VerificationReport) VerificationEvidenceConformanceResult {
	if err := report.Validate(); err != nil {
		return failedVerificationEvidenceConformance("valid-report", err)
	}
	return passedVerificationEvidenceConformance("valid-report")
}

func checkVerificationRequiredCheckIDs(report gopact.VerificationReport, required []string) VerificationEvidenceConformanceResult {
	for _, id := range required {
		if id == "" {
			return failedVerificationEvidenceConformance("required-check-ids", errors.New("required check id is empty"))
		}
		check, ok := verificationReportCheckByID(report, id)
		if !ok {
			return failedVerificationEvidenceConformance("required-check-ids", fmt.Errorf("missing required check id %q", id))
		}
		switch check.Status {
		case gopact.VerificationStatusPassed:
		case gopact.VerificationStatusFailed:
			return failedVerificationEvidenceConformance("required-check-ids", fmt.Errorf("required check id %q failed", id))
		case gopact.VerificationStatusSkipped:
			return failedVerificationEvidenceConformance("required-check-ids", fmt.Errorf("required check id %q skipped", id))
		default:
			return failedVerificationEvidenceConformance(
				"required-check-ids",
				fmt.Errorf("required check id %q status %s is not passed", id, check.Status),
			)
		}
	}
	return passedVerificationEvidenceConformance("required-check-ids")
}

func checkVerificationRequiredEvidenceTypes(report gopact.VerificationReport, required []string) VerificationEvidenceConformanceResult {
	types := verificationReportEvidenceTypes(report)
	for _, evidenceType := range required {
		if evidenceType == "" {
			return failedVerificationEvidenceConformance("required-evidence-types", errors.New("required evidence type is empty"))
		}
		if !slices.Contains(types, evidenceType) {
			return failedVerificationEvidenceConformance("required-evidence-types", fmt.Errorf("missing required evidence type %q", evidenceType))
		}
	}
	return passedVerificationEvidenceConformance("required-evidence-types")
}

func checkVerificationRequiredCIGates(report gopact.VerificationReport, required []string) VerificationEvidenceConformanceResult {
	gates := verificationReportCIGates(report)
	for _, gate := range required {
		if gate == "" {
			return failedVerificationEvidenceConformance("required-ci-gates", errors.New("required CI gate is empty"))
		}
		status, ok := gates[gate]
		if !ok {
			return failedVerificationEvidenceConformance("required-ci-gates", fmt.Errorf("missing required CI gate %q", gate))
		}
		if status != gopact.VerificationStatusPassed {
			return failedVerificationEvidenceConformance(
				"required-ci-gates",
				fmt.Errorf("required CI gate %q status %s is not passed", gate, status),
			)
		}
	}
	return passedVerificationEvidenceConformance("required-ci-gates")
}

func verificationReportCheckByID(report gopact.VerificationReport, id string) (gopact.VerificationCheck, bool) {
	for _, check := range report.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return gopact.VerificationCheck{}, false
}

func verificationReportScopedToChecks(
	report gopact.VerificationReport,
	requiredCheckIDs []string,
) gopact.VerificationReport {
	if len(requiredCheckIDs) == 0 {
		return report
	}

	scoped := report
	scoped.Checks = make([]gopact.VerificationCheck, 0, len(requiredCheckIDs))
	for _, id := range requiredCheckIDs {
		check, ok := verificationReportCheckByID(report, id)
		if ok {
			scoped.Checks = append(scoped.Checks, check)
		}
	}
	return scoped
}

func verificationReportEvidenceTypes(report gopact.VerificationReport) []string {
	seen := map[string]bool{}
	var out []string
	for _, check := range report.Checks {
		if check.Status != gopact.VerificationStatusPassed {
			continue
		}
		for _, evidence := range check.Evidence {
			if evidence.Type == "" || seen[evidence.Type] {
				continue
			}
			seen[evidence.Type] = true
			out = append(out, evidence.Type)
		}
	}
	slices.Sort(out)
	return out
}

func verificationReportCIGates(report gopact.VerificationReport) map[string]gopact.VerificationStatus {
	gates := make(map[string]gopact.VerificationStatus)
	for _, check := range report.Checks {
		if check.Status != gopact.VerificationStatusPassed {
			continue
		}
		for _, evidence := range check.Evidence {
			if evidence.Type != VerificationEvidenceTypeCIGate {
				continue
			}
			gate, ok := evidence.Metadata["gate"].(string)
			if !ok || gate == "" {
				continue
			}
			rawStatus, ok := evidence.Metadata["status"].(string)
			if !ok || rawStatus == "" {
				continue
			}
			gates[gate] = gopact.VerificationStatus(rawStatus)
		}
	}
	return gates
}

func verificationEvidenceRequirementName(index int, name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return fmt.Sprintf("requirement-%d", index+1)
}

func passedVerificationEvidenceConformance(name string) VerificationEvidenceConformanceResult {
	return VerificationEvidenceConformanceResult{Case: name, Passed: true}
}

func failedVerificationEvidenceConformance(name string, err error) VerificationEvidenceConformanceResult {
	return VerificationEvidenceConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrVerificationEvidenceConformanceFailed, err),
	}
}
