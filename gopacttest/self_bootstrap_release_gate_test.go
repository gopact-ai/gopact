package gopacttest

import (
	"context"
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

func TestCheckSelfBootstrapReleaseGatePassesCompleteExportAndReport(t *testing.T) {
	export := selfBootstrapRunExport(gopact.RunCompleted)
	report := selfBootstrapReleaseGateReportForExport(t, export, selfBootstrapReleaseGateGates())
	export.VerificationReports = []gopact.VerificationReport{report}

	results := CheckSelfBootstrapReleaseGate(context.Background(), export, report)
	if failed := failedVerificationEvidenceConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckSelfBootstrapReleaseGate() failed cases: %v", failed)
	}
	RequireSelfBootstrapReleaseGateForExport(t, export, report)
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
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-external-ci/required-ci-gates") {
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
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-external-ci/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing examples mock CI gate: %+v", results)
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
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-external-ci/required-ci-gates") {
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

func TestSelfBootstrapReleaseGateRequirementsRejectMissingGraphConformanceCommand(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheckIfPresent(&report, SelfBootstrapCheckGraphConformanceCommand)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing graph conformance command: %+v", results)
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

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExternalRepositoryReadiness(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	removeSelfBootstrapReleaseGateCheck(t, &report, SelfBootstrapCheckExternalRepositories)

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-external-repository-readiness/required-check-ids") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing external repository readiness: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingExternalRepositoryCIGate(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	replaceSelfBootstrapReleaseGateCheck(t, &report, selfBootstrapExternalCICheck([]string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateVet,
	}))

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-external-repository-readiness/required-ci-gates") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing external repository CI gate: %+v", results)
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
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-evidence-types") {
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
	for i, check := range report.Checks {
		if check.ID == "checkpoint:thread-self-bootstrap:1:1" {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			break
		}
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing checkpoint evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingArtifactEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	for i, check := range report.Checks {
		if check.ID == "artifact-integrity:self-bootstrap" {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			break
		}
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing artifact evidence: %+v", results)
	}
}

func TestSelfBootstrapReleaseGateRequirementsRejectMissingRunEffectReplayEvidence(t *testing.T) {
	report := selfBootstrapReleaseGateReport(t, selfBootstrapReleaseGateGates())
	for i, check := range report.Checks {
		if check.ID == gopact.VerificationCheckRunEffectReplay+":run-self-bootstrap" {
			report.Checks = append(report.Checks[:i], report.Checks[i+1:]...)
			report.PassedCount--
			break
		}
	}

	results := CheckVerificationEvidenceRequirements(context.Background(), report, SelfBootstrapReleaseGateRequirements())
	if !hasFailedVerificationEvidenceConformanceCase(results, "self-bootstrap-behavior-evidence/required-evidence-types") {
		t.Fatalf("CheckVerificationEvidenceRequirements() did not report missing run effect replay evidence: %+v", results)
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
	checks := []gopact.VerificationCheck{
		{
			ID:     gopact.VerificationCheckRunExport + ":run-self-bootstrap",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: gopact.VerificationEvidenceTypeRunExport, Ref: "run-self-bootstrap", Summary: "run export captured"},
			},
		},
		selfBootstrapCICheck(gates),
		{
			ID:     "diff:worktree",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: VerificationEvidenceTypeDiff, Ref: "worktree", Summary: "diff captured"},
			},
		},
		{
			ID:     "file-snapshot:go.mod",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: VerificationEvidenceTypeFileSnapshot, Ref: "go.mod", Summary: "file snapshot captured"},
			},
		},
		selfBootstrapFileSnapshotCheck("docs/design/public-api-boundary.json"),
		selfBootstrapFileSnapshotCheck("docs/design/public-api-examples.json"),
		selfBootstrapFileSnapshotCheck("docs/design/deprecation-policy.md"),
		selfBootstrapFileSnapshotCheck("docs/design/api-ergonomics.md"),
		selfBootstrapFileSnapshotCheck("docs/design/repository-boundary.json"),
		selfBootstrapFileSnapshotCheck("docs/design/v1-migration-plan.json"),
		selfBootstrapFileSnapshotCheck("docs/design/migration-guide.md"),
		{
			ID:     SelfBootstrapCheckExternalRepositories,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: SelfBootstrapEvidenceTypeExternalRepositoryReadiness, Ref: "gopact-ai", Summary: "external repositories ready"},
			},
		},
		selfBootstrapExternalCICheck(selfBootstrapExternalRepositoryCIGates()),
		{
			ID:     "command:go-test",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: VerificationEvidenceTypeCommand, Ref: "go test -count=1 ./...", Summary: "command passed"},
			},
		},
		selfBootstrapCommandCheck("go test -run '^Example' ./..."),
		selfBootstrapCommandCheck("go test -count=1 ./gopacttest/graphconformance"),
		{
			ID:     "checkpoint:thread-self-bootstrap:1:1",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: "checkpoint", Ref: "thread-self-bootstrap:1:1", Summary: "checkpoint captured"},
			},
		},
		{
			ID:     "artifact-integrity:self-bootstrap",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: "artifact", Ref: "artifact:self-bootstrap", Summary: "artifact verified"},
			},
		},
		{
			ID:     gopact.VerificationCheckRunEffectReplay + ":run-self-bootstrap",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: gopact.VerificationEvidenceTypeRunEffectReplay, Ref: "run-self-bootstrap", Summary: "run effect replay verified"},
			},
		},
		{
			ID:     VerificationCheckTrajectoryGolden,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: VerificationEvidenceTypeTrajectoryGolden, Ref: "testdata/self-bootstrap.golden.json", Summary: "golden matched"},
			},
		},
		{
			ID:     "review:release",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: VerificationEvidenceTypeReview, Ref: "review:self-bootstrap", Summary: "review approved"},
			},
		},
		{
			ID:     SelfBootstrapCheckReleaseBundle,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: SelfBootstrapEvidenceTypeReleaseBundle, Ref: "self-bootstrap", Summary: "release bundle captured"},
			},
		},
		{
			ID:     gopact.VerificationCheckPolicyDecision + ":run-self-bootstrap:tool:execute",
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: gopact.VerificationEvidenceTypePolicyDecision, Ref: "run-self-bootstrap:tool:execute", Summary: "policy allowed"},
			},
		},
	}
	report, err := gopact.BuildVerificationReport(export, checks)
	if err != nil {
		t.Fatalf("BuildVerificationReport() error = %v", err)
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

func selfBootstrapCICheck(gates []string) gopact.VerificationCheck {
	evidence := make([]gopact.VerificationEvidence, 0, len(gates))
	for _, gate := range gates {
		evidence = append(evidence, ciGateVerificationEvidence(gate, gopact.VerificationStatusPassed))
	}
	return gopact.VerificationCheck{
		ID:       VerificationCheckCIGates,
		Status:   gopact.VerificationStatusPassed,
		Summary:  "self-bootstrap CI gates passed",
		Evidence: evidence,
	}
}

func selfBootstrapFileSnapshotCheck(path string) gopact.VerificationCheck {
	return gopact.VerificationCheck{
		ID:     "file-snapshot:" + path,
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			{Type: VerificationEvidenceTypeFileSnapshot, Ref: path, Summary: path + " snapshot captured"},
		},
	}
}

func selfBootstrapCommandCheck(command string) gopact.VerificationCheck {
	return gopact.VerificationCheck{
		ID:     "command:" + command,
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			{Type: VerificationEvidenceTypeCommand, Ref: command, Summary: command + " passed"},
		},
	}
}

func selfBootstrapExternalRepositoryCIGates() []string {
	return []string{
		SelfBootstrapCIGateWhitespace,
		SelfBootstrapCIGateModuleTidiness,
		SelfBootstrapCIGateUnit,
		SelfBootstrapCIGateVet,
	}
}

func selfBootstrapExternalCICheck(gates []string) gopact.VerificationCheck {
	evidence := make([]gopact.VerificationEvidence, 0, len(gates))
	for _, gate := range gates {
		evidence = append(evidence, ciGateVerificationEvidence(gate, gopact.VerificationStatusPassed))
	}
	return gopact.VerificationCheck{
		ID:       SelfBootstrapCheckExternalCI,
		Status:   gopact.VerificationStatusPassed,
		Summary:  "external repository CI gates passed",
		Evidence: evidence,
	}
}
