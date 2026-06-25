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
	conformance := byPath["CONFORMANCE.md"]
	for _, want := range []string{
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
		GoVersion: "1.25",
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
		GoVersion:   "1.25",
		SourcePaths: []string{"adapters/example"},
		Targets: []Target{
			{
				Name:               "gopact-adapters-example-target",
				Kind:               "adapter",
				PackagePath:        "example",
				MinimalExamplePath: "examples/minimal_test.go",
				SourcePaths:        []string{"adapters/example"},
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
