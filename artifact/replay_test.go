package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestReplayHandlerVerifiesRecordedArtifacts(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{
		Ref:     gopact.ArtifactRef{Name: "trace.json", MIMEType: "application/json"},
		Content: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "artifact-1",
					Type:           EffectTypeArtifactWrite,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "artifact:trace",
					Artifacts:      []gopact.ArtifactRef{ref},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	refs, ok := results[0].Metadata[EffectReplayMetadataArtifactRefs].([]gopact.ArtifactRef)
	if !ok {
		t.Fatalf("artifact refs metadata = %#v, want []gopact.ArtifactRef", results[0].Metadata[EffectReplayMetadataArtifactRefs])
	}
	if len(refs) != 1 || refs[0].ID != ref.ID {
		t.Fatalf("artifact refs = %+v, want recorded ref %q", refs, ref.ID)
	}
	if count, ok := results[0].Metadata[EffectReplayMetadataArtifactCount].(int); !ok || count != 1 {
		t.Fatalf("artifact count metadata = %#v, want 1", results[0].Metadata[EffectReplayMetadataArtifactCount])
	}
}

func TestReplayHandlerCanBeRegisteredInEffectReplayRegistry(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{Content: []byte("report")})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	replayRegistry := gopact.NewEffectReplayRegistry()
	if err := replayRegistry.Register(EffectTypeArtifactWrite, NewReplayHandler(store)); err != nil {
		t.Fatalf("Register(replay handler) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "artifact-1",
					Type:           EffectTypeArtifactWrite,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "artifact:report",
					Artifacts:      []gopact.ArtifactRef{ref},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, replayRegistry)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	refs := results[0].Metadata[EffectReplayMetadataArtifactRefs].([]gopact.ArtifactRef)
	if refs[0].ID != ref.ID {
		t.Fatalf("artifact ref id = %q, want %q", refs[0].ID, ref.ID)
	}
}

func TestReplayHandlerRejectsTamperedArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{Content: []byte("original")})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	tampered := store.items[ref.ID]
	tampered.Content = []byte("tampered")
	store.items[ref.ID] = tampered
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "artifact-1",
					Type:           EffectTypeArtifactWrite,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "artifact:tampered",
					Artifacts:      []gopact.ArtifactRef{ref},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err = gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrIntegrityMismatch", err)
	}
}

func TestReplayHandlerRejectsMissingArtifactRefs(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "artifact-1",
					Type:           EffectTypeArtifactWrite,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "artifact:missing",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrReplayArtifactsMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayArtifactsMissing", err)
	}
}

func TestReplayHandlerRejectsNonArtifactWriteEffect(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "effect-1",
					Type:           "tool_call",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "artifact:wrong-type",
					Artifacts:      []gopact.ArtifactRef{{ID: "artifact-1"}},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrReplayEffectType) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayEffectType", err)
	}
}
