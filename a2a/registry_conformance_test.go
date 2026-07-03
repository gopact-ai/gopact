package a2a_test

import (
	"testing"
	"time"

	"github.com/gopact-ai/gopact/a2a"
	"github.com/gopact-ai/gopact/gopacttest/a2aconformance"
)

func TestRegistrySatisfiesCardRegistrarConformance(t *testing.T) {
	a2aconformance.RequireCardRegistrarConformance(t, a2aconformance.CardRegistrarConformanceHarness{
		Registrar: a2a.NewRegistry(),
		Card: a2a.AgentCard{
			Name:         "reviewer",
			URL:          "http://127.0.0.1:8080",
			Capabilities: []string{"code.review"},
			Tags:         []string{"code"},
			Metadata:     map[string]any{"region": "local"},
		},
		TTL: time.Minute,
	})
}
