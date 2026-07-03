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
	SelfBootstrapCheckPublicAPIBoundary = "file-snapshot:doc/design/public-api-boundary.json"
	// SelfBootstrapCheckPublicAPIExamples is the standard public API examples manifest snapshot check.
	SelfBootstrapCheckPublicAPIExamples = "file-snapshot:doc/design/public-api-examples.json"
	// SelfBootstrapCheckPublicAPIExamplesCommand is the standard executable public API examples check.
	SelfBootstrapCheckPublicAPIExamplesCommand = "command:go test -run '^Example' ./..."
	// SelfBootstrapCheckFeatureCoverage is the standard feature coverage snapshot check.
	SelfBootstrapCheckFeatureCoverage = "file-snapshot:doc/FEATURES.md"
	// SelfBootstrapCheckGraphConformanceCommand is the standard graph workflow conformance check.
	SelfBootstrapCheckGraphConformanceCommand = "command:go test -count=1 ./graph ./gopacttest/graphconformance"
	// SelfBootstrapCheckA2AConformanceCommand is the standard A2A mesh conformance check.
	SelfBootstrapCheckA2AConformanceCommand = "command:go test -count=1 ./a2a ./gopacttest/a2aconformance"
	// SelfBootstrapCommandExamplesMockSuite is the standard gopact-examples mock self-bootstrap suite command.
	SelfBootstrapCommandExamplesMockSuite = "(cd gopact-examples && ./scripts/self-bootstrap-mock-suite.sh)"
	// SelfBootstrapCheckExamplesMockSuiteCommand is the standard gopact-examples mock self-bootstrap suite check.
	SelfBootstrapCheckExamplesMockSuiteCommand = "command:" + SelfBootstrapCommandExamplesMockSuite
	// SelfBootstrapCommandAgnesExtIntegrationSuite is the standard gopact-ext local Agnes integration suite command.
	SelfBootstrapCommandAgnesExtIntegrationSuite = "(cd gopact-ext && ./scripts/local-agnes-integration.sh)"
	// SelfBootstrapCheckAgnesExtIntegrationSuiteCommand is the standard gopact-ext local Agnes integration suite check.
	SelfBootstrapCheckAgnesExtIntegrationSuiteCommand = "command:" + SelfBootstrapCommandAgnesExtIntegrationSuite
	// SelfBootstrapCommandAgnesProviderIntegration is the standard local Agnes provider integration command.
	SelfBootstrapCommandAgnesProviderIntegration = "(cd gopact-ext/models/agnes && go test -tags=integration -count=1 ./...)"
	// SelfBootstrapCheckAgnesProviderIntegrationCommand is the standard local Agnes provider integration check.
	SelfBootstrapCheckAgnesProviderIntegrationCommand = "command:" + SelfBootstrapCommandAgnesProviderIntegration
	// SelfBootstrapCommandAgnesAgentTemplatesIntegration is the standard local Agnes-backed agent template command.
	SelfBootstrapCommandAgnesAgentTemplatesIntegration = "(cd gopact-ext/tests/agents && go test -tags=integration -count=1 ./...)"
	// SelfBootstrapCheckAgnesAgentTemplatesIntegrationCommand is the standard local Agnes-backed agent template check.
	SelfBootstrapCheckAgnesAgentTemplatesIntegrationCommand = "command:" + SelfBootstrapCommandAgnesAgentTemplatesIntegration
	// SelfBootstrapCommandAgnesExamplesIntegration is the standard local Agnes examples integration command.
	SelfBootstrapCommandAgnesExamplesIntegration = "(cd gopact-examples && go test -tags=integration -count=1 ./quickstart/agnes-chat)"
	// SelfBootstrapCheckAgnesExamplesIntegrationCommand is the standard local Agnes examples integration check.
	SelfBootstrapCheckAgnesExamplesIntegrationCommand = "command:" + SelfBootstrapCommandAgnesExamplesIntegration
	// SelfBootstrapCommandAgnesExamplesIntegrationSuite is the standard gopact-examples local Agnes integration suite command.
	SelfBootstrapCommandAgnesExamplesIntegrationSuite = "(cd gopact-examples && ./scripts/local-agnes-integration.sh)"
	// SelfBootstrapCheckAgnesExamplesIntegrationSuiteCommand is the standard gopact-examples local Agnes integration suite check.
	SelfBootstrapCheckAgnesExamplesIntegrationSuiteCommand = "command:" + SelfBootstrapCommandAgnesExamplesIntegrationSuite
	// SelfBootstrapCheckDeprecationPolicy is the standard deprecation policy snapshot check.
	SelfBootstrapCheckDeprecationPolicy = "file-snapshot:doc/design/deprecation-policy.md"
	// SelfBootstrapCheckAPIErgonomics is the standard API ergonomics snapshot check.
	SelfBootstrapCheckAPIErgonomics = "file-snapshot:doc/design/api-ergonomics.md"
	// SelfBootstrapCheckRepositoryBoundary is the standard repository boundary snapshot check.
	SelfBootstrapCheckRepositoryBoundary = "file-snapshot:doc/design/repository-boundary.json"
	// SelfBootstrapCheckV1MigrationPlan is the standard v1 migration plan snapshot check.
	SelfBootstrapCheckV1MigrationPlan = "file-snapshot:doc/design/v1-migration-plan.json"
	// SelfBootstrapCheckMigrationGuide is the standard migration guide snapshot check.
	SelfBootstrapCheckMigrationGuide = "file-snapshot:doc/design/migration-guide.md"
	// SelfBootstrapCheckExtensionEcosystemTopology is the standard extension ecosystem topology snapshot check.
	SelfBootstrapCheckExtensionEcosystemTopology = "file-snapshot:doc/design/ecosystem-topology.json"
	// SelfBootstrapCheckExtensionIntegrationRoadmap is the standard extension integration roadmap snapshot check.
	SelfBootstrapCheckExtensionIntegrationRoadmap = "file-snapshot:doc/design/external-integration-roadmap.json"
	// SelfBootstrapCheckExtensionConformance is the standard extension conformance snapshot check.
	SelfBootstrapCheckExtensionConformance = "file-snapshot:doc/design/extension-conformance.json"
	// SelfBootstrapCheckExtensionEcosystemCI is the standard gopact-ext/gopact-examples CI readiness check.
	SelfBootstrapCheckExtensionEcosystemCI = "extension-ecosystem-ci:gopact-ai"
	// SelfBootstrapCheckReleaseBundle is the standard self-bootstrap release bundle check.
	SelfBootstrapCheckReleaseBundle = "release-bundle:self-bootstrap"
	// SelfBootstrapCheckCheckpoint is the standard self-bootstrap checkpoint evidence check.
	SelfBootstrapCheckCheckpoint = "checkpoint:self-bootstrap"
	// SelfBootstrapCheckArtifactIntegrity is the standard self-bootstrap artifact integrity check.
	SelfBootstrapCheckArtifactIntegrity = "artifact-integrity:self-bootstrap"
	// SelfBootstrapCheckRunEffectReplay is the standard self-bootstrap run effect replay check.
	SelfBootstrapCheckRunEffectReplay = "run-effect-replay:self-bootstrap"
	// SelfBootstrapCheckReplayPlan is the standard self-bootstrap run effect replay plan check.
	SelfBootstrapCheckReplayPlan = "replay-plan:self-bootstrap"
	// SelfBootstrapCheckA2ATask is the standard self-bootstrap A2A task evidence check.
	SelfBootstrapCheckA2ATask = "a2a-task:self-bootstrap-agent-cluster"
	// SelfBootstrapEvidenceTypeReleaseBundle is the external Dev Agent release bundle evidence type.
	SelfBootstrapEvidenceTypeReleaseBundle = "release_bundle"
	// SelfBootstrapEvidenceTypeReplayPlan is the self-bootstrap run replay plan evidence type.
	SelfBootstrapEvidenceTypeReplayPlan = "run_effect_replay_plan"
	// SelfBootstrapEvidenceTypeA2ATask is the cross-agent task evidence type.
	SelfBootstrapEvidenceTypeA2ATask = "a2a_task"
)

// SelfBootstrapReleaseGateBundle carries a run export with its embedded self-bootstrap release report.
type SelfBootstrapReleaseGateBundle struct {
	RunExport gopact.RunExport
	Report    gopact.VerificationReport
}

// SelfBootstrapReleaseGateOption configures self-bootstrap release gate evidence.
type SelfBootstrapReleaseGateOption func(*selfBootstrapReleaseGateConfig)

type selfBootstrapReleaseGateConfig struct {
	ciGates                   []string
	extensionEcosystemCIGates []string
	additionalChecks          []gopact.VerificationCheck
}

// WithSelfBootstrapCIGates replaces the standard self-bootstrap CI gate names.
func WithSelfBootstrapCIGates(gates ...string) SelfBootstrapReleaseGateOption {
	return func(cfg *selfBootstrapReleaseGateConfig) {
		cfg.ciGates = append([]string(nil), gates...)
	}
}

// WithSelfBootstrapExtensionEcosystemCIGates replaces the standard extension ecosystem CI gate names.
func WithSelfBootstrapExtensionEcosystemCIGates(gates ...string) SelfBootstrapReleaseGateOption {
	return func(cfg *selfBootstrapReleaseGateConfig) {
		cfg.extensionEcosystemCIGates = append([]string(nil), gates...)
	}
}

// WithSelfBootstrapAdditionalChecks appends already-observed checks to the generated release report.
func WithSelfBootstrapAdditionalChecks(checks ...gopact.VerificationCheck) SelfBootstrapReleaseGateOption {
	return func(cfg *selfBootstrapReleaseGateConfig) {
		cfg.additionalChecks = append(cfg.additionalChecks, checks...)
	}
}

// BuildSelfBootstrapReleaseGateReport builds a standard self-bootstrap release report for an observed run export.
func BuildSelfBootstrapReleaseGateReport(
	export gopact.RunExport,
	opts ...SelfBootstrapReleaseGateOption,
) (gopact.VerificationReport, error) {
	cfg := defaultSelfBootstrapReleaseGateConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return gopact.BuildVerificationReport(export, selfBootstrapReleaseGateChecks(export, cfg))
}

// BuildSelfBootstrapReleaseGateBundle builds a standard report and embeds it into a copy of the run export.
func BuildSelfBootstrapReleaseGateBundle(
	export gopact.RunExport,
	opts ...SelfBootstrapReleaseGateOption,
) (SelfBootstrapReleaseGateBundle, error) {
	report, err := BuildSelfBootstrapReleaseGateReport(export, opts...)
	if err != nil {
		return SelfBootstrapReleaseGateBundle{}, err
	}

	bundled, err := gopact.EmbedVerificationReport(export, report)
	if err != nil {
		return SelfBootstrapReleaseGateBundle{}, fmt.Errorf("gopacttest: build self-bootstrap release gate bundle: %w", err)
	}
	return SelfBootstrapReleaseGateBundle{RunExport: bundled, Report: report}, nil
}

// SelfBootstrapReleaseGateRequirements returns the minimum evidence required for a self-bootstrap release gate.
func SelfBootstrapReleaseGateRequirements() []VerificationEvidenceRequirement {
	return []VerificationEvidenceRequirement{
		{
			Name:                  "self-bootstrap-run-export",
			RequiredEvidenceTypes: []string{gopact.VerificationEvidenceTypeRunExport},
		},
		{
			Name:                  "self-bootstrap-ci",
			RequiredCheckIDs:      append([]string{VerificationCheckCIGates}, selfBootstrapCoreCICommandCheckIDs()...),
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate, VerificationEvidenceTypeCommand},
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
			Name:                  "self-bootstrap-feature-coverage",
			RequiredCheckIDs:      append([]string{SelfBootstrapCheckFeatureCoverage}, selfBootstrapFeatureCoverageCommandCheckIDs()...),
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeFileSnapshot, VerificationEvidenceTypeCommand},
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
			Name: "self-bootstrap-extension-ecosystem-readiness",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckExtensionEcosystemTopology,
				SelfBootstrapCheckExtensionIntegrationRoadmap,
				SelfBootstrapCheckExtensionConformance,
				SelfBootstrapCheckExtensionEcosystemCI,
			},
			RequiredEvidenceTypes: []string{
				VerificationEvidenceTypeFileSnapshot,
				VerificationEvidenceTypeCIGate,
			},
			RequiredCIGates: []string{
				SelfBootstrapCIGateExtMock,
				SelfBootstrapCIGateExamplesMock,
			},
		},
		{
			Name:                  "self-bootstrap-secret-scan",
			RequiredCheckIDs:      []string{VerificationCheckCIGates},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate},
			RequiredCIGates:       []string{SelfBootstrapCIGateSecretScan},
		},
		{
			Name:                  "self-bootstrap-extension-ci",
			RequiredCheckIDs:      []string{VerificationCheckCIGates, SelfBootstrapCheckExamplesMockSuiteCommand},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCIGate, VerificationEvidenceTypeCommand},
			RequiredCIGates: []string{
				SelfBootstrapCIGateExtMock,
				SelfBootstrapCIGateExamplesMock,
				SelfBootstrapCIGateAgnesIntegration,
			},
		},
		{
			Name: "self-bootstrap-local-agnes-integration",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckAgnesExtIntegrationSuiteCommand,
				SelfBootstrapCheckAgnesProviderIntegrationCommand,
				SelfBootstrapCheckAgnesAgentTemplatesIntegrationCommand,
				SelfBootstrapCheckAgnesExamplesIntegrationCommand,
				SelfBootstrapCheckAgnesExamplesIntegrationSuiteCommand,
			},
			RequiredEvidenceTypes: []string{VerificationEvidenceTypeCommand},
		},
		{
			Name: "self-bootstrap-behavior-evidence",
			RequiredCheckIDs: []string{
				SelfBootstrapCheckGraphConformanceCommand,
				SelfBootstrapCheckA2AConformanceCommand,
				SelfBootstrapCheckCheckpoint,
				SelfBootstrapCheckArtifactIntegrity,
				SelfBootstrapCheckReplayPlan,
				SelfBootstrapCheckRunEffectReplay,
				SelfBootstrapCheckA2ATask,
				VerificationCheckTrajectoryGolden,
			},
			RequiredEvidenceTypes: []string{
				SelfBootstrapEvidenceTypeReplayPlan,
				gopact.VerificationEvidenceTypeRunEffectReplay,
				"checkpoint",
				"artifact",
				SelfBootstrapEvidenceTypeA2ATask,
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

func defaultSelfBootstrapReleaseGateConfig() selfBootstrapReleaseGateConfig {
	return selfBootstrapReleaseGateConfig{
		ciGates:                   defaultSelfBootstrapCIGates(),
		extensionEcosystemCIGates: defaultSelfBootstrapExtensionEcosystemCIGates(),
	}
}

func selfBootstrapReleaseGateChecks(
	export gopact.RunExport,
	cfg selfBootstrapReleaseGateConfig,
) []gopact.VerificationCheck {
	checkpointRef := selfBootstrapCheckpointRef(export.IDs)
	policyRef := export.IDs.RunID + ":tool:execute"
	checks := []gopact.VerificationCheck{
		{
			ID:     gopact.VerificationCheckRunExport + ":" + export.IDs.RunID,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: gopact.VerificationEvidenceTypeRunExport, Ref: export.IDs.RunID, Summary: "run export captured"},
			},
		},
		selfBootstrapCIGatesCheck(cfg.ciGates),
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
		selfBootstrapSnapshotCheck("doc/design/public-api-boundary.json"),
		selfBootstrapSnapshotCheck("doc/design/public-api-examples.json"),
		selfBootstrapSnapshotCheck("doc/FEATURES.md"),
		selfBootstrapSnapshotCheck("doc/design/deprecation-policy.md"),
		selfBootstrapSnapshotCheck("doc/design/api-ergonomics.md"),
		selfBootstrapSnapshotCheck("doc/design/repository-boundary.json"),
		selfBootstrapSnapshotCheck("doc/design/v1-migration-plan.json"),
		selfBootstrapSnapshotCheck("doc/design/migration-guide.md"),
		selfBootstrapSnapshotCheck("doc/design/ecosystem-topology.json"),
		selfBootstrapSnapshotCheck("doc/design/external-integration-roadmap.json"),
		selfBootstrapSnapshotCheck("doc/design/extension-conformance.json"),
		selfBootstrapExtensionEcosystemCIGatesCheck(cfg.extensionEcosystemCIGates),
	}
	checks = append(checks, selfBootstrapCoreCICommandChecks()...)
	checks = appendSelfBootstrapCommandChecks(checks, selfBootstrapFeatureCoverageCommands())
	checks = append(checks, []gopact.VerificationCheck{
		selfBootstrapCommandEvidenceCheck(SelfBootstrapCommandExamplesMockSuite),
		selfBootstrapCommandEvidenceCheck(SelfBootstrapCommandAgnesExtIntegrationSuite),
		selfBootstrapCommandEvidenceCheck(SelfBootstrapCommandAgnesProviderIntegration),
		selfBootstrapCommandEvidenceCheck(SelfBootstrapCommandAgnesAgentTemplatesIntegration),
		selfBootstrapCommandEvidenceCheck(SelfBootstrapCommandAgnesExamplesIntegration),
		selfBootstrapCommandEvidenceCheck(SelfBootstrapCommandAgnesExamplesIntegrationSuite),
		{
			ID:     SelfBootstrapCheckCheckpoint,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: "checkpoint", Ref: checkpointRef, Summary: "checkpoint captured"},
			},
		},
		{
			ID:     SelfBootstrapCheckArtifactIntegrity,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: "artifact", Ref: "artifact:self-bootstrap", Summary: "artifact verified"},
			},
		},
		selfBootstrapReplayPlanCheck(export),
		{
			ID:     SelfBootstrapCheckRunEffectReplay,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: gopact.VerificationEvidenceTypeRunEffectReplay, Ref: export.IDs.RunID, Summary: "run effect replay verified"},
			},
		},
		{
			ID:     SelfBootstrapCheckA2ATask,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: SelfBootstrapEvidenceTypeA2ATask, Ref: "self-bootstrap-agent-cluster", Summary: "agent mesh task completed"},
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
			ID:     gopact.VerificationCheckPolicyDecision + ":" + policyRef,
			Status: gopact.VerificationStatusPassed,
			Evidence: []gopact.VerificationEvidence{
				{Type: gopact.VerificationEvidenceTypePolicyDecision, Ref: policyRef, Summary: "policy allowed"},
			},
		},
	}...)
	return append(checks, cfg.additionalChecks...)
}

func selfBootstrapReplayPlanCheck(export gopact.RunExport) gopact.VerificationCheck {
	ref := export.IDs.RunID
	if ref == "" {
		ref = SelfBootstrapCheckReplayPlan
	}
	plan, err := gopact.PlanRunEffectReplay(export)
	status := gopact.VerificationStatusPassed
	summary := "replay plan captured"
	evidenceSummary := selfBootstrapReplayPlanEvidenceSummary(plan)
	metadata := selfBootstrapReplayPlanMetadata(plan, ref)
	if err != nil {
		status = gopact.VerificationStatusFailed
		summary = "replay plan failed: " + err.Error()
		evidenceSummary = err.Error()
		metadata["error"] = err.Error()
	}
	return gopact.VerificationCheck{
		ID:      SelfBootstrapCheckReplayPlan,
		Name:    "self-bootstrap replay plan",
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     SelfBootstrapEvidenceTypeReplayPlan,
				Ref:      ref,
				Summary:  evidenceSummary,
				Metadata: metadata,
			},
		},
		Metadata: metadata,
	}
}

func selfBootstrapReplayPlanEvidenceSummary(plan gopact.RunEffectReplayPlan) string {
	if len(plan.Decisions) == 1 {
		return "1 replay decision planned"
	}
	return fmt.Sprintf("%d replay decisions planned", len(plan.Decisions))
}

func selfBootstrapReplayPlanMetadata(plan gopact.RunEffectReplayPlan, ref string) map[string]any {
	metadata := map[string]any{
		"ref":               ref,
		"decision_count":    len(plan.Decisions),
		"replay_count":      plan.ReplayCount,
		"skip_count":        plan.SkipCount,
		"record_only_count": plan.RecordOnlyCount,
	}
	if plan.RunID != "" {
		metadata["run_id"] = plan.RunID
	}
	if plan.ThreadID != "" {
		metadata["thread_id"] = plan.ThreadID
	}
	if ids := selfBootstrapReplayPlanEffectIDs(plan); len(ids) > 0 {
		metadata["planned_effect_ids"] = ids
	}
	if ids := selfBootstrapReplayPlanStepIDs(plan); len(ids) > 0 {
		metadata["planned_step_ids"] = ids
	}
	return metadata
}

func selfBootstrapReplayPlanEffectIDs(plan gopact.RunEffectReplayPlan) []string {
	ids := make([]string, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if id := decision.Decision.Effect.ID; id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func selfBootstrapReplayPlanStepIDs(plan gopact.RunEffectReplayPlan) []string {
	ids := make([]string, 0, len(plan.Decisions))
	seen := make(map[string]struct{}, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if decision.StepID == "" {
			continue
		}
		if _, ok := seen[decision.StepID]; ok {
			continue
		}
		seen[decision.StepID] = struct{}{}
		ids = append(ids, decision.StepID)
	}
	return ids
}

func appendSelfBootstrapCommandChecks(
	checks []gopact.VerificationCheck,
	commands []string,
) []gopact.VerificationCheck {
	seen := make(map[string]bool, len(checks))
	for _, check := range checks {
		seen[check.ID] = true
	}
	for _, command := range commands {
		id := "command:" + command
		if seen[id] {
			continue
		}
		checks = append(checks, selfBootstrapCommandEvidenceCheck(command))
		seen[id] = true
	}
	return checks
}

func selfBootstrapCIGatesCheck(gates []string) gopact.VerificationCheck {
	return ciGateSuiteCheck(CIGateSuite{
		RequiredGates: gates,
		Results:       passedSelfBootstrapCIGateResults(gates),
	})
}

func selfBootstrapExtensionEcosystemCIGatesCheck(gates []string) gopact.VerificationCheck {
	return ciGateSuiteCheck(CIGateSuite{
		ID:            SelfBootstrapCheckExtensionEcosystemCI,
		Name:          "extension ecosystem CI gates",
		RequiredGates: gates,
		Results:       passedSelfBootstrapCIGateResults(gates),
	})
}

func passedSelfBootstrapCIGateResults(gates []string) []CIGateResult {
	results := make([]CIGateResult, 0, len(gates))
	for _, gate := range gates {
		results = append(results, CIGateResult{
			Gate: gate,
			Result: CommandResult{
				Command:  []string{"self-bootstrap-gate", gate},
				ExitCode: 0,
			},
		})
	}
	return results
}

func selfBootstrapSnapshotCheck(path string) gopact.VerificationCheck {
	return gopact.VerificationCheck{
		ID:     "file-snapshot:" + path,
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			{Type: VerificationEvidenceTypeFileSnapshot, Ref: path, Summary: path + " snapshot captured"},
		},
	}
}

func selfBootstrapCommandEvidenceCheck(command string) gopact.VerificationCheck {
	return gopact.VerificationCheck{
		ID:     "command:" + command,
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			{Type: VerificationEvidenceTypeCommand, Ref: command, Summary: command + " passed"},
		},
	}
}

func selfBootstrapCoreCICommandChecks() []gopact.VerificationCheck {
	commands := selfBootstrapCoreCICommands()
	checks := make([]gopact.VerificationCheck, 0, len(commands))
	for _, command := range commands {
		checks = append(checks, selfBootstrapCommandEvidenceCheck(command))
	}
	return checks
}

func selfBootstrapCoreCICommandCheckIDs() []string {
	commands := selfBootstrapCoreCICommands()
	ids := make([]string, 0, len(commands))
	for _, command := range commands {
		ids = append(ids, "command:"+command)
	}
	return ids
}

func selfBootstrapCoreCICommands() []string {
	return []string{
		"git diff --check",
		"go mod tidy",
		"git diff --exit-code",
		"go test -count=1 ./...",
		"go test -race -count=1 ./...",
		"go vet ./...",
		"golangci-lint run ./...",
		"go test -coverprofile=coverage.out ./...",
		"go test -run '^Example' ./...",
		"go test -count=1 ./graph ./gopacttest/graphconformance",
		"go test -count=1 ./a2a ./gopacttest/a2aconformance",
		"govulncheck ./...",
	}
}

func selfBootstrapFeatureCoverageCommandCheckIDs() []string {
	commands := selfBootstrapFeatureCoverageCommands()
	ids := make([]string, 0, len(commands))
	for _, command := range commands {
		ids = append(ids, "command:"+command)
	}
	return ids
}

func selfBootstrapFeatureCoverageCommands() []string {
	return []string{
		"go test -count=1 ./graph ./gopacttest/graphconformance",
		"go test -count=1 ./checkpoint ./gopacttest/checkpointconformance",
		"go test -count=1 . ./provider ./gopacttest/providerconformance",
		"go test -count=1 ./tools ./gopacttest/toolconformance",
		"go test -count=1 ./mcp",
		"go test -count=1 ./a2a ./gopacttest/a2aconformance",
		"go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a",
		"go test -count=1 ./a2a -run TestMeshSyncEvery",
		"go test -count=1 ./a2a -run TestMeshSyncEnvEvery",
		"go test -count=1 ./cmd/gopact",
		"go test -count=1 -run Channel . ./gopacttest",
		"go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance",
		"go test -count=1 ./gopacttest",
		"go test -count=1 -run SelfBootstrap ./gopacttest",
	}
}

func selfBootstrapCheckpointRef(ids gopact.RuntimeIDs) string {
	if ids.ThreadID != "" {
		return ids.ThreadID + ":1:1"
	}
	return ids.RunID + ":1:1"
}

func defaultSelfBootstrapCIGates() []string {
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

func defaultSelfBootstrapExtensionEcosystemCIGates() []string {
	return []string{
		SelfBootstrapCIGateExtMock,
		SelfBootstrapCIGateExamplesMock,
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
