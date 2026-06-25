package trace

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// ErrPolicyRequired is returned when a policy exporter is constructed without a policy.
var ErrPolicyRequired = errors.New("trace: policy is required")

// PolicyKind identifies the trace exporter operation kind being authorized.
type PolicyKind string

const (
	// PolicyKindSpan authorizes one span export operation.
	PolicyKindSpan PolicyKind = "span"
)

// PolicyInput is the stable policy input for trace exporter operations.
type PolicyInput struct {
	Kind PolicyKind `json:"kind,omitempty"`
	Span SpanRecord `json:"span,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyOption configures a policy-wrapped trace exporter.
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

// PolicyExporter authorizes span export before delegating to an exporter.
type PolicyExporter struct {
	next   Exporter
	policy gopact.Policy
	cfg    policyConfig
}

var _ Exporter = (*PolicyExporter)(nil)

// NewPolicyExporter wraps a trace exporter with policy checks.
func NewPolicyExporter(next Exporter, policy gopact.Policy, opts ...PolicyOption) (*PolicyExporter, error) {
	if next == nil {
		return nil, ErrExporterRequired
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
	return &PolicyExporter{next: next, policy: policy, cfg: cfg}, nil
}

// ExportSpan authorizes and exports a span.
func (e *PolicyExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if e == nil || e.next == nil {
		return ErrExporterRequired
	}
	if e.policy == nil {
		return ErrPolicyRequired
	}
	ctx = normalizeContext(ctx)
	if err := e.authorize(ctx, PolicyInput{Kind: PolicyKindSpan, Span: copySpan(span)}); err != nil {
		return err
	}
	return e.next.ExportSpan(ctx, span)
}

func (e *PolicyExporter) authorize(ctx context.Context, input PolicyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := gopact.PolicyRequest{
		IDs:      e.cfg.ids,
		Boundary: gopact.PolicyBoundaryExporter,
		Action:   gopact.PolicyActionExport,
		Input:    copyPolicyInput(input),
		Metadata: copyAnyMap(e.cfg.metadata),
	}
	if err := e.publish(ctx, gopact.NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := e.policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("trace: policy: %w", err)
	}
	if err := e.publish(ctx, gopact.NewPolicyDecidedEvent(req, decision)); err != nil {
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

func (e *PolicyExporter) publish(ctx context.Context, event gopact.Event) error {
	if e.cfg.sink == nil {
		return nil
	}
	if err := e.cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("trace: policy event sink: %w", err)
	}
	return nil
}

func copyPolicyInput(input PolicyInput) PolicyInput {
	input.Span = copySpan(input.Span)
	return input
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
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
