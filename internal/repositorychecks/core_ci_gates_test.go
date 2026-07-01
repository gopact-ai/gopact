package repositorychecks

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact/gopacttest"
)

func TestCoreCIGatesDocumentedAndConfigured(t *testing.T) {
	manifest := loadCoreCIGatesManifest(t)

	if manifest.WorkflowPath != ".github/workflows/ci.yml" {
		t.Fatalf("core CI workflow_path = %q, want .github/workflows/ci.yml", manifest.WorkflowPath)
	}
	if len(manifest.GoVersions) == 0 {
		t.Fatal("core CI go_versions is empty")
	}
	if len(manifest.RequiredCommands) == 0 {
		t.Fatal("core CI required_commands is empty")
	}
	if manifest.LintConfigPath != ".golangci.yml" {
		t.Fatalf("core CI lint_config_path = %q, want .golangci.yml", manifest.LintConfigPath)
	}
	if manifest.CoverageProfilePath != "coverage.out" {
		t.Fatalf("core CI coverage_profile_path = %q, want coverage.out", manifest.CoverageProfilePath)
	}
	if manifest.ToolVersions.GolangCILint != "v2.8.0" {
		t.Fatalf("core CI golangci-lint version = %q, want v2.8.0", manifest.ToolVersions.GolangCILint)
	}
	if manifest.ToolVersions.Govulncheck != "v1.1.4" {
		t.Fatalf("core CI govulncheck version = %q, want v1.1.4", manifest.ToolVersions.Govulncheck)
	}
	if len(manifest.RequiredLinters) == 0 {
		t.Fatal("core CI required_linters is empty")
	}
	if len(manifest.RequiredFormatters) == 0 {
		t.Fatal("core CI required_formatters is empty")
	}

	workflow := readTextFile(t, manifest.WorkflowPath)
	for _, version := range manifest.GoVersions {
		if !strings.Contains(workflow, `go-version: "`+version+`"`) {
			t.Fatalf("core CI workflow missing go version %q", version)
		}
	}
	for _, command := range manifest.RequiredCommands {
		if !strings.Contains(workflow, command) {
			t.Fatalf("core CI workflow missing required command %q", command)
		}
	}
	for _, action := range []string{"actions/checkout@v7", "actions/setup-go@v6"} {
		if !strings.Contains(workflow, action) {
			t.Fatalf("core CI workflow missing current GitHub Action %q", action)
		}
	}
	if !strings.Contains(workflow, "golangci-lint@"+manifest.ToolVersions.GolangCILint) {
		t.Fatalf("core CI workflow does not pin golangci-lint %s", manifest.ToolVersions.GolangCILint)
	}
	if !strings.Contains(workflow, "govulncheck@"+manifest.ToolVersions.Govulncheck) {
		t.Fatalf("core CI workflow does not pin govulncheck %s", manifest.ToolVersions.Govulncheck)
	}
	if !strings.Contains(workflow, "-coverprofile="+manifest.CoverageProfilePath) {
		t.Fatalf("core CI workflow does not write configured coverage profile %q", manifest.CoverageProfilePath)
	}

	makefile := readTextFile(t, "Makefile")
	for _, target := range []string{"check:", "test:", "race:", "vet:", "lint:", "coverage:", "examples:", "graph:", "a2a-mesh:", "security:"} {
		if !strings.Contains(makefile, target) {
			t.Fatalf("Makefile missing target %q", strings.TrimSuffix(target, ":"))
		}
	}
	for _, command := range manifest.RequiredCommands {
		if command == "git diff --check" {
			continue
		}
		if !strings.Contains(makefile, command) {
			t.Fatalf("Makefile missing required command %q", command)
		}
	}
	if !strings.Contains(makefile, "-coverprofile="+manifest.CoverageProfilePath) {
		t.Fatalf("Makefile does not write configured coverage profile %q", manifest.CoverageProfilePath)
	}

	for _, gate := range []string{"whitespace", "unit", "race", "vet", "lint", "coverage", "examples", "security"} {
		if !slices.Contains(manifest.RequiredGates, gate) {
			t.Fatalf("core CI required_gates missing %q", gate)
		}
	}

	lintConfig := readTextFile(t, manifest.LintConfigPath)
	for _, linter := range manifest.RequiredLinters {
		if !strings.Contains(lintConfig, "- "+linter) {
			t.Fatalf("lint config missing required linter %q", linter)
		}
	}
	for _, formatter := range manifest.RequiredFormatters {
		if !strings.Contains(lintConfig, "- "+formatter) {
			t.Fatalf("lint config missing required formatter %q", formatter)
		}
	}
}

func TestCoreCIGateEvidenceBridgeIsDocumented(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "development-plan.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "RecordCIGateSuiteCheck") {
			t.Fatalf("%s does not document gopacttest.RecordCIGateSuiteCheck", path)
		}
		if !strings.Contains(content, "ci_gate") {
			t.Fatalf("%s does not document ci_gate evidence", path)
		}
	}
}

func TestCoreCIGatesRunGraphConformance(t *testing.T) {
	const command = "go test -count=1 ./graph ./gopacttest/graphconformance"

	manifest := loadCoreCIGatesManifest(t)
	if !slices.Contains(manifest.RequiredCommands, command) {
		t.Fatalf("core CI required_commands = %#v, want %q", manifest.RequiredCommands, command)
	}
	workflow := readTextFile(t, manifest.WorkflowPath)
	if !strings.Contains(workflow, command) {
		t.Fatalf("core CI workflow missing graph conformance command %q", command)
	}
	makefile := readTextFile(t, "Makefile")
	if !strings.Contains(makefile, command) {
		t.Fatalf("Makefile missing graph conformance command %q", command)
	}
}

func TestCoreCIConfiguresPrivateModuleAccess(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/ci.yml")
	for _, want := range []string{
		"GOPRIVATE: github.com/gopact-ai/*",
		"GOPACT_GITHUB_TOKEN",
		`git config --global url."https://x-access-token:${GOPACT_GITHUB_TOKEN}@github.com/gopact-ai/".insteadOf "https://github.com/gopact-ai/"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("core CI workflow missing private module access config %q", want)
		}
	}
}

func TestSelfBootstrapReleaseGateTracksCoreCIGates(t *testing.T) {
	manifest := loadCoreCIGatesManifest(t)
	var selfBootstrapGates []string
	for _, requirement := range gopacttest.SelfBootstrapReleaseGateRequirements() {
		if requirement.Name == "self-bootstrap-ci" {
			selfBootstrapGates = requirement.RequiredCIGates
			break
		}
	}
	if !slices.Equal(selfBootstrapGates, manifest.RequiredGates) {
		t.Fatalf("self-bootstrap CI gates = %v, want core gates %v", selfBootstrapGates, manifest.RequiredGates)
	}
}

type coreCIGatesManifest struct {
	Version             int      `json:"version"`
	WorkflowPath        string   `json:"workflow_path"`
	GoVersions          []string `json:"go_versions"`
	LintConfigPath      string   `json:"lint_config_path"`
	CoverageProfilePath string   `json:"coverage_profile_path"`
	ToolVersions        struct {
		GolangCILint string `json:"golangci-lint"`
		Govulncheck  string `json:"govulncheck"`
	} `json:"tool_versions"`
	RequiredGates      []string `json:"required_gates"`
	RequiredCommands   []string `json:"required_commands"`
	RequiredLinters    []string `json:"required_linters"`
	RequiredFormatters []string `json:"required_formatters"`
}

func loadCoreCIGatesManifest(t *testing.T) coreCIGatesManifest {
	t.Helper()

	raw := readFile(t, filepath.Join("docs", "design", "core-ci-gates.json"))
	var manifest coreCIGatesManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode core CI gates manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("core CI gates manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}
