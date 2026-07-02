package gopacttest

import (
	"context"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckVerificationEvidenceConformancePassesReport(t *testing.T) {
	report := verificationConformanceReport(t)
	harness := VerificationEvidenceConformanceHarness{
		Report:                report,
		RequiredCheckIDs:      []string{"run-export", "diff"},
		RequiredEvidenceTypes: []string{"run_export", "diff", VerificationEvidenceTypeCIGate},
		RequiredCIGates:       []string{"unit", "vet"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if failed := failedVerificationEvidenceConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckVerificationEvidenceConformance() failed cases: %v", failed)
	}
	RequireVerificationEvidenceConformance(t, harness)
}

func TestCheckVerificationEvidenceRequirementsPassesMultipleRequirements(t *testing.T) {
	report := verificationConformanceReport(t)
	requirements := []VerificationEvidenceRequirement{
		{
			Name:                  "core-ci-gates",
			RequiredCheckIDs:      []string{"core-ci"},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate},
			RequiredCIGates:       []string{"unit", "vet"},
		},
		{
			Name:                  "run-export",
			RequiredCheckIDs:      []string{"run-export"},
			RequiredEvidenceTypes: []string{"run_export"},
		},
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, requirements)
	if failed := failedVerificationEvidenceConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckVerificationEvidenceRequirements() failed cases: %v", failed)
	}
	RequireVerificationEvidenceRequirements(t, report, requirements)
}

func TestCheckVerificationEvidenceRequirementsScopesCIGatesToRequiredChecks(t *testing.T) {
	report := verificationConformanceReport(t)
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:     "extension-ci",
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			ciGateVerificationEvidence("ext-mock-ci", gopact.VerificationStatusPassed),
		},
	})
	report.PassedCount++
	requirements := []VerificationEvidenceRequirement{
		{
			Name:                  "extension-ecosystem",
			RequiredCheckIDs:      []string{"extension-ci"},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate},
			RequiredCIGates:       []string{"unit"},
		},
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, requirements)
	if !hasFailedVerificationEvidenceConformanceCase(results, "extension-ecosystem/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() accepted CI gate from another check: %+v", results)
	}
}

func TestCheckVerificationEvidenceRequirementsPrefixesFailedRequirementName(t *testing.T) {
	report := verificationConformanceReport(t)
	requirements := []VerificationEvidenceRequirement{
		{
			Name:             "extension-ecosystem-readiness",
			RequiredCheckIDs: []string{"extension-ecosystem-ci:gopact-ai"},
		},
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, requirements)
	if !hasFailedVerificationEvidenceConformanceCase(results, "extension-ecosystem-readiness/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report named failed requirement: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsMissingEvidenceType(t *testing.T) {
	report := verificationConformanceReport(t)
	harness := VerificationEvidenceConformanceHarness{
		Report:                report,
		RequiredEvidenceTypes: []string{"run_export", "checkpoint"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report missing evidence type: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsRequiredEvidenceTypeThatDidNotPass(t *testing.T) {
	report := verificationConformanceReport(t)
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:      "checkpoint-check",
		Name:    "checkpoint check",
		Status:  gopact.VerificationStatusFailed,
		Summary: "checkpoint failed",
		Evidence: []gopact.VerificationEvidence{
			{Type: "checkpoint", Ref: "checkpoint:run-1", Summary: "checkpoint rejected"},
		},
	})
	report.Status = gopact.VerificationStatusFailed
	report.FailedCount++
	harness := VerificationEvidenceConformanceHarness{
		Report:                report,
		RequiredEvidenceTypes: []string{"checkpoint"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report failed required evidence type: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsRequiredCheckIDThatDidNotPass(t *testing.T) {
	report := verificationConformanceReport(t)
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:      "release-gate",
		Name:    "release gate",
		Status:  gopact.VerificationStatusFailed,
		Summary: "release gate failed",
		Evidence: []gopact.VerificationEvidence{
			{Type: "release_gate", Ref: "release-gate:write", Summary: "release gate rejected"},
		},
	})
	report.Status = gopact.VerificationStatusFailed
	report.FailedCount++
	harness := VerificationEvidenceConformanceHarness{
		Report:           report,
		RequiredCheckIDs: []string{"release-gate"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report failed required check id: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsMissingCIGate(t *testing.T) {
	report := verificationConformanceReport(t)
	harness := VerificationEvidenceConformanceHarness{
		Report:          report,
		RequiredCIGates: []string{"unit", "race"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report missing CI gate: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsSkippedCIGate(t *testing.T) {
	report := verificationConformanceReport(t)
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:     "skipped-ci",
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			ciGateVerificationEvidence("lint", gopact.VerificationStatusSkipped),
		},
	})
	report.PassedCount++
	harness := VerificationEvidenceConformanceHarness{
		Report:          report,
		RequiredCIGates: []string{"lint"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report skipped CI gate: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsCIGateWithoutStatus(t *testing.T) {
	report := verificationConformanceReport(t)
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:     "ci-without-status",
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:    VerificationEvidenceTypeCIGate,
				Ref:     "ci-gate:coverage",
				Summary: "coverage gate observed without explicit status",
				Metadata: map[string]any{
					"gate": "coverage",
				},
			},
		},
	})
	report.PassedCount++
	harness := VerificationEvidenceConformanceHarness{
		Report:          report,
		RequiredCIGates: []string{"coverage"},
	}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report CI gate without status: %+v", results)
	}
}

func TestCheckVerificationEvidenceConformanceReportsInvalidReport(t *testing.T) {
	report := verificationConformanceReport(t)
	report.Checks[0].Evidence = nil
	harness := VerificationEvidenceConformanceHarness{Report: report}

	results := CheckVerificationEvidenceConformance(context.Background(), harness)
	if !hasFailedVerificationEvidenceConformanceCase(results, "valid-report") {
		t.Fatalf("CheckVerificationEvidenceConformance() did not report invalid report: %+v", results)
	}
}

func verificationConformanceReport(t *testing.T) gopact.VerificationReport {
	t.Helper()

	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: gopact.RunCompleted,
	}
	report, err := gopact.BuildVerificationReport(export, []gopact.VerificationCheck{
		{
			ID:     "run-export",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: "run_export", Ref: "run:run-1", Summary: "run export captured"},
			},
		},
		{
			ID:     "diff",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: "diff", Ref: "diff:worktree", Summary: "diff captured"},
			},
		},
		{
			ID:     "core-ci",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				ciGateVerificationEvidence("unit", gopact.VerificationStatusPassed),
				ciGateVerificationEvidence("vet", gopact.VerificationStatusPassed),
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildVerificationReport() error = %v", err)
	}
	return report
}

func ciGateVerificationEvidence(gate string, status gopact.VerificationStatus) gopact.VerificationEvidence {
	return gopact.VerificationEvidence{
		Type:    VerificationEvidenceTypeCIGate,
		Ref:     "ci-gate:" + gate,
		Summary: gate + " gate " + string(status),
		Metadata: map[string]any{
			"gate":   gate,
			"status": string(status),
		},
	}
}

func failedVerificationEvidenceConformanceCases(results []VerificationEvidenceConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedVerificationEvidenceConformanceCase(results []VerificationEvidenceConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}
