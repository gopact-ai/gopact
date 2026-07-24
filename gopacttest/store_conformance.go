package gopacttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
	"github.com/gopact-ai/gopact/workflow"
)

const (
	afterAllCheckpointVersions     = int64(1 << 60)
	conformanceCheckpointSchema    = 2
	conformanceSecondEventSequence = 2
)

// RequireStoreConformance verifies portable workflow recovery and RunLog
// behavior shared by Store implementations. Each subtest requests a fresh Store.
func RequireStoreConformance(t *testing.T, newStore func(*testing.T) workflow.Store) {
	t.Helper()
	if newStore == nil {
		t.Fatal("store factory is nil")
	}

	t.Run("interrupt resume and snapshot", func(t *testing.T) {
		store := requireConformanceStore(t, newStore)
		bodyCalls := 0
		interrupt := true
		wf := storeConformanceWorkflow(store, &bodyCalls, &interrupt)

		_, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("store-conformance-run"))
		var interrupted workflow.InterruptError
		if !errors.As(err, &interrupted) {
			t.Fatalf("Invoke() error = %v, want InterruptError", err)
		}

		wf = storeConformanceWorkflow(store, &bodyCalls, &interrupt)
		output, err := wf.Invoke(t.Context(), "", workflow.WithResume(workflow.ResumeRequest{
			RunID:        "store-conformance-run",
			CheckpointID: interrupted.CheckpointID,
			Resolutions: []workflow.InterruptResolution{{
				InterruptID: "approval",
				PayloadRef:  "resolution://approved",
			}},
		}))
		if err != nil || output != "input-done" || bodyCalls != 1 {
			t.Fatalf("Resume() = %q, %v, calls %d; want input-done, nil, 1", output, err, bodyCalls)
		}

		snapshot, err := wf.Snapshot(t.Context(), workflow.SnapshotRequest{
			RunID: "store-conformance-run",
		})
		if err != nil || len(snapshot.Timeline) == 0 || len(snapshot.Checkpoints) == 0 {
			t.Fatalf("Snapshot() = %+v, %v, want timeline and checkpoints", snapshot, err)
		}
		history, err := store.ListCheckpoints(t.Context(), workflow.CheckpointHistoryRequest{
			RunID:        "store-conformance-run",
			AfterVersion: afterAllCheckpointVersions,
		})
		if err != nil || len(history) != 0 {
			t.Fatalf("ListCheckpoints(after end) = %+v, %v, want empty history", history, err)
		}
	})

	t.Run("lease fencing", func(t *testing.T) {
		store := requireConformanceStore(t, newStore)
		record := storeConformanceCheckpoint("store-conformance-lease")
		if err := store.Create(t.Context(), record); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		created, err := store.Load(t.Context(), record.RunID)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if err := store.RenewLease(t.Context(), workflow.CheckpointLease{
			RunID:         created.RunID,
			OwnerID:       created.OwnerID,
			ClaimSequence: created.ClaimSequence,
			ExpiresAt:     created.LeaseExpiresAt.Add(time.Hour),
		}); err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		renewed, err := store.Load(t.Context(), record.RunID)
		if err != nil {
			t.Fatalf("Load() after renewal error = %v", err)
		}
		if !renewed.LeaseExpiresAt.After(created.LeaseExpiresAt) {
			t.Fatalf("renewed expiry = %v, want after %v", renewed.LeaseExpiresAt, created.LeaseExpiresAt)
		}

		created.Payload = []byte(`{"state":"saved"}`)
		if err := store.Save(t.Context(), created, created.Version); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		saved, err := store.Load(t.Context(), record.RunID)
		if err != nil {
			t.Fatalf("Load() after Save error = %v", err)
		}
		if !saved.LeaseExpiresAt.Equal(renewed.LeaseExpiresAt) {
			t.Fatalf("Save() changed renewed expiry from %v to %v", renewed.LeaseExpiresAt, saved.LeaseExpiresAt)
		}

		staleOwner := saved
		staleOwner.OwnerID = "stale-owner"
		staleOwner.Version++
		if err := store.Save(t.Context(), staleOwner, staleOwner.Version); !errors.Is(err, workflow.ErrCheckpointLeaseLost) {
			t.Fatalf("Save(stale owner and version) error = %v, want ErrCheckpointLeaseLost", err)
		}
		staleVersion := saved
		staleVersion.Version++
		if err := store.Save(t.Context(), staleVersion, staleVersion.Version); !errors.Is(err, workflow.ErrCheckpointConflict) {
			t.Fatalf("Save(stale version) error = %v, want ErrCheckpointConflict", err)
		}
		if err := store.RenewLease(t.Context(), workflow.CheckpointLease{
			RunID:         saved.RunID,
			OwnerID:       saved.OwnerID,
			ClaimSequence: saved.ClaimSequence + 1,
			ExpiresAt:     saved.LeaseExpiresAt.Add(time.Hour),
		}); !errors.Is(err, workflow.ErrCheckpointLeaseLost) {
			t.Fatalf("RenewLease(stale claim) error = %v, want ErrCheckpointLeaseLost", err)
		}

		expired := storeConformanceCheckpoint("store-conformance-claim")
		expired.LeaseExpiresAt = time.Now().UTC().Add(-time.Hour)
		if err := store.Create(t.Context(), expired); err != nil {
			t.Fatalf("Create(expired claim) error = %v", err)
		}
		candidate := expired
		candidate.OwnerID = "store-conformance-new-owner"
		candidate.ClaimSequence++
		candidate.LeaseExpiresAt = time.Now().UTC().Add(time.Hour)
		if err := store.Claim(t.Context(), candidate, expired.Version); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		staleClaim := expired
		staleClaim.Version++
		if err := store.Save(t.Context(), staleClaim, staleClaim.Version); !errors.Is(err, workflow.ErrCheckpointLeaseLost) {
			t.Fatalf("Save(stale claim) error = %v, want ErrCheckpointLeaseLost", err)
		}
	})

	t.Run("run log idempotence and conflict", func(t *testing.T) {
		store := requireConformanceStore(t, newStore)
		record := storeConformanceRunLogRecord("store-conformance-log", 1)
		if err := store.Append(t.Context(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := store.Append(t.Context(), record); err != nil {
			t.Fatalf("idempotent Append() error = %v", err)
		}
		conflict := record
		conflict.Summary = "different"
		if err := store.Append(t.Context(), conflict); !errors.Is(err, runlog.ErrConflict) {
			t.Fatalf("conflicting Append() error = %v, want ErrConflict", err)
		}
		records, err := store.List(t.Context(), runlog.Query{RunID: record.RunID})
		if err != nil || len(records) != 1 || records[0].Summary != record.Summary {
			t.Fatalf("List() = %+v, %v, want one unchanged record", records, err)
		}
	})

	t.Run("fenced run log", func(t *testing.T) {
		store := requireConformanceStore(t, newStore)
		checkpoint := storeConformanceCheckpoint("store-conformance-fenced-log")
		if err := store.Create(t.Context(), checkpoint); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		current, err := store.Load(t.Context(), checkpoint.RunID)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		fence := runlog.Fence{OwnerID: current.OwnerID, ClaimSequence: current.ClaimSequence}
		record := storeConformanceRunLogRecord(current.RunID, 1)
		if err := store.AppendFenced(t.Context(), record, fence); err != nil {
			t.Fatalf("AppendFenced() error = %v", err)
		}
		if err := store.AppendFenced(t.Context(), record, fence); err != nil {
			t.Fatalf("idempotent AppendFenced() error = %v", err)
		}
		conflict := record
		conflict.Summary = "different"
		if err := store.AppendFenced(t.Context(), conflict, fence); !errors.Is(err, runlog.ErrConflict) {
			t.Fatalf("conflicting AppendFenced() error = %v, want ErrConflict", err)
		}

		next := storeConformanceRunLogRecord(current.RunID, conformanceSecondEventSequence)
		for _, stale := range []runlog.Fence{
			{OwnerID: "stale-owner", ClaimSequence: current.ClaimSequence},
			{OwnerID: current.OwnerID, ClaimSequence: current.ClaimSequence + 1},
		} {
			if err := store.AppendFenced(t.Context(), next, stale); !errors.Is(err, workflow.ErrCheckpointLeaseLost) {
				t.Fatalf("AppendFenced(stale fence %+v) error = %v, want ErrCheckpointLeaseLost", stale, err)
			}
		}

		expired := storeConformanceCheckpoint("store-conformance-expired-log")
		expired.LeaseExpiresAt = time.Now().UTC().Add(-time.Hour)
		if err := store.Create(t.Context(), expired); err != nil {
			t.Fatalf("Create(expired fence) error = %v", err)
		}
		expiredRecord := storeConformanceRunLogRecord(expired.RunID, 1)
		expiredFence := runlog.Fence{OwnerID: expired.OwnerID, ClaimSequence: expired.ClaimSequence}
		if err := store.AppendFenced(t.Context(), expiredRecord, expiredFence); !errors.Is(err, workflow.ErrCheckpointLeaseLost) {
			t.Fatalf("AppendFenced(expired fence) error = %v, want ErrCheckpointLeaseLost", err)
		}
		expiredRecords, err := store.List(t.Context(), runlog.Query{RunID: expired.RunID})
		if err != nil || len(expiredRecords) != 0 {
			t.Fatalf("List(expired fence) = %+v, %v, want no record", expiredRecords, err)
		}

		current.Status = workflow.CheckpointCompleted
		current.OwnerID = ""
		current.LeaseExpiresAt = time.Time{}
		if err := store.Finish(t.Context(), current, current.Version); err != nil {
			t.Fatalf("Finish() error = %v", err)
		}
		if err := store.AppendFenced(t.Context(), next, fence); !errors.Is(err, workflow.ErrCheckpointLeaseLost) {
			t.Fatalf("AppendFenced(after Finish) error = %v, want ErrCheckpointLeaseLost", err)
		}
		records, err := store.List(t.Context(), runlog.Query{RunID: current.RunID})
		if err != nil || len(records) != 1 ||
			records[0].Sequence != record.Sequence || records[0].Summary != record.Summary {
			t.Fatalf("List() = %+v, %v, want one unchanged fenced record", records, err)
		}
	})
}

func requireConformanceStore(t *testing.T, newStore func(*testing.T) workflow.Store) workflow.Store {
	t.Helper()
	store := newStore(t)
	if isNilConformanceValue(store) {
		t.Fatal("store factory returned nil")
	}
	return store
}

func storeConformanceWorkflow(store workflow.Store, bodyCalls *int, interrupt *bool) *workflow.Workflow[string, string] {
	wf := workflow.New[string, string](
		"store-conformance",
		workflow.WithTopologyVersion("v1"),
		workflow.WithStore(store),
	)
	work := wf.Node("work", func(_ context.Context, input string) (string, error) {
		*bodyCalls++
		return input + "-done", nil
	})
	work.Guard(workflow.BeforeRun(
		"approval",
		workflow.GuardFunc[string, string](func(context.Context, workflow.GuardContext[string, string]) (workflow.GuardDecision[string, string], error) {
			if !*interrupt {
				return workflow.GuardAllow[string, string]{}, nil
			}
			*interrupt = false
			return workflow.GuardInterrupt[string, string]{
				Request: workflow.InterruptRequest{ID: "approval"},
			}, nil
		}),
	))
	wf.Entry(work)
	wf.Exit(work)
	return wf
}

func storeConformanceCheckpoint(runID string) workflow.CheckpointRecord {
	now := time.Now().UTC()
	return workflow.CheckpointRecord{
		ID:              "checkpoint:" + runID,
		SessionID:       "store-conformance-session",
		RunID:           runID,
		WorkflowName:    "store-conformance",
		TopologyVersion: "v1",
		SchemaVersion:   conformanceCheckpointSchema,
		Version:         1,
		Status:          workflow.CheckpointRunning,
		Payload:         []byte(`{"state":"running"}`),
		ReplayStatus:    workflow.ReplayUnknown,
		OwnerID:         "store-conformance-owner",
		ClaimSequence:   1,
		LeaseExpiresAt:  now.Add(time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func storeConformanceRunLogRecord(runID string, sequence int64) runlog.Record {
	return runlog.Record{
		SessionID: "store-conformance-session",
		RunID:     runID,
		Sequence:  sequence,
		EventType: "conformance.event",
		Source:    "gopacttest",
		Timestamp: time.Unix(1, 0).UTC(),
	}
}
