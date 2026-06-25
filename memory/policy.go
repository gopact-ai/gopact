package memory

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

var (
	ErrStoreRequired  = errors.New("memory: store is required")
	ErrPolicyRequired = errors.New("memory: policy is required")
)

// PolicyInput is the stable policy input for memory operations.
type PolicyInput struct {
	ID     ID     `json:"id,omitempty"`
	Memory Memory `json:"memory,omitempty"`
	Query  Query  `json:"query,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyOption configures a policy-wrapped memory store.
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
		cfg.metadata = copyMetadata(metadata)
	}
}

// WithPolicyEventSink publishes policy requested/decided events to sink.
func WithPolicyEventSink(sink gopact.EventSubscriber) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.sink = sink
	}
}

// PolicyStore authorizes memory operations before calling the wrapped store.
type PolicyStore struct {
	next   Store
	policy gopact.Policy
	cfg    policyConfig
}

// NewPolicyStore wraps a memory store with policy checks.
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

func (s *PolicyStore) Put(ctx context.Context, memory Memory) (ID, error) {
	if err := s.authorize(ctx, gopact.PolicyActionPut, PolicyInput{Memory: copyMemory(memory)}); err != nil {
		return "", err
	}
	return s.next.Put(ctx, memory)
}

func (s *PolicyStore) Get(ctx context.Context, id ID) (Memory, error) {
	if err := s.authorize(ctx, gopact.PolicyActionGet, PolicyInput{ID: id}); err != nil {
		return Memory{}, err
	}
	return s.next.Get(ctx, id)
}

func (s *PolicyStore) Search(ctx context.Context, query Query) (SearchResult, error) {
	if err := s.authorize(ctx, gopact.PolicyActionSearch, PolicyInput{Query: copyQuery(query)}); err != nil {
		return SearchResult{}, err
	}
	return s.next.Search(ctx, query)
}

func (s *PolicyStore) Delete(ctx context.Context, id ID) error {
	if err := s.authorize(ctx, gopact.PolicyActionDelete, PolicyInput{ID: id}); err != nil {
		return err
	}
	return s.next.Delete(ctx, id)
}

func (s *PolicyStore) authorize(ctx context.Context, action gopact.PolicyRequestAction, input PolicyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := gopact.PolicyRequest{
		IDs:      s.cfg.ids,
		Boundary: gopact.PolicyBoundaryMemory,
		Action:   action,
		Input:    input,
		Metadata: copyMetadata(s.cfg.metadata),
	}
	if err := s.publish(ctx, gopact.NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := s.policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("memory: policy: %w", err)
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
		return fmt.Errorf("memory: policy event sink: %w", err)
	}
	return nil
}

func copyQuery(query Query) Query {
	query.Types = append([]Type(nil), query.Types...)
	return query
}
