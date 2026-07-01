package a2a

import (
	"os"
	"strings"
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

func splitEnvList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
