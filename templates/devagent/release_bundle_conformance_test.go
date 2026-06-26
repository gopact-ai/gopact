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

func TestCheckReleaseBundleConformancePassesWorkflowReleaseBundle(t *testing.T) {
	bundle := releaseBundleWithWorkflowFixture(t)

	results := CheckReleaseBundleConformance(context.Background(), ReleaseBundleConformanceHarness{Bundle: bundle})
	if failed := failedReleaseBundleConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckReleaseBundleConformance() failed cases: %v", failed)
	}
}

func TestCheckReleaseBundleConformanceReportsWorkflowReleaseSummaryDrift(t *testing.T) {
	bundle := releaseBundleWithWorkflowFixture(t)
	output, ok := bundle.RunExport.Tasks[0].Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow parent output = %T, want map", bundle.RunExport.Tasks[0].Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[2]["release_gate_input_id"] = "devagent:run-1:release_gate:forged"

	results := CheckReleaseBundleConformance(context.Background(), ReleaseBundleConformanceHarness{Bundle: bundle})
	if !hasFailedReleaseBundleConformanceCase(results, "workflow-release-alignment") {
		t.Fatalf("CheckReleaseBundleConformance() did not report workflow release summary drift: %+v", results)
	}
}

func TestCheckReleaseBundleConformanceReportsWorkflowReleaseCountDrift(t *testing.T) {
	bundle := releaseBundleWithWorkflowFixture(t)
	output, ok := bundle.RunExport.Tasks[0].Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow parent output = %T, want map", bundle.RunExport.Tasks[0].Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[2]["input_count"] = 0

	results := CheckReleaseBundleConformance(context.Background(), ReleaseBundleConformanceHarness{Bundle: bundle})
	if !hasFailedReleaseBundleConformanceCase(results, "workflow-release-alignment") {
		t.Fatalf("CheckReleaseBundleConformance() did not report workflow release count drift: %+v", results)
	}
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

func releaseBundleWithWorkflowFixture(t *testing.T) ReleaseBundle {
	t.Helper()

	input := validReleaseBundleInput(t)
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  input.Export.IDs,
		Name: "release workflow",
		Actions: []ProcessInput{
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeAnalyze,
					Action: ActionAnalyze,
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not enter release conformance\n",
					Files: []PatchFile{
						{Path: "templates/devagent/release_bundle_conformance_test.go", Intent: "cover workflow release conformance"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Review: input.Review,
				Gate:   &input.Gate,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	process, err := WorkflowActionProcessRecords(workflow, 3)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}
	input.Process = process
	input.Export.Tasks = append([]gopact.TaskRecord{workflow.Task}, workflow.Tasks...)
	input.Export.Inputs = workflow.Inputs
	input.Export.Interventions = workflow.Interventions

	bundle, err := BuildReleaseBundle(input)
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	return bundle
}
