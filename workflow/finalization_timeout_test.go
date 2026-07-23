package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type deadlineObservation struct {
	remaining time.Duration
	ok        bool
}

type deadlineBlockingStore struct {
	*MemoryStore
	appendEntered       chan<- deadlineObservation
	finishEntered       chan<- deadlineObservation
	interruptEntered    chan<- deadlineObservation
	leaseReleaseEntered chan<- deadlineObservation
	appendErr           error
	release             <-chan struct{}
	appendOnce          sync.Once
	finishOnce          sync.Once
	interruptOnce       sync.Once
	leaseReleaseOnce    sync.Once
}

func (store *deadlineBlockingStore) AppendFenced(ctx context.Context, record runlog.Record, fence runlog.Fence) error {
	if store.appendErr != nil {
		return store.appendErr
	}
	blocked := false
	if store.appendEntered != nil {
		store.appendOnce.Do(func() { blocked = true })
	}
	if !blocked {
		return store.MemoryStore.AppendFenced(ctx, record, fence)
	}
	store.appendEntered <- observeDeadline(ctx)
	return waitForDeadline(ctx, store.release)
}

func (store *deadlineBlockingStore) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	var entered chan<- deadlineObservation
	switch {
	case store.interruptEntered != nil && record.Status == CheckpointInterrupted:
		store.interruptOnce.Do(func() { entered = store.interruptEntered })
	case store.leaseReleaseEntered != nil && record.Status == CheckpointRunning &&
		record.OwnerID == "" && record.LeaseExpiresAt.IsZero() && record.LeaseDuration == 0:
		store.leaseReleaseOnce.Do(func() { entered = store.leaseReleaseEntered })
	}
	if entered == nil {
		return store.MemoryStore.Save(ctx, record, version)
	}
	entered <- observeDeadline(ctx)
	return waitForDeadline(ctx, store.release)
}

func (store *deadlineBlockingStore) Finish(ctx context.Context, record CheckpointRecord, version int64) error {
	blocked := false
	if store.finishEntered != nil {
		store.finishOnce.Do(func() { blocked = true })
	}
	if !blocked {
		return store.MemoryStore.Finish(ctx, record, version)
	}
	store.finishEntered <- observeDeadline(ctx)
	return waitForDeadline(ctx, store.release)
}

func observeDeadline(ctx context.Context) deadlineObservation {
	deadline, ok := ctx.Deadline()
	return deadlineObservation{remaining: time.Until(deadline), ok: ok}
}

func waitForDeadline(ctx context.Context, release <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-release:
		return errors.New("workflow test: released blocked store")
	}
}

func TestWorkflowJournalAuthorityWriteHasLeaseBoundDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
			writeTimeout  = leaseDuration - renewEvery
		)
		entered := make(chan deadlineObservation, 1)
		release := make(chan struct{})
		store := &deadlineBlockingStore{
			MemoryStore: NewMemoryStore(), appendEntered: entered, release: release,
		}
		wf := finalizationTimeoutWorkflow(store, leaseDuration, renewEvery)
		done := make(chan error, 1)
		go func() {
			_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID("bounded-journal"))
			done <- err
		}()

		observation := <-entered
		if !observation.ok {
			close(release)
			synctest.Wait()
			<-done
			t.Fatal("AppendFenced() context has no deadline")
		}
		if observation.remaining != writeTimeout {
			t.Fatalf("AppendFenced() deadline = %s, want %s", observation.remaining, writeTimeout)
		}
		time.Sleep(writeTimeout)
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Invoke() error = %v, want context deadline exceeded", err)
		}
		record, err := store.Load(t.Context(), "bounded-journal")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if record.OwnerID != "" || !record.LeaseExpiresAt.IsZero() {
			t.Fatalf("checkpoint lease = %q/%s, want released after sink failure", record.OwnerID, record.LeaseExpiresAt)
		}
	})
}

func TestWorkflowTerminalFinishHasLeaseBoundDeadlineAndCanResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
			writeTimeout  = leaseDuration - renewEvery
		)
		entered := make(chan deadlineObservation, 1)
		release := make(chan struct{})
		store := &deadlineBlockingStore{
			MemoryStore: NewMemoryStore(), finishEntered: entered, release: release,
		}
		wf := finalizationTimeoutWorkflow(store, leaseDuration, renewEvery)
		done := make(chan error, 1)
		go func() {
			_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID("bounded-finish"))
			done <- err
		}()

		observation := <-entered
		if !observation.ok {
			close(release)
			synctest.Wait()
			<-done
			t.Fatal("Finish() context has no deadline")
		}
		if observation.remaining != writeTimeout {
			t.Fatalf("Finish() deadline = %s, want %s", observation.remaining, writeTimeout)
		}
		time.Sleep(writeTimeout)
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("first Invoke() error = %v, want context deadline exceeded", err)
		}
		record, err := store.Load(t.Context(), "bounded-finish")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if record.Status != CheckpointRunning || record.PendingSequence == 0 {
			t.Fatalf("checkpoint = %+v, want recoverable pending terminal", record)
		}

		time.Sleep(renewEvery + time.Nanosecond)
		synctest.Wait()
		got, err := finalizationTimeoutWorkflow(store, leaseDuration, renewEvery).Invoke(
			context.Background(),
			"ignored",
			WithResume(ResumeRequest{RunID: "bounded-finish"}),
		)
		if err != nil || got != "input" {
			t.Fatalf("resume Invoke() = %q, %v, want input", got, err)
		}
		record, err = store.Load(t.Context(), "bounded-finish")
		if err != nil || record.Status != CheckpointCompleted {
			t.Fatalf("final checkpoint = %+v, %v, want completed", record, err)
		}
	})
}

func TestWorkflowInterruptPersistenceHasLeaseBoundDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
			writeTimeout  = leaseDuration - renewEvery
		)
		entered := make(chan deadlineObservation, 1)
		release := make(chan struct{})
		store := &deadlineBlockingStore{
			MemoryStore: NewMemoryStore(), interruptEntered: entered, release: release,
		}
		wf := finalizationInterruptWorkflow(store, leaseDuration, renewEvery)
		done := make(chan error, 1)
		go func() {
			_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID("bounded-interrupt"))
			done <- err
		}()

		observation := <-entered
		if !observation.ok {
			close(release)
			synctest.Wait()
			<-done
			t.Fatal("interrupt Save() context has no deadline")
		}
		if observation.remaining != writeTimeout {
			t.Fatalf("interrupt Save() deadline = %s, want %s", observation.remaining, writeTimeout)
		}
		time.Sleep(writeTimeout)
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Invoke() error = %v, want context deadline exceeded", err)
		}
	})
}

func TestWorkflowLeaseReleaseHasLeaseBoundDeadlineAfterSinkFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
			writeTimeout  = leaseDuration - renewEvery
		)
		sinkErr := errors.New("workflow test: sink unavailable")
		entered := make(chan deadlineObservation, 1)
		release := make(chan struct{})
		store := &deadlineBlockingStore{
			MemoryStore: NewMemoryStore(), leaseReleaseEntered: entered,
			appendErr: sinkErr, release: release,
		}
		wf := finalizationTimeoutWorkflow(store, leaseDuration, renewEvery)
		done := make(chan error, 1)
		go func() {
			_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID("bounded-release"))
			done <- err
		}()

		observation := <-entered
		if !observation.ok {
			close(release)
			synctest.Wait()
			<-done
			t.Fatal("lease release Save() context has no deadline")
		}
		if observation.remaining != writeTimeout {
			t.Fatalf("lease release Save() deadline = %s, want %s", observation.remaining, writeTimeout)
		}
		time.Sleep(writeTimeout)
		synctest.Wait()
		if err := <-done; !errors.Is(err, sinkErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Invoke() error = %v, want sink and context deadline errors", err)
		}
	})
}

func finalizationTimeoutWorkflow(store Store, leaseTTL, renewEvery time.Duration) *Workflow[string, string] {
	wf := New[string, string](
		"finalization-timeout",
		WithStore(store),
		WithCheckpointLease(leaseTTL, renewEvery),
	)
	node := wf.Node("node", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	return wf
}

func finalizationInterruptWorkflow(store Store, leaseTTL, renewEvery time.Duration) *Workflow[string, string] {
	wf := New[string, string](
		"finalization-interrupt",
		WithStore(store),
		WithCheckpointLease(leaseTTL, renewEvery),
	)
	node := wf.Node("node", func(_ context.Context, input string) (string, error) { return input, nil })
	node.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval"}}, nil
		},
	)))
	wf.Entry(node)
	wf.Exit(node)
	return wf
}
