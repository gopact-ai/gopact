package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrToolRetryAttemptRequired = errors.New("gopact: tool retry attempt is required")
	ErrToolRetryDeciderRequired = errors.New("gopact: tool retry decider is required")
)

// ToolRetryAction describes the decision for a failed tool invocation.
type ToolRetryAction string

const (
	ToolRetryStop  ToolRetryAction = "stop"
	ToolRetryRetry ToolRetryAction = "retry"
)

// ToolRetryBackoff computes the delay before the next retry attempt.
type ToolRetryBackoff func(ToolRetryRequest) time.Duration

// ToolRetryErrorPredicate decides whether an error is retryable.
type ToolRetryErrorPredicate func(error) bool

// ToolRetryPolicy is a local retry policy for already-failed tool invocations.
// It does not execute tools. Callers must explicitly run the next attempt when
// the decision action is ToolRetryRetry.
type ToolRetryPolicy struct {
	MaxAttempts        int
	RetryNonIdempotent bool
	RetryIf            ToolRetryErrorPredicate
	Backoff            ToolRetryBackoff
	Metadata           map[string]any
}

// DecideToolRetry lets ToolRetryPolicy satisfy ToolRetryDecider.
func (p ToolRetryPolicy) DecideToolRetry(ctx context.Context, request ToolRetryRequest) (ToolRetryDecision, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ToolRetryDecision{}, err
	}
	return DecideToolRetry(request, p)
}

// ToolRetryRequest describes one failed tool attempt.
type ToolRetryRequest struct {
	ToolName       string
	Attempt        int
	Err            error
	Result         ToolResult
	Effects        []EffectRecord
	IdempotencyKey string
	Metadata       map[string]any
}

// ToolRetryDecision records whether and how to run another tool attempt.
type ToolRetryDecision struct {
	Action         ToolRetryAction
	Attempt        int
	NextAttempt    int
	Delay          time.Duration
	Reason         string
	IdempotencyKey string
	Metadata       map[string]any
}

// ToolRetryDecider decides whether a failed tool attempt may be retried.
type ToolRetryDecider interface {
	DecideToolRetry(ctx context.Context, request ToolRetryRequest) (ToolRetryDecision, error)
}

// ToolRetryDeciderFunc adapts a function into a ToolRetryDecider.
type ToolRetryDeciderFunc func(context.Context, ToolRetryRequest) (ToolRetryDecision, error)

// DecideToolRetry calls the wrapped function.
func (f ToolRetryDeciderFunc) DecideToolRetry(ctx context.Context, request ToolRetryRequest) (ToolRetryDecision, error) {
	if f == nil {
		return ToolRetryDecision{}, ErrToolRetryDeciderRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ToolRetryDecision{}, err
	}
	return f(ctx, copyToolRetryRequest(request))
}

// FixedToolRetryBackoff returns a backoff function that always yields delay.
func FixedToolRetryBackoff(delay time.Duration) ToolRetryBackoff {
	if delay < 0 {
		delay = 0
	}
	return func(ToolRetryRequest) time.Duration {
		return delay
	}
}

// DecideToolRetry applies policy to an already-failed tool attempt.
func DecideToolRetry(request ToolRetryRequest, policy ToolRetryPolicy) (ToolRetryDecision, error) {
	request = copyToolRetryRequest(request)
	decision := ToolRetryDecision{
		Action:         ToolRetryStop,
		Attempt:        request.Attempt,
		IdempotencyKey: toolRetryIdempotencyKey(request),
		Metadata:       toolRetryMetadata(request, policy),
	}
	if request.Err == nil {
		decision.Reason = "tool call did not fail"
		return decision, nil
	}
	if request.Attempt <= 0 {
		return ToolRetryDecision{}, ErrToolRetryAttemptRequired
	}
	if policy.MaxAttempts <= 0 {
		decision.Reason = "tool retry disabled"
		return decision, nil
	}
	if errors.Is(request.Err, context.Canceled) || errors.Is(request.Err, context.DeadlineExceeded) {
		decision.Reason = "context ended"
		return decision, nil
	}
	if policy.RetryIf != nil && !policy.RetryIf(request.Err) {
		decision.Reason = "tool error is not retryable"
		return decision, nil
	}
	if request.Attempt >= policy.MaxAttempts {
		decision.Reason = fmt.Sprintf("max attempts reached: %d", policy.MaxAttempts)
		return decision, nil
	}
	if decision.IdempotencyKey == "" && !policy.RetryNonIdempotent {
		decision.Reason = "tool retry requires idempotency key"
		return decision, nil
	}

	decision.Action = ToolRetryRetry
	decision.NextAttempt = request.Attempt + 1
	decision.Delay = toolRetryDelay(request, policy)
	if decision.IdempotencyKey != "" {
		decision.Reason = "idempotent tool may be retried"
	} else {
		decision.Reason = "non-idempotent retry explicitly allowed"
	}
	return decision, nil
}

// ToolRetryMiddleware retries failed downstream tool invocations when decider allows it.
func ToolRetryMiddleware(decider ToolRetryDecider) ToolHandler {
	return func(c *ToolContext) error {
		if decider == nil {
			return ErrToolRetryDeciderRequired
		}
		if c == nil {
			c = NewToolContext(context.TODO(), ToolContextOptions{})
		}
		if c.Context == nil {
			c.Context = context.TODO()
		}
		state := captureToolRetryState(c)
		attempt := 1
		for {
			restoreToolRetryState(c, state)
			err := c.Next()
			if err == nil {
				c.Err = state.err
				return nil
			}
			request := ToolRetryRequest{
				ToolName: c.Name,
				Attempt:  attempt,
				Err:      err,
				Result:   copyToolResult(c.Result),
				Effects:  copyEffectRecords(c.Effects),
				Metadata: copyAnyMap(c.Metadata),
			}
			decision, decisionErr := decider.DecideToolRetry(c.Context, request)
			if decisionErr != nil {
				return fmt.Errorf("gopact: tool retry decision: %w", decisionErr)
			}
			if decision.Action != ToolRetryRetry {
				return err
			}
			if err := waitToolRetryDelay(c.Context, decision.Delay); err != nil {
				return fmt.Errorf("gopact: tool retry wait: %w", err)
			}
			attempt++
		}
	}
}

type toolRetryState struct {
	index    int
	name     string
	spec     ToolSpec
	ids      RuntimeIDs
	args     json.RawMessage
	result   ToolResult
	effects  []EffectRecord
	events   []Event
	metadata map[string]any
	err      error
}

func captureToolRetryState(c *ToolContext) toolRetryState {
	return toolRetryState{
		index:    c.index,
		name:     c.Name,
		spec:     copyToolSpec(c.Spec),
		ids:      c.IDs,
		args:     append(json.RawMessage(nil), c.Args...),
		result:   copyToolResult(c.Result),
		effects:  copyEffectRecords(c.Effects),
		events:   copyEvents(c.Events),
		metadata: copyAnyMap(c.Metadata),
		err:      c.Err,
	}
}

func restoreToolRetryState(c *ToolContext, state toolRetryState) {
	c.index = state.index
	c.Name = state.name
	c.Spec = copyToolSpec(state.spec)
	c.IDs = state.ids
	c.Args = append(json.RawMessage(nil), state.args...)
	c.Result = copyToolResult(state.result)
	c.Effects = copyEffectRecords(state.effects)
	c.Events = copyEvents(state.events)
	c.Metadata = copyAnyMap(state.metadata)
	c.Err = state.err
}

func copyToolSpec(spec ToolSpec) ToolSpec {
	spec.InputSchema = copyJSONSchema(spec.InputSchema)
	return spec
}

func waitToolRetryDelay(ctx context.Context, delay time.Duration) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func toolRetryDelay(request ToolRetryRequest, policy ToolRetryPolicy) time.Duration {
	if policy.Backoff == nil {
		return 0
	}
	delay := policy.Backoff(copyToolRetryRequest(request))
	if delay < 0 {
		return 0
	}
	return delay
}

func toolRetryIdempotencyKey(request ToolRetryRequest) string {
	if request.IdempotencyKey != "" {
		return request.IdempotencyKey
	}
	if key := effectRetryIdempotencyKey(request.Effects); key != "" {
		return key
	}
	return effectRetryIdempotencyKey(request.Result.Effects)
}

func effectRetryIdempotencyKey(effects []EffectRecord) string {
	for _, effect := range effects {
		if effect.ReplayPolicy == EffectReplayIdempotent && effect.IdempotencyKey != "" {
			return effect.IdempotencyKey
		}
	}
	return ""
}

func toolRetryMetadata(request ToolRetryRequest, policy ToolRetryPolicy) map[string]any {
	metadata := copyAnyMap(policy.Metadata)
	for key, value := range request.Metadata {
		if metadata == nil {
			metadata = make(map[string]any, len(request.Metadata))
		}
		metadata[key] = value
	}
	if request.ToolName != "" {
		if metadata == nil {
			metadata = make(map[string]any, 1)
		}
		metadata["tool_name"] = request.ToolName
	}
	return metadata
}

func copyToolRetryRequest(in ToolRetryRequest) ToolRetryRequest {
	out := in
	out.Effects = copyEffectRecords(in.Effects)
	out.Result = in.Result
	out.Result.Artifacts = copyArtifactRefs(in.Result.Artifacts)
	out.Result.Effects = copyEffectRecords(in.Result.Effects)
	out.Result.Events = copyEvents(in.Result.Events)
	out.Result.Metadata = copyAnyMap(in.Result.Metadata)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}
