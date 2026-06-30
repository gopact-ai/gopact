package a2aconformance

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
)

func TestRequireDiscovererConformanceAcceptsStaticDiscoverer(t *testing.T) {
	card := a2a.AgentCard{
		Name:         "reviewer",
		URL:          "http://127.0.0.1:8080",
		Capabilities: []string{"review"},
		Metadata:     map[string]any{"region": "local"},
	}
	discoverer := a2a.NewStaticDiscoverer(card)

	RequireDiscovererConformance(t, DiscovererConformanceHarness{
		Discoverer:       discoverer,
		Query:            a2a.DiscoveryQuery{Name: "reviewer", Require: []string{"review"}, Metadata: map[string]any{"region": "local"}},
		ExpectedCard:     card,
		RequireListCards: true,
	})
}

func TestCheckDiscovererConformanceReportsFailures(t *testing.T) {
	results := CheckDiscovererConformance(context.Background(), DiscovererConformanceHarness{
		Discoverer:       &badDiscoverer{card: a2a.AgentCard{Name: "wrong", Metadata: map[string]any{"region": "local"}}},
		Query:            a2a.DiscoveryQuery{Name: "reviewer", Metadata: map[string]any{"region": "local"}},
		ExpectedCard:     a2a.AgentCard{Name: "reviewer", Metadata: map[string]any{"region": "local"}},
		RequireListCards: true,
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
			if !errors.Is(result.Err, ErrDiscovererConformanceFailed) {
				t.Fatalf("case %q error = %v, want ErrDiscovererConformanceFailed", result.Case, result.Err)
			}
		}
	}
	for _, want := range []string{
		"discover-respects-canceled-context",
		"discover-returns-expected-card",
		"discover-does-not-mutate-query",
		"implements-card-lister",
	} {
		if !failures[want] {
			t.Fatalf("failures = %v, want case %q to fail", failures, want)
		}
	}
}

func TestCheckDiscovererConformanceReportsSharedCardMutation(t *testing.T) {
	discoverer := &sharedCardDiscoverer{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"review"},
			Metadata:     map[string]any{"region": "local"},
		},
	}

	results := CheckDiscovererConformance(context.Background(), DiscovererConformanceHarness{
		Discoverer:   discoverer,
		Query:        a2a.DiscoveryQuery{Name: "reviewer"},
		ExpectedCard: a2a.AgentCard{Name: "reviewer", Capabilities: []string{"review"}, Metadata: map[string]any{"region": "local"}},
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
		}
	}
	if !failures["discover-returns-defensive-copy"] {
		t.Fatalf("failures = %v, want discover-returns-defensive-copy to fail", failures)
	}
}

func TestCheckDiscovererConformanceReportsSharedListCardMutation(t *testing.T) {
	discoverer := &sharedCardListDiscoverer{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"review"},
		},
	}

	results := CheckDiscovererConformance(context.Background(), DiscovererConformanceHarness{
		Discoverer:       discoverer,
		Query:            a2a.DiscoveryQuery{Name: "reviewer"},
		ExpectedCard:     a2a.AgentCard{Name: "reviewer", Capabilities: []string{"review"}},
		RequireListCards: true,
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
		}
	}
	if !failures["list-returns-defensive-copy"] {
		t.Fatalf("failures = %v, want list-returns-defensive-copy to fail", failures)
	}
}

type sharedCardListDiscoverer struct {
	card a2a.AgentCard
}

func (d *sharedCardListDiscoverer) Discover(ctx context.Context, _ a2a.DiscoveryQuery) (a2a.DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return a2a.DiscoveryResult{}, err
	}
	card := d.card
	card.Capabilities = append([]string(nil), d.card.Capabilities...)
	return a2a.DiscoveryResult{Card: card}, nil
}

func (d *sharedCardListDiscoverer) ListCards(ctx context.Context) ([]a2a.AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []a2a.AgentCard{d.card}, nil
}

type badDiscoverer struct {
	card a2a.AgentCard
}

func (d *badDiscoverer) Discover(_ context.Context, query a2a.DiscoveryQuery) (a2a.DiscoveryResult, error) {
	query.Metadata["mutated"] = true
	return a2a.DiscoveryResult{Card: d.card}, nil
}

type sharedCardDiscoverer struct {
	card a2a.AgentCard
}

func (d *sharedCardDiscoverer) Discover(ctx context.Context, _ a2a.DiscoveryQuery) (a2a.DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return a2a.DiscoveryResult{}, err
	}
	return a2a.DiscoveryResult{Card: d.card}, nil
}
