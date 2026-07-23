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
			AfterVersion: 1 << 60,
		})
		if err != nil || len(history) != 0 {
			t.Fatalf("ListCheckpoints(after end) = %+v, %v, want empty history", history, err)
		}
	})

	t.Run("run log idempotence and conflict", func(t *testing.T) {
		store := requireConformanceStore(t, newStore)
		record := runlog.Record{
			SessionID: "store-conformance-session",
			RunID:     "store-conformance-log",
			Sequence:  1,
			EventType: "conformance.event",
			Source:    "gopacttest",
			Timestamp: time.Unix(1, 0).UTC(),
		}
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
		if err != nil || len(records) != 1 {
			t.Fatalf("List() = %+v, %v, want one record", records, err)
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

func storeConformanceWorkflow(
	store workflow.Store,
	bodyCalls *int,
	interrupt *bool,
) *workflow.Workflow[string, string] {
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
		workflow.GuardFunc[string, string](func(
			context.Context,
			workflow.GuardContext[string, string],
		) (workflow.GuardDecision[string, string], error) {
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
