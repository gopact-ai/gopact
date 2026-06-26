package extensionscaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRemoteRepositoriesReportsGitHubStatus(t *testing.T) {
	ghPath := writeFakeGH(t, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GH_LOG}"
if [[ "$1" == "repo" && "$2" == "view" ]]; then
  repo="$3"
  name="${repo#*/}"
  printf '{"name":"%s","visibility":"PRIVATE","isPrivate":true,"url":"https://github.com/%s","defaultBranchRef":{"name":"main"}}' "${name}" "${repo}"
  exit 0
fi
if [[ "$1" == "api" ]]; then
  printf '{".github/workflows/ci.yml":"present","path":".github/workflows/ci.yml"}'
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
echo "unexpected gh args: $*" >&2
exit 2
`)
	logPath := filepath.Join(t.TempDir(), "gh.log")
	t.Setenv("GH_LOG", logPath)

	report, err := CheckRemoteRepositories(context.Background(), filepath.Join("..", ".."), RemoteStatusOptions{
		GHPath: ghPath,
	})
	if err != nil {
		t.Fatalf("CheckRemoteRepositories() error = %v", err)
	}
	if report.Organization != "gopact-ai" {
		t.Fatalf("Organization = %q, want gopact-ai", report.Organization)
	}
	if len(report.Repositories) != expectedScaffoldRepositoryCount {
		t.Fatalf("repositories = %d, want %d", len(report.Repositories), expectedScaffoldRepositoryCount)
	}
	model := report.Repository("gopact-adapters-model")
	if model == nil {
		t.Fatal("report missing gopact-adapters-model")
	}
	if !model.Exists || !model.Private || !model.CIWorkflowPresent || !model.CIRunPassed || !model.Ready {
		t.Fatalf("model status = %+v, want existing private repo with passing CI workflow ready", *model)
	}
	if len(model.BlockingReasons) != 0 || len(model.RequiredActions) != 0 {
		t.Fatalf("model remediation = %v/%v, want empty for ready repository", model.BlockingReasons, model.RequiredActions)
	}
	if model.PrivateSDKSecretName != "GOPACT_GITHUB_TOKEN" || !model.PrivateSDKSecretPresent {
		t.Fatalf("model private SDK secret = %q/%v, want GOPACT_GITHUB_TOKEN present", model.PrivateSDKSecretName, model.PrivateSDKSecretPresent)
	}
	if model.DefaultBranch != "main" {
		t.Fatalf("model DefaultBranch = %q, want main", model.DefaultBranch)
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	for _, want := range []string{
		"repo view gopact-ai/gopact-adapters-model",
		"secret list -R gopact-ai/gopact-adapters-model",
		"api repos/gopact-ai/gopact-adapters-model/contents/.github/workflows/ci.yml",
		"run list -R gopact-ai/gopact-adapters-model",
	} {
		if !strings.Contains(string(logBody), want) {
			t.Fatalf("gh log missing %q:\n%s", want, string(logBody))
		}
	}
}

func TestCheckRemoteRepositoriesReportsMissingWorkflow(t *testing.T) {
	ghPath := writeFakeGH(t, `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "repo" && "$2" == "view" ]]; then
  repo="$3"
  name="${repo#*/}"
  printf '{"name":"%s","visibility":"PRIVATE","isPrivate":true,"url":"https://github.com/%s","defaultBranchRef":{"name":"main"}}' "${name}" "${repo}"
  exit 0
fi
if [[ "$1" == "api" ]]; then
  echo "not found" >&2
  exit 1
fi
if [[ "$1" == "secret" && "$2" == "list" ]]; then
  printf '[]'
  exit 0
fi
if [[ "$1" == "run" && "$2" == "list" ]]; then
  printf '[]'
  exit 0
fi
exit 2
`)

	report, err := CheckRemoteRepositories(context.Background(), filepath.Join("..", ".."), RemoteStatusOptions{
		GHPath: ghPath,
	})
	if err != nil {
		t.Fatalf("CheckRemoteRepositories() error = %v", err)
	}
	model := report.Repository("gopact-adapters-model")
	if model == nil {
		t.Fatal("report missing gopact-adapters-model")
	}
	if model.Ready {
		t.Fatalf("model Ready = true, want false for missing workflow")
	}
	if model.CIWorkflowPresent {
		t.Fatalf("model CIWorkflowPresent = true, want false")
	}
	if model.CIWorkflowError == "" {
		t.Fatalf("model CIWorkflowError is empty")
	}
	if model.PrivateSDKSecretPresent {
		t.Fatalf("model PrivateSDKSecretPresent = true, want false")
	}
	assertContainsString(t, model.BlockingReasons, "ci workflow is missing")
	assertContainsString(t, model.BlockingReasons, "GOPACT_GITHUB_TOKEN secret is missing")
	assertContainsString(t, model.RequiredActions, "push .github/workflows/ci.yml with sync-repos.sh")
	assertContainsString(t, model.RequiredActions, "configure GOPACT_GITHUB_TOKEN with sync-secrets.sh")
	if report.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d, want 0", report.ReadyCount)
	}
}

func TestCheckRemoteRepositoriesReportsMissingPrivateSDKSecret(t *testing.T) {
	ghPath := writeFakeGH(t, `#!/usr/bin/env bash
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

	report, err := CheckRemoteRepositories(context.Background(), filepath.Join("..", ".."), RemoteStatusOptions{
		GHPath: ghPath,
	})
	if err != nil {
		t.Fatalf("CheckRemoteRepositories() error = %v", err)
	}
	model := report.Repository("gopact-adapters-model")
	if model == nil {
		t.Fatal("report missing gopact-adapters-model")
	}
	if model.PrivateSDKSecretName != "GOPACT_GITHUB_TOKEN" {
		t.Fatalf("PrivateSDKSecretName = %q, want GOPACT_GITHUB_TOKEN", model.PrivateSDKSecretName)
	}
	if model.PrivateSDKSecretPresent {
		t.Fatalf("PrivateSDKSecretPresent = true, want false")
	}
	if model.Ready {
		t.Fatalf("Ready = true, want false while latest CI failed")
	}
	assertContainsString(t, model.BlockingReasons, "GOPACT_GITHUB_TOKEN secret is missing")
	assertContainsString(t, model.BlockingReasons, "latest ci workflow run did not pass")
	assertContainsString(t, model.RequiredActions, "configure GOPACT_GITHUB_TOKEN with sync-secrets.sh")
	assertContainsString(t, model.RequiredActions, "rerun ci workflow with rerun-ci.sh after fixing blockers")
}

func TestCheckRemoteRepositoriesRequiresPrivateSDKSecretForReadiness(t *testing.T) {
	ghPath := writeFakeGH(t, `#!/usr/bin/env bash
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
  printf '[{"databaseId":123,"workflowName":"ci","status":"completed","conclusion":"success","event":"push","headBranch":"main","url":"https://github.com/%s/actions/runs/123"}]' "$4"
  exit 0
fi
exit 2
`)

	report, err := CheckRemoteRepositories(context.Background(), filepath.Join("..", ".."), RemoteStatusOptions{
		GHPath: ghPath,
	})
	if err != nil {
		t.Fatalf("CheckRemoteRepositories() error = %v", err)
	}
	model := report.Repository("gopact-adapters-model")
	if model == nil {
		t.Fatal("report missing gopact-adapters-model")
	}
	if model.Ready {
		t.Fatalf("Ready = true, want false while GOPACT_GITHUB_TOKEN is missing")
	}
	assertContainsString(t, model.BlockingReasons, "GOPACT_GITHUB_TOKEN secret is missing")
	assertContainsString(t, model.RequiredActions, "configure GOPACT_GITHUB_TOKEN with sync-secrets.sh")
}

func writeFakeGH(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return path
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
