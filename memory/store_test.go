package memory

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestStorePutGetSearchAndDelete(t *testing.T) {
	ctx := context.Background()
	store := New()

	id, err := store.Put(ctx, Memory{
		Scope:   Scope{UserID: "user-1", SessionID: "session-1"},
		Type:    TypeSemantic,
		Content: "prefers concise answers",
		Metadata: map[string]any{
			"source": "test",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if id == "" {
		t.Fatal("Put() returned empty id")
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != id || got.Content != "prefers concise answers" || got.CreatedAt.IsZero() {
		t.Fatalf("Get() = %+v", got)
	}

	results, err := store.Search(ctx, Query{
		Scope: Scope{UserID: "user-1"},
		Text:  "concise",
		Types: []Type{TypeSemantic},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results.Memories) != 1 || results.Memories[0].ID != id {
		t.Fatalf("Search() = %+v", results)
	}

	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, err = store.Get(ctx, id)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want %v", err, ErrNotFound)
	}
}

func TestStoreSearchHonorsScopeIsolation(t *testing.T) {
	ctx := context.Background()
	store := New()
	_, _ = store.Put(ctx, Memory{Scope: Scope{UserID: "alice"}, Type: TypeProfile, Content: "likes go"})
	_, _ = store.Put(ctx, Memory{Scope: Scope{UserID: "bob"}, Type: TypeProfile, Content: "likes go"})

	results, err := store.Search(ctx, Query{Scope: Scope{UserID: "alice"}, Text: "go"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results.Memories) != 1 || results.Memories[0].Scope.UserID != "alice" {
		t.Fatalf("Search() = %+v, want only alice memory", results)
	}
}

func TestStoreRejectsInvalidMemory(t *testing.T) {
	ctx := context.Background()
	store := New()
	tests := []struct {
		name   string
		memory Memory
	}{
		{name: "missing type", memory: Memory{Content: "x"}},
		{name: "invalid type", memory: Memory{Type: Type("unknown"), Content: "x"}},
		{name: "missing content", memory: Memory{Type: TypeSemantic}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.Put(ctx, tt.memory); err == nil {
				t.Fatal("Put() error = nil, want validation error")
			}
		})
	}
}

func TestStoreCopiesMetadata(t *testing.T) {
	ctx := context.Background()
	store := New()
	metadata := map[string]any{"source": "before"}

	id, err := store.Put(ctx, Memory{Type: TypeEpisodic, Content: "ran tests", Metadata: metadata})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	metadata["source"] = "after"

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(got.Metadata, map[string]any{"source": "before"}) {
		t.Fatalf("Metadata = %+v, want copied metadata", got.Metadata)
	}
}
