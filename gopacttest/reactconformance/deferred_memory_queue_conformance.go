// Package reactconformance provides reusable ReAct template contract tests.
package reactconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/react"
)

// ErrDeferredMemoryWorkQueueConformanceFailed is returned when a deferred memory queue violates the conformance harness.
var ErrDeferredMemoryWorkQueueConformanceFailed = errors.New("gopacttest: deferred memory work queue conformance failed")

// DeferredMemoryWorkQueueConformanceHarness describes one DeferredMemoryWorkQueue implementation under test.
type DeferredMemoryWorkQueueConformanceHarness struct {
	NewQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error)
	Jobs     []react.DeferredMemoryWorkJob
}

// DeferredMemoryWorkQueueVisibilityConformanceHarness describes a queue with delivery receipt and visibility semantics.
type DeferredMemoryWorkQueueVisibilityConformanceHarness struct {
	NewQueue                 func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error)
	Jobs                     []react.DeferredMemoryWorkJob
	AdvanceVisibilityTimeout func(context.Context) error
}

// DeferredMemoryWorkQueueConformanceResult is the observed result for one deferred memory queue contract case.
type DeferredMemoryWorkQueueConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckDeferredMemoryWorkQueueConformance runs reusable DeferredMemoryWorkQueue contract cases.
func CheckDeferredMemoryWorkQueueConformance(ctx context.Context, harness DeferredMemoryWorkQueueConformanceHarness) []DeferredMemoryWorkQueueConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	jobs := normalizeDeferredMemoryWorkQueueConformanceJobs(harness.Jobs)

	return []DeferredMemoryWorkQueueConformanceResult{
		checkDeferredMemoryWorkQueueFactory(harness.NewQueue),
		checkDeferredMemoryWorkQueueDequeueEmpty(ctx, harness.NewQueue),
		checkDeferredMemoryWorkQueueDequeueCanceledContext(harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJobs(jobs)),
		checkDeferredMemoryWorkQueueCompleteCanceledContext(harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueRetryCanceledContext(harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueDeadLetterCanceledContext(harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueStopCanceledContext(harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueDequeuesJob(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueCompleteRemovesJob(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueRetryRequeuesJob(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueRetryPreservesJobMetadata(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueDeadLetterRemovesJob(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueStopRemovesJob(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueCompleteDoesNotMutateInput(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueRetryDoesNotMutateInput(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueDeadLetterDoesNotMutateInput(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueStopDoesNotMutateInput(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueConcurrentDequeueDeliversEachJobOnce(ctx, harness.NewQueue, deferredMemoryWorkQueueConformanceConcurrentJobs(jobs)),
	}
}

// CheckDeferredMemoryWorkQueueVisibilityConformance runs reusable visibility timeout contract cases.
//
// The harness factory must return a queue configured with visibility semantics.
// AdvanceVisibilityTimeout must move that queue past its visibility timeout,
// either by advancing a test clock or by waiting for the adapter's configured
// timeout. The helper does not sleep on behalf of adapters.
func CheckDeferredMemoryWorkQueueVisibilityConformance(ctx context.Context, harness DeferredMemoryWorkQueueVisibilityConformanceHarness) []DeferredMemoryWorkQueueConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	jobs := normalizeDeferredMemoryWorkQueueConformanceJobs(harness.Jobs)

	return []DeferredMemoryWorkQueueConformanceResult{
		checkDeferredMemoryWorkQueueFactory(harness.NewQueue),
		checkDeferredMemoryWorkQueueVisibilityAdvance(harness.AdvanceVisibilityTimeout),
		checkDeferredMemoryWorkQueueVisibilityDequeueSetsDeliveryReceipt(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueVisibilityHidesInFlightBeforeTimeout(ctx, harness.NewQueue, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueVisibilityRedeliversAfterTimeoutAndRejectsStale(ctx, harness.NewQueue, harness.AdvanceVisibilityTimeout, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
		checkDeferredMemoryWorkQueueVisibilityRejectsExpiredTransitionBeforeRedelivery(ctx, harness.NewQueue, harness.AdvanceVisibilityTimeout, copyDeferredMemoryWorkQueueConformanceJob(jobs[0])),
	}
}

// RequireDeferredMemoryWorkQueueVisibilityConformance fails the test unless queue satisfies visibility semantics.
func RequireDeferredMemoryWorkQueueVisibilityConformance(t testing.TB, harness DeferredMemoryWorkQueueVisibilityConformanceHarness) {
	t.Helper()

	for _, result := range CheckDeferredMemoryWorkQueueVisibilityConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("deferred memory work queue visibility conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

// RequireDeferredMemoryWorkQueueConformance fails the test unless queue satisfies the DeferredMemoryWorkQueue contract.
func RequireDeferredMemoryWorkQueueConformance(t testing.TB, harness DeferredMemoryWorkQueueConformanceHarness) {
	t.Helper()

	for _, result := range CheckDeferredMemoryWorkQueueConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("deferred memory work queue conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkDeferredMemoryWorkQueueFactory(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error)) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, nil)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("has-queue-factory", err)
	}
	if queue == nil {
		return failedDeferredMemoryWorkQueueConformance("has-queue-factory", errors.New("deferred memory work queue is nil"))
	}
	return passedDeferredMemoryWorkQueueConformance("has-queue-factory")
}

func checkDeferredMemoryWorkQueueDequeueEmpty(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error)) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, nil)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dequeue-empty", err)
	}
	_, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dequeue-empty", err)
	}
	if ok {
		return failedDeferredMemoryWorkQueueConformance("dequeue-empty", errors.New("Dequeue on empty queue returned ok=true"))
	}
	return passedDeferredMemoryWorkQueueConformance("dequeue-empty")
}

func checkDeferredMemoryWorkQueueVisibilityAdvance(advance func(context.Context) error) DeferredMemoryWorkQueueConformanceResult {
	if advance == nil {
		return failedDeferredMemoryWorkQueueConformance("has-visibility-timeout-advance", errors.New("visibility timeout advance function is nil"))
	}
	return passedDeferredMemoryWorkQueueConformance("has-visibility-timeout-advance")
}

func checkDeferredMemoryWorkQueueDequeueCanceledContext(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), jobs []react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, jobs)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dequeue-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := queue.Dequeue(ctx); !errors.Is(err, context.Canceled) {
		return failedDeferredMemoryWorkQueueConformance("dequeue-respects-canceled-context", fmt.Errorf("Dequeue canceled context error = %v, want context.Canceled", err))
	}
	return passedDeferredMemoryWorkQueueConformance("dequeue-respects-canceled-context")
}

func checkDeferredMemoryWorkQueueCompleteCanceledContext(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("complete-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.Complete(ctx, job, defaultDeferredMemoryWorkQueueConformanceReport()); !errors.Is(err, context.Canceled) {
		return failedDeferredMemoryWorkQueueConformance("complete-respects-canceled-context", fmt.Errorf("Complete canceled context error = %v, want context.Canceled", err))
	}
	return passedDeferredMemoryWorkQueueConformance("complete-respects-canceled-context")
}

func checkDeferredMemoryWorkQueueRetryCanceledContext(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.Retry(ctx, job, defaultDeferredMemoryWorkQueueConformanceRetryDecision(job)); !errors.Is(err, context.Canceled) {
		return failedDeferredMemoryWorkQueueConformance("retry-respects-canceled-context", fmt.Errorf("Retry canceled context error = %v, want context.Canceled", err))
	}
	return passedDeferredMemoryWorkQueueConformance("retry-respects-canceled-context")
}

func checkDeferredMemoryWorkQueueDeadLetterCanceledContext(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.DeadLetter(ctx, job, defaultDeferredMemoryWorkQueueConformanceDeadLetterDecision(job)); !errors.Is(err, context.Canceled) {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-respects-canceled-context", fmt.Errorf("DeadLetter canceled context error = %v, want context.Canceled", err))
	}
	return passedDeferredMemoryWorkQueueConformance("dead-letter-respects-canceled-context")
}

func checkDeferredMemoryWorkQueueStopCanceledContext(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("stop-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.Stop(ctx, job, defaultDeferredMemoryWorkQueueConformanceReport(), defaultDeferredMemoryWorkQueueConformanceStopDecision(job)); !errors.Is(err, context.Canceled) {
		return failedDeferredMemoryWorkQueueConformance("stop-respects-canceled-context", fmt.Errorf("Stop canceled context error = %v, want context.Canceled", err))
	}
	return passedDeferredMemoryWorkQueueConformance("stop-respects-canceled-context")
}

func checkDeferredMemoryWorkQueueDequeuesJob(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dequeues-job", err)
	}
	got, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dequeues-job", err)
	}
	if !ok {
		return failedDeferredMemoryWorkQueueConformance("dequeues-job", errors.New("Dequeue returned ok=false for seeded job"))
	}
	if got.ID != job.ID || got.Attempt != job.Attempt || got.MaxAttempts != job.MaxAttempts {
		return failedDeferredMemoryWorkQueueConformance("dequeues-job", fmt.Errorf("Dequeue job = %s/%d/%d, want %s/%d/%d", got.ID, got.Attempt, got.MaxAttempts, job.ID, job.Attempt, job.MaxAttempts))
	}
	return passedDeferredMemoryWorkQueueConformance("dequeues-job")
}

func checkDeferredMemoryWorkQueueCompleteRemovesJob(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, got, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("complete-removes-job", err)
	}
	if err := queue.Complete(ctx, got, defaultDeferredMemoryWorkQueueConformanceReport()); err != nil {
		return failedDeferredMemoryWorkQueueConformance("complete-removes-job", err)
	}
	if _, ok, err := queue.Dequeue(ctx); err != nil || ok {
		return failedDeferredMemoryWorkQueueConformance("complete-removes-job", fmt.Errorf("Dequeue after Complete ok=%v err=%v, want false nil", ok, err))
	}
	return passedDeferredMemoryWorkQueueConformance("complete-removes-job")
}

func checkDeferredMemoryWorkQueueRetryRequeuesJob(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, got, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-requeues-job", err)
	}
	decision := defaultDeferredMemoryWorkQueueConformanceRetryDecision(got)
	if err := queue.Retry(ctx, got, decision); err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-requeues-job", err)
	}
	again, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-requeues-job", err)
	}
	if !ok {
		return failedDeferredMemoryWorkQueueConformance("retry-requeues-job", errors.New("Dequeue after Retry returned ok=false"))
	}
	if again.ID != got.ID || again.Attempt != decision.NextAttempt {
		return failedDeferredMemoryWorkQueueConformance("retry-requeues-job", fmt.Errorf("retry job = %s/attempt-%d, want %s/attempt-%d", again.ID, again.Attempt, got.ID, decision.NextAttempt))
	}
	return passedDeferredMemoryWorkQueueConformance("retry-requeues-job")
}

func checkDeferredMemoryWorkQueueRetryPreservesJobMetadata(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	job = copyDeferredMemoryWorkQueueConformanceJob(job)
	if job.Metadata == nil {
		job.Metadata = map[string]any{}
	}
	job.Metadata["gopact_conformance_retry_host_metadata"] = "must-survive-retry"

	queue, got, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata", err)
	}
	decision := defaultDeferredMemoryWorkQueueConformanceRetryDecision(got)
	if decision.Metadata == nil {
		decision.Metadata = map[string]any{}
	}
	decision.Metadata["gopact_conformance_retry_decision_metadata"] = "must-merge-into-retry"
	wantJobMetadata := copyDeferredMemoryWorkQueueConformanceAnyMap(got.Metadata)
	wantDecisionMetadata := copyDeferredMemoryWorkQueueConformanceAnyMap(decision.Metadata)

	if err := queue.Retry(ctx, got, decision); err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata", err)
	}
	again, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata", err)
	}
	if !ok {
		return failedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata", errors.New("Dequeue after Retry returned ok=false"))
	}
	for key, want := range wantJobMetadata {
		if got := again.Metadata[key]; !reflect.DeepEqual(got, want) {
			return failedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata", fmt.Errorf("retry metadata[%q] = %#v, want preserved %#v", key, got, want))
		}
	}
	for key, want := range wantDecisionMetadata {
		if got := again.Metadata[key]; !reflect.DeepEqual(got, want) {
			return failedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata", fmt.Errorf("retry decision metadata[%q] = %#v, want merged %#v", key, got, want))
		}
	}
	return passedDeferredMemoryWorkQueueConformance("retry-preserves-job-metadata")
}

func checkDeferredMemoryWorkQueueDeadLetterRemovesJob(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, got, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-removes-job", err)
	}
	if err := queue.DeadLetter(ctx, got, defaultDeferredMemoryWorkQueueConformanceDeadLetterDecision(got)); err != nil {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-removes-job", err)
	}
	if _, ok, err := queue.Dequeue(ctx); err != nil || ok {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-removes-job", fmt.Errorf("Dequeue after DeadLetter ok=%v err=%v, want false nil", ok, err))
	}
	return passedDeferredMemoryWorkQueueConformance("dead-letter-removes-job")
}

func checkDeferredMemoryWorkQueueStopRemovesJob(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, got, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("stop-removes-job", err)
	}
	if err := queue.Stop(ctx, got, defaultDeferredMemoryWorkQueueConformanceReport(), defaultDeferredMemoryWorkQueueConformanceStopDecision(got)); err != nil {
		return failedDeferredMemoryWorkQueueConformance("stop-removes-job", err)
	}
	if _, ok, err := queue.Dequeue(ctx); err != nil || ok {
		return failedDeferredMemoryWorkQueueConformance("stop-removes-job", fmt.Errorf("Dequeue after Stop ok=%v err=%v, want false nil", ok, err))
	}
	return passedDeferredMemoryWorkQueueConformance("stop-removes-job")
}

func checkDeferredMemoryWorkQueueCompleteDoesNotMutateInput(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("complete-does-not-mutate-input", err)
	}
	report := defaultDeferredMemoryWorkQueueConformanceReport()
	beforeJob := copyDeferredMemoryWorkQueueConformanceJob(job)
	beforeReport := copyDeferredMemoryWorkQueueConformanceReport(report)
	if err := queue.Complete(ctx, job, report); err != nil {
		return failedDeferredMemoryWorkQueueConformance("complete-does-not-mutate-input", err)
	}
	if !reflect.DeepEqual(job, beforeJob) {
		return failedDeferredMemoryWorkQueueConformance("complete-does-not-mutate-input", errors.New("Complete mutated input job"))
	}
	if !reflect.DeepEqual(report, beforeReport) {
		return failedDeferredMemoryWorkQueueConformance("complete-does-not-mutate-input", errors.New("Complete mutated input report"))
	}
	return passedDeferredMemoryWorkQueueConformance("complete-does-not-mutate-input")
}

func checkDeferredMemoryWorkQueueRetryDoesNotMutateInput(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-does-not-mutate-input", err)
	}
	decision := defaultDeferredMemoryWorkQueueConformanceRetryDecision(job)
	beforeJob := copyDeferredMemoryWorkQueueConformanceJob(job)
	beforeDecision := copyDeferredMemoryWorkQueueConformanceScheduleDecision(decision)
	if err := queue.Retry(ctx, job, decision); err != nil {
		return failedDeferredMemoryWorkQueueConformance("retry-does-not-mutate-input", err)
	}
	if !reflect.DeepEqual(job, beforeJob) {
		return failedDeferredMemoryWorkQueueConformance("retry-does-not-mutate-input", errors.New("Retry mutated input job"))
	}
	if !reflect.DeepEqual(decision, beforeDecision) {
		return failedDeferredMemoryWorkQueueConformance("retry-does-not-mutate-input", errors.New("Retry mutated input decision"))
	}
	return passedDeferredMemoryWorkQueueConformance("retry-does-not-mutate-input")
}

func checkDeferredMemoryWorkQueueDeadLetterDoesNotMutateInput(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-does-not-mutate-input", err)
	}
	decision := defaultDeferredMemoryWorkQueueConformanceDeadLetterDecision(job)
	beforeJob := copyDeferredMemoryWorkQueueConformanceJob(job)
	beforeDecision := copyDeferredMemoryWorkQueueConformanceScheduleDecision(decision)
	if err := queue.DeadLetter(ctx, job, decision); err != nil {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-does-not-mutate-input", err)
	}
	if !reflect.DeepEqual(job, beforeJob) {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-does-not-mutate-input", errors.New("DeadLetter mutated input job"))
	}
	if !reflect.DeepEqual(decision, beforeDecision) {
		return failedDeferredMemoryWorkQueueConformance("dead-letter-does-not-mutate-input", errors.New("DeadLetter mutated input decision"))
	}
	return passedDeferredMemoryWorkQueueConformance("dead-letter-does-not-mutate-input")
}

func checkDeferredMemoryWorkQueueStopDoesNotMutateInput(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("stop-does-not-mutate-input", err)
	}
	report := defaultDeferredMemoryWorkQueueConformanceReport()
	decision := defaultDeferredMemoryWorkQueueConformanceStopDecision(job)
	beforeJob := copyDeferredMemoryWorkQueueConformanceJob(job)
	beforeReport := copyDeferredMemoryWorkQueueConformanceReport(report)
	beforeDecision := copyDeferredMemoryWorkQueueConformanceScheduleDecision(decision)
	if err := queue.Stop(ctx, job, report, decision); err != nil {
		return failedDeferredMemoryWorkQueueConformance("stop-does-not-mutate-input", err)
	}
	if !reflect.DeepEqual(job, beforeJob) {
		return failedDeferredMemoryWorkQueueConformance("stop-does-not-mutate-input", errors.New("Stop mutated input job"))
	}
	if !reflect.DeepEqual(report, beforeReport) {
		return failedDeferredMemoryWorkQueueConformance("stop-does-not-mutate-input", errors.New("Stop mutated input report"))
	}
	if !reflect.DeepEqual(decision, beforeDecision) {
		return failedDeferredMemoryWorkQueueConformance("stop-does-not-mutate-input", errors.New("Stop mutated input decision"))
	}
	return passedDeferredMemoryWorkQueueConformance("stop-does-not-mutate-input")
}

func checkDeferredMemoryWorkQueueConcurrentDequeueDeliversEachJobOnce(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), jobs []react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, jobs)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once", err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	start := make(chan struct{})
	results := make(chan deferredMemoryWorkQueueConcurrentDequeueResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			select {
			case <-ctx.Done():
				results <- deferredMemoryWorkQueueConcurrentDequeueResult{err: ctx.Err()}
				return
			case <-start:
			}
			job, ok, err := queue.Dequeue(ctx)
			results <- deferredMemoryWorkQueueConcurrentDequeueResult{job: job, ok: ok, err: err}
		}()
	}
	ready.Wait()
	close(start)

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.err != nil {
				return failedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once", result.err)
			}
			if !result.ok {
				return failedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once", errors.New("concurrent Dequeue returned ok=false for seeded job"))
			}
			if result.job.ID == "" {
				return failedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once", errors.New("concurrent Dequeue returned empty job ID"))
			}
			if seen[result.job.ID] {
				return failedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once", fmt.Errorf("concurrent Dequeue delivered duplicate job ID %q", result.job.ID))
			}
			seen[result.job.ID] = true
		case <-ctx.Done():
			return failedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once", ctx.Err())
		}
	}
	return passedDeferredMemoryWorkQueueConformance("concurrent-dequeue-delivers-each-job-once")
}

type deferredMemoryWorkQueueConcurrentDequeueResult struct {
	job react.DeferredMemoryWorkJob
	ok  bool
	err error
}

func checkDeferredMemoryWorkQueueVisibilityDequeueSetsDeliveryReceipt(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	job.DeliveryID = "stale-input-delivery"
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt", err)
	}
	got, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt", err)
	}
	if !ok {
		return failedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt", errors.New("Dequeue returned ok=false for seeded job"))
	}
	if got.DeliveryID == "" {
		return failedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt", errors.New("Dequeue did not set DeliveryID"))
	}
	if got.DeliveryID == job.DeliveryID {
		return failedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt", errors.New("Dequeue reused input DeliveryID"))
	}
	if got.Metadata["conformance"] != job.Metadata["conformance"] || len(got.Metadata) != len(job.Metadata) {
		return failedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt", fmt.Errorf("Dequeue metadata = %+v, want host metadata only", got.Metadata))
	}
	return passedDeferredMemoryWorkQueueConformance("visibility-dequeue-sets-delivery-receipt")
}

func checkDeferredMemoryWorkQueueVisibilityHidesInFlightBeforeTimeout(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	queue, _, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-hides-in-flight-before-timeout", err)
	}
	_, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-hides-in-flight-before-timeout", err)
	}
	if ok {
		return failedDeferredMemoryWorkQueueConformance("visibility-hides-in-flight-before-timeout", errors.New("Dequeue returned in-flight job before visibility timeout"))
	}
	return passedDeferredMemoryWorkQueueConformance("visibility-hides-in-flight-before-timeout")
}

func checkDeferredMemoryWorkQueueVisibilityRedeliversAfterTimeoutAndRejectsStale(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), advance func(context.Context) error, job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	if advance == nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", errors.New("visibility timeout advance function is nil"))
	}
	queue, first, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", err)
	}
	if err := advance(ctx); err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", err)
	}
	second, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", err)
	}
	if !ok {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", errors.New("Dequeue after visibility timeout returned ok=false"))
	}
	if second.ID != first.ID {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", fmt.Errorf("redelivery ID = %q, want %q", second.ID, first.ID))
	}
	if second.DeliveryID == "" || second.DeliveryID == first.DeliveryID {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", fmt.Errorf("redelivery DeliveryID = %q, first %q", second.DeliveryID, first.DeliveryID))
	}
	if err := queue.Complete(ctx, first, defaultDeferredMemoryWorkQueueConformanceReport()); !errors.Is(err, react.ErrDeferredMemoryWorkDeliveryNotFound) {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", fmt.Errorf("Complete stale delivery error = %v, want ErrDeferredMemoryWorkDeliveryNotFound", err))
	}
	if err := queue.Complete(ctx, second, defaultDeferredMemoryWorkQueueConformanceReport()); err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale", fmt.Errorf("Complete current delivery error = %w", err))
	}
	return passedDeferredMemoryWorkQueueConformance("visibility-redelivers-after-timeout-and-rejects-stale")
}

func checkDeferredMemoryWorkQueueVisibilityRejectsExpiredTransitionBeforeRedelivery(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), advance func(context.Context) error, job react.DeferredMemoryWorkJob) DeferredMemoryWorkQueueConformanceResult {
	if advance == nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery", errors.New("visibility timeout advance function is nil"))
	}
	queue, first, err := dequeueDeferredMemoryWorkQueueConformanceJob(ctx, newQueue, job)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery", err)
	}
	if err := advance(ctx); err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery", err)
	}
	if err := queue.Complete(ctx, first, defaultDeferredMemoryWorkQueueConformanceReport()); !errors.Is(err, react.ErrDeferredMemoryWorkDeliveryNotFound) {
		return failedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery", fmt.Errorf("Complete expired delivery error = %v, want ErrDeferredMemoryWorkDeliveryNotFound", err))
	}
	again, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return failedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery", err)
	}
	if !ok || again.ID != first.ID {
		return failedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery", fmt.Errorf("Dequeue after expired transition = %+v/%v, want redelivery", again, ok))
	}
	return passedDeferredMemoryWorkQueueConformance("visibility-rejects-expired-transition-before-redelivery")
}

func dequeueDeferredMemoryWorkQueueConformanceJob(ctx context.Context, newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), job react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, react.DeferredMemoryWorkJob, error) {
	queue, err := newDeferredMemoryWorkQueueConformanceQueue(newQueue, []react.DeferredMemoryWorkJob{job})
	if err != nil {
		return nil, react.DeferredMemoryWorkJob{}, err
	}
	got, ok, err := queue.Dequeue(ctx)
	if err != nil {
		return nil, react.DeferredMemoryWorkJob{}, err
	}
	if !ok {
		return nil, react.DeferredMemoryWorkJob{}, errors.New("Dequeue returned ok=false for seeded job")
	}
	return queue, got, nil
}

func newDeferredMemoryWorkQueueConformanceQueue(newQueue func([]react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error), jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
	if newQueue == nil {
		return nil, errors.New("deferred memory work queue factory is nil")
	}
	queue, err := newQueue(copyDeferredMemoryWorkQueueConformanceJobs(jobs))
	if err != nil {
		return nil, err
	}
	if queue == nil {
		return nil, errors.New("deferred memory work queue factory returned nil")
	}
	return queue, nil
}

func passedDeferredMemoryWorkQueueConformance(name string) DeferredMemoryWorkQueueConformanceResult {
	return DeferredMemoryWorkQueueConformanceResult{Case: name, Passed: true}
}

func failedDeferredMemoryWorkQueueConformance(name string, err error) DeferredMemoryWorkQueueConformanceResult {
	return DeferredMemoryWorkQueueConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrDeferredMemoryWorkQueueConformanceFailed, err),
	}
}

func normalizeDeferredMemoryWorkQueueConformanceJobs(in []react.DeferredMemoryWorkJob) []react.DeferredMemoryWorkJob {
	if len(in) > 0 {
		return copyDeferredMemoryWorkQueueConformanceJobs(in)
	}
	return []react.DeferredMemoryWorkJob{defaultDeferredMemoryWorkQueueConformanceJob()}
}

func deferredMemoryWorkQueueConformanceConcurrentJobs(in []react.DeferredMemoryWorkJob) []react.DeferredMemoryWorkJob {
	jobs := copyDeferredMemoryWorkQueueConformanceJobs(in)
	if len(jobs) == 0 {
		jobs = []react.DeferredMemoryWorkJob{defaultDeferredMemoryWorkQueueConformanceJob()}
	}
	if len(jobs) == 1 {
		second := copyDeferredMemoryWorkQueueConformanceJob(jobs[0])
		second.ID = jobs[0].ID + "-2"
		second.Attempt = jobs[0].Attempt
		second.MaxAttempts = jobs[0].MaxAttempts
		jobs = append(jobs, second)
	}
	if jobs[1].ID == jobs[0].ID {
		jobs[1].ID = jobs[0].ID + "-2"
	}
	return []react.DeferredMemoryWorkJob{
		copyDeferredMemoryWorkQueueConformanceJob(jobs[0]),
		copyDeferredMemoryWorkQueueConformanceJob(jobs[1]),
	}
}

func defaultDeferredMemoryWorkQueueConformanceJob() react.DeferredMemoryWorkJob {
	return react.DeferredMemoryWorkJob{
		ID:          "gopact-conformance-memory-work",
		Export:      defaultDeferredMemoryWorkQueueConformanceRunExport(),
		Attempt:     1,
		MaxAttempts: 3,
		Metadata:    map[string]any{"conformance": "deferred-memory-work-queue"},
	}
}

func defaultDeferredMemoryWorkQueueConformanceRunExport() gopact.RunExport {
	return gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread"},
		Outcome:   gopact.RunCompleted,
		CreatedAt: time.Unix(1, 0),
		Metadata:  map[string]any{"conformance": "deferred-memory-work-queue"},
	}
}

func defaultDeferredMemoryWorkQueueConformanceReport() react.DeferredMemoryWorkReport {
	return react.DeferredMemoryWorkReport{
		RunID:       "gopact-conformance-run",
		ThreadID:    "gopact-conformance-thread",
		Status:      react.DeferredMemoryWorkFailed,
		ReplayCount: 1,
		ResultCount: 0,
		Error:       "gopact conformance failure",
	}
}

func defaultDeferredMemoryWorkQueueConformanceRetryDecision(job react.DeferredMemoryWorkJob) react.DeferredMemoryWorkScheduleDecision {
	return react.DeferredMemoryWorkScheduleDecision{
		Report:      defaultDeferredMemoryWorkQueueConformanceReport(),
		Action:      react.DeferredMemoryWorkScheduleRetry,
		Attempt:     job.Attempt,
		NextAttempt: job.Attempt + 1,
		MaxAttempts: job.MaxAttempts,
		Reason:      "gopact conformance retry",
		Metadata:    map[string]any{"queue_decision": "retry"},
	}
}

func defaultDeferredMemoryWorkQueueConformanceDeadLetterDecision(job react.DeferredMemoryWorkJob) react.DeferredMemoryWorkScheduleDecision {
	return react.DeferredMemoryWorkScheduleDecision{
		Report:      defaultDeferredMemoryWorkQueueConformanceReport(),
		Action:      react.DeferredMemoryWorkScheduleDeadLetter,
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
		Reason:      "gopact conformance dead-letter",
		Metadata:    map[string]any{"queue_decision": "dead_letter"},
	}
}

func defaultDeferredMemoryWorkQueueConformanceStopDecision(job react.DeferredMemoryWorkJob) react.DeferredMemoryWorkScheduleDecision {
	return react.DeferredMemoryWorkScheduleDecision{
		Report:      defaultDeferredMemoryWorkQueueConformanceReport(),
		Action:      react.DeferredMemoryWorkScheduleStop,
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
		Reason:      "gopact conformance stop",
		Metadata:    map[string]any{"queue_decision": "stop"},
	}
}

func copyDeferredMemoryWorkQueueConformanceJobs(in []react.DeferredMemoryWorkJob) []react.DeferredMemoryWorkJob {
	if len(in) == 0 {
		return nil
	}
	out := make([]react.DeferredMemoryWorkJob, len(in))
	for i, job := range in {
		out[i] = copyDeferredMemoryWorkQueueConformanceJob(job)
	}
	return out
}

func copyDeferredMemoryWorkQueueConformanceJob(in react.DeferredMemoryWorkJob) react.DeferredMemoryWorkJob {
	out := in
	out.Export = copyDeferredMemoryWorkQueueConformanceRunExport(in.Export)
	out.Metadata = copyDeferredMemoryWorkQueueConformanceAnyMap(in.Metadata)
	return out
}

func copyDeferredMemoryWorkQueueConformanceReport(in react.DeferredMemoryWorkReport) react.DeferredMemoryWorkReport {
	out := in
	out.Results = append([]gopact.RunEffectReplayResult(nil), in.Results...)
	return out
}

func copyDeferredMemoryWorkQueueConformanceScheduleDecision(in react.DeferredMemoryWorkScheduleDecision) react.DeferredMemoryWorkScheduleDecision {
	out := in
	out.Report = copyDeferredMemoryWorkQueueConformanceReport(in.Report)
	out.Metadata = copyDeferredMemoryWorkQueueConformanceAnyMap(in.Metadata)
	return out
}

func copyDeferredMemoryWorkQueueConformanceRunExport(in gopact.RunExport) gopact.RunExport {
	out := in
	out.Events = append([]gopact.Event(nil), in.Events...)
	out.Steps = append([]gopact.StepSnapshot(nil), in.Steps...)
	out.Tasks = append([]gopact.TaskRecord(nil), in.Tasks...)
	out.Inputs = append([]gopact.InputRecord(nil), in.Inputs...)
	out.Interventions = append([]gopact.InterventionRecord(nil), in.Interventions...)
	out.Failures = append([]gopact.FailureAttribution(nil), in.Failures...)
	out.EntropyAudits = append([]gopact.EntropyAudit(nil), in.EntropyAudits...)
	out.VerificationReports = append([]gopact.VerificationReport(nil), in.VerificationReports...)
	out.Metadata = copyDeferredMemoryWorkQueueConformanceAnyMap(in.Metadata)
	return out
}

func copyDeferredMemoryWorkQueueConformanceAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
