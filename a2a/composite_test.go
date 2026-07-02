package a2a

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCompositeDiscovererListsAndDiscoversAcrossSources(t *testing.T) {
	discoverer := NewCompositeDiscoverer(
		NewStaticDiscoverer(AgentCard{
			Name:     "planner",
			Tags:     []string{"planning"},
			Metadata: map[string]any{"domain": "dev"},
		}),
		NewStaticDiscoverer(AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "dev"},
		}),
	)

	cards, err := discoverer.ListCards(context.Background())
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if got, want := cardNames(cards), []string{"planner", "reviewer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListCards() names = %v, want %v", got, want)
	}
	cards[0].Metadata["domain"] = "mutated"

	again, err := discoverer.ListCards(context.Background())
	if err != nil {
		t.Fatalf("ListCards(second) error = %v", err)
	}
	if again[0].Metadata["domain"] != "dev" {
		t.Fatalf("ListCards() returned shared card metadata: %+v", again[0].Metadata)
	}

	result, err := discoverer.Discover(context.Background(), DiscoveryQuery{Require: []string{"code.review"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" || result.Metadata["source"] != "composite" {
		t.Fatalf("Discover() = %+v, want reviewer from composite source", result)
	}
	result.Card.Metadata["domain"] = "mutated"

	result, err = discoverer.Discover(context.Background(), DiscoveryQuery{Name: "reviewer"})
	if err != nil {
		t.Fatalf("Discover(second) error = %v", err)
	}
	if result.Card.Metadata["domain"] != "dev" {
		t.Fatalf("Discover() returned shared card metadata: %+v", result.Card.Metadata)
	}
}

func TestCompositeDiscovererPropagatesListErrors(t *testing.T) {
	wantErr := errors.New("registry down")
	discoverer := NewCompositeDiscoverer(cardListerFunc(func(context.Context) ([]AgentCard, error) {
		return nil, wantErr
	}))

	_, err := discoverer.ListCards(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("ListCards() error = %v, want %v", err, wantErr)
	}

	_, err = discoverer.Discover(context.Background(), DiscoveryQuery{Name: "reviewer"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Discover() error = %v, want %v", err, wantErr)
	}
}

func cardNames(cards []AgentCard) []string {
	names := make([]string, 0, len(cards))
	for _, card := range cards {
		names = append(names, card.Name)
	}
	return names
}
