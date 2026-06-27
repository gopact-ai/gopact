package gopact

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrSecretProviderRequired = errors.New("gopact: secret provider is required")
	ErrSecretPolicyRequired   = errors.New("gopact: secret policy is required")
)

// SecretPolicyInput is the stable policy input for secret resolution.
type SecretPolicyInput struct {
	Ref SecretRef `json:"ref"`
}

type secretPolicyConfig struct {
	ids      RuntimeIDs
	metadata map[string]any
	sink     EventSubscriber
}

// SecretPolicyOption configures a policy-wrapped secret provider.
type SecretPolicyOption func(*secretPolicyConfig)

// WithSecretPolicyIDs sets the runtime ids used in policy requests and events.
func WithSecretPolicyIDs(ids RuntimeIDs) SecretPolicyOption {
	return func(cfg *secretPolicyConfig) {
		cfg.ids = ids
	}
}

// WithSecretPolicyMetadata sets metadata copied into every policy request.
func WithSecretPolicyMetadata(metadata map[string]any) SecretPolicyOption {
	return func(cfg *secretPolicyConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// WithSecretPolicyEventSink publishes policy requested/decided events to sink.
func WithSecretPolicyEventSink(sink EventSubscriber) SecretPolicyOption {
	return func(cfg *secretPolicyConfig) {
		cfg.sink = sink
	}
}

// PolicySecretProvider authorizes secret resolution before delegating to a provider.
type PolicySecretProvider struct {
	next   SecretProvider
	policy Policy
	cfg    secretPolicyConfig
}

// NewPolicySecretProvider wraps a secret provider with policy checks.
func NewPolicySecretProvider(
	next SecretProvider,
	policy Policy,
	opts ...SecretPolicyOption,
) (*PolicySecretProvider, error) {
	if next == nil {
		return nil, ErrSecretProviderRequired
	}
	if policy == nil {
		return nil, ErrSecretPolicyRequired
	}
	cfg := secretPolicyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicySecretProvider{next: next, policy: policy, cfg: cfg}, nil
}

// ResolveSecret authorizes ref resolution before calling the wrapped provider.
func (p *PolicySecretProvider) ResolveSecret(ctx context.Context, ref SecretRef) (SecretValue, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return SecretValue{}, err
	}
	if err := ValidateSecretRef(ref); err != nil {
		return SecretValue{}, err
	}
	req := PolicyRequest{
		IDs:      p.cfg.ids,
		Boundary: PolicyBoundarySecret,
		Action:   PolicyActionResolve,
		Input:    SecretPolicyInput{Ref: ref},
		Metadata: copyAnyMap(p.cfg.metadata),
	}
	if err := p.publish(ctx, NewPolicyRequestedEvent(req)); err != nil {
		return SecretValue{}, err
	}
	decision, err := p.policy.Decide(ctx, req)
	if err != nil {
		return SecretValue{}, fmt.Errorf("gopact: secret policy: %w", err)
	}
	if err := p.publish(ctx, NewPolicyDecidedEvent(req, decision)); err != nil {
		return SecretValue{}, err
	}
	if decision.Action == PolicyReview {
		return SecretValue{}, NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return SecretValue{}, &PolicyDeniedError{Decision: decision, Request: req}
	}
	return p.next.ResolveSecret(ctx, ref)
}

func (p *PolicySecretProvider) publish(ctx context.Context, event Event) error {
	if p.cfg.sink == nil {
		return nil
	}
	if err := p.cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("gopact: secret policy event sink: %w", err)
	}
	return nil
}
