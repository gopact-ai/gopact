package gopact

import (
	"errors"
	"testing"
	"time"
)

func TestRecordRunExportCheckRecordsCompletedExportAsPassedCheck(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 11, 15, 0, 0, time.UTC)
	ids := RuntimeIDs{
		RunID:    "run-1",
		ThreadID: "thread-1",
		UserID:   "user-1",
	}
	export := RunExport{
		Version:   RunExportVersion,
		IDs:       ids,
		Outcome:   RunCompleted,
		CreatedAt: createdAt,
		Events: []Event{
			{Type: EventRunStarted, IDs: ids, CreatedAt: createdAt},
		},
		Steps: []StepSnapshot{
			{ID: "step-1", Step: 0, Node: "call_model", Phase: StepCompleted, IDs: ids},
		},
		Metadata: map[string]any{"source": "react"},
	}

	if err := RecordRunExportCheck(recorder, export); err != nil {
		t.Fatalf("RecordRunExportCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "run-export:run-1" || check.Name != "run export" || check.Status != VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed run export check", check)
	}
	if len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypeRunExport ||
		check.Evidence[0].Ref != "run-1" {
		t.Fatalf("evidence = %+v, want run export evidence", check.Evidence)
	}
	if check.Metadata["outcome"] != string(RunCompleted) ||
		check.Metadata["event_count"] != 1 ||
		check.Metadata["step_count"] != 1 ||
		check.Metadata["run_id"] != "run-1" ||
		check.Metadata["thread_id"] != "thread-1" ||
		check.Metadata["user_id"] != "user-1" ||
		check.Metadata["source"] != "react" {
		t.Fatalf("metadata = %+v, want run export metadata", check.Metadata)
	}
}

func TestRecordRunExportCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 11, 15, 0, 0, time.UTC)
	ids := RuntimeIDs{
		RunID:    "run-1",
		ThreadID: "thread-1",
		UserID:   "user-1",
	}
	export := RunExport{
		Version:   RunExportVersion,
		IDs:       ids,
		Outcome:   RunCompleted,
		CreatedAt: createdAt,
		Events: []Event{
			{Type: EventRunStarted, IDs: ids, CreatedAt: createdAt},
		},
		Steps: []StepSnapshot{
			{ID: "step-1", Step: 0, Node: "call_model", Phase: StepCompleted, IDs: ids},
		},
		Metadata: map[string]any{
			"ref":                "forged-ref",
			"outcome":            string(RunFailed),
			"event_count":        999,
			"step_count":         999,
			"run_id":             "forged-run",
			"thread_id":          "forged-thread",
			"user_id":            "forged-user",
			"run_export_version": 999,
			"source":             "react",
		},
	}

	if err := RecordRunExportCheck(recorder, export); err != nil {
		t.Fatalf("RecordRunExportCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	if check.Metadata["ref"] != "run-1" ||
		check.Metadata["outcome"] != string(RunCompleted) ||
		check.Metadata["event_count"] != 1 ||
		check.Metadata["step_count"] != 1 ||
		check.Metadata["run_id"] != "run-1" ||
		check.Metadata["thread_id"] != "thread-1" ||
		check.Metadata["user_id"] != "user-1" ||
		check.Metadata["run_export_version"] != RunExportVersion {
		t.Fatalf("metadata = %+v, want canonical run export fields preserved", check.Metadata)
	}
	if check.Metadata["source"] != "react" {
		t.Fatalf("metadata = %+v, want non-conflicting caller metadata preserved", check.Metadata)
	}
}

func TestRecordRunExportCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordRunExportCheck(recorder, RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunFailed,
	})
	if !errors.Is(err, ErrRunExportFailed) {
		t.Fatalf("RecordRunExportCheck() error = %v, want ErrRunExportFailed", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed run export check", checks)
	}
	if checks[0].Metadata["outcome"] != string(RunFailed) {
		t.Fatalf("metadata = %+v, want failed outcome", checks[0].Metadata)
	}
}

func TestRecordRunExportCheckRejectsCompletedExportWithoutProcess(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordRunExportCheck(recorder, RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunCompleted,
	})
	if !errors.Is(err, ErrRunExportIncomplete) {
		t.Fatalf("RecordRunExportCheck() error = %v, want ErrRunExportIncomplete", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed run export check", checks)
	}
	if checks[0].Metadata["event_count"] != 0 || checks[0].Metadata["step_count"] != 0 {
		t.Fatalf("metadata = %+v, want zero event/step counts", checks[0].Metadata)
	}
}

func TestRecordRunExportCheckRecordsInterruptedExportAsSkippedCheck(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordRunExportCheck(recorder, RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunInterrupted,
	}); err != nil {
		t.Fatalf("RecordRunExportCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped run export check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Type != VerificationEvidenceTypeRunExport {
		t.Fatalf("evidence = %+v, want run export evidence for skipped check", checks[0].Evidence)
	}
}

func TestRecordRunExportCheckRejectsInvalidInput(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordRunExportCheck(nil, RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunCompleted,
	}); err == nil {
		t.Fatal("RecordRunExportCheck(nil) error = nil, want error")
	}
	if err := RecordRunExportCheck(recorder, RunExport{
		Version: RunExportVersion,
		Outcome: RunCompleted,
	}); err == nil {
		t.Fatal("RecordRunExportCheck(missing run id) error = nil, want validation error")
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("checks = %+v, want none after invalid export", recorder.Checks())
	}
}
