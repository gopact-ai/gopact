package react

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestMemoryDeferredMemoryWorkQueueTransitions(t *testing.T) {
	ctx := context.Background()
	queue := NewMemoryDeferredMemoryWorkQueue()
	job := DeferredMemoryWorkJob{
		ID:          "job-1",
		Export:      deferredMemoryWorkExport([]gopact.EffectRecord{pendingMemoryPutEffect("pending-1", "memory:pending-1", "remember queued")}),
		Attempt:     1,
		MaxAttempts: 3,
		Metadata:    map[string]any{"queue": "local"},
	}

	if err := queue.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	job.Metadata["queue"] = "mutated"

	dequeued, ok, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}
	if !ok || dequeued.ID != "job-1" || dequeued.Metadata["queue"] != "local" {
		t.Fatalf("Dequeue() = %+v/%v, want copied job-1", dequeued, ok)
	}
	if _, ok, err := queue.Dequeue(ctx); err != nil || ok {
		t.Fatalf("Dequeue(empty) = ok %v err %v, want empty queue", ok, err)
	}

	retryDecision := DeferredMemoryWorkScheduleDecision{
		Action:      DeferredMemoryWorkScheduleRetry,
		Attempt:     1,
		NextAttempt: 2,
		MaxAttempts: 3,
		Delay:       5 * time.Millisecond,
		Reason:      "temporary memory store outage",
		Metadata:    map[string]any{"scheduler": "local"},
	}
	if err := queue.Retry(ctx, dequeued, retryDecision); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	retried, ok, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue(retry) error = %v", err)
	}
	if !ok || retried.ID != "job-1" || retried.Attempt != 2 || retried.MaxAttempts != 3 {
		t.Fatalf("retried job = %+v/%v, want attempt 2", retried, ok)
	}
	if retried.Metadata["queue"] != "local" || retried.Metadata["scheduler"] != "local" {
		t.Fatalf("retried metadata = %+v, want original and scheduler metadata", retried.Metadata)
	}

	deadLetterDecision := DeferredMemoryWorkScheduleDecision{
		Action:  DeferredMemoryWorkScheduleDeadLetter,
		Attempt: 2,
		Reason:  "max attempts reached",
	}
	if err := queue.DeadLetter(ctx, retried, deadLetterDecision); err != nil {
		t.Fatalf("DeadLetter() error = %v", err)
	}

	snapshot := queue.Snapshot()
	if len(snapshot.Pending) != 0 || len(snapshot.DeadLettered) != 1 {
		t.Fatalf("snapshot = %+v, want one dead-lettered job and no pending", snapshot)
	}
	if snapshot.DeadLettered[0].Job.ID != "job-1" ||
		snapshot.DeadLettered[0].Decision.Action != DeferredMemoryWorkScheduleDeadLetter {
		t.Fatalf("dead-letter snapshot = %+v, want job-1 decision", snapshot.DeadLettered)
	}
	snapshot.DeadLettered[0].Job.Metadata["queue"] = "mutated"
	if got := queue.Snapshot().DeadLettered[0].Job.Metadata["queue"]; got != "local" {
		t.Fatalf("snapshot mutation leaked into queue: %v", got)
	}
}

func TestMemoryDeferredMemoryWorkQueueCompleteAndStop(t *testing.T) {
	ctx := context.Background()
	queue := NewMemoryDeferredMemoryWorkQueue()
	report := DeferredMemoryWorkReport{Status: DeferredMemoryWorkSucceeded}

	completedJob := DeferredMemoryWorkJob{ID: "completed", Export: deferredMemoryWorkExport(nil)}
	if err := queue.Enqueue(ctx, completedJob); err != nil {
		t.Fatalf("Enqueue(completed) error = %v", err)
	}
	dequeued, ok, err := queue.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("Dequeue(completed) = %+v/%v/%v, want job", dequeued, ok, err)
	}
	if err := queue.Complete(ctx, dequeued, report); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	stoppedJob := DeferredMemoryWorkJob{ID: "stopped", Export: deferredMemoryWorkExport(nil)}
	if err := queue.Enqueue(ctx, stoppedJob); err != nil {
		t.Fatalf("Enqueue(stopped) error = %v", err)
	}
	dequeued, ok, err = queue.Dequeue(ctx)
	if err != nil || !ok {
		t.Fatalf("Dequeue(stopped) = %+v/%v/%v, want job", dequeued, ok, err)
	}
	stopDecision := DeferredMemoryWorkScheduleDecision{
		Action:  DeferredMemoryWorkScheduleStop,
		Attempt: 1,
		Reason:  "host stopped scheduling",
	}
	if err := queue.Stop(ctx, dequeued, DeferredMemoryWorkReport{Status: DeferredMemoryWorkSkipped}, stopDecision); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	snapshot := queue.Snapshot()
	if len(snapshot.Completed) != 1 || len(snapshot.Stopped) != 1 {
		t.Fatalf("snapshot = %+v, want completed and stopped jobs", snapshot)
	}
	if snapshot.Completed[0].Job.ID != "completed" || snapshot.Completed[0].Report.Status != DeferredMemoryWorkSucceeded {
		t.Fatalf("completed snapshot = %+v, want completed report", snapshot.Completed)
	}
	if snapshot.Stopped[0].Job.ID != "stopped" || snapshot.Stopped[0].Decision.Action != DeferredMemoryWorkScheduleStop {
		t.Fatalf("stopped snapshot = %+v, want stopped decision", snapshot.Stopped)
	}
}

func TestMemoryDeferredMemoryWorkQueueHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queue := NewMemoryDeferredMemoryWorkQueue()

	if err := queue.Enqueue(ctx, DeferredMemoryWorkJob{ID: "job-1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enqueue(canceled) error = %v, want context.Canceled", err)
	}
	if _, _, err := queue.Dequeue(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dequeue(canceled) error = %v, want context.Canceled", err)
	}
}
