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
			{Type: gopact.EventNodeCompleted, Node: "devagent.analyze", Step: intPtr(1)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.plan", Step: intPtr(2)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.release_gate", Step: intPtr(3)},
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
			{Type: gopact.EventNodeCompleted, Node: "devagent.analyze", Step: intPtr(1)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.plan", Step: intPtr(2)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.apply_patch", Step: intPtr(3)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.release_gate", Step: intPtr(4)},
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
			{Type: gopact.EventNodeCompleted, Node: "devagent.analyze", Step: intPtr(1)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.plan", Step: intPtr(2)},
			{Type: gopact.EventNodeFailed, Node: "devagent.release_gate", Step: intPtr(3)},
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
			{Type: gopact.EventNodeCompleted, Node: "devagent.analyze", Step: intPtr(1)},
			{Type: gopact.EventNodeCompleted, Node: "devagent.plan", Step: intPtr(2)},
			{Type: gopact.EventNodeFailed, Node: "devagent.apply_patch", Step: intPtr(3)},
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
		Review: review,
		Gate:   gate,
		Process: ProcessRecords{
			Task:          workflow.Tasks[2],
			Inputs:        workflow.Inputs,
			Interventions: workflow.Interventions,
		},
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
		Review: review,
		Gate:   gate,
		Process: ProcessRecords{
			Task:          workflow.Tasks[3],
			Inputs:        workflow.Inputs,
			Interventions: workflow.Interventions,
		},
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
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: passedReport,
		Review: review,
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:        "entropy-1",
				Status:    gopact.VerificationStatusPassed,
				IDs:       ids,
				CreatedAt: createdAt,
			},
		},
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
		Metadata: map[string]any{
			gopact.EventMetadataFailureKind: string(gopact.FailureVerification),
		},
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
			Metadata: map[string]any{
				gopact.EventMetadataFailureKind: string(gopact.FailurePolicy),
			},
		},
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

func devAgentFailedStepEvent(ids gopact.RuntimeIDs, at time.Time, step int, node string, errText string) gopact.Event {
	return gopact.Event{
		Type:      gopact.EventNodeFailed,
		IDs:       ids,
		Node:      node,
		Step:      step,
		CreatedAt: at,
		Err:       errors.New(errText),
		StepSnapshot: &gopact.StepSnapshot{
			ID:          "devagent:" + ids.RunID + ":step:" + node,
			Step:        step,
			Node:        node,
			Phase:       gopact.StepFailed,
			IDs:         ids,
			Error:       errText,
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
	snapshot := &gopact.StepSnapshot{
		ID:        "devagent:" + ids.RunID + ":step:" + node,
		Step:      step,
		Node:      node,
		Phase:     phase,
		IDs:       ids,
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
		StepSnapshot: snapshot,
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
