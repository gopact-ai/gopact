package gopact

import (
	"context"
	"errors"
	"fmt"
	"iter"
)

var (
	ErrChannelRequired       = errors.New("gopact: channel is required")
	ErrChannelPolicyRequired = errors.New("gopact: channel policy is required")
)

// ChannelPolicyInput is the stable policy input for channel operations.
type ChannelPolicyInput struct {
	Channel ChannelTarget  `json:"channel,omitempty"`
	Payload ChannelPayload `json:"payload,omitempty"`
	Event   ChannelEvent   `json:"event,omitempty"`
}

type channelPolicyConfig struct {
	ids      RuntimeIDs
	metadata map[string]any
	sink     EventSubscriber
}

// ChannelPolicyOption configures a policy-wrapped channel.
type ChannelPolicyOption func(*channelPolicyConfig)

// WithChannelPolicyIDs sets the runtime ids used in policy requests and events.
func WithChannelPolicyIDs(ids RuntimeIDs) ChannelPolicyOption {
	return func(cfg *channelPolicyConfig) {
		cfg.ids = ids
	}
}

// WithChannelPolicyMetadata sets metadata copied into every policy request.
func WithChannelPolicyMetadata(metadata map[string]any) ChannelPolicyOption {
	return func(cfg *channelPolicyConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// WithChannelPolicyEventSink publishes policy requested/decided events to sink.
func WithChannelPolicyEventSink(sink EventSubscriber) ChannelPolicyOption {
	return func(cfg *channelPolicyConfig) {
		cfg.sink = sink
	}
}

// PolicyChannel authorizes channel sends and inbound events before exposing them.
type PolicyChannel struct {
	next   Channel
	policy Policy
	cfg    channelPolicyConfig
}

// NewPolicyChannel wraps a channel with policy checks.
func NewPolicyChannel(next Channel, policy Policy, opts ...ChannelPolicyOption) (*PolicyChannel, error) {
	if next == nil {
		return nil, ErrChannelRequired
	}
	if policy == nil {
		return nil, ErrChannelPolicyRequired
	}
	cfg := channelPolicyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicyChannel{next: next, policy: policy, cfg: cfg}, nil
}

// Name returns the wrapped channel name.
func (c *PolicyChannel) Name() string {
	return c.next.Name()
}

// Send authorizes an outbound channel payload before sending it.
func (c *PolicyChannel) Send(ctx context.Context, payload ChannelPayload) error {
	input := ChannelPolicyInput{
		Channel: payload.Target,
		Payload: copyChannelPayload(payload),
	}
	if err := c.authorize(ctx, c.cfg.ids, PolicyActionSend, input); err != nil {
		return err
	}
	return c.next.Send(ctx, payload)
}

// Events authorizes inbound channel events before yielding them to callers.
func (c *PolicyChannel) Events(ctx context.Context) iter.Seq2[ChannelEvent, error] {
	return func(yield func(ChannelEvent, error) bool) {
		for event, err := range c.next.Events(ctx) {
			if err != nil {
				yield(ChannelEvent{}, err)
				return
			}
			input := ChannelPolicyInput{
				Channel: event.Channel,
				Event:   copyChannelEvent(event),
			}
			ids := event.IDs.WithDefaults(c.cfg.ids)
			if err := c.authorize(ctx, ids, PolicyActionReceive, input); err != nil {
				yield(ChannelEvent{}, err)
				return
			}
			if !yield(copyChannelEvent(event), nil) {
				return
			}
		}
	}
}

// Close closes the wrapped channel.
func (c *PolicyChannel) Close(ctx context.Context) error {
	return c.next.Close(ctx)
}

func (c *PolicyChannel) authorize(ctx context.Context, ids RuntimeIDs, action PolicyRequestAction, input ChannelPolicyInput) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	req := PolicyRequest{
		IDs:      ids,
		Boundary: PolicyBoundaryChannel,
		Action:   action,
		Input:    input,
		Metadata: copyAnyMap(c.cfg.metadata),
	}
	if err := c.publish(ctx, NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := c.policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("gopact: channel policy: %w", err)
	}
	if err := c.publish(ctx, NewPolicyDecidedEvent(req, decision)); err != nil {
		return err
	}
	if decision.Action == PolicyReview {
		return NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return &PolicyDeniedError{Decision: decision, Request: req}
	}
	return nil
}

func (c *PolicyChannel) publish(ctx context.Context, event Event) error {
	if c.cfg.sink == nil {
		return nil
	}
	if err := c.cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("gopact: channel policy event sink: %w", err)
	}
	return nil
}
