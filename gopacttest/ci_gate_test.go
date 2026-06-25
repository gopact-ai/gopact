package gopacttest

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
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
