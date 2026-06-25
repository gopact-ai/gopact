package artifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

const (
	// EffectTypeArtifactWrite is the standard effect type for artifact-producing writes.
	EffectTypeArtifactWrite = "artifact_write"

	// EffectReplayMetadataArtifactRefs stores the verified artifact refs from replay.
	EffectReplayMetadataArtifactRefs = "artifact_refs"

	// EffectReplayMetadataArtifactCount stores the number of verified artifact refs.
	EffectReplayMetadataArtifactCount = "artifact_count"
)

var (
	// ErrReplayArtifactsMissing is returned when artifact replay has no artifact refs.
	ErrReplayArtifactsMissing = errors.New("artifact: replay artifacts missing")
	// ErrReplayEffectType is returned when an effect is not an artifact write.
	ErrReplayEffectType = errors.New("artifact: replay effect type is not artifact_write")
)

// ReplayHandler verifies recorded artifact refs for idempotent artifact_write effects.
type ReplayHandler struct {
	getter Getter
}

// NewReplayHandler creates an EffectReplayExecutor for artifact_write effects.
func NewReplayHandler(getter Getter) *ReplayHandler {
	return &ReplayHandler{getter: getter}
}

// ReplayEffect implements gopact.EffectReplayExecutor.
func (h *ReplayHandler) ReplayEffect(ctx context.Context, decision gopact.EffectReplayDecision) (gopact.EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if h == nil || h.getter == nil {
		return gopact.EffectReplayResult{}, errors.New("artifact: replay getter is nil")
	}
	if decision.Action != gopact.EffectReplayActionReplay || decision.ReplayPolicy != gopact.EffectReplayIdempotent {
		return gopact.EffectReplayResult{}, fmt.Errorf("artifact: effect %q is not an idempotent replay decision", decision.Effect.ID)
	}
	if decision.Effect.Type != EffectTypeArtifactWrite {
		return gopact.EffectReplayResult{}, fmt.Errorf("%w: %q", ErrReplayEffectType, decision.Effect.Type)
	}
	if len(decision.Effect.Artifacts) == 0 {
		return gopact.EffectReplayResult{}, ErrReplayArtifactsMissing
	}

	refs := copyArtifactRefs(decision.Effect.Artifacts)
	if _, err := VerifyRefs(ctx, h.getter, refs); err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("artifact: replay verify refs: %w", err)
	}
	return gopact.EffectReplayResult{
		Metadata: map[string]any{
			EffectReplayMetadataArtifactRefs:  refs,
			EffectReplayMetadataArtifactCount: len(refs),
		},
	}, nil
}

func copyArtifactRefs(refs []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(refs))
	copy(out, refs)
	for i := range out {
		if len(out[i].Metadata) == 0 {
			continue
		}
		metadata := make(map[string]any, len(out[i].Metadata))
		for key, value := range out[i].Metadata {
			metadata[key] = value
		}
		out[i].Metadata = metadata
	}
	return out
}
