package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestRowTurnLoopStorePersistsStateAcrossInstances(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopRowBackend()
	store, err := NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}
	state := TurnLoopState{
		Pending: []TurnInputRecord{
			{
				ID:    "turn-input:1",
				Kind:  TurnInputUser,
				Input: "queued",
				IDs:   RuntimeIDs{ThreadID: "thread-1"},
			},
		},
		Interrupted: &TurnInputRecord{
			ID:    "turn-input:2",
			Kind:  TurnInputUser,
			Input: "question",
		},
		InputSeq: 2,
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	restored, err := NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restored.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if got.InputSeq != 2 || len(got.Pending) != 1 || got.Pending[0].Input != "queued" {
		t.Fatalf("Load() = %+v, want queued pending state", got)
	}
	if got.Interrupted == nil || got.Interrupted.Input != "question" {
		t.Fatalf("Load().Interrupted = %+v, want question", got.Interrupted)
	}
}

func TestRowTurnLoopStoreReplacesStateForSameKey(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopRowBackend()
	store, err := NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	got, ok, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("Load() ok=%v state=%+v, want latest state", ok, got)
	}
}

func TestRowTurnLoopStoreRestoresTurnLoopPendingInput(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopRowBackend()
	store, err := NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	loop, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, store))
	if err != nil {
		t.Fatalf("NewTurnLoop() error = %v", err)
	}
	if _, err := loop.Push(ctx, "queued", WithTurnRuntimeIDs(RuntimeIDs{ThreadID: "thread-1"})); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	restoredStore, err := NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore(restored) error = %v", err)
	}
	restored, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, restoredStore))
	if err != nil {
		t.Fatalf("NewTurnLoop(restored) error = %v", err)
	}
	if pending := restored.Pending(); len(pending) != 1 || pending[0].Input != "queued" {
		t.Fatalf("restored Pending() = %+v, want queued input", pending)
	}
}

func TestRowTurnLoopStoreRejectsSchemaMismatch(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopRowBackend()
	if err := backend.UpsertTurnLoopState(ctx, TurnLoopRowRecord{
		Key:           "turns/main",
		SchemaVersion: "turnloop.v0",
	}); err != nil {
		t.Fatalf("UpsertTurnLoopState() error = %v", err)
	}
	store, err := NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}

	_, ok, err := store.Load(ctx)
	if !errors.Is(err, ErrTurnLoopStoreSchemaMismatch) || ok {
		t.Fatalf("Load() ok=%v err=%v, want schema mismatch", ok, err)
	}
}

func TestNewRowTurnLoopStoreRejectsNilBackend(t *testing.T) {
	store, err := NewRowTurnLoopStore(nil, "turns/main")
	if !errors.Is(err, ErrTurnLoopRowBackendRequired) || store != nil {
		t.Fatalf("NewRowTurnLoopStore(nil) store=%v err=%v, want ErrTurnLoopRowBackendRequired", store, err)
	}
}

func TestMemoryTurnLoopRowBackendCopiesRecords(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopRowBackend()
	record := TurnLoopRowRecord{
		Key:           "turns/main",
		SchemaVersion: turnLoopStoreSchemaVersion,
		State: TurnLoopState{
			Pending: []TurnInputRecord{{ID: "turn-input:1", Kind: TurnInputUser, Input: "original"}},
		},
	}
	if err := backend.UpsertTurnLoopState(ctx, record); err != nil {
		t.Fatalf("UpsertTurnLoopState() error = %v", err)
	}
	record.State.Pending[0].Input = "mutated"

	got, ok, err := backend.GetTurnLoopState(ctx, "turns/main")
	if err != nil {
		t.Fatalf("GetTurnLoopState() error = %v", err)
	}
	if !ok {
		t.Fatal("GetTurnLoopState() ok = false, want true")
	}
	if got.State.Pending[0].Input != "original" {
		t.Fatalf("stored input = %q, want original", got.State.Pending[0].Input)
	}
	got.State.Pending[0].Input = "mutated"

	again, ok, err := backend.GetTurnLoopState(ctx, "turns/main")
	if err != nil {
		t.Fatalf("GetTurnLoopState(again) error = %v", err)
	}
	if !ok || again.State.Pending[0].Input != "original" {
		t.Fatalf("GetTurnLoopState(again) ok=%v record=%+v, want original", ok, again)
	}
}
