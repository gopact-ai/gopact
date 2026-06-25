package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

const (
	// EffectTypeToolCall is the standard effect type for registry tool invocations.
	EffectTypeToolCall = "tool_call"

	// EffectMetadataToolArgs stores the JSON arguments needed to replay an idempotent tool call.
	EffectMetadataToolArgs = "tool_args"

	// EffectReplayMetadataToolResult stores the ToolResult produced by a replayed tool call.
	EffectReplayMetadataToolResult = "tool_result"

	// EffectReplayMetadataCommitHit marks that replay returned a previously committed tool result.
	EffectReplayMetadataCommitHit = "commit_hit"

	// EffectReplayMetadataCommitStored marks that replay stored the tool result for future idempotent calls.
	EffectReplayMetadataCommitStored = "commit_stored"
)

var (
	ErrReplayArgsMissing = errors.New("tools: replay args missing")
	ErrReplayEffectType  = errors.New("tools: replay effect type is not tool_call")
)

// ReplayOption configures a tool replay handler.
type ReplayOption func(*ReplayHandler)

// ReplayHandler replays idempotent tool_call effects through a Registry.
type ReplayHandler struct {
	registry    *Registry
	scope       Scope
	commitStore CommitStore
}

// NewReplayHandler creates an EffectReplayExecutor for tool_call effects.
func NewReplayHandler(registry *Registry, opts ...ReplayOption) *ReplayHandler {
	handler := &ReplayHandler{registry: registry}
	for _, opt := range opts {
		if opt != nil {
			opt(handler)
		}
	}
	return handler
}

// WithReplayScope sets the base scope used while replaying tool calls.
func WithReplayScope(scope Scope) ReplayOption {
	return func(handler *ReplayHandler) {
		handler.scope = scope
	}
}

// WithReplayCommitStore sets the store used to avoid duplicate idempotent tool replay.
func WithReplayCommitStore(store CommitStore) ReplayOption {
	return func(handler *ReplayHandler) {
		handler.commitStore = store
	}
}

// ReplayEffect implements gopact.EffectReplayExecutor.
func (h *ReplayHandler) ReplayEffect(ctx context.Context, decision gopact.EffectReplayDecision) (gopact.EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if h == nil || h.registry == nil {
		return gopact.EffectReplayResult{}, errors.New("tools: replay registry is nil")
	}
	if decision.Action != gopact.EffectReplayActionReplay || decision.ReplayPolicy != gopact.EffectReplayIdempotent {
		return gopact.EffectReplayResult{}, fmt.Errorf("tools: effect %q is not an idempotent replay decision", decision.Effect.ID)
	}
	if decision.Effect.Type != EffectTypeToolCall {
		return gopact.EffectReplayResult{}, fmt.Errorf("%w: %q", ErrReplayEffectType, decision.Effect.Type)
	}
	if decision.Effect.Target == "" {
		return gopact.EffectReplayResult{}, errors.New("tools: replay target is required")
	}

	if h.commitStore != nil && decision.Effect.IdempotencyKey != "" {
		record, ok, err := h.commitStore.Load(ctx, decision.Effect.IdempotencyKey)
		if err != nil {
			return gopact.EffectReplayResult{}, fmt.Errorf("tools: load replay commit: %w", err)
		}
		if ok {
			return gopact.EffectReplayResult{
				Metadata: map[string]any{
					EffectReplayMetadataToolResult: record.Result,
					EffectReplayMetadataCommitHit:  true,
				},
			}, nil
		}
	}

	args, err := replayArgs(decision.Effect)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	scope := h.scope
	if scope.IDs.CallID == "" {
		scope.IDs.CallID = decision.Effect.ID
	}
	result, err := h.registry.Invoke(ctx, decision.Effect.Target, args, scope)
	if err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("tools: replay tool %q: %w", decision.Effect.Target, err)
	}
	metadata := map[string]any{
		EffectReplayMetadataToolResult: result,
	}
	if h.commitStore != nil && decision.Effect.IdempotencyKey != "" {
		err := h.commitStore.Store(ctx, CommitRecord{
			IdempotencyKey: decision.Effect.IdempotencyKey,
			EffectID:       decision.Effect.ID,
			ToolName:       decision.Effect.Target,
			Result:         result,
		})
		if err != nil {
			return gopact.EffectReplayResult{}, fmt.Errorf("tools: store replay commit: %w", err)
		}
		metadata[EffectReplayMetadataCommitStored] = true
	}
	return gopact.EffectReplayResult{
		Metadata: metadata,
	}, nil
}

func replayArgs(effect gopact.EffectRecord) (json.RawMessage, error) {
	if len(effect.Metadata) == 0 {
		return nil, ErrReplayArgsMissing
	}
	value, ok := effect.Metadata[EffectMetadataToolArgs]
	if !ok {
		return nil, ErrReplayArgsMissing
	}

	var raw []byte
	switch args := value.(type) {
	case json.RawMessage:
		raw = append([]byte(nil), args...)
	case []byte:
		raw = append([]byte(nil), args...)
	case string:
		raw = []byte(args)
	default:
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("tools: encode replay args: %w", err)
		}
		raw = encoded
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, ErrReplayArgsMissing
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("tools: replay args are not valid JSON")
	}
	return json.RawMessage(append([]byte(nil), raw...)), nil
}
