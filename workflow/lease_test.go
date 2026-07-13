package workflow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestMemoryCheckpointerClaimRejectsLeaseRenewedAfterLoad(t *testing.T) {
	ctx := context.Background()
	past := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryCheckpointer()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		OwnerID: "owner-a", ClaimSequence: 1, LeaseExpiresAt: past,
		CreatedAt: past, UpdatedAt: past,
	}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	stale, err := store.Load(ctx, record.RunID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := store.RenewLease(ctx, CheckpointLease{
		RunID: record.RunID, OwnerID: "owner-a", ClaimSequence: 1, ExpiresAt: future,
	}); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	stale.OwnerID = "owner-b"
	stale.LeaseExpiresAt = future
	stale.ClaimSequence++
	if err := store.Claim(ctx, stale, stale.Version); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("Claim() error = %v, want ErrCheckpointConflict", err)
	}
	loaded, err := store.Load(ctx, record.RunID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.OwnerID != "owner-a" || !loaded.LeaseExpiresAt.Equal(future) || loaded.ClaimSequence != 1 || loaded.Version != 1 {
		t.Fatalf("Load() = %+v, want owner-a expiry %v claim sequence 1 version 1", loaded, future)
	}
}

func TestMemoryCheckpointerClaimAllowsOneConcurrentClaimant(t *testing.T) {
	ctx := context.Background()
	past := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryCheckpointer()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		OwnerID: "expired-owner", ClaimSequence: 1, LeaseExpiresAt: past,
		CreatedAt: past, UpdatedAt: past,
	}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	head, err := store.Load(ctx, record.RunID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, ownerID := range []string{"owner-a", "owner-b"} {
		candidate := head
		candidate.OwnerID = ownerID
		candidate.LeaseExpiresAt = future
		candidate.ClaimSequence++
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- store.Claim(ctx, candidate, head.Version)
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrCheckpointConflict):
			conflicted++
		default:
			t.Fatalf("Claim() error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("Claim() results = %d succeeded, %d conflicted; want 1 and 1", succeeded, conflicted)
	}
	loaded, err := store.Load(ctx, record.RunID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Version != head.Version+1 || loaded.ClaimSequence != head.ClaimSequence+1 {
		t.Fatalf("Load() = version %d claim sequence %d, want %d and %d", loaded.Version, loaded.ClaimSequence, head.Version+1, head.ClaimSequence+1)
	}
}

func TestMemoryCheckpointerRenewLease(t *testing.T) {
	now := time.Now().UTC()
	newRecord := func() CheckpointRecord {
		return CheckpointRecord{
			ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
			TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
			Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
			OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: now.Add(time.Minute),
			CreatedAt: now, UpdatedAt: now,
		}
	}

	t.Run("updates current record without history version", func(t *testing.T) {
		store := NewMemoryCheckpointer()
		if err := store.Create(context.Background(), newRecord()); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		expiresAt := now.Add(2 * time.Minute)
		if err := store.RenewLease(context.Background(), CheckpointLease{
			RunID: "run-1", OwnerID: "owner-1", ClaimSequence: 1, ExpiresAt: expiresAt,
		}); err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		loaded, err := store.Load(context.Background(), "run-1")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if loaded.Version != 1 || !loaded.LeaseExpiresAt.Equal(expiresAt) {
			t.Fatalf("Load() = %+v, want version 1 and expiry %v", loaded, expiresAt)
		}
		history, err := store.ListCheckpoints(context.Background(), CheckpointHistoryRequest{RunID: "run-1"})
		if err != nil {
			t.Fatalf("ListCheckpoints() error = %v", err)
		}
		if len(history) != 1 {
			t.Fatalf("history versions = %d, want 1", len(history))
		}
	})

	tests := []struct {
		name        string
		mutateLease func(*CheckpointLease)
		terminal    bool
	}{
		{
			name: "wrong owner",
			mutateLease: func(lease *CheckpointLease) {
				lease.OwnerID = "owner-2"
			},
		},
		{
			name: "wrong claim sequence",
			mutateLease: func(lease *CheckpointLease) {
				lease.ClaimSequence = 2
			},
		},
		{
			name:     "terminal checkpoint",
			terminal: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryCheckpointer()
			record := newRecord()
			lease := CheckpointLease{
				RunID: "run-1", OwnerID: "owner-1", ClaimSequence: 1, ExpiresAt: now.Add(2 * time.Minute),
			}
			if err := store.Create(context.Background(), record); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if test.terminal {
				record.Status = CheckpointCompleted
				if err := store.Finish(context.Background(), record, record.Version); err != nil {
					t.Fatalf("Finish() error = %v", err)
				}
			}
			if test.mutateLease != nil {
				test.mutateLease(&lease)
			}
			if err := store.RenewLease(context.Background(), lease); !errors.Is(err, ErrCheckpointLeaseLost) {
				t.Fatalf("RenewLease() error = %v, want ErrCheckpointLeaseLost", err)
			}
		})
	}
}

func TestWorkflowHeartbeatKeepsLongNodeLeased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
		)
		store := NewMemoryStore()
		started := make(chan struct{})
		release := make(chan struct{})
		first := New[string, string](
			"lease-heartbeat",
			WithCheckpointer(store),
			WithCheckpointLease(leaseDuration, renewEvery),
		)
		firstNode := first.Node("node", func(ctx context.Context, input string) (string, error) {
			close(started)
			select {
			case <-release:
				return input, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		})
		first.Entry(firstNode)
		first.Exit(firstNode)

		var secondRuns atomic.Int32
		second := New[string, string](
			"lease-heartbeat",
			WithCheckpointer(store),
			WithCheckpointLease(leaseDuration, renewEvery),
		)
		secondNode := second.Node("node", func(_ context.Context, input string) (string, error) {
			secondRuns.Add(1)
			return input, nil
		})
		second.Entry(secondNode)
		second.Exit(secondNode)

		firstDone := make(chan error, 1)
		go func() {
			_, err := first.Invoke(context.Background(), "input", gopact.WithRunID("run-1"))
			firstDone <- err
		}()
		<-started
		time.Sleep(2 * leaseDuration)
		synctest.Wait()

		_, err := second.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "run-1"}))
		if !errors.Is(err, ErrCheckpointConflict) {
			t.Fatalf("second Invoke() error = %v, want ErrCheckpointConflict", err)
		}
		if got := secondRuns.Load(); got != 0 {
			t.Fatalf("second node runs = %d, want 0", got)
		}

		close(release)
		synctest.Wait()
		if err := <-firstDone; err != nil {
			t.Fatalf("first Invoke() error = %v", err)
		}
	})
}

func TestWorkflowLeaseLossCancelsNode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &leaseRejectingCheckpointer{MemoryCheckpointer: NewMemoryCheckpointer()}
		started := make(chan struct{})
		wf := New[int, int](
			"lease-loss",
			WithCheckpointer(store),
			WithCheckpointLease(time.Minute, 20*time.Second),
		)
		node := wf.Node("node", func(ctx context.Context, _ int) (int, error) {
			close(started)
			<-ctx.Done()
			return 0, ctx.Err()
		})
		wf.Entry(node)
		wf.Exit(node)

		done := make(chan error, 1)
		go func() {
			_, err := wf.Invoke(context.Background(), 1, gopact.WithRunID("run-1"))
			done <- err
		}()
		<-started
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if err := <-done; !errors.Is(err, ErrCheckpointLeaseLost) {
			t.Fatalf("Invoke() error = %v, want ErrCheckpointLeaseLost", err)
		}
	})
}

func TestWorkflowRejectsInvalidCheckpointLease(t *testing.T) {
	wf := New[string, string]("invalid-lease", WithCheckpointLease(time.Second, time.Second))
	node := wf.Node("node", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	if err := wf.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid checkpoint lease")
	}
}

type leaseRejectingCheckpointer struct {
	*MemoryCheckpointer
}

func (store *leaseRejectingCheckpointer) RenewLease(context.Context, CheckpointLease) error {
	return ErrCheckpointConflict
}
