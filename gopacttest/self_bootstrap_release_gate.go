package gopacttest

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

const (
	// SelfBootstrapCIGateWhitespace is the standard whitespace/diff hygiene gate.
	SelfBootstrapCIGateWhitespace = "whitespace"
	// SelfBootstrapCIGateUnit is the standard unit/contract test gate.
	SelfBootstrapCIGateUnit = "unit"
	// SelfBootstrapCIGateRace is the standard race detector gate.
	SelfBootstrapCIGateRace = "race"
	// SelfBootstrapCIGateVet is the standard go vet gate.
	SelfBootstrapCIGateVet = "vet"
	// SelfBootstrapCIGateLint is the standard lint gate.
	SelfBootstrapCIGateLint = "lint"
	// SelfBootstrapCIGateCoverage is the standard coverage gate.
	SelfBootstrapCIGateCoverage = "coverage"
	// SelfBootstrapCIGateExamples is the standard executable examples gate.
	SelfBootstrapCIGateExamples = "examples"
	// SelfBootstrapCIGateSecurity is the standard vulnerability/secret scan gate.
	SelfBootstrapCIGateSecurity = "security"
	// SelfBootstrapCIGateSecretScan is the standard hardcoded-secret scan gate.
	SelfBootstrapCIGateSecretScan = "secret-scan"
	// SelfBootstrapCIGateModuleTidiness is the standard external module tidiness gate.
	SelfBootstrapCIGateModuleTidiness = "module-tidiness"
	// SelfBootstrapCIGateExtMock is the standard gopact-ext mock CI gate.
	SelfBootstrapCIGateExtMock = "ext-mock-ci"
	// SelfBootstrapCIGateExamplesMock is the standard gopact-examples mock CI gate.
	SelfBootstrapCIGateExamplesMock = "examples-mock-ci"
	// SelfBootstrapCIGateAgnesIntegration is the standard local Agnes integration evidence gate.
	SelfBootstrapCIGateAgnesIntegration = "agnes-integration"

	// SelfBootstrapCheckPublicAPIBoundary is the standard public API boundary snapshot check.
	SelfBootstrapCheckPublicAPIBoundary = "file-snapshot:docs/design/public-api-boundary.json"
	// SelfBootstrapCheckPublicAPIExamples is the standard public API examples manifest snapshot check.
	SelfBootstrapCheckPublicAPIExamples = "file-snapshot:docs/design/public-api-examples.json"
	// SelfBootstrapCheckPublicAPIExamplesCommand is the standard executable public API examples check.
	SelfBootstrapCheckPublicAPIExamplesCommand = "command:go test -run '^Example' ./..."
	// SelfBootstrapCheckGraphConformanceCommand is the standard graph workflow conformance check.
	SelfBootstrapCheckGraphConformanceCommand = "command:go test -count=1 ./gopacttest/graphconformance"
	// SelfBootstrapCheckDeprecationPolicy is the standard deprecation policy snapshot check.
	SelfBootstrapCheckDeprecationPolicy = "file-snapshot:docs/design/deprecation-policy.md"
	// SelfBootstrapCheckAPIErgonomics is the standard API ergonomics snapshot check.
	SelfBootstrapCheckAPIErgonomics = "file-snapshot:docs/design/api-ergonomics.md"
	// SelfBootstrapCheckRepositoryBoundary is the standard repository boundary snapshot check.
	SelfBootstrapCheckRepositoryBoundary = "file-snapshot:docs/design/repository-boundary.json"
	// SelfBootstrapCheckV1MigrationPlan is the standard v1 migration plan snapshot check.
	SelfBootstrapCheckV1MigrationPlan = "file-snapshot:docs/design/v1-migration-plan.json"
	// SelfBootstrapCheckMigrationGuide is the standard migration guide snapshot check.
	SelfBootstrapCheckMigrationGuide = "file-snapshot:docs/design/migration-guide.md"
	// SelfBootstrapCheckExternalRepositories is the standard external repository readiness check.
	SelfBootstrapCheckExternalRepositories = "external-repositories:gopact-ai"
	// SelfBootstrapCheckExternalCI is the standard external repository CI readiness check.
	SelfBootstrapCheckExternalCI = "external-ci:gopact-ai"
	// SelfBootstrapCheckReleaseBundle is the standard self-bootstrap release bundle check.
	SelfBootstrapCheckReleaseBundle = "release-bundle:self-bootstrap"
	// SelfBootstrapEvidenceTypeExternalRepositoryReadiness is the external repository readiness evidence type.
	SelfBootstrapEvidenceTypeExternalRepositoryReadiness = "external_repository_readiness"
	// SelfBootstrapEvidenceTypeReleaseBundle is the external Dev Agent release bundle evidence type.
	SelfBootstrapEvidenceTypeReleaseBundle = "release_bundle"
)

// SelfBootstrapReleaseGateRequirements returns the minimum evidence required for a self-bootstrap release gate.
func SelfBootstrapReleaseGateRequirements() []VerificationEvidenceRequirement {
	return []VerificationEvidenceRequirement{
		{
			Name:                  "self-bootstrap-ci",
			RequiredCheckIDs:      []string{VerificationCheckCIGates},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate},
			RequiredCIGates: []string{
				SelfBootstrapCIGateWhitespace,
				SelfBootstrapCIGateUnit,
				SelfBootstrapCIGateRace,
				SelfBootstrapCIGateVet,
				SelfBootstrapCIGateLint,
				SelfBootstrapCIGateCoverage,
				SelfBootstrapCIGateExamples,
				SelfBootstrapCIGateSecurity,
			},
		},
		{
			Name: "self-bootstrap-change-evidence",
			RequiredEvidenceTypes: []string{
				VerificationEvidenceTypeDiff,
				VerificationEvidenceTypeFileSnapshot,
			},
		},
		{
			Name: "self-bootstrap-public-api-boundary",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckPublicAPIBoundary,
				SelfBootstrapCheckDeprecationPolicy,
				SelfBootstrapCheckAPIErgonomics,
			},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeFileSnapshot},
		},
		{
			Name: "self-bootstrap-public-api-examples",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckPublicAPIExamples,
				SelfBootstrapCheckPublicAPIExamplesCommand,
			},
			RequiredEvidenceTypes: []string{
				VerificationEvidenceTypeFileSnapshot,
				VerificationEvidenceTypeCommand,
			},
		},
		{
			Name:                  "self-bootstrap-repository-boundary",
			RequiredCheckIDs:      []string{SelfBootstrapCheckRepositoryBoundary},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeFileSnapshot},
		},
		{
			Name: "self-bootstrap-v1-migration",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckV1MigrationPlan,
				SelfBootstrapCheckMigrationGuide,
			},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeFileSnapshot},
		},
		{
			Name: "self-bootstrap-external-repository-readiness",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckExternalRepositories,
				SelfBootstrapCheckExternalCI,
			},
			RequiredEvidenceTypes: []string{
				SelfBootstrapEvidenceTypeExternalRepositoryReadiness,
				VerificationEvidenceTypeCIGate,
			},
			RequiredCIGates: []string{
				SelfBootstrapCIGateWhitespace,
				SelfBootstrapCIGateModuleTidiness,
				SelfBootstrapCIGateUnit,
				SelfBootstrapCIGateVet,
			},
		},
		{
			Name:                  "self-bootstrap-secret-scan",
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate},
			RequiredCIGates:       []string{SelfBootstrapCIGateSecretScan},
		},
		{
			Name:                  "self-bootstrap-external-ci",
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate},
			RequiredCIGates: []string{
				SelfBootstrapCIGateExtMock,
				SelfBootstrapCIGateExamplesMock,
				SelfBootstrapCIGateAgnesIntegration,
			},
		},
		{
			Name: "self-bootstrap-behavior-evidence",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckGraphConformanceCommand,
			},
			RequiredEvidenceTypes: []string{
				gopact.VerificationEvidenceTypeRunExport,
				gopact.VerificationEvidenceTypeRunEffectReplay,
				"checkpoint",
				"artifact",
				VerificationEvidenceTypeCommand,
				VerificationEvidenceTypeTrajectoryGolden,
			},
		},
		{
			Name:                  "self-bootstrap-release-bundle",
			RequiredCheckIDs:      []string{SelfBootstrapCheckReleaseBundle},
			RequiredEvidenceTypes: []string{SelfBootstrapEvidenceTypeReleaseBundle},
		},
		{
			Name: "self-bootstrap-governance",
			RequiredEvidenceTypes: []string{
				gopact.VerificationEvidenceTypePolicyDecision,
				VerificationEvidenceTypeReview,
			},
		},
	}
}

// CheckSelfBootstrapReleaseGate checks the minimum report and run-export invariants for a self-bootstrap release.
func CheckSelfBootstrapReleaseGate(
	ctx context.Context,
	export gopact.RunExport,
	report gopact.VerificationReport,
) []VerificationEvidenceConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []VerificationEvidenceConformanceResult{failedVerificationEvidenceConformance("context", err)}
	}

	results := CheckVerificationEvidenceRequirements(ctx, report, SelfBootstrapReleaseGateRequirements())
	results = append(results,
		checkSelfBootstrapRunExportValid(export),
		checkSelfBootstrapRunExportCompleted(export),
		checkSelfBootstrapRunExportWithoutFailures(export),
		checkSelfBootstrapRunExportContainsReport(export, report),
		checkSelfBootstrapReportPassed(report),
		checkSelfBootstrapReportMatchesExport(export, report),
	)
	return results
}

// RequireSelfBootstrapReleaseGate fails the test unless report satisfies the self-bootstrap release gate.
func RequireSelfBootstrapReleaseGate(t testing.TB, report gopact.VerificationReport) {
	t.Helper()
	RequireVerificationEvidenceRequirements(t, report, SelfBootstrapReleaseGateRequirements())
}

// RequireSelfBootstrapReleaseGateForExport fails unless export and report satisfy the self-bootstrap release gate.
func RequireSelfBootstrapReleaseGateForExport(
	t testing.TB,
	export gopact.RunExport,
	report gopact.VerificationReport,
) {
	t.Helper()
	for _, result := range CheckSelfBootstrapReleaseGate(context.Background(), export, report) {
		if !result.Passed {
			t.Fatalf("self-bootstrap release gate case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkSelfBootstrapRunExportValid(export gopact.RunExport) VerificationEvidenceConformanceResult {
	if err := export.Validate(); err != nil {
		return failedVerificationEvidenceConformance("self-bootstrap-run-export/valid-export", err)
	}
	return passedVerificationEvidenceConformance("self-bootstrap-run-export/valid-export")
}

func checkSelfBootstrapRunExportCompleted(export gopact.RunExport) VerificationEvidenceConformanceResult {
	if export.Outcome != gopact.RunCompleted {
		return failedVerificationEvidenceConformance(
			"self-bootstrap-run-export/completed-outcome",
			fmt.Errorf("run export outcome %q is not completed", export.Outcome),
		)
	}
	return passedVerificationEvidenceConformance("self-bootstrap-run-export/completed-outcome")
}

func checkSelfBootstrapRunExportWithoutFailures(export gopact.RunExport) VerificationEvidenceConformanceResult {
	if len(export.Failures) > 0 {
		return failedVerificationEvidenceConformance(
			"self-bootstrap-run-export/no-failure-attributions",
			fmt.Errorf("run export contains %d failure attributions", len(export.Failures)),
		)
	}
	return passedVerificationEvidenceConformance("self-bootstrap-run-export/no-failure-attributions")
}

func checkSelfBootstrapRunExportContainsReport(
	export gopact.RunExport,
	report gopact.VerificationReport,
) VerificationEvidenceConformanceResult {
	for _, embedded := range export.VerificationReports {
		if reflect.DeepEqual(embedded, report) {
			return passedVerificationEvidenceConformance("self-bootstrap-run-export/contains-verification-report")
		}
	}
	return failedVerificationEvidenceConformance(
		"self-bootstrap-run-export/contains-verification-report",
		fmt.Errorf("run export does not contain the verification report"),
	)
}

func checkSelfBootstrapReportPassed(report gopact.VerificationReport) VerificationEvidenceConformanceResult {
	if report.Status != gopact.VerificationStatusPassed {
		return failedVerificationEvidenceConformance(
			"self-bootstrap-report-alignment/report-passed",
			fmt.Errorf("verification report status %q is not passed", report.Status),
		)
	}
	return passedVerificationEvidenceConformance("self-bootstrap-report-alignment/report-passed")
}

func checkSelfBootstrapReportMatchesExport(
	export gopact.RunExport,
	report gopact.VerificationReport,
) VerificationEvidenceConformanceResult {
	if report.IDs != export.IDs {
		return failedVerificationEvidenceConformance(
			"self-bootstrap-report-alignment/report-matches-export",
			fmt.Errorf("verification report ids do not match run export ids"),
		)
	}
	if report.Outcome != export.Outcome {
		return failedVerificationEvidenceConformance(
			"self-bootstrap-report-alignment/report-matches-export",
			fmt.Errorf("verification report outcome %q does not match run export outcome %q", report.Outcome, export.Outcome),
		)
	}
	return passedVerificationEvidenceConformance("self-bootstrap-report-alignment/report-matches-export")
}
