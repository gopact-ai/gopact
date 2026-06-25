package react

import (
	"context"
	"sync"
)

// MemoryDeferredMemoryWorkQueue is an in-process DeferredMemoryWorkQueue.
//
// It is intended for tests, local workers, and single-process deployments. It
// records queue transitions for inspection, but it does not provide durability,
// visibility timeouts, distributed leases, or cross-process exactly-once
// guarantees.
type MemoryDeferredMemoryWorkQueue struct {
	mu           sync.Mutex
	pending      []DeferredMemoryWorkJob
	completed    []MemoryDeferredMemoryWorkQueueRecord
	retried      []MemoryDeferredMemoryWorkQueueRecord
	deadLettered []MemoryDeferredMemoryWorkQueueRecord
	stopped      []MemoryDeferredMemoryWorkQueueRecord
}

var _ DeferredMemoryWorkQueue = (*MemoryDeferredMemoryWorkQueue)(nil)

// MemoryDeferredMemoryWorkQueueRecord is one observed in-memory queue transition.
type MemoryDeferredMemoryWorkQueueRecord struct {
	Job      DeferredMemoryWorkJob              `json:"job"`
	Report   DeferredMemoryWorkReport           `json:"report,omitempty"`
	Decision DeferredMemoryWorkScheduleDecision `json:"decision,omitempty"`
}

// MemoryDeferredMemoryWorkQueueSnapshot is a defensive copy of the queue state.
type MemoryDeferredMemoryWorkQueueSnapshot struct {
	Pending      []DeferredMemoryWorkJob               `json:"pending,omitempty"`
	Completed    []MemoryDeferredMemoryWorkQueueRecord `json:"completed,omitempty"`
	Retried      []MemoryDeferredMemoryWorkQueueRecord `json:"retried,omitempty"`
	DeadLettered []MemoryDeferredMemoryWorkQueueRecord `json:"dead_lettered,omitempty"`
	Stopped      []MemoryDeferredMemoryWorkQueueRecord `json:"stopped,omitempty"`
}

// NewMemoryDeferredMemoryWorkQueue creates an in-process queue seeded with jobs.
func NewMemoryDeferredMemoryWorkQueue(jobs ...DeferredMemoryWorkJob) *MemoryDeferredMemoryWorkQueue {
	queue := &MemoryDeferredMemoryWorkQueue{}
	for _, job := range jobs {
		queue.pending = append(queue.pending, normalizeDeferredMemoryWorkJob(copyDeferredMemoryWorkJob(job)))
	}
	return queue
}

// Enqueue appends a job to the in-memory pending queue.
func (q *MemoryDeferredMemoryWorkQueue) Enqueue(ctx context.Context, job DeferredMemoryWorkJob) error {
	if err := deferredMemoryWorkQueueContextErr(ctx); err != nil {
		return err
	}
	if q == nil {
		return ErrDeferredMemoryWorkQueueRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, normalizeDeferredMemoryWorkJob(copyDeferredMemoryWorkJob(job)))
	return nil
}

// Dequeue removes the next pending job, if one is available.
func (q *MemoryDeferredMemoryWorkQueue) Dequeue(ctx context.Context) (DeferredMemoryWorkJob, bool, error) {
	if err := deferredMemoryWorkQueueContextErr(ctx); err != nil {
		return DeferredMemoryWorkJob{}, false, err
	}
	if q == nil {
		return DeferredMemoryWorkJob{}, false, ErrDeferredMemoryWorkQueueRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return DeferredMemoryWorkJob{}, false, nil
	}
	job := q.pending[0]
	q.pending = q.pending[1:]
	return copyDeferredMemoryWorkJob(job), true, nil
}

// Complete records successful handling of a dequeued job.
func (q *MemoryDeferredMemoryWorkQueue) Complete(ctx context.Context, job DeferredMemoryWorkJob, report DeferredMemoryWorkReport) error {
	if err := deferredMemoryWorkQueueContextErr(ctx); err != nil {
		return err
	}
	if q == nil {
		return ErrDeferredMemoryWorkQueueRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, MemoryDeferredMemoryWorkQueueRecord{
		Job:    copyDeferredMemoryWorkJob(job),
		Report: copyDeferredMemoryWorkReport(report),
	})
	return nil
}

// Retry records a retry decision and appends the job back to the pending queue.
func (q *MemoryDeferredMemoryWorkQueue) Retry(ctx context.Context, job DeferredMemoryWorkJob, decision DeferredMemoryWorkScheduleDecision) error {
	if err := deferredMemoryWorkQueueContextErr(ctx); err != nil {
		return err
	}
	if q == nil {
		return ErrDeferredMemoryWorkQueueRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	record := MemoryDeferredMemoryWorkQueueRecord{
		Job:      copyDeferredMemoryWorkJob(job),
		Decision: copyDeferredMemoryWorkScheduleDecision(decision),
	}
	q.retried = append(q.retried, record)
	q.pending = append(q.pending, deferredMemoryWorkRetryJob(job, decision))
	return nil
}

// DeadLetter records that a failed job was moved to the local dead-letter set.
func (q *MemoryDeferredMemoryWorkQueue) DeadLetter(ctx context.Context, job DeferredMemoryWorkJob, decision DeferredMemoryWorkScheduleDecision) error {
	if err := deferredMemoryWorkQueueContextErr(ctx); err != nil {
		return err
	}
	if q == nil {
		return ErrDeferredMemoryWorkQueueRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deadLettered = append(q.deadLettered, MemoryDeferredMemoryWorkQueueRecord{
		Job:      copyDeferredMemoryWorkJob(job),
		Decision: copyDeferredMemoryWorkScheduleDecision(decision),
	})
	return nil
}

// Stop records that the host stopped scheduling a job.
func (q *MemoryDeferredMemoryWorkQueue) Stop(ctx context.Context, job DeferredMemoryWorkJob, report DeferredMemoryWorkReport, decision DeferredMemoryWorkScheduleDecision) error {
	if err := deferredMemoryWorkQueueContextErr(ctx); err != nil {
		return err
	}
	if q == nil {
		return ErrDeferredMemoryWorkQueueRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stopped = append(q.stopped, MemoryDeferredMemoryWorkQueueRecord{
		Job:      copyDeferredMemoryWorkJob(job),
		Report:   copyDeferredMemoryWorkReport(report),
		Decision: copyDeferredMemoryWorkScheduleDecision(decision),
	})
	return nil
}

// Snapshot returns a defensive copy of the in-memory queue state.
func (q *MemoryDeferredMemoryWorkQueue) Snapshot() MemoryDeferredMemoryWorkQueueSnapshot {
	if q == nil {
		return MemoryDeferredMemoryWorkQueueSnapshot{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return MemoryDeferredMemoryWorkQueueSnapshot{
		Pending:      copyDeferredMemoryWorkJobs(q.pending),
		Completed:    copyMemoryDeferredMemoryWorkQueueRecords(q.completed),
		Retried:      copyMemoryDeferredMemoryWorkQueueRecords(q.retried),
		DeadLettered: copyMemoryDeferredMemoryWorkQueueRecords(q.deadLettered),
		Stopped:      copyMemoryDeferredMemoryWorkQueueRecords(q.stopped),
	}
}

func deferredMemoryWorkRetryJob(job DeferredMemoryWorkJob, decision DeferredMemoryWorkScheduleDecision) DeferredMemoryWorkJob {
	out := copyDeferredMemoryWorkJob(job)
	if decision.NextAttempt > 0 {
		out.Attempt = decision.NextAttempt
	} else {
		out.Attempt++
	}
	if decision.MaxAttempts > 0 {
		out.MaxAttempts = decision.MaxAttempts
	}
	out.Metadata = copyAnyMap(job.Metadata)
	for key, value := range decision.Metadata {
		if out.Metadata == nil {
			out.Metadata = map[string]any{}
		}
		out.Metadata[key] = value
	}
	return normalizeDeferredMemoryWorkJob(out)
}

func deferredMemoryWorkQueueContextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func copyDeferredMemoryWorkJobs(in []DeferredMemoryWorkJob) []DeferredMemoryWorkJob {
	if len(in) == 0 {
		return nil
	}
	out := make([]DeferredMemoryWorkJob, len(in))
	for i, job := range in {
		out[i] = copyDeferredMemoryWorkJob(job)
	}
	return out
}

func copyMemoryDeferredMemoryWorkQueueRecords(in []MemoryDeferredMemoryWorkQueueRecord) []MemoryDeferredMemoryWorkQueueRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]MemoryDeferredMemoryWorkQueueRecord, len(in))
	for i, record := range in {
		out[i] = MemoryDeferredMemoryWorkQueueRecord{
			Job:      copyDeferredMemoryWorkJob(record.Job),
			Report:   copyDeferredMemoryWorkReport(record.Report),
			Decision: copyDeferredMemoryWorkScheduleDecision(record.Decision),
		}
	}
	return out
}
