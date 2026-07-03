package repositorychecks

import (
	"encoding/json"
	"os"
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
		filepath.Join("doc", "design", "index.md"),
		filepath.Join("doc", "design", "development-plan.md"),
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

func TestSelfBootstrapReleaseGateTracksGraphConformanceCommand(t *testing.T) {
	const command = "go test -count=1 ./graph ./gopacttest/graphconformance"
	if got := gopacttest.SelfBootstrapCheckGraphConformanceCommand; got != "command:"+command {
		t.Fatalf("self-bootstrap graph conformance check = %q, want %q", got, "command:"+command)
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

func TestRepositoryPublicReadinessAndPRGovernanceAreConfigured(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/ci.yml")
	readiness := readTextFile(t, filepath.Join("scripts", "public-readiness-check.sh"))
	prGovernance := readTextFile(t, filepath.Join(".github", "workflows", "pr-governance.yml"))
	adminAutomerge := readTextFile(t, filepath.Join(".github", "workflows", "admin-automerge.yml"))
	governanceDoc := readTextFile(t, filepath.Join("doc", "maintainers", "repository-governance.md"))

	for _, want := range []string{
		"permissions:",
		"contents: read",
		"fetch-depth: 0",
		"./scripts/public-readiness-check.sh",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("core CI workflow missing public readiness control %q", want)
		}
	}
	for _, want := range []string{
		"git ls-files -- .env '.env.*'",
		"git rev-list --all",
		"commit message",
		"api-key-[0-9]{14,}",
		"sk-vx[[:alnum:]_-]{20,}",
		"ep-[0-9]{14}-[[:alnum:]_-]+",
	} {
		if !strings.Contains(readiness, want) {
			t.Fatalf("public readiness script missing %q", want)
		}
	}
	for _, want := range []string{
		"name: pr-governance",
		"pull_request_target:",
		"pull_request_review:",
		"author-policy",
		"collaborators/${author}/permission",
		"== \"APPROVED\"",
	} {
		if !strings.Contains(prGovernance, want) {
			t.Fatalf("PR governance workflow missing %q", want)
		}
	}
	for _, want := range []string{
		"name: admin-automerge",
		"pull_request_target:",
		"gh pr merge",
		"--auto",
		"--squash",
		"--delete-branch",
		"!= \"admin\"",
	} {
		if !strings.Contains(adminAutomerge, want) {
			t.Fatalf("admin automerge workflow missing %q", want)
		}
	}
	for _, want := range []string{
		"author-policy",
		"Admin-authored PRs",
		"Non-admin-authored PRs",
		"Do not configure a global required review count",
		"Require status checks to pass",
	} {
		if !strings.Contains(governanceDoc, want) {
			t.Fatalf("repository governance doc missing %q", want)
		}
	}
}

func TestCoreCIWorkflowOptimizesIndependentGatesForParallelFeedback(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/ci.yml")

	for _, want := range []string{
		"concurrency:",
		"group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}",
		"cancel-in-progress: ${{ github.event_name == 'pull_request' }}",
		"hygiene:",
		"unit:",
		"race:",
		"static:",
		"coverage:",
		"conformance:",
		"security:",
		"self-bootstrap:",
		"needs: [hygiene, unit, race, static, coverage, conformance, security, self-bootstrap]",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("core CI workflow missing parallel feedback control %q", want)
		}
	}
}

func TestCoreSelfBootstrapMockSuiteIsExecutableAndUsedByCI(t *testing.T) {
	const suitePath = "scripts/self-bootstrap-mock-suite.sh"

	info, err := os.Stat(repoPath(t, suitePath))
	if err != nil {
		t.Fatalf("stat %s: %v", suitePath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("%s is not executable: mode %v", suitePath, info.Mode())
	}

	suite := readTextFile(t, suitePath)
	for _, want := range []string{
		"set -euo pipefail",
		"./scripts/public-readiness-check.sh",
		"git diff --exit-code -- go.mod go.sum",
		"go test -count=1 ./...",
		"go test -run '^Example' ./...",
		"go test -count=1 ./graph ./gopacttest/graphconformance",
		"go test -count=1 ./a2a ./gopacttest/a2aconformance",
		"go test -count=1 -run SelfBootstrap ./gopacttest",
	} {
		if !strings.Contains(suite, want) {
			t.Fatalf("core self-bootstrap mock suite missing %q", want)
		}
	}

	workflow := readTextFile(t, ".github/workflows/ci.yml")
	for _, want := range []string{
		"self-bootstrap:",
		"./scripts/self-bootstrap-mock-suite.sh",
		"needs: [hygiene, unit, race, static, coverage, conformance, security, self-bootstrap]",
		"check_gate self-bootstrap",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("core CI workflow missing self-bootstrap suite control %q", want)
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

func TestSelfBootstrapReleaseGateTracksCoreCICommandEvidence(t *testing.T) {
	manifest := loadCoreCIGatesManifest(t)
	var selfBootstrapRequirement gopacttest.VerificationEvidenceRequirement
	for _, requirement := range gopacttest.SelfBootstrapReleaseGateRequirements() {
		if requirement.Name == "self-bootstrap-ci" {
			selfBootstrapRequirement = requirement
			break
		}
	}
	if selfBootstrapRequirement.Name == "" {
		t.Fatal("self-bootstrap release gate missing self-bootstrap-ci requirement")
	}
	if !slices.Contains(selfBootstrapRequirement.RequiredEvidenceTypes, gopacttest.VerificationEvidenceTypeCommand) {
		t.Fatalf(
			"self-bootstrap-ci evidence types = %v, want command evidence",
			selfBootstrapRequirement.RequiredEvidenceTypes,
		)
	}
	for _, command := range manifest.RequiredCommands {
		id := "command:" + command
		if !slices.Contains(selfBootstrapRequirement.RequiredCheckIDs, id) {
			t.Fatalf("self-bootstrap-ci required check IDs missing %q", id)
		}
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

	raw := readFile(t, filepath.Join("doc", "design", "core-ci-gates.json"))
	var manifest coreCIGatesManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode core CI gates manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("core CI gates manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}
