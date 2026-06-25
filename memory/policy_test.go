package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyStoreDenySkipsPut(t *testing.T) {
	ctx := context.Background()
	base := New()
	var calls int
	policy := gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		calls++
		if req.Boundary != gopact.PolicyBoundaryMemory {
			t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryMemory)
		}
		if req.Action != gopact.PolicyActionPut {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionPut)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if input.Memory.Content != "secret preference" {
			t.Fatalf("memory content = %q, want secret preference", input.Memory.Content)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "memory blocked"}, nil
	})
	store, err := NewPolicyStore(base, policy, WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-1"}))
	if err != nil {
		t.Fatalf("NewPolicyStore() error = %v", err)
	}

	_, err = store.Put(ctx, Memory{Type: TypeSemantic, Content: "secret preference"})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Put() error = %v, want ErrPolicyDenied", err)
	}
	if calls != 1 {
		t.Fatalf("policy calls = %d, want 1", calls)
	}
	results, err := base.Search(ctx, Query{Text: "secret"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results.Memories) != 0 {
		t.Fatalf("base memories = %+v, want none", results.Memories)
	}
}

func TestPolicyStoreReviewReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	base := New()
	id, err := base.Put(ctx, Memory{Type: TypeSemantic, Content: "keep"})
	if err != nil {
		t.Fatalf("seed Put() error = %v", err)
	}
	store, err := NewPolicyStore(base, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionDelete {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionDelete)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review delete"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyStore() error = %v", err)
	}

	err = store.Delete(ctx, id)
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Delete() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Delete() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryMemory) {
		t.Fatalf("RequiredBy = %q, want memory", interruptErr.Record.RequiredBy)
	}
	if _, err := base.Get(ctx, id); err != nil {
		t.Fatalf("base Get() after review error = %v", err)
	}
}

func TestPolicyStorePublishesPolicyEvents(t *testing.T) {
	ctx := context.Background()
	var events []gopact.Event
	store, err := NewPolicyStore(New(), gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
	}), WithPolicyEventSink(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyStore() error = %v", err)
	}

	if _, err := store.Put(ctx, Memory{Type: TypeSemantic, Content: "evented"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 events", events)
	}
	if events[0].Type != gopact.EventPolicyRequested || events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %q, %q", events[0].Type, events[1].Type)
	}
	if events[1].PolicyDecision == nil || events[1].PolicyDecision.Action != gopact.PolicyAllow {
		t.Fatalf("decision event = %+v, want allow decision", events[1])
	}
}
