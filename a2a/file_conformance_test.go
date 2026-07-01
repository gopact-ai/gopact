package a2a_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
	"github.com/gopact-ai/gopact/gopacttest/a2aconformance"
)

func TestFileDiscovererSatisfiesDiscovererConformance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`[
		{
			"name": "reviewer",
			"url": "http://127.0.0.1:8080",
			"capabilities": ["code.review"],
			"metadata": {"region": "local"}
		}
	]`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := a2a.NewFileDiscoverer(path)
	if err != nil {
		t.Fatalf("NewFileDiscoverer() error = %v", err)
	}

	a2aconformance.RequireDiscovererConformance(t, a2aconformance.DiscovererConformanceHarness{
		Discoverer: discoverer,
		Query: a2a.DiscoveryQuery{
			Name:     "reviewer",
			Require:  []string{"code.review"},
			Metadata: map[string]any{"region": "local"},
		},
		ExpectedCard: a2a.AgentCard{
			Name:         "reviewer",
			URL:          "http://127.0.0.1:8080",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"region": "local"},
		},
		RequireListCards: true,
	})
}
