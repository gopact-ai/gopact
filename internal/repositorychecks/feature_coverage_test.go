package repositorychecks

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFeatureCoverageMatrixDocumentsCoreCapabilities(t *testing.T) {
	matrix := readTextFile(t, "FEATURES.md")
	readme := readTextFile(t, "README.md")
	index := readTextFile(t, filepath.Join("docs", "design", "index.md"))

	for _, indexed := range []struct {
		path string
		body string
	}{
		{path: "README.md", body: readme},
		{path: filepath.Join("docs", "design", "index.md"), body: index},
	} {
		if !strings.Contains(indexed.body, "FEATURES.md") {
			t.Fatalf("%s must link to FEATURES.md", indexed.path)
		}
	}

	tests := []struct {
		capability string
		path       string
		command    string
	}{
		{
			capability: "workflow graph execution",
			path:       "graph",
			command:    "go test -count=1 ./graph ./gopacttest/graphconformance",
		},
		{
			capability: "checkpoint and resume",
			path:       "checkpoint",
			command:    "go test -count=1 ./checkpoint ./gopacttest/checkpointconformance",
		},
		{
			capability: "provider-neutral model contract",
			path:       "model.go",
			command:    "go test -count=1 . ./provider ./gopacttest/providerconformance",
		},
		{
			capability: "tool registry and replay",
			path:       "tools",
			command:    "go test -count=1 ./tools ./gopacttest/toolconformance",
		},
		{
			capability: "MCP client/server contracts",
			path:       "mcp",
			command:    "go test -count=1 ./mcp",
		},
		{
			capability: "A2A agent mesh",
			path:       "a2a",
			command:    "go test -count=1 ./a2a ./gopacttest/a2aconformance",
		},
		{
			capability: "A2A HTTP registry discovery",
			path:       "a2a/http_example_test.go",
			command:    "go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a",
		},
		{
			capability: "agent scaffold generator",
			path:       "cmd/gopact",
			command:    "go test -count=1 ./cmd/gopact",
		},
		{
			capability: "channel and surface transfer",
			path:       "channel_policy.go",
			command:    "go test -count=1 -run Channel . ./gopacttest",
		},
		{
			capability: "policy, redaction, and safety contracts",
			path:       "policy.go",
			command:    "go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance",
		},
		{
			capability: "verification evidence and release gate",
			path:       "gopacttest",
			command:    "go test -count=1 ./gopacttest",
		},
		{
			capability: "self-bootstrap release bundle",
			path:       "gopacttest",
			command:    "go test -count=1 -run SelfBootstrap ./gopacttest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.capability, func(t *testing.T) {
			for _, want := range []string{tt.capability, tt.path, tt.command} {
				if !strings.Contains(matrix, want) {
					t.Fatalf("FEATURES.md missing %q", want)
				}
			}
		})
	}
}
