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
	if model.DefaultBranch != "main" {
		t.Fatalf("model DefaultBranch = %q, want main", model.DefaultBranch)
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	for _, want := range []string{
		"repo view gopact-ai/gopact-adapters-model",
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
	if report.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d, want 0", report.ReadyCount)
	}
}

func writeFakeGH(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return path
}
