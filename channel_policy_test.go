package gopact

import (
	"context"
	"errors"
	"iter"
	"testing"
)

func TestPolicyChannelBlocksDeniedSend(t *testing.T) {
	var requests []PolicyRequest
	var events []Event
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		requests = append(requests, req)
		if req.Boundary != PolicyBoundaryChannel {
			t.Fatalf("boundary = %q, want channel", req.Boundary)
		}
		if req.Action != PolicyActionSend {
			t.Fatalf("action = %q, want send", req.Action)
		}
		input, ok := req.Input.(ChannelPolicyInput)
		if !ok {
			t.Fatalf("input type = %T, want ChannelPolicyInput", req.Input)
		}
		if input.Payload.Target != "lark" {
			t.Fatalf("payload target = %q, want lark", input.Payload.Target)
		}
		return PolicyDecision{Action: PolicyDeny, Reason: "external send blocked"}, nil
	})
	sent := false
	channel := ChannelFunc{
		SendFunc: func(ctx context.Context, payload ChannelPayload) error {
			sent = true
			return nil
		},
	}
	wrapped, err := NewPolicyChannel(
		channel,
		policy,
		WithChannelPolicyIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}),
		WithChannelPolicyEventSink(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyChannel() error = %v", err)
	}

	err = wrapped.Send(context.Background(), ChannelPayload{Target: "lark", Data: "card"})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Send() error = %v, want ErrPolicyDenied", err)
	}
	if sent {
		t.Fatal("underlying channel should not send after policy denial")
	}
	if len(requests) != 1 || requests[0].IDs.RunID != "run-1" {
		t.Fatalf("policy requests = %+v", requests)
	}
	if len(events) != 2 || events[0].Type != EventPolicyRequested || events[1].Type != EventPolicyDecided {
		t.Fatalf("policy events = %+v", events)
	}
}

func TestPolicyChannelReviewStopsInboundAction(t *testing.T) {
	var events []Event
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		if req.Boundary != PolicyBoundaryChannel {
			t.Fatalf("boundary = %q, want channel", req.Boundary)
		}
		if req.Action != PolicyActionReceive {
			t.Fatalf("action = %q, want receive", req.Action)
		}
		input, ok := req.Input.(ChannelPolicyInput)
		if !ok {
			t.Fatalf("input type = %T, want ChannelPolicyInput", req.Input)
		}
		if input.Event.Action.Type != SurfaceActionResume {
			t.Fatalf("event action = %+v, want resume", input.Event.Action)
		}
		return PolicyDecision{Action: PolicyReview, Reason: "confirm channel action"}, nil
	})
	channel := ChannelFunc{
		EventsFunc: func(ctx context.Context) iter.Seq2[ChannelEvent, error] {
			return func(yield func(ChannelEvent, error) bool) {
				yield(ChannelEvent{
					ID:      "event-1",
					Channel: "lark",
					Type:    ChannelEventAction,
					IDs:     RuntimeIDs{RunID: "run-1"},
					Action: SurfaceAction{
						Type:        SurfaceActionResume,
						InterruptID: "interrupt-1",
					},
				}, nil)
			}
		},
		SendFunc: func(ctx context.Context, payload ChannelPayload) error { return nil },
	}
	wrapped, err := NewPolicyChannel(
		channel,
		policy,
		WithChannelPolicyEventSink(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyChannel() error = %v", err)
	}

	got, err := collectChannelEvents(wrapped.Events(context.Background()))
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Events() error = %v, want ErrInterrupted", err)
	}
	if len(got) != 0 {
		t.Fatalf("Events() yielded %+v, want none", got)
	}
	var interruptErr *InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Events() error = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(PolicyBoundaryChannel) {
		t.Fatalf("RequiredBy = %q, want channel", interruptErr.Record.RequiredBy)
	}
	if len(events) != 2 || events[0].Type != EventPolicyRequested || events[1].Type != EventPolicyDecided {
		t.Fatalf("policy events = %+v", events)
	}
}

func TestNewPolicyChannelRequiresDependencies(t *testing.T) {
	if _, err := NewPolicyChannel(nil, PolicyFunc(func(context.Context, PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{Action: PolicyAllow}, nil
	})); !errors.Is(err, ErrChannelRequired) {
		t.Fatalf("NewPolicyChannel(nil, policy) error = %v, want ErrChannelRequired", err)
	}
	if _, err := NewPolicyChannel(ChannelFunc{SendFunc: func(context.Context, ChannelPayload) error {
		return nil
	}}, nil); !errors.Is(err, ErrChannelPolicyRequired) {
		t.Fatalf("NewPolicyChannel(channel, nil) error = %v, want ErrChannelPolicyRequired", err)
	}
}
