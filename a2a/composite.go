package a2a

import "context"

// CompositeDiscoverer lists and discovers cards from multiple sources in order.
type CompositeDiscoverer struct {
	listers []CardLister
}

var _ Discoverer = (*CompositeDiscoverer)(nil)
var _ CardLister = (*CompositeDiscoverer)(nil)

// NewCompositeDiscoverer combines multiple card listers into one discovery source.
func NewCompositeDiscoverer(listers ...CardLister) *CompositeDiscoverer {
	out := make([]CardLister, len(listers))
	copy(out, listers)
	return &CompositeDiscoverer{listers: out}
}

// ListCards returns cards from all sources in source order.
func (d *CompositeDiscoverer) ListCards(ctx context.Context) ([]AgentCard, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, ErrDiscovererRequired
	}
	var cards []AgentCard
	for _, lister := range d.listers {
		if lister == nil {
			return nil, ErrDiscovererRequired
		}
		listed, err := lister.ListCards(ctx)
		if err != nil {
			return nil, err
		}
		for _, card := range listed {
			if card.Name == "" {
				return nil, ErrCardNameRequired
			}
			cards = append(cards, copyAgentCard(card))
		}
	}
	return cards, nil
}

// Discover returns the first card matching query across all sources.
func (d *CompositeDiscoverer) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	if !hasDiscoveryCriteria(query) {
		return DiscoveryResult{}, ErrDiscoveryRequired
	}
	cards, err := d.ListCards(ctx)
	if err != nil {
		return DiscoveryResult{}, err
	}
	// ponytail: linear scan keeps local/service-discovery composition tiny; registries own indexing if catalogs grow.
	for _, card := range cards {
		if matchesDiscoveryQuery(card, query) {
			return DiscoveryResult{
				Card:     copyAgentCard(card),
				Metadata: map[string]any{"source": "composite"},
			}, nil
		}
	}
	return DiscoveryResult{}, ErrAgentNotFound
}
