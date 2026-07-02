package repositorychecks

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact/gopacttest"
)

func TestFeatureCoverageMatrixDocumentsCoreCapabilities(t *testing.T) {
	matrix := readTextFile(t, "doc/FEATURES.md")
	readme := readTextFile(t, "README.md")
	index := readTextFile(t, filepath.Join("doc", "design", "index.md"))

	for _, indexed := range []struct {
		path string
		body string
	}{
		{path: "README.md", body: readme},
		{path: filepath.Join("doc", "design", "index.md"), body: index},
	} {
		if !strings.Contains(indexed.body, "doc/FEATURES.md") {
			t.Fatalf("%s must link to FEATURES.md", indexed.path)
		}
	}

	tests := []struct {
		capability string
		path       string
		command    string
		boundary   string
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
			boundary:   "readiness-gated HTTP discovery",
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
			for _, want := range []string{tt.capability, tt.path, tt.command, tt.boundary} {
				if want == "" {
					continue
				}
				if !strings.Contains(matrix, want) {
					t.Fatalf("FEATURES.md missing %q", want)
				}
			}
		})
	}
}

func TestSelfBootstrapReleaseGateTracksFeatureCoverageCommands(t *testing.T) {
	var featureCoverageRequirement gopacttest.VerificationEvidenceRequirement
	for _, requirement := range gopacttest.SelfBootstrapReleaseGateRequirements() {
		if requirement.Name == "self-bootstrap-feature-coverage" {
			featureCoverageRequirement = requirement
			break
		}
	}
	if featureCoverageRequirement.Name == "" {
		t.Fatal("self-bootstrap release gate missing self-bootstrap-feature-coverage requirement")
	}
	if !slices.Contains(featureCoverageRequirement.RequiredEvidenceTypes, gopacttest.VerificationEvidenceTypeCommand) {
		t.Fatalf(
			"self-bootstrap-feature-coverage evidence types = %v, want command evidence",
			featureCoverageRequirement.RequiredEvidenceTypes,
		)
	}

	for _, command := range featureCoverageCommands() {
		id := "command:" + command
		if !slices.Contains(featureCoverageRequirement.RequiredCheckIDs, id) {
			t.Fatalf("self-bootstrap-feature-coverage required check IDs missing %q", id)
		}
	}
}

func featureCoverageCommands() []string {
	return []string{
		"go test -count=1 ./graph ./gopacttest/graphconformance",
		"go test -count=1 ./checkpoint ./gopacttest/checkpointconformance",
		"go test -count=1 . ./provider ./gopacttest/providerconformance",
		"go test -count=1 ./tools ./gopacttest/toolconformance",
		"go test -count=1 ./mcp",
		"go test -count=1 ./a2a ./gopacttest/a2aconformance",
		"go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a",
		"go test -count=1 ./cmd/gopact",
		"go test -count=1 -run Channel . ./gopacttest",
		"go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance",
		"go test -count=1 ./gopacttest",
		"go test -count=1 -run SelfBootstrap ./gopacttest",
	}
}
