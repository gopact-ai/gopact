package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

func TestObjectStorePersistsCheckpointsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryObjectBackend()

	store, err := NewObjectStore[string](
		backend,
		WithObjectPrefix[string]("agent/prod"),
		WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint:1/with path",
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

	restored, err := NewObjectStore[string](
		backend,
		WithObjectPrefix[string]("agent/prod"),
		WithConfigVersion[string]("config:v1"),
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
	if latest.ID != "checkpoint-2" || latest.Step != 2 || latest.State != "two" || latest.ConfigVersion != "config:v1" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 step 2 state two config:v1", latest)
	}

	first, ok, err := restored.Get(ctx, "checkpoint:1/with path")
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
	if list[0].ID != "checkpoint:1/with path" || list[1].ID != "checkpoint-2" {
		t.Fatalf("List() ids = %q, %q; want insertion order by created time", list[0].ID, list[1].ID)
	}
}

func TestObjectStoreReplacesCheckpointWithSameID(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryObjectBackend()
	store, err := NewObjectStore[string](backend)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
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

func TestObjectStoreVerifiesIntegrityOnRead(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryObjectBackend()
	store, err := NewObjectStore[string](backend, WithObjectPrefix[string]("prod"))
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
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

	infos, err := backend.ListObjects(ctx, "prod/records/")
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("ListObjects() count = %d, want 1", len(infos))
	}
	raw, err := backend.GetObject(ctx, infos[0].Key)
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	record.State = []byte(`"tampered"`)
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := backend.PutObject(ctx, infos[0].Key, raw); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}

	_, ok, err := store.Latest(ctx, "thread-1")
	if !errors.Is(err, ErrIntegrityMismatch) || ok {
		t.Fatalf("Latest() ok=%v err=%v, want integrity mismatch", ok, err)
	}
}

func TestObjectStoreUsesSharedCodecOption(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryObjectBackend()

	store, err := NewObjectStore[string](
		backend,
		WithObjectPrefix[string]("codec"),
		WithCodec[string](prefixedStringCodec{}),
	)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
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

	infos, err := backend.ListObjects(ctx, "codec/records/")
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("ListObjects() count = %d, want 1", len(infos))
	}
	raw, err := backend.GetObject(ctx, infos[0].Key)
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record.StateCodec != "prefixed-string" {
		t.Fatalf("StateCodec = %q, want prefixed-string", record.StateCodec)
	}

	restored, err := NewObjectStore[string](
		backend,
		WithObjectPrefix[string]("codec"),
		WithCodec[string](prefixedStringCodec{}),
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
	if latest.State != "custom" {
		t.Fatalf("Latest().State = %q, want custom", latest.State)
	}

	jsonStore, err := NewObjectStore[string](backend, WithObjectPrefix[string]("codec"))
	if err != nil {
		t.Fatalf("NewObjectStore(json) error = %v", err)
	}
	if _, ok, err := jsonStore.Latest(ctx, "thread-1"); !errors.Is(err, ErrCodecMismatch) || ok {
		t.Fatalf("Latest(json) ok=%v err=%v, want codec mismatch", ok, err)
	}
}

func TestNewObjectStoreRejectsNilBackend(t *testing.T) {
	store, err := NewObjectStore[string](nil)
	if !errors.Is(err, ErrObjectBackendRequired) || store != nil {
		t.Fatalf("NewObjectStore(nil) store=%v err=%v, want ErrObjectBackendRequired", store, err)
	}
}

func TestMemoryObjectBackendCopiesData(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryObjectBackend()
	data := []byte("original")

	if err := backend.PutObject(ctx, "records/a.json", data); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	data[0] = 'x'

	got, err := backend.GetObject(ctx, "records/a.json")
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("GetObject() = %q, want original", got)
	}
	got[0] = 'x'

	again, err := backend.GetObject(ctx, "records/a.json")
	if err != nil {
		t.Fatalf("GetObject(again) error = %v", err)
	}
	if string(again) != "original" {
		t.Fatalf("GetObject(again) = %q, want original", again)
	}

	infos, err := backend.ListObjects(ctx, "records/")
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
	if len(infos) != 1 || infos[0].Key != "records/a.json" {
		t.Fatalf("ListObjects() = %+v, want one object", infos)
	}
}
