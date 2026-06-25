package gopacttest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

func TestRecordCIGateSuiteCheckRecordsPassedSuite(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCIGateSuiteCheck(recorder, CIGateSuite{
		ID:            "core-ci",
		Name:          "core CI gates",
		RequiredGates: []string{"unit", "vet"},
		Results: []CIGateResult{
			{
				Gate: "unit",
				Result: CommandResult{
					Command:  []string{"go", "test", "-count=1", "./..."},
					ExitCode: 0,
					Stdout:   "ok",
				},
			},
			{
				Gate: "vet",
				Result: CommandResult{
					Command:  []string{"go", "vet", "./..."},
					ExitCode: 0,
				},
			},
		},
		Metadata: map[string]any{"profile": "release"},
	}); err != nil {
		t.Fatalf("RecordCIGateSuiteCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "core-ci" || check.Name != "core CI gates" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed core CI check", check)
	}
	if len(check.Evidence) != 2 {
		t.Fatalf("evidence = %+v, want one evidence item per gate", check.Evidence)
	}
	if check.Evidence[0].Type != VerificationEvidenceTypeCIGate ||
		check.Evidence[0].Ref != "ci-gate:unit" ||
		check.Evidence[0].Metadata["gate"] != "unit" ||
		check.Evidence[0].Metadata["status"] != string(gopact.VerificationStatusPassed) {
		t.Fatalf("first evidence = %+v, want unit gate evidence", check.Evidence[0])
	}
	if check.Metadata["profile"] != "release" ||
		check.Metadata["gate_count"] != 2 ||
		check.Metadata["passed_gate_count"] != 2 ||
		check.Metadata["failed_gate_count"] != 0 ||
		check.Metadata["skipped_gate_count"] != 0 {
		t.Fatalf("metadata = %+v, want CI gate counts and caller metadata", check.Metadata)
	}
	required, ok := check.Metadata["required_gates"].([]string)
	if !ok || !reflect.DeepEqual(required, []string{"unit", "vet"}) {
		t.Fatalf("required gates = %#v, want copied required gates", check.Metadata["required_gates"])
	}
}

func TestRecordCIGateSuiteCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	err := RecordCIGateSuiteCheck(recorder, CIGateSuite{
		RequiredGates: []string{"unit"},
		Results: []CIGateResult{
			{
				Gate: "unit",
				Result: CommandResult{
					Command:  []string{"go", "test", "-count=1", "./..."},
					ExitCode: 0,
					Stdout:   "ok",
				},
				Metadata: map[string]any{
					"gate":      "forged-gate",
					"status":    string(gopact.VerificationStatusFailed),
					"command":   []string{"forged"},
					"exit_code": 99,
					"stdout":    "forged stdout",
					"profile":   "release",
				},
			},
		},
		Metadata: map[string]any{
			"gate_count":         99,
			"passed_gate_count":  99,
			"failed_gate_count":  99,
			"skipped_gate_count": 99,
			"required_gates":     []string{"forged"},
			"profile":            "release",
		},
	})
	if err != nil {
		t.Fatalf("RecordCIGateSuiteCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	if check.Metadata["gate_count"] != 1 ||
		check.Metadata["passed_gate_count"] != 1 ||
		check.Metadata["failed_gate_count"] != 0 ||
		check.Metadata["skipped_gate_count"] != 0 {
		t.Fatalf("metadata = %+v, want canonical CI suite counts preserved", check.Metadata)
	}
	required, ok := check.Metadata["required_gates"].([]string)
	if !ok || !reflect.DeepEqual(required, []string{"unit"}) {
		t.Fatalf("required gates = %#v, want canonical required gates", check.Metadata["required_gates"])
	}
	if check.Metadata["profile"] != "release" {
		t.Fatalf("metadata = %+v, want supplemental suite metadata preserved", check.Metadata)
	}

	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["gate"] != "unit" ||
		evidenceMetadata["status"] != string(gopact.VerificationStatusPassed) ||
		evidenceMetadata["exit_code"] != 0 ||
		evidenceMetadata["stdout"] != "ok" {
		t.Fatalf("evidence metadata = %+v, want canonical CI gate evidence preserved", evidenceMetadata)
	}
	command, ok := evidenceMetadata["command"].([]string)
	if !ok || !reflect.DeepEqual(command, []string{"go", "test", "-count=1", "./..."}) {
		t.Fatalf("evidence command = %#v, want canonical command", evidenceMetadata["command"])
	}
	if evidenceMetadata["profile"] != "release" {
		t.Fatalf("evidence metadata = %+v, want supplemental gate metadata preserved", evidenceMetadata)
	}
}

func TestRecordCIGateSuiteCheckNormalizesGateNames(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCIGateSuiteCheck(recorder, CIGateSuite{
		RequiredGates: []string{"unit"},
		Results: []CIGateResult{
			{
				Gate: " unit ",
				Result: CommandResult{
					Command:  []string{"go", "test", "-count=1", "./..."},
					ExitCode: 0,
				},
			},
		},
	}); err != nil {
		t.Fatalf("RecordCIGateSuiteCheck() error = %v", err)
	}

	evidence := recorder.Checks()[0].Evidence[0]
	if evidence.Ref != "ci-gate:unit" ||
		evidence.Metadata["gate"] != "unit" ||
		evidence.Summary != "unit gate passed" {
		t.Fatalf("evidence = %+v, want normalized gate name", evidence)
	}
}

func TestRecordCIGateSuiteCheckRecordsFailedSuiteBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordCIGateSuiteCheck(recorder, CIGateSuite{
		RequiredGates: []string{"unit", "lint"},
		Results: []CIGateResult{
			{
				Gate: "unit",
				Result: CommandResult{
					Command:  []string{"go", "test", "-count=1", "./..."},
					ExitCode: 0,
				},
			},
			{
				Gate: "lint",
				Result: CommandResult{
					Command:  []string{"golangci-lint", "run", "./..."},
					ExitCode: 1,
					Stderr:   "lint failed",
				},
			},
		},
	})
	if !errors.Is(err, ErrCIGateFailed) {
		t.Fatalf("RecordCIGateSuiteCheck() error = %v, want ErrCIGateFailed", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["failed_gate_count"] != 1 {
		t.Fatalf("metadata = %+v, want one failed gate", check.Metadata)
	}
	if check.Evidence[1].Metadata["stderr"] != "lint failed" ||
		check.Evidence[1].Metadata["exit_code"] != 1 {
		t.Fatalf("failed evidence metadata = %+v, want command failure metadata", check.Evidence[1].Metadata)
	}
}

func TestRecordCIGateSuiteCheckRejectsMissingRequiredGateWithoutRecording(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordCIGateSuiteCheck(recorder, CIGateSuite{
		RequiredGates: []string{"unit", "race"},
		Results: []CIGateResult{
			{
				Gate: "unit",
				Result: CommandResult{
					Command:  []string{"go", "test", "-count=1", "./..."},
					ExitCode: 0,
				},
			},
		},
	})
	if !errors.Is(err, ErrCIGateRequired) {
		t.Fatalf("RecordCIGateSuiteCheck() error = %v, want ErrCIGateRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after incomplete suite", len(recorder.Checks()))
	}

	if err := RecordCIGateSuiteCheck(nil, CIGateSuite{RequiredGates: []string{"unit"}}); err == nil {
		t.Fatal("RecordCIGateSuiteCheck(nil) error = nil, want error")
	}
}

func TestRecordCIRunCheckRecordsRemoteRunAsCIGateEvidence(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	startedAt := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Minute)

	if err := RecordCIRunCheck(recorder, CIRun{
		ID:            "github-main-ci",
		Name:          "GitHub main CI",
		Provider:      "github-actions",
		Repository:    "gopact-ai/gopact",
		Workflow:      "ci",
		RunID:         "28201163807",
		URL:           "https://github.com/gopact-ai/gopact/actions/runs/28201163807",
		HeadSHA:       "25265c0fe2663e0feda8b4ebdfa5a079975b617d",
		HeadBranch:    "main",
		Status:        "completed",
		Conclusion:    "success",
		RequiredGates: []string{"test", "race", "security"},
		Gates: []CIRunGate{
			{
				Gate:        "test",
				Status:      gopact.VerificationStatusPassed,
				Job:         "test",
				Step:        "go test",
				URL:         "https://github.com/gopact-ai/gopact/actions/runs/28201163807/job/1",
				StartedAt:   startedAt,
				CompletedAt: completedAt,
				Duration:    2 * time.Minute,
			},
			{Gate: "race", Status: gopact.VerificationStatusPassed, Job: "race"},
			{Gate: "security", Status: gopact.VerificationStatusPassed, Job: "govulncheck"},
		},
		Metadata: map[string]any{"profile": "release"},
	}); err != nil {
		t.Fatalf("RecordCIRunCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "github-main-ci" ||
		check.Name != "GitHub main CI" ||
		check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed remote CI run check", check)
	}
	if len(check.Evidence) != 3 {
		t.Fatalf("evidence = %+v, want one evidence item per remote gate", check.Evidence)
	}
	if check.Metadata["ci_provider"] != "github-actions" ||
		check.Metadata["repository"] != "gopact-ai/gopact" ||
		check.Metadata["workflow"] != "ci" ||
		check.Metadata["run_id"] != "28201163807" ||
		check.Metadata["head_sha"] != "25265c0fe2663e0feda8b4ebdfa5a079975b617d" ||
		check.Metadata["head_branch"] != "main" ||
		check.Metadata["status"] != "completed" ||
		check.Metadata["conclusion"] != "success" ||
		check.Metadata["url"] != "https://github.com/gopact-ai/gopact/actions/runs/28201163807" ||
		check.Metadata["profile"] != "release" {
		t.Fatalf("metadata = %+v, want canonical remote CI run metadata", check.Metadata)
	}
	if check.Metadata["gate_count"] != 3 ||
		check.Metadata["passed_gate_count"] != 3 ||
		check.Metadata["failed_gate_count"] != 0 ||
		check.Metadata["skipped_gate_count"] != 0 {
		t.Fatalf("metadata = %+v, want remote CI gate counts", check.Metadata)
	}
	required, ok := check.Metadata["required_gates"].([]string)
	if !ok || !reflect.DeepEqual(required, []string{"test", "race", "security"}) {
		t.Fatalf("required gates = %#v, want copied required gates", check.Metadata["required_gates"])
	}

	evidence := check.Evidence[0]
	if evidence.Type != VerificationEvidenceTypeCIGate ||
		evidence.Ref != "ci-gate:test" ||
		evidence.Metadata["gate"] != "test" ||
		evidence.Metadata["status"] != string(gopact.VerificationStatusPassed) ||
		evidence.Metadata["ci_provider"] != "github-actions" ||
		evidence.Metadata["repository"] != "gopact-ai/gopact" ||
		evidence.Metadata["workflow"] != "ci" ||
		evidence.Metadata["run_id"] != "28201163807" ||
		evidence.Metadata["job"] != "test" ||
		evidence.Metadata["step"] != "go test" ||
		evidence.Metadata["url"] != "https://github.com/gopact-ai/gopact/actions/runs/28201163807/job/1" ||
		evidence.Metadata["duration_ms"] != int64(120000) ||
		evidence.Metadata["started_at"] != startedAt.Format(time.RFC3339Nano) ||
		evidence.Metadata["completed_at"] != completedAt.Format(time.RFC3339Nano) {
		t.Fatalf("first evidence = %+v, want canonical remote CI gate evidence", evidence)
	}
}

func TestRecordCIRunCheckEvidenceIsDevAgentCompatible(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCIRunCheck(recorder, CIRun{
		Provider:      "github-actions",
		Repository:    "gopact-ai/gopact",
		Workflow:      "ci",
		RunID:         "28201163807",
		RequiredGates: []string{"test", "race"},
		Gates: []CIRunGate{
			{Gate: "test", Status: gopact.VerificationStatusPassed},
			{Gate: "race", Status: gopact.VerificationStatusPassed},
		},
	}); err != nil {
		t.Fatalf("RecordCIRunCheck() error = %v", err)
	}

	report := gopact.VerificationReport{
		Version:     gopact.VerificationReportVersion,
		IDs:         gopact.RuntimeIDs{RunID: "run-1"},
		Outcome:     gopact.RunCompleted,
		Status:      gopact.VerificationStatusPassed,
		Checks:      recorder.Checks(),
		PassedCount: 1,
		CreatedAt:   time.Now(),
	}
	result, err := devagent.EvaluateReleaseGate(
		devagent.GateInput{
			Mode:   devagent.ModeWrite,
			Report: report,
			Review: devagent.ReviewDecision{Status: devagent.ReviewApproved},
		},
		devagent.RequireCIGates("test", "race"),
	)
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	if result.Status != devagent.GatePassed {
		t.Fatalf("gate status = %q, want passed", result.Status)
	}
}

func TestRecordCIRunCheckNormalizesGateNames(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCIRunCheck(recorder, CIRun{
		RequiredGates: []string{"test"},
		Gates: []CIRunGate{
			{Gate: " test ", Status: gopact.VerificationStatusPassed},
		},
	}); err != nil {
		t.Fatalf("RecordCIRunCheck() error = %v", err)
	}

	evidence := recorder.Checks()[0].Evidence[0]
	if evidence.Ref != "ci-gate:test" ||
		evidence.Metadata["gate"] != "test" ||
		evidence.Summary != "test gate passed" {
		t.Fatalf("evidence = %+v, want normalized remote CI gate name", evidence)
	}
}

func TestRecordCIRunCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	err := RecordCIRunCheck(recorder, CIRun{
		Provider:      "github-actions",
		Repository:    "gopact-ai/gopact",
		Workflow:      "ci",
		RunID:         "28201163807",
		RequiredGates: []string{"test"},
		Gates: []CIRunGate{
			{
				Gate:   "test",
				Status: gopact.VerificationStatusPassed,
				Job:    "test",
				Metadata: map[string]any{
					"gate":        "forged",
					"status":      string(gopact.VerificationStatusFailed),
					"ci_provider": "forged-provider",
					"repository":  "forged/repo",
					"workflow":    "forged-workflow",
					"run_id":      "forged-run",
					"job":         "forged-job",
					"profile":     "release",
				},
			},
		},
		Metadata: map[string]any{
			"gate_count":         99,
			"passed_gate_count":  99,
			"failed_gate_count":  99,
			"skipped_gate_count": 99,
			"required_gates":     []string{"forged"},
			"ci_provider":        "forged-provider",
			"repository":         "forged/repo",
			"workflow":           "forged-workflow",
			"run_id":             "forged-run",
			"profile":            "release",
		},
	})
	if err != nil {
		t.Fatalf("RecordCIRunCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	if check.Metadata["gate_count"] != 1 ||
		check.Metadata["passed_gate_count"] != 1 ||
		check.Metadata["failed_gate_count"] != 0 ||
		check.Metadata["skipped_gate_count"] != 0 ||
		check.Metadata["ci_provider"] != "github-actions" ||
		check.Metadata["repository"] != "gopact-ai/gopact" ||
		check.Metadata["workflow"] != "ci" ||
		check.Metadata["run_id"] != "28201163807" ||
		check.Metadata["profile"] != "release" {
		t.Fatalf("metadata = %+v, want canonical CI run metadata preserved", check.Metadata)
	}
	required, ok := check.Metadata["required_gates"].([]string)
	if !ok || !reflect.DeepEqual(required, []string{"test"}) {
		t.Fatalf("required gates = %#v, want canonical required gates", check.Metadata["required_gates"])
	}

	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["gate"] != "test" ||
		evidenceMetadata["status"] != string(gopact.VerificationStatusPassed) ||
		evidenceMetadata["ci_provider"] != "github-actions" ||
		evidenceMetadata["repository"] != "gopact-ai/gopact" ||
		evidenceMetadata["workflow"] != "ci" ||
		evidenceMetadata["run_id"] != "28201163807" ||
		evidenceMetadata["job"] != "test" ||
		evidenceMetadata["profile"] != "release" {
		t.Fatalf("evidence metadata = %+v, want canonical CI gate metadata preserved", evidenceMetadata)
	}
}

func TestRecordCIRunCheckRecordsFailedRunBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordCIRunCheck(recorder, CIRun{
		RequiredGates: []string{"test", "lint"},
		Gates: []CIRunGate{
			{Gate: "test", Status: gopact.VerificationStatusPassed},
			{Gate: "lint", Status: gopact.VerificationStatusFailed, Job: "golangci-lint"},
		},
	})
	if !errors.Is(err, ErrCIGateFailed) {
		t.Fatalf("RecordCIRunCheck() error = %v, want ErrCIGateFailed", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["failed_gate_count"] != 1 {
		t.Fatalf("metadata = %+v, want one failed gate", check.Metadata)
	}
	if check.Evidence[1].Metadata["gate"] != "lint" ||
		check.Evidence[1].Metadata["status"] != string(gopact.VerificationStatusFailed) ||
		check.Evidence[1].Metadata["job"] != "golangci-lint" {
		t.Fatalf("failed evidence metadata = %+v, want remote CI failure metadata", check.Evidence[1].Metadata)
	}
}

func TestRecordCIRunCheckRejectsInvalidRunWithoutRecording(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordCIRunCheck(recorder, CIRun{
		RequiredGates: []string{"test", "race"},
		Gates:         []CIRunGate{{Gate: "test", Status: gopact.VerificationStatusPassed}},
	})
	if !errors.Is(err, ErrCIGateRequired) {
		t.Fatalf("RecordCIRunCheck() missing gate error = %v, want ErrCIGateRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after incomplete remote CI run", len(recorder.Checks()))
	}

	err = RecordCIRunCheck(recorder, CIRun{
		Gates: []CIRunGate{{Gate: "test", Status: gopact.VerificationStatusPartial}},
	})
	if !errors.Is(err, ErrCIGateRequired) {
		t.Fatalf("RecordCIRunCheck() invalid status error = %v, want ErrCIGateRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after invalid remote CI run", len(recorder.Checks()))
	}

	if err := RecordCIRunCheck(nil, CIRun{Gates: []CIRunGate{{Gate: "test", Status: gopact.VerificationStatusPassed}}}); err == nil {
		t.Fatal("RecordCIRunCheck(nil) error = nil, want error")
	}
}
