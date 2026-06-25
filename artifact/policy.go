package artifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrStoreRequired is returned when a policy store is created without a store.
	ErrStoreRequired = errors.New("artifact: store is required")
	// ErrPolicyRequired is returned when a policy store is created without a policy.
	ErrPolicyRequired = errors.New("artifact: policy is required")
)

// Store is the artifact storage contract used by the policy wrapper.
type Store interface {
	Put(ctx context.Context, artifact gopact.Artifact) (gopact.ArtifactRef, error)
	Get(ctx context.Context, id string) (gopact.Artifact, error)
	List(ctx context.Context) ([]gopact.ArtifactRef, error)
}

// Info is payload-free artifact metadata passed to policy checks.
type Info struct {
	Ref      gopact.ArtifactRef `json:"ref,omitempty"`
	Size     int64              `json:"size,omitempty"`
	Metadata map[string]any     `json:"metadata,omitempty"`
}

// PolicyInput is the stable policy input for artifact operations.
type PolicyInput struct {
	ID       string `json:"id,omitempty"`
	Artifact Info   `json:"artifact,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyOption configures a policy-wrapped artifact store.
type PolicyOption func(*policyConfig)

// WithPolicyIDs sets the runtime ids used in policy requests and events.
func WithPolicyIDs(ids gopact.RuntimeIDs) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.ids = ids
	}
}

// WithPolicyMetadata sets metadata copied into every policy request.
func WithPolicyMetadata(metadata map[string]any) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// WithPolicyEventSink publishes policy requested/decided events to sink.
func WithPolicyEventSink(sink gopact.EventSubscriber) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.sink = sink
	}
}

// PolicyStore authorizes artifact operations before calling the wrapped store.
type PolicyStore struct {
	next   Store
	policy gopact.Policy
	cfg    policyConfig
}

// NewPolicyStore wraps an artifact store with policy checks.
func NewPolicyStore(next Store, policy gopact.Policy, opts ...PolicyOption) (*PolicyStore, error) {
	if next == nil {
		return nil, ErrStoreRequired
	}
	if policy == nil {
		return nil, ErrPolicyRequired
	}
	cfg := policyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicyStore{next: next, policy: policy, cfg: cfg}, nil
}

// Put authorizes and stores artifact.
func (s *PolicyStore) Put(ctx context.Context, artifact gopact.Artifact) (gopact.ArtifactRef, error) {
	input := PolicyInput{Artifact: artifactInfo(artifact)}
	if err := s.authorize(ctx, gopact.PolicyActionPut, input); err != nil {
		return gopact.ArtifactRef{}, err
	}
	return s.next.Put(ctx, artifact)
}

// Get authorizes and reads one artifact by id.
func (s *PolicyStore) Get(ctx context.Context, id string) (gopact.Artifact, error) {
	if err := s.authorize(ctx, gopact.PolicyActionGet, PolicyInput{ID: id}); err != nil {
		return gopact.Artifact{}, err
	}
	return s.next.Get(ctx, id)
}

// List authorizes and lists stored artifact refs.
func (s *PolicyStore) List(ctx context.Context) ([]gopact.ArtifactRef, error) {
	if err := s.authorize(ctx, gopact.PolicyActionList, PolicyInput{}); err != nil {
		return nil, err
	}
	return s.next.List(ctx)
}

func (s *PolicyStore) authorize(ctx context.Context, action gopact.PolicyRequestAction, input PolicyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := gopact.PolicyRequest{
		IDs:      s.cfg.ids,
		Boundary: gopact.PolicyBoundaryArtifact,
		Action:   action,
		Input:    input,
		Metadata: copyAnyMap(s.cfg.metadata),
	}
	if err := s.publish(ctx, gopact.NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := s.policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("artifact: policy: %w", err)
	}
	if err := s.publish(ctx, gopact.NewPolicyDecidedEvent(req, decision)); err != nil {
		return err
	}
	if decision.Action == gopact.PolicyReview {
		return gopact.NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return &gopact.PolicyDeniedError{Decision: decision, Request: req}
	}
	return nil
}

func (s *PolicyStore) publish(ctx context.Context, event gopact.Event) error {
	if s.cfg.sink == nil {
		return nil
	}
	if err := s.cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("artifact: policy event sink: %w", err)
	}
	return nil
}

func artifactInfo(artifact gopact.Artifact) Info {
	return Info{
		Ref:      copyArtifactRef(artifact.Ref),
		Size:     int64(len(artifact.Content)),
		Metadata: copyAnyMap(artifact.Metadata),
	}
}

func copyArtifactRef(ref gopact.ArtifactRef) gopact.ArtifactRef {
	ref.Metadata = copyAnyMap(ref.Metadata)
	return ref
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
