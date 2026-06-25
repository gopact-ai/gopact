package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Registry stores named model providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds a provider by its stable name.
func (r *Registry) Register(ctx context.Context, provider Provider) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if provider == nil {
		return errors.New("provider: provider is nil")
	}
	name := provider.Name()
	if name == "" {
		return errors.New("provider: provider name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	if _, ok := r.providers[name]; ok {
		return fmt.Errorf("%w: %s", ErrProviderExists, name)
	}
	r.providers[name] = provider
	return nil
}

// Resolve returns a provider by name.
func (r *Registry) Resolve(ctx context.Context, name string) (Provider, bool) {
	if r == nil || ctx.Err() != nil {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.providers[name]
	return provider, ok
}

// List returns registered provider information ordered by name.
func (r *Registry) List(ctx context.Context) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("provider: registry is nil")
	}

	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()

	infos := make([]Info, 0, len(providers))
	for _, provider := range providers {
		models, err := provider.Models(ctx)
		if err != nil {
			return nil, fmt.Errorf("provider: list provider %q models: %w", provider.Name(), err)
		}
		infos = append(infos, Info{Name: provider.Name(), Models: models})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}
