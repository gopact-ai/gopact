package react

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordDeferredMemoryWorkScheduleCheckRecordsRetryDecision(t *testing.T) {
	report := failedDeferredMemoryWorkReport(t)

	recorder := gopact.NewVerificationRecorder()
	err := RecordDeferredMemoryWorkScheduleCheck(recorder, DeferredMemoryWorkScheduleDecision{
		Report:      report,
		Action:      DeferredMemoryWorkScheduleRetry,
		Attempt:     2,
		NextAttempt: 3,
		MaxAttempts: 5,
		Delay:       250 * time.Millisecond,
		Reason:      "temporary memory store outage",
		Metadata: map[string]any{
			"queue":               "memory-default",
			"action":              string(DeferredMemoryWorkScheduleDeadLetter),
			"attempt":             99,
			"next_attempt":        99,
			"max_attempts":        99,
			"delay_ms":            99,
			"worker_status":       "forged",
			"report_replay_count": 99,
			"report_result_count": 99,
			"run_id":              "forged-run",
			"thread_id":           "forged-thread",
			"error":               "forged-error",
		},
	})
	if err != nil {
		t.Fatalf("RecordDeferredMemoryWorkScheduleCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("recorded checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != VerificationCheckDeferredMemoryWorkSchedule+":run-1:attempt-2" {
		t.Fatalf("check ID = %q, want schedule decision check", check.ID)
	}
	if check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check status = %q, want passed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeDeferredMemoryWorkSchedule {
		t.Fatalf("check evidence = %+v, want memory work schedule evidence", check.Evidence)
	}
	metadata := check.Metadata
	if metadata["action"] != string(DeferredMemoryWorkScheduleRetry) ||
		metadata["attempt"] != 2 ||
		metadata["next_attempt"] != 3 ||
		metadata["max_attempts"] != 5 ||
		metadata["delay_ms"] != int64(250) ||
		metadata["worker_status"] != string(DeferredMemoryWorkFailed) ||
		metadata["report_replay_count"] != 2 ||
		metadata["report_result_count"] != 1 ||
		metadata["run_id"] != "run-1" ||
		metadata["thread_id"] != "thread-1" ||
		metadata["error"] != report.Error {
		t.Fatalf("metadata = %+v, want canonical schedule fields preserved", metadata)
	}
	if metadata["queue"] != "memory-default" {
		t.Fatalf("metadata = %+v, want supplemental queue metadata", metadata)
	}
}

func TestRecordDeferredMemoryWorkScheduleCheckRecordsDeadLetterAsFailure(t *testing.T) {
	report := failedDeferredMemoryWorkReport(t)

	recorder := gopact.NewVerificationRecorder()
	err := RecordDeferredMemoryWorkScheduleCheck(recorder, DeferredMemoryWorkScheduleDecision{
		Report:      report,
		Action:      DeferredMemoryWorkScheduleDeadLetter,
		Attempt:     3,
		MaxAttempts: 3,
		Reason:      "max attempts reached",
		Metadata:    map[string]any{"queue": "memory-dead-letter"},
	})
	if !errors.Is(err, ErrDeferredMemoryWorkDeadLettered) {
		t.Fatalf("RecordDeferredMemoryWorkScheduleCheck() error = %v, want ErrDeferredMemoryWorkDeadLettered", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("recorded checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["action"] != string(DeferredMemoryWorkScheduleDeadLetter) ||
		check.Metadata["queue"] != "memory-dead-letter" {
		t.Fatalf("check metadata = %+v, want dead-letter action and queue metadata", check.Metadata)
	}
}

func TestRecordDeferredMemoryWorkScheduleCheckRecordsStopAfterFailureAsGenericFailure(t *testing.T) {
	report := failedDeferredMemoryWorkReport(t)

	recorder := gopact.NewVerificationRecorder()
	err := RecordDeferredMemoryWorkScheduleCheck(recorder, DeferredMemoryWorkScheduleDecision{
		Report:  report,
		Action:  DeferredMemoryWorkScheduleStop,
		Attempt: 2,
		Reason:  "operator stopped retries",
	})
	if !errors.Is(err, ErrDeferredMemoryWorkScheduleFailed) {
		t.Fatalf("RecordDeferredMemoryWorkScheduleCheck() error = %v, want ErrDeferredMemoryWorkScheduleFailed", err)
	}
	if errors.Is(err, ErrDeferredMemoryWorkDeadLettered) {
		t.Fatalf("RecordDeferredMemoryWorkScheduleCheck() error = %v, should not report dead-letter for stop", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed stop check", checks)
	}
}

func TestRecordDeferredMemoryWorkScheduleCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	report := failedDeferredMemoryWorkReport(t)

	if err := RecordDeferredMemoryWorkScheduleCheck(nil, DeferredMemoryWorkScheduleDecision{
		Report:  report,
		Action:  DeferredMemoryWorkScheduleRetry,
		Attempt: 1,
	}); err == nil {
		t.Fatal("RecordDeferredMemoryWorkScheduleCheck(nil) error = nil, want error")
	}
	if err := RecordDeferredMemoryWorkScheduleCheck(recorder, DeferredMemoryWorkScheduleDecision{
		Report:  report,
		Attempt: 1,
	}); !errors.Is(err, ErrDeferredMemoryWorkScheduleDecisionRequired) {
		t.Fatalf("RecordDeferredMemoryWorkScheduleCheck(missing action) error = %v, want ErrDeferredMemoryWorkScheduleDecisionRequired", err)
	}
	if err := RecordDeferredMemoryWorkScheduleCheck(recorder, DeferredMemoryWorkScheduleDecision{
		Report: report,
		Action: DeferredMemoryWorkScheduleRetry,
	}); !errors.Is(err, ErrDeferredMemoryWorkScheduleAttemptRequired) {
		t.Fatalf("RecordDeferredMemoryWorkScheduleCheck(missing attempt) error = %v, want ErrDeferredMemoryWorkScheduleAttemptRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("checks = %+v, want no checks for invalid input", recorder.Checks())
	}
}

func failedDeferredMemoryWorkReport(t *testing.T) DeferredMemoryWorkReport {
	t.Helper()

	replayErr := errors.New("worker executor failed")
	export := deferredMemoryWorkExport([]gopact.EffectRecord{
		pendingMemoryPutEffect("pending-1", "memory:pending-1", "first memory"),
		pendingMemoryPutEffect("pending-2", "memory:pending-2", "second memory"),
	})
	executor := gopact.EffectReplayFunc(func(_ context.Context, decision gopact.EffectReplayDecision) (gopact.EffectReplayResult, error) {
		if decision.Effect.ID == "pending-2" {
			return gopact.EffectReplayResult{}, replayErr
		}
		return gopact.EffectReplayResult{
			EffectID:     decision.Effect.ID,
			Action:       gopact.EffectReplayActionReplay,
			ReplayPolicy: decision.Effect.ReplayPolicy,
			Effect:       decision.Effect,
		}, nil
	})
	report, err := RunDeferredMemoryWork(context.Background(), export, executor)
	if !errors.Is(err, replayErr) {
		t.Fatalf("RunDeferredMemoryWork() error = %v, want replayErr", err)
	}
	return report
}
