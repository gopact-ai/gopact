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
	"time"

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
		if repo.GoVersion != "1.25.11" {
			t.Fatalf("repository %q GoVersion = %q, want 1.25.11", repo.Name, repo.GoVersion)
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
	if len(target.Migrations) != 1 {
		t.Fatalf("model target Migrations = %#v, want 1 migration", target.Migrations)
	}
	if target.Migrations[0].SourcePath != "adapters/model/openaicompatible" {
		t.Fatalf("model target migration SourcePath = %q, want adapters/model/openaicompatible", target.Migrations[0].SourcePath)
	}
	if target.Migrations[0].Action != "move-to-adapter-repo" {
		t.Fatalf("model target migration Action = %q, want move-to-adapter-repo", target.Migrations[0].Action)
	}

	template := byName["gopact-templates-devagent"]
	if template.Kind != "template" {
		t.Fatalf("gopact-templates-devagent Kind = %q, want template", template.Kind)
	}
	if !slices.Contains(template.SourcePaths, "templates/devagent") {
		t.Fatalf("gopact-templates-devagent SourcePaths = %#v, want templates/devagent", template.SourcePaths)
	}
	if len(template.Targets) != 1 || len(template.Targets[0].Migrations) != 5 {
		t.Fatalf("gopact-templates-devagent migrations = %#v, want 5 source migrations", template.Targets)
	}
	devAgentMigrationPaths := make([]string, 0, len(template.Targets[0].Migrations))
	for _, migration := range template.Targets[0].Migrations {
		devAgentMigrationPaths = append(devAgentMigrationPaths, migration.SourcePath)
	}
	for _, want := range []string{
		"adapters/devagent/channelreview",
		"adapters/devagent/cireview",
		"adapters/devagent/gitdiff",
		"adapters/devagent/modelreview",
		"templates/devagent",
	} {
		if !slices.Contains(devAgentMigrationPaths, want) {
			t.Fatalf("gopact-templates-devagent migration paths = %#v, want %q", devAgentMigrationPaths, want)
		}
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
	if workspace.SecretScript.Path != "sync-secrets.sh" {
		t.Fatalf("secret script path = %q, want sync-secrets.sh", workspace.SecretScript.Path)
	}
	if workspace.RerunScript.Path != "rerun-ci.sh" {
		t.Fatalf("rerun script path = %q, want rerun-ci.sh", workspace.RerunScript.Path)
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
	secretScriptBody, err := os.ReadFile(filepath.Join(dir, "sync-secrets.sh"))
	if err != nil {
		t.Fatalf("read sync-secrets.sh: %v", err)
	}
	rerunScriptBody, err := os.ReadFile(filepath.Join(dir, "rerun-ci.sh"))
	if err != nil {
		t.Fatalf("read rerun-ci.sh: %v", err)
	}
	syncScriptInfo, err := os.Stat(filepath.Join(dir, "sync-repos.sh"))
	if err != nil {
		t.Fatalf("stat sync-repos.sh: %v", err)
	}
	secretScriptInfo, err := os.Stat(filepath.Join(dir, "sync-secrets.sh"))
	if err != nil {
		t.Fatalf("stat sync-secrets.sh: %v", err)
	}
	rerunScriptInfo, err := os.Stat(filepath.Join(dir, "rerun-ci.sh"))
	if err != nil {
		t.Fatalf("stat rerun-ci.sh: %v", err)
	}
	if syncScriptInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("sync-repos.sh mode = %s, want executable bit", syncScriptInfo.Mode())
	}
	if secretScriptInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("sync-secrets.sh mode = %s, want executable bit", secretScriptInfo.Mode())
	}
	if rerunScriptInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("rerun-ci.sh mode = %s, want executable bit", rerunScriptInfo.Mode())
	}
	if !strings.Contains(string(syncPlanBody), `"create_command": "gh repo create gopact-ai/gopact-adapters-model --private --source <generated>/gopact-adapters-model --remote origin --push"`) {
		t.Fatalf("sync-plan.json missing unescaped create command:\n%s", string(syncPlanBody))
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"gh repo view \"${repo}\"",
		"sync_repo 'gopact-adapters-model' 'gopact-adapters-model' 'private'",
		"prepare_remote_go_module \"${repo_dir}\"",
		"GOWORK=off",
		"git -C \"${repo_dir}\" commit -m \"chore: bootstrap gopact extension scaffold\"",
		"gh repo create \"${repo}\" \"${visibility_flag}\" --source \"${repo_dir}\" --remote origin --push",
	} {
		if !strings.Contains(string(syncScriptBody), want) {
			t.Fatalf("sync-repos.sh missing %q:\n%s", want, string(syncScriptBody))
		}
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"GOPACT_GITHUB_TOKEN must contain a GitHub token",
		"gh secret set GOPACT_GITHUB_TOKEN",
		"set_secret 'gopact-ai/gopact-adapters-model'",
		"set_secret 'gopact-ai/gopact-templates-agenttool'",
	} {
		if !strings.Contains(string(secretScriptBody), want) {
			t.Fatalf("sync-secrets.sh missing %q:\n%s", want, string(secretScriptBody))
		}
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"rerun_ci 'gopact-ai/gopact-adapters-model' 'ci' 'main'",
		"rerun_ci 'gopact-ai/gopact-templates-agenttool' 'ci' 'main'",
		"gh run rerun -R \"${repo}\" \"${latest_id}\"",
	} {
		if !strings.Contains(string(rerunScriptBody), want) {
			t.Fatalf("rerun-ci.sh missing %q:\n%s", want, string(rerunScriptBody))
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

func TestVerifyBootstrapWorkspaceRunsCommandLinesWithShell(t *testing.T) {
	dir := tempDirWithRetryCleanup(t)
	repoDir := filepath.Join(dir, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("create generated repository: %v", err)
	}

	requiredCommands := []string{
		"printf shell-one > shell-one.txt",
		"printf shell-two > shell-two.txt",
	}
	workspace := BootstrapWorkspace{
		Scaffolds: []RepositoryScaffold{{
			Repository: Repository{
				Name:               "repo",
				RequiredCICommands: requiredCommands,
			},
			Directory: "repo",
		}},
	}

	report, err := VerifyBootstrapWorkspace(context.Background(), dir, workspace)
	if err != nil {
		t.Fatalf("VerifyBootstrapWorkspace() error = %v", err)
	}
	if len(report.Results) != len(requiredCommands) {
		t.Fatalf("verification results = %d, want %d", len(report.Results), len(requiredCommands))
	}
	gotCommands := verificationCommandsForRepository(report.Results, "repo")
	if !stringSlicesEqual(gotCommands, requiredCommands) {
		t.Fatalf("verification commands = %#v, want required CI commands %#v", gotCommands, requiredCommands)
	}
	for _, path := range []string{"shell-one.txt", "shell-two.txt"} {
		if _, err := os.Stat(filepath.Join(repoDir, path)); err != nil {
			t.Fatalf("shell command output %s missing: %v", path, err)
		}
	}
}

func TestVerifyBootstrapWorkspaceRemovesTemporaryGitRepository(t *testing.T) {
	dir := tempDirWithRetryCleanup(t)
	repoDir := filepath.Join(dir, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("create generated repository: %v", err)
	}

	workspace := BootstrapWorkspace{
		Scaffolds: []RepositoryScaffold{{
			Repository: Repository{
				Name: "repo",
				RequiredCICommands: []string{
					"git diff --check",
				},
			},
			Directory: "repo",
		}},
	}

	if _, err := VerifyBootstrapWorkspace(context.Background(), dir, workspace); err != nil {
		t.Fatalf("VerifyBootstrapWorkspace() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("temporary .git stat error = %v, want not exist", err)
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
	if model.RerunCommand != "gh run rerun -R gopact-ai/gopact-adapters-model <latest-ci-run-id>" {
		t.Fatalf("model RerunCommand = %q", model.RerunCommand)
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
		"prepare_remote_go_module \"${repo_dir}\"",
		"GIT_CONFIG_KEY_0=\"${git_key}\"",
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

func TestRenderSecretScriptFromDesignCapturesRemoteSecretSteps(t *testing.T) {
	file, err := RenderSecretScriptFromDesign(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("RenderSecretScriptFromDesign() error = %v", err)
	}
	if file.Path != "sync-secrets.sh" {
		t.Fatalf("script path = %q, want sync-secrets.sh", file.Path)
	}
	for _, want := range []string{
		"set -euo pipefail",
		"GOPACT_GITHUB_TOKEN must contain a GitHub token",
		"gh secret set GOPACT_GITHUB_TOKEN",
		"set_secret 'gopact-ai/gopact-adapters-model'",
		"set_secret 'gopact-ai/gopact-templates-devagent'",
	} {
		if !strings.Contains(file.Body, want) {
			t.Fatalf("script missing %q:\n%s", want, file.Body)
		}
	}
}

func TestRenderRerunScriptFromDesignCapturesRemoteCIRerunSteps(t *testing.T) {
	file, err := RenderRerunScriptFromDesign(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("RenderRerunScriptFromDesign() error = %v", err)
	}
	if file.Path != "rerun-ci.sh" {
		t.Fatalf("script path = %q, want rerun-ci.sh", file.Path)
	}
	for _, want := range []string{
		"set -euo pipefail",
		"require_secret 'gopact-ai/gopact-adapters-model'",
		"gh run list -R \"${repo}\" --workflow \"${workflow}\" --branch \"${branch}\"",
		"gh run rerun -R \"${repo}\" \"${latest_id}\"",
		"gh workflow run \"${workflow}\" -R \"${repo}\" --ref \"${branch}\"",
		"rerun_ci 'gopact-ai/gopact-adapters-model' 'ci' 'main'",
		"rerun_ci 'gopact-ai/gopact-templates-devagent' 'ci' 'main'",
	} {
		if !strings.Contains(file.Body, want) {
			t.Fatalf("script missing %q:\n%s", want, file.Body)
		}
	}
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
  "version": 1,
  "sdk_compatibility": {
    "module": "github.com/gopact-ai/gopact",
    "go_versions": ["1.25.11"]
  },
  "targets": [{
    "name": "missing-target",
    "kind": "adapter",
    "source_paths": ["adapters/example"],
    "conformance_suites": ["example"],
    "required_examples": ["minimal example"]
  }]
}`)
  "version": 1,
  "repositories": [{
    "name": "gopact-adapters-example",
    "module_path": "github.com/gopact-ai/gopact-adapters-example",
    "targets": []
  }]
}`)
  "version": 1,
  "repository_migrations": []
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

func tempDirWithRetryCleanup(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "gopact-extscaffold-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		var removeErr error
		for range 10 {
			removeErr = os.RemoveAll(dir)
			if removeErr == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("remove temp dir %s: %v", dir, removeErr)
	})
	return dir
}
