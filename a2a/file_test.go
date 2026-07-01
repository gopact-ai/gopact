package a2a

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileDiscovererFindsAgentCardByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{
		"agents": [
			{
				"name": "planner",
				"description": "plans tasks",
				"url": "http://127.0.0.1:8081",
				"capabilities": ["planning"],
				"metadata": {"owner": "agents"}
			}
		]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{Name: "planner"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "planner" ||
		result.Card.Description != "plans tasks" ||
		result.Card.URL != "http://127.0.0.1:8081" ||
		result.Card.Metadata["owner"] != "agents" ||
		result.Metadata["source"] != "file" {
		t.Fatalf("Discover() = %+v, want planner card from file", result)
	}
}

func TestFileDiscovererAcceptsBareAgentCardArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`[
		{"name": "planner", "capabilities": ["planning"]},
		{"name": "reviewer", "capabilities": ["code.review"]}
	]`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

	cards, err := discoverer.ListCards(context.Background())
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "planner" || cards[1].Name != "reviewer" {
		t.Fatalf("ListCards() = %+v, want bare array order", cards)
	}
}

func TestFileDiscovererListCardsReturnsOrderedDefensiveCopies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{
		"agents": [
			{"name": "planner", "capabilities": ["planning"], "metadata": {"owner": "agents"}},
			{"name": "reviewer", "capabilities": ["code.review"], "metadata": {"owner": "review"}}
		]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

	cards, err := discoverer.ListCards(context.Background())
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "planner" || cards[1].Name != "reviewer" {
		t.Fatalf("ListCards() = %+v, want file order", cards)
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

func TestFileDiscovererListCardsRejectsMissingName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"agents":[{"url":"http://127.0.0.1:8081"}]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

	if _, err := discoverer.ListCards(context.Background()); !errors.Is(err, ErrCardNameRequired) {
		t.Fatalf("ListCards() error = %v, want %v", err, ErrCardNameRequired)
	}
}

func TestFileDiscovererFindsAgentCardByURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{
		"agents": [
			{"name": "planner", "url": "http://127.0.0.1:8081"},
			{"name": "reviewer", "url": "http://127.0.0.1:8082"}
		]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{URL: "http://127.0.0.1:8082"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover() = %+v, want reviewer card", result.Card)
	}
}

func TestFileDiscovererFindsAgentCardByMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{
		"agents": [
			{"name": "researcher", "metadata": {"domain": "research", "tier": "gold"}},
			{"name": "reviewer", "metadata": {"domain": "code", "tier": "gold"}}
		]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

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

func TestFileDiscovererFindsAgentCardByCapability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{
		"agents": [
			{"name": "researcher", "capabilities": ["web.search", "summarize"]},
			{"name": "reviewer", "capabilities": ["code.review", "git.diff"]}
		]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

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

func TestFileDiscovererRejectsInvalidInputs(t *testing.T) {
	if _, err := NewFileDiscoverer(""); err == nil {
		t.Fatal("NewFileDiscoverer() error = nil, want path error")
	}

	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"agents":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}
	if _, err := discoverer.Discover(context.Background(), DiscoveryQuery{}); !errors.Is(err, ErrDiscoveryRequired) {
		t.Fatalf("Discover() error = %v, want %v", err, ErrDiscoveryRequired)
	}
	if _, err := discoverer.Discover(context.Background(), DiscoveryQuery{Name: "missing"}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Discover() error = %v, want %v", err, ErrAgentNotFound)
	}
}
