package a2a

import (
	"context"
	"iter"
	"os"
	"strings"
	"time"
)

const (
	EnvA2ARegistryFile = "GOPACT_A2A_REGISTRY_FILE"
	EnvA2ARegistryURL  = "GOPACT_A2A_REGISTRY_URL"
	EnvA2AEndpoints    = "GOPACT_A2A_ENDPOINTS"
)

// NewEnvCardListers builds mesh bootstrap sources from the standard gopact A2A environment variables.
func NewEnvCardListers(lookup func(string) string, opts ...HTTPAgentOption) ([]CardLister, []string, error) {
	if lookup == nil {
		lookup = os.Getenv
	}
	var listers []CardLister
	var sources []string
	if path := strings.TrimSpace(lookup(EnvA2ARegistryFile)); path != "" {
		discoverer, err := NewFileDiscoverer(path)
		if err != nil {
			return nil, nil, err
		}
		listers = append(listers, discoverer)
		sources = append(sources, "file registry")
	}
	if registryURL := strings.TrimSpace(lookup(EnvA2ARegistryURL)); registryURL != "" {
		registry, err := NewHTTPRegistry(registryURL, opts...)
		if err != nil {
			return nil, nil, err
		}
		listers = append(listers, registry)
		sources = append(sources, "HTTP registry")
	}
	endpointListers, err := NewHTTPCardListers(splitEnvList(lookup(EnvA2AEndpoints)), opts...)
	if err != nil {
		return nil, nil, err
	}
	if len(endpointListers) > 0 {
		listers = append(listers, endpointListers...)
		sources = append(sources, "HTTP endpoints")
	}
	return listers, sources, nil
}

// BootstrapEnv imports agent cards from standard gopact A2A environment variables.
func (m *Mesh) BootstrapEnv(ctx context.Context, lookup func(string) string, opts ...HTTPAgentOption) (BootstrapResult, []string, error) {
	listers, sources, err := NewEnvCardListers(lookup, opts...)
	if err != nil {
		return BootstrapResult{}, nil, err
	}
	if len(listers) == 0 {
		return BootstrapResult{}, sources, nil
	}
	result, err := m.withHTTPOptions(opts).Bootstrap(ctx, listers...)
	return result, sources, err
}

// SyncEnv imports cards from standard gopact A2A environment variables, prunes unready agents, and returns the final card snapshot.
func (m *Mesh) SyncEnv(ctx context.Context, lookup func(string) string, opts ...HTTPAgentOption) (SyncResult, error) {
	listers, sources, err := NewEnvCardListers(lookup, opts...)
	if err != nil {
		return SyncResult{}, err
	}
	result, err := m.withHTTPOptions(opts).Sync(ctx, listers...)
	result.Sources = append([]string(nil), sources...)
	if err != nil {
		return result, err
	}
	return result, nil
}

// SyncEnvEvery runs SyncEnv immediately and then again every interval until ctx is canceled.
func (m *Mesh) SyncEnvEvery(ctx context.Context, interval time.Duration, lookup func(string) string, opts ...HTTPAgentOption) iter.Seq2[SyncResult, error] {
	return func(yield func(SyncResult, error) bool) {
		if interval <= 0 {
			yield(SyncResult{}, ErrSyncIntervalRequired)
			return
		}
		if ctx == nil {
			ctx = context.TODO()
		}
		for {
			if err := ctx.Err(); err != nil {
				return
			}
			result, err := m.SyncEnv(ctx, lookup, opts...)
			if !yield(result, err) || err != nil {
				return
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (m *Mesh) withHTTPOptions(opts []HTTPAgentOption) *Mesh {
	if m == nil || len(opts) == 0 {
		return m
	}
	clone := *m
	clone.httpOptions = append(append([]HTTPAgentOption(nil), m.httpOptions...), opts...)
	return &clone
}

func splitEnvList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
