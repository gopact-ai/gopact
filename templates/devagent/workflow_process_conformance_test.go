package devagent

import (
	"context"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckWorkflowProcessConformancePassesObservedWorkflowRecords(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	harness := WorkflowProcessConformanceHarness{
		Records: records,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionRelease,
		},
		RequiredInputSources: []string{
			"devagent.patch",
			"devagent.release_gate",
		},
	}

	results := CheckWorkflowProcessConformance(context.Background(), harness)
	if failed := failedWorkflowProcessConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckWorkflowProcessConformance() failed cases: %v", failed)
	}
	RequireWorkflowProcessConformance(t, harness)
}

func TestCheckWorkflowProcessConformanceReportsMissingRequiredAction(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	harness := WorkflowProcessConformanceHarness{
		Records: records,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionApplyPatch,
			ActionRelease,
		},
	}

	results := CheckWorkflowProcessConformance(context.Background(), harness)
	if !hasFailedWorkflowProcessConformanceCase(results, "required-actions") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report missing action sequence: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsBrokenParentChildLink(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	records.Tasks[1].ParentID = ""

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "parent-child-links") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report broken parent link: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsFailedSummaryDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	output["failed_action_count"] = 0

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "failure-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report failed summary drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsWorkflowCountDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	output["input_count"] = 1

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report workflow input count drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsChildSummaryCountDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[2]["intervention_count"] = 0

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child summary intervention count drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsChildSummaryActionDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[1]["action"] = string(ActionRelease)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child summary action drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsChildSummaryActionStatusDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[1]["action_status"] = string(ActionAllowed)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child summary action status drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsReleaseSummaryBoundaryDrift(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  string
	}{
		{name: "release gate input id", key: "release_gate_input_id"},
		{name: "review intervention id", key: "review_intervention_id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			records := workflowProcessConformanceFixture(t)
			output, ok := records.Task.Output.(map[string]any)
			if !ok {
				t.Fatalf("workflow output = %T, want map", records.Task.Output)
			}
			summaries, err := workflowProcessActionSummaries(output)
			if err != nil {
				t.Fatalf("workflowProcessActionSummaries() error = %v", err)
			}
			delete(summaries[2], tt.key)

			results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
			if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
				t.Fatalf("CheckWorkflowProcessConformance() did not report release summary boundary drift: %+v", results)
			}
		})
	}
}

func TestCheckWorkflowProcessConformanceReportsReleaseWithoutGateInput(t *testing.T) {
	records := workflowProcessConformanceReleaseWithoutGateFixture(t)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "release-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report release without gate input: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsReleaseWithoutResolvedReview(t *testing.T) {
	records := workflowProcessConformanceReleaseWithoutReviewFixture(t)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "release-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report release without resolved review: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsRawDiffLeak(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	value, ok := records.Inputs[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("patch input value = %T, want map", records.Inputs[0].Value)
	}
	value["diff"] = "diff --git a/secret b/secret\n+raw diff must not leak\n"

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "input-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report raw diff leak: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsInputActionIndexDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	records.Inputs[0].Metadata["workflow_action_index"] = len(records.Tasks) + 1

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "input-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report input action index drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsInterventionActionMetadataDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	records.Interventions[0].Metadata["action"] = string(ActionApplyPatch)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "intervention-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report intervention action metadata drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := CheckWorkflowProcessConformance(ctx, WorkflowProcessConformanceHarness{
		Records: workflowProcessConformanceFixture(t),
	})
	if !hasFailedWorkflowProcessConformanceCase(results, "context") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report canceled context: %+v", results)
	}
}

func workflowProcessConformanceFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name: "self-bootstrap release workflow",
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
					Summary: "document workflow process conformance",
					Diff:    "diff --git a/secret b/secret\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_conformance_test.go", Intent: "cover workflow process conformance"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Gate: &GateResult{
					Status:       GatePassed,
					Mode:         ModeWrite,
					ReportStatus: gopact.VerificationStatusPassed,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
					Summary:  "workflow process is complete",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	return records
}

func workflowProcessConformanceReleaseWithoutGateFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name: "self-bootstrap release without gate",
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
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	return records
}

func workflowProcessConformanceReleaseWithoutReviewFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name: "self-bootstrap release without review",
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
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Gate: &GateResult{
					Status:       GatePassed,
					Mode:         ModeWrite,
					ReportStatus: gopact.VerificationStatusPassed,
					ReviewStatus: ReviewApproved,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	return records
}

func workflowProcessConformanceRejectedFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name: "self-bootstrap rejected workflow",
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
					Status: ActionRejected,
					Mode:   ModeWrite,
					Action: ActionApplyPatch,
					Reasons: []string{
						"observed diff is required",
					},
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "attempt write",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_conformance_test.go", Intent: "cover rejected workflow"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	return records
}

func failedWorkflowProcessConformanceCases(results []WorkflowProcessConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedWorkflowProcessConformanceCase(results []WorkflowProcessConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}
