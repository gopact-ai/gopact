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

func TestCheckWorkflowProcessConformanceReportsDuplicateChildTaskID(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	records.Tasks[1].ID = records.Tasks[0].ID
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[1]["task_id"] = records.Tasks[1].ID

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "child-task-ids") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report duplicate child task id: %+v", results)
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

func TestCheckWorkflowProcessConformanceReportsWorkflowTaskInputDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	input, ok := records.Task.Input.(map[string]any)
	if !ok {
		t.Fatalf("workflow input = %T, want map", records.Task.Input)
	}
	input["action_count"] = 1

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-task-io") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report workflow task input drift: %+v", results)
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

func TestCheckWorkflowProcessConformanceReportsChildSummaryReasonCountDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[1]["reason_count"] = 0

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child summary reason count drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsChildTaskInputDrift(t *testing.T) {
	records := workflowProcessConformanceFixture(t)
	input, ok := records.Tasks[1].Input.(map[string]any)
	if !ok {
		t.Fatalf("child task input = %T, want map", records.Tasks[1].Input)
	}
	input["mode"] = string(ModeAnalyze)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "child-task-io") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child task input drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsChildTaskOutputDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedFixture(t)
	output, ok := records.Tasks[1].Output.(map[string]any)
	if !ok {
		t.Fatalf("child task output = %T, want map", records.Tasks[1].Output)
	}
	output["status"] = string(ActionAllowed)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "child-task-io") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child task output drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsChildTaskReasonCountDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedFixture(t)
	input, ok := records.Tasks[1].Input.(map[string]any)
	if !ok {
		t.Fatalf("child task input = %T, want map", records.Tasks[1].Input)
	}
	input["reason_count"] = 0

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "child-task-io") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report child task reason count drift: %+v", results)
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

func TestCheckWorkflowProcessConformancePassesRejectedReleaseReviewBoundary(t *testing.T) {
	records := workflowProcessConformanceRejectedReleaseFixture(t)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if failed := failedWorkflowProcessConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckWorkflowProcessConformance() failed cases: %v", failed)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{Records: records})
}

func TestCheckWorkflowProcessConformanceReportsRejectedReleaseWithoutReview(t *testing.T) {
	records := workflowProcessConformanceRejectedReleaseWithoutReviewFixture(t)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "release-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report rejected release without review: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsRejectedReleaseReviewStatusDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedReleaseFixture(t)
	records.Interventions[0].Metadata["review_status"] = string(ReviewApproved)

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "release-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report rejected release review status drift: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsRejectedReleaseSummaryBoundaryDrift(t *testing.T) {
	records := workflowProcessConformanceRejectedReleaseFixture(t)
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[1]["review_intervention_id"] = "devagent:run-1:review:forged"

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "workflow-summary") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report rejected release summary boundary drift: %+v", results)
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

func TestCheckWorkflowProcessConformanceReportsBoundaryTaskIDDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(WorkflowRecords) WorkflowRecords
		want string
	}{
		{
			name: "input",
			edit: func(records WorkflowRecords) WorkflowRecords {
				records.Inputs[0].Metadata["workflow_task_id"] = records.Tasks[0].ID
				return records
			},
			want: "input-boundaries",
		},
		{
			name: "intervention",
			edit: func(records WorkflowRecords) WorkflowRecords {
				records.Interventions[0].Metadata["workflow_task_id"] = records.Tasks[0].ID
				return records
			},
			want: "intervention-boundaries",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := tt.edit(workflowProcessConformanceFixture(t))

			results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
			if !hasFailedWorkflowProcessConformanceCase(results, tt.want) {
				t.Fatalf("CheckWorkflowProcessConformance() did not report boundary task id drift: %+v", results)
			}
		})
	}
}

func TestCheckWorkflowProcessConformanceReportsResumeInputWithoutReviewResume(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
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
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
				Resume: &gopact.ResumeRequest{
					InterruptID: "approval-1",
					Payload: map[string]any{
						"decision": "approved",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	records.Interventions[0].Resume = nil

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "input-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report orphaned resume input: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsReviewResumeWithoutResumeInput(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
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
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
				Resume: &gopact.ResumeRequest{
					InterruptID: "approval-1",
					Payload: map[string]any{
						"decision": "approved",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	records.Inputs = records.Inputs[:1]
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	output["input_count"] = 1
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	summaries[1]["input_count"] = 1

	results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
	if !hasFailedWorkflowProcessConformanceCase(results, "intervention-boundaries") {
		t.Fatalf("CheckWorkflowProcessConformance() did not report orphaned review resume: %+v", results)
	}
}

func TestCheckWorkflowProcessConformanceReportsApplyResumeSummaryBoundaryDrift(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  string
	}{
		{name: "resume input id", key: "resume_input_id"},
		{name: "review intervention id", key: "review_intervention_id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			records := workflowProcessApplyResumeFixture(t)
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
				t.Fatalf("CheckWorkflowProcessConformance() did not report apply resume summary drift: %+v", results)
			}
		})
	}
}

func TestCheckWorkflowProcessConformanceReportsResumeMetadataDrift(t *testing.T) {
	setResumeInputMetadata := func(key, value string) func(WorkflowRecords) WorkflowRecords {
		return func(records WorkflowRecords) WorkflowRecords {
			for i := range records.Inputs {
				if records.Inputs[i].Kind == gopact.InputResume {
					records.Inputs[i].Metadata[key] = value
					return records
				}
			}
			t.Fatal("fixture missing resume input")
			return records
		}
	}
	setReviewInterventionMetadata := func(key, value string) func(WorkflowRecords) WorkflowRecords {
		return func(records WorkflowRecords) WorkflowRecords {
			records.Interventions[0].Metadata[key] = value
			return records
		}
	}

	tests := []struct {
		name string
		edit func(WorkflowRecords) WorkflowRecords
		want string
	}{
		{
			name: "resume input checkpoint",
			edit: setResumeInputMetadata("resume_checkpoint_id", "checkpoint-other"),
			want: "input-boundaries",
		},
		{
			name: "resume input step",
			edit: setResumeInputMetadata("resume_step_id", "step-other"),
			want: "input-boundaries",
		},
		{
			name: "resume input payload codec",
			edit: setResumeInputMetadata("resume_payload_codec", "text/plain"),
			want: "input-boundaries",
		},
		{
			name: "review intervention checkpoint",
			edit: setReviewInterventionMetadata("resume_checkpoint_id", "checkpoint-other"),
			want: "intervention-boundaries",
		},
		{
			name: "review intervention step",
			edit: setReviewInterventionMetadata("resume_step_id", "step-other"),
			want: "intervention-boundaries",
		},
		{
			name: "review intervention payload codec",
			edit: setReviewInterventionMetadata("resume_payload_codec", "text/plain"),
			want: "intervention-boundaries",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := tt.edit(workflowProcessApplyResumeFixture(t))

			results := CheckWorkflowProcessConformance(context.Background(), WorkflowProcessConformanceHarness{Records: records})
			if !hasFailedWorkflowProcessConformanceCase(results, tt.want) {
				t.Fatalf("CheckWorkflowProcessConformance() did not report resume metadata drift: %+v", results)
			}
		})
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

func workflowProcessApplyResumeFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"}
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  ids,
		Name: "self-bootstrap apply resume workflow",
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
					Summary: "prepare resumable apply",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_conformance_test.go", Intent: "cover apply resume summary"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionApplyPatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare resumable apply",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_conformance_test.go", Intent: "cover apply resume summary"},
					},
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
					Summary:  "apply approved through policy review",
				},
				Resume: &gopact.ResumeRequest{
					CheckpointID: "checkpoint-apply-1",
					StepID:       "devagent:run-1:step:devagent.apply_patch",
					InterruptID:  "approval-1",
					IDs:          ids,
					Payload: map[string]any{
						"decision": "approved",
					},
					PayloadCodec: "application/json",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	return records
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

func workflowProcessConformanceRejectedReleaseFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name: "self-bootstrap rejected release workflow",
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
					Action: ActionRelease,
					Reasons: []string{
						"release gate rejected: review status rejected",
					},
				},
				Gate: &GateResult{
					Status:       GateRejected,
					Mode:         ModeWrite,
					ReportStatus: gopact.VerificationStatusPassed,
					ReviewStatus: ReviewRejected,
				},
				Review: ReviewDecision{
					Status:   ReviewRejected,
					Reviewer: "human",
					Summary:  "release needs another plan pass",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	return records
}

func workflowProcessConformanceRejectedReleaseWithoutReviewFixture(t *testing.T) WorkflowRecords {
	t.Helper()

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name: "self-bootstrap rejected release without review",
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
					Action: ActionRelease,
					Reasons: []string{
						"release gate rejected: review status rejected",
					},
				},
				Gate: &GateResult{
					Status:       GateRejected,
					Mode:         ModeWrite,
					ReportStatus: gopact.VerificationStatusPassed,
					ReviewStatus: ReviewRejected,
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
