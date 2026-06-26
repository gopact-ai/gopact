package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const expectedScaffoldRepositoryCount = 11

func TestRunDryRunPrintsRepositoryPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../..", "-dry-run"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"gopact-adapters-model",
		"gopact-templates-react",
		"gopact-templates-devagent",
		"5 files",
		"dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunPrintsSyncPlanJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../..", "-plan-json"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var plan struct {
		Organization string `json:"organization"`
		Repositories []struct {
			Name          string `json:"name"`
			CreateCommand string `json:"create_command"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("stdout is not sync plan JSON: %v\n%s", err, stdout.String())
	}
	if plan.Organization != "gopact-ai" {
		t.Fatalf("plan Organization = %q, want gopact-ai", plan.Organization)
	}
	if len(plan.Repositories) != expectedScaffoldRepositoryCount {
		t.Fatalf("plan repositories = %d, want %d", len(plan.Repositories), expectedScaffoldRepositoryCount)
	}
	if plan.Repositories[0].Name == "" {
		t.Fatal("first repository name is empty")
	}
	if !strings.Contains(stdout.String(), "gh repo create gopact-ai/gopact-adapters-model --private") {
		t.Fatalf("stdout missing private repo create command:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "gopact-templates-react") {
		t.Fatalf("stdout missing ReAct template repository:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "<generated>/gopact-adapters-model") {
		t.Fatalf("stdout escaped generated source path:\n%s", stdout.String())
	}
}

func TestRunPrintsSyncPlanShellScript(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../..", "-plan-sh"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"local organization='gopact-ai'",
		"prepare_remote_go_module \"${repo_dir}\"",
		"GOWORK=off",
		"copy_generated_scaffold \"${repo_dir}\" \"${sync_dir}\"",
		"gh repo clone \"${repo}\" \"${sync_dir}\"",
		"run_ci_command \"${repo_dir}\" \"${command}\"",
		"sync_repo 'gopact-adapters-model' 'gopact-adapters-model' 'private' 'git diff --check' 'go test -count=1 ./...' 'go vet ./...'",
		"gh repo create \"${repo}\" \"${visibility_flag}\" --source \"${repo_dir}\" --remote origin --push",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunPrintsSecretSyncShellScript(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../..", "-plan-secrets-sh"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"GOPACT_GITHUB_TOKEN must contain a GitHub token",
		"gh secret set GOPACT_GITHUB_TOKEN",
		"set_secret 'gopact-ai/gopact-adapters-model'",
		"set_secret 'gopact-ai/gopact-templates-agenttool'",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunPrintsCIRerunShellScript(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../..", "-plan-rerun-sh"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"require_secret 'gopact-ai/gopact-adapters-model'",
		"gh run list -R \"${repo}\" --workflow \"${workflow}\" --branch \"${branch}\"",
		"gh run rerun -R \"${repo}\" \"${latest_id}\"",
		"rerun_ci 'gopact-ai/gopact-adapters-model' 'ci' 'main'",
		"rerun_ci 'gopact-ai/gopact-templates-agenttool' 'ci' 'main'",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunPrintsRemoteStatusJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := installFakeGH(t, `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "repo" && "$2" == "view" ]]; then
  repo="$3"
  name="${repo#*/}"
  printf '{"name":"%s","visibility":"PRIVATE","isPrivate":true,"url":"https://github.com/%s","defaultBranchRef":{"name":"main"}}' "${name}" "${repo}"
  exit 0
fi
if [[ "$1" == "api" ]]; then
  printf '{"path":".github/workflows/ci.yml"}'
  exit 0
fi
if [[ "$1" == "secret" && "$2" == "list" ]]; then
  printf '[{"name":"GOPACT_GITHUB_TOKEN"}]'
  exit 0
fi
if [[ "$1" == "run" && "$2" == "list" ]]; then
  printf '[{"databaseId":123,"workflowName":"ci","status":"completed","conclusion":"success","event":"push","headBranch":"main","url":"https://github.com/%s/actions/runs/123"}]' "$4"
  exit 0
fi
exit 2
`)
	defer restore()

	code := run(context.Background(), []string{"-root", "../..", "-remote-status-json"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var report struct {
		Organization string `json:"organization"`
		ReadyCount   int    `json:"ready_count"`
		Repositories []struct {
			Name                    string `json:"name"`
			Exists                  bool   `json:"exists"`
			Private                 bool   `json:"private"`
			CIWorkflowPresent       bool   `json:"ci_workflow_present"`
			CIRunPassed             bool   `json:"ci_run_passed"`
			PrivateSDKSecretPresent bool   `json:"private_sdk_token_secret_present"`
			Ready                   bool   `json:"ready"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not remote status JSON: %v\n%s", err, stdout.String())
	}
	if report.Organization != "gopact-ai" {
		t.Fatalf("Organization = %q, want gopact-ai", report.Organization)
	}
	if report.ReadyCount != expectedScaffoldRepositoryCount {
		t.Fatalf("ReadyCount = %d, want %d", report.ReadyCount, expectedScaffoldRepositoryCount)
	}
	if len(report.Repositories) != expectedScaffoldRepositoryCount {
		t.Fatalf("repositories = %d, want %d", len(report.Repositories), expectedScaffoldRepositoryCount)
	}
	if !report.Repositories[0].Exists || !report.Repositories[0].Private || !report.Repositories[0].CIWorkflowPresent || !report.Repositories[0].CIRunPassed || !report.Repositories[0].PrivateSDKSecretPresent || !report.Repositories[0].Ready {
		t.Fatalf("first repository status = %+v, want ready", report.Repositories[0])
	}
}

func TestRunPrintsRemoteStatusRemediationJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := installFakeGH(t, `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "repo" && "$2" == "view" ]]; then
  repo="$3"
  name="${repo#*/}"
  printf '{"name":"%s","visibility":"PRIVATE","isPrivate":true,"url":"https://github.com/%s","defaultBranchRef":{"name":"main"}}' "${name}" "${repo}"
  exit 0
fi
if [[ "$1" == "api" ]]; then
  printf '{"path":".github/workflows/ci.yml"}'
  exit 0
fi
if [[ "$1" == "secret" && "$2" == "list" ]]; then
  printf '[]'
  exit 0
fi
if [[ "$1" == "run" && "$2" == "list" ]]; then
  printf '[{"databaseId":123,"workflowName":"ci","status":"completed","conclusion":"failure","event":"push","headBranch":"main","url":"https://github.com/%s/actions/runs/123"}]' "$4"
  exit 0
fi
exit 2
`)
	defer restore()

	code := run(context.Background(), []string{"-root", "../..", "-remote-status-json"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var report struct {
		Repositories []struct {
			Name             string   `json:"name"`
			BlockingReasons  []string `json:"blocking_reasons"`
			RequiredActions  []string `json:"required_actions"`
			CIRunPassed      bool     `json:"ci_run_passed"`
			PrivateSDKSecret bool     `json:"private_sdk_token_secret_present"`
			Ready            bool     `json:"ready"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not remote status JSON: %v\n%s", err, stdout.String())
	}
	if len(report.Repositories) != expectedScaffoldRepositoryCount {
		t.Fatalf("repositories = %d, want %d", len(report.Repositories), expectedScaffoldRepositoryCount)
	}
	first := report.Repositories[0]
	if first.Ready || first.CIRunPassed || first.PrivateSDKSecret {
		t.Fatalf("first repository status = %+v, want not ready due to failed CI and missing secret", first)
	}
	assertContainsString(t, first.BlockingReasons, "GOPACT_GITHUB_TOKEN secret is missing")
	assertContainsString(t, first.BlockingReasons, "latest ci workflow run did not pass")
	assertContainsString(t, first.RequiredActions, "configure GOPACT_GITHUB_TOKEN with sync-secrets.sh")
	assertContainsString(t, first.RequiredActions, "rerun ci workflow with rerun-ci.sh after fixing blockers")
}

func TestRunPrintsRemoteStatusEvidenceJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := installFakeGH(t, `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "repo" && "$2" == "view" ]]; then
  repo="$3"
  name="${repo#*/}"
  printf '{"name":"%s","visibility":"PRIVATE","isPrivate":true,"url":"https://github.com/%s","defaultBranchRef":{"name":"main"}}' "${name}" "${repo}"
  exit 0
fi
if [[ "$1" == "api" ]]; then
  printf '{"path":".github/workflows/ci.yml"}'
  exit 0
fi
if [[ "$1" == "secret" && "$2" == "list" ]]; then
  printf '[]'
  exit 0
fi
if [[ "$1" == "run" && "$2" == "list" ]]; then
  printf '[{"databaseId":123,"workflowName":"ci","status":"completed","conclusion":"failure","event":"push","headBranch":"main","url":"https://github.com/%s/actions/runs/123"}]' "$4"
  exit 0
fi
exit 2
`)
	defer restore()

	code := run(context.Background(), []string{"-root", "../..", "-remote-status-evidence-json"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var check struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Metadata struct {
			Organization        string `json:"organization"`
			RepositoryCount     int    `json:"repository_count"`
			ReadyCount          int    `json:"ready_count"`
			MissingCount        int    `json:"missing_count"`
			BlockingReasonCount int    `json:"blocking_reason_count"`
			RequiredActionCount int    `json:"required_action_count"`
		} `json:"metadata"`
		Evidence []struct {
			Type     string `json:"type"`
			Ref      string `json:"ref"`
			Metadata struct {
				Repository                   string   `json:"repository"`
				Remote                       string   `json:"remote"`
				Status                       string   `json:"status"`
				PrivateSDKTokenSecretPresent bool     `json:"private_sdk_token_secret_present"`
				CIRunPassed                  bool     `json:"ci_run_passed"`
				Ready                        bool     `json:"ready"`
				BlockingReasons              []string `json:"blocking_reasons"`
				RequiredActions              []string `json:"required_actions"`
			} `json:"metadata"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &check); err != nil {
		t.Fatalf("stdout is not remote status evidence JSON: %v\n%s", err, stdout.String())
	}
	if check.ID != "external-repositories:gopact-ai" ||
		check.Name != "external repository readiness" ||
		check.Status != "failed" {
		t.Fatalf("check = %+v, want failed external repository readiness check", check)
	}
	if check.Metadata.Organization != "gopact-ai" ||
		check.Metadata.RepositoryCount != expectedScaffoldRepositoryCount ||
		check.Metadata.ReadyCount != 0 ||
		check.Metadata.MissingCount != expectedScaffoldRepositoryCount ||
		check.Metadata.BlockingReasonCount != expectedScaffoldRepositoryCount*2 ||
		check.Metadata.RequiredActionCount != expectedScaffoldRepositoryCount*2 {
		t.Fatalf("metadata = %+v, want remote readiness counts", check.Metadata)
	}
	if len(check.Evidence) != expectedScaffoldRepositoryCount {
		t.Fatalf("evidence count = %d, want %d", len(check.Evidence), expectedScaffoldRepositoryCount)
	}
	first := check.Evidence[0]
	if first.Type != "external_repository_readiness" ||
		first.Ref != "external-repository:gopact-ai/gopact-adapters-model" ||
		first.Metadata.Repository != "gopact-adapters-model" ||
		first.Metadata.Remote != "gopact-ai/gopact-adapters-model" ||
		first.Metadata.Status != "failed" ||
		first.Metadata.PrivateSDKTokenSecretPresent ||
		first.Metadata.CIRunPassed ||
		first.Metadata.Ready {
		t.Fatalf("first evidence = %+v, want failed repository readiness evidence", first)
	}
	assertContainsString(t, first.Metadata.BlockingReasons, "GOPACT_GITHUB_TOKEN secret is missing")
	assertContainsString(t, first.Metadata.RequiredActions, "configure GOPACT_GITHUB_TOKEN with sync-secrets.sh")
}

func TestRunWritesScaffoldWorkspace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	dir := t.TempDir()

	code := run(context.Background(), []string{"-root", "../..", "-out", dir}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote 11 repositories") {
		t.Fatalf("stdout = %q, want written repository count", stdout.String())
	}
	if !strings.Contains(stdout.String(), "go.work") {
		t.Fatalf("stdout = %q, want go.work summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "sync-plan.json") {
		t.Fatalf("stdout = %q, want sync-plan.json summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "sync-repos.sh") {
		t.Fatalf("stdout = %q, want sync-repos.sh summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "sync-secrets.sh") {
		t.Fatalf("stdout = %q, want sync-secrets.sh summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "rerun-ci.sh") {
		t.Fatalf("stdout = %q, want rerun-ci.sh summary", stdout.String())
	}

	for _, path := range []string{
		"go.work",
		"sync-plan.json",
		"sync-repos.sh",
		"sync-secrets.sh",
		"rerun-ci.sh",
		"gopact-adapters-model/go.mod",
		"gopact-adapters-model/README.md",
		"gopact-adapters-model/CONFORMANCE.md",
		"gopact-adapters-model/.github/workflows/ci.yml",
		"gopact-adapters-model/examples/minimal_test.go",
		"gopact-templates-react/go.mod",
		"gopact-templates-react/examples/minimal_test.go",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(path))); err != nil {
			t.Fatalf("written scaffold missing %s: %v", path, err)
		}
	}
}

func TestRunWritesAndVerifiesScaffoldWorkspace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := writeScaffoldFixture(t)
	dir := t.TempDir()

	code := run(context.Background(), []string{"-root", root, "-out", dir, "-verify"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"wrote 1 repositories",
		"verified 2 checks across 1 repositories",
		"gopact-adapters-example\tprintf cli-one > cli-one.txt\tok",
		"gopact-adapters-example\tprintf cli-two > cli-two.txt\tok",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	readme := readFixtureOutputFile(t, dir, "gopact-adapters-example/README.md")
	conformance := readFixtureOutputFile(t, dir, "gopact-adapters-example/CONFORMANCE.md")
	for _, content := range []string{readme, conformance} {
		for _, want := range []string{
			"V1 Migration Ownership",
			"adapters/example",
			"move-to-adapter-repo",
			"Example adapter leaves core after the external repository owns the source path.",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("generated scaffold document missing %q:\n%s", want, content)
			}
		}
	}
}

func writeScaffoldFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeFixtureFile(t, root, "docs/design/external-repositories.json", `{
  "version": 1,
  "organization": "gopact-ai",
  "default_visibility": "private",
  "bootstrap_sequence": ["create-private-repos"],
  "repositories": [{
    "name": "gopact-adapters-example",
    "route": "adapter-repo",
    "visibility": "private",
    "module_path": "github.com/gopact-ai/gopact-adapters-example",
    "scaffold_status": "ready-to-create",
    "host_owned_config": true,
    "extension_targets": ["gopact-adapters-example-target"],
    "required_files": [
      "go.mod",
      "README.md",
      "CONFORMANCE.md",
      "examples/minimal_test.go",
      ".github/workflows/ci.yml"
    ],
    "required_ci_commands": [
      "printf cli-one > cli-one.txt",
      "printf cli-two > cli-two.txt"
    ]
  }]
}`)
	writeFixtureFile(t, root, "docs/design/extension-conformance.json", `{
  "version": 1,
  "sdk_compatibility": {
    "module": "github.com/gopact-ai/gopact",
    "go_versions": ["1.25.11"]
  },
  "targets": [{
    "name": "gopact-adapters-example-target",
    "kind": "adapter",
    "source_paths": ["adapters/example"],
    "conformance_suites": ["gopacttest-extension-scaffold-conformance"],
    "required_examples": ["minimal example"]
  }]
}`)
	writeFixtureFile(t, root, "docs/design/extension-scaffold-spec.json", `{
  "version": 1,
  "repositories": [{
    "name": "gopact-adapters-example",
    "module_path": "github.com/gopact-ai/gopact-adapters-example",
    "targets": [{
      "name": "gopact-adapters-example-target",
      "package_path": "example",
      "minimal_example_path": "examples/minimal_test.go"
    }]
	}]
}`)
	writeFixtureFile(t, root, "docs/design/v1-migration-plan.json", `{
  "version": 1,
  "repository_migrations": [{
    "source_path": "adapters/example",
    "action": "move-to-adapter-repo",
    "extension_target": "gopact-adapters-example-target",
    "v1_condition": "Example adapter leaves core after the external repository owns the source path."
  }]
}`)
	return root
}

func readFixtureOutputFile(t *testing.T, root, path string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read fixture output %s: %v", path, err)
	}
	return string(body)
}

func writeFixtureFile(t *testing.T, root, path, body string) {
	t.Helper()

	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create fixture directory for %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func TestRunRejectsMissingOutputDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../.."}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "-out is required") {
		t.Fatalf("stderr = %q, want missing output error", stderr.String())
	}
}

func installFakeGH(t *testing.T, body string) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	return func() {
		t.Setenv("PATH", oldPath)
	}
}

func assertContainsString(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("values = %v, want to contain %q", values, want)
}

func TestRunRejectsConflictingPlanModes(t *testing.T) {
	tests := [][]string{
		{"-root", "../..", "-dry-run", "-plan-sh"},
		{"-root", "../..", "-plan-json", "-plan-sh"},
		{"-root", "../..", "-plan-json", "-plan-secrets-sh"},
		{"-root", "../..", "-plan-secrets-sh", "-plan-rerun-sh"},
		{"-root", "../..", "-remote-status-json", "-plan-secrets-sh"},
		{"-root", "../..", "-remote-status-json", "-plan-rerun-sh"},
		{"-root", "../..", "-remote-status-json", "-remote-status-evidence-json"},
		{"-root", "../..", "-remote-status-evidence-json", "-plan-rerun-sh"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer

		code := run(context.Background(), args, &stdout, &stderr)
		if code != exitUsage {
			t.Fatalf("run(%v) code = %d, want %d", args, code, exitUsage)
		}
		if stdout.Len() != 0 {
			t.Fatalf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "cannot be used") {
			t.Fatalf("run(%v) stderr = %q, want conflict error", args, stderr.String())
		}
	}
}

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-root", "../..", "-dry-run", "extra"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Fatalf("stderr = %q, want unexpected arguments error", stderr.String())
	}
}
