package devagent

import (
	"context"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckReleaseBundleConformancePassesReleaseBundleWithRequiredCIGates(t *testing.T) {
	bundle := releaseBundleFixture(t)
	harness := ReleaseBundleConformanceHarness{
		Bundle:                bundle,
		RequiredCheckIDs:      []string{"unit-tests", "diff-check"},
		RequiredEvidenceTypes: []string{"command", "diff"},
		RequiredCIGates:       []string{"unit", "vet"},
	}

	results := CheckReleaseBundleConformance(context.Background(), harness)
	if failed := failedReleaseBundleConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckReleaseBundleConformance() failed cases: %v", failed)
	}
	RequireReleaseBundleConformance(t, harness)
}

func TestCheckReleaseBundleConformanceReportsMissingRequiredCIGate(t *testing.T) {
	bundle := releaseBundleFixture(t)
	harness := ReleaseBundleConformanceHarness{
		Bundle:          bundle,
		RequiredCIGates: []string{"unit", "race"},
	}

	results := CheckReleaseBundleConformance(context.Background(), harness)
	if !hasFailedReleaseBundleConformanceCase(results, "required-ci-gates") {
		t.Fatalf("CheckReleaseBundleConformance() did not report missing CI gate: %+v", results)
	}
}

func TestCheckReleaseBundleConformanceReportsReleaseBundleEvidenceWithoutRequiredCIGateMetadata(t *testing.T) {
	bundle := releaseBundleFixture(t)
	bundle.RequiredCIGates = nil
	harness := ReleaseBundleConformanceHarness{
		Bundle:          bundle,
		RequiredCIGates: []string{"unit"},
	}

	results := CheckReleaseBundleConformance(context.Background(), harness)
	if !hasFailedReleaseBundleConformanceCase(results, "release-bundle-evidence") {
		t.Fatalf("CheckReleaseBundleConformance() did not report missing release bundle metadata: %+v", results)
	}
}

func TestCheckReleaseBundleConformanceReportsInvalidBundle(t *testing.T) {
	bundle := releaseBundleFixture(t)
	bundle.Gate.Status = GateRejected

	results := CheckReleaseBundleConformance(context.Background(), ReleaseBundleConformanceHarness{Bundle: bundle})
	if !hasFailedReleaseBundleConformanceCase(results, "valid-bundle") {
		t.Fatalf("CheckReleaseBundleConformance() did not report invalid bundle: %+v", results)
	}
}

func TestCheckReleaseBundleConformanceReportsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := CheckReleaseBundleConformance(ctx, ReleaseBundleConformanceHarness{Bundle: releaseBundleFixture(t)})
	if !hasFailedReleaseBundleConformanceCase(results, "context") {
		t.Fatalf("CheckReleaseBundleConformance() did not report canceled context: %+v", results)
	}
}

func failedReleaseBundleConformanceCases(results []ReleaseBundleConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedReleaseBundleConformanceCase(results []ReleaseBundleConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

func TestCheckReleaseBundleConformanceReportsReleaseBundleEvidenceMetadataDrift(t *testing.T) {
	bundle := releaseBundleFixture(t)
	bundle.VerificationReport.Checks = append(bundle.VerificationReport.Checks, gopact.VerificationCheck{
		ID:      "race-only",
		Name:    "race",
		Status:  gopact.VerificationStatusPassed,
		Summary: "race passed outside the sealed bundle requirement",
		Evidence: []gopact.VerificationEvidence{
			{
				Type:    "ci_gate",
				Ref:     "ci-gate:race",
				Summary: "race passed",
				Metadata: map[string]any{
					"gate":   "race",
					"status": string(gopact.VerificationStatusPassed),
				},
			},
		},
	})
	bundle.VerificationReport.PassedCount++
	harness := ReleaseBundleConformanceHarness{
		Bundle:          bundle,
		RequiredCIGates: []string{"unit", "race"},
	}

	results := CheckReleaseBundleConformance(context.Background(), harness)
	if !hasFailedReleaseBundleConformanceCase(results, "release-bundle-evidence") {
		t.Fatalf("CheckReleaseBundleConformance() did not report required gate metadata drift: %+v", results)
	}
}
