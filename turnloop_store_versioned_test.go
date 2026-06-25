package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestVersionedTurnLoopStoreDetectsStaleSaveAcrossInstances(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopVersionedBackend()
	first, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(first) error = %v", err)
	}
	second, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(second) error = %v", err)
	}

	if _, ok, err := first.Load(ctx); err != nil || ok {
		t.Fatalf("first Load() ok=%v err=%v, want empty store", ok, err)
	}
	if _, ok, err := second.Load(ctx); err != nil || ok {
		t.Fatalf("second Load() ok=%v err=%v, want empty store", ok, err)
	}
	if err := first.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := second.Save(ctx, TurnLoopState{InputSeq: 2}); !errors.Is(err, ErrTurnLoopStoreConflict) {
		t.Fatalf("second Save() error = %v, want ErrTurnLoopStoreConflict", err)
	}

	restored, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restored.Load(ctx)
	if err != nil {
		t.Fatalf("restored Load() error = %v", err)
	}
	if !ok || got.InputSeq != 1 {
		t.Fatalf("restored Load() ok=%v state=%+v, want first saved state", ok, got)
	}
}

func TestVersionedTurnLoopStoreUpdatesExpectedVersionAfterSave(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopVersionedBackend()
	store, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore() error = %v", err)
	}
	if _, ok, err := store.Load(ctx); err != nil || ok {
		t.Fatalf("Load() ok=%v err=%v, want empty store", ok, err)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	got, ok, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load(after saves) error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("Load(after saves) ok=%v state=%+v, want latest state", ok, got)
	}
}

func TestVersionedTurnLoopStorePropagatesPushConflict(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopVersionedBackend()
	firstStore, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(first) error = %v", err)
	}
	secondStore, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(second) error = %v", err)
	}
	runner, err := NewRunner(fakeTurnRunnable{events: []Event{{Type: EventRunCompleted}}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	firstLoop, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, firstStore))
	if err != nil {
		t.Fatalf("NewTurnLoop(first) error = %v", err)
	}
	secondLoop, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, secondStore))
	if err != nil {
		t.Fatalf("NewTurnLoop(second) error = %v", err)
	}

	if _, err := firstLoop.Push(ctx, "first"); err != nil {
		t.Fatalf("first Push() error = %v", err)
	}
	if _, err := secondLoop.Push(ctx, "second"); !errors.Is(err, ErrTurnLoopStoreConflict) {
		t.Fatalf("second Push() error = %v, want ErrTurnLoopStoreConflict", err)
	}
}

func TestVersionedTurnLoopStoreRejectsSchemaMismatch(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopVersionedBackend()
	if _, err := backend.CompareAndSwapTurnLoopState(ctx, TurnLoopVersionedRecord{
		Key:           "turns/main",
		SchemaVersion: "turnloop.v0",
		State:         TurnLoopState{InputSeq: 1},
	}, ""); err != nil {
		t.Fatalf("CompareAndSwapTurnLoopState() error = %v", err)
	}
	store, err := NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore() error = %v", err)
	}

	_, ok, err := store.Load(ctx)
	if !errors.Is(err, ErrTurnLoopStoreSchemaMismatch) || ok {
		t.Fatalf("Load() ok=%v err=%v, want schema mismatch", ok, err)
	}
}

func TestNewVersionedTurnLoopStoreRejectsNilBackend(t *testing.T) {
	store, err := NewVersionedTurnLoopStore(nil, "turns/main")
	if !errors.Is(err, ErrTurnLoopVersionedBackendRequired) || store != nil {
		t.Fatalf("NewVersionedTurnLoopStore(nil) store=%v err=%v, want ErrTurnLoopVersionedBackendRequired", store, err)
	}
}
