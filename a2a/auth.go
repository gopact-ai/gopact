package a2a

import (
	"context"
	"errors"
	"iter"

	"github.com/gopact-ai/gopact"
)

// ErrAuthenticatorRequired is returned when an auth agent has no authenticator.
var ErrAuthenticatorRequired = errors.New("a2a: authenticator is required")

type authConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
}

// AuthAgentOption configures an auth-wrapped A2A agent.
type AuthAgentOption func(*authConfig)

// WithAuthIDs sets fallback runtime ids used in auth requests.
func WithAuthIDs(ids gopact.RuntimeIDs) AuthAgentOption {
	return func(cfg *authConfig) {
		cfg.ids = ids
	}
}

// WithAuthMetadata sets metadata copied into every auth request.
func WithAuthMetadata(metadata map[string]any) AuthAgentOption {
	return func(cfg *authConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// AuthAgent injects A2A auth for send, stream, and cancel operations.
type AuthAgent struct {
	next          Agent
	authenticator Authenticator
	cfg           authConfig
}

var (
	_ Agent          = (*AuthAgent)(nil)
	_ StreamingAgent = (*AuthAgent)(nil)
)

// NewAuthAgent wraps an A2A agent with authentication injection.
func NewAuthAgent(next Agent, authenticator Authenticator, opts ...AuthAgentOption) (*AuthAgent, error) {
	if next == nil {
		return nil, ErrAgentRequired
	}
	if authenticator == nil {
		return nil, ErrAuthenticatorRequired
	}
	cfg := authConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &AuthAgent{next: next, authenticator: authenticator, cfg: cfg}, nil
}

// Card returns the wrapped agent card.
func (a *AuthAgent) Card() AgentCard {
	if a == nil || a.next == nil {
		return AgentCard{}
	}
	return copyAgentCard(a.next.Card())
}

// Send injects auth into one outbound task before sending it.
func (a *AuthAgent) Send(ctx context.Context, task Task) (Result, error) {
	ctx, task, err := a.taskContext(ctx, gopact.PolicyActionSend, task)
	if err != nil {
		return Result{}, err
	}
	return a.next.Send(ctx, task)
}

// Stream injects auth into one outbound streaming task before streaming it.
func (a *AuthAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		ctx, task, err := a.taskContext(ctx, gopact.PolicyActionStream, task)
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

// Cancel injects auth into the cancellation context before canceling a remote task.
func (a *AuthAgent) Cancel(ctx context.Context, taskID string) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ids := runtimeIDsWithContext(ctx, gopact.RuntimeIDs{}, a.cfg.ids)
	ctx = contextWithRuntimeIDs(ctx, ids)
	card := a.Card()
	auth, err := a.authenticator.Authenticate(ctx, AuthRequest{
		IDs:       ids,
		AgentName: card.Name,
		Card:      card,
		Action:    gopact.PolicyActionCancel,
		TaskID:    taskID,
		Metadata:  copyAnyMap(a.cfg.metadata),
	})
	if err != nil {
		return err
	}
	if !auth.IsZero() {
		ctx = ContextWithAuth(ctx, auth)
	}
	return a.next.Cancel(ctx, taskID)
}

func (a *AuthAgent) taskContext(ctx context.Context, action gopact.PolicyRequestAction, task Task) (context.Context, Task, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ctx, task, err
	}
	task = copyTask(task)
	task.IDs = runtimeIDsWithContext(ctx, task.IDs, a.cfg.ids)
	ctx = contextWithRuntimeIDs(ctx, task.IDs)
	if task.Auth != nil && !task.Auth.IsZero() {
		return ContextWithAuth(ctx, *task.Auth), task, nil
	}
	card := a.Card()
	auth, err := a.authenticator.Authenticate(ctx, AuthRequest{
		IDs:       task.IDs,
		AgentName: card.Name,
		Card:      card,
		Action:    action,
		Task:      copiedTaskPtr(task),
		Metadata:  copyAnyMap(a.cfg.metadata),
	})
	if err != nil {
		return ctx, task, err
	}
	if !auth.IsZero() {
		task.Auth = &auth
		ctx = ContextWithAuth(ctx, auth)
	}
	return ctx, task, nil
}
