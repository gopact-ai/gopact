package a2a

import "context"

// StaticDiscoverer looks up agent cards from an in-memory list.
type StaticDiscoverer struct {
	cards []AgentCard
}

var _ Discoverer = (*StaticDiscoverer)(nil)
var _ CardLister = (*StaticDiscoverer)(nil)

// NewStaticDiscoverer creates an in-memory agent card discoverer.
func NewStaticDiscoverer(cards ...AgentCard) *StaticDiscoverer {
	d := &StaticDiscoverer{cards: make([]AgentCard, len(cards))}
	for i, card := range cards {
		d.cards[i] = copyAgentCard(card)
	}
	return d
}

// ListCards returns all static cards in configured order.
func (d *StaticDiscoverer) ListCards(ctx context.Context) ([]AgentCard, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, ErrAgentNotFound
	}
	cards := make([]AgentCard, 0, len(d.cards))
	for _, card := range d.cards {
		if card.Name == "" {
			return nil, ErrCardNameRequired
		}
		cards = append(cards, copyAgentCard(card))
	}
	return cards, nil
}

// Discover returns the first static card matching the discovery query.
func (d *StaticDiscoverer) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	if !hasDiscoveryCriteria(query) {
		return DiscoveryResult{}, ErrDiscoveryRequired
	}
	if d == nil {
		return DiscoveryResult{}, ErrAgentNotFound
	}
	// ponytail: linear scan is enough for local/static discovery; external registries own large catalogs.
	for _, card := range d.cards {
		if !matchesDiscoveryQuery(card, query) {
			continue
		}
		if card.Name == "" {
			return DiscoveryResult{}, ErrCardNameRequired
		}
		return DiscoveryResult{
			Card:     copyAgentCard(card),
			Metadata: map[string]any{"source": "static"},
		}, nil
	}
	return DiscoveryResult{}, ErrAgentNotFound
}
