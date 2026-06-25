package gopact

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDecideToolRetryRetriesIdempotentToolWithinLimit(t *testing.T) {
	boom := errors.New("temporary upstream failure")

	decision, err := DecideToolRetry(ToolRetryRequest{
		ToolName: "local.apply_patch",
		Attempt:  1,
		Err:      boom,
		Effects: []EffectRecord{{
			ID:             "tool-call-1",
			Type:           "tool_call",
			ReplayPolicy:   EffectReplayIdempotent,
			IdempotencyKey: "tool:local.apply_patch:tool-call-1",
		}},
		Metadata: map[string]any{"phase": "apply"},
	}, ToolRetryPolicy{
		MaxAttempts: 3,
		Backoff:     FixedToolRetryBackoff(25 * time.Millisecond),
		Metadata:    map[string]any{"policy": "unit"},
	})
	if err != nil {
		t.Fatalf("DecideToolRetry() error = %v", err)
	}
	if decision.Action != ToolRetryRetry || decision.NextAttempt != 2 {
		t.Fatalf("decision = %+v, want retry next attempt 2", decision)
	}
	if decision.Delay != 25*time.Millisecond {
		t.Fatalf("decision.Delay = %s, want 25ms", decision.Delay)
	}
	if decision.IdempotencyKey != "tool:local.apply_patch:tool-call-1" {
		t.Fatalf("decision.IdempotencyKey = %q", decision.IdempotencyKey)
	}
	if decision.Metadata["policy"] != "unit" || decision.Metadata["phase"] != "apply" {
		t.Fatalf("decision metadata = %+v, want policy and request metadata", decision.Metadata)
	}
}

func TestDecideToolRetryStopsNonIdempotentByDefault(t *testing.T) {
	decision, err := DecideToolRetry(ToolRetryRequest{
		ToolName: "local.create_issue",
		Attempt:  1,
		Err:      errors.New("timeout after side effect may have happened"),
		Effects: []EffectRecord{{
			ID:           "tool-call-1",
			Type:         "tool_call",
			ReplayPolicy: EffectReplayRecordOnly,
		}},
	}, ToolRetryPolicy{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("DecideToolRetry() error = %v", err)
	}
	if decision.Action != ToolRetryStop {
		t.Fatalf("decision.Action = %q, want stop for non-idempotent tool", decision.Action)
	}
	if decision.Reason == "" {
		t.Fatalf("decision.Reason is empty, want non-idempotent reason")
	}
}

func TestDecideToolRetryStopsAtMaxAttemptsAndContextCancellation(t *testing.T) {
	tests := []struct {
		name    string
		request ToolRetryRequest
		policy  ToolRetryPolicy
	}{
		{
			name: "max attempts reached",
			request: ToolRetryRequest{
				ToolName:       "local.search",
				Attempt:        3,
				Err:            errors.New("still failing"),
				IdempotencyKey: "search:query",
			},
			policy: ToolRetryPolicy{MaxAttempts: 3},
		},
		{
			name: "context canceled",
			request: ToolRetryRequest{
				ToolName:       "local.search",
				Attempt:        1,
				Err:            context.Canceled,
				IdempotencyKey: "search:query",
			},
			policy: ToolRetryPolicy{MaxAttempts: 3},
		},
		{
			name: "context deadline exceeded",
			request: ToolRetryRequest{
				ToolName:       "local.search",
				Attempt:        1,
				Err:            context.DeadlineExceeded,
				IdempotencyKey: "search:query",
			},
			policy: ToolRetryPolicy{MaxAttempts: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := DecideToolRetry(tt.request, tt.policy)
			if err != nil {
				t.Fatalf("DecideToolRetry() error = %v", err)
			}
			if decision.Action != ToolRetryStop {
				t.Fatalf("decision.Action = %q, want stop", decision.Action)
			}
		})
	}
}

func TestDecideToolRetryRejectsInvalidAttempt(t *testing.T) {
	_, err := DecideToolRetry(ToolRetryRequest{
		ToolName:       "local.search",
		Err:            errors.New("failed"),
		IdempotencyKey: "search:query",
	}, ToolRetryPolicy{MaxAttempts: 3})
	if !errors.Is(err, ErrToolRetryAttemptRequired) {
		t.Fatalf("DecideToolRetry() error = %v, want ErrToolRetryAttemptRequired", err)
	}
}

func TestToolRetryDeciderFunc(t *testing.T) {
	want := ToolRetryDecision{Action: ToolRetryStop, Reason: "custom"}
	decider := ToolRetryDeciderFunc(func(_ context.Context, request ToolRetryRequest) (ToolRetryDecision, error) {
		if request.ToolName != "local.search" {
			t.Fatalf("request.ToolName = %q, want local.search", request.ToolName)
		}
		return want, nil
	})

	got, err := decider.DecideToolRetry(context.Background(), ToolRetryRequest{ToolName: "local.search"})
	if err != nil {
		t.Fatalf("DecideToolRetry() error = %v", err)
	}
	if got.Action != want.Action || got.Reason != want.Reason {
		t.Fatalf("decision = %+v, want %+v", got, want)
	}
}
