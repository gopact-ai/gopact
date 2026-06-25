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
