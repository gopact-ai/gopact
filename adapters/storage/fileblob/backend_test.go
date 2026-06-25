package fileblob

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsCheckpointObjectsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	backend, err := NewBackend(root)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewObjectStore[string](
		backend,
		checkpoint.WithObjectPrefix[string]("agent/prod"),
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		IDs:       gopact.RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
		ThreadID:  "thread-1",
		Step:      1,
		Node:      "first",
		State:     "one",
		CreatedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-2",
		IDs:       gopact.RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
		ThreadID:  "thread-1",
		Step:      2,
		Node:      "second",
		State:     "two",
		CreatedAt: time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}

	restoredBackend, err := NewBackend(root)
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restored, err := checkpoint.NewObjectStore[string](
		restoredBackend,
		checkpoint.WithObjectPrefix[string]("agent/prod"),
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewObjectStore(restored) error = %v", err)
	}
	latest, ok, err := restored.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.ID != "checkpoint-2" || latest.State != "two" || latest.ConfigVersion != "config:v1" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 state two config:v1", latest)
	}

	list, err := restored.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "checkpoint-1" || list[1].ID != "checkpoint-2" {
		t.Fatalf("List() = %+v, want two checkpoints in created order", list)
	}
}

func TestBackendPersistsTurnLoopBlobsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	backend, err := NewBackend(root)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := gopact.NewBlobTurnLoopStore(backend, "turns/main.json")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore() error = %v", err)
	}
	state := gopact.TurnLoopState{
		Pending: []gopact.TurnInputRecord{
			{ID: "turn-input:1", Kind: gopact.TurnInputUser, Input: "queued"},
		},
		Interrupted: &gopact.TurnInputRecord{ID: "turn-input:2", Input: "question"},
		InputSeq:    2,
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	restoredBackend, err := NewBackend(root)
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restored, err := gopact.NewBlobTurnLoopStore(restoredBackend, "turns/main.json")
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

func TestBackendReturnsTypedNotFound(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if _, err := backend.GetObject(ctx, "missing/object.json"); !errors.Is(err, checkpoint.ErrObjectNotFound) {
		t.Fatalf("GetObject(missing) error = %v, want ErrObjectNotFound", err)
	}
	if _, err := backend.GetBlob(ctx, "missing/blob.json"); !errors.Is(err, gopact.ErrTurnLoopBlobNotFound) {
		t.Fatalf("GetBlob(missing) error = %v, want ErrTurnLoopBlobNotFound", err)
	}
}

func TestBackendRejectsUnsafeKeys(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if err := backend.PutObject(ctx, "../escape.json", []byte("bad")); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("PutObject(escape) error = %v, want ErrUnsafeKey", err)
	}
	if err := backend.PutBlob(ctx, "turns/../../escape.json", []byte("bad")); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("PutBlob(escape) error = %v, want ErrUnsafeKey", err)
	}
}

func TestNewBackendRejectsEmptyRoot(t *testing.T) {
	backend, err := NewBackend("")
	if !errors.Is(err, ErrRootRequired) || backend != nil {
		t.Fatalf("NewBackend(empty) backend=%v err=%v, want ErrRootRequired", backend, err)
	}
}
