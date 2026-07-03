package gopacttest

import (
	"context"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestSelfBootstrapReleaseGateRequirementsPassCompleteReport(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if failed := failedVerificationEvidenceConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckVerificationEvidenceRequirements() failed cases: %v", failed)
	}
	RequireSelfBootstrapReleaseGate(t, report)
}

func TestBuildSelfBootstrapReleaseGateBundleEmbedsReport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	bundle, err := BuildSelfBootstrapReleaseGateBundle(export)
	if err != nil {
		t.Fatalf("BuildSelfBootstrapReleaseGateBundle() error = %v", err)
	}
	if len(bundle.RunExport.VerificationReports) != 1 {
		t.Fatalf("VerificationReports = %+v, want one embedded report", bundle.RunExport.VerificationReports)
	}
	if bundle.RunExport.VerificationReports[0].Status != gopact.VerificationStatusPassed {
		t.Fatalf("embedded report status = %q, want passed", bundle.RunExport.VerificationReports[0].Status)
	}
	RequireSelfBootstrapReleaseGateForExport(t, bundle.RunExport, bundle.Report)
}

func TestCheckSelfBootstrapReleaseGatePassesCompleteExportAndReport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	bundle, err := BuildSelfBootstrapReleaseGateBundle(export)
	if err != nil {
		t.Fatalf("BuildSelfBootstrapReleaseGateBundle() error = %v", err)
	}

	results := CheckSelfBootstrapReleaseGate(context.Background(), bundle.RunExport, bundle.Report)
	if failed := failedVerificationEvidenceConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckSelfBootstrapReleaseGate() failed cases: %v", failed)
	}
	RequireSelfBootstrapReleaseGateForExport(t, bundle.RunExport, bundle.Report)
}

func TestBuildSelfBootstrapReleaseGateReportRecordsReplayPlanFromRunExport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	export.Steps = []gopact.StepSnapshot{
		{
			ID:    "step-code",
			Step:  1,
			Node:  "code-agent",
			Phase: gopact.StepCompleted,
			Effects: []gopact.EffectRecord{
				{
					ID:             "effect-code",
					Type:           "command",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "code-agent:command",
				},
			},
		},
		{
			ID:    "step-review",
			Step:  2,
			Node:  "review-agent",
			Phase: gopact.StepCompleted,
			Effects: []gopact.EffectRecord{
				{
					ID:           "effect-review",
					Type:         "review",
					ReplayPolicy: gopact.EffectReplaySkip,
					DependsOn:    []string{"effect-code"},
				},
			},
		},
	}

	report := selfBootstrapReleaseGateReportForExport(t, export, selfBootstrapReleaseGateGates())
	check := findSelfBootstrapReleaseGateCheck(t, report, SelfBootstrapCheckReplayPlan)
	if len(check.Evidence) != 1 {
		t.Fatalf("replay plan evidence count = %d, want 1", len(check.Evidence))
	}
	evidence := check.Evidence[0]
	if evidence.Type != SelfBootstrapEvidenceTypeReplayPlan {
		t.Fatalf("replay plan evidence type = %q, want %q", evidence.Type, SelfBootstrapEvidenceTypeReplayPlan)
	}
	metadata := evidence.Metadata
	if metadata["decision_count"] != 2 || metadata["replay_count"] != 1 || metadata["skip_count"] != 1 {
		t.Fatalf("replay plan metadata = %+v, want decision/replay/skip counts", metadata)
	}
	if !reflect.DeepEqual(metadata["planned_effect_ids"], []string{"effect-code", "effect-review"}) {
		t.Fatalf("planned effect ids = %+v, want effect-code/effect-review", metadata["planned_effect_ids"])
	}
	if !reflect.DeepEqual(metadata["planned_step_ids"], []string{"step-code", "step-review"}) {
		t.Fatalf("planned step ids = %+v, want step-code/step-review", metadata["planned_step_ids"])
	}
}

func TestCheckSelfBootstrapReleaseGateRejectsFailureAttributions(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	export.Failures = []gopact.FailureAttribution{
		{ID: "failure-1", Kind: gopact.FailureVerification, Summary: "verification failed"},
	}
	report := selfBootstrapReleaseGateReportForExport(t, export, selfBootstrapReleaseGateGates())
	export.VerificationReports = []gopact.VerificationReport{report}

	results := CheckSelfBootstrapReleaseGate(context.Background(), export, report)
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-run-export/no-failure-attributions") {
		t.Fatalf("CheckSelfBootstrapReleaseGate() did not report failure attributions: %+v", results)
	}
	if hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-run-export/contains-verification-report") {
		t.Fatalf("CheckSelfBootstrapReleaseGate() also reported missing embedded report: %+v", results)
	}
}

func TestCheckSelfBootstrapReleaseGateRejectsNonCompletedExport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunInterrupted)
	report := selfBootstrapReleaseGateReportForExport(t, export, selfBootstrapReleaseGateGates())
	export.VerificationReports = []gopact.VerificationReport{report}

	results := CheckSelfBootstrapReleaseGate(context.Background(), export, report)
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-run-export/completed-outcome") {
		t.Fatalf("CheckSelfBootstrapReleaseGate() did not report non-completed export: %+v", results)
	}
}

func TestCheckSelfBootstrapReleaseGateRejectsMismatchedReport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	reportExport := export
	reportExport.IDs.RunID = "other-run"
	report := selfBootstrapReleaseGateReportForExport(t, reportExport, selfBootstrapReleaseGateGates())
	export.VerificationReports = []gopact.VerificationReport{report}

	results := CheckSelfBootstrapReleaseGate(context.Background(), export, report)
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-report-alignment/report-matches-export") {
		t.Fatalf("CheckSelfBootstrapReleaseGate() did not report mismatched report: %+v", results)
	}
}

func TestCheckSelfBootstrapReleaseGateRejectsFailedReport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	report := selfBootstrapReleaseGateReportForExport(t, export, selfBootstrapReleaseGateGates())
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:      "extra-failed-check",
		Status:  gopact.VerificationStatusFailed,
		Summary: "extra check failed",
		Evidence: []gopact.VerificationEvidence{
			{Type: "extra", Ref: "extra:failed", Summary: "extra failure"},
		},
	})
	report.Status = gopact.VerificationStatusFailed
	report.FailedCount++
	export.VerificationReports = []gopact.VerificationReport{report}

	results := CheckSelfBootstrapReleaseGate(context.Background(), export, report)
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-report-alignment/report-passed") {
		t.Fatalf("CheckSelfBootstrapReleaseGate() did not report failed report: %+v", results)
	}
}

func TestCheckSelfBootstrapReleaseGateRejectsExportWithoutEmbeddedReport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	report := selfBootstrapReleaseGateReportForExport(t, export, selfBootstrapReleaseGateGates())

	results := CheckSelfBootstrapReleaseGate(context.Background(), export, report)
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-run-export/contains-verification-report") {
		t.Fatalf("CheckSelfBootstrapReleaseGate() did not report missing embedded report: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingGate(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateExamples,
	})

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-ci/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing security gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExamplesGate(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateSecurity,
	})

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-ci/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing examples gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingSecretScan(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateExamples,
		SelfBootstrapCIGateSecurity,
		SelfBootstrapCIGateExtMock,
		SelfBootstrapCIGateExamplesMock,
		SelfBootstrapCIGateAgnesIntegration,
	})

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-secret-scan/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing secret scan gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExternalMockCI(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateExamples,
		SelfBootstrapCIGateSecurity,
		SelfBootstrapCIGateSecretScan,
		SelfBootstrapCIGateExamplesMock,
		SelfBootstrapCIGateAgnesIntegration,
	})

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ci/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing ext mock CI gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExamplesMockCI(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateExamples,
		SelfBootstrapCIGateSecurity,
		SelfBootstrapCIGateSecretScan,
		SelfBootstrapCIGateExtMock,
		SelfBootstrapCIGateAgnesIntegration,
	})

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ci/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing examples mock CI gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateReportIncludesExamplesMockSuiteCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())

	if findSelfBootstrapReleaseGateCheck(t, report, SelfBootstrapCheckExamplesMockSuiteCommand).ID == "" {
		t.Fatal("self-bootstrap release gate report missing examples mock suite command")
	}
}

func TestSelfBootstrapReleaseGateReportIncludesCoreMockSuiteCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())

	if findSelfBootstrapReleaseGateCheck(t, report, SelfBootstrapCheckCoreMockSuiteCommand).ID == "" {
		t.Fatal("self-bootstrap release gate report missing core mock suite command")
	}
}

func TestSelfBootstrapReleaseGateReportIncludesExtMockSuiteCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())

	if findSelfBootstrapReleaseGateCheck(t, report, SelfBootstrapCheckExtMockSuiteCommand).ID == "" {
		t.Fatal("self-bootstrap release gate report missing ext mock suite command")
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExamplesMockSuiteCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckExamplesMockSuiteCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ci/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing examples mock suite command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingCoreMockSuiteCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckCoreMockSuiteCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-ci/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing core mock suite command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExtMockSuiteCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckExtMockSuiteCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ci/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing ext mock suite command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingAgnesIntegration(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateExamples,
		SelfBootstrapCIGateSecurity,
		SelfBootstrapCIGateSecretScan,
		SelfBootstrapCIGateExtMock,
		SelfBootstrapCIGateExamplesMock,
	})

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ci/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing Agnes integration gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingPublicAPIBoundary(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckPublicAPIBoundary)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-public-api-boundary/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing public API boundary: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingPublicAPIExamples(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckPublicAPIExamples)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-public-api-examples/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing public API examples: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingPublicAPIExamplesCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckPublicAPIExamplesCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-public-api-examples/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing public API examples command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingFeatureCoverage(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckFeatureCoverage)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-feature-coverage/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing feature coverage: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingFeatureCoverageCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheckIfPresent(&report, "command:go test -count=1 ./checkpoint ./gopacttest/checkpointconformance")

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-feature-coverage/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing feature coverage command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingCoreCICommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheckIfPresent(&report, "command:go vet ./...")

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-ci/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing core CI command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingGraphConformanceCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheckIfPresent(&report, SelfBootstrapCheckGraphConformanceCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing graph conformance command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingA2AConformanceCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckA2AConformanceCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing A2A conformance command: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingAgnesIntegrationCommands(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{
			name: "provider",
			id:   SelfBootstrapCheckAgnesProviderIntegrationCommand,
		},
		{
			name: "agent templates",
			id:   SelfBootstrapCheckAgnesAgentTemplatesIntegrationCommand,
		},
		{
			name: "examples",
			id:   SelfBootstrapCheckAgnesExamplesIntegrationCommand,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
			removeSelfBootstrapReleaseGateCheck(t, &report, tt.id)

			results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
			if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-local-agnes-integration/required-check-ids") {
				t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing Agnes integration command: %+v", results)
			}
		})
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingAgnesIntegrationSuites(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{
			name: "ext suite",
			id:   SelfBootstrapCheckAgnesExtIntegrationSuiteCommand,
		},
		{
			name: "examples suite",
			id:   SelfBootstrapCheckAgnesExamplesIntegrationSuiteCommand,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
			removeSelfBootstrapReleaseGateCheckIfPresent(&report, tt.id)

			results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
			if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-local-agnes-integration/required-check-ids") {
				t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing Agnes integration suite: %+v", results)
			}
		})
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingRepositoryBoundary(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckRepositoryBoundary)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-repository-boundary/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing repository boundary: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingV1MigrationPlan(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckV1MigrationPlan)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-v1-migration/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing v1 migration plan: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingMigrationGuide(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckMigrationGuide)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-v1-migration/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing migration guide: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExtensionEcosystemReadiness(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckExtensionIntegrationRoadmap)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ecosystem-readiness/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing extension ecosystem readiness: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExtensionEcosystemCIGate(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	replaceSelfBootstrapReleaseGateCheck(t, &report, selfBootstrapExtensionEcosystemCICheck([]string{
		SelfBootstrapCIGateExtMock,
	}))

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-extension-ecosystem-readiness/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing extension ecosystem CI gate: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingRunExportEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	for i, check := range report.Checks {
		if check.ID == gopact.VerificationCheckRunExport+":run-self-bootstrap" {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			break
		}
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-run-export/required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing run export evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingPolicyEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	for i, check := range report.Checks {
		if check.ID == gopact.VerificationCheckPolicyDecision+":run-self-bootstrap:tool:execute" {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			break
		}
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-governance/required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing policy evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingCheckpointEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckCheckpoint)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing checkpoint evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingArtifactEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckArtifactIntegrity)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing artifact evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingRunEffectReplayEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckRunEffectReplay)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing run effect replay evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingReplayPlanEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckReplayPlan)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing replay plan evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingA2ATaskEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckA2ATask)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing A2A task evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingReleaseBundleEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckReleaseBundle)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-release-bundle/required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing release bundle evidence: %+v", results)
	}
}

func selfBootstrapReleaseGateReport(t *testing.T, gates []string) gopact.VerificationReport {
	t.Helper()
	return selfBootstrapReleaseGateReportForExport(t, selfBootstrapRunExport(gopact.RunCompleted), gates)
}

func selfBootstrapReleaseGateReportForExport(
	t *testing.T,
	export gopact.RunExport,
	gates []string,
) gopact.VerificationReport {
	t.Helper()
	report, err := BuildSelfBootstrapReleaseGateReport(export, WithSelfBootstrapCIGates(gates...))
	if err != nil {
		t.Fatalf("BuildSelfBootstrapReleaseGateReport() error = %v", err)
	}
	return report
}

func removeSelfBootstrapReleaseGateCheck(t *testing.T, report *gopact.VerificationReport, id string) {
	t.Helper()
	for i, check := range report.Checks {
		if check.ID == id {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			return
		}
	}
	t.Fatalf("self-bootstrap fixture missing check %q", id)
}

func findSelfBootstrapReleaseGateCheck(
	t *testing.T,
	report gopact.VerificationReport,
	id string,
) gopact.VerificationCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("self-bootstrap fixture missing check %q", id)
	return gopact.VerificationCheck{}
}

func removeSelfBootstrapReleaseGateCheckIfPresent(report *gopact.VerificationReport, id string) {
	for i, check := range report.Checks {
		if check.ID == id {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			return
		}
	}
}

func replaceSelfBootstrapReleaseGateCheck(
	t *testing.T,
	report *gopact.VerificationReport,
	replacement gopact.VerificationCheck,
) {
	t.Helper()
	for i, check := range report.Checks {
		if check.ID == replacement.ID {
			report.Checks[i] = replacement
			return
		}
	}
	t.Fatalf("self-bootstrap fixture missing check %q", replacement.ID)
}

func selfBootstrapRunExport(outcome gopact.RunOutcome) gopact.RunExport {
	return gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-self-bootstrap", ThreadID: "thread-self-bootstrap"},
		Outcome: outcome,
	}
}

func selfBootstrapReleaseGateGates() []string {
	return []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateRace,
		SelfBootstrapCIGateVet,
		SelfBootstrapCIGateLint,
		SelfBootstrapCIGateCoverage,
		SelfBootstrapCIGateExamples,
		SelfBootstrapCIGateSecurity,
		SelfBootstrapCIGateSecretScan,
		SelfBootstrapCIGateExtMock,
		SelfBootstrapCIGateExamplesMock,
		SelfBootstrapCIGateAgnesIntegration,
	}
}

func selfBootstrapExtensionEcosystemCICheck(gates []string) gopact.VerificationCheck {
	evidence := make([]gopact.VerificationEvidence, 0, len(gates))
	for _, gate := range gates {
		evidence = append(evidence, ciGateVerificationEvidence(gate, gopact.VerificationStatusPassed))
	}
	return gopact.VerificationCheck{
		ID:       SelfBootstrapCheckExtensionEcosystemCI,
		Status:   gopact.VerificationStatusPassed,
		Summary:  "extension ecosystem CI gates passed",
		Evidence: evidence,
	}
}
