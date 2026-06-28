package gopacttest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordCommandCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCommandCheck(recorder, CommandResult{
		ID:       "go-test",
		Name:     "unit tests",
		Command:  []string{"go", "test", "./..."},
		Dir:      "/repo",
		ExitCode: 0,
		Stdout:   "ok",
	}); err != nil {
		t.Fatalf("RecordCommandCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "go-test" || check.Name != "unit tests" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed go-test check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeCommand || check.Evidence[0].Ref != "go test ./..." {
		t.Fatalf("evidence = %+v, want command evidence", check.Evidence)
	}
	if check.Metadata["exit_code"] != 0 || check.Metadata["dir"] != "/repo" || check.Metadata["stdout"] != "ok" {
		t.Fatalf("metadata = %+v, want command metadata", check.Metadata)
	}
	command, ok := check.Metadata["command"].([]string)
	if !ok || !reflect.DeepEqual(command, []string{"go", "test", "./..."}) {
		t.Fatalf("metadata command = %#v, want copied command args", check.Metadata["command"])
	}
}

func TestRecordCommandCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	err := RecordCommandCheck(recorder, CommandResult{
		Command:  []string{"go", "test", "./..."},
		Dir:      "/repo",
		ExitCode: 0,
		Stdout:   "ok",
		Duration: 1500 * time.Millisecond,
		Metadata: map[string]any{
			"command":       []string{"forged"},
			"dir":           "/forged",
			"exit_code":     99,
			"stdout":        "forged stdout",
			"duration_ms":   int64(999),
			"metadata_keys": []string{"forged"},
			"gate":          "unit",
		},
	})
	if err != nil {
		t.Fatalf("RecordCommandCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	metadata := check.Metadata
	if metadata["dir"] != "/repo" ||
		metadata["exit_code"] != 0 ||
		metadata["stdout"] != "ok" ||
		metadata["duration_ms"] != int64(1500) {
		t.Fatalf("metadata = %+v, want canonical command fields preserved", metadata)
	}
	command, ok := metadata["command"].([]string)
	if !ok || !reflect.DeepEqual(command, []string{"go", "test", "./..."}) {
		t.Fatalf("metadata command = %#v, want canonical command", metadata["command"])
	}
	if metadata["gate"] != "unit" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
	if got, want := metadata["metadata_keys"], []string{"gate"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata metadata_keys = %#v, want %#v", got, want)
	}

	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["dir"] != "/repo" ||
		evidenceMetadata["exit_code"] != 0 ||
		evidenceMetadata["stdout"] != "ok" ||
		evidenceMetadata["duration_ms"] != int64(1500) {
		t.Fatalf("evidence metadata = %+v, want canonical command fields preserved", evidenceMetadata)
	}
	evidenceCommand, ok := evidenceMetadata["command"].([]string)
	if !ok || !reflect.DeepEqual(evidenceCommand, []string{"go", "test", "./..."}) {
		t.Fatalf("evidence metadata command = %#v, want canonical command", evidenceMetadata["command"])
	}
	if evidenceMetadata["gate"] != "unit" {
		t.Fatalf("evidence metadata = %+v, want supplemental metadata preserved", evidenceMetadata)
	}
	if got, want := evidenceMetadata["metadata_keys"], []string{"gate"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence metadata metadata_keys = %#v, want %#v", got, want)
	}
}

func TestRecordCommandCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordCommandCheck(recorder, CommandResult{
		ID:       "go-test",
		Command:  []string{"go", "test", "./..."},
		ExitCode: 1,
		Stderr:   "compile failed",
	})
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("RecordCommandCheck() error = %v, want ErrCommandFailed", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Ref != "go test ./..." {
		t.Fatalf("evidence = %+v, want command evidence", check.Evidence)
	}
	if check.Metadata["stderr"] != "compile failed" || check.Metadata["exit_code"] != 1 {
		t.Fatalf("metadata = %+v, want failure metadata", check.Metadata)
	}
}

func TestRecordCommandCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCommandCheck(recorder, CommandResult{
		ID:      "race",
		Command: []string{"go", "test", "-race", "./..."},
		Skipped: true,
		Summary: "race gate not requested",
	}); err != nil {
		t.Fatalf("RecordCommandCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped command check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "go test -race ./..." {
		t.Fatalf("evidence = %+v, want skipped command evidence", checks[0].Evidence)
	}
}

func TestRecordCommandCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordCommandCheck(nil, CommandResult{ID: "go-test", Command: []string{"go", "test"}}); err == nil {
		t.Fatal("RecordCommandCheck(nil) error = nil, want error")
	}
	if err := RecordCommandCheck(recorder, CommandResult{ID: "go-test"}); err == nil {
		t.Fatal("RecordCommandCheck(empty command) error = nil, want error")
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
