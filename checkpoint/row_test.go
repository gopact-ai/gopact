package checkpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

func TestRowStorePersistsCheckpointsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryRowBackend()

	store, err := NewRowStore[string](
		backend,
		WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		ThreadID:  "thread-1",
		Step:      1,
		Node:      "first",
		State:     "one",
		CreatedAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-2",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		ThreadID:  "thread-1",
		Step:      2,
		Node:      "second",
		State:     "two",
		CreatedAt: time.Unix(2, 0),
	})
	if err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}

	restored, err := NewRowStore[string](
		backend,
		WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewRowStore(restored) error = %v", err)
	}
	latest, ok, err := restored.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.ID != "checkpoint-2" || latest.Step != 2 || latest.State != "two" || latest.ConfigVersion != "config:v1" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 step 2 state two config:v1", latest)
	}

	first, ok, err := restored.Get(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if first.State != "one" || first.Node != "first" {
		t.Fatalf("Get() = %+v, want first checkpoint", first)
	}

	list, err := restored.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() count = %d, want 2", len(list))
	}
	if list[0].ID != "checkpoint-1" || list[1].ID != "checkpoint-2" {
		t.Fatalf("List() ids = %q, %q; want created-time order", list[0].ID, list[1].ID)
	}
}

func TestRowStoreReplacesCheckpointWithSameID(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryRowBackend()
	store, err := NewRowStore[string](backend)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}

	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		ThreadID:  "thread-1",
		Step:      1,
		State:     "old",
		CreatedAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("Put(old) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		ThreadID:  "thread-1",
		Step:      2,
		State:     "new",
		CreatedAt: time.Unix(2, 0),
	})
	if err != nil {
		t.Fatalf("Put(new) error = %v", err)
	}

	list, err := store.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() count = %d, want 1", len(list))
	}
	if list[0].Step != 2 || list[0].State != "new" {
		t.Fatalf("List()[0] = %+v, want replacement checkpoint", list[0])
	}
}

func TestRowStoreVerifiesIntegrityOnRead(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryRowBackend()
	store, err := NewRowStore[string](backend)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
		Step:     1,
		State:    "original",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	record, ok, err := backend.GetRecord(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if !ok {
		t.Fatal("GetRecord() ok = false, want true")
	}
	record.State = []byte(`"tampered"`)
	if err := backend.UpsertRecord(ctx, record); err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}

	_, ok, err = store.Latest(ctx, "thread-1")
	if !errors.Is(err, ErrIntegrityMismatch) || ok {
		t.Fatalf("Latest() ok=%v err=%v, want integrity mismatch", ok, err)
	}
}

func TestRowStoreUsesSharedCodecOption(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryRowBackend()

	store, err := NewRowStore[string](
		backend,
		WithCodec[string](prefixedStringCodec{}),
	)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
		Step:     1,
		State:    "custom",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	record, ok, err := backend.GetRecord(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if !ok {
		t.Fatal("GetRecord() ok = false, want true")
	}
	if record.StateCodec != "prefixed-string" {
		t.Fatalf("StateCodec = %q, want prefixed-string", record.StateCodec)
	}

	restored, err := NewRowStore[string](
		backend,
		WithCodec[string](prefixedStringCodec{}),
	)
	if err != nil {
		t.Fatalf("NewRowStore(restored) error = %v", err)
	}
	latest, ok, err := restored.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.State != "custom" {
		t.Fatalf("Latest().State = %q, want custom", latest.State)
	}

	jsonStore, err := NewRowStore[string](backend)
	if err != nil {
		t.Fatalf("NewRowStore(json) error = %v", err)
	}
	if _, ok, err := jsonStore.Latest(ctx, "thread-1"); !errors.Is(err, ErrCodecMismatch) || ok {
		t.Fatalf("Latest(json) ok=%v err=%v, want codec mismatch", ok, err)
	}
}

func TestNewRowStoreRejectsNilBackend(t *testing.T) {
	store, err := NewRowStore[string](nil)
	if !errors.Is(err, ErrRowBackendRequired) || store != nil {
		t.Fatalf("NewRowStore(nil) store=%v err=%v, want ErrRowBackendRequired", store, err)
	}
}

func TestMemoryRowBackendCopiesRecords(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryRowBackend()
	record := Record{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
		State:    []byte(`"original"`),
		Queue:    []string{"next"},
		Metadata: map[string]any{"key": "value"},
	}

	if err := backend.UpsertRecord(ctx, record); err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}
	record.State[1] = 'x'
	record.Queue[0] = "mutated"
	record.Metadata["key"] = "mutated"

	got, ok, err := backend.GetRecord(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if !ok {
		t.Fatal("GetRecord() ok = false, want true")
	}
	if string(got.State) != `"original"` || got.Queue[0] != "next" || got.Metadata["key"] != "value" {
		t.Fatalf("GetRecord() = %+v, want copied original record", got)
	}
	got.State[1] = 'x'
	got.Queue[0] = "mutated"
	got.Metadata["key"] = "mutated"

	again, ok, err := backend.GetRecord(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("GetRecord(again) error = %v", err)
	}
	if !ok {
		t.Fatal("GetRecord(again) ok = false, want true")
	}
	if string(again.State) != `"original"` || again.Queue[0] != "next" || again.Metadata["key"] != "value" {
		t.Fatalf("GetRecord(again) = %+v, want copied original record", again)
	}

	list, err := backend.ListRecords(ctx, "thread-1")
	if err != nil {
		t.Fatalf("ListRecords() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "checkpoint-1" {
		t.Fatalf("ListRecords() = %+v, want one copied record", list)
	}
}
