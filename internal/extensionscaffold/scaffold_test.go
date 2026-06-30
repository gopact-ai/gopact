package extensionscaffold

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact/gopacttest"
)

func TestRenderRepositoryProducesConformantScaffoldFiles(t *testing.T) {
	repo := exampleRepository()

	files, err := RenderRepository(repo)
	if err != nil {
		t.Fatalf("RenderRepository() error = %v", err)
	}
	byPath := filesByPath(files)
	for _, path := range repo.RequiredFiles {
		if byPath[path] == "" {
			t.Fatalf("rendered files missing %q", path)
		}
	}

	gopacttest.RequireExtensionScaffoldConformance(t, gopacttest.ExtensionScaffoldConformanceHarness{
		ModulePath:       repo.ModulePath,
		ModulePathPrefix: "github.com/gopact-ai/",
		RequiredFiles:    repo.RequiredFiles,
		Files:            byPath,
	})

	if !strings.Contains(byPath["go.mod"], "require github.com/gopact-ai/gopact v0.0.0") {
		t.Fatalf("go.mod = %q, want explicit gopact requirement", byPath["go.mod"])
	}
	if !strings.Contains(byPath["README.md"], "host-owned config") {
		t.Fatalf("README.md = %q, want host-owned config boundary", byPath["README.md"])
	}
	if !strings.Contains(byPath["README.md"], "V1 Migration Ownership") {
		t.Fatalf("README.md = %q, want v1 migration ownership section", byPath["README.md"])
	}
	workflow := byPath[".github/workflows/ci.yml"]
	for _, want := range []string{
		"Configure private SDK access",
		"GOPACT_GITHUB_TOKEN: ${{ secrets.GOPACT_GITHUB_TOKEN || github.token }}",
		"go env -w GOPRIVATE='github.com/gopact-ai/*'",
		"Check module tidiness",
		"go mod tidy && git diff --exit-code",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf(".github/workflows/ci.yml missing %q:\n%s", want, workflow)
		}
	}
	conformance := byPath["CONFORMANCE.md"]
	for _, want := range []string{
		"## V1 Migration Ownership",
		"adapters/example",
		"move-to-adapter-repo",
		"External repository owns adapters/example before v1.",
		"gopacttest/providerconformance.RequireProviderConformance",
		"gopacttest/reactconformance.RequireDeferredMemoryWorkQueueConformance",
	} {
		if !strings.Contains(conformance, want) {
			t.Fatalf("CONFORMANCE.md missing helper reference %q:\n%s", want, conformance)
		}
	}
	minimal := byPath["examples/minimal_test.go"]
	for _, want := range []string{
		"gopacttest.RequireExtensionScaffoldConformance",
		"TestRequiredConformanceHelperReferences",
		"providerconformance.RequireProviderConformance",
		"reactconformance.RequireDeferredMemoryWorkQueueConformance",
		"github.com/gopact-ai/gopact-adapters-example",
		`"go.mod"`,
		`"README.md"`,
		`"CONFORMANCE.md"`,
		`".github/workflows/ci.yml"`,
	} {
		if !strings.Contains(minimal, want) {
			t.Fatalf("examples/minimal_test.go missing %q:\n%s", want, minimal)
		}
	}
}

func TestRenderRepositoryDocumentsA2AConformanceHelperReference(t *testing.T) {
	repo := exampleRepository()
	repo.Targets[0].ConformanceSuites = []string{
		"gopacttest-extension-scaffold-conformance",
		"gopacttest-a2a-agent-conformance",
		"gopacttest-a2a-discoverer-conformance",
		"gopacttest-a2a-agent-mesh-conformance",
	}

	files, err := RenderRepository(repo)
	if err != nil {
		t.Fatalf("RenderRepository() error = %v", err)
	}
	byPath := filesByPath(files)
	for _, path := range []string{"CONFORMANCE.md", "examples/minimal_test.go"} {
		if !strings.Contains(byPath[path], "gopacttest/a2aconformance.RequireAgentConformance") {
			t.Fatalf("%s missing a2a conformance helper reference:\n%s", path, byPath[path])
		}
		if !strings.Contains(byPath[path], "gopacttest/a2aconformance.RequireDiscovererConformance") {
			t.Fatalf("%s missing a2a discoverer conformance helper reference:\n%s", path, byPath[path])
		}
		if !strings.Contains(byPath[path], "gopacttest/a2aconformance.RequireAgentMeshConformance") {
			t.Fatalf("%s missing a2a agent mesh conformance helper reference:\n%s", path, byPath[path])
		}
	}
}

func TestRenderRepositoryDocumentsGraphConformanceHelperReference(t *testing.T) {
	repo := exampleRepository()
	repo.Targets[0].ConformanceSuites = []string{
		"gopacttest-extension-scaffold-conformance",
		"gopacttest-graph-conformance",
	}

	files, err := RenderRepository(repo)
	if err != nil {
		t.Fatalf("RenderRepository() error = %v", err)
	}
	byPath := filesByPath(files)
	for _, path := range []string{"CONFORMANCE.md", "examples/minimal_test.go"} {
		if !strings.Contains(byPath[path], "gopacttest/graphconformance.RequireGraphConformance") {
			t.Fatalf("%s missing graph conformance helper reference:\n%s", path, byPath[path])
		}
	}
}

func TestWriteRepositoryWritesRenderedFiles(t *testing.T) {
	repo := exampleRepository()
	dir := t.TempDir()

	files, err := WriteRepository(context.Background(), dir, repo)
	if err != nil {
		t.Fatalf("WriteRepository() error = %v", err)
	}
	if len(files) != len(repo.RequiredFiles) {
		t.Fatalf("written files = %d, want %d", len(files), len(repo.RequiredFiles))
	}

	for _, file := range files {
		if !slices.Contains(repo.RequiredFiles, file.Path) {
			t.Fatalf("WriteRepository() wrote unexpected path %q", file.Path)
		}
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatalf("read written %q: %v", file.Path, err)
		}
		if string(body) != file.Body {
			t.Fatalf("written %q body mismatch", file.Path)
		}
	}
}

func TestRenderRepositoryRejectsIncompleteInput(t *testing.T) {
	_, err := RenderRepository(Repository{
		Name:      "gopact-adapters-example",
		GoVersion: "1.25.11",
	})
	if err == nil {
		t.Fatal("RenderRepository() error = nil, want validation error")
	}
}

func TestRenderRepositoryRejectsUnsafeTargetPaths(t *testing.T) {
	repo := exampleRepository()
	repo.Targets[0].MinimalExamplePath = "../minimal_test.go"

	_, err := RenderRepository(repo)
	if err == nil {
		t.Fatal("RenderRepository() error = nil, want unsafe path validation error")
	}
}

func exampleRepository() Repository {
	return Repository{
		Name:        "gopact-adapters-example",
		Kind:        "adapter",
		ModulePath:  "github.com/gopact-ai/gopact-adapters-example",
		SDKModule:   "github.com/gopact-ai/gopact",
		SDKVersion:  "v0.0.0",
		GoVersion:   "1.25.11",
		SourcePaths: []string{"adapters/example"},
		Targets: []Target{
			{
				Name:               "gopact-adapters-example-target",
				Kind:               "adapter",
				PackagePath:        "example",
				MinimalExamplePath: "examples/minimal_test.go",
				SourcePaths:        []string{"adapters/example"},
				Migrations: []Migration{
					{
						SourcePath:  "adapters/example",
						Action:      "move-to-adapter-repo",
						V1Condition: "External repository owns adapters/example before v1.",
					},
				},
				ConformanceSuites: []string{
					"gopacttest-extension-scaffold-conformance",
					"gopacttest-provider-conformance",
					"gopacttest-react-deferred-memory-queue-conformance",
					"example-adapter-conformance",
				},
				RequiredExamples: []string{
					"minimal constructor-based setup",
				},
			},
		},
		RequiredFiles: []string{
			"go.mod",
			"README.md",
			"CONFORMANCE.md",
			"examples/minimal_test.go",
			".github/workflows/ci.yml",
		},
		RequiredCICommands: []string{
			"git diff --check",
			"go mod tidy && git diff --exit-code",
			"go test -count=1 ./...",
			"go vet ./...",
		},
	}
}

func filesByPath(files []File) map[string]string {
	out := make(map[string]string, len(files))
	for _, file := range files {
		out[file.Path] = file.Body
	}
	return out
}
