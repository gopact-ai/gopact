package devagent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBuildProcessRecordsDescribesObservedWriteActionWithoutRawDiff(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC)
	records, err := BuildProcessRecords(ProcessInput{
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		CreatedAt: createdAt,
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionApplyPatch,
		},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "update scaffold docs",
			Diff:    "diff --git a/README.md b/README.md\n+secret raw diff should stay out of process input\n",
			Files: []PatchFile{
				{Path: "README.md", Intent: "document scaffold helper"},
			},
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe docs/test change",
		},
		Gate: &GateResult{
			Status:             GatePassed,
			Mode:               ModeWrite,
			ReportStatus:       gopact.VerificationStatusPassed,
			ReviewStatus:       ReviewApproved,
			MaxEntropySeverity: gopact.EntropySeverityLow,
		},
		Metadata: map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("BuildProcessRecords() error = %v", err)
	}

	if records.Task.ID != "devagent:run-1:apply_patch" ||
		records.Task.Status != gopact.TaskCompleted ||
		records.Task.Name != "devagent apply_patch" ||
		!records.Task.CreatedAt.Equal(createdAt) ||
		records.Task.IDs.RunID != "run-1" {
		t.Fatalf("task = %+v, want completed apply_patch task with runtime ids", records.Task)
	}
	if records.Task.Metadata["mode"] != string(ModeWrite) ||
		records.Task.Metadata["action"] != string(ActionApplyPatch) ||
		records.Task.Metadata["patch_id"] != "patch-1" ||
		records.Task.Metadata["gate_status"] != string(GatePassed) ||
		records.Task.Metadata["review_status"] != string(ReviewApproved) {
		t.Fatalf("task metadata = %+v, want mode/action/patch/gate/review", records.Task.Metadata)
	}

	if len(records.Inputs) != 2 {
		t.Fatalf("inputs = %+v, want patch and gate inputs", records.Inputs)
	}
	patchInput := records.Inputs[0]
	if patchInput.ID != "devagent:run-1:patch:patch-1" ||
		patchInput.Kind != gopact.InputExternal ||
		patchInput.Source != "devagent.patch" {
		t.Fatalf("patch input = %+v, want external patch input", patchInput)
	}
	patchValue, ok := patchInput.Value.(map[string]any)
	if !ok {
		t.Fatalf("patch input value = %T, want map", patchInput.Value)
	}
	if patchValue["id"] != "patch-1" || patchValue["file_count"] != 1 || patchValue["has_diff"] != true {
		t.Fatalf("patch input value = %+v, want patch summary", patchValue)
	}
	if strings.Contains(toString(patchValue["diff"]), "secret raw diff") {
		t.Fatalf("patch input value leaked raw diff: %+v", patchValue)
	}

	if records.Inputs[1].Source != "devagent.release_gate" {
		t.Fatalf("gate input = %+v, want release gate input", records.Inputs[1])
	}
	gateValue, ok := records.Inputs[1].Value.(map[string]any)
	if !ok {
		t.Fatalf("gate input value = %T, want map", records.Inputs[1].Value)
	}
	if gateValue["review_status"] != string(ReviewApproved) ||
		gateValue["max_entropy_severity"] != string(gopact.EntropySeverityLow) {
		t.Fatalf("gate input value = %+v, want review and entropy summaries", gateValue)
	}
	if len(records.Interventions) != 1 {
		t.Fatalf("interventions = %+v, want one review intervention", records.Interventions)
	}
	intervention := records.Interventions[0]
	if intervention.ID != "devagent:run-1:review:human" ||
		intervention.Type != gopact.InterruptApproval ||
		intervention.Status != gopact.InterventionResolved ||
		intervention.Metadata["reviewer"] != "human" {
		t.Fatalf("intervention = %+v, want resolved review intervention", intervention)
	}
}

func TestRecordProcessRecordsAppendsToRunRecorder(t *testing.T) {
	recorder := gopact.NewRunRecorder()
	if err := RecordProcessRecords(recorder, ProcessInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
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
			Reasons: []string{
				"verification failed",
			},
		},
	}); err != nil {
		t.Fatalf("RecordProcessRecords() error = %v", err)
	}
	if err := recorder.Record(gopact.Event{
		Type: gopact.EventRunFailed,
		IDs:  gopact.RuntimeIDs{RunID: "run-1"},
	}); err != nil {
		t.Fatalf("Record(run failed) error = %v", err)
	}

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.Tasks) != 1 || export.Tasks[0].Status != gopact.TaskFailed {
		t.Fatalf("export tasks = %+v, want failed release task", export.Tasks)
	}
	if len(export.Inputs) != 1 || export.Inputs[0].Source != "devagent.release_gate" {
		t.Fatalf("export inputs = %+v, want release gate input", export.Inputs)
	}
}

func TestBuildProcessRecordsRejectsInvalidActionResult(t *testing.T) {
	_, err := BuildProcessRecords(ProcessInput{
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
		},
	})
	if err == nil || !errors.Is(err, ErrInvalidActionResult) {
		t.Fatalf("BuildProcessRecords() error = %v, want ErrInvalidActionResult", err)
	}
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
