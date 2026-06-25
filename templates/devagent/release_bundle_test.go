package devagent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBuildReleaseBundleCapturesReleaseEvidenceWithoutRawDiff(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 14, 30, 0, 0, time.UTC)
	export := gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Outcome:   gopact.RunCompleted,
		CreatedAt: createdAt,
	}
	report := verificationReportWithChecks(t,
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		verificationCheck("checkpoint-check", gopact.VerificationStatusPassed, "checkpoint"),
		verificationCheck("trajectory-check", gopact.VerificationStatusPassed, "trajectory"),
	)
	gate, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe docs/test change",
		},
	}, RequireCheckIDs("unit-tests", "diff-check"), RequireEvidenceTypes("command", "diff", "checkpoint", "trajectory"))
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}

	bundle, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		RequiredCheckIDs:      []string{"unit-tests", "diff-check"},
		RequiredEvidenceTypes: []string{"command", "diff", "checkpoint", "trajectory"},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "update release docs",
			Diff:    "diff --git a/secret b/secret\n+raw diff must not be copied into process records\n",
			Files: []PatchFile{
				{Path: "README.md", Intent: "document release bundle"},
			},
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe docs/test change",
		},
		Gate:      gate,
		CreatedAt: createdAt,
		Metadata:  map[string]any{"scope": "m5-release"},
	})
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}

	if bundle.Version != ReleaseBundleVersion ||
		bundle.IDs.RunID != "run-1" ||
		bundle.Mode != ModeWrite ||
		bundle.Outcome != gopact.RunCompleted ||
		bundle.Gate.Status != GatePassed ||
		!bundle.CreatedAt.Equal(createdAt) {
		t.Fatalf("bundle = %+v, want release-ready write bundle", bundle)
	}
	if bundle.Metadata["scope"] != "m5-release" {
		t.Fatalf("bundle metadata = %+v, want copied metadata", bundle.Metadata)
	}
	if len(bundle.Process.Inputs) != 2 {
		t.Fatalf("process inputs = %+v, want patch and release gate inputs", bundle.Process.Inputs)
	}
	if bundle.Process.Inputs[0].Source != "devagent.patch" || bundle.Process.Inputs[1].Source != "devagent.release_gate" {
		t.Fatalf("process inputs = %+v, want patch then release gate", bundle.Process.Inputs)
	}
	patchValue, ok := bundle.Process.Inputs[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("patch input value = %T, want map", bundle.Process.Inputs[0].Value)
	}
	if strings.Contains(toString(patchValue["diff"]), "raw diff") {
		t.Fatalf("release bundle leaked raw diff in patch process input: %+v", patchValue)
	}
	if len(bundle.Process.Interventions) != 1 || bundle.Process.Interventions[0].Status != gopact.InterventionResolved {
		t.Fatalf("process interventions = %+v, want resolved review intervention", bundle.Process.Interventions)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle.Validate() error = %v", err)
	}
}

func TestBuildReleaseBundleDefensivelyCopiesRunExport(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.Report.Metadata = map[string]any{"report": "original"}
	input.Report.Checks[0].Metadata = map[string]any{"check": "original"}
	input.Report.Checks[0].Evidence[0].Metadata = map[string]any{"evidence": "original"}
	process, err := BuildProcessRecords(ProcessInput{
		IDs:    input.Export.IDs,
		Action: input.Action,
		Review: input.Review,
		Gate:   &input.Gate,
	})
	if err != nil {
		t.Fatalf("BuildProcessRecords() error = %v", err)
	}
	report := copyVerificationReport(input.Report)
	input.Export.Tasks = []gopact.TaskRecord{process.Task}
	input.Export.VerificationReports = []gopact.VerificationReport{report}
	input.Export.Metadata = map[string]any{"export": "original"}

	bundle, err := BuildReleaseBundle(input)
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}

	input.Export.Tasks[0].Input.(map[string]any)["mode"] = "mutated"
	input.Export.Tasks[0].Metadata["source"] = "mutated"
	input.Export.VerificationReports[0].Metadata["report"] = "mutated"
	input.Export.VerificationReports[0].Checks[0].Metadata["check"] = "mutated"
	input.Export.VerificationReports[0].Checks[0].Evidence[0].Metadata["evidence"] = "mutated"
	input.Export.Metadata["export"] = "mutated"

	taskInput, ok := bundle.RunExport.Tasks[0].Input.(map[string]any)
	if !ok {
		t.Fatalf("bundle run export task input = %T, want map", bundle.RunExport.Tasks[0].Input)
	}
	if taskInput["mode"] != string(ModeWrite) ||
		bundle.RunExport.Tasks[0].Metadata["source"] != "devagent" ||
		bundle.RunExport.VerificationReports[0].Metadata["report"] != "original" ||
		bundle.RunExport.VerificationReports[0].Checks[0].Metadata["check"] != "original" ||
		bundle.RunExport.VerificationReports[0].Checks[0].Evidence[0].Metadata["evidence"] != "original" ||
		bundle.RunExport.Metadata["export"] != "original" {
		t.Fatalf("bundle run export was mutated through input alias: %+v", bundle.RunExport)
	}
}

func TestBuildReleaseBundleUsesObservedWorkflowProcessRecords(t *testing.T) {
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
	if len(workflow.Tasks) != 2 || len(workflow.Inputs) != 1 || len(workflow.Interventions) != 1 {
		t.Fatalf("workflow records = %+v, want release child task/input/intervention", workflow)
	}
	input.Process = ProcessRecords{
		Task:          workflow.Tasks[1],
		Inputs:        workflow.Inputs,
		Interventions: workflow.Interventions,
	}
	input.Export.Tasks = append([]gopact.TaskRecord{workflow.Task}, workflow.Tasks...)
	input.Export.Inputs = workflow.Inputs
	input.Export.Interventions = workflow.Interventions

	bundle, err := BuildReleaseBundle(input)
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}

	if bundle.Process.Task.Metadata["workflow_id"] != workflow.Task.ID ||
		bundle.Process.Task.Metadata["workflow_action_index"] != 2 ||
		bundle.Process.Task.ParentID != workflow.Task.ID {
		t.Fatalf("bundle process task = %+v, want observed workflow child process record", bundle.Process.Task)
	}
	if bundle.RunExport.Tasks[0].ID != workflow.Task.ID || bundle.RunExport.Tasks[1].ParentID != workflow.Task.ID {
		t.Fatalf("bundle run export tasks = %+v, want workflow parent and children preserved", bundle.RunExport.Tasks)
	}
}

func TestBuildReleaseBundleRejectsRejectedGate(t *testing.T) {
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
	}
	report := verificationReport(t, gopact.VerificationStatusPassed)

	_, err := BuildReleaseBundle(ReleaseBundleInput{
		Export: export,
		Report: report,
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Gate: GateResult{
			Status: GateRejected,
			Mode:   ModeWrite,
			Reasons: []string{
				"verification failed",
			},
		},
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
}

func TestBuildReleaseBundleRejectsRunExportFailures(t *testing.T) {
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
		Failures: []gopact.FailureAttribution{
			{
				ID:      "failure-1",
				Kind:    gopact.FailureTool,
				IDs:     gopact.RuntimeIDs{RunID: "run-1"},
				Node:    "call_tool",
				Step:    2,
				Summary: "tool failed before recovery",
			},
		},
	}
	report := verificationReport(t, gopact.VerificationStatusPassed)

	_, err := BuildReleaseBundle(ReleaseBundleInput{
		Export: export,
		Report: report,
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe release",
		},
		Gate: GateResult{
			Status:       GatePassed,
			Mode:         ModeWrite,
			ReportStatus: report.Status,
			ReviewStatus: ReviewApproved,
		},
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "run export contains failure attribution failure-1") {
		t.Fatalf("BuildReleaseBundle() error = %v, want failure attribution rejection", err)
	}
}

func TestBuildReleaseBundleRejectsReleaseEvidenceThatIsNotReviewApproved(t *testing.T) {
	tests := []struct {
		name        string
		review      ReviewDecision
		wantMessage string
	}{
		{
			name:        "missing review",
			wantMessage: "review approval is required",
		},
		{
			name: "rejected review",
			review: ReviewDecision{
				Status:   ReviewRejected,
				Reviewer: "human",
				Summary:  "needs more evidence",
			},
			wantMessage: "review rejected: needs more evidence",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReleaseBundleInput(t)
			input.Review = tt.review

			_, err := BuildReleaseBundle(input)
			if !errors.Is(err, ErrReleaseGateRejected) {
				t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("BuildReleaseBundle() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestBuildReleaseBundleRejectsNonReleaseActionEvidence(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.Action.Action = ActionApplyPatch

	_, err := BuildReleaseBundle(input)
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "action apply_patch is not release") {
		t.Fatalf("BuildReleaseBundle() error = %v, want release action rejection", err)
	}
}

func TestBuildReleaseBundleRejectsNonWriteModeEvidence(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.Action.Mode = ModePlan
	input.Gate.Mode = ModePlan

	_, err := BuildReleaseBundle(input)
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "release bundle requires write mode") {
		t.Fatalf("BuildReleaseBundle() error = %v, want write mode rejection", err)
	}
}

func TestBuildReleaseBundleRejectsMissingGateSummaries(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*ReleaseBundleInput)
		wantMessage string
	}{
		{
			name: "missing report status",
			mutate: func(input *ReleaseBundleInput) {
				input.Gate.ReportStatus = ""
			},
			wantMessage: "release gate report status is required",
		},
		{
			name: "missing review status",
			mutate: func(input *ReleaseBundleInput) {
				input.Gate.ReviewStatus = ReviewUnknown
			},
			wantMessage: "release gate review status is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReleaseBundleInput(t)
			tt.mutate(&input)

			_, err := BuildReleaseBundle(input)
			if !errors.Is(err, ErrInvalidReleaseBundle) {
				t.Fatalf("BuildReleaseBundle() error = %v, want ErrInvalidReleaseBundle", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("BuildReleaseBundle() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestBuildReleaseBundleRejectsMismatchedGateSummaries(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*ReleaseBundleInput)
		wantMessage string
	}{
		{
			name: "mismatched report status",
			mutate: func(input *ReleaseBundleInput) {
				input.Gate.ReportStatus = gopact.VerificationStatusFailed
			},
			wantMessage: "release gate report status \"failed\" does not match verification report \"passed\"",
		},
		{
			name: "mismatched review status",
			mutate: func(input *ReleaseBundleInput) {
				input.Gate.ReviewStatus = ReviewRejected
			},
			wantMessage: "release gate review status \"rejected\" does not match review \"approved\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReleaseBundleInput(t)
			tt.mutate(&input)

			_, err := BuildReleaseBundle(input)
			if !errors.Is(err, ErrInvalidReleaseBundle) {
				t.Fatalf("BuildReleaseBundle() error = %v, want ErrInvalidReleaseBundle", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("BuildReleaseBundle() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestBuildReleaseBundleRejectsMismatchedGateEntropySummary(t *testing.T) {
	tests := []struct {
		name        string
		gateMax     gopact.EntropySeverity
		wantMessage string
	}{
		{
			name:        "missing max severity",
			wantMessage: "release gate max entropy severity \"\" does not match entropy audits \"low\"",
		},
		{
			name:        "wrong max severity",
			gateMax:     gopact.EntropySeverityMedium,
			wantMessage: "release gate max entropy severity \"medium\" does not match entropy audits \"low\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReleaseBundleInput(t)
			input.EntropyAudits = []gopact.EntropyAudit{
				entropyAuditWithFinding(t, gopact.VerificationStatusPassed, gopact.EntropySeverityLow),
			}
			input.Gate.MaxEntropySeverity = tt.gateMax

			_, err := BuildReleaseBundle(input)
			if !errors.Is(err, ErrInvalidReleaseBundle) {
				t.Fatalf("BuildReleaseBundle() error = %v, want ErrInvalidReleaseBundle", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("BuildReleaseBundle() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestBuildReleaseBundleRejectsFailedEntropyAudit(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.EntropyAudits = []gopact.EntropyAudit{
		entropyAuditWithFinding(t, gopact.VerificationStatusFailed, gopact.EntropySeverityHigh),
	}
	input.Gate.MaxEntropySeverity = gopact.EntropySeverityHigh

	_, err := BuildReleaseBundle(input)
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "entropy audit entropy-1 status failed") {
		t.Fatalf("BuildReleaseBundle() error = %v, want failed entropy rejection", err)
	}
}

func TestReleaseBundleValidateRejectsProcessRecordsFromDifferentRun(t *testing.T) {
	bundle := validReleaseBundle(t)
	bundle.Process.Task.IDs.RunID = "other-run"

	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "process task run id \"other-run\" does not match \"run-1\"") {
		t.Fatalf("Validate() error = %v, want process task run id mismatch", err)
	}
}

func TestReleaseBundleValidateRejectsRuntimeIdentityMismatch(t *testing.T) {
	ids := gopact.RuntimeIDs{
		RunID:     "run-1",
		ThreadID:  "thread-1",
		UserID:    "user-1",
		SessionID: "session-1",
		AgentID:   "agent-1",
		CallID:    "call-1",
	}
	tests := []struct {
		name        string
		mutate      func(*ReleaseBundle)
		wantMessage string
	}{
		{
			name: "run export thread id",
			mutate: func(bundle *ReleaseBundle) {
				bundle.RunExport.IDs.ThreadID = "other-thread"
			},
			wantMessage: "run export thread id \"other-thread\" does not match \"thread-1\"",
		},
		{
			name: "verification report user id",
			mutate: func(bundle *ReleaseBundle) {
				bundle.VerificationReport.IDs.UserID = "other-user"
			},
			wantMessage: "verification report user id \"other-user\" does not match \"user-1\"",
		},
		{
			name: "entropy audit agent id",
			mutate: func(bundle *ReleaseBundle) {
				bundle.EntropyAudits = []gopact.EntropyAudit{
					{
						ID:        "entropy-1",
						Status:    gopact.VerificationStatusPassed,
						IDs:       bundle.IDs,
						CreatedAt: time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC),
					},
				}
				bundle.EntropyAudits[0].IDs.AgentID = "other-agent"
			},
			wantMessage: "entropy audit entropy-1 agent id \"other-agent\" does not match \"agent-1\"",
		},
		{
			name: "process task session id",
			mutate: func(bundle *ReleaseBundle) {
				bundle.Process.Task.IDs.SessionID = "other-session"
			},
			wantMessage: "process task session id \"other-session\" does not match \"session-1\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := validReleaseBundleWithIDs(t, ids)
			tt.mutate(&bundle)

			err := bundle.Validate()
			if !errors.Is(err, ErrInvalidReleaseBundle) {
				t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestReleaseBundleValidateRejectsMismatchedRunExportVerificationReport(t *testing.T) {
	bundle := validReleaseBundle(t)
	bundle.RunExport.VerificationReports = []gopact.VerificationReport{copyVerificationReport(bundle.VerificationReport)}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() with matching exported report error = %v", err)
	}

	bundle.RunExport.VerificationReports[0].Checks[0].ID = "other-check"
	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "run export verification reports do not include bundle verification report") {
		t.Fatalf("Validate() error = %v, want exported report mismatch", err)
	}
}

func TestReleaseBundleValidateRejectsMismatchedRunExportProcessRecords(t *testing.T) {
	tests := []struct {
		name        string
		attach      func(*ReleaseBundle)
		mutate      func(*ReleaseBundle)
		wantMessage string
	}{
		{
			name: "task",
			attach: func(bundle *ReleaseBundle) {
				bundle.RunExport.Tasks = []gopact.TaskRecord{
					cloneTaskRecordForTest(bundle.Process.Task),
				}
			},
			mutate: func(bundle *ReleaseBundle) {
				bundle.RunExport.Tasks[0].Metadata["action"] = string(ActionApplyPatch)
			},
			wantMessage: "run export process tasks do not include bundle process task",
		},
		{
			name: "release gate input",
			attach: func(bundle *ReleaseBundle) {
				bundle.RunExport.Inputs = []gopact.InputRecord{
					cloneInputRecordForTest(bundle.Process.Inputs[0]),
				}
			},
			mutate: func(bundle *ReleaseBundle) {
				value, ok := bundle.RunExport.Inputs[0].Value.(map[string]any)
				if !ok {
					t.Fatalf("run export input value = %T, want map", bundle.RunExport.Inputs[0].Value)
				}
				value["report_status"] = string(gopact.VerificationStatusFailed)
			},
			wantMessage: "run export process inputs do not include bundle process input devagent:run-1:release_gate",
		},
		{
			name: "review intervention",
			attach: func(bundle *ReleaseBundle) {
				bundle.RunExport.Interventions = []gopact.InterventionRecord{
					cloneInterventionRecordForTest(bundle.Process.Interventions[0]),
				}
			},
			mutate: func(bundle *ReleaseBundle) {
				bundle.RunExport.Interventions[0].Metadata["reviewer"] = "other-human"
			},
			wantMessage: "run export process interventions do not include bundle process intervention devagent:run-1:review:human",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := validReleaseBundle(t)
			tt.attach(&bundle)
			if err := bundle.Validate(); err != nil {
				t.Fatalf("Validate() with matching exported process record error = %v", err)
			}

			tt.mutate(&bundle)
			err := bundle.Validate()
			if !errors.Is(err, ErrInvalidReleaseBundle) {
				t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestReleaseBundleValidateRejectsMissingReleaseGateProcessInput(t *testing.T) {
	bundle := validReleaseBundle(t)
	bundle.Process.Inputs = nil

	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "process release gate input is required") {
		t.Fatalf("Validate() error = %v, want missing release gate input", err)
	}
}

func TestReleaseBundleValidateRejectsMissingApprovedReviewIntervention(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*ReleaseBundle)
		wantMessage string
	}{
		{
			name: "missing review intervention",
			mutate: func(bundle *ReleaseBundle) {
				bundle.Process.Interventions = nil
			},
			wantMessage: "process resolved review intervention is required",
		},
		{
			name: "rejected review intervention",
			mutate: func(bundle *ReleaseBundle) {
				bundle.Process.Interventions[0].Status = gopact.InterventionRejected
			},
			wantMessage: "process review intervention status \"rejected\" is not resolved",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := validReleaseBundle(t)
			tt.mutate(&bundle)

			err := bundle.Validate()
			if !errors.Is(err, ErrInvalidReleaseBundle) {
				t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestReleaseBundleValidateRejectsNonCompletedReleaseProcessTask(t *testing.T) {
	bundle := validReleaseBundle(t)
	bundle.Process.Task.Status = gopact.TaskFailed

	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "process task status \"failed\" is not completed") {
		t.Fatalf("Validate() error = %v, want failed process task rejection", err)
	}
}

func TestReleaseBundleValidateRejectsMismatchedProcessTaskMetadata(t *testing.T) {
	bundle := validReleaseBundle(t)
	bundle.Process.Task.Metadata["action"] = string(ActionApplyPatch)

	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "process task metadata action \"apply_patch\" does not match \"release\"") {
		t.Fatalf("Validate() error = %v, want process task action mismatch", err)
	}
}

func TestReleaseBundleValidateRejectsMismatchedReleaseGateProcessInput(t *testing.T) {
	bundle := validReleaseBundle(t)
	value, ok := bundle.Process.Inputs[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("release gate input value = %T, want map", bundle.Process.Inputs[0].Value)
	}
	value["report_status"] = string(gopact.VerificationStatusFailed)

	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "process release gate input report_status \"failed\" does not match \"passed\"") {
		t.Fatalf("Validate() error = %v, want release gate report status mismatch", err)
	}
}

func TestReleaseBundleValidateRejectsMismatchedReviewIntervention(t *testing.T) {
	bundle := validReleaseBundle(t)
	bundle.Process.Interventions[0].Metadata["reviewer"] = "other-human"

	err := bundle.Validate()
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("Validate() error = %v, want ErrInvalidReleaseBundle", err)
	}
	if !strings.Contains(err.Error(), "process review intervention reviewer \"other-human\" does not match \"human\"") {
		t.Fatalf("Validate() error = %v, want review intervention reviewer mismatch", err)
	}
}

func TestBuildReleaseBundleRejectsMismatchedReport(t *testing.T) {
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
	}
	report := verificationReport(t, gopact.VerificationStatusPassed)
	report.IDs.RunID = "other-run"

	_, err := BuildReleaseBundle(ReleaseBundleInput{
		Export: export,
		Report: report,
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe release",
		},
		Gate: GateResult{
			Status:       GatePassed,
			Mode:         ModeWrite,
			ReportStatus: report.Status,
			ReviewStatus: ReviewApproved,
		},
	})
	if !errors.Is(err, ErrInvalidReleaseBundle) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrInvalidReleaseBundle", err)
	}
}

func TestBuildReleaseBundleRejectsMissingRequiredEvidence(t *testing.T) {
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
	}
	report := verificationReport(t, gopact.VerificationStatusPassed, "command")

	_, err := BuildReleaseBundle(ReleaseBundleInput{
		Export:                export,
		Report:                report,
		RequiredEvidenceTypes: []string{"command", "diff"},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe release",
		},
		Gate: GateResult{
			Status:       GatePassed,
			Mode:         ModeWrite,
			ReportStatus: report.Status,
			ReviewStatus: ReviewApproved,
		},
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "verification evidence type diff is required") {
		t.Fatalf("BuildReleaseBundle() error = %v, want missing diff evidence", err)
	}
}

func TestBuildReleaseBundleRequiresPassedCIGates(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.Report = verificationReportWithChecks(t,
		ciGateCheck("core-ci", map[string]gopact.VerificationStatus{
			"unit": gopact.VerificationStatusPassed,
			"vet":  gopact.VerificationStatusPassed,
		}),
	)
	input.Gate.ReportStatus = input.Report.Status
	input.RequiredCIGates = []string{"unit", "vet"}

	bundle, err := BuildReleaseBundle(input)
	if err != nil {
		t.Fatalf("BuildReleaseBundle() error = %v", err)
	}
	if got := bundle.RequiredCIGates; len(got) != 2 || got[0] != "unit" || got[1] != "vet" {
		t.Fatalf("bundle.RequiredCIGates = %#v, want unit/vet", got)
	}

	bundle.VerificationReport.Checks[0].Evidence = bundle.VerificationReport.Checks[0].Evidence[:1]
	err = bundle.Validate()
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("Validate() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "required CI gate vet is missing") {
		t.Fatalf("Validate() error = %v, want missing vet CI gate", err)
	}
}

func TestBuildReleaseBundleRejectsMissingRequiredCIGates(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.RequiredCIGates = []string{"unit", "race"}

	_, err := BuildReleaseBundle(input)
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "required CI gate race is missing") {
		t.Fatalf("BuildReleaseBundle() error = %v, want missing race CI gate", err)
	}
}

func TestBuildReleaseBundleRejectsRequiredCIGateWithoutStatus(t *testing.T) {
	input := validReleaseBundleInput(t)
	input.Report = verificationReportWithChecks(t,
		ciGateWithoutStatusCheck("ci-without-status", "coverage"),
	)
	input.Gate.ReportStatus = input.Report.Status
	input.RequiredCIGates = []string{"coverage"}

	_, err := BuildReleaseBundle(input)
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("BuildReleaseBundle() error = %v, want ErrReleaseGateRejected", err)
	}
	if !strings.Contains(err.Error(), "required CI gate coverage is missing") {
		t.Fatalf("BuildReleaseBundle() error = %v, want missing coverage CI gate", err)
	}
}

func validReleaseBundleInput(t *testing.T) ReleaseBundleInput {
	t.Helper()

	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
	}
	report := verificationReport(t, gopact.VerificationStatusPassed)
	return ReleaseBundleInput{
		Export: export,
		Report: report,
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
		},
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "human",
			Summary:  "safe release",
		},
		Gate: GateResult{
			Status:       GatePassed,
			Mode:         ModeWrite,
			ReportStatus: report.Status,
			ReviewStatus: ReviewApproved,
		},
	}
}

func validReleaseBundle(t *testing.T) ReleaseBundle {
	t.Helper()

	bundle, err := BuildReleaseBundle(validReleaseBundleInput(t))
	if err != nil {
		t.Fatalf("BuildReleaseBundle(valid) error = %v", err)
	}
	return bundle
}

func validReleaseBundleWithIDs(t *testing.T, ids gopact.RuntimeIDs) ReleaseBundle {
	t.Helper()

	input := validReleaseBundleInput(t)
	input.Export.IDs = ids
	input.Report.IDs = ids
	bundle, err := BuildReleaseBundle(input)
	if err != nil {
		t.Fatalf("BuildReleaseBundle(valid with ids) error = %v", err)
	}
	return bundle
}

func cloneTaskRecordForTest(record gopact.TaskRecord) gopact.TaskRecord {
	out := record
	out.Input = cloneAnyMapForTest(record.Input)
	out.Output = cloneAnyMapForTest(record.Output)
	out.Metadata = copyDevAgentMetadata(record.Metadata)
	return out
}

func cloneInputRecordForTest(record gopact.InputRecord) gopact.InputRecord {
	out := record
	out.Value = cloneAnyMapForTest(record.Value)
	out.Metadata = copyDevAgentMetadata(record.Metadata)
	return out
}

func cloneInterventionRecordForTest(record gopact.InterventionRecord) gopact.InterventionRecord {
	out := record
	out.Metadata = copyDevAgentMetadata(record.Metadata)
	return out
}

func cloneAnyMapForTest(value any) any {
	values, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := make(map[string]any, len(values))
	for key, val := range values {
		out[key] = val
	}
	return out
}

func entropyAuditWithFinding(t *testing.T, status gopact.VerificationStatus, severity gopact.EntropySeverity) gopact.EntropyAudit {
	t.Helper()

	audit := gopact.EntropyAudit{
		ID:        "entropy-1",
		Status:    status,
		IDs:       gopact.RuntimeIDs{RunID: "run-1"},
		CreatedAt: time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC),
		Findings: []gopact.EntropyFinding{
			{
				ID:        "finding-1",
				Category:  gopact.EntropySecurity,
				Severity:  severity,
				Summary:   "observed entropy finding",
				CreatedAt: time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC),
			},
		},
	}
	if err := audit.Validate(); err != nil {
		t.Fatalf("entropy audit fixture invalid: %v", err)
	}
	return audit
}
