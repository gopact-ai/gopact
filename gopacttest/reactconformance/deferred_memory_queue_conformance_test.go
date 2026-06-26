package reactconformance

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/react"
)

func TestCheckDeferredMemoryWorkQueueConformancePassesWellBehavedQueue(t *testing.T) {
	harness := DeferredMemoryWorkQueueConformanceHarness{
		NewQueue: func(jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
			return newConformanceMemoryQueue(jobs, nil), nil
		},
	}

	results := CheckDeferredMemoryWorkQueueConformance(context.Background(), harness)
	if failed := failedDeferredMemoryWorkQueueConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckDeferredMemoryWorkQueueConformance() failed cases: %v", failed)
	}
	RequireDeferredMemoryWorkQueueConformance(t, harness)
}

func TestCheckDeferredMemoryWorkQueueConformanceReportsRetryWithoutRequeue(t *testing.T) {
	harness := DeferredMemoryWorkQueueConformanceHarness{
		NewQueue: func(jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
			return newConformanceMemoryQueue(jobs, map[string]bool{"retry": true}), nil
		},
	}

	results := CheckDeferredMemoryWorkQueueConformance(context.Background(), harness)
	if !hasFailedDeferredMemoryWorkQueueConformanceCase(results, "retry-requeues-job") {
		t.Fatalf("CheckDeferredMemoryWorkQueueConformance() did not report retry failure: %+v", results)
	}
	if err := deferredMemoryWorkQueueConformanceError(results); !errors.Is(err, ErrDeferredMemoryWorkQueueConformanceFailed) {
		t.Fatalf("conformance error = %v, want ErrDeferredMemoryWorkQueueConformanceFailed", err)
	}
}

func TestCheckDeferredMemoryWorkQueueConformanceReportsInputMutation(t *testing.T) {
	harness := DeferredMemoryWorkQueueConformanceHarness{
		NewQueue: func(jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
			return newConformanceMemoryQueue(jobs, map[string]bool{"complete_mutates_job": true}), nil
		},
	}

	results := CheckDeferredMemoryWorkQueueConformance(context.Background(), harness)
	if !hasFailedDeferredMemoryWorkQueueConformanceCase(results, "complete-does-not-mutate-input") {
		t.Fatalf("CheckDeferredMemoryWorkQueueConformance() did not report input mutation: %+v", results)
	}
}

func TestCheckDeferredMemoryWorkQueueVisibilityConformancePassesWellBehavedQueue(t *testing.T) {
	clock := time.Unix(100, 0)
	harness := DeferredMemoryWorkQueueVisibilityConformanceHarness{
		NewQueue: func(jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
			return newConformanceVisibilityQueue(jobs, &clock, nil), nil
		},
		AdvanceVisibilityTimeout: func(context.Context) error {
			clock = clock.Add(time.Minute + time.Nanosecond)
			return nil
		},
	}

	results := CheckDeferredMemoryWorkQueueVisibilityConformance(context.Background(), harness)
	if failed := failedDeferredMemoryWorkQueueConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckDeferredMemoryWorkQueueVisibilityConformance() failed cases: %v", failed)
	}
	RequireDeferredMemoryWorkQueueVisibilityConformance(t, harness)
}

func TestCheckDeferredMemoryWorkQueueVisibilityConformancePassesMemoryQueue(t *testing.T) {
	const timeout = 2 * time.Millisecond
	harness := DeferredMemoryWorkQueueVisibilityConformanceHarness{
		NewQueue: func(jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
			return react.NewMemoryDeferredMemoryWorkQueueWithVisibilityTimeout(timeout, jobs...)
		},
		AdvanceVisibilityTimeout: func(ctx context.Context) error {
			timer := time.NewTimer(timeout + 5*time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}

	results := CheckDeferredMemoryWorkQueueVisibilityConformance(context.Background(), harness)
	if failed := failedDeferredMemoryWorkQueueConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckDeferredMemoryWorkQueueVisibilityConformance(memory queue) failed cases: %v", failed)
	}
}

func TestCheckDeferredMemoryWorkQueueVisibilityConformanceReportsMissingDeliveryReceipt(t *testing.T) {
	clock := time.Unix(100, 0)
	harness := DeferredMemoryWorkQueueVisibilityConformanceHarness{
		NewQueue: func(jobs []react.DeferredMemoryWorkJob) (react.DeferredMemoryWorkQueue, error) {
			return newConformanceVisibilityQueue(jobs, &clock, map[string]bool{"missing_delivery_id": true}), nil
		},
		AdvanceVisibilityTimeout: func(context.Context) error {
			clock = clock.Add(time.Minute + time.Nanosecond)
			return nil
		},
	}

	results := CheckDeferredMemoryWorkQueueVisibilityConformance(context.Background(), harness)
	if !hasFailedDeferredMemoryWorkQueueConformanceCase(results, "visibility-dequeue-sets-delivery-receipt") {
		t.Fatalf("CheckDeferredMemoryWorkQueueVisibilityConformance() did not report missing receipt: %+v", results)
	}
}

type conformanceMemoryQueue struct {
	jobs  []react.DeferredMemoryWorkJob
	fault map[string]bool
}

type conformanceVisibilityInFlight struct {
	job       react.DeferredMemoryWorkJob
	visibleAt time.Time
}

type conformanceVisibilityQueue struct {
	pending      []react.DeferredMemoryWorkJob
	inFlight     []conformanceVisibilityInFlight
	now          *time.Time
	nextDelivery int
	fault        map[string]bool
}

func newConformanceMemoryQueue(jobs []react.DeferredMemoryWorkJob, fault map[string]bool) *conformanceMemoryQueue {
	out := &conformanceMemoryQueue{fault: fault}
	for _, job := range jobs {
		out.jobs = append(out.jobs, copyConformanceMemoryJob(job))
	}
	return out
}

func newConformanceVisibilityQueue(jobs []react.DeferredMemoryWorkJob, now *time.Time, fault map[string]bool) *conformanceVisibilityQueue {
	out := &conformanceVisibilityQueue{now: now, fault: fault}
	for _, job := range jobs {
		job = copyConformanceMemoryJob(job)
		job.DeliveryID = ""
		out.pending = append(out.pending, job)
	}
	return out
}

func (q *conformanceMemoryQueue) Dequeue(ctx context.Context) (react.DeferredMemoryWorkJob, bool, error) {
	if err := ctx.Err(); err != nil {
		return react.DeferredMemoryWorkJob{}, false, err
	}
	if len(q.jobs) == 0 {
		return react.DeferredMemoryWorkJob{}, false, nil
	}
	job := copyConformanceMemoryJob(q.jobs[0])
	q.jobs = q.jobs[1:]
	return job, true, nil
}

func (q *conformanceMemoryQueue) Complete(ctx context.Context, job react.DeferredMemoryWorkJob, report react.DeferredMemoryWorkReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.fault["complete_mutates_job"] {
		job.Metadata["mutated"] = true
	}
	return nil
}

func (q *conformanceMemoryQueue) Retry(ctx context.Context, job react.DeferredMemoryWorkJob, decision react.DeferredMemoryWorkScheduleDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.fault["retry"] {
		return nil
	}
	next := copyConformanceMemoryJob(job)
	next.Attempt = decision.NextAttempt
	next.MaxAttempts = decision.MaxAttempts
	next.Metadata = copyConformanceMemoryMetadata(decision.Metadata)
	q.jobs = append(q.jobs, next)
	return nil
}

func (q *conformanceMemoryQueue) DeadLetter(ctx context.Context, job react.DeferredMemoryWorkJob, decision react.DeferredMemoryWorkScheduleDecision) error {
	return ctx.Err()
}

func (q *conformanceMemoryQueue) Stop(ctx context.Context, job react.DeferredMemoryWorkJob, report react.DeferredMemoryWorkReport, decision react.DeferredMemoryWorkScheduleDecision) error {
	return ctx.Err()
}

func (q *conformanceVisibilityQueue) Dequeue(ctx context.Context) (react.DeferredMemoryWorkJob, bool, error) {
	if err := ctx.Err(); err != nil {
		return react.DeferredMemoryWorkJob{}, false, err
	}
	q.requeueExpired()
	if len(q.pending) == 0 {
		return react.DeferredMemoryWorkJob{}, false, nil
	}
	job := copyConformanceMemoryJob(q.pending[0])
	q.pending = q.pending[1:]
	q.nextDelivery++
	if !q.fault["missing_delivery_id"] {
		job.DeliveryID = "delivery-" + strconv.Itoa(q.nextDelivery)
	}
	q.inFlight = append(q.inFlight, conformanceVisibilityInFlight{
		job:       copyConformanceMemoryJob(job),
		visibleAt: q.now.Add(time.Minute),
	})
	return job, true, nil
}

func (q *conformanceVisibilityQueue) Complete(ctx context.Context, job react.DeferredMemoryWorkJob, report react.DeferredMemoryWorkReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.requeueExpired()
	if !q.removeInFlight(job) {
		return react.ErrDeferredMemoryWorkDeliveryNotFound
	}
	return nil
}

func (q *conformanceVisibilityQueue) Retry(ctx context.Context, job react.DeferredMemoryWorkJob, decision react.DeferredMemoryWorkScheduleDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.requeueExpired()
	if !q.removeInFlight(job) {
		return react.ErrDeferredMemoryWorkDeliveryNotFound
	}
	job = copyConformanceMemoryJob(job)
	job.DeliveryID = ""
	job.Attempt = decision.NextAttempt
	q.pending = append(q.pending, job)
	return nil
}

func (q *conformanceVisibilityQueue) DeadLetter(ctx context.Context, job react.DeferredMemoryWorkJob, decision react.DeferredMemoryWorkScheduleDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.requeueExpired()
	if !q.removeInFlight(job) {
		return react.ErrDeferredMemoryWorkDeliveryNotFound
	}
	return nil
}

func (q *conformanceVisibilityQueue) Stop(ctx context.Context, job react.DeferredMemoryWorkJob, report react.DeferredMemoryWorkReport, decision react.DeferredMemoryWorkScheduleDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.requeueExpired()
	if !q.removeInFlight(job) {
		return react.ErrDeferredMemoryWorkDeliveryNotFound
	}
	return nil
}

func (q *conformanceVisibilityQueue) requeueExpired() {
	if q.now == nil {
		return
	}
	kept := q.inFlight[:0]
	for _, inFlight := range q.inFlight {
		if !inFlight.visibleAt.After(*q.now) {
			job := copyConformanceMemoryJob(inFlight.job)
			job.DeliveryID = ""
			q.pending = append([]react.DeferredMemoryWorkJob{job}, q.pending...)
			continue
		}
		kept = append(kept, inFlight)
	}
	q.inFlight = kept
}

func (q *conformanceVisibilityQueue) removeInFlight(job react.DeferredMemoryWorkJob) bool {
	for i, inFlight := range q.inFlight {
		if inFlight.job.DeliveryID == "" || inFlight.job.DeliveryID != job.DeliveryID {
			continue
		}
		q.inFlight = append(q.inFlight[:i], q.inFlight[i+1:]...)
		return true
	}
	return false
}

func failedDeferredMemoryWorkQueueConformanceCases(results []DeferredMemoryWorkQueueConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedDeferredMemoryWorkQueueConformanceCase(results []DeferredMemoryWorkQueueConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

func deferredMemoryWorkQueueConformanceError(results []DeferredMemoryWorkQueueConformanceResult) error {
	for _, result := range results {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func copyConformanceMemoryJob(in react.DeferredMemoryWorkJob) react.DeferredMemoryWorkJob {
	out := in
	out.Export = conformanceMemoryRunExport()
	if in.Export.IDs.RunID != "" {
		out.Export = in.Export
	}
	out.Metadata = copyConformanceMemoryMetadata(in.Metadata)
	return out
}

func copyConformanceMemoryMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func conformanceMemoryRunExport() gopact.RunExport {
	return gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread"},
		CreatedAt: time.Unix(1, 0),
		Outcome:   gopact.RunCompleted,
	}
}
