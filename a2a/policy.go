package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrAgentRequired is returned when a policy agent has no wrapped agent.
	ErrAgentRequired = errors.New("a2a: agent is required")
	// ErrPolicyRequired is returned when a policy agent has no policy.
	ErrPolicyRequired = errors.New("a2a: policy is required")
)

// PolicyInput is the stable policy input for A2A operations.
type PolicyInput struct {
	AgentName string    `json:"agent_name,omitempty"`
	Card      AgentCard `json:"card,omitempty"`
	Task      *Task     `json:"task,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyAgentOption configures a policy-wrapped A2A agent.
type PolicyAgentOption func(*policyConfig)

// WithPolicyIDs sets fallback runtime ids used in policy requests and events.
func WithPolicyIDs(ids gopact.RuntimeIDs) PolicyAgentOption {
	return func(cfg *policyConfig) {
		cfg.ids = ids
	}
}

// WithPolicyMetadata sets metadata copied into every policy request.
func WithPolicyMetadata(metadata map[string]any) PolicyAgentOption {
	return func(cfg *policyConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// WithPolicyEventSink publishes policy requested/decided events to sink.
func WithPolicyEventSink(sink gopact.EventSubscriber) PolicyAgentOption {
	return func(cfg *policyConfig) {
		cfg.sink = sink
	}
}

// PolicyAgent authorizes A2A send, stream, and cancel operations.
type PolicyAgent struct {
	next   Agent
	policy gopact.Policy
	cfg    policyConfig
}

var (
	_ Agent          = (*PolicyAgent)(nil)
	_ StreamingAgent = (*PolicyAgent)(nil)
)

// NewPolicyAgent wraps an A2A agent with policy checks.
func NewPolicyAgent(next Agent, policy gopact.Policy, opts ...PolicyAgentOption) (*PolicyAgent, error) {
	if next == nil {
		return nil, ErrAgentRequired
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
	return &PolicyAgent{next: next, policy: policy, cfg: cfg}, nil
}

// Card returns the wrapped agent card.
func (a *PolicyAgent) Card() AgentCard {
	if a == nil || a.next == nil {
		return AgentCard{}
	}
	return copyAgentCard(a.next.Card())
}

// Send authorizes one outbound A2A task before sending it.
func (a *PolicyAgent) Send(ctx context.Context, task Task) (Result, error) {
	ctx, task = a.taskContext(ctx, task)
	events, err := a.authorize(ctx, task.IDs, gopact.PolicyActionSend, PolicyInput{
		AgentName: a.Card().Name,
		Card:      a.Card(),
		Task:      copiedTaskPtr(task),
	})
	if err != nil {
		return Result{TaskID: taskID(task), Events: events}, err
	}
	result, err := a.next.Send(ctx, task)
	result.Events = append(events, copyEvents(result.Events)...)
	return result, err
}

// Stream authorizes one outbound A2A streaming task before streaming it.
func (a *PolicyAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		ctx, task = a.taskContext(ctx, task)
		_, err := a.authorize(ctx, task.IDs, gopact.PolicyActionStream, PolicyInput{
			AgentName: a.Card().Name,
			Card:      a.Card(),
			Task:      copiedTaskPtr(task),
		})
		if err != nil {
			yield(failedTaskEvent(task, err), err)
			return
		}
		streamer, ok := a.next.(StreamingAgent)
		if !ok {
			yield(failedTaskEvent(task, ErrStreamNotSupported), ErrStreamNotSupported)
			return
		}
		for event, err := range streamer.Stream(ctx, task) {
			if !yield(event, err) || err != nil {
				return
			}
		}
	}
}

// Cancel authorizes a remote A2A task cancellation before canceling it.
func (a *PolicyAgent) Cancel(ctx context.Context, taskID string) error {
	ids := runtimeIDsWithContext(ctx, gopact.RuntimeIDs{}, a.cfg.ids)
	ctx = contextWithRuntimeIDs(ctx, ids)
	if _, err := a.authorize(ctx, ids, gopact.PolicyActionCancel, PolicyInput{
		AgentName: a.Card().Name,
		Card:      a.Card(),
		TaskID:    taskID,
	}); err != nil {
		return err
	}
	return a.next.Cancel(ctx, taskID)
}

func (a *PolicyAgent) taskContext(ctx context.Context, task Task) (context.Context, Task) {
	task = copyTask(task)
	task.IDs = runtimeIDsWithContext(ctx, task.IDs, a.cfg.ids)
	return contextWithRuntimeIDs(ctx, task.IDs), task
}

func (a *PolicyAgent) authorize(ctx context.Context, ids gopact.RuntimeIDs, action gopact.PolicyRequestAction, input PolicyInput) ([]gopact.Event, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req := gopact.PolicyRequest{
		IDs:      ids,
		Boundary: gopact.PolicyBoundaryA2A,
		Action:   action,
		Input:    input,
		Metadata: copyAnyMap(a.cfg.metadata),
	}
	requested := gopact.NewPolicyRequestedEvent(req)
	events := []gopact.Event{requested}
	if err := a.publish(ctx, requested); err != nil {
		return events, err
	}
	decision, err := a.policy.Decide(ctx, req)
	if err != nil {
		return events, fmt.Errorf("a2a: policy: %w", err)
	}
	decided := gopact.NewPolicyDecidedEvent(req, decision)
	events = append(events, decided)
	if err := a.publish(ctx, decided); err != nil {
		return events, err
	}
	if decision.Action == gopact.PolicyReview {
		return events, gopact.NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return events, &gopact.PolicyDeniedError{Decision: decision, Request: req}
	}
	return events, nil
}

func (a *PolicyAgent) publish(ctx context.Context, event gopact.Event) error {
	if a.cfg.sink == nil {
		return nil
	}
	if err := a.cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("a2a: policy event sink: %w", err)
	}
	return nil
}

func copiedTaskPtr(task Task) *Task {
	out := copyTask(task)
	return &out
}
