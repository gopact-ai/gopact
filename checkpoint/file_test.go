package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

func TestFileStorePersistsCheckpointsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "checkpoints.json")

	store, err := NewFileStore[string](path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		IDs:      gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		ThreadID: "thread-1",
		Step:     1,
		Node:     "first",
		State:    "one",
	})
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-2",
		IDs:      gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		ThreadID: "thread-1",
		Step:     2,
		Node:     "second",
		State:    "two",
	})
	if err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}

	restored, err := NewFileStore[string](path)
	if err != nil {
		t.Fatalf("NewFileStore(restored) error = %v", err)
	}
	latest, ok, err := restored.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.ID != "checkpoint-2" || latest.Step != 2 || latest.State != "two" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 step 2 state two", latest)
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
}

func TestFileStoreVerifiesIntegrityOnRead(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "checkpoints.json")

	store, err := NewFileStore[string](path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
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

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var stored fileStoreData
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	stored.Records[0].State = []byte(`"tampered"`)
	data, err = json.Marshal(stored)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, ok, err := store.Latest(ctx, "thread-1")
	if !errors.Is(err, ErrIntegrityMismatch) || ok {
		t.Fatalf("Latest() ok=%v err=%v, want integrity mismatch", ok, err)
	}
}

func TestFileStoreReplacesCheckpointWithSameID(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "checkpoints.json")

	store, err := NewFileStore[string](path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
		Step:     1,
		State:    "old",
	})
	if err != nil {
		t.Fatalf("Put(old) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
		Step:     2,
		State:    "new",
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

func TestFileStoreUsesSharedCodecOption(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "checkpoints.json")

	store, err := NewFileStore[string](path, WithCodec[string](prefixedStringCodec{}))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
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

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var stored fileStoreData
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if stored.Records[0].StateCodec != "prefixed-string" {
		t.Fatalf("StateCodec = %q, want prefixed-string", stored.Records[0].StateCodec)
	}

	restored, err := NewFileStore[string](path, WithCodec[string](prefixedStringCodec{}))
	if err != nil {
		t.Fatalf("NewFileStore(restored) error = %v", err)
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

	jsonStore, err := NewFileStore[string](path)
	if err != nil {
		t.Fatalf("NewFileStore(json) error = %v", err)
	}
	if _, ok, err := jsonStore.Latest(ctx, "thread-1"); !errors.Is(err, ErrCodecMismatch) || ok {
		t.Fatalf("Latest(json) ok=%v err=%v, want codec mismatch", ok, err)
	}
}

type prefixedStringCodec struct{}

func (prefixedStringCodec) Name() string {
	return "prefixed-string"
}

func (prefixedStringCodec) Marshal(state string) ([]byte, error) {
	return []byte("state:" + state), nil
}

func (prefixedStringCodec) Unmarshal(data []byte) (string, error) {
	return strings.TrimPrefix(string(data), "state:"), nil
}
