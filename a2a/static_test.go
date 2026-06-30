package a2a

import (
	"context"
	"errors"
	"testing"
)

func TestStaticDiscovererFindsAgentCard(t *testing.T) {
	discoverer := NewStaticDiscoverer(
		AgentCard{
			Name:         "planner",
			Description:  "plans tasks",
			URL:          "http://127.0.0.1:8081",
			Capabilities: []string{"planning"},
			Metadata:     map[string]any{"owner": "agents"},
		},
		AgentCard{Name: "reviewer", URL: "http://127.0.0.1:8082"},
	)

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{Name: "planner"})
	if err != nil {
		t.Fatalf("Discover(name) error = %v", err)
	}
	if result.Card.Name != "planner" ||
		result.Card.Description != "plans tasks" ||
		result.Card.Metadata["owner"] != "agents" ||
		result.Metadata["source"] != "static" {
		t.Fatalf("Discover(name) = %+v, want planner card", result)
	}

	result, err = discoverer.Discover(context.Background(), DiscoveryQuery{URL: "http://127.0.0.1:8082"})
	if err != nil {
		t.Fatalf("Discover(url) error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover(url) = %+v, want reviewer card", result.Card)
	}
}

func TestStaticDiscovererListCardsReturnsOrderedDefensiveCopies(t *testing.T) {
	discoverer := NewStaticDiscoverer(
		AgentCard{
			Name:         "planner",
			Capabilities: []string{"planning"},
			Metadata:     map[string]any{"owner": "agents"},
		},
		AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"owner": "review"},
		},
	)

	cards, err := discoverer.ListCards(context.Background())
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "planner" || cards[1].Name != "reviewer" {
		t.Fatalf("ListCards() = %+v, want static order", cards)
	}
	cards[0].Capabilities[0] = "mutated"
	cards[0].Metadata["owner"] = "mutated"

	cards, err = discoverer.ListCards(context.Background())
	if err != nil {
		t.Fatalf("ListCards() after mutation error = %v", err)
	}
	if cards[0].Capabilities[0] != "planning" || cards[0].Metadata["owner"] != "agents" {
		t.Fatalf("ListCards() = %+v, want defensive copies", cards)
	}
}

func TestStaticDiscovererFindsAgentCardByMetadata(t *testing.T) {
	discoverer := NewStaticDiscoverer(
		AgentCard{Name: "researcher", Metadata: map[string]any{"domain": "research", "tier": "gold"}},
		AgentCard{Name: "reviewer", Metadata: map[string]any{"domain": "code", "tier": "gold"}},
	)

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{
		Metadata: map[string]any{"domain": "code"},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover() = %+v, want reviewer card", result.Card)
	}

	_, err = discoverer.Discover(context.Background(), DiscoveryQuery{
		Name:     "reviewer",
		Metadata: map[string]any{"domain": "research"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Discover() mismatched metadata error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestStaticDiscovererFindsAgentCardByCapability(t *testing.T) {
	discoverer := NewStaticDiscoverer(
		AgentCard{Name: "researcher", Capabilities: []string{"web.search", "summarize"}},
		AgentCard{Name: "reviewer", Capabilities: []string{"code.review", "git.diff"}},
	)

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{
		Require: []string{"code.review", "git.diff"},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover() = %+v, want reviewer card", result.Card)
	}

	_, err = discoverer.Discover(context.Background(), DiscoveryQuery{
		Name:    "reviewer",
		Require: []string{"web.search"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Discover() mismatched capability error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestStaticDiscovererReturnsDefensiveCopies(t *testing.T) {
	discoverer := NewStaticDiscoverer(AgentCard{
		Name:         "planner",
		Capabilities: []string{"planning"},
		Metadata:     map[string]any{"owner": "agents"},
	})

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{Name: "planner"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	result.Card.Capabilities[0] = "mutated"
	result.Card.Metadata["owner"] = "mutated"

	result, err = discoverer.Discover(context.Background(), DiscoveryQuery{Name: "planner"})
	if err != nil {
		t.Fatalf("Discover() after mutation error = %v", err)
	}
	if result.Card.Capabilities[0] != "planning" || result.Card.Metadata["owner"] != "agents" {
		t.Fatalf("Discover() card = %+v, want defensive copy", result.Card)
	}
}

func TestStaticDiscovererRejectsInvalidInputs(t *testing.T) {
	discoverer := NewStaticDiscoverer()
	if _, err := discoverer.Discover(context.Background(), DiscoveryQuery{}); !errors.Is(err, ErrDiscoveryRequired) {
		t.Fatalf("Discover() error = %v, want %v", err, ErrDiscoveryRequired)
	}
	if _, err := discoverer.Discover(context.Background(), DiscoveryQuery{Name: "missing"}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Discover() error = %v, want %v", err, ErrAgentNotFound)
	}
}
