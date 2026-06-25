package gopacttest

import (
	"context"
	"errors"
	"testing"
)

func TestCheckExtensionScaffoldConformancePassesForMinimalRepository(t *testing.T) {
	results := CheckExtensionScaffoldConformance(context.Background(), ExtensionScaffoldConformanceHarness{
		ModulePath:       "github.com/gopact-ai/gopact-adapters-example",
		ModulePathPrefix: "github.com/gopact-ai/",
		RequiredFiles: []string{
			"go.mod",
			"README.md",
			"CONFORMANCE.md",
			"examples/minimal_test.go",
			".github/workflows/ci.yml",
		},
		Files: map[string]string{
			"go.mod":                   "module github.com/gopact-ai/gopact-adapters-example\n\ngo 1.24\n",
			"README.md":                "# example\n\nUses host-owned config and typed constructors.\n",
			"CONFORMANCE.md":           "## CI Commands\n\ngit diff --check\ngo test -count=1 ./...\ngo vet ./...\n",
			"examples/minimal_test.go": "package examples\n\nfunc Example() {}\n",
			".github/workflows/ci.yml": "run: go test -count=1 ./...\n",
		},
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("case %q failed: %v", result.Case, result.Err)
		}
	}
}

func TestCheckExtensionScaffoldConformanceReportsMissingRequiredFile(t *testing.T) {
	results := CheckExtensionScaffoldConformance(context.Background(), ExtensionScaffoldConformanceHarness{
		ModulePath:       "github.com/gopact-ai/gopact-adapters-example",
		ModulePathPrefix: "github.com/gopact-ai/",
		RequiredFiles:    []string{"go.mod", "README.md"},
		Files: map[string]string{
			"go.mod": "module github.com/gopact-ai/gopact-adapters-example\n\ngo 1.24\n",
		},
	})

	var found bool
	for _, result := range results {
		if result.Case != "required-files" {
			continue
		}
		found = true
		if result.Passed {
			t.Fatal("required-files case passed, want failure")
		}
		if !errors.Is(result.Err, ErrExtensionScaffoldConformanceFailed) {
			t.Fatalf("required-files error = %v, want ErrExtensionScaffoldConformanceFailed", result.Err)
		}
	}
	if !found {
		t.Fatal("required-files case not returned")
	}
}

func TestCheckExtensionScaffoldConformanceReportsEmptyObservedScaffoldFiles(t *testing.T) {
	results := CheckExtensionScaffoldConformance(context.Background(), ExtensionScaffoldConformanceHarness{
		ModulePath:       "github.com/gopact-ai/gopact-adapters-example",
		ModulePathPrefix: "github.com/gopact-ai/",
		RequiredFiles: []string{
			"go.mod",
			"README.md",
			"CONFORMANCE.md",
			"examples/minimal_test.go",
		},
		Files: map[string]string{
			"go.mod":                   "",
			"README.md":                "",
			"CONFORMANCE.md":           "",
			"examples/minimal_test.go": "",
		},
	})

	failedCases := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failedCases[result.Case] = true
		}
	}
	for _, name := range []string{"go-mod", "readme", "conformance-doc", "minimal-example"} {
		if !failedCases[name] {
			t.Fatalf("case %q passed, want failure", name)
		}
	}
}

func TestRequireExtensionScaffoldConformancePasses(t *testing.T) {
	RequireExtensionScaffoldConformance(t, ExtensionScaffoldConformanceHarness{
		ModulePath:       "github.com/gopact-ai/gopact-adapters-example",
		ModulePathPrefix: "github.com/gopact-ai/",
		RequiredFiles:    []string{"go.mod"},
		Files: map[string]string{
			"go.mod": "module github.com/gopact-ai/gopact-adapters-example\n\ngo 1.24\n",
		},
	})
}
