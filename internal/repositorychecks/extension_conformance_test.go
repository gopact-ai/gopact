package repositorychecks

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExtensionConformanceManifestCoversMovedBoundaryTargets(t *testing.T) {
	boundary := loadRepositoryBoundaryManifest(t)
	manifest := loadExtensionConformanceManifest(t)

	if manifest.ReadmeTemplatePath == "" {
		t.Fatal("extension conformance readme_template_path is empty")
	}
	readmeTemplate := readExtensionConformanceFile(t, manifest.ReadmeTemplatePath)
	for _, section := range []string{
		"## Compatibility",
		"## Installation",
		"## Usage",
		"## Conformance",
		"## Examples",
		"## Security",
	} {
		if !strings.Contains(readmeTemplate, section) {
			t.Fatalf("readme template %q missing section %q", manifest.ReadmeTemplatePath, section)
		}
	}

	for _, command := range []string{
		"git diff --check",
		"go test -count=1 ./...",
		"go vet ./...",
	} {
		if !slices.Contains(manifest.RequiredCICommands, command) {
			t.Fatalf("extension conformance required_ci_commands missing %q", command)
		}
	}
	if len(manifest.SDKCompatibility.GoVersions) == 0 {
		t.Fatal("extension conformance sdk_compatibility.go_versions is empty")
	}
	if manifest.SDKCompatibility.Module == "" {
		t.Fatal("extension conformance sdk_compatibility.module is empty")
	}

	targets := make(map[string]extensionConformanceTarget, len(manifest.Targets))
	for _, target := range manifest.Targets {
		if target.Name == "" {
			t.Fatal("extension conformance target name is empty")
		}
		if target.Kind != "adapter" && target.Kind != "template" && target.Kind != "plugin" {
			t.Fatalf("extension conformance target %q has invalid kind %q", target.Name, target.Kind)
		}
		if len(target.SourcePaths) == 0 {
			t.Fatalf("extension conformance target %q source_paths is empty", target.Name)
		}
		if len(target.ConformanceSuites) == 0 {
			t.Fatalf("extension conformance target %q conformance_suites is empty", target.Name)
		}
		if len(target.RequiredExamples) == 0 {
			t.Fatalf("extension conformance target %q required_examples is empty", target.Name)
		}
		if targets[target.Name].Name != "" {
			t.Fatalf("extension conformance target %q is duplicated", target.Name)
		}
		targets[target.Name] = target
	}

	for _, want := range movedRepositoryBoundaryTargets(boundary) {
		target, ok := targets[want.name]
		if !ok {
			t.Fatalf("extension conformance manifest missing target %q", want.name)
		}
		if target.Kind != want.kind {
			t.Fatalf("extension conformance target %q kind = %q, want %q", want.name, target.Kind, want.kind)
		}
		for _, path := range want.paths {
			if !slices.Contains(target.SourcePaths, path) {
				t.Fatalf("extension conformance target %q missing source path %q", want.name, path)
			}
		}
	}
}

func TestExtensionScaffoldContractDocumentsExternalRepoCI(t *testing.T) {
	manifest := loadExtensionConformanceManifest(t)

	if manifest.Scaffold.RepositoryOwner != "gopact-ai" {
		t.Fatalf("extension scaffold repository_owner = %q, want gopact-ai", manifest.Scaffold.RepositoryOwner)
	}
	if manifest.Scaffold.ModulePathPrefix != "github.com/gopact-ai/" {
		t.Fatalf("extension scaffold module_path_prefix = %q, want github.com/gopact-ai/", manifest.Scaffold.ModulePathPrefix)
	}
	for _, file := range []string{
		"go.mod",
		"README.md",
		"CONFORMANCE.md",
		"examples/minimal_test.go",
		".github/workflows/ci.yml",
	} {
		if !slices.Contains(manifest.Scaffold.RequiredFiles, file) {
			t.Fatalf("extension scaffold required_files missing %q", file)
		}
	}
	for _, item := range []string{
		"host-owned config",
		"offline conformance",
		"extension scaffold conformance",
		"integration build tags",
		"no production adapter in core repo",
	} {
		if !slices.Contains(manifest.Scaffold.Checklist, item) {
			t.Fatalf("extension scaffold checklist missing %q", item)
		}
	}

	ciWorkflow := readExtensionConformanceFile(t, manifest.Scaffold.CIWorkflowTemplatePath)
	for _, command := range manifest.RequiredCICommands {
		if !strings.Contains(ciWorkflow, command) {
			t.Fatalf("extension CI workflow template missing required command %q", command)
		}
	}
	if !strings.Contains(ciWorkflow, "go-version") {
		t.Fatal("extension CI workflow template must pin go-version")
	}
	for _, action := range []string{"actions/checkout@v7", "actions/setup-go@v6"} {
		if !strings.Contains(ciWorkflow, action) {
			t.Fatalf("extension CI workflow template missing current GitHub Action %q", action)
		}
	}

	conformanceTemplate := readExtensionConformanceFile(t, manifest.Scaffold.ConformanceTemplatePath)
	for _, section := range []string{
		"## Extension Target",
		"## Required Suites",
		"## CI Commands",
		"## Integration Tests",
		"## Security Boundary",
	} {
		if !strings.Contains(conformanceTemplate, section) {
			t.Fatalf("conformance template %q missing section %q", manifest.Scaffold.ConformanceTemplatePath, section)
		}
	}
	for _, command := range manifest.RequiredCICommands {
		if !strings.Contains(conformanceTemplate, command) {
			t.Fatalf("conformance template missing required command %q", command)
		}
	}

	for _, target := range manifest.Targets {
		modulePath := manifest.Scaffold.ModulePathPrefix + target.Name
		if !strings.HasPrefix(modulePath, manifest.Scaffold.ModulePathPrefix) {
			t.Fatalf("extension scaffold module path %q does not use prefix %q", modulePath, manifest.Scaffold.ModulePathPrefix)
		}
	}
}

type extensionConformanceManifest struct {
	Version            int                          `json:"version"`
	SDKCompatibility   extensionSDKCompatibility    `json:"sdk_compatibility"`
	ReadmeTemplatePath string                       `json:"readme_template_path"`
	Scaffold           extensionScaffold            `json:"scaffold"`
	RequiredCICommands []string                     `json:"required_ci_commands"`
	Targets            []extensionConformanceTarget `json:"targets"`
}

type extensionScaffold struct {
	RepositoryOwner         string   `json:"repository_owner"`
	ModulePathPrefix        string   `json:"module_path_prefix"`
	CIWorkflowTemplatePath  string   `json:"ci_workflow_template_path"`
	ConformanceTemplatePath string   `json:"conformance_template_path"`
	RequiredFiles           []string `json:"required_files"`
	Checklist               []string `json:"checklist"`
}

type extensionSDKCompatibility struct {
	Module     string   `json:"module"`
	GoVersions []string `json:"go_versions"`
}

type extensionConformanceTarget struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	SourcePaths       []string `json:"source_paths"`
	ConformanceSuites []string `json:"conformance_suites"`
	RequiredExamples  []string `json:"required_examples"`
}

type movedRepositoryBoundaryTarget struct {
	name  string
	kind  string
	paths []string
}

func loadExtensionConformanceManifest(t *testing.T) extensionConformanceManifest {
	t.Helper()

	raw := readFile(t, filepath.Join("doc", "design", "extension-conformance.json"))
	var manifest extensionConformanceManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode extension conformance manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("extension conformance manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}

func readExtensionConformanceFile(t *testing.T, path string) string {
	t.Helper()

	return readTextFile(t, filepath.Clean(path))
}

func movedRepositoryBoundaryTargets(manifest repositoryBoundaryManifest) []movedRepositoryBoundaryTarget {
	targets := map[string]movedRepositoryBoundaryTarget{}
	for _, entry := range manifest.Entries {
		if entry.Disposition != "move-to-adapter-repo" && entry.Disposition != "move-to-template-repo" {
			continue
		}
		kind := "adapter"
		if entry.Disposition == "move-to-template-repo" {
			kind = "template"
		}
		target := targets[entry.Target]
		target.name = entry.Target
		target.kind = kind
		target.paths = append(target.paths, entry.Path)
		targets[entry.Target] = target
	}

	out := make([]movedRepositoryBoundaryTarget, 0, len(targets))
	for _, target := range targets {
		slices.Sort(target.paths)
		out = append(out, target)
	}
	slices.SortFunc(out, func(a, b movedRepositoryBoundaryTarget) int {
		return strings.Compare(a.name, b.name)
	})
	return out
}
