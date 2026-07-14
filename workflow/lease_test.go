package workflow

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type renewalCountingStore struct {
	*MemoryStore
	renews atomic.Int32
}

type delayedRenewalStore struct {
	Store
	delay time.Duration
	calls int
}

func (store *delayedRenewalStore) RenewLease(ctx context.Context, _ CheckpointLease) error {
	store.calls++
	timer := time.NewTimer(store.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (store *renewalCountingStore) RenewLease(ctx context.Context, lease CheckpointLease) error {
	store.renews.Add(1)
	return store.MemoryStore.RenewLease(ctx, lease)
}

func TestMemoryCheckpointerClaimRejectsLeaseRenewedAfterLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		now := time.Now()
		originalExpiry := now.Add(2 * time.Second)
		renewedExpiry := now.Add(time.Minute)
		store := NewMemoryCheckpointer()
		record := CheckpointRecord{
			ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
			TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
			Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
			OwnerID: "owner-a", ClaimSequence: 1, LeaseExpiresAt: originalExpiry,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		stale, err := store.Load(ctx, record.RunID)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		time.Sleep(time.Second)
		if err := store.RenewLease(ctx, CheckpointLease{
			RunID: record.RunID, OwnerID: "owner-a", ClaimSequence: 1, ExpiresAt: renewedExpiry,
		}); err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		time.Sleep(2 * time.Second)
		stale.OwnerID = "owner-b"
		stale.LeaseExpiresAt = time.Now().Add(time.Minute)
		stale.ClaimSequence++
		if err := store.Claim(ctx, stale, stale.Version); !errors.Is(err, ErrCheckpointConflict) {
			t.Fatalf("Claim() error = %v, want ErrCheckpointConflict", err)
		}
		loaded, err := store.Load(ctx, record.RunID)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if loaded.OwnerID != "owner-a" || !loaded.LeaseExpiresAt.Equal(renewedExpiry) ||
			loaded.ClaimSequence != 1 || loaded.Version != 1 {
			t.Fatalf("Load() = %+v, want owner-a expiry %v claim sequence 1 version 1", loaded, renewedExpiry)
		}
	})
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

func TestMemoryCheckpointerLeaseDurationUsesLockedClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const leaseDuration = time.Minute
		ctx := context.Background()
		now := time.Now()
		store := NewMemoryCheckpointer()
		record := CheckpointRecord{
			ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
			TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
			Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
			OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: now.Add(24 * time.Hour),
			LeaseDuration: leaseDuration, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		loaded, err := store.Load(ctx, record.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if want := now.Add(leaseDuration); !loaded.LeaseExpiresAt.Equal(want) {
			t.Fatalf("Create() expiry = %v, want locked now + duration %v", loaded.LeaseExpiresAt, want)
		}
		if loaded.LeaseDuration != 0 {
			t.Fatalf("Create()/Load() lease duration = %v, want transient value cleared", loaded.LeaseDuration)
		}
		if store.records[record.RunID].LeaseDuration != 0 || store.history[record.RunID][0].LeaseDuration != 0 {
			t.Fatal("Create() persisted transient lease duration")
		}
		history, err := store.ListCheckpoints(ctx, CheckpointHistoryRequest{RunID: record.RunID})
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 1 || history[0].LeaseDuration != 0 {
			t.Fatalf("Create()/ListCheckpoints() = %+v, want transient lease duration cleared", history)
		}

		loaded.LeaseDuration = leaseDuration
		loaded.LeaseExpiresAt = now.Add(24 * time.Hour)
		loaded.UpdatedAt = now.Add(time.Second)
		if err := store.Save(ctx, loaded, loaded.Version); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		loaded, err = store.Load(ctx, record.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if want := now.Add(leaseDuration); !loaded.LeaseExpiresAt.Equal(want) {
			t.Fatalf("Save() expiry = %v, want %v instead of caller expiry", loaded.LeaseExpiresAt, want)
		}
		if loaded.LeaseDuration != 0 {
			t.Fatalf("Save()/Load() lease duration = %v, want transient value cleared", loaded.LeaseDuration)
		}
		if store.records[record.RunID].LeaseDuration != 0 || store.history[record.RunID][1].LeaseDuration != 0 {
			t.Fatal("Save() persisted transient lease duration")
		}
		history, err = store.ListCheckpoints(ctx, CheckpointHistoryRequest{RunID: record.RunID})
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 2 || history[1].LeaseDuration != 0 {
			t.Fatalf("Save()/ListCheckpoints() = %+v, want transient lease duration cleared", history)
		}

		time.Sleep(10 * time.Second)
		if err := store.RenewLease(ctx, CheckpointLease{
			RunID: record.RunID, OwnerID: record.OwnerID, ClaimSequence: record.ClaimSequence,
			ExpiresAt: now.Add(24 * time.Hour), Duration: leaseDuration,
		}); err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		loaded, err = store.Load(ctx, record.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Now().Add(leaseDuration); !loaded.LeaseExpiresAt.Equal(want) {
			t.Fatalf("RenewLease() expiry = %v, want locked now + duration %v", loaded.LeaseExpiresAt, want)
		}
		if loaded.LeaseDuration != 0 {
			t.Fatalf("RenewLease()/Load() lease duration = %v, want transient value cleared", loaded.LeaseDuration)
		}
		if store.records[record.RunID].LeaseDuration != 0 {
			t.Fatal("RenewLease() persisted transient lease duration")
		}
	})
}

func TestMemoryCheckpointerClaimUsesLeaseDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const leaseDuration = time.Minute
		ctx := context.Background()
		now := time.Now()
		store := NewMemoryCheckpointer()
		record := CheckpointRecord{
			ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
			TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
			Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
			OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: now.Add(-time.Minute),
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
		candidate := record
		candidate.OwnerID = "owner-2"
		candidate.ClaimSequence++
		candidate.LeaseExpiresAt = now.Add(24 * time.Hour)
		candidate.LeaseDuration = leaseDuration
		if err := store.Claim(ctx, candidate, record.Version); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		loaded, err := store.Load(ctx, record.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if want := now.Add(leaseDuration); !loaded.LeaseExpiresAt.Equal(want) {
			t.Fatalf("Claim() expiry = %v, want locked now + duration %v", loaded.LeaseExpiresAt, want)
		}
		if loaded.LeaseDuration != 0 {
			t.Fatalf("Claim()/Load() lease duration = %v, want transient value cleared", loaded.LeaseDuration)
		}
		if store.records[record.RunID].LeaseDuration != 0 || store.history[record.RunID][1].LeaseDuration != 0 {
			t.Fatal("Claim() persisted transient lease duration")
		}
		history, err := store.ListCheckpoints(ctx, CheckpointHistoryRequest{RunID: record.RunID})
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 2 || history[1].LeaseDuration != 0 {
			t.Fatalf("Claim()/ListCheckpoints() = %+v, want transient lease duration cleared", history)
		}
	})
}

func TestWorkflowClaimDefersExpiryDecisionToStore(t *testing.T) {
	const leaseDuration = time.Minute
	futureOnWorkflowClock := time.Now().Add(24 * time.Hour)
	store := &authoritativeClaimCheckpointer{}
	compiled := &compiled[int, int]{
		name: "example", topologyVersion: "topology-v1",
		store: storeWithCheckpointer(store), checkpointLeaseDuration: leaseDuration,
	}
	payload, err := encodeCheckpointPayloadWithMeta[int](runState{}, nil, 1, compiled.checkpointMeta(checkpointPayloadMeta{
		OwnerID: "owner-old", LeaseExpiresAt: futureOnWorkflowClock, ClaimSequence: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: compiled.name,
		TopologyVersion: compiled.topologyVersion, SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: payload,
		OwnerID: "owner-old", ClaimSequence: 1, LeaseExpiresAt: futureOnWorkflowClock,
		CreatedAt: now, UpdatedAt: now,
	}
	claimed, err := compiled.claimCheckpoint(t.Context(), record, "owner-new", ResumeRequest{})
	if err != nil {
		t.Fatalf("claimCheckpoint() error = %v, want Store to decide expiry", err)
	}
	if len(store.claimed) != 1 || claimed.OwnerID != "owner-new" || claimed.LeaseDuration != leaseDuration {
		t.Fatalf("claimed = %+v, Store calls = %+v", claimed, store.claimed)
	}
}

func TestWorkflowRenewLeasePassesDuration(t *testing.T) {
	const leaseDuration = 37 * time.Second
	now := time.Now()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: now.Add(time.Minute),
		CreatedAt: now, UpdatedAt: now,
	}
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{record.RunID: record}}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	execution := workflowExecution[int, int]{
		compiled: &compiled[int, int]{
			store: storeWithCheckpointer(store), checkpointLeaseDuration: leaseDuration,
			checkpointLeaseRenewEvery: 10 * time.Second,
		},
		checkpoint: record,
		cancel:     cancel,
	}
	lease := CheckpointLease{RunID: record.RunID, OwnerID: record.OwnerID, ClaimSequence: record.ClaimSequence}
	if err := execution.renewCheckpointLease(ctx, &lease); err != nil {
		t.Fatalf("renewCheckpointLease() error = %v", err)
	}
	if len(store.renewed) != 1 || store.renewed[0].Duration != leaseDuration {
		t.Fatalf("renewed leases = %+v, want duration %v", store.renewed, leaseDuration)
	}
	if store.renewed[0].ExpiresAt.IsZero() {
		t.Fatal("renewed lease ExpiresAt is zero, want compatibility value")
	}
}

func TestWorkflowRenewLeaseAllowsStoreLatencyBeyondRenewInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = 300 * time.Millisecond
			renewEvery    = 50 * time.Millisecond
		)
		store := &delayedRenewalStore{Store: NewMemoryStore(), delay: 2 * renewEvery}
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		execution := workflowExecution[int, int]{
			compiled: &compiled[int, int]{
				store: store, checkpointLeaseDuration: leaseDuration,
				checkpointLeaseRenewEvery: renewEvery,
			},
			cancel: cancel,
		}
		lease := CheckpointLease{RunID: "run-1", OwnerID: "owner-1", ClaimSequence: 1}
		if err := execution.renewCheckpointLease(ctx, &lease); err != nil {
			t.Fatalf("renewCheckpointLease() error = %v", err)
		}
		if store.calls != 1 {
			t.Fatalf("RenewLease() calls = %d, want 1", store.calls)
		}
	})
}

type authoritativeClaimCheckpointer struct {
	recordingCheckpointer
	claimed []CheckpointRecord
}

func (store *authoritativeClaimCheckpointer) Claim(_ context.Context, candidate CheckpointRecord, _ int64) error {
	store.claimed = append(store.claimed, candidate)
	return nil
}

func TestMemoryCheckpointerSavePreservesConcurrentLeaseRenewal(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	originalExpiry := now.Add(time.Minute)
	renewedExpiry := now.Add(2 * time.Minute)
	store := NewMemoryCheckpointer()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: originalExpiry,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	stale, err := store.Load(ctx, record.RunID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := store.RenewLease(ctx, CheckpointLease{
		RunID: record.RunID, OwnerID: record.OwnerID,
		ClaimSequence: record.ClaimSequence, ExpiresAt: renewedExpiry,
	}); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	stale.ConfirmedSequence = 1
	stale.ReplayStatus = ReplaySafe
	stale.UpdatedAt = now.Add(time.Second)
	if err := store.Save(ctx, stale, stale.Version); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load(ctx, record.RunID)
	if err != nil {
		t.Fatalf("Load() after Save error = %v", err)
	}
	if loaded.Version != 2 || loaded.ConfirmedSequence != 1 ||
		loaded.OwnerID != record.OwnerID || loaded.ClaimSequence != record.ClaimSequence ||
		!loaded.LeaseExpiresAt.Equal(renewedExpiry) {
		t.Fatalf("Load() = %+v, want saved state with renewed expiry %v", loaded, renewedExpiry)
	}
}

func TestMemoryCheckpointerSaveRejectsChangedLeaseIdentity(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*CheckpointRecord)
	}{
		{
			name: "owner",
			mutate: func(record *CheckpointRecord) {
				record.OwnerID = "owner-2"
			},
		},
		{
			name: "claim sequence",
			mutate: func(record *CheckpointRecord) {
				record.ClaimSequence++
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryCheckpointer()
			record := CheckpointRecord{
				ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
				TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
				Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
				OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: now.Add(time.Minute),
				CreatedAt: now, UpdatedAt: now,
			}
			if err := store.Create(t.Context(), record); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			candidate := record
			test.mutate(&candidate)
			if err := store.Save(t.Context(), candidate, candidate.Version); !errors.Is(err, ErrCheckpointLeaseLost) {
				t.Fatalf("Save() error = %v, want ErrCheckpointLeaseLost", err)
			}
			loaded, err := store.Load(t.Context(), record.RunID)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if loaded.Version != record.Version || loaded.OwnerID != record.OwnerID ||
				loaded.ClaimSequence != record.ClaimSequence {
				t.Fatalf("Load() = %+v, want original lease identity", loaded)
			}
		})
	}
}

func TestMemoryCheckpointerWritesRejectExpiredLease(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name  string
		write func(context.Context, *MemoryCheckpointer, CheckpointRecord) error
	}{
		{
			name: "save",
			write: func(ctx context.Context, store *MemoryCheckpointer, record CheckpointRecord) error {
				record.LeaseExpiresAt = now.Add(time.Minute)
				return store.Save(ctx, record, record.Version)
			},
		},
		{
			name: "finish",
			write: func(ctx context.Context, store *MemoryCheckpointer, record CheckpointRecord) error {
				record.Status = CheckpointCompleted
				record.OwnerID = ""
				record.LeaseExpiresAt = time.Time{}
				return store.Finish(ctx, record, record.Version)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryCheckpointer()
			record := CheckpointRecord{
				ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
				TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
				Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
				OwnerID: "owner-1", ClaimSequence: 1, LeaseExpiresAt: now.Add(-time.Minute),
				CreatedAt: now, UpdatedAt: now,
			}
			if err := store.Create(t.Context(), record); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if err := test.write(t.Context(), store, record); !errors.Is(err, ErrCheckpointLeaseLost) {
				t.Fatalf("write error = %v, want ErrCheckpointLeaseLost", err)
			}
		})
	}
}

func TestMemoryStoreAppendFencedRejectsStaleLease(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	checkpoint := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		OwnerID: "owner-old", ClaimSequence: 1, LeaseExpiresAt: now.Add(-time.Minute),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(t.Context(), checkpoint); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	record := runlog.Record{
		SessionID: checkpoint.SessionID, RunID: checkpoint.RunID, Sequence: 1,
		EventType: "audit.custom", Source: "workflow", Timestamp: now,
	}
	if err := store.AppendFenced(t.Context(), record, runlog.Fence{
		OwnerID: checkpoint.OwnerID, ClaimSequence: checkpoint.ClaimSequence,
	}); !errors.Is(err, ErrCheckpointLeaseLost) {
		t.Fatalf("AppendFenced(expired) error = %v, want ErrCheckpointLeaseLost", err)
	}
	claimed := checkpoint
	claimed.OwnerID = "owner-new"
	claimed.ClaimSequence++
	claimed.LeaseExpiresAt = now.Add(time.Minute)
	if err := store.Claim(t.Context(), claimed, checkpoint.Version); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := store.AppendFenced(t.Context(), record, runlog.Fence{
		OwnerID: checkpoint.OwnerID, ClaimSequence: checkpoint.ClaimSequence,
	}); !errors.Is(err, ErrCheckpointLeaseLost) {
		t.Fatalf("AppendFenced(stale) error = %v, want ErrCheckpointLeaseLost", err)
	}
	if err := store.AppendFenced(t.Context(), record, runlog.Fence{
		OwnerID: claimed.OwnerID, ClaimSequence: claimed.ClaimSequence,
	}); err != nil {
		t.Fatalf("AppendFenced(current) error = %v", err)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: checkpoint.RunID})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].Sequence != record.Sequence {
		t.Fatalf("records = %+v, want current owner record", records)
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

	t.Run("rejects expired current claim", func(t *testing.T) {
		store := NewMemoryCheckpointer()
		record := newRecord()
		record.LeaseExpiresAt = now.Add(-time.Minute)
		if err := store.Create(context.Background(), record); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		err := store.RenewLease(context.Background(), CheckpointLease{
			RunID: "run-1", OwnerID: "owner-1", ClaimSequence: 1, ExpiresAt: now.Add(2 * time.Minute),
		})
		if !errors.Is(err, ErrCheckpointLeaseLost) {
			t.Fatalf("RenewLease() error = %v, want ErrCheckpointLeaseLost", err)
		}
	})

	t.Run("does not shorten current lease", func(t *testing.T) {
		store := NewMemoryCheckpointer()
		record := newRecord()
		record.LeaseExpiresAt = now.Add(2 * time.Minute)
		if err := store.Create(context.Background(), record); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if err := store.RenewLease(context.Background(), CheckpointLease{
			RunID: "run-1", OwnerID: "owner-1", ClaimSequence: 1, ExpiresAt: now.Add(time.Minute),
		}); err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		loaded, err := store.Load(context.Background(), "run-1")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !loaded.LeaseExpiresAt.Equal(record.LeaseExpiresAt) {
			t.Fatalf("lease expiry = %v, want preserved %v", loaded.LeaseExpiresAt, record.LeaseExpiresAt)
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
		store := &renewalCountingStore{MemoryStore: NewMemoryStore()}
		started := make(chan struct{})
		release := make(chan struct{})
		first := New[string, string](
			"lease-heartbeat",
			WithStore(store),
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
			WithStore(store),
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
		if store.renews.Load() == 0 {
			t.Fatal("observer blocked without any lease renewal")
		}

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

func TestWorkflowHeartbeatStaysActiveThroughTerminalObserver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
		)
		store := &renewalCountingStore{MemoryStore: NewMemoryStore()}
		entered := make(chan struct{})
		release := make(chan struct{})
		build := func() *Workflow[string, string] {
			wf := New[string, string]("terminal-observer-lease", WithStore(store), WithCheckpointLease(leaseDuration, renewEvery))
			node := wf.Node("node", func(_ context.Context, input string) (string, error) { return input, nil })
			wf.Entry(node)
			wf.Exit(node)
			return wf
		}

		done := make(chan error, 1)
		go func() {
			_, err := build().Invoke(context.Background(), "input", gopact.WithRunID("terminal-observer-run"),
				gopact.WithEventHandler(func(_ context.Context, event gopact.Event) error {
					if event.Type == EventWorkflowCompleted {
						close(entered)
						<-release
					}
					return nil
				}))
			done <- err
		}()
		<-entered
		time.Sleep(2 * leaseDuration)
		synctest.Wait()
		if store.renews.Load() == 0 {
			t.Fatal("observer blocked without any lease renewal")
		}

		_, err := build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "terminal-observer-run"}))
		if !errors.Is(err, ErrCheckpointConflict) {
			t.Fatalf("takeover Invoke() error = %v, want ErrCheckpointConflict", err)
		}
		close(release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatalf("first Invoke() error = %v", err)
		}
		renews := store.renews.Load()
		time.Sleep(2 * renewEvery)
		synctest.Wait()
		if store.renews.Load() != renews {
			t.Fatal("lease heartbeat continued after terminal checkpoint closed")
		}
	})
}

func TestWorkflowHeartbeatStaysActiveThroughInterruptObserver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			leaseDuration = time.Minute
			renewEvery    = 20 * time.Second
		)
		store := &renewalCountingStore{MemoryStore: NewMemoryStore()}
		entered := make(chan struct{})
		release := make(chan struct{})
		build := func() *Workflow[string, string] {
			wf := New[string, string]("interrupt-observer-lease", WithStore(store), WithCheckpointLease(leaseDuration, renewEvery))
			node := wf.Node("node", func(_ context.Context, input string) (string, error) { return input, nil })
			node.Guard(BeforeRun("approval", GuardFunc[string, string](func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1"}}, nil
			})))
			wf.Entry(node)
			wf.Exit(node)
			return wf
		}

		done := make(chan error, 1)
		go func() {
			_, err := build().Invoke(context.Background(), "input", gopact.WithRunID("interrupt-observer-run"),
				gopact.WithEventHandler(func(_ context.Context, event gopact.Event) error {
					if event.Type == EventWorkflowInterrupted {
						close(entered)
						<-release
					}
					return nil
				}))
			done <- err
		}()
		<-entered
		time.Sleep(2 * leaseDuration)
		synctest.Wait()
		if store.renews.Load() == 0 {
			t.Fatal("observer blocked without any lease renewal")
		}

		_, err := build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "interrupt-observer-run"}))
		if !errors.Is(err, ErrCheckpointConflict) {
			t.Fatalf("takeover Invoke() error = %v, want ErrCheckpointConflict", err)
		}
		close(release)
		synctest.Wait()
		var interrupted InterruptError
		if err := <-done; !errors.As(err, &interrupted) {
			t.Fatalf("first Invoke() error = %v, want InterruptError", err)
		}
		renews := store.renews.Load()
		time.Sleep(2 * renewEvery)
		synctest.Wait()
		if store.renews.Load() != renews {
			t.Fatal("lease heartbeat continued after interrupted checkpoint closed")
		}
	})
}

func TestWorkflowHeartbeatStaysActiveThroughErrorTerminalObservers(t *testing.T) {
	for _, test := range []struct {
		name      string
		eventType string
		terminate bool
	}{
		{name: "failed", eventType: EventWorkflowFailed},
		{name: "terminated", eventType: EventWorkflowTerminated, terminate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const (
					leaseDuration = time.Minute
					renewEvery    = 20 * time.Second
				)
				store := &renewalCountingStore{MemoryStore: NewMemoryStore()}
				entered := make(chan struct{})
				release := make(chan struct{})
				started := make(chan struct{})
				wf := New[string, string]("error-terminal-observer-"+test.name, WithStore(store), WithCheckpointLease(leaseDuration, renewEvery))
				node := wf.Node("node", func(ctx context.Context, input string) (string, error) {
					if !test.terminate {
						return "", errors.New("node failed")
					}
					close(started)
					<-ctx.Done()
					return "", ctx.Err()
				})
				wf.Entry(node)
				wf.Exit(node)
				done := make(chan error, 1)
				go func() {
					_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID("error-terminal-run-"+test.name),
						gopact.WithEventHandler(func(_ context.Context, event gopact.Event) error {
							if event.Type == test.eventType {
								if event.Summary != "" {
									t.Errorf("terminal event summary = %q, want metadata only", event.Summary)
								}
								close(entered)
								<-release
							}
							return nil
						}))
					done <- err
				}()
				if test.terminate {
					<-started
					if err := wf.Terminate("error-terminal-run-" + test.name); err != nil {
						t.Fatal(err)
					}
				}
				<-entered
				time.Sleep(2 * leaseDuration)
				synctest.Wait()
				if store.renews.Load() == 0 {
					t.Fatal("observer blocked without any lease renewal")
				}
				close(release)
				synctest.Wait()
				if err := <-done; err == nil {
					t.Fatal("Invoke() error = nil")
				}
				renews := store.renews.Load()
				time.Sleep(2 * renewEvery)
				synctest.Wait()
				if store.renews.Load() != renews {
					t.Fatal("lease heartbeat continued after terminal checkpoint closed")
				}
			})
		})
	}
}

func TestTerminalObserverUsesCallerContextAndFreshResumeConverges(t *testing.T) {
	tests := []struct {
		status    CheckpointStatus
		eventType string
	}{
		{status: CheckpointCompleted, eventType: EventWorkflowCompleted},
		{status: CheckpointFailed, eventType: EventWorkflowFailed},
		{status: CheckpointCanceled, eventType: EventWorkflowCanceled},
		{status: CheckpointTerminated, eventType: EventWorkflowTerminated},
	}
	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			testTerminalObserverCallerContext(t, test.status, test.eventType)
		})
	}
}

func testTerminalObserverCallerContext(t *testing.T, status CheckpointStatus, eventType string) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		const renewEvery = 20 * time.Second
		store := &renewalCountingStore{MemoryStore: NewMemoryStore()}
		started := make(chan struct{})
		entered := make(chan struct{})
		exited := make(chan struct{})
		build := func() *Workflow[string, string] {
			wf := New[string, string]("caller-context-"+string(status), WithStore(store), WithCheckpointLease(time.Minute, renewEvery))
			node := wf.Node("node", func(ctx context.Context, input string) (string, error) {
				return terminalContextNode(ctx, input, status, started)
			})
			wf.Entry(node)
			wf.Exit(node)
			return wf
		}
		wf := build()
		callerCtx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := wf.Invoke(callerCtx, "input", gopact.WithRunID("caller-context-run"), gopact.WithStrictEventHandler(func(ctx context.Context, event gopact.Event) error {
				if event.Type != eventType {
					return nil
				}
				close(entered)
				<-ctx.Done()
				close(exited)
				return ctx.Err()
			}))
			done <- err
		}()
		(terminalContextTrigger{t: t, wf: wf, status: status, started: started, entered: entered, cancel: cancel}).run()
		<-exited
		if err := <-done; err == nil {
			t.Fatal("first Invoke() error = nil")
		}
		renews := store.renews.Load()
		time.Sleep(2 * renewEvery)
		synctest.Wait()
		if store.renews.Load() != renews {
			t.Fatal("heartbeat continued after strict observer cancellation")
		}
		_, _ = build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "caller-context-run"}))
		record, err := store.Load(t.Context(), "caller-context-run")
		if err != nil {
			t.Fatal(err)
		}
		if record.Status != status {
			t.Fatalf("checkpoint status = %q, want %q", record.Status, status)
		}
	})
}

func terminalContextNode(ctx context.Context, input string, status CheckpointStatus, started chan<- struct{}) (string, error) {
	switch status {
	case CheckpointCompleted:
		return input, nil
	case CheckpointFailed:
		return "", errors.New("node failed")
	default:
		return waitTerminalContext(ctx, started)
	}
}

func waitTerminalContext(ctx context.Context, started chan<- struct{}) (string, error) {
	close(started)
	<-ctx.Done()
	return "", ctx.Err()
}

type terminalContextTrigger struct {
	t       *testing.T
	wf      *Workflow[string, string]
	status  CheckpointStatus
	started <-chan struct{}
	entered <-chan struct{}
	cancel  context.CancelFunc
}

func (trigger terminalContextTrigger) run() {
	trigger.t.Helper()
	switch trigger.status {
	case CheckpointCanceled:
		trigger.cancelRun()
	case CheckpointTerminated:
		trigger.terminateRun()
	default:
		trigger.cancelObserver()
	}
}

func (trigger terminalContextTrigger) cancelRun() {
	<-trigger.started
	trigger.cancel()
	<-trigger.entered
}

func (trigger terminalContextTrigger) terminateRun() {
	<-trigger.started
	if err := trigger.wf.Terminate("caller-context-run"); err != nil {
		trigger.t.Fatal(err)
	}
	<-trigger.entered
}

func (trigger terminalContextTrigger) cancelObserver() {
	<-trigger.entered
	trigger.cancel()
}

func TestInterruptObserverUsesCallerContextAndFreshResumeConverges(t *testing.T) {
	for _, multi := range []bool{false, true} {
		name := "single"
		if multi {
			name = "multi"
		}
		t.Run(name, func(t *testing.T) {
			testInterruptObserverCallerContext(t, multi)
		})
	}
}

func testInterruptObserverCallerContext(t *testing.T, multi bool) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		const renewEvery = 20 * time.Second
		store := &renewalCountingStore{MemoryStore: NewMemoryStore()}
		build := func() *Workflow[string, string] {
			return interruptContextWorkflow(store, multi)
		}
		entered := make(chan struct{})
		exited := make(chan struct{})
		callerCtx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := build().Invoke(callerCtx, "input", gopact.WithRunID("interrupt-context-run"), gopact.WithStrictEventHandler(func(ctx context.Context, event gopact.Event) error {
				if event.Type != EventGuardInterrupted {
					return nil
				}
				close(entered)
				<-ctx.Done()
				close(exited)
				return ctx.Err()
			}))
			done <- err
		}()
		<-entered
		cancel()
		<-exited
		if err := <-done; err == nil {
			t.Fatal("first Invoke() error = nil")
		}
		renews := store.renews.Load()
		time.Sleep(2 * renewEvery)
		synctest.Wait()
		if store.renews.Load() != renews {
			t.Fatal("heartbeat continued after strict interrupt observer cancellation")
		}
		_, err := build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "interrupt-context-run"}))
		var interrupted InterruptError
		if !errors.As(err, &interrupted) {
			t.Fatalf("resume Invoke() error = %v, want InterruptError", err)
		}
		record, err := store.Load(t.Context(), "interrupt-context-run")
		if err != nil {
			t.Fatal(err)
		}
		if record.Status != CheckpointInterrupted {
			t.Fatalf("checkpoint status = %q, want interrupted", record.Status)
		}
	})
}

func interruptContextWorkflow(store Store, multi bool) *Workflow[string, string] {
	wf := New[string, string]("interrupt-caller-context", WithStore(store), WithCheckpointLease(time.Minute, 20*time.Second), WithMaxParallelism(2))
	if !multi {
		node := interruptContextNode(wf, "node", "approval-1")
		wf.Entry(node)
		wf.Exit(node)
		return wf
	}
	plan := wf.Node("plan", func(_ context.Context, input string) (string, error) { return input, nil })
	first := interruptContextNode(wf, "first", "approval-first")
	second := interruptContextNode(wf, "second", "approval-second")
	merge := wf.Merge("merge", func(_ context.Context, _ Inputs) (string, error) { return "done", nil })
	plan.Route(func(_ context.Context, input string) (Dispatch, error) {
		return plan.Once(first, input).And(plan.Once(second, input)).WithSettle(SettleAll()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, first)
	wf.Edge(plan, second)
	wf.Edge(first, merge)
	wf.Edge(second, merge)
	wf.Exit(merge)
	return wf
}

func interruptContextNode(wf *Workflow[string, string], name, interruptID string) *Node[string, string] {
	node := wf.Node(name, func(_ context.Context, input string) (string, error) { return input, nil })
	node.Guard(BeforeRun("approval", GuardFunc[string, string](func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
		return GuardInterrupt[string, string]{Request: InterruptRequest{ID: interruptID}}, nil
	})))
	return node
}

func TestWorkflowLeaseLossCancelsNode(t *testing.T) {
	tests := []struct {
		name       string
		nodeResult func(context.Context) error
	}{
		{name: "context cancellation", nodeResult: func(ctx context.Context) error { return ctx.Err() }},
		{name: "ordinary error", nodeResult: func(context.Context) error { return errors.New("node failed") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := &leaseRejectingCheckpointer{MemoryCheckpointer: NewMemoryCheckpointer()}
				journal := runlog.NewMemoryLog()
				started := make(chan struct{})
				leaseCause := make(chan error, 1)
				release := make(chan struct{})
				wf := New[int, int](
					"lease-loss",
					WithStore(storeWithCheckpointerAndLog(store, journal)),
					WithCheckpointLease(time.Minute, 20*time.Second),
				)
				node := wf.Node("node", func(ctx context.Context, _ int) (int, error) {
					close(started)
					<-ctx.Done()
					leaseCause <- context.Cause(ctx)
					<-release
					return 0, test.nodeResult(ctx)
				})
				wf.Entry(node)
				wf.Exit(node)

				done := make(chan error, 1)
				go func() {
					_, err := wf.Invoke(context.Background(), 1, gopact.WithRunID("run-1"))
					done <- err
				}()
				<-started
				if cause := <-leaseCause; !errors.Is(cause, ErrCheckpointLeaseLost) {
					t.Fatalf("node context cause = %v, want ErrCheckpointLeaseLost", cause)
				}
				before, err := journal.List(t.Context(), runlog.Query{RunID: "run-1"})
				if err != nil {
					t.Fatalf("List() before release error = %v", err)
				}
				close(release)
				if err := <-done; !errors.Is(err, ErrCheckpointLeaseLost) {
					t.Fatalf("Invoke() error = %v, want ErrCheckpointLeaseLost", err)
				}
				after, err := journal.List(t.Context(), runlog.Query{RunID: "run-1"})
				if err != nil {
					t.Fatalf("List() after release error = %v", err)
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("records after lease loss = %+v, want unchanged %+v", after, before)
				}
			})
		})
	}
}

func TestWorkflowLeaseLossFencesCustomEventJournal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &leaseRejectingCheckpointer{MemoryCheckpointer: NewMemoryCheckpointer()}
		journal := runlog.NewMemoryLog()
		started := make(chan struct{})
		leaseCause := make(chan error, 1)
		release := make(chan struct{})
		emitResult := make(chan error, 1)
		wf := New[int, int](
			"lease-loss-custom-event",
			WithStore(storeWithCheckpointerAndLog(store, journal)),
			WithCheckpointLease(time.Minute, 20*time.Second),
		)
		node := wf.Node("node", func(ctx context.Context, _ int) (int, error) {
			close(started)
			<-ctx.Done()
			leaseCause <- context.Cause(ctx)
			<-release
			emitResult <- Emit(ctx, gopact.Event{Type: "audit.custom"})
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
		if cause := <-leaseCause; !errors.Is(cause, ErrCheckpointLeaseLost) {
			t.Fatalf("node context cause = %v, want ErrCheckpointLeaseLost", cause)
		}
		before, err := journal.List(t.Context(), runlog.Query{RunID: "run-1"})
		if err != nil {
			t.Fatalf("List() before release error = %v", err)
		}
		close(release)
		if err := <-emitResult; !errors.Is(err, ErrCheckpointLeaseLost) {
			t.Fatalf("Emit() error = %v, want ErrCheckpointLeaseLost", err)
		}
		if err := <-done; !errors.Is(err, ErrCheckpointLeaseLost) {
			t.Fatalf("Invoke() error = %v, want ErrCheckpointLeaseLost", err)
		}
		after, err := journal.List(t.Context(), runlog.Query{RunID: "run-1"})
		if err != nil {
			t.Fatalf("List() after release error = %v", err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("records after lease loss = %+v, want unchanged %+v", after, before)
		}
	})
}

func TestWorkflowNodeLeaseSentinelIsOrdinaryFailure(t *testing.T) {
	store := NewMemoryStore()
	wf := New[int, int](
		"node-lease-sentinel",
		WithStore(store),
	)
	node := wf.Node("node", func(context.Context, int) (int, error) {
		return 0, ErrCheckpointLeaseLost
	})
	wf.Entry(node)
	wf.Exit(node)
	_, err := wf.Invoke(t.Context(), 1, gopact.WithRunID("run-node-lease-sentinel"))
	if !errors.Is(err, ErrCheckpointLeaseLost) {
		t.Fatalf("Invoke() error = %v, want node error preserved", err)
	}
	checkpoint, err := store.Load(t.Context(), "run-node-lease-sentinel")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if checkpoint.Status != CheckpointFailed {
		t.Fatalf("checkpoint status = %q, want %q", checkpoint.Status, CheckpointFailed)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: "run-node-lease-sentinel"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) == 0 || records[len(records)-1].EventType != EventWorkflowFailed {
		t.Fatalf("records = %+v, want workflow.failed terminal event", records)
	}
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

func (store *leaseRejectingCheckpointer) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	return store.MemoryCheckpointer.Save(context.WithoutCancel(ctx), record, version)
}

func (store *leaseRejectingCheckpointer) RenewLease(context.Context, CheckpointLease) error {
	return ErrCheckpointConflict
}
