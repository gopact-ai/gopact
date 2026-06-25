package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestBlobTurnLoopStorePersistsStateAcrossInstances(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopBlobBackend()
	store, err := NewBlobTurnLoopStore(backend, "threads/thread-1/turnloop.json")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore() error = %v", err)
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

	restored, err := NewBlobTurnLoopStore(backend, "threads/thread-1/turnloop.json")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore(restored) error = %v", err)
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

func TestBlobTurnLoopStoreRestoresTurnLoopPendingInput(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopBlobBackend()
	store, err := NewBlobTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore() error = %v", err)
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

	restoredStore, err := NewBlobTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore(restored) error = %v", err)
	}
	restored, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, restoredStore))
	if err != nil {
		t.Fatalf("NewTurnLoop(restored) error = %v", err)
	}
	if pending := restored.Pending(); len(pending) != 1 || pending[0].Input != "queued" {
		t.Fatalf("restored Pending() = %+v, want queued input", pending)
	}
}

func TestBlobTurnLoopStoreRejectsSchemaMismatch(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopBlobBackend()
	if err := backend.PutBlob(ctx, "turns/main", []byte(`{"schema_version":"turnloop.v0"}`)); err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	store, err := NewBlobTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore() error = %v", err)
	}

	_, ok, err := store.Load(ctx)
	if !errors.Is(err, ErrTurnLoopStoreSchemaMismatch) || ok {
		t.Fatalf("Load() ok=%v err=%v, want schema mismatch", ok, err)
	}
}

func TestNewBlobTurnLoopStoreRejectsNilBackend(t *testing.T) {
	store, err := NewBlobTurnLoopStore(nil, "turns/main")
	if !errors.Is(err, ErrTurnLoopBlobBackendRequired) || store != nil {
		t.Fatalf("NewBlobTurnLoopStore(nil) store=%v err=%v, want ErrTurnLoopBlobBackendRequired", store, err)
	}
}

func TestMemoryTurnLoopBlobBackendCopiesData(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryTurnLoopBlobBackend()
	data := []byte("original")

	if err := backend.PutBlob(ctx, "turns/main", data); err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	data[0] = 'x'

	got, err := backend.GetBlob(ctx, "turns/main")
	if err != nil {
		t.Fatalf("GetBlob() error = %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("GetBlob() = %q, want original", got)
	}
	got[0] = 'x'

	again, err := backend.GetBlob(ctx, "turns/main")
	if err != nil {
		t.Fatalf("GetBlob(again) error = %v", err)
	}
	if string(again) != "original" {
		t.Fatalf("GetBlob(again) = %q, want original", again)
	}

	if _, err := backend.GetBlob(ctx, "missing"); !errors.Is(err, ErrTurnLoopBlobNotFound) {
		t.Fatalf("GetBlob(missing) error = %v, want ErrTurnLoopBlobNotFound", err)
	}
}
