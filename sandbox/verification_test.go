package sandbox

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordExecCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordExecCheck(recorder, ExecCheck{
		ID:        "go-test",
		Name:      "unit tests",
		SessionID: "sandbox-1",
		Request: ExecRequest{
			Command: []string{"go", "test", "./..."},
			Metadata: map[string]any{
				"purpose": "verification",
			},
		},
		Result: ExecResult{
			ExitCode: 0,
			Stdout:   []byte("ok\n"),
			Usage:    ResourceUsage{Duration: 1500 * time.Millisecond},
		},
		Metadata: map[string]any{
			"gate": "pre-release",
		},
	})
	if err != nil {
		t.Fatalf("RecordExecCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "go-test" || check.Name != "unit tests" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed go-test check", check)
	}
	if len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypeSandboxExec ||
		check.Evidence[0].Ref != "go test ./..." {
		t.Fatalf("evidence = %+v, want sandbox exec evidence", check.Evidence)
	}
	if check.Metadata["session_id"] != "sandbox-1" ||
		check.Metadata["exit_code"] != 0 ||
		check.Metadata["stdout"] != "ok\n" ||
		check.Metadata["duration_ms"] != int64(1500) ||
		check.Metadata["gate"] != "pre-release" {
		t.Fatalf("metadata = %+v, want sandbox exec metadata", check.Metadata)
	}
	command, ok := check.Metadata["command"].([]string)
	if !ok || !reflect.DeepEqual(command, []string{"go", "test", "./..."}) {
		t.Fatalf("metadata command = %#v, want copied command args", check.Metadata["command"])
	}
	requestMetadata, ok := check.Metadata["request_metadata"].(map[string]any)
	if !ok || requestMetadata["purpose"] != "verification" {
		t.Fatalf("request metadata = %#v, want copied request metadata", check.Metadata["request_metadata"])
	}
}

func TestRecordExecCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	err := RecordExecCheck(recorder, ExecCheck{
		SessionID: "sandbox-1",
		Request: ExecRequest{
			Command:  []string{"go", "test", "./..."},
			Metadata: map[string]any{"purpose": "verification"},
		},
		Result: ExecResult{
			ExitCode: 0,
			Stdout:   []byte("ok\n"),
			Usage:    ResourceUsage{Duration: 1500 * time.Millisecond},
		},
		Metadata: map[string]any{
			"command":          []string{"forged"},
			"exit_code":        99,
			"session_id":       "forged-session",
			"request_metadata": map[string]any{"purpose": "forged"},
			"stdout":           "forged stdout",
			"duration_ms":      int64(999),
			"gate":             "pre-release",
		},
	})
	if err != nil {
		t.Fatalf("RecordExecCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["exit_code"] != 0 ||
		metadata["session_id"] != "sandbox-1" ||
		metadata["stdout"] != "ok\n" ||
		metadata["duration_ms"] != int64(1500) {
		t.Fatalf("metadata = %+v, want canonical sandbox exec fields preserved", metadata)
	}
	command, ok := metadata["command"].([]string)
	if !ok || !reflect.DeepEqual(command, []string{"go", "test", "./..."}) {
		t.Fatalf("metadata command = %#v, want canonical command", metadata["command"])
	}
	requestMetadata, ok := metadata["request_metadata"].(map[string]any)
	if !ok || requestMetadata["purpose"] != "verification" {
		t.Fatalf("request metadata = %#v, want canonical request metadata", metadata["request_metadata"])
	}
	if metadata["gate"] != "pre-release" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
}

func TestRecordExecCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	execErr := errors.New("sandbox exec timeout")

	err := RecordExecCheck(recorder, ExecCheck{
		SessionID: "sandbox-1",
		Request:   ExecRequest{Command: []string{"go", "test", "./..."}},
		Result: ExecResult{
			ExitCode: 1,
			Stderr:   []byte("compile failed\n"),
		},
		Err: execErr,
	})
	if !errors.Is(err, ErrExecCheckFailed) || !errors.Is(err, execErr) {
		t.Fatalf("RecordExecCheck() error = %v, want ErrExecCheckFailed and execErr", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["stderr"] != "compile failed\n" ||
		check.Metadata["exit_code"] != 1 ||
		check.Metadata["error"] != execErr.Error() {
		t.Fatalf("metadata = %+v, want failure metadata", check.Metadata)
	}
}

func TestRecordExecCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordExecCheck(recorder, ExecCheck{
		ID:      "race",
		Request: ExecRequest{Command: []string{"go", "test", "-race", "./..."}},
		Skipped: true,
		Summary: "race gate not requested",
	})
	if err != nil {
		t.Fatalf("RecordExecCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped sandbox exec check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "go test -race ./..." {
		t.Fatalf("evidence = %+v, want skipped sandbox exec evidence", checks[0].Evidence)
	}
}

func TestRecordExecCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordExecCheck(nil, ExecCheck{Request: ExecRequest{Command: []string{"go", "test"}}}); err == nil {
		t.Fatal("RecordExecCheck(nil) error = nil, want error")
	}
	if err := RecordExecCheck(recorder, ExecCheck{}); !errors.Is(err, ErrExecCheckCommandRequired) {
		t.Fatalf("RecordExecCheck(empty command) error = %v, want ErrExecCheckCommandRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
