package checkpoint

import (
	"context"
	"testing"

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

	if _, ok := store.Latest(ctx, "missing"); ok {
		t.Fatal("Latest() ok = true, want false")
	}

	_ = store.Put(ctx, graph.Checkpoint[int]{ThreadID: "thread", Step: 1, State: 10})
	_ = store.Put(ctx, graph.Checkpoint[int]{ThreadID: "thread", Step: 2, State: 20})

	got, ok := store.Latest(ctx, "thread")
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got.Step != 2 || got.State != 20 {
		t.Fatalf("Latest() = %+v, want step 2 state 20", got)
	}
}
