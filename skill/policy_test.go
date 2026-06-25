package skill

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyRegistryDenySkipsRegister(t *testing.T) {
	ctx := context.Background()
	base := NewRegistry()
	var calls int
	registry, err := NewPolicyRegistry(
		base,
		gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			calls++
			if req.Boundary != gopact.PolicyBoundarySkill {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundarySkill)
			}
			if req.Action != gopact.PolicyActionCreate {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionCreate)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindSkill || input.Name != "repo-review" {
				t.Fatalf("policy input = %+v, want skill repo-review", input)
			}
			if input.Skill.Metadata["secret"] != "redacted-by-policy" {
				t.Fatalf("metadata = %+v, want copied skill metadata", input.Skill.Metadata)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "skill blocked"}, nil
		}),
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-1"}),
	)
	if err != nil {
		t.Fatalf("NewPolicyRegistry() error = %v", err)
	}

	err = registry.Register(ctx, Skill{
		Name:     "repo-review",
		Metadata: map[string]any{"secret": "redacted-by-policy"},
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Register() error = %v, want ErrPolicyDenied", err)
	}
	if calls != 1 {
		t.Fatalf("policy calls = %d, want 1", calls)
	}
	if _, err := base.Get(ctx, "repo-review"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("base Get() error = %v, want ErrNotFound", err)
	}
}

func TestPolicyRegistryPublishesEventsAndAllowsSearch(t *testing.T) {
	ctx := context.Background()
	base := NewRegistry()
	if err := base.Register(ctx, Skill{Name: "repo-review", Description: "reviews repository changes"}); err != nil {
		t.Fatalf("seed Register() error = %v", err)
	}
	var events []gopact.Event
	registry, err := NewPolicyRegistry(
		base,
		gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Boundary != gopact.PolicyBoundarySkill {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundarySkill)
			}
			if req.Action != gopact.PolicyActionSearch {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionSearch)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Query.Text != "review" || input.Query.Limit != 5 {
				t.Fatalf("query = %+v, want review limit 5", input.Query)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
		}),
		WithPolicyEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyRegistry() error = %v", err)
	}

	results, err := registry.Search(ctx, Query{Text: "review", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := skillNames(results); len(got) != 1 || got[0] != "repo-review" {
		t.Fatalf("Search() names = %v, want [repo-review]", got)
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

func TestPolicyRegistryReviewActivateReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	base := NewRegistry()
	if err := base.Register(ctx, Skill{Name: "repo-review"}); err != nil {
		t.Fatalf("seed Register() error = %v", err)
	}
	registry, err := NewPolicyRegistry(base, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionActivate {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionActivate)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review skill activation"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyRegistry() error = %v", err)
	}

	_, err = registry.Activate(ctx, "repo-review")
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Activate() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Activate() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundarySkill) {
		t.Fatalf("RequiredBy = %q, want skill", interruptErr.Record.RequiredBy)
	}
}

func TestNewPolicyRegistryRequiresDependencies(t *testing.T) {
	if _, err := NewPolicyRegistry(nil, gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})); !errors.Is(err, ErrRegistryRequired) {
		t.Fatalf("NewPolicyRegistry(nil, policy) error = %v, want ErrRegistryRequired", err)
	}
	if _, err := NewPolicyRegistry(NewRegistry(), nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyRegistry(registry, nil) error = %v, want ErrPolicyRequired", err)
	}
}
