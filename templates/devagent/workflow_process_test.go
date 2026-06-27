package devagent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBuildWorkflowProcessRecordsGroupsObservedActions(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 16, 0, 0, 0, time.UTC)

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name:      "docs release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5"},
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
					Summary: "document workflow process records",
					Diff:    "diff --git a/secret b/secret\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "docs/design/development-plan.md", Intent: "document workflow process records"},
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
					Summary:  "safe docs/test change",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	if records.Task.ID != "devagent:run-1:workflow" ||
		records.Task.Name != "docs release workflow" ||
		records.Task.Status != gopact.TaskCompleted ||
		!records.Task.CreatedAt.Equal(createdAt) {
		t.Fatalf("workflow task = %+v, want completed workflow task", records.Task)
	}
	if records.Task.Metadata["source"] != "devagent" ||
		records.Task.Metadata["scope"] != "m5" ||
		records.Task.Metadata["action_count"] != 3 ||
		records.Task.Metadata["failed_action_count"] != 0 {
		t.Fatalf("workflow metadata = %+v, want workflow summary metadata", records.Task.Metadata)
	}
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	actionSummaries, ok := output["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow output actions = %T, want []map[string]any", output["actions"])
	}
	if len(actionSummaries) != 3 {
		t.Fatalf("workflow output actions = %+v, want 3 summaries", actionSummaries)
	}
	for i, summary := range actionSummaries {
		if summary["index"] != i+1 ||
			summary["task_id"] == "" ||
			summary["status"] != string(gopact.TaskCompleted) ||
			summary["input_count"] == nil ||
			summary["intervention_count"] == nil {
			t.Fatalf("workflow action summary %d = %+v, want stable child summary", i, summary)
		}
	}
	if actionSummaries[0]["action"] != string(ActionAnalyze) ||
		actionSummaries[1]["action"] != string(ActionProposePatch) ||
		actionSummaries[2]["action"] != string(ActionRelease) {
		t.Fatalf("workflow output actions = %+v, want observed action order", actionSummaries)
	}
	if actionSummaries[2]["release_gate_input_id"] != "devagent:run-1:release_gate" ||
		actionSummaries[2]["review_intervention_id"] != "devagent:run-1:review:human" {
		t.Fatalf("release action summary = %+v, want gate input and review intervention ids", actionSummaries[2])
	}
	if len(records.Tasks) != 3 {
		t.Fatalf("child tasks = %+v, want 3 child tasks", records.Tasks)
	}
	for _, task := range records.Tasks {
		if task.ParentID != records.Task.ID {
			t.Fatalf("child task parent = %q, want %q", task.ParentID, records.Task.ID)
		}
		if task.IDs.RunID != "run-1" || task.IDs.ThreadID != "thread-1" || task.IDs.UserID != "user-1" {
			t.Fatalf("child task ids = %+v, want workflow runtime ids", task.IDs)
		}
	}
	for i, task := range records.Tasks {
		if task.Metadata["workflow_id"] != records.Task.ID ||
			task.Metadata["workflow_action_index"] != i+1 ||
			task.Metadata["workflow_action_count"] != 3 {
			t.Fatalf("child task metadata = %+v, want stable workflow position", task.Metadata)
		}
	}
	if len(records.Inputs) != 2 {
		t.Fatalf("inputs = %+v, want patch and release gate inputs", records.Inputs)
	}
	if records.Inputs[0].Source != "devagent.patch" || records.Inputs[1].Source != "devagent.release_gate" {
		t.Fatalf("inputs = %+v, want patch then release gate", records.Inputs)
	}
	for _, input := range records.Inputs {
		if input.Metadata["workflow_id"] != records.Task.ID ||
			input.Metadata["workflow_action_count"] != 3 {
			t.Fatalf("input metadata = %+v, want workflow identity", input.Metadata)
		}
	}
	patchValue, ok := records.Inputs[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("patch input value = %T, want map", records.Inputs[0].Value)
	}
	if strings.Contains(toString(patchValue["diff"]), "raw diff") {
		t.Fatalf("patch input leaked raw diff: %+v", patchValue)
	}
	if len(records.Interventions) != 1 ||
		records.Interventions[0].Status != gopact.InterventionResolved ||
		records.Interventions[0].Metadata["reviewer"] != "human" {
		t.Fatalf("interventions = %+v, want resolved review intervention", records.Interventions)
	}
}

func TestBuildWorkflowProcessRecordsDisambiguatesRepeatedActionTaskIDs(t *testing.T) {
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare first patch",
					Files: []PatchFile{
						{Path: "docs/design/templates.md", Intent: "document first patch"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-2",
					Summary: "prepare second patch",
					Files: []PatchFile{
						{Path: "docs/design/development-plan.md", Intent: "document second patch"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	if records.Tasks[1].ID == records.Tasks[2].ID {
		t.Fatalf("repeated action task ids = %q, want stable disambiguated ids", records.Tasks[1].ID)
	}

	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	actionSummaries, ok := output["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow output actions = %T, want []map[string]any", output["actions"])
	}
	if actionSummaries[1]["task_id"] != records.Tasks[1].ID ||
		actionSummaries[2]["task_id"] != records.Tasks[2].ID {
		t.Fatalf("workflow action summaries = %+v, want disambiguated child task ids", actionSummaries)
	}

	process, err := WorkflowActionProcessRecordsByTaskID(records, records.Tasks[2].ID)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsByTaskID(second patch) error = %v", err)
	}
	if process.Task.ID != records.Tasks[2].ID {
		t.Fatalf("process task = %+v, want second patch child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.patch" {
		t.Fatalf("process inputs = %+v, want second patch input", process.Inputs)
	}
	patchValue, ok := process.Inputs[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("patch input value = %T, want map", process.Inputs[0].Value)
	}
	if patchValue["id"] != "patch-2" {
		t.Fatalf("patch input value = %+v, want second patch", patchValue)
	}
}

func TestBuildWorkflowProcessRecordsStampsWorkflowTaskIDOnBoundaries(t *testing.T) {
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare first patch",
					Files: []PatchFile{
						{Path: "docs/design/templates.md", Intent: "document first patch"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-2",
					Summary: "prepare second patch",
					Files: []PatchFile{
						{Path: "docs/design/development-plan.md", Intent: "document second patch"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	for _, input := range records.Inputs {
		index, ok := workflowProcessIntMetadata(input.Metadata, "workflow_action_index")
		if !ok {
			t.Fatalf("input metadata = %+v, want workflow_action_index", input.Metadata)
		}
		if input.Metadata["workflow_task_id"] != records.Tasks[index-1].ID {
			t.Fatalf("input metadata = %+v, want workflow_task_id %q", input.Metadata, records.Tasks[index-1].ID)
		}
	}
	for _, intervention := range records.Interventions {
		index, ok := workflowProcessIntMetadata(intervention.Metadata, "workflow_action_index")
		if !ok {
			t.Fatalf("intervention metadata = %+v, want workflow_action_index", intervention.Metadata)
		}
		if intervention.Metadata["workflow_task_id"] != records.Tasks[index-1].ID {
			t.Fatalf(
				"intervention metadata = %+v, want workflow_task_id %q",
				intervention.Metadata,
				records.Tasks[index-1].ID,
			)
		}
	}
}

func TestBuildWorkflowProcessRecordsPreservesActionMetadataOnChildBoundaries(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1"},
		Name: "self-bootstrap release workflow",
		Actions: []ProcessInput{
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeAnalyze,
					Action: ActionAnalyze,
					Metadata: map[string]any{
						"prompt_id": "analyze-prompt-v1",
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
					Metadata: map[string]any{
						"prompt_id": "release-prompt-v1",
						"eval_id":   "release-eval-v1",
					},
				},
				Gate: &GateResult{
					Status:       GatePassed,
					Mode:         ModeWrite,
					ReportStatus: gopact.VerificationStatusPassed,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	if len(records.Tasks) != 2 {
		t.Fatalf("child tasks = %+v, want analyze and release tasks", records.Tasks)
	}
	if records.Tasks[0].Metadata["prompt_id"] != "analyze-prompt-v1" {
		t.Fatalf("analyze task metadata = %+v, want action prompt id", records.Tasks[0].Metadata)
	}
	if records.Tasks[1].Metadata["prompt_id"] != "release-prompt-v1" ||
		records.Tasks[1].Metadata["eval_id"] != "release-eval-v1" {
		t.Fatalf("release task metadata = %+v, want action governance metadata", records.Tasks[1].Metadata)
	}
	if len(records.Inputs) != 1 || records.Inputs[0].Metadata["prompt_id"] != "release-prompt-v1" {
		t.Fatalf("release gate input metadata = %+v, want action prompt id", records.Inputs)
	}
	if len(records.Interventions) != 1 || records.Interventions[0].Metadata["eval_id"] != "release-eval-v1" {
		t.Fatalf("review intervention metadata = %+v, want action eval id", records.Interventions)
	}
}

func TestBuildWorkflowProcessRecordsCarriesReviewResumeInput(t *testing.T) {
	createdAt := time.Date(2026, 6, 26, 14, 30, 0, 0, time.UTC)
	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"}

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap resumed release workflow",
		CreatedAt: createdAt,
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
					Summary:  "resume approved through lark",
				},
				Resume: &gopact.ResumeRequest{
					CheckpointID: "checkpoint-1",
					StepID:       "release-gate",
					InterruptID:  "approval-1",
					IDs:          ids,
					Payload: map[string]any{
						"decision": "approved",
					},
					PayloadCodec: "application/json",
					CreatedAt:    createdAt.Add(time.Minute),
					Metadata:     map[string]any{"channel": "lark"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	if len(records.Inputs) != 2 {
		t.Fatalf("inputs = %+v, want release gate and resume inputs", records.Inputs)
	}
	resumeInput := records.Inputs[1]
	if resumeInput.Kind != gopact.InputResume ||
		resumeInput.Source != "devagent.review_resume" ||
		resumeInput.Resume == nil ||
		resumeInput.Resume.InterruptID != "approval-1" ||
		resumeInput.Metadata["workflow_id"] != records.Task.ID ||
		resumeInput.Metadata["workflow_action_index"] != 2 ||
		resumeInput.Metadata["resume_interrupt_id"] != "approval-1" {
		t.Fatalf("resume input = %+v, want workflow-scoped resume input", resumeInput)
	}
	if len(records.Interventions) != 1 ||
		records.Interventions[0].Resume == nil ||
		records.Interventions[0].Resume.InterruptID != "approval-1" {
		t.Fatalf("interventions = %+v, want review intervention linked to resume", records.Interventions)
	}
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	actionSummaries, ok := output["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow output actions = %T, want []map[string]any", output["actions"])
	}
	if actionSummaries[1]["input_count"] != 2 ||
		actionSummaries[1]["intervention_count"] != 1 {
		t.Fatalf("release action summary = %+v, want gate + resume inputs and review intervention", actionSummaries[1])
	}

	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records:              records,
		RequiredActions:      []ActionKind{ActionAnalyze, ActionRelease},
		RequiredInputSources: []string{"devagent.release_gate", "devagent.review_resume"},
	})

	process, err := WorkflowActionProcessRecords(records, 2)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}
	if len(process.Inputs) != 2 ||
		process.Inputs[1].Kind != gopact.InputResume ||
		process.Inputs[1].Resume == nil ||
		process.Inputs[1].Resume.InterruptID != "approval-1" {
		t.Fatalf("process inputs = %+v, want release action resume input", process.Inputs)
	}
}

func TestBuildWorkflowProcessRecordsSummarizesApplyResumeBoundary(t *testing.T) {
	createdAt := time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC)
	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"}

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap resumed apply workflow",
		CreatedAt: createdAt,
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
				Patch: selfBootstrapPatchProposal("resume apply after policy approval"),
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionApplyPatch,
				},
				Patch: selfBootstrapPatchProposal("resume apply after policy approval"),
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
					CreatedAt:    createdAt.Add(time.Minute),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	actionSummaries, ok := output["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow output actions = %T, want []map[string]any", output["actions"])
	}
	apply := actionSummaries[2]
	if apply["action"] != string(ActionApplyPatch) ||
		apply["input_count"] != 2 ||
		apply["intervention_count"] != 1 ||
		apply["resume_input_id"] != "devagent:run-1:resume:approval-1" ||
		apply["review_intervention_id"] != "devagent:run-1:review:human" {
		t.Fatalf("apply action summary = %+v, want patch, resume, and review boundary ids", apply)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: records,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionApplyPatch,
		},
		RequiredInputSources: []string{
			"devagent.patch",
			"devagent.review_resume",
		},
	})
}

func TestBuildWorkflowProcessRecordsSummarizesRejectedChildAction(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Name: "self-bootstrap workflow",
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
						"write mode requires observed diff",
					},
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "attempt core edit",
					Diff:    "diff --git a/secret b/secret\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover rejected workflow process"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	if records.Task.Status != gopact.TaskFailed ||
		records.Task.Metadata["failed_action_count"] != 1 {
		t.Fatalf("workflow task = %+v, want failed parent with one failed action", records.Task)
	}
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	if output["failed_action_count"] != 1 || output["action_count"] != 2 {
		t.Fatalf("workflow output = %+v, want action and failed counts", output)
	}
	actionSummaries, ok := output["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow output actions = %T, want []map[string]any", output["actions"])
	}
	if len(actionSummaries) != 2 {
		t.Fatalf("workflow output actions = %+v, want 2 child summaries", actionSummaries)
	}
	rejected := actionSummaries[1]
	if rejected["index"] != 2 ||
		rejected["status"] != string(gopact.TaskFailed) ||
		rejected["mode"] != string(ModeWrite) ||
		rejected["action"] != string(ActionApplyPatch) ||
		rejected["action_status"] != string(ActionRejected) ||
		rejected["reason_count"] != 1 ||
		rejected["input_count"] != 1 ||
		rejected["intervention_count"] != 0 {
		t.Fatalf("rejected action summary = %+v, want failed write/apply_patch summary", rejected)
	}
	if len(records.Tasks) != 2 ||
		records.Tasks[1].Status != gopact.TaskFailed ||
		records.Tasks[1].Metadata["workflow_action_index"] != 2 ||
		records.Tasks[1].Metadata["action_status"] != string(ActionRejected) {
		t.Fatalf("child tasks = %+v, want rejected second action task", records.Tasks)
	}
	if len(records.Inputs) != 1 || records.Inputs[0].Source != "devagent.patch" {
		t.Fatalf("inputs = %+v, want one sanitized patch input", records.Inputs)
	}
	patchValue, ok := records.Inputs[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("patch input value = %T, want map", records.Inputs[0].Value)
	}
	if strings.Contains(toString(patchValue["diff"]), "raw diff") ||
		patchValue["file_count"] != 1 ||
		patchValue["has_diff"] != true {
		t.Fatalf("patch input value = %+v, want sanitized patch summary", patchValue)
	}
}

func TestBuildWorkflowProcessRecordsSummarizesInterruptedChildAction(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 17, 0, 0, 0, time.UTC)
	pending := gopact.InterruptRecord{
		ID:         "approval-1",
		Type:       gopact.InterruptApproval,
		Reason:     "release approval is pending",
		RequiredBy: "devagent.release_gate",
		Prompt: gopact.Message{
			Role:    gopact.RoleAssistant,
			Content: "Review the proposed self-bootstrap release.",
		},
		CreatedAt: createdAt,
	}

	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name:      "self-bootstrap interrupted release workflow",
		CreatedAt: createdAt,
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
					Status: ActionInterrupted,
					Mode:   ModeWrite,
					Action: ActionRelease,
					Reasons: []string{
						"release approval is pending",
					},
				},
				Gate: &GateResult{
					Status:       GatePending,
					Mode:         ModeWrite,
					ReportStatus: gopact.VerificationStatusPassed,
					Reasons: []string{
						"release approval is pending",
					},
				},
				Pending: &pending,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	if records.Task.Status != gopact.TaskInterrupted ||
		records.Task.Metadata["failed_action_count"] != 0 ||
		records.Task.Metadata["interrupted_action_count"] != 1 {
		t.Fatalf("workflow task = %+v, want interrupted parent with one interrupted action", records.Task)
	}
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", records.Task.Output)
	}
	if output["failed_action_count"] != 0 ||
		output["interrupted_action_count"] != 1 ||
		output["action_count"] != 2 {
		t.Fatalf("workflow output = %+v, want action, failed, and interrupted counts", output)
	}
	actionSummaries, ok := output["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow output actions = %T, want []map[string]any", output["actions"])
	}
	interrupted := actionSummaries[1]
	if interrupted["index"] != 2 ||
		interrupted["status"] != string(gopact.TaskInterrupted) ||
		interrupted["mode"] != string(ModeWrite) ||
		interrupted["action"] != string(ActionRelease) ||
		interrupted["action_status"] != string(ActionInterrupted) ||
		interrupted["reason_count"] != 1 ||
		interrupted["input_count"] != 1 ||
		interrupted["intervention_count"] != 1 ||
		interrupted["release_gate_input_id"] == "" ||
		interrupted["review_intervention_id"] == "" {
		t.Fatalf("interrupted action summary = %+v, want interrupted release summary", interrupted)
	}
	if len(records.Tasks) != 2 ||
		records.Tasks[1].Status != gopact.TaskInterrupted ||
		records.Tasks[1].Metadata["action_status"] != string(ActionInterrupted) {
		t.Fatalf("child tasks = %+v, want interrupted release child task", records.Tasks)
	}
	if len(records.Inputs) != 1 || records.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("inputs = %+v, want one release gate input", records.Inputs)
	}
	if len(records.Interventions) != 1 ||
		records.Interventions[0].Status != gopact.InterventionRequested ||
		records.Interventions[0].Request == nil ||
		records.Interventions[0].Request.ID != "approval-1" {
		t.Fatalf("interventions = %+v, want requested approval intervention", records.Interventions)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records:              records,
		RequiredActions:      []ActionKind{ActionAnalyze, ActionRelease},
		RequiredInputSources: []string{"devagent.release_gate"},
	})
}

func TestRecordWorkflowProcessRecordsAppendsParentAndChildren(t *testing.T) {
	recorder := gopact.NewRunRecorder()

	if err := RecordWorkflowProcessRecords(recorder, WorkflowInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
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
						"verification failed",
					},
				},
				Gate: &GateResult{
					Status: GateRejected,
					Mode:   ModeWrite,
				},
			},
		},
	}); err != nil {
		t.Fatalf("RecordWorkflowProcessRecords() error = %v", err)
	}
	if err := recorder.Record(gopact.Event{Type: gopact.EventRunFailed, IDs: gopact.RuntimeIDs{RunID: "run-1"}}); err != nil {
		t.Fatalf("Record(run failed) error = %v", err)
	}

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.Tasks) != 3 {
		t.Fatalf("export tasks = %+v, want parent and two child tasks", export.Tasks)
	}
	if export.Tasks[0].ID != "devagent:run-1:workflow" || export.Tasks[0].Status != gopact.TaskFailed {
		t.Fatalf("parent task = %+v, want failed workflow parent", export.Tasks[0])
	}
	if export.Tasks[1].ParentID != export.Tasks[0].ID || export.Tasks[2].ParentID != export.Tasks[0].ID {
		t.Fatalf("export tasks = %+v, want child tasks linked to parent", export.Tasks)
	}
	if len(export.Inputs) != 1 || export.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("export inputs = %+v, want release gate input", export.Inputs)
	}
}

func TestWorkflowRecordsFromRunExportRestoresWorkflowProcessRecords(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Name:      "self-bootstrap export workflow",
		CreatedAt: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
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
					Summary: "restore workflow records from run export",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover export restore"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionRejected,
					Mode:   ModeWrite,
					Action: ActionApplyPatch,
					Reasons: []string{
						"policy allow decision is required",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Tasks: []gopact.TaskRecord{
			records.Tasks[2],
			records.Task,
			records.Tasks[0],
			records.Tasks[1],
		},
		Inputs:        records.Inputs,
		Interventions: records.Interventions,
	}

	restored, err := WorkflowRecordsFromRunExport(export, "")
	if err != nil {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v", err)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records:              restored,
		RequiredActions:      []ActionKind{ActionAnalyze, ActionProposePatch, ActionApplyPatch},
		RequiredInputSources: []string{"devagent.patch"},
	})
	if restored.Tasks[0].Metadata["action"] != string(ActionAnalyze) ||
		restored.Tasks[1].Metadata["action"] != string(ActionProposePatch) ||
		restored.Tasks[2].Metadata["action"] != string(ActionApplyPatch) {
		t.Fatalf("restored tasks = %+v, want workflow action order", restored.Tasks)
	}

	restored.Task.Metadata["mutated"] = true
	if _, ok := export.Tasks[1].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowRecordsFromRunExport returned records sharing export metadata")
	}
}

func TestWorkflowRecordsFromRunExportRejectsWorkflowWithoutChildren(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Actions: []ProcessInput{
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeAnalyze,
					Action: ActionAnalyze,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	_, err = WorkflowRecordsFromRunExport(gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Tasks:   []gopact.TaskRecord{records.Task},
	}, "")
	if err == nil {
		t.Fatal("WorkflowRecordsFromRunExport() error = nil, want missing child task error")
	}
	if !strings.Contains(err.Error(), `workflow task "devagent:run-1:workflow" has no child tasks`) {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v, want missing child task error", err)
	}
}

func TestWorkflowRecordsFromRunExportRejectsNonConformantWorkflowRecords(t *testing.T) {
	records, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Actions: []ProcessInput{
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeAnalyze,
					Action: ActionAnalyze,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	child := records.Tasks[0]
	child.Metadata["workflow_id"] = "devagent:other-run:workflow"
	_, err = WorkflowRecordsFromRunExport(gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Tasks: []gopact.TaskRecord{
			records.Task,
			child,
		},
	}, "")
	if !errors.Is(err, ErrWorkflowProcessConformanceFailed) {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v, want ErrWorkflowProcessConformanceFailed", err)
	}
	if !strings.Contains(err.Error(), `workflow process records failed conformance case "parent-child-links"`) {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v, want parent-child conformance error", err)
	}
}

func TestWorkflowActionProcessRecordsReturnsOnlyMatchingActionBoundaries(t *testing.T) {
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover action process extraction"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	process, err := WorkflowActionProcessRecords(records, 3)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}
	if process.Task.ID != records.Tasks[2].ID {
		t.Fatalf("process task = %+v, want release child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want only release gate input", process.Inputs)
	}
	if len(process.Interventions) != 1 || process.Interventions[0].Type != gopact.InterruptApproval {
		t.Fatalf("process interventions = %+v, want only review intervention", process.Interventions)
	}
	process.Inputs[0].Metadata["mutated"] = true
	if _, ok := records.Inputs[1].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowActionProcessRecords returned records sharing input metadata")
	}
}

func TestWorkflowActionProcessRecordsByActionReturnsMatchingAction(t *testing.T) {
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover action kind extraction"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	process, err := WorkflowActionProcessRecordsByAction(records, ActionRelease)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsByAction() error = %v", err)
	}
	if process.Task.ID != records.Tasks[2].ID {
		t.Fatalf("process task = %+v, want release child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want only release gate input", process.Inputs)
	}
	if len(process.Interventions) != 1 || process.Interventions[0].Type != gopact.InterruptApproval {
		t.Fatalf("process interventions = %+v, want only review intervention", process.Interventions)
	}
	process.Inputs[0].Metadata["mutated"] = true
	if _, ok := records.Inputs[1].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowActionProcessRecordsByAction returned records sharing input metadata")
	}
}

func TestWorkflowActionProcessRecordsByActionRejectsDuplicateAction(t *testing.T) {
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare first patch",
					Files: []PatchFile{
						{Path: "docs/design/templates.md", Intent: "document first patch"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-2",
					Summary: "prepare second patch",
					Files: []PatchFile{
						{Path: "docs/design/development-plan.md", Intent: "document second patch"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	_, err = WorkflowActionProcessRecordsByAction(records, ActionProposePatch)
	if err == nil {
		t.Fatal("WorkflowActionProcessRecordsByAction() error = nil, want duplicate action error")
	}
	if !strings.Contains(err.Error(), "duplicate workflow action") {
		t.Fatalf("WorkflowActionProcessRecordsByAction() error = %v, want duplicate workflow action", err)
	}
}

func TestWorkflowActionProcessRecordsByTaskIDReturnsMatchingAction(t *testing.T) {
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover task id action extraction"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	process, err := WorkflowActionProcessRecordsByTaskID(records, records.Tasks[2].ID)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsByTaskID() error = %v", err)
	}
	if process.Task.ID != records.Tasks[2].ID {
		t.Fatalf("process task = %+v, want release child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want only release gate input", process.Inputs)
	}
	if len(process.Interventions) != 1 || process.Interventions[0].Type != gopact.InterruptApproval {
		t.Fatalf("process interventions = %+v, want only review intervention", process.Interventions)
	}
	process.Interventions[0].Metadata["mutated"] = true
	if _, ok := records.Interventions[0].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowActionProcessRecordsByTaskID returned records sharing intervention metadata")
	}
}

func TestWorkflowActionProcessRecordsFromRunExportReturnsSingleAction(t *testing.T) {
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover run export action process extraction"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	export := gopact.RunExport{
		Version:       gopact.RunExportVersion,
		IDs:           workflow.Task.IDs,
		Tasks:         append([]gopact.TaskRecord{workflow.Task}, workflow.Tasks...),
		Inputs:        workflow.Inputs,
		Interventions: workflow.Interventions,
	}

	process, err := WorkflowActionProcessRecordsFromRunExport(export, "", 3)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExport() error = %v", err)
	}
	if process.Task.ID != workflow.Tasks[2].ID {
		t.Fatalf("process task = %+v, want release child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want only release gate input", process.Inputs)
	}
	if len(process.Interventions) != 1 || process.Interventions[0].Type != gopact.InterruptApproval {
		t.Fatalf("process interventions = %+v, want only review intervention", process.Interventions)
	}
	process.Inputs[0].Metadata["mutated"] = true
	if _, ok := export.Inputs[1].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExport returned records sharing input metadata")
	}
}

func TestWorkflowActionProcessRecordsFromRunExportByActionReturnsSingleAction(t *testing.T) {
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover run export action kind extraction"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	export := gopact.RunExport{
		Version:       gopact.RunExportVersion,
		IDs:           workflow.Task.IDs,
		Tasks:         append([]gopact.TaskRecord{workflow.Task}, workflow.Tasks...),
		Inputs:        workflow.Inputs,
		Interventions: workflow.Interventions,
	}

	process, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionRelease)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction() error = %v", err)
	}
	if process.Task.ID != workflow.Tasks[2].ID {
		t.Fatalf("process task = %+v, want release child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want only release gate input", process.Inputs)
	}
	if len(process.Interventions) != 1 || process.Interventions[0].Type != gopact.InterruptApproval {
		t.Fatalf("process interventions = %+v, want only review intervention", process.Interventions)
	}
	process.Inputs[0].Metadata["mutated"] = true
	if _, ok := export.Inputs[1].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction returned records sharing input metadata")
	}
}

func TestWorkflowActionProcessRecordsFromRunExportByTaskIDReturnsSingleAction(t *testing.T) {
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
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
					Mode:   ModePlan,
					Action: ActionProposePatch,
				},
				Patch: PatchProposal{
					ID:      "patch-1",
					Summary: "prepare release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover run export task id extraction"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	export := gopact.RunExport{
		Version:       gopact.RunExportVersion,
		IDs:           workflow.Task.IDs,
		Tasks:         append([]gopact.TaskRecord{workflow.Task}, workflow.Tasks...),
		Inputs:        workflow.Inputs,
		Interventions: workflow.Interventions,
	}

	process, err := WorkflowActionProcessRecordsFromRunExportByTaskID(export, "", workflow.Tasks[2].ID)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByTaskID() error = %v", err)
	}
	if process.Task.ID != workflow.Tasks[2].ID {
		t.Fatalf("process task = %+v, want release child task", process.Task)
	}
	if len(process.Inputs) != 1 || process.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want only release gate input", process.Inputs)
	}
	if len(process.Interventions) != 1 || process.Interventions[0].Type != gopact.InterruptApproval {
		t.Fatalf("process interventions = %+v, want only review intervention", process.Interventions)
	}
	process.Task.Metadata["mutated"] = true
	if _, ok := export.Tasks[3].Metadata["mutated"]; ok {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByTaskID returned task sharing metadata")
	}
}

func TestImportProcessRecordsRecordsDefensiveCopy(t *testing.T) {
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
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
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	process, err := WorkflowActionProcessRecords(workflow, 2)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}

	recorder := gopact.NewRunRecorder()
	if err := ImportProcessRecords(recorder, process); err != nil {
		t.Fatalf("ImportProcessRecords() error = %v", err)
	}
	if err := recorder.Record(gopact.Event{
		Type: gopact.EventRunCompleted,
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
	}); err != nil {
		t.Fatalf("Record(run completed) error = %v", err)
	}
	process.Task.Metadata["mutated"] = true
	process.Inputs[0].Value.(map[string]any)["status"] = "mutated"
	process.Interventions[0].Metadata["mutated"] = true

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.Tasks) != 1 || export.Tasks[0].ID != process.Task.ID {
		t.Fatalf("export tasks = %+v, want imported task", export.Tasks)
	}
	if _, ok := export.Tasks[0].Metadata["mutated"]; ok {
		t.Fatalf("ImportProcessRecords recorded task sharing metadata")
	}
	if len(export.Inputs) != 1 || export.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("export inputs = %+v, want imported release gate input", export.Inputs)
	}
	value, ok := export.Inputs[0].Value.(map[string]any)
	if !ok || value["status"] != string(GatePassed) {
		t.Fatalf("export input value = %+v, want defensive copy of release gate value", export.Inputs[0].Value)
	}
	if len(export.Interventions) != 1 || export.Interventions[0].Status != gopact.InterventionResolved {
		t.Fatalf("export interventions = %+v, want imported review intervention", export.Interventions)
	}
	if _, ok := export.Interventions[0].Metadata["mutated"]; ok {
		t.Fatalf("ImportProcessRecords recorded intervention sharing metadata")
	}
}

func TestImportWorkflowRecordsCanBeRestoredFromRunExport(t *testing.T) {
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Name: "self-bootstrap apply release workflow",
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
					Summary: "prepare apply release",
					Diff:    "diff --git a/private b/private\n+raw diff must not be copied\n",
					Files: []PatchFile{
						{Path: "templates/devagent/workflow_process_test.go", Intent: "cover workflow records import"},
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
					ReviewStatus: ReviewApproved,
				},
				Review: ReviewDecision{
					Status:   ReviewApproved,
					Reviewer: "human",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}

	recorder := gopact.NewRunRecorder()
	if err := ImportWorkflowRecords(recorder, workflow); err != nil {
		t.Fatalf("ImportWorkflowRecords() error = %v", err)
	}
	if err := recorder.Record(gopact.Event{
		Type: gopact.EventRunCompleted,
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
	}); err != nil {
		t.Fatalf("Record(run completed) error = %v", err)
	}
	workflow.Task.Metadata["mutated"] = true
	workflow.Inputs[0].Value.(map[string]any)["summary"] = "mutated"

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	restored, err := WorkflowRecordsFromRunExport(export, workflow.Task.ID)
	if err != nil {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v", err)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: restored,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionRelease,
		},
		RequiredInputSources: []string{"devagent.patch", "devagent.release_gate"},
	})
	if _, ok := restored.Task.Metadata["mutated"]; ok {
		t.Fatalf("ImportWorkflowRecords recorded workflow task sharing metadata")
	}
	value, ok := restored.Inputs[0].Value.(map[string]any)
	if !ok || value["summary"] != "prepare apply release" {
		t.Fatalf("restored patch input value = %+v, want defensive copy", restored.Inputs[0].Value)
	}
}

func TestBuildWorkflowProcessRecordsRejectsInvalidInput(t *testing.T) {
	_, err := BuildWorkflowProcessRecords(WorkflowInput{})
	if !errors.Is(err, ErrInvalidActionResult) {
		t.Fatalf("BuildWorkflowProcessRecords(empty) error = %v, want ErrInvalidActionResult", err)
	}

	_, err = BuildWorkflowProcessRecords(WorkflowInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
	})
	if !errors.Is(err, ErrInvalidActionResult) {
		t.Fatalf("BuildWorkflowProcessRecords(no actions) error = %v, want ErrInvalidActionResult", err)
	}

	if err := RecordWorkflowProcessRecords(nil, WorkflowInput{IDs: gopact.RuntimeIDs{RunID: "run-1"}}); err == nil {
		t.Fatal("RecordWorkflowProcessRecords(nil) error = nil, want error")
	}
}

func TestBuildWorkflowProcessRecordsRejectsActionRuntimeIdentityMismatch(t *testing.T) {
	workflowIDs := gopact.RuntimeIDs{
		RunID:        "run-1",
		ThreadID:     "thread-1",
		UserID:       "user-1",
		SessionID:    "session-1",
		AgentID:      "agent-1",
		AppID:        "app-1",
		CallID:       "call-1",
		ParentCallID: "parent-call-1",
		TraceID:      "trace-1",
	}
	tests := []struct {
		name        string
		actionIDs   gopact.RuntimeIDs
		wantMessage string
	}{
		{
			name:        "run id",
			actionIDs:   gopact.RuntimeIDs{RunID: "other-run"},
			wantMessage: `run id "other-run" does not match workflow run id "run-1"`,
		},
		{
			name:        "thread id",
			actionIDs:   gopact.RuntimeIDs{ThreadID: "other-thread"},
			wantMessage: `thread id "other-thread" does not match workflow thread id "thread-1"`,
		},
		{
			name:        "user id",
			actionIDs:   gopact.RuntimeIDs{UserID: "other-user"},
			wantMessage: `user id "other-user" does not match workflow user id "user-1"`,
		},
		{
			name:        "session id",
			actionIDs:   gopact.RuntimeIDs{SessionID: "other-session"},
			wantMessage: `session id "other-session" does not match workflow session id "session-1"`,
		},
		{
			name:        "agent id",
			actionIDs:   gopact.RuntimeIDs{AgentID: "other-agent"},
			wantMessage: `agent id "other-agent" does not match workflow agent id "agent-1"`,
		},
		{
			name:        "app id",
			actionIDs:   gopact.RuntimeIDs{AppID: "other-app"},
			wantMessage: `app id "other-app" does not match workflow app id "app-1"`,
		},
		{
			name:        "call id",
			actionIDs:   gopact.RuntimeIDs{CallID: "other-call"},
			wantMessage: `call id "other-call" does not match workflow call id "call-1"`,
		},
		{
			name:        "parent call id",
			actionIDs:   gopact.RuntimeIDs{ParentCallID: "other-parent"},
			wantMessage: `parent call id "other-parent" does not match workflow parent call id "parent-call-1"`,
		},
		{
			name:        "trace id",
			actionIDs:   gopact.RuntimeIDs{TraceID: "other-trace"},
			wantMessage: `trace id "other-trace" does not match workflow trace id "trace-1"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildWorkflowProcessRecords(WorkflowInput{
				IDs: workflowIDs,
				Actions: []ProcessInput{
					{
						IDs: tt.actionIDs,
						Action: ActionResult{
							Status: ActionAllowed,
							Mode:   ModeAnalyze,
							Action: ActionAnalyze,
						},
					},
				},
			})
			if !errors.Is(err, ErrInvalidActionResult) {
				t.Fatalf("BuildWorkflowProcessRecords() error = %v, want ErrInvalidActionResult", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("BuildWorkflowProcessRecords() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}
