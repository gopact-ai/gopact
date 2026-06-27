package devagent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestSelfBootstrapReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_release.golden.json"

	export, bundle := selfBootstrapReleaseTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventRunCompleted,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.release_gate", 3),
		},
	})

	if len(export.Steps) != 3 {
		t.Fatalf("exported steps = %d, want 3", len(export.Steps))
	}
	for i, want := range []struct {
		node string
		step int
	}{
		{node: "devagent.analyze", step: 1},
		{node: "devagent.plan", step: 2},
		{node: "devagent.release_gate", step: 3},
	} {
		got := export.Steps[i]
		if got.Node != want.node || got.Step != want.step || got.Phase != gopact.StepCompleted {
			t.Fatalf("exported step %d = %+v, want completed %s step %d", i, got, want.node, want.step)
		}
	}
	if bundle.Process.Task.ID != "devagent:run-self-bootstrap-1:release" ||
		bundle.Process.Task.ParentID != "devagent:run-self-bootstrap-1:workflow" {
		t.Fatalf("release process task = %+v, want workflow child release task", bundle.Process.Task)
	}
}

func TestSelfBootstrapInterruptedReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_interrupted_release.golden.json"

	export, workflow := selfBootstrapInterruptedReleaseTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-interrupted-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventInterrupted,
			gopact.EventRunInterrupted,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventInterrupted, "devagent.release_gate", 3),
			devAgentFramePattern(gopact.EventRunInterrupted, "devagent.release_gate", 3),
		},
	})

	if export.Outcome != gopact.RunInterrupted {
		t.Fatalf("export outcome = %q, want interrupted", export.Outcome)
	}
	if len(export.Steps) != 3 ||
		export.Steps[2].Phase != gopact.StepInterrupted ||
		export.Steps[2].Pending == nil ||
		export.Steps[2].Pending.ID != "approval-1" {
		t.Fatalf("exported steps = %+v, want interrupted release gate step with approval pending", export.Steps)
	}
	if workflow.Task.Status != gopact.TaskInterrupted ||
		workflow.Task.Metadata["interrupted_action_count"] != 1 {
		t.Fatalf("workflow task = %+v, want interrupted workflow process summary", workflow.Task)
	}
	restored, err := WorkflowRecordsFromRunExport(export, "")
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
	release, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionRelease)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction(release) error = %v", err)
	}
	if release.Task.Status != gopact.TaskInterrupted ||
		len(release.Interventions) != 1 ||
		release.Interventions[0].Status != gopact.InterventionRequested ||
		release.Interventions[0].Request == nil ||
		release.Interventions[0].Request.ID != "approval-1" {
		t.Fatalf("release process = %+v, want interrupted release with requested approval", release)
	}
	if len(export.VerificationReports) != 1 ||
		export.VerificationReports[0].Status != gopact.VerificationStatusPartial ||
		export.VerificationReports[0].SkippedCount != 1 {
		t.Fatalf("verification reports = %+v, want partial report with skipped pending release gate", export.VerificationReports)
	}
}

func TestSelfBootstrapResumedReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_resumed_release.golden.json"

	export, bundle := selfBootstrapResumedReleaseTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-resumed-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventStepImported,
			gopact.EventResumeReceived,
			gopact.EventNodeResumed,
			gopact.EventNodeCompleted,
			gopact.EventRunCompleted,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventStepImported, "devagent.release_gate", 3),
			devAgentFramePattern(gopact.EventResumeReceived, "devagent.release_gate", 3),
			devAgentFramePattern(gopact.EventNodeResumed, "devagent.release_gate", 4),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.release_gate", 4),
		},
	})

	if export.Outcome != gopact.RunCompleted {
		t.Fatalf("export outcome = %q, want completed", export.Outcome)
	}
	if len(export.Steps) != 2 ||
		export.Steps[0].Phase != gopact.StepInterrupted ||
		export.Steps[0].Pending == nil ||
		export.Steps[0].Pending.ID != "approval-1" ||
		export.Steps[1].Phase != gopact.StepCompleted {
		t.Fatalf("exported steps = %+v, want imported interrupted step followed by completed resumed step", export.Steps)
	}
	workflow, err := WorkflowRecordsFromRunExport(export, "")
	if err != nil {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v", err)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: workflow,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionRelease,
		},
		RequiredInputSources: []string{
			"devagent.patch",
			"devagent.release_gate",
			"devagent.review_resume",
		},
	})
	release, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionRelease)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction(release) error = %v", err)
	}
	if release.Task.Status != gopact.TaskCompleted {
		t.Fatalf("release task status = %q, want completed", release.Task.Status)
	}
	if len(release.Inputs) != 2 ||
		release.Inputs[1].Kind != gopact.InputResume ||
		release.Inputs[1].Source != "devagent.review_resume" ||
		release.Inputs[1].Resume == nil ||
		release.Inputs[1].Resume.InterruptID != "approval-1" {
		t.Fatalf("release inputs = %+v, want release gate input and resume input", release.Inputs)
	}
	if len(release.Interventions) != 1 ||
		release.Interventions[0].Status != gopact.InterventionResolved ||
		release.Interventions[0].Resume == nil ||
		release.Interventions[0].Resume.InterruptID != "approval-1" {
		t.Fatalf("release interventions = %+v, want resolved review resume", release.Interventions)
	}
	if len(bundle.Process.Inputs) != 2 ||
		bundle.Process.Inputs[1].Kind != gopact.InputResume ||
		bundle.Process.Inputs[1].Resume == nil ||
		bundle.Process.Inputs[1].Resume.InterruptID != "approval-1" {
		t.Fatalf("bundle process inputs = %+v, want release bundle to preserve resume boundary", bundle.Process.Inputs)
	}
}

func TestSelfBootstrapApplyReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_apply_release.golden.json"

	export, bundle := selfBootstrapApplyReleaseTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-apply-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventPolicyDecided,
			gopact.EventSandboxFileWritten,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventRunCompleted,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.apply_patch", 3),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.release_gate", 4),
		},
	})

	if len(export.Steps) != 4 {
		t.Fatalf("exported steps = %d, want 4", len(export.Steps))
	}
	if bundle.Process.Task.ID != "devagent:run-self-bootstrap-1:release" ||
		bundle.Process.Task.ParentID != "devagent:run-self-bootstrap-1:workflow" {
		t.Fatalf("bundle process task = %+v, want workflow child release task", bundle.Process.Task)
	}
	if len(export.Tasks) < 5 || export.Tasks[3].Metadata["action"] != string(ActionApplyPatch) {
		t.Fatalf("export tasks = %+v, want workflow apply_patch child task", export.Tasks)
	}
}

func TestSelfBootstrapInterruptedApplyMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_interrupted_apply.golden.json"

	export, workflow := selfBootstrapInterruptedApplyTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-interrupted-apply",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventInterrupted,
			gopact.EventRunInterrupted,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventInterrupted, "devagent.apply_patch", 3),
			devAgentFramePattern(gopact.EventRunInterrupted, "devagent.apply_patch", 3),
		},
	})
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: workflow,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionApplyPatch,
		},
		RequiredInputSources: []string{"devagent.patch"},
	})

	if export.Outcome != gopact.RunInterrupted {
		t.Fatalf("export outcome = %q, want interrupted", export.Outcome)
	}
	if len(export.Steps) != 3 ||
		export.Steps[2].Phase != gopact.StepInterrupted ||
		export.Steps[2].Pending == nil ||
		export.Steps[2].Pending.ID != "approval-1" {
		t.Fatalf("exported steps = %+v, want interrupted apply_patch step with approval pending", export.Steps)
	}
	if workflow.Task.Status != gopact.TaskInterrupted ||
		workflow.Task.Metadata["interrupted_action_count"] != 1 {
		t.Fatalf("workflow task = %+v, want interrupted workflow process summary", workflow.Task)
	}
	apply, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionApplyPatch)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction(apply) error = %v", err)
	}
	if apply.Task.Status != gopact.TaskInterrupted ||
		apply.Task.Metadata["action_status"] != string(ActionInterrupted) ||
		len(apply.Inputs) != 1 ||
		apply.Inputs[0].Source != "devagent.patch" ||
		len(apply.Interventions) != 1 ||
		apply.Interventions[0].Status != gopact.InterventionRequested ||
		apply.Interventions[0].Request == nil ||
		apply.Interventions[0].Request.ID != "approval-1" {
		t.Fatalf("apply process = %+v, want interrupted apply process with requested approval", apply)
	}
	if len(export.VerificationReports) != 1 ||
		export.VerificationReports[0].Status != gopact.VerificationStatusPartial ||
		export.VerificationReports[0].SkippedCount != 1 {
		t.Fatalf("verification reports = %+v, want partial report with skipped pending apply approval", export.VerificationReports)
	}
}

func TestSelfBootstrapResumedApplyReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_resumed_apply_release.golden.json"

	export, bundle := selfBootstrapResumedApplyReleaseTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-resumed-apply-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventStepImported,
			gopact.EventResumeReceived,
			gopact.EventNodeResumed,
			gopact.EventPolicyRequested,
			gopact.EventPolicyDecided,
			gopact.EventSandboxFileWritten,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventRunCompleted,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventStepImported, "devagent.apply_patch", 3),
			devAgentFramePattern(gopact.EventResumeReceived, "devagent.apply_patch", 3),
			devAgentFramePattern(gopact.EventNodeResumed, "devagent.apply_patch", 4),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.apply_patch", 4),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.release_gate", 5),
		},
	})

	workflow, err := WorkflowRecordsFromRunExport(export, "")
	if err != nil {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v", err)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: workflow,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionApplyPatch,
			ActionRelease,
		},
		RequiredInputSources: []string{
			"devagent.patch",
			"devagent.review_resume",
			"devagent.release_gate",
		},
	})
	output, ok := workflow.Task.Output.(map[string]any)
	if !ok {
		t.Fatalf("workflow output = %T, want map", workflow.Task.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		t.Fatalf("workflowProcessActionSummaries() error = %v", err)
	}
	if summaries[2]["resume_input_id"] != "devagent:run-self-bootstrap-1:resume:approval-1" ||
		summaries[2]["review_intervention_id"] != "devagent:run-self-bootstrap-1:review:policy-reviewer" {
		t.Fatalf("apply summary = %+v, want resumed apply boundary refs", summaries[2])
	}
	apply, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionApplyPatch)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction(apply) error = %v", err)
	}
	if apply.Task.Status != gopact.TaskCompleted ||
		len(apply.Inputs) != 2 ||
		apply.Inputs[1].Kind != gopact.InputResume ||
		apply.Inputs[1].Resume == nil ||
		apply.Inputs[1].Resume.InterruptID != "approval-1" ||
		len(apply.Interventions) != 1 ||
		apply.Interventions[0].Resume == nil ||
		apply.Interventions[0].Resume.InterruptID != "approval-1" {
		t.Fatalf("apply process = %+v, want resumed apply patch process records", apply)
	}
	if bundle.Process.Task.ID != "devagent:run-self-bootstrap-1:release" ||
		bundle.Process.Task.ParentID != "devagent:run-self-bootstrap-1:workflow" {
		t.Fatalf("bundle process task = %+v, want workflow child release task", bundle.Process.Task)
	}
}

func TestSelfBootstrapRejectedReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_rejected_release.golden.json"

	export, report := selfBootstrapRejectedReleaseTrajectoryFixture(t)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-rejected-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventNodeFailed,
			gopact.EventRunFailed,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventNodeFailed, "devagent.release_gate", 3),
		},
	})

	if export.Outcome != gopact.RunFailed {
		t.Fatalf("export outcome = %q, want failed", export.Outcome)
	}
	if len(export.Steps) != 3 || export.Steps[2].Phase != gopact.StepFailed {
		t.Fatalf("exported steps = %+v, want failed release gate step", export.Steps)
	}
	if len(export.Failures) != 1 || export.Failures[0].Kind != gopact.FailureVerification {
		t.Fatalf("export failures = %+v, want verification failure attribution", export.Failures)
	}
	if len(export.VerificationReports) != 1 || export.VerificationReports[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("export verification reports = %+v, want failed report", export.VerificationReports)
	}
	if report.Status != gopact.VerificationStatusFailed {
		t.Fatalf("report status = %q, want failed", report.Status)
	}
	workflow, err := WorkflowRecordsFromRunExport(export, "")
	if err != nil {
		t.Fatalf("WorkflowRecordsFromRunExport() error = %v", err)
	}
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: workflow,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionRelease,
		},
		RequiredInputSources: []string{"devagent.patch", "devagent.release_gate"},
	})
	if workflow.Task.Status != gopact.TaskFailed {
		t.Fatalf("workflow task status = %q, want failed", workflow.Task.Status)
	}
}

func TestSelfBootstrapCanceledReleaseMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_canceled_release.golden.json"

	export, workflow := selfBootstrapCanceledReleaseTrajectoryFixture(t, goldenPath)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-canceled-release",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventRunCanceled,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventRunCanceled, "devagent.release_gate", 3),
		},
	})

	if export.Outcome != gopact.RunCanceled {
		t.Fatalf("export outcome = %q, want canceled", export.Outcome)
	}
	if len(export.Steps) != 3 ||
		export.Steps[2].Phase != gopact.StepCanceled ||
		export.Steps[2].Error != "context canceled" {
		t.Fatalf("exported steps = %+v, want canceled release_gate step", export.Steps)
	}
	restored, err := WorkflowRecordsFromRunExport(export, "")
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
	if workflow.Task.Status != gopact.TaskCanceled ||
		workflow.Task.Metadata["canceled_action_count"] != 1 {
		t.Fatalf("workflow task = %+v, want canceled workflow process summary", workflow.Task)
	}
	release, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionRelease)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction(release) error = %v", err)
	}
	if release.Task.Status != gopact.TaskCanceled ||
		release.Task.Metadata["action_status"] != string(ActionCanceled) ||
		release.Task.Metadata["gate_status"] != string(GateSkipped) ||
		len(release.Inputs) != 1 ||
		release.Inputs[0].Source != "devagent.release_gate" ||
		len(release.Interventions) != 0 {
		t.Fatalf("release process = %+v, want canceled release gate process without review intervention", release)
	}
	if len(export.VerificationReports) != 1 ||
		export.VerificationReports[0].Status != gopact.VerificationStatusPartial ||
		export.VerificationReports[0].SkippedCount != 1 {
		t.Fatalf("verification reports = %+v, want partial report with skipped canceled release gate", export.VerificationReports)
	}
}

func TestSelfBootstrapRejectedApplyMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_rejected_apply.golden.json"

	export, action, workflow := selfBootstrapRejectedApplyTrajectoryFixture(t)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-rejected-apply",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventNodeFailed,
			gopact.EventRunFailed,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventNodeFailed, "devagent.apply_patch", 3),
		},
	})
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: workflow,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionApplyPatch,
		},
		RequiredInputSources: []string{"devagent.patch"},
	})

	if export.Outcome != gopact.RunFailed {
		t.Fatalf("export outcome = %q, want failed", export.Outcome)
	}
	if len(export.Steps) != 3 || export.Steps[2].Phase != gopact.StepFailed {
		t.Fatalf("exported steps = %+v, want failed apply_patch step", export.Steps)
	}
	if len(export.Failures) != 1 || export.Failures[0].Kind != gopact.FailurePolicy {
		t.Fatalf("export failures = %+v, want policy failure attribution", export.Failures)
	}
	if action.Status != ActionRejected ||
		!containsActionReason(action.Reasons, "policy allow decision is required") ||
		!containsActionReason(action.Reasons, "sandbox event is required") ||
		!containsActionReason(action.Reasons, "observed diff is required") ||
		!containsActionReason(action.Reasons, "observed checkpoint is required") {
		t.Fatalf("apply action = %+v, want rejected missing-evidence reasons", action)
	}
	if workflow.Task.Status != gopact.TaskFailed {
		t.Fatalf("workflow task status = %q, want failed", workflow.Task.Status)
	}
}

func TestSelfBootstrapCanceledApplyMatchesGoldenTrajectory(t *testing.T) {
	const goldenPath = "testdata/self_bootstrap_canceled_apply.golden.json"

	export, workflow := selfBootstrapCanceledApplyTrajectoryFixture(t)

	gopacttest.RequireRunExportGoldenTrajectoryFrames(t, goldenPath, export)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:      "devagent.self-bootstrap-canceled-apply",
		RunExport: &export,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventNodeCompleted,
			gopact.EventRunCanceled,
		},
		RequiredFrames: []gopacttest.TrajectoryFramePattern{
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.analyze", 1),
			devAgentFramePattern(gopact.EventNodeCompleted, "devagent.plan", 2),
			devAgentFramePattern(gopact.EventRunCanceled, "devagent.apply_patch", 3),
		},
	})
	RequireWorkflowProcessConformance(t, WorkflowProcessConformanceHarness{
		Records: workflow,
		RequiredActions: []ActionKind{
			ActionAnalyze,
			ActionProposePatch,
			ActionApplyPatch,
		},
		RequiredInputSources: []string{"devagent.patch"},
	})

	if export.Outcome != gopact.RunCanceled {
		t.Fatalf("export outcome = %q, want canceled", export.Outcome)
	}
	if len(export.Steps) != 3 ||
		export.Steps[2].Phase != gopact.StepCanceled ||
		export.Steps[2].Error != "context canceled" {
		t.Fatalf("exported steps = %+v, want canceled apply_patch step", export.Steps)
	}
	if workflow.Task.Status != gopact.TaskCanceled {
		t.Fatalf("workflow task status = %q, want canceled", workflow.Task.Status)
	}
	apply, err := WorkflowActionProcessRecordsFromRunExportByAction(export, "", ActionApplyPatch)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecordsFromRunExportByAction(apply) error = %v", err)
	}
	if apply.Task.Status != gopact.TaskCanceled ||
		apply.Task.Metadata["action_status"] != string(ActionCanceled) ||
		len(apply.Inputs) != 1 ||
		apply.Inputs[0].Source != "devagent.patch" ||
		len(apply.Interventions) != 0 {
		t.Fatalf("apply process = %+v, want canceled apply process without review intervention", apply)
	}
}

func selfBootstrapReleaseTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, ReleaseBundle) {
	t.Helper()

	createdAt := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapReleaseEvents(ids, createdAt) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	review := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "human",
		Summary:  "self-bootstrap trajectory and release evidence approved",
	}
	entropy := gopact.EntropyAudit{
		ID:        "entropy-1",
		Status:    gopact.VerificationStatusPassed,
		IDs:       ids,
		CreatedAt: createdAt,
		Findings: []gopact.EntropyFinding{
			{
				ID:        "finding-1",
				Category:  gopact.EntropyProcess,
				Severity:  gopact.EntropySeverityLow,
				Summary:   "release trajectory is exportable and replayable",
				CreatedAt: createdAt,
			},
		},
	}
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:          ModeWrite,
		Report:        report,
		Review:        review,
		EntropyAudits: []gopact.EntropyAudit{entropy},
	},
		RequireCheckIDs("unit-tests", "diff-check", gopacttest.VerificationCheckTrajectoryGolden),
		RequireEvidenceTypes("command", "diff", gopacttest.VerificationEvidenceTypeTrajectoryGolden),
	)
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
					Summary: "add self-bootstrap release trajectory evidence",
					Diff:    "diff --git a/private b/private\n+raw diff must not enter release evidence\n",
					Files: []PatchFile{
						{Path: "templates/devagent/self_bootstrap_trajectory_test.go", Intent: "cover workflow release trajectory"},
					},
				},
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Review: review,
				Gate:   &gate,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordEntropyAudit(entropy); err != nil {
		t.Fatalf("RecordEntropyAudit() error = %v", err)
	}
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	process, err := WorkflowActionProcessRecords(workflow, 3)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}

	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		EntropyAudits:         []gopact.EntropyAudit{entropy},
		RequiredCheckIDs:      []string{"unit-tests", "diff-check", gopacttest.VerificationCheckTrajectoryGolden},
		RequiredEvidenceTypes: []string{"command", "diff", gopacttest.VerificationEvidenceTypeTrajectoryGolden},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review:    review,
		Gate:      gate,
		Process:   process,
		CreatedAt: createdAt,
		Metadata:  map[string]any{"release": "self-bootstrap"},
	})
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	return export, bundle
}

func selfBootstrapInterruptedReleaseTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, WorkflowRecords) {
	t.Helper()

	createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	pending := selfBootstrapReleaseApprovalInterrupt(createdAt)
	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapInterruptedReleaseEvents(ids, createdAt, pending) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	gate := GateResult{
		Status:       GatePending,
		Mode:         ModeWrite,
		ReportStatus: gopact.VerificationStatusPassed,
		Reasons: []string{
			"release approval is pending",
		},
	}
	if err := RecordReleaseGateCheck(reportRecorder, gate); err != nil {
		t.Fatalf("RecordReleaseGateCheck(pending) error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report(interrupted export) error = %v", err)
	}

	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap interrupted release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: selfBootstrapPatchProposal("prepare self-bootstrap release while approval is pending"),
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
				Gate:    &gate,
				Pending: &pending,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export(with process records) error = %v", err)
	}
	return export, workflow
}

func selfBootstrapResumedReleaseTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, ReleaseBundle) {
	t.Helper()

	createdAt := time.Date(2026, 6, 26, 13, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	pending := selfBootstrapReleaseApprovalInterrupt(createdAt)
	resume := gopact.ResumeRequest{
		CheckpointID: "checkpoint-release-gate-1",
		StepID:       "devagent:" + ids.RunID + ":step:devagent.release_gate",
		InterruptID:  pending.ID,
		IDs:          ids,
		Payload: map[string]any{
			"decision": "approved",
			"comment":  "release approved",
		},
		PayloadCodec: "application/json",
		CreatedAt:    createdAt.Add(2 * time.Second),
		Metadata: map[string]any{
			"channel": "lark",
		},
	}
	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapResumedReleaseEvents(ids, createdAt, pending, resume) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report(resumed export) error = %v", err)
	}

	review := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "human",
		Summary:  "self-bootstrap release approval resumed",
	}
	entropy := gopact.EntropyAudit{
		ID:        "entropy-1",
		Status:    gopact.VerificationStatusPassed,
		IDs:       ids,
		CreatedAt: createdAt,
		Findings: []gopact.EntropyFinding{
			{
				ID:        "finding-1",
				Category:  gopact.EntropyProcess,
				Severity:  gopact.EntropySeverityLow,
				Summary:   "release trajectory imported the interrupted approval step before resuming",
				CreatedAt: createdAt,
			},
		},
	}
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:          ModeWrite,
		Report:        report,
		Review:        review,
		EntropyAudits: []gopact.EntropyAudit{entropy},
	},
		RequireCheckIDs("unit-tests", "diff-check", gopacttest.VerificationCheckTrajectoryGolden),
		RequireEvidenceTypes("command", "diff", gopacttest.VerificationEvidenceTypeTrajectoryGolden),
	)
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap resumed release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: selfBootstrapPatchProposal("resume self-bootstrap release after approval"),
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Review: review,
				Gate:   &gate,
				Resume: &resume,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordEntropyAudit(entropy); err != nil {
		t.Fatalf("RecordEntropyAudit() error = %v", err)
	}
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export(with process records) error = %v", err)
	}
	process, err := WorkflowActionProcessRecords(workflow, 3)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}

	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		EntropyAudits:         []gopact.EntropyAudit{entropy},
		RequiredCheckIDs:      []string{"unit-tests", "diff-check", gopacttest.VerificationCheckTrajectoryGolden},
		RequiredEvidenceTypes: []string{"command", "diff", gopacttest.VerificationEvidenceTypeTrajectoryGolden},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review:    review,
		Gate:      gate,
		Process:   process,
		CreatedAt: createdAt,
		Metadata:  map[string]any{"release": "self-bootstrap"},
	})
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	return export, bundle
}

func selfBootstrapApplyReleaseTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, ReleaseBundle) {
	t.Helper()

	createdAt := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	patch := selfBootstrapPatchProposal("apply self-bootstrap release trajectory evidence")
	applyEvents := selfBootstrapApplyPatchEvents(ids, createdAt)
	applyAction, err := EvaluateAction(ActionInput{
		Mode:                  ModeWrite,
		Action:                ActionApplyPatch,
		Patch:                 patch,
		ObservedDiff:          "diff --git a/templates/devagent/self_bootstrap_trajectory_test.go b/templates/devagent/self_bootstrap_trajectory_test.go\n",
		ObservedCheckpointRef: "checkpoint:thread-1:apply-patch",
		PolicyDecision: &gopact.PolicyDecision{
			Action: gopact.PolicyAllow,
		},
		Events: applyEvents,
		Metadata: map[string]any{
			"policy_ref": "policy:self-bootstrap-apply",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateAction(apply) error = %v", err)
	}

	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapApplyReleaseEvents(ids, createdAt, applyEvents) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		verificationCheck("checkpoint-check", gopact.VerificationStatusPassed, "checkpoint"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	review := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "human",
		Summary:  "self-bootstrap apply and release evidence approved",
	}
	entropy := gopact.EntropyAudit{
		ID:        "entropy-1",
		Status:    gopact.VerificationStatusPassed,
		IDs:       ids,
		CreatedAt: createdAt,
		Findings: []gopact.EntropyFinding{
			{
				ID:        "finding-1",
				Category:  gopact.EntropyProcess,
				Severity:  gopact.EntropySeverityLow,
				Summary:   "apply patch trajectory has policy, sandbox, diff, checkpoint, and release gate evidence",
				CreatedAt: createdAt,
			},
		},
	}
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:          ModeWrite,
		Report:        report,
		Review:        review,
		EntropyAudits: []gopact.EntropyAudit{entropy},
	},
		RequireCheckIDs("unit-tests", "diff-check", "checkpoint-check", gopacttest.VerificationCheckTrajectoryGolden),
		RequireEvidenceTypes("command", "diff", "checkpoint", gopacttest.VerificationEvidenceTypeTrajectoryGolden),
	)
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap apply release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: patch,
			},
			{
				Action: applyAction,
				Patch:  patch,
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Review: review,
				Gate:   &gate,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordEntropyAudit(entropy); err != nil {
		t.Fatalf("RecordEntropyAudit() error = %v", err)
	}
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	process, err := WorkflowActionProcessRecords(workflow, 4)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}

	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		EntropyAudits:         []gopact.EntropyAudit{entropy},
		RequiredCheckIDs:      []string{"unit-tests", "diff-check", "checkpoint-check", gopacttest.VerificationCheckTrajectoryGolden},
		RequiredEvidenceTypes: []string{"command", "diff", "checkpoint", gopacttest.VerificationEvidenceTypeTrajectoryGolden},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review:    review,
		Gate:      gate,
		Process:   process,
		CreatedAt: createdAt,
		Metadata:  map[string]any{"release": "self-bootstrap"},
	})
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	return export, bundle
}

func selfBootstrapInterruptedApplyTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, WorkflowRecords) {
	t.Helper()

	createdAt := time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	patch := selfBootstrapPatchProposal("pause self-bootstrap apply while approval is pending")
	pending := selfBootstrapApplyApprovalInterrupt(createdAt)
	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapInterruptedApplyEvents(ids, createdAt, pending) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		{
			ID:      "apply-approval",
			Name:    "apply approval",
			Status:  gopact.VerificationStatusSkipped,
			Summary: "apply approval is pending",
		},
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report(interrupted apply export) error = %v", err)
	}

	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap interrupted apply workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: patch,
			},
			{
				Action: ActionResult{
					Status: ActionInterrupted,
					Mode:   ModeWrite,
					Action: ActionApplyPatch,
					Reasons: []string{
						"apply approval is pending",
					},
				},
				Patch:   patch,
				Pending: &pending,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export(with process records) error = %v", err)
	}
	return export, workflow
}

func selfBootstrapResumedApplyReleaseTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, ReleaseBundle) {
	t.Helper()

	createdAt := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	patch := selfBootstrapPatchProposal("resume apply before self-bootstrap release")
	pending := selfBootstrapApplyApprovalInterrupt(createdAt)
	resume := gopact.ResumeRequest{
		CheckpointID: "checkpoint-apply-patch-1",
		StepID:       "devagent:" + ids.RunID + ":step:devagent.apply_patch",
		InterruptID:  pending.ID,
		IDs:          ids,
		Payload: map[string]any{
			"decision": "approved",
			"comment":  "apply approved",
		},
		PayloadCodec: "application/json",
		CreatedAt:    createdAt.Add(2 * time.Second),
		Metadata: map[string]any{
			"channel": "lark",
		},
	}
	applyEvents := selfBootstrapResumedApplyPatchEvidenceEvents(ids, createdAt)
	applyAction, err := EvaluateAction(ActionInput{
		Mode:                  ModeWrite,
		Action:                ActionApplyPatch,
		Patch:                 patch,
		ObservedDiff:          "diff --git a/templates/devagent/self_bootstrap_trajectory_test.go b/templates/devagent/self_bootstrap_trajectory_test.go\n",
		ObservedCheckpointRef: "checkpoint:thread-1:apply-patch-resumed",
		PolicyDecision: &gopact.PolicyDecision{
			Action: gopact.PolicyAllow,
		},
		Events: applyEvents,
		Metadata: map[string]any{
			"policy_ref": "policy:self-bootstrap-apply",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateAction(apply) error = %v", err)
	}

	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapResumedApplyReleaseEvents(ids, createdAt, pending, resume, applyEvents) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		verificationCheck("checkpoint-check", gopact.VerificationStatusPassed, "checkpoint"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	applyReview := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "policy-reviewer",
		Summary:  "self-bootstrap apply approval resumed",
	}
	releaseReview := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "human",
		Summary:  "self-bootstrap resumed apply and release evidence approved",
	}
	entropy := gopact.EntropyAudit{
		ID:        "entropy-1",
		Status:    gopact.VerificationStatusPassed,
		IDs:       ids,
		CreatedAt: createdAt,
		Findings: []gopact.EntropyFinding{
			{
				ID:        "finding-1",
				Category:  gopact.EntropyProcess,
				Severity:  gopact.EntropySeverityLow,
				Summary:   "apply patch trajectory imported the interrupted approval step before resuming",
				CreatedAt: createdAt,
			},
		},
	}
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:          ModeWrite,
		Report:        report,
		Review:        releaseReview,
		EntropyAudits: []gopact.EntropyAudit{entropy},
	},
		RequireCheckIDs("unit-tests", "diff-check", "checkpoint-check", gopacttest.VerificationCheckTrajectoryGolden),
		RequireEvidenceTypes("command", "diff", "checkpoint", gopacttest.VerificationEvidenceTypeTrajectoryGolden),
	)
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap resumed apply release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: patch,
			},
			{
				Action: applyAction,
				Patch:  patch,
				Review: applyReview,
				Resume: &resume,
			},
			{
				Action: ActionResult{
					Status: ActionAllowed,
					Mode:   ModeWrite,
					Action: ActionRelease,
				},
				Review: releaseReview,
				Gate:   &gate,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordEntropyAudit(entropy); err != nil {
		t.Fatalf("RecordEntropyAudit() error = %v", err)
	}
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export(with process records) error = %v", err)
	}
	process, err := WorkflowActionProcessRecords(workflow, 4)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}

	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		EntropyAudits:         []gopact.EntropyAudit{entropy},
		RequiredCheckIDs:      []string{"unit-tests", "diff-check", "checkpoint-check", gopacttest.VerificationCheckTrajectoryGolden},
		RequiredEvidenceTypes: []string{"command", "diff", "checkpoint", gopacttest.VerificationEvidenceTypeTrajectoryGolden},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review:    releaseReview,
		Gate:      gate,
		Process:   process,
		CreatedAt: createdAt,
		Metadata:  map[string]any{"release": "self-bootstrap"},
	})
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	return export, bundle
}

func selfBootstrapRejectedReleaseTrajectoryFixture(t *testing.T) (gopact.RunExport, gopact.VerificationReport) {
	t.Helper()

	createdAt := time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	recorder := gopact.NewRunRecorder()
	events := selfBootstrapReleaseEvents(ids, createdAt)
	for _, event := range events[:len(events)-2] {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	candidate := gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       ids,
		Outcome:   gopact.RunCompleted,
		CreatedAt: createdAt,
	}
	passedReport, err := reportRecorder.Report(candidate)
	if err != nil {
		t.Fatalf("Report(candidate) error = %v", err)
	}
	review := ReviewDecision{
		Status:   ReviewRejected,
		Reviewer: "human",
		Summary:  "release needs another plan pass",
	}
	entropy := gopact.EntropyAudit{
		ID:        "entropy-1",
		Status:    gopact.VerificationStatusPassed,
		IDs:       ids,
		CreatedAt: createdAt,
	}
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:          ModeWrite,
		Report:        passedReport,
		Review:        review,
		EntropyAudits: []gopact.EntropyAudit{entropy},
	}, RequireCheckIDs("unit-tests", "diff-check"), RequireEvidenceTypes("command", "diff"))
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if err := RecordReleaseGateCheck(reportRecorder, gate); !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("RecordReleaseGateCheck() error = %v, want ErrReleaseGateRejected", err)
	}
	report, err := reportRecorder.Report(gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       ids,
		Outcome:   gopact.RunFailed,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("Report(failed export) error = %v", err)
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap rejected release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: selfBootstrapPatchProposal("reject self-bootstrap release until the plan is updated"),
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
				Review: review,
				Gate:   &gate,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordEntropyAudit(entropy); err != nil {
		t.Fatalf("RecordEntropyAudit() error = %v", err)
	}
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	if err := recorder.Record(devAgentFailedStepEvent(
		ids,
		createdAt.Add(6*time.Second),
		3,
		"devagent.release_gate",
		"release gate rejected: review status rejected",
	)); err != nil {
		t.Fatalf("Record(failed step) error = %v", err)
	}
	if err := recorder.Record(gopact.Event{
		Type:      gopact.EventRunFailed,
		IDs:       ids,
		Node:      "devagent.release_gate",
		Step:      3,
		CreatedAt: createdAt.Add(7 * time.Second),
		Err:       errors.New("release gate rejected: review status rejected"),
		Metadata: devAgentStepMetadataWith("devagent.release_gate", map[string]any{
			gopact.EventMetadataFailureKind: string(gopact.FailureVerification),
		}),
	}); err != nil {
		t.Fatalf("Record(run failed) error = %v", err)
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	return export, report
}

func selfBootstrapRejectedApplyTrajectoryFixture(t *testing.T) (gopact.RunExport, ActionResult, WorkflowRecords) {
	t.Helper()

	createdAt := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	patch := selfBootstrapPatchProposal("attempt self-bootstrap apply without required write evidence")
	applyAction, err := EvaluateAction(ActionInput{
		Mode:   ModeWrite,
		Action: ActionApplyPatch,
		Patch:  patch,
		Metadata: map[string]any{
			"policy_ref": "policy:self-bootstrap-apply",
		},
	})
	if !errors.Is(err, ErrActionRejected) {
		t.Fatalf("EvaluateAction(apply) error = %v, want ErrActionRejected", err)
	}

	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapRejectedApplyEvents(ids, createdAt, applyAction) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap rejected apply workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: patch,
			},
			{
				Action: applyAction,
				Patch:  patch,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	return export, applyAction, workflow
}

func selfBootstrapCanceledReleaseTrajectoryFixture(t *testing.T, goldenPath string) (gopact.RunExport, WorkflowRecords) {
	t.Helper()

	createdAt := time.Date(2026, 6, 27, 13, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapCanceledReleaseEvents(ids, createdAt) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	if err := gopacttest.RecordRunExportGoldenTrajectoryCheck(reportRecorder, goldenPath, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	gate := GateResult{
		Status:       GateSkipped,
		Mode:         ModeWrite,
		ReportStatus: gopact.VerificationStatusPartial,
		Reasons: []string{
			"release gate canceled before final decision",
		},
	}
	if err := RecordReleaseGateCheck(reportRecorder, gate); err != nil {
		t.Fatalf("RecordReleaseGateCheck(skipped) error = %v", err)
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report(canceled release export) error = %v", err)
	}

	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap canceled release workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: selfBootstrapPatchProposal("cancel self-bootstrap release after planning"),
			},
			{
				Action: ActionResult{
					Status: ActionCanceled,
					Mode:   ModeWrite,
					Action: ActionRelease,
					Reasons: []string{
						"context canceled",
					},
				},
				Gate: &gate,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	export, err = recorder.Export()
	if err != nil {
		t.Fatalf("Export(with process records) error = %v", err)
	}
	return export, workflow
}

func selfBootstrapCanceledApplyTrajectoryFixture(t *testing.T) (gopact.RunExport, WorkflowRecords) {
	t.Helper()

	createdAt := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	ids := selfBootstrapRuntimeIDs()
	patch := selfBootstrapPatchProposal("cancel self-bootstrap apply while preserving process evidence")
	recorder := gopact.NewRunRecorder()
	for _, event := range selfBootstrapCanceledApplyEvents(ids, createdAt) {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(event) error = %v", err)
		}
	}
	workflow, err := BuildWorkflowProcessRecords(WorkflowInput{
		IDs:       ids,
		Name:      "self-bootstrap canceled apply workflow",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-workflow"},
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
				Patch: patch,
			},
			{
				Action: ActionResult{
					Status: ActionCanceled,
					Mode:   ModeWrite,
					Action: ActionApplyPatch,
					Reasons: []string{
						"context canceled",
					},
				},
				Patch: patch,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowProcessRecords() error = %v", err)
	}
	recordWorkflowRecords(t, recorder, workflow)
	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	return export, workflow
}

func selfBootstrapRuntimeIDs() gopact.RuntimeIDs {
	return gopact.RuntimeIDs{
		RunID:     "run-self-bootstrap-1",
		ThreadID:  "thread-1",
		UserID:    "user-1",
		SessionID: "session-1",
		AgentID:   "dev-agent-1",
		AppID:     "gopact",
		CallID:    "call-1",
		TraceID:   "trace-1",
	}
}

func selfBootstrapReleaseEvents(ids gopact.RuntimeIDs, createdAt time.Time) []gopact.Event {
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.release_gate", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(6*time.Second), gopact.EventNodeCompleted, 3, "devagent.release_gate", gopact.StepCompleted),
		{Type: gopact.EventRunCompleted, IDs: ids, CreatedAt: createdAt.Add(7 * time.Second)},
	}
}

func selfBootstrapInterruptedReleaseEvents(
	ids gopact.RuntimeIDs,
	createdAt time.Time,
	pending gopact.InterruptRecord,
) []gopact.Event {
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.release_gate", gopact.StepRunning),
		devAgentInterruptedStepEvent(ids, createdAt.Add(6*time.Second), 3, "devagent.release_gate", pending),
		{
			Type:      gopact.EventRunInterrupted,
			IDs:       ids,
			Node:      "devagent.release_gate",
			Step:      3,
			CreatedAt: createdAt.Add(7 * time.Second),
			Err:       gopact.Interrupt(pending),
			Metadata:  devAgentStepMetadata("devagent.release_gate"),
		},
	}
}

func selfBootstrapResumedReleaseEvents(
	ids gopact.RuntimeIDs,
	createdAt time.Time,
	pending gopact.InterruptRecord,
	resume gopact.ResumeRequest,
) []gopact.Event {
	imported := gopact.StepSnapshot{
		ID:          "devagent:" + ids.RunID + ":step:devagent.release_gate",
		Step:        3,
		Node:        "devagent.release_gate",
		Phase:       gopact.StepInterrupted,
		IDs:         ids,
		Metadata:    devAgentStepMetadata("devagent.release_gate"),
		Pending:     &pending,
		StartedAt:   createdAt.Add(time.Second),
		CompletedAt: createdAt.Add(time.Second),
	}
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		{
			Type:         gopact.EventStepImported,
			IDs:          ids,
			Node:         "devagent.release_gate",
			Step:         3,
			StepSnapshot: &imported,
			CreatedAt:    createdAt.Add(time.Second),
			Metadata:     devAgentStepMetadata("devagent.release_gate"),
		},
		{
			Type:      gopact.EventResumeReceived,
			IDs:       ids,
			Node:      "devagent.release_gate",
			Step:      3,
			CreatedAt: createdAt.Add(2 * time.Second),
			Metadata: devAgentStepMetadataWith("devagent.release_gate", map[string]any{
				"checkpoint_id": resume.CheckpointID,
				"interrupt_id":  resume.InterruptID,
			}),
		},
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeResumed, 4, "devagent.release_gate", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 4, "devagent.release_gate", gopact.StepCompleted),
		{Type: gopact.EventRunCompleted, IDs: ids, CreatedAt: createdAt.Add(5 * time.Second)},
	}
}

func selfBootstrapApplyReleaseEvents(ids gopact.RuntimeIDs, createdAt time.Time, applyEvents []gopact.Event) []gopact.Event {
	events := []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.apply_patch", gopact.StepRunning),
	}
	events = append(events, applyEvents...)
	events = append(events,
		devAgentStepEvent(ids, createdAt.Add(9*time.Second), gopact.EventNodeCompleted, 3, "devagent.apply_patch", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(10*time.Second), gopact.EventNodeStarted, 4, "devagent.release_gate", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(11*time.Second), gopact.EventNodeCompleted, 4, "devagent.release_gate", gopact.StepCompleted),
		gopact.Event{Type: gopact.EventRunCompleted, IDs: ids, CreatedAt: createdAt.Add(12 * time.Second)},
	)
	return events
}

func selfBootstrapInterruptedApplyEvents(
	ids gopact.RuntimeIDs,
	createdAt time.Time,
	pending gopact.InterruptRecord,
) []gopact.Event {
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.apply_patch", gopact.StepRunning),
		devAgentInterruptedStepEvent(ids, createdAt.Add(6*time.Second), 3, "devagent.apply_patch", pending),
		{
			Type:      gopact.EventRunInterrupted,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      3,
			CreatedAt: createdAt.Add(7 * time.Second),
			Err:       gopact.Interrupt(pending),
			Metadata:  devAgentStepMetadata("devagent.apply_patch"),
		},
	}
}

func selfBootstrapResumedApplyReleaseEvents(
	ids gopact.RuntimeIDs,
	createdAt time.Time,
	pending gopact.InterruptRecord,
	resume gopact.ResumeRequest,
	applyEvents []gopact.Event,
) []gopact.Event {
	imported := gopact.StepSnapshot{
		ID:          "devagent:" + ids.RunID + ":step:devagent.apply_patch",
		Step:        3,
		Node:        "devagent.apply_patch",
		Phase:       gopact.StepInterrupted,
		IDs:         ids,
		Metadata:    devAgentStepMetadata("devagent.apply_patch"),
		Pending:     &pending,
		StartedAt:   createdAt.Add(time.Second),
		CompletedAt: createdAt.Add(time.Second),
	}
	events := []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		{
			Type:         gopact.EventStepImported,
			IDs:          ids,
			Node:         "devagent.apply_patch",
			Step:         3,
			StepSnapshot: &imported,
			CreatedAt:    createdAt.Add(time.Second),
			Metadata:     devAgentStepMetadata("devagent.apply_patch"),
		},
		{
			Type:      gopact.EventResumeReceived,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      3,
			CreatedAt: createdAt.Add(2 * time.Second),
			Metadata: devAgentStepMetadataWith("devagent.apply_patch", map[string]any{
				"checkpoint_id": resume.CheckpointID,
				"interrupt_id":  resume.InterruptID,
			}),
		},
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeResumed, 4, "devagent.apply_patch", gopact.StepRunning),
	}
	events = append(events, applyEvents...)
	events = append(events,
		devAgentStepEvent(ids, createdAt.Add(7*time.Second), gopact.EventNodeCompleted, 4, "devagent.apply_patch", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(8*time.Second), gopact.EventNodeStarted, 5, "devagent.release_gate", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(9*time.Second), gopact.EventNodeCompleted, 5, "devagent.release_gate", gopact.StepCompleted),
		gopact.Event{Type: gopact.EventRunCompleted, IDs: ids, CreatedAt: createdAt.Add(10 * time.Second)},
	)
	return events
}

func selfBootstrapRejectedApplyEvents(ids gopact.RuntimeIDs, createdAt time.Time, action ActionResult) []gopact.Event {
	reason := "apply action rejected: " + strings.Join(action.Reasons, "; ")
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.apply_patch", gopact.StepRunning),
		devAgentFailedStepEvent(ids, createdAt.Add(6*time.Second), 3, "devagent.apply_patch", reason),
		{
			Type:      gopact.EventRunFailed,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      3,
			CreatedAt: createdAt.Add(7 * time.Second),
			Err:       errors.New(reason),
			Metadata: devAgentStepMetadataWith("devagent.apply_patch", map[string]any{
				gopact.EventMetadataFailureKind: string(gopact.FailurePolicy),
			}),
		},
	}
}

func selfBootstrapCanceledApplyEvents(ids gopact.RuntimeIDs, createdAt time.Time) []gopact.Event {
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.apply_patch", gopact.StepRunning),
		devAgentCanceledStepEvent(ids, createdAt.Add(6*time.Second), 3, "devagent.apply_patch"),
	}
}

func selfBootstrapCanceledReleaseEvents(ids gopact.RuntimeIDs, createdAt time.Time) []gopact.Event {
	return []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: ids, CreatedAt: createdAt},
		devAgentStepEvent(ids, createdAt.Add(time.Second), gopact.EventNodeStarted, 1, "devagent.analyze", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(2*time.Second), gopact.EventNodeCompleted, 1, "devagent.analyze", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(3*time.Second), gopact.EventNodeStarted, 2, "devagent.plan", gopact.StepRunning),
		devAgentStepEvent(ids, createdAt.Add(4*time.Second), gopact.EventNodeCompleted, 2, "devagent.plan", gopact.StepCompleted),
		devAgentStepEvent(ids, createdAt.Add(5*time.Second), gopact.EventNodeStarted, 3, "devagent.release_gate", gopact.StepRunning),
		devAgentCanceledStepEvent(ids, createdAt.Add(6*time.Second), 3, "devagent.release_gate"),
	}
}

func selfBootstrapApplyPatchEvents(ids gopact.RuntimeIDs, createdAt time.Time) []gopact.Event {
	return []gopact.Event{
		{
			Type:      gopact.EventPolicyRequested,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      3,
			CreatedAt: createdAt.Add(6 * time.Second),
		},
		{
			Type: gopact.EventPolicyDecided,
			IDs:  ids,
			Node: "devagent.apply_patch",
			Step: 3,
			PolicyDecision: &gopact.PolicyDecision{
				Action: gopact.PolicyAllow,
			},
			CreatedAt: createdAt.Add(7 * time.Second),
		},
		{
			Type:      gopact.EventSandboxFileWritten,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      3,
			CreatedAt: createdAt.Add(8 * time.Second),
		},
	}
}

func selfBootstrapResumedApplyPatchEvidenceEvents(ids gopact.RuntimeIDs, createdAt time.Time) []gopact.Event {
	return []gopact.Event{
		{
			Type:      gopact.EventPolicyRequested,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      4,
			CreatedAt: createdAt.Add(4 * time.Second),
		},
		{
			Type: gopact.EventPolicyDecided,
			IDs:  ids,
			Node: "devagent.apply_patch",
			Step: 4,
			PolicyDecision: &gopact.PolicyDecision{
				Action: gopact.PolicyAllow,
			},
			CreatedAt: createdAt.Add(5 * time.Second),
		},
		{
			Type:      gopact.EventSandboxFileWritten,
			IDs:       ids,
			Node:      "devagent.apply_patch",
			Step:      4,
			CreatedAt: createdAt.Add(6 * time.Second),
		},
	}
}

func selfBootstrapPatchProposal(summary string) PatchProposal {
	return PatchProposal{
		ID:      "patch-1",
		Summary: summary,
		Diff:    "diff --git a/private b/private\n+raw diff must not enter release evidence\n",
		Files: []PatchFile{
			{Path: "templates/devagent/self_bootstrap_trajectory_test.go", Intent: "cover workflow trajectory evidence"},
		},
	}
}

func selfBootstrapReleaseApprovalInterrupt(createdAt time.Time) gopact.InterruptRecord {
	return gopact.InterruptRecord{
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
}

func selfBootstrapApplyApprovalInterrupt(createdAt time.Time) gopact.InterruptRecord {
	return gopact.InterruptRecord{
		ID:         "approval-1",
		Type:       gopact.InterruptApproval,
		Reason:     "apply approval is pending",
		RequiredBy: "devagent.apply_patch",
		Prompt: gopact.Message{
			Role:    gopact.RoleAssistant,
			Content: "Review the proposed self-bootstrap patch before applying it.",
		},
		CreatedAt: createdAt,
	}
}

func devAgentFailedStepEvent(ids gopact.RuntimeIDs, at time.Time, step int, node string, errText string) gopact.Event {
	metadata := devAgentStepMetadata(node)
	return gopact.Event{
		Type:      gopact.EventNodeFailed,
		IDs:       ids,
		Node:      node,
		Step:      step,
		CreatedAt: at,
		Err:       errors.New(errText),
		Metadata:  copyDevAgentMetadata(metadata),
		StepSnapshot: &gopact.StepSnapshot{
			ID:          "devagent:" + ids.RunID + ":step:" + node,
			Step:        step,
			Node:        node,
			Phase:       gopact.StepFailed,
			IDs:         ids,
			Error:       errText,
			Metadata:    copyDevAgentMetadata(metadata),
			StartedAt:   at,
			CompletedAt: at,
		},
	}
}

func devAgentInterruptedStepEvent(
	ids gopact.RuntimeIDs,
	at time.Time,
	step int,
	node string,
	pending gopact.InterruptRecord,
) gopact.Event {
	metadata := devAgentStepMetadata(node)
	return gopact.Event{
		Type:      gopact.EventInterrupted,
		IDs:       ids,
		Node:      node,
		Step:      step,
		CreatedAt: at,
		Metadata:  copyDevAgentMetadata(metadata),
		StepSnapshot: &gopact.StepSnapshot{
			ID:          "devagent:" + ids.RunID + ":step:" + node,
			Step:        step,
			Node:        node,
			Phase:       gopact.StepInterrupted,
			IDs:         ids,
			Pending:     &pending,
			Metadata:    copyDevAgentMetadata(metadata),
			StartedAt:   at,
			CompletedAt: at,
		},
	}
}

func devAgentCanceledStepEvent(ids gopact.RuntimeIDs, at time.Time, step int, node string) gopact.Event {
	metadata := devAgentStepMetadata(node)
	return gopact.Event{
		Type:      gopact.EventRunCanceled,
		IDs:       ids,
		Node:      node,
		Step:      step,
		CreatedAt: at,
		Err:       errors.New("context canceled"),
		Metadata:  copyDevAgentMetadata(metadata),
		StepSnapshot: &gopact.StepSnapshot{
			ID:          "devagent:" + ids.RunID + ":step:" + node,
			Step:        step,
			Node:        node,
			Phase:       gopact.StepCanceled,
			IDs:         ids,
			Error:       "context canceled",
			Metadata:    copyDevAgentMetadata(metadata),
			StartedAt:   at,
			CompletedAt: at,
		},
	}
}

func devAgentStepEvent(
	ids gopact.RuntimeIDs,
	at time.Time,
	eventType gopact.EventType,
	step int,
	node string,
	phase gopact.StepPhase,
) gopact.Event {
	metadata := devAgentStepMetadata(node)
	snapshot := &gopact.StepSnapshot{
		ID:        "devagent:" + ids.RunID + ":step:" + node,
		Step:      step,
		Node:      node,
		Phase:     phase,
		IDs:       ids,
		Metadata:  copyDevAgentMetadata(metadata),
		StartedAt: at,
	}
	if phase != gopact.StepRunning {
		snapshot.CompletedAt = at
	}
	return gopact.Event{
		Type:         eventType,
		IDs:          ids,
		Node:         node,
		Step:         step,
		CreatedAt:    at,
		Metadata:     copyDevAgentMetadata(metadata),
		StepSnapshot: snapshot,
	}
}

func devAgentStepMetadata(node string) map[string]any {
	switch node {
	case "devagent.analyze":
		return map[string]any{
			"action": string(ActionAnalyze),
			"mode":   string(ModeAnalyze),
		}
	case "devagent.plan":
		return map[string]any{
			"action": string(ActionProposePatch),
			"mode":   string(ModePlan),
		}
	case "devagent.apply_patch":
		return map[string]any{
			"action": string(ActionApplyPatch),
			"mode":   string(ModeWrite),
		}
	case "devagent.release_gate":
		return map[string]any{
			"action": string(ActionRelease),
			"mode":   string(ModeWrite),
		}
	default:
		return nil
	}
}

func devAgentStepMetadataWith(node string, extra map[string]any) map[string]any {
	metadata := copyDevAgentMetadata(devAgentStepMetadata(node))
	for key, value := range extra {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata[key] = value
	}
	return metadata
}

func devAgentFramePattern(eventType gopact.EventType, node string, step int) gopacttest.TrajectoryFramePattern {
	return gopacttest.TrajectoryFramePattern{
		Type:     eventType,
		Node:     node,
		Step:     intPtr(step),
		Metadata: devAgentStepMetadata(node),
	}
}

func recordWorkflowRecords(t *testing.T, recorder *gopact.RunRecorder, records WorkflowRecords) {
	t.Helper()

	if err := recorder.RecordTask(records.Task); err != nil {
		t.Fatalf("RecordTask(workflow) error = %v", err)
	}
	for _, record := range records.Tasks {
		if err := recorder.RecordTask(record); err != nil {
			t.Fatalf("RecordTask(child) error = %v", err)
		}
	}
	for _, record := range records.Inputs {
		if err := recorder.RecordInput(record); err != nil {
			t.Fatalf("RecordInput() error = %v", err)
		}
	}
	for _, record := range records.Interventions {
		if err := recorder.RecordIntervention(record); err != nil {
			t.Fatalf("RecordIntervention() error = %v", err)
		}
	}
}

func intPtr(v int) *int {
	return &v
}
