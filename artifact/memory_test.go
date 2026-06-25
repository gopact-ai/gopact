package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestMemoryPutGetAndList(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	ref, err := store.Put(ctx, gopact.Artifact{
		Ref:     gopact.ArtifactRef{Name: "trace.json", MIMEType: "application/json", Scope: gopact.ArtifactScopeRun},
		Content: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if ref.ID == "" || ref.Size == 0 || ref.SHA256 == "" {
		t.Fatalf("artifact ref missing integrity metadata: %+v", ref)
	}

	got, err := store.Get(ctx, ref.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got.Content) != `{"ok":true}` || got.Ref.ID != ref.ID {
		t.Fatalf("Get() = %+v", got)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != ref.ID {
		t.Fatalf("List() = %+v", list)
	}
}

func TestMemoryGetRejectsMissingArtifact(t *testing.T) {
	_, err := NewMemory().Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotFound)
	}
}

func TestMemoryPutCopiesContent(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	content := []byte("payload")

	ref, err := store.Put(ctx, gopact.Artifact{Content: content})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	content[0] = 'P'

	got, err := store.Get(ctx, ref.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got.Content) != "payload" {
		t.Fatalf("stored content = %q, want payload", got.Content)
	}
}
