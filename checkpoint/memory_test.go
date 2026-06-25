package checkpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

func TestMemoryStoresCheckpointsByThread(t *testing.T) {
	ctx := context.Background()
	store := NewMemory[string]()

	err := store.Put(ctx, graph.Checkpoint[string]{ThreadID: "a", Step: 1, Node: "first", State: "one"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{ThreadID: "b", Step: 1, Node: "other", State: "ignored"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{ThreadID: "a", Step: 2, Node: "second", State: "two"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got := store.List(ctx, "a")
	if len(got) != 2 {
		t.Fatalf("List() count = %d, want 2", len(got))
	}
	if got[1].State != "two" {
		t.Fatalf("latest state = %q, want two", got[1].State)
	}

	got[0].State = "mutated"
	again := store.List(ctx, "a")
	if again[0].State != "one" {
		t.Fatalf("List() returned mutable backing storage")
	}
}

func TestMemoryLatestReturnsMostRecentCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := NewMemory[int]()

	if _, ok, err := store.Latest(ctx, "missing"); err != nil || ok {
		t.Fatal("Latest() ok = true, want false")
	}

	_ = store.Put(ctx, graph.Checkpoint[int]{ThreadID: "thread", Step: 1, State: 10})
	_ = store.Put(ctx, graph.Checkpoint[int]{ThreadID: "thread", Step: 2, State: 20})

	got, ok, err := store.Latest(ctx, "thread")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got.Step != 2 || got.State != 20 {
		t.Fatalf("Latest() = %+v, want step 2 state 20", got)
	}
}

func TestMemoryGetReturnsCheckpointByID(t *testing.T) {
	ctx := context.Background()
	store := NewMemory[string]()

	err := store.Put(ctx, graph.Checkpoint[string]{ID: "checkpoint-1", ThreadID: "thread", Step: 1, State: "one"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok, err := store.Get(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.State != "one" || got.ID != "checkpoint-1" {
		t.Fatalf("Get() = %+v, want checkpoint-1 state one", got)
	}

	if _, ok, err := store.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) ok=%v err=%v, want false nil", ok, err)
	}
}

func TestEncodeDecodeCheckpointVerifiesIntegrity(t *testing.T) {
	checkpoint := graph.Checkpoint[string]{
		ID:            "checkpoint-1",
		IDs:           gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		ThreadID:      "thread-1",
		Step:          2,
		Node:          "write",
		Phase:         gopact.StepCompleted,
		State:         "state",
		Queue:         []string{"next"},
		ConfigVersion: "config:v1",
	}

	record, err := EncodeCheckpoint(checkpoint, JSONCodec[string]{})
	if err != nil {
		t.Fatalf("EncodeCheckpoint() error = %v", err)
	}
	if record.StateCodec != "json" || record.StateHash == "" || len(record.State) == 0 {
		t.Fatalf("record codec/hash/state = %q/%q/%q", record.StateCodec, record.StateHash, record.State)
	}
	if record.ConfigVersion != "config:v1" {
		t.Fatalf("record config version = %q, want config:v1", record.ConfigVersion)
	}

	decoded, err := DecodeCheckpoint[string](record, JSONCodec[string]{})
	if err != nil {
		t.Fatalf("DecodeCheckpoint() error = %v", err)
	}
	if decoded.State != "state" || decoded.ThreadID != "thread-1" || decoded.ID != "checkpoint-1" || decoded.ConfigVersion != "config:v1" {
		t.Fatalf("decoded checkpoint = %+v", decoded)
	}

	record.State = []byte(`"tampered"`)
	if _, err := DecodeCheckpoint[string](record, JSONCodec[string]{}); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("DecodeCheckpoint() error = %v, want ErrIntegrityMismatch", err)
	}
}

func TestDecodeCheckpointRejectsConfigDriftByDefault(t *testing.T) {
	record, err := EncodeCheckpoint(graph.Checkpoint[string]{
		ID:            "checkpoint-1",
		ThreadID:      "thread-1",
		Step:          1,
		Node:          "write",
		State:         "state",
		ConfigVersion: "config:v1",
	}, JSONCodec[string]{})
	if err != nil {
		t.Fatalf("EncodeCheckpoint() error = %v", err)
	}

	_, err = DecodeCheckpoint[string](
		record,
		JSONCodec[string]{},
		WithCurrentConfigVersion[string]("config:v2"),
	)
	if !errors.Is(err, ErrConfigDrift) {
		t.Fatalf("DecodeCheckpoint() error = %v, want ErrConfigDrift", err)
	}
}

func TestDecodeCheckpointAllowsConfigDriftAndAnnotatesMetadata(t *testing.T) {
	record, err := EncodeCheckpoint(graph.Checkpoint[string]{
		ID:            "checkpoint-1",
		ThreadID:      "thread-1",
		Step:          1,
		Node:          "write",
		State:         "state",
		ConfigVersion: "config:v1",
	}, JSONCodec[string]{})
	if err != nil {
		t.Fatalf("EncodeCheckpoint() error = %v", err)
	}

	decoded, err := DecodeCheckpoint[string](
		record,
		JSONCodec[string]{},
		WithCurrentConfigVersion[string]("config:v2"),
		WithConfigDriftPolicy[string](ConfigDriftAllow),
	)
	if err != nil {
		t.Fatalf("DecodeCheckpoint() error = %v", err)
	}

	if decoded.ConfigVersion != "config:v1" {
		t.Fatalf("decoded config version = %q, want config:v1", decoded.ConfigVersion)
	}
	drift, ok := decoded.Metadata[MetadataConfigDrift].(ConfigDrift)
	if !ok {
		t.Fatalf("config drift metadata = %#v, want ConfigDrift", decoded.Metadata[MetadataConfigDrift])
	}
	if drift.StoredVersion != "config:v1" || drift.CurrentVersion != "config:v2" {
		t.Fatalf("config drift = %+v, want stored v1 current v2", drift)
	}
}

func TestDecodeCheckpointAppliesRecordMigration(t *testing.T) {
	record := Record{
		ID:            "checkpoint-1",
		SchemaVersion: "checkpoint.v0",
		ThreadID:      "thread-1",
		Step:          1,
		Node:          "write",
		Phase:         gopact.StepCompleted,
		State:         []byte(`"old"`),
		StateCodec:    "json",
		StateHash:     hashState([]byte(`"old"`)),
	}

	decoded, err := DecodeCheckpoint[string](
		record,
		JSONCodec[string]{},
		WithRecordMigration[string]("checkpoint.v0", func(record Record) (Record, error) {
			record.SchemaVersion = SchemaVersion
			record.State = []byte(`"new"`)
			record.StateHash = hashState(record.State)
			record.Metadata = map[string]any{"migrated_from": "checkpoint.v0"}
			return record, nil
		}),
	)
	if err != nil {
		t.Fatalf("DecodeCheckpoint() error = %v", err)
	}
	if decoded.State != "new" {
		t.Fatalf("decoded state = %q, want new", decoded.State)
	}
	if decoded.Metadata["migrated_from"] != "checkpoint.v0" {
		t.Fatalf("decoded metadata = %+v, want migration marker", decoded.Metadata)
	}
}

func TestMemoryAppliesCurrentConfigVersionOnWrite(t *testing.T) {
	ctx := context.Background()
	store := NewMemory[string](WithConfigVersion[string]("config:v1"))

	err := store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
		Step:     1,
		Node:     "write",
		State:    "state",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok, err := store.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got.ConfigVersion != "config:v1" {
		t.Fatalf("Latest().ConfigVersion = %q, want config:v1", got.ConfigVersion)
	}
}

func TestMemoryStoresEncodedRecordsAndVerifiesIntegrity(t *testing.T) {
	ctx := context.Background()
	store := NewMemory[string]()

	err := store.Put(ctx, graph.Checkpoint[string]{
		ID:       "checkpoint-1",
		IDs:      gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		ThreadID: "thread-1",
		Step:     1,
		Node:     "write",
		State:    "state",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if len(store.byThread["thread-1"]) != 1 {
		t.Fatalf("stored record count = %d, want 1", len(store.byThread["thread-1"]))
	}
	if store.byThread["thread-1"][0].StateHash == "" {
		t.Fatalf("stored record missing state hash: %+v", store.byThread["thread-1"][0])
	}

	store.byThread["thread-1"][0].State = []byte(`"changed"`)
	if _, ok, err := store.Latest(ctx, "thread-1"); !errors.Is(err, ErrIntegrityMismatch) || ok {
		t.Fatalf("Latest() ok=%v error=%v, want integrity mismatch", ok, err)
	}
}
