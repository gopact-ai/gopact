package devagent

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordReleaseBundleCheckRecordsPassedBundleEvidence(t *testing.T) {
	bundle := releaseBundleFixture(t)
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseBundleCheck(recorder, bundle); err != nil {
		t.Fatalf("RecordReleaseBundleCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "release-bundle:run-1" || check.Name != "release bundle" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed release bundle check", check)
	}
	if len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypeReleaseBundle ||
		check.Evidence[0].Ref != "release-bundle:run-1" {
		t.Fatalf("evidence = %+v, want release bundle evidence", check.Evidence)
	}
	if check.Metadata["release"] != "m5" {
		t.Fatalf("check metadata = %+v, want copied release metadata", check.Metadata)
	}
	wantRequiredChecks := []string{"unit-tests", "diff-check"}
	if !reflect.DeepEqual(check.Metadata["required_check_ids"], wantRequiredChecks) {
		t.Fatalf("required check metadata = %#v, want %#v", check.Metadata["required_check_ids"], wantRequiredChecks)
	}
	wantRequiredCIGates := []string{"unit", "vet"}
	if !reflect.DeepEqual(check.Metadata["required_ci_gates"], wantRequiredCIGates) {
		t.Fatalf("required CI gate metadata = %#v, want %#v", check.Metadata["required_ci_gates"], wantRequiredCIGates)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["version"] != ReleaseBundleVersion ||
		metadata["run_id"] != "run-1" ||
		metadata["thread_id"] != "thread-1" ||
		metadata["mode"] != string(ModeWrite) ||
		metadata["outcome"] != string(gopact.RunCompleted) ||
		metadata["action"] != string(ActionRelease) ||
		metadata["action_status"] != string(ActionAllowed) ||
		metadata["report_status"] != string(gopact.VerificationStatusPassed) ||
		metadata["gate_status"] != string(GatePassed) ||
		metadata["review_status"] != string(ReviewApproved) ||
		metadata["reviewer"] != "reviewer-1" ||
		metadata["gate_report_status"] != string(gopact.VerificationStatusPassed) ||
		metadata["gate_review_status"] != string(ReviewApproved) ||
		metadata["max_entropy_severity"] != string(gopact.EntropySeverityLow) ||
		metadata["process_task_id"] != "devagent:run-1:release" ||
		metadata["release_gate_input_id"] != "devagent:run-1:release_gate" ||
		metadata["review_intervention_id"] != "devagent:run-1:review:reviewer-1" ||
		metadata["check_count"] != 3 ||
		metadata["entropy_audit_count"] != 1 ||
		metadata["process_input_count"] != 2 ||
		metadata["process_intervention_count"] != 1 {
		t.Fatalf("evidence metadata = %+v, want release bundle metadata", metadata)
	}
	if metadata["release"] != "m5" {
		t.Fatalf("evidence metadata = %+v, want copied release metadata", metadata)
	}
}

func TestRecordReleaseBundleCheckPreservesCanonicalMetadata(t *testing.T) {
	bundle := releaseBundleFixture(t)
	bundle.Metadata["run_id"] = "forged-run"
	bundle.Metadata["gate_status"] = string(GateRejected)
	bundle.Metadata["reviewer"] = "forged-reviewer"
	bundle.Metadata["release"] = "m5"
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseBundleCheck(recorder, bundle); err != nil {
		t.Fatalf("RecordReleaseBundleCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	if check.Metadata["run_id"] != "run-1" ||
		check.Metadata["gate_status"] != string(GatePassed) ||
		check.Metadata["reviewer"] != "reviewer-1" {
		t.Fatalf("check metadata = %+v, want canonical release bundle fields preserved", check.Metadata)
	}
	if check.Metadata["release"] != "m5" {
		t.Fatalf("check metadata = %+v, want non-conflicting caller metadata preserved", check.Metadata)
	}
	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["run_id"] != "run-1" ||
		evidenceMetadata["gate_status"] != string(GatePassed) ||
		evidenceMetadata["reviewer"] != "reviewer-1" {
		t.Fatalf("evidence metadata = %+v, want canonical release bundle fields preserved", evidenceMetadata)
	}
	if evidenceMetadata["release"] != "m5" {
		t.Fatalf("evidence metadata = %+v, want non-conflicting caller metadata preserved", evidenceMetadata)
	}
}

func TestRecordReleaseBundleCheckCapturesObservedWorkflowRelease(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 18, 0, 0, 0, time.UTC)
	ids := gopact.RuntimeIDs{
		RunID:     "run-self-bootstrap-1",
		ThreadID:  "thread-1",
		UserID:    "user-1",
		SessionID: "session-1",
		AgentID:   "dev-agent-1",
		AppID:     "gopact",
		CallID:    "call-1",
		TraceID:   "trace-1",
	}
	reportRecorder := gopact.NewVerificationRecorder()
	for _, check := range []gopact.VerificationCheck{
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		verificationCheck("trajectory-check", gopact.VerificationStatusPassed, "trajectory"),
	} {
		if err := reportRecorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	export := gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       ids,
		Outcome:   gopact.RunCompleted,
		CreatedAt: createdAt,
	}
	report, err := reportRecorder.Report(export)
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	review := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "human",
		Summary:  "self-bootstrap docs/test change approved",
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
				Summary:   "workflow evidence is complete",
				CreatedAt: createdAt,
			},
		},
	}
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:          ModeWrite,
		Report:        report,
		Review:        review,
		EntropyAudits: []gopact.EntropyAudit{entropy},
	}, RequireCheckIDs("unit-tests", "diff-check", "trajectory-check"), RequireEvidenceTypes("command", "diff", "trajectory"))
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
					Summary: "add self-bootstrap release evidence",
					Diff:    "diff --git a/private b/private\n+raw diff must not enter release evidence\n",
					Files: []PatchFile{
						{Path: "templates/devagent/release_bundle_evidence_test.go", Intent: "cover workflow release evidence"},
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
	export.Tasks = append([]gopact.TaskRecord{workflow.Task}, workflow.Tasks...)
	export.Inputs = workflow.Inputs
	export.Interventions = workflow.Interventions
	export.VerificationReports = []gopact.VerificationReport{report}
	process, err := WorkflowActionProcessRecords(workflow, 3)
	if err != nil {
		t.Fatalf("WorkflowActionProcessRecords() error = %v", err)
	}

	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		EntropyAudits:         []gopact.EntropyAudit{entropy},
		RequiredCheckIDs:      []string{"unit-tests", "diff-check", "trajectory-check"},
		RequiredEvidenceTypes: []string{"command", "diff", "trajectory"},
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
	if bundle.Process.Task.ParentID != workflow.Task.ID ||
		bundle.Process.Task.Metadata["workflow_id"] != workflow.Task.ID ||
		bundle.Process.Task.Metadata["workflow_action_index"] != 3 {
		t.Fatalf("bundle process task = %+v, want workflow release child", bundle.Process.Task)
	}
	for _, input := range bundle.Process.Inputs {
		if strings.Contains(toString(input.Value), "raw diff") {
			t.Fatalf("bundle process input leaked raw diff: %+v", input)
		}
	}

	recorder := gopact.NewVerificationRecorder()
	if err := RecordReleaseBundleCheck(recorder, bundle); err != nil {
		t.Fatalf("RecordReleaseBundleCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Metadata["release"] != "self-bootstrap" {
		t.Fatalf("check metadata = %+v, want caller release metadata", check.Metadata)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["process_task_id"] != "devagent:run-self-bootstrap-1:release" ||
		metadata["process_input_count"] != 1 ||
		metadata["process_intervention_count"] != 1 ||
		metadata["release_gate_input_id"] != "devagent:run-self-bootstrap-1:release_gate" ||
		metadata["review_intervention_id"] != "devagent:run-self-bootstrap-1:review:human" ||
		metadata["max_entropy_severity"] != string(gopact.EntropySeverityLow) {
		t.Fatalf("evidence metadata = %+v, want workflow release metadata", metadata)
	}
	if metadata["release"] != "self-bootstrap" {
		t.Fatalf("evidence metadata = %+v, want caller release metadata", metadata)
	}
}

func TestRecordReleaseBundleCheckRejectsInvalidBundleWithoutRecording(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseBundleCheck(nil, releaseBundleFixture(t)); err == nil {
		t.Fatal("RecordReleaseBundleCheck(nil) error = nil, want error")
	}
	if err := RecordReleaseBundleCheck(recorder, ReleaseBundle{}); !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("RecordReleaseBundleCheck(empty) error = %v, want ErrInvalidReleaseBundle", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after invalid bundle", len(recorder.Checks()))
	}
}

func releaseBundleFixture(t *testing.T) ReleaseBundle {
	t.Helper()

	createdAt := time.Date(2026, 6, 25, 14, 30, 0, 0, time.UTC)
	export := gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome:   gopact.RunCompleted,
		CreatedAt: createdAt,
	}
	report := verificationReportWithChecks(t,
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		ciGateCheck("core-ci", map[string]gopact.VerificationStatus{
			"unit": gopact.VerificationStatusPassed,
			"vet":  gopact.VerificationStatusPassed,
		}),
	)
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "reviewer-1",
			Summary:  "safe docs/test change",
		},
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:        "entropy-1",
				Status:    gopact.VerificationStatusPassed,
				IDs:       export.IDs,
				CreatedAt: createdAt,
				Findings: []gopact.EntropyFinding{
					{
						ID:        "finding-1",
						Category:  gopact.EntropyProcess,
						Severity:  gopact.EntropySeverityLow,
						Summary:   "process evidence is complete",
						CreatedAt: createdAt,
					},
				},
			},
		},
	}, RequireCheckIDs("unit-tests", "diff-check"), RequireEvidenceTypes("command", "diff"), RequireCIGates("unit", "vet"))
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export: export,
		Report: report,
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:        "entropy-1",
				Status:    gopact.VerificationStatusPassed,
				IDs:       export.IDs,
				CreatedAt: createdAt,
				Findings: []gopact.EntropyFinding{
					{
						ID:        "finding-1",
						Category:  gopact.EntropyProcess,
						Severity:  gopact.EntropySeverityLow,
						Summary:   "process evidence is complete",
						CreatedAt: createdAt,
					},
				},
			},
		},
		RequiredCheckIDs:      []string{"unit-tests", "diff-check"},
		RequiredEvidenceTypes: []string{"command", "diff"},
		RequiredCIGates:       []string{"unit", "vet"},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "update release docs",
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "reviewer-1",
			Summary:  "safe docs/test change",
		},
		Gate:      gate,
		CreatedAt: createdAt,
		Metadata:  map[string]any{"release": "m5"},
	})
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	return bundle
}
