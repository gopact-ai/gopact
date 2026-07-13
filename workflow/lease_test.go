package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gopact-ai/gopact"
)

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
