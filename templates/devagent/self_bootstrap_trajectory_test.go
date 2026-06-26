package devagent

import (
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
