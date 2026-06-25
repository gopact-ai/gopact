package reactconformance

import (
	"context"
	"errors"
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

type conformanceMemoryQueue struct {
	jobs  []react.DeferredMemoryWorkJob
	fault map[string]bool
}

func newConformanceMemoryQueue(jobs []react.DeferredMemoryWorkJob, fault map[string]bool) *conformanceMemoryQueue {
	out := &conformanceMemoryQueue{fault: fault}
	for _, job := range jobs {
		out.jobs = append(out.jobs, copyConformanceMemoryJob(job))
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
