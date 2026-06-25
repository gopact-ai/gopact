package sqlstore

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestVersionedBackendPersistsTurnLoopStateWithCAS(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	backend, err := NewVersionedBackend(db)
	if err != nil {
		t.Fatalf("NewVersionedBackend() error = %v", err)
	}
	store, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore() error = %v", err)
	}
	if _, ok, err := store.Load(ctx); err != nil || ok {
		t.Fatalf("Load(empty) ok=%v err=%v, want empty store", ok, err)
	}
	if err := store.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := store.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	restoredBackend, err := NewVersionedBackend(db)
	if err != nil {
		t.Fatalf("NewVersionedBackend(restored) error = %v", err)
	}
	restored, err := gopact.NewVersionedTurnLoopStore(restoredBackend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restored.Load(ctx)
	if err != nil {
		t.Fatalf("Load(restored) error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("Load(restored) ok=%v state=%+v, want latest state", ok, got)
	}
}

func TestVersionedBackendDetectsStaleSaveAcrossStores(t *testing.T) {
	ctx := context.Background()
	backend, err := NewVersionedBackend(openTestDB(t))
	if err != nil {
		t.Fatalf("NewVersionedBackend() error = %v", err)
	}
	first, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(first) error = %v", err)
	}
	second, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(second) error = %v", err)
	}
	if _, ok, err := first.Load(ctx); err != nil || ok {
		t.Fatalf("first Load() ok=%v err=%v, want empty store", ok, err)
	}
	if _, ok, err := second.Load(ctx); err != nil || ok {
		t.Fatalf("second Load() ok=%v err=%v, want empty store", ok, err)
	}
	if err := first.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := second.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); !errors.Is(err, gopact.ErrTurnLoopStoreConflict) {
		t.Fatalf("second Save() error = %v, want ErrTurnLoopStoreConflict", err)
	}

	got, ok, err := first.Load(ctx)
	if err != nil {
		t.Fatalf("Load(after conflict) error = %v", err)
	}
	if !ok || got.InputSeq != 1 {
		t.Fatalf("Load(after conflict) ok=%v state=%+v, want first state", ok, got)
	}
}

func TestVersionedBackendReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	backend, err := NewVersionedBackend(openTestDB(t))
	if err != nil {
		t.Fatalf("NewVersionedBackend() error = %v", err)
	}

	record, ok, err := backend.GetTurnLoopVersionedState(ctx, "missing")
	if err != nil {
		t.Fatalf("GetTurnLoopVersionedState() error = %v", err)
	}
	if ok || record.Key != "" || record.Version != "" {
		t.Fatalf("GetTurnLoopVersionedState() = %+v, %v; want zero false", record, ok)
	}
}

func TestNewVersionedBackendRejectsNilDB(t *testing.T) {
	backend, err := NewVersionedBackend(nil)
	if !errors.Is(err, ErrDBRequired) || backend != nil {
		t.Fatalf("NewVersionedBackend(nil) backend=%v err=%v, want ErrDBRequired", backend, err)
	}
}

func TestDefaultVersionedQueriesRejectsUnsafeTableName(t *testing.T) {
	queries, err := DefaultVersionedQueries("gopact_turnloop_states; drop table users", DialectSQLite)
	if err == nil || queries != (VersionedQueries{}) {
		t.Fatalf("DefaultVersionedQueries(unsafe) queries=%+v err=%v, want error", queries, err)
	}
}
