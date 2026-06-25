package gopact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTurnLoopStorePersistsStateAcrossInstances(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "turnloop.json")

	store, err := NewFileTurnLoopStore(path)
	if err != nil {
		t.Fatalf("NewFileTurnLoopStore() error = %v", err)
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

	restored, err := NewFileTurnLoopStore(path)
	if err != nil {
		t.Fatalf("NewFileTurnLoopStore(restored) error = %v", err)
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

func TestFileTurnLoopStoreRestoresTurnLoopPendingInput(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "turnloop.json")
	store, err := NewFileTurnLoopStore(path)
	if err != nil {
		t.Fatalf("NewFileTurnLoopStore() error = %v", err)
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

	restoredStore, err := NewFileTurnLoopStore(path)
	if err != nil {
		t.Fatalf("NewFileTurnLoopStore(restored) error = %v", err)
	}
	restored, err := NewTurnLoop(runner, WithTurnLoopStore(ctx, restoredStore))
	if err != nil {
		t.Fatalf("NewTurnLoop(restored) error = %v", err)
	}
	if pending := restored.Pending(); len(pending) != 1 || pending[0].Input != "queued" {
		t.Fatalf("restored Pending() = %+v, want queued input", pending)
	}
}

func TestFileTurnLoopStoreRejectsSchemaMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turnloop.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"turnloop.v0"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store, err := NewFileTurnLoopStore(path)
	if err != nil {
		t.Fatalf("NewFileTurnLoopStore() error = %v", err)
	}

	_, ok, err := store.Load(context.Background())
	if !errors.Is(err, ErrTurnLoopStoreSchemaMismatch) || ok {
		t.Fatalf("Load() ok=%v err=%v, want schema mismatch", ok, err)
	}
}
