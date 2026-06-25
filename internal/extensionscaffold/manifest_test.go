package extensionscaffold

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact/gopacttest"
)

const expectedScaffoldRepositoryCount = 11

func TestLoadRepositoriesFromDesignRendersConformantScaffolds(t *testing.T) {
	repos, err := LoadRepositoriesFromDesign(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("LoadRepositoriesFromDesign() error = %v", err)
	}
	if len(repos) != expectedScaffoldRepositoryCount {
		t.Fatalf("repositories = %d, want %d", len(repos), expectedScaffoldRepositoryCount)
	}

	byName := make(map[string]Repository, len(repos))
	for _, repo := range repos {
		if repo.Name == "" {
			t.Fatal("loaded repository with empty name")
		}
		if byName[repo.Name].Name != "" {
			t.Fatalf("loaded duplicate repository %q", repo.Name)
		}
		if repo.SDKModule != "github.com/gopact-ai/gopact" {
			t.Fatalf("repository %q SDKModule = %q, want github.com/gopact-ai/gopact", repo.Name, repo.SDKModule)
		}
		if repo.GoVersion != "1.25" {
			t.Fatalf("repository %q GoVersion = %q, want 1.25", repo.Name, repo.GoVersion)
		}
		if repo.SDKVersion != DefaultSDKVersion {
			t.Fatalf("repository %q SDKVersion = %q, want %q", repo.Name, repo.SDKVersion, DefaultSDKVersion)
		}
		if len(repo.SourcePaths) == 0 {
			t.Fatalf("repository %q SourcePaths is empty", repo.Name)
		}
		files, err := RenderRepository(repo)
		if err != nil {
			t.Fatalf("RenderRepository(%q) error = %v", repo.Name, err)
		}
		gopacttest.RequireExtensionScaffoldConformance(t, gopacttest.ExtensionScaffoldConformanceHarness{
			ModulePath:       repo.ModulePath,
			ModulePathPrefix: "github.com/gopact-ai/",
			RequiredFiles:    repo.RequiredFiles,
			Files:            filesByPath(files),
		})
		byName[repo.Name] = repo
	}

	model := byName["gopact-adapters-model"]
	if model.Kind != "adapter" {
		t.Fatalf("gopact-adapters-model Kind = %q, want adapter", model.Kind)
	}
	if len(model.Targets) != 1 {
		t.Fatalf("gopact-adapters-model targets = %d, want 1", len(model.Targets))
	}
	target := model.Targets[0]
	if target.PackagePath != "openaicompatible" {
		t.Fatalf("model target PackagePath = %q, want openaicompatible", target.PackagePath)
	}
	if !slices.Contains(target.ConformanceSuites, "gopacttest-provider-conformance") {
		t.Fatalf("model target ConformanceSuites = %#v, want provider conformance", target.ConformanceSuites)
	}
	if !slices.Contains(target.RequiredExamples, "host-injected HTTP client") {
		t.Fatalf("model target RequiredExamples = %#v, want host-injected HTTP client", target.RequiredExamples)
	}

	template := byName["gopact-templates-devagent"]
	if template.Kind != "template" {
		t.Fatalf("gopact-templates-devagent Kind = %q, want template", template.Kind)
	}
	if !slices.Contains(template.SourcePaths, "templates/devagent") {
		t.Fatalf("gopact-templates-devagent SourcePaths = %#v, want templates/devagent", template.SourcePaths)
	}

	reactTemplate := byName["gopact-templates-react"]
	if reactTemplate.Kind != "template" {
		t.Fatalf("gopact-templates-react Kind = %q, want template", reactTemplate.Kind)
	}
	if !slices.Contains(reactTemplate.SourcePaths, "templates/react") {
		t.Fatalf("gopact-templates-react SourcePaths = %#v, want templates/react", reactTemplate.SourcePaths)
	}
	if len(reactTemplate.Targets) != 1 || !slices.Contains(reactTemplate.Targets[0].ConformanceSuites, "gopacttest-react-deferred-memory-queue-conformance") {
		t.Fatalf("gopact-templates-react targets = %#v, want deferred memory queue conformance", reactTemplate.Targets)
	}
}

func TestRenderRepositoriesFromDesignPlansEveryScaffold(t *testing.T) {
	scaffolds, err := RenderRepositoriesFromDesign(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("RenderRepositoriesFromDesign() error = %v", err)
	}
	if len(scaffolds) != expectedScaffoldRepositoryCount {
		t.Fatalf("scaffolds = %d, want %d", len(scaffolds), expectedScaffoldRepositoryCount)
	}

	for _, scaffold := range scaffolds {
		repo := scaffold.Repository
		if scaffold.Directory != repo.Name {
			t.Fatalf("repository %q Directory = %q, want repo name", repo.Name, scaffold.Directory)
		}
		byPath := filesByPath(scaffold.Files)
		for _, path := range repo.RequiredFiles {
			if byPath[path] == "" {
				t.Fatalf("repository %q scaffold files missing %q", repo.Name, path)
			}
		}
		gopacttest.RequireExtensionScaffoldConformance(t, gopacttest.ExtensionScaffoldConformanceHarness{
			ModulePath:       repo.ModulePath,
			ModulePathPrefix: "github.com/gopact-ai/",
			RequiredFiles:    repo.RequiredFiles,
			Files:            byPath,
		})
	}
}

func TestWriteRepositoriesFromDesignWritesNamedRepositoryDirectories(t *testing.T) {
	dir := t.TempDir()

	scaffolds, err := WriteRepositoriesFromDesign(context.Background(), filepath.Join("..", ".."), dir)
	if err != nil {
		t.Fatalf("WriteRepositoriesFromDesign() error = %v", err)
	}
	if len(scaffolds) != expectedScaffoldRepositoryCount {
		t.Fatalf("written scaffolds = %d, want %d", len(scaffolds), expectedScaffoldRepositoryCount)
	}

	for _, scaffold := range scaffolds {
		repoDir := filepath.Join(dir, scaffold.Directory)
		for _, file := range scaffold.Files {
			body, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(file.Path)))
			if err != nil {
				t.Fatalf("read written %s/%s: %v", scaffold.Directory, file.Path, err)
			}
			if string(body) != file.Body {
				t.Fatalf("written %s/%s body mismatch", scaffold.Directory, file.Path)
			}
		}
	}
}

func TestWriteRepositoriesFromDesignRejectsEmptyOutputDirectory(t *testing.T) {
	_, err := WriteRepositoriesFromDesign(context.Background(), filepath.Join("..", ".."), "")
	if err == nil {
		t.Fatal("WriteRepositoriesFromDesign() error = nil, want output directory validation error")
	}
	if !strings.Contains(err.Error(), "output directory") {
		t.Fatalf("WriteRepositoriesFromDesign() error = %v, want output directory", err)
	}
}

func TestWriteBootstrapWorkspaceSupportsGeneratedRepositoryTests(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	dir := t.TempDir()

	workspace, err := WriteBootstrapWorkspace(context.Background(), root, dir)
	if err != nil {
		t.Fatalf("WriteBootstrapWorkspace() error = %v", err)
	}
	if workspace.GoWork.Path != "go.work" {
		t.Fatalf("go.work path = %q, want go.work", workspace.GoWork.Path)
	}
	if workspace.SyncPlan.Path != "sync-plan.json" {
		t.Fatalf("sync plan path = %q, want sync-plan.json", workspace.SyncPlan.Path)
	}
	if workspace.SyncScript.Path != "sync-repos.sh" {
		t.Fatalf("sync script path = %q, want sync-repos.sh", workspace.SyncScript.Path)
	}
	if len(workspace.Scaffolds) != expectedScaffoldRepositoryCount {
		t.Fatalf("workspace scaffolds = %d, want %d", len(workspace.Scaffolds), expectedScaffoldRepositoryCount)
	}

	body, err := os.ReadFile(filepath.Join(dir, "go.work"))
	if err != nil {
		t.Fatalf("read go.work: %v", err)
	}
	syncPlanBody, err := os.ReadFile(filepath.Join(dir, "sync-plan.json"))
	if err != nil {
		t.Fatalf("read sync-plan.json: %v", err)
	}
	syncScriptBody, err := os.ReadFile(filepath.Join(dir, "sync-repos.sh"))
	if err != nil {
		t.Fatalf("read sync-repos.sh: %v", err)
	}
	syncScriptInfo, err := os.Stat(filepath.Join(dir, "sync-repos.sh"))
	if err != nil {
		t.Fatalf("stat sync-repos.sh: %v", err)
	}
	if syncScriptInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("sync-repos.sh mode = %s, want executable bit", syncScriptInfo.Mode())
	}
	if !strings.Contains(string(syncPlanBody), `"create_command": "gh repo create gopact-ai/gopact-adapters-model --private --source <generated>/gopact-adapters-model --remote origin --push"`) {
		t.Fatalf("sync-plan.json missing unescaped create command:\n%s", string(syncPlanBody))
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"gh repo view \"${repo}\"",
		"sync_repo 'gopact-adapters-model' 'gopact-adapters-model' 'private'",
		"git -C \"${repo_dir}\" commit -m \"chore: bootstrap gopact extension scaffold\"",
		"gh repo create \"${repo}\" \"${visibility_flag}\" --source \"${repo_dir}\" --remote origin --push",
	} {
		if !strings.Contains(string(syncScriptBody), want) {
			t.Fatalf("sync-repos.sh missing %q:\n%s", want, string(syncScriptBody))
		}
	}
	for _, want := range []string{
		filepath.ToSlash(root),
		"./gopact-adapters-model",
		"./gopact-templates-react",
		"./gopact-templates-devagent",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("go.work missing %q:\n%s", want, string(body))
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = filepath.Join(dir, "gopact-adapters-model")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated repository go test failed: %v\n%s", err, string(output))
	}
}

func TestVerifyBootstrapWorkspaceRunsGeneratedRepositoryTests(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	dir := t.TempDir()

	workspace, err := WriteBootstrapWorkspace(context.Background(), root, dir)
	if err != nil {
		t.Fatalf("WriteBootstrapWorkspace() error = %v", err)
	}
	report, err := VerifyBootstrapWorkspace(context.Background(), dir, workspace)
	if err != nil {
		t.Fatalf("VerifyBootstrapWorkspace() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gocache")); err != nil {
		t.Fatalf("workspace-local GOCACHE missing: %v", err)
	}
	wantResultCount := 0
	for _, scaffold := range workspace.Scaffolds {
		wantResultCount += len(scaffold.Repository.RequiredCICommands)
	}
	if len(report.Results) != wantResultCount {
		t.Fatalf("verification results = %d, want %d", len(report.Results), wantResultCount)
	}
	for _, result := range report.Results {
		if !result.Passed {
			t.Fatalf("repository %q verification failed:\n%s", result.Repository, result.Output)
		}
		if result.Directory == "" {
			t.Fatalf("repository %q Directory is empty", result.Repository)
		}
	}
}

func TestVerifyBootstrapWorkspaceRunsRequiredCICommands(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	dir := t.TempDir()

	workspace, err := WriteBootstrapWorkspace(context.Background(), root, dir)
	if err != nil {
		t.Fatalf("WriteBootstrapWorkspace() error = %v", err)
	}
	report, err := VerifyBootstrapWorkspace(context.Background(), dir, workspace)
	if err != nil {
		t.Fatalf("VerifyBootstrapWorkspace() error = %v", err)
	}

	model, ok := scaffoldByName(workspace.Scaffolds, "gopact-adapters-model")
	if !ok {
		t.Fatal("workspace missing gopact-adapters-model")
	}
	got := verificationCommandsForRepository(report.Results, "gopact-adapters-model")
	if !stringSlicesEqual(got, model.Repository.RequiredCICommands) {
		t.Fatalf("verification commands = %#v, want required CI commands %#v", got, model.Repository.RequiredCICommands)
	}
}

func TestVerifyBootstrapWorkspaceRunsCommandLinesWithShell(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("create generated repository: %v", err)
	}

	const commandLine = "printf shell-ok > shell-command.txt"
	workspace := BootstrapWorkspace{
		Scaffolds: []RepositoryScaffold{{
			Repository: Repository{
				Name:               "repo",
				RequiredCICommands: []string{commandLine},
			},
			Directory: "repo",
		}},
	}

	report, err := VerifyBootstrapWorkspace(context.Background(), dir, workspace)
	if err != nil {
		t.Fatalf("VerifyBootstrapWorkspace() error = %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("verification results = %d, want 1", len(report.Results))
	}
	if report.Results[0].CommandLine != commandLine {
		t.Fatalf("result CommandLine = %q, want %q", report.Results[0].CommandLine, commandLine)
	}
	got, err := os.ReadFile(filepath.Join(repoDir, "shell-command.txt"))
	if err != nil {
		t.Fatalf("shell command output missing: %v", err)
	}
	if string(got) != "shell-ok" {
		t.Fatalf("shell command output = %q, want shell-ok", string(got))
	}
}

func TestRenderSyncPlanFromDesignCapturesRemoteBootstrapSteps(t *testing.T) {
	plan, err := RenderSyncPlanFromDesign(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("RenderSyncPlanFromDesign() error = %v", err)
	}
	if plan.Organization != "gopact-ai" {
		t.Fatalf("plan Organization = %q, want gopact-ai", plan.Organization)
	}
	if plan.DefaultVisibility != "private" {
		t.Fatalf("plan DefaultVisibility = %q, want private", plan.DefaultVisibility)
	}
	if len(plan.Sequence) == 0 {
		t.Fatal("plan Sequence is empty")
	}
	if !slices.Contains(plan.Sequence, "create-private-repos") {
		t.Fatalf("plan Sequence = %#v, want create-private-repos", plan.Sequence)
	}
	if len(plan.Repositories) != expectedScaffoldRepositoryCount {
		t.Fatalf("plan repositories = %d, want %d", len(plan.Repositories), expectedScaffoldRepositoryCount)
	}

	var model SyncRepositoryPlan
	for _, repo := range plan.Repositories {
		if repo.Name == "gopact-adapters-model" {
			model = repo
			break
		}
	}
	if model.Name == "" {
		t.Fatal("plan missing gopact-adapters-model")
	}
	if model.CreateCommand != "gh repo create gopact-ai/gopact-adapters-model --private --source <generated>/gopact-adapters-model --remote origin --push" {
		t.Fatalf("model CreateCommand = %q", model.CreateCommand)
	}
	if model.Directory != "gopact-adapters-model" {
		t.Fatalf("model Directory = %q, want gopact-adapters-model", model.Directory)
	}
	if model.ModulePath != "github.com/gopact-ai/gopact-adapters-model" {
		t.Fatalf("model ModulePath = %q", model.ModulePath)
	}
	if !slices.Contains(model.Files, "examples/minimal_test.go") {
		t.Fatalf("model Files = %#v, want examples/minimal_test.go", model.Files)
	}
	if !stringSlicesEqual(model.VerifyCommands, model.CICommands) {
		t.Fatalf("model VerifyCommands = %#v, want CICommands %#v", model.VerifyCommands, model.CICommands)
	}
	if !slices.Contains(model.CICommands, "go vet ./...") {
		t.Fatalf("model CICommands = %#v, want go vet ./...", model.CICommands)
	}
	if len(model.ExtensionTargets) != 1 || model.ExtensionTargets[0] != "gopact-adapters-model-openaicompatible" {
		t.Fatalf("model ExtensionTargets = %#v", model.ExtensionTargets)
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal sync plan: %v", err)
	}
	if !strings.Contains(string(raw), `"repositories"`) {
		t.Fatalf("sync plan JSON missing repositories: %s", raw)
	}
}

func TestRenderSyncScriptFromDesignCapturesRemoteBootstrapSteps(t *testing.T) {
	file, err := RenderSyncScriptFromDesign(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("RenderSyncScriptFromDesign() error = %v", err)
	}
	if file.Path != "sync-repos.sh" {
		t.Fatalf("script path = %q, want sync-repos.sh", file.Path)
	}
	for _, want := range []string{
		"set -euo pipefail",
		"local organization='gopact-ai'",
		"ensure_git_repo \"${repo_dir}\"",
		"run_ci_command \"${repo_dir}\" \"${command}\"",
		"sync_repo 'gopact-adapters-model' 'gopact-adapters-model' 'private' 'git diff --check' 'go test -count=1 ./...' 'go vet ./...'",
		"sync_repo 'gopact-templates-devagent' 'gopact-templates-devagent' 'private' 'git diff --check' 'go test -count=1 ./...' 'go vet ./...'",
		"git -C \"${repo_dir}\" push -u origin HEAD:main",
	} {
		if !strings.Contains(file.Body, want) {
			t.Fatalf("script missing %q:\n%s", want, file.Body)
		}
	}
}

func scaffoldByName(scaffolds []RepositoryScaffold, name string) (RepositoryScaffold, bool) {
	for _, scaffold := range scaffolds {
		if scaffold.Repository.Name == name {
			return scaffold, true
		}
	}
	return RepositoryScaffold{}, false
}

func verificationCommandsForRepository(results []VerificationResult, repository string) []string {
	var commands []string
	for _, result := range results {
		if result.Repository == repository {
			commands = append(commands, result.CommandLine)
		}
	}
	return commands
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLoadRepositoriesFromDesignRejectsMissingTargetScaffoldSpec(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "docs/design/external-repositories.json", `{
  "version": 1,
  "module_path_prefix": "github.com/gopact-ai/",
  "repositories": [{
    "name": "gopact-adapters-example",
    "route": "adapter-repo",
    "module_path": "github.com/gopact-ai/gopact-adapters-example",
    "extension_targets": ["missing-target"],
    "required_files": ["go.mod"],
    "required_ci_commands": ["go test -count=1 ./..."]
  }]
}`)
	writeManifestFixture(t, dir, "docs/design/extension-conformance.json", `{
  "version": 1,
  "sdk_compatibility": {
    "module": "github.com/gopact-ai/gopact",
    "go_versions": ["1.25"]
  },
  "targets": [{
    "name": "missing-target",
    "kind": "adapter",
    "source_paths": ["adapters/example"],
    "conformance_suites": ["example"],
    "required_examples": ["minimal example"]
  }]
}`)
	writeManifestFixture(t, dir, "docs/design/extension-scaffold-spec.json", `{
  "version": 1,
  "repositories": [{
    "name": "gopact-adapters-example",
    "module_path": "github.com/gopact-ai/gopact-adapters-example",
    "targets": []
  }]
}`)

	_, err := LoadRepositoriesFromDesign(dir)
	if err == nil {
		t.Fatal("LoadRepositoriesFromDesign() error = nil, want missing scaffold target error")
	}
	if !strings.Contains(err.Error(), "missing-target") {
		t.Fatalf("LoadRepositoriesFromDesign() error = %v, want target name", err)
	}
}

func writeManifestFixture(t *testing.T, root, path, body string) {
	t.Helper()

	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create fixture directory for %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}
