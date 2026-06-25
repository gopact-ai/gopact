package gopact

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrEffectReplayHandlerExists is returned when registering a duplicate replay handler.
	ErrEffectReplayHandlerExists = errors.New("gopact: effect replay handler already exists")
	// ErrEffectReplayHandlerNotFound is returned when no replay handler matches an effect.
	ErrEffectReplayHandlerNotFound = errors.New("gopact: effect replay handler not found")
)

type effectReplayHandlerKey struct {
	effectType string
	target     string
}

// EffectReplayRegistry dispatches replayable effects to type- or target-specific handlers.
type EffectReplayRegistry struct {
	mu       sync.RWMutex
	handlers map[effectReplayHandlerKey]EffectReplayExecutor
	fallback EffectReplayExecutor
}

// NewEffectReplayRegistry creates an empty replay handler registry.
func NewEffectReplayRegistry() *EffectReplayRegistry {
	return &EffectReplayRegistry{handlers: make(map[effectReplayHandlerKey]EffectReplayExecutor)}
}

// Register installs a handler for all replayable effects of one type.
func (r *EffectReplayRegistry) Register(effectType string, handler EffectReplayExecutor) error {
	return r.register(effectReplayHandlerKey{effectType: effectType}, handler)
}

// RegisterTarget installs a handler for one replayable effect type and target pair.
func (r *EffectReplayRegistry) RegisterTarget(effectType, target string, handler EffectReplayExecutor) error {
	if target == "" {
		return errors.New("gopact: effect replay target is required")
	}
	return r.register(effectReplayHandlerKey{effectType: effectType, target: target}, handler)
}

// RegisterFallback installs the handler used when no type or target handler matches.
func (r *EffectReplayRegistry) RegisterFallback(handler EffectReplayExecutor) error {
	if r == nil {
		return errors.New("gopact: effect replay registry is nil")
	}
	if handler == nil {
		return errors.New("gopact: effect replay handler is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fallback != nil {
		return ErrEffectReplayHandlerExists
	}
	r.fallback = handler
	return nil
}

// ReplayEffect implements EffectReplayExecutor by routing decisions to registered handlers.
func (r *EffectReplayRegistry) ReplayEffect(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return EffectReplayResult{}, err
	}
	if r == nil {
		return EffectReplayResult{}, errors.New("gopact: effect replay registry is nil")
	}

	handler, ok := r.resolve(decision.Effect)
	if !ok {
		return EffectReplayResult{}, fmt.Errorf("%w: type %q target %q", ErrEffectReplayHandlerNotFound, decision.Effect.Type, decision.Effect.Target)
	}
	result, err := handler.ReplayEffect(ctx, decision)
	if err != nil {
		return EffectReplayResult{}, err
	}
	return normalizeEffectReplayResult(decision, result), nil
}

func (r *EffectReplayRegistry) register(key effectReplayHandlerKey, handler EffectReplayExecutor) error {
	if r == nil {
		return errors.New("gopact: effect replay registry is nil")
	}
	if key.effectType == "" {
		return errors.New("gopact: effect replay type is required")
	}
	if handler == nil {
		return errors.New("gopact: effect replay handler is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.handlers == nil {
		r.handlers = make(map[effectReplayHandlerKey]EffectReplayExecutor)
	}
	if _, ok := r.handlers[key]; ok {
		return fmt.Errorf("%w: type %q target %q", ErrEffectReplayHandlerExists, key.effectType, key.target)
	}
	r.handlers[key] = handler
	return nil
}

func (r *EffectReplayRegistry) resolve(effect EffectRecord) (EffectReplayExecutor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if effect.Target != "" {
		if handler, ok := r.handlers[effectReplayHandlerKey{effectType: effect.Type, target: effect.Target}]; ok {
			return handler, true
		}
	}
	if handler, ok := r.handlers[effectReplayHandlerKey{effectType: effect.Type}]; ok {
		return handler, true
	}
	if r.fallback != nil {
		return r.fallback, true
	}
	return nil, false
}
