package extensionscaffold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// SyncPlan is a machine-readable dry-run plan for external repository bootstrap.
type SyncPlan struct {
	Organization      string               `json:"organization"`
	DefaultVisibility string               `json:"default_visibility"`
	Sequence          []string             `json:"sequence"`
	Repositories      []SyncRepositoryPlan `json:"repositories"`
}

// SyncRepositoryPlan describes how one generated scaffold should be synced to its private repository.
type SyncRepositoryPlan struct {
	Name             string   `json:"name"`
	Directory        string   `json:"directory"`
	Route            string   `json:"route"`
	Visibility       string   `json:"visibility"`
	ModulePath       string   `json:"module_path"`
	ScaffoldStatus   string   `json:"scaffold_status"`
	HostOwnedConfig  bool     `json:"host_owned_config"`
	ExtensionTargets []string `json:"extension_targets"`
	Files            []string `json:"files"`
	CICommands       []string `json:"ci_commands"`
	VerifyCommands   []string `json:"verify_commands"`
	CreateCommand    string   `json:"create_command"`
	RerunCommand     string   `json:"rerun_command"`
}

// RenderSyncPlanFromDesign renders the remote bootstrap dry-run plan from design manifests.
func RenderSyncPlanFromDesign(root string) (SyncPlan, error) {
	manifest, err := loadJSON[designExternalRepositoryManifest](root, externalRepositoriesManifestPath)
	if err != nil {
		return SyncPlan{}, err
	}
	scaffolds, err := RenderRepositoriesFromDesign(root)
	if err != nil {
		return SyncPlan{}, err
	}

	byName := make(map[string]designExternalRepository, len(manifest.Repositories))
	for _, repo := range manifest.Repositories {
		byName[repo.Name] = repo
	}

	plan := SyncPlan{
		Organization:      manifest.Organization,
		DefaultVisibility: manifest.DefaultVisibility,
		Sequence:          append([]string(nil), manifest.BootstrapSequence...),
		Repositories:      make([]SyncRepositoryPlan, 0, len(scaffolds)),
	}
	for _, scaffold := range scaffolds {
		manifestRepo, ok := byName[scaffold.Repository.Name]
		if !ok {
			return SyncPlan{}, fmt.Errorf("extensionscaffold: repository %q missing external repository manifest", scaffold.Repository.Name)
		}
		visibility := strings.TrimSpace(manifestRepo.Visibility)
		if visibility == "" {
			visibility = manifest.DefaultVisibility
		}
		files := make([]string, 0, len(scaffold.Files))
		for _, file := range scaffold.Files {
			files = append(files, file.Path)
		}
		plan.Repositories = append(plan.Repositories, SyncRepositoryPlan{
			Name:             scaffold.Repository.Name,
			Directory:        scaffold.Directory,
			Route:            manifestRepo.Route,
			Visibility:       visibility,
			ModulePath:       scaffold.Repository.ModulePath,
			ScaffoldStatus:   manifestRepo.ScaffoldStatus,
			HostOwnedConfig:  manifestRepo.HostOwnedConfig,
			ExtensionTargets: append([]string(nil), manifestRepo.ExtensionTargets...),
			Files:            files,
			CICommands:       append([]string(nil), scaffold.Repository.RequiredCICommands...),
			VerifyCommands:   append([]string(nil), scaffold.Repository.RequiredCICommands...),
			CreateCommand:    renderCreateCommand(manifest.Organization, scaffold.Directory, visibility),
			RerunCommand:     renderRerunCommand(manifest.Organization, scaffold.Repository.Name),
		})
	}
	return plan, nil
}

func renderCreateCommand(organization, directory, visibility string) string {
	visibilityFlag := "--private"
	if visibility == "public" {
		visibilityFlag = "--public"
	}
	return fmt.Sprintf("gh repo create %s/%s %s --source <generated>/%s --remote origin --push", organization, directory, visibilityFlag, directory)
}

func renderRerunCommand(organization, name string) string {
	return fmt.Sprintf("gh run rerun -R %s/%s <latest-ci-run-id>", organization, name)
}

func renderSyncPlanFile(plan SyncPlan) (File, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(plan); err != nil {
		return File{}, fmt.Errorf("extensionscaffold: encode sync plan: %w", err)
	}
	return File{Path: "sync-plan.json", Body: body.String()}, nil
}

// RenderSyncScriptFromDesign renders a reviewable shell script for syncing generated scaffolds.
func RenderSyncScriptFromDesign(root string) (File, error) {
	plan, err := RenderSyncPlanFromDesign(root)
	if err != nil {
		return File{}, err
	}
	return renderSyncScriptFile(plan), nil
}

// RenderSecretScriptFromDesign renders a reviewable shell script for configuring required repository secrets.
func RenderSecretScriptFromDesign(root string) (File, error) {
	plan, err := RenderSyncPlanFromDesign(root)
	if err != nil {
		return File{}, err
	}
	return renderSecretScriptFile(plan), nil
}

// RenderRerunScriptFromDesign renders a reviewable shell script for rerunning external repository CI.
func RenderRerunScriptFromDesign(root string) (File, error) {
	plan, err := RenderSyncPlanFromDesign(root)
	if err != nil {
		return File{}, err
	}
	return renderRerunScriptFile(plan), nil
}

func renderSyncScriptFile(plan SyncPlan) File {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("generated_root=\"${1:-$(cd \"$(dirname \"${BASH_SOURCE[0]}\")\" && pwd)}\"\n\n")
	b.WriteString("if ! command -v gh >/dev/null 2>&1; then\n")
	b.WriteString("  echo \"gh CLI is required to sync external repositories\" >&2\n")
	b.WriteString("  exit 127\n")
	b.WriteString("fi\n\n")
	b.WriteString("if ! command -v git >/dev/null 2>&1; then\n")
	b.WriteString("  echo \"git is required to sync external repositories\" >&2\n")
	b.WriteString("  exit 127\n")
	b.WriteString("fi\n\n")
	b.WriteString("if ! command -v go >/dev/null 2>&1; then\n")
	b.WriteString("  echo \"go is required to sync external repositories\" >&2\n")
	b.WriteString("  exit 127\n")
	b.WriteString("fi\n\n")
	b.WriteString("ensure_git_repo() {\n")
	b.WriteString("  local repo_dir=\"$1\"\n")
	b.WriteString("  if ! git -C \"${repo_dir}\" rev-parse --is-inside-work-tree >/dev/null 2>&1; then\n")
	b.WriteString("    git -C \"${repo_dir}\" init -b main\n")
	b.WriteString("  fi\n")
	b.WriteString("  git -C \"${repo_dir}\" add -N .\n")
	b.WriteString("}\n\n")
	b.WriteString("prepare_remote_go_module() {\n")
	b.WriteString("  local repo_dir=\"$1\"\n")
	b.WriteString("  if [[ ! -f \"${repo_dir}/go.mod\" ]]; then\n")
	b.WriteString("    return 0\n")
	b.WriteString("  fi\n")
	b.WriteString("  local git_key=\"url.ssh://git@github.com/.insteadOf\"\n")
	b.WriteString("  if [[ -n \"${GOPACT_GITHUB_TOKEN:-}\" ]]; then\n")
	b.WriteString("    git_key=\"url.https://x-access-token:${GOPACT_GITHUB_TOKEN}@github.com/.insteadOf\"\n")
	b.WriteString("  fi\n")
	b.WriteString("  echo \"    GOWORK=off go mod tidy\"\n")
	b.WriteString("  (\n")
	b.WriteString("    cd \"${repo_dir}\"\n")
	b.WriteString("    env \\\n")
	b.WriteString("      GOWORK=off \\\n")
	b.WriteString("      GOPRIVATE=\"${GOPRIVATE:-github.com/gopact-ai/*}\" \\\n")
	b.WriteString("      GONOSUMDB=\"${GONOSUMDB:-github.com/gopact-ai/*}\" \\\n")
	b.WriteString("      GONOPROXY=\"${GONOPROXY:-github.com/gopact-ai/*}\" \\\n")
	b.WriteString("      GIT_CONFIG_COUNT=1 \\\n")
	b.WriteString("      GIT_CONFIG_KEY_0=\"${git_key}\" \\\n")
	b.WriteString("      GIT_CONFIG_VALUE_0=\"https://github.com/\" \\\n")
	b.WriteString("      go mod tidy\n")
	b.WriteString("  )\n")
	b.WriteString("  git -C \"${repo_dir}\" add -N .\n")
	b.WriteString("}\n\n")
	b.WriteString("run_ci_command() {\n")
	b.WriteString("  local repo_dir=\"$1\"\n")
	b.WriteString("  local command=\"$2\"\n")
	b.WriteString("  echo \"    ${command}\"\n")
	b.WriteString("  (cd \"${repo_dir}\" && bash -lc \"${command}\")\n")
	b.WriteString("}\n\n")
	b.WriteString("sync_repo() {\n")
	b.WriteString("  local name=\"$1\"\n")
	b.WriteString("  local directory=\"$2\"\n")
	b.WriteString("  local visibility=\"$3\"\n")
	b.WriteString("  shift 3\n")
	fmt.Fprintf(&b, "  local organization=%s\n", shellQuote(plan.Organization))
	b.WriteString("  local repo=\"${organization}/${name}\"\n")
	b.WriteString("  local repo_dir=\"${generated_root}/${directory}\"\n")
	b.WriteString("  local visibility_flag=\"--private\"\n")
	b.WriteString("  if [[ \"${visibility}\" == \"public\" ]]; then\n")
	b.WriteString("    visibility_flag=\"--public\"\n")
	b.WriteString("  fi\n\n")
	b.WriteString("  echo \"==> ${repo}\"\n")
	b.WriteString("  if [[ ! -d \"${repo_dir}\" ]]; then\n")
	b.WriteString("    echo \"missing generated repository: ${repo_dir}\" >&2\n")
	b.WriteString("    exit 1\n")
	b.WriteString("  fi\n")
	b.WriteString("  ensure_git_repo \"${repo_dir}\"\n")
	b.WriteString("  prepare_remote_go_module \"${repo_dir}\"\n")
	b.WriteString("  for command in \"$@\"; do\n")
	b.WriteString("    run_ci_command \"${repo_dir}\" \"${command}\"\n")
	b.WriteString("  done\n")
	b.WriteString("  git -C \"${repo_dir}\" add .\n")
	b.WriteString("  if ! git -C \"${repo_dir}\" diff --cached --quiet; then\n")
	b.WriteString("    git -C \"${repo_dir}\" commit -m \"chore: bootstrap gopact extension scaffold\"\n")
	b.WriteString("  fi\n")
	b.WriteString("  if gh repo view \"${repo}\" >/dev/null 2>&1; then\n")
	b.WriteString("    if git -C \"${repo_dir}\" remote get-url origin >/dev/null 2>&1; then\n")
	b.WriteString("      git -C \"${repo_dir}\" remote set-url origin \"git@github.com:${repo}.git\"\n")
	b.WriteString("    else\n")
	b.WriteString("      git -C \"${repo_dir}\" remote add origin \"git@github.com:${repo}.git\"\n")
	b.WriteString("    fi\n")
	b.WriteString("    git -C \"${repo_dir}\" push -u origin HEAD:main\n")
	b.WriteString("  else\n")
	b.WriteString("    gh repo create \"${repo}\" \"${visibility_flag}\" --source \"${repo_dir}\" --remote origin --push\n")
	b.WriteString("  fi\n")
	b.WriteString("}\n\n")

	for _, repo := range plan.Repositories {
		fmt.Fprintf(&b, "sync_repo %s %s %s", shellQuote(repo.Name), shellQuote(repo.Directory), shellQuote(repo.Visibility))
		for _, command := range repo.VerifyCommands {
			fmt.Fprintf(&b, " %s", shellQuote(command))
		}
		b.WriteByte('\n')
	}
	return File{Path: "sync-repos.sh", Body: b.String()}
}

func renderSecretScriptFile(plan SyncPlan) File {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("if ! command -v gh >/dev/null 2>&1; then\n")
	b.WriteString("  echo \"gh CLI is required to configure external repository secrets\" >&2\n")
	b.WriteString("  exit 127\n")
	b.WriteString("fi\n\n")
	b.WriteString("if [[ -z \"${GOPACT_GITHUB_TOKEN:-}\" ]]; then\n")
	b.WriteString("  echo \"GOPACT_GITHUB_TOKEN must contain a GitHub token with read access to github.com/gopact-ai/gopact\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n\n")
	b.WriteString("set_secret() {\n")
	b.WriteString("  local repo=\"$1\"\n")
	b.WriteString("  echo \"==> ${repo}\"\n")
	b.WriteString("  gh secret set GOPACT_GITHUB_TOKEN -R \"${repo}\" --body \"${GOPACT_GITHUB_TOKEN}\"\n")
	b.WriteString("}\n\n")
	for _, repo := range plan.Repositories {
		fmt.Fprintf(&b, "set_secret %s\n", shellQuote(plan.Organization+"/"+repo.Name))
	}
	return File{Path: "sync-secrets.sh", Body: b.String()}
}

func renderRerunScriptFile(plan SyncPlan) File {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("if ! command -v gh >/dev/null 2>&1; then\n")
	b.WriteString("  echo \"gh CLI is required to rerun external repository CI\" >&2\n")
	b.WriteString("  exit 127\n")
	b.WriteString("fi\n\n")
	b.WriteString("require_secret() {\n")
	b.WriteString("  local repo=\"$1\"\n")
	b.WriteString("  local found\n")
	b.WriteString("  found=\"$(gh secret list -R \"${repo}\" --json name --jq '.[] | select(.name == \"GOPACT_GITHUB_TOKEN\") | .name')\"\n")
	b.WriteString("  if [[ \"${found}\" != \"GOPACT_GITHUB_TOKEN\" ]]; then\n")
	b.WriteString("    echo \"${repo} is missing GOPACT_GITHUB_TOKEN; run sync-secrets.sh before rerunning CI\" >&2\n")
	b.WriteString("    exit 1\n")
	b.WriteString("  fi\n")
	b.WriteString("}\n\n")
	b.WriteString("latest_ci_run_id() {\n")
	b.WriteString("  local repo=\"$1\"\n")
	b.WriteString("  local workflow=\"$2\"\n")
	b.WriteString("  local branch=\"$3\"\n")
	b.WriteString("  gh run list -R \"${repo}\" --workflow \"${workflow}\" --branch \"${branch}\" --limit 1 --json databaseId --jq '.[0].databaseId // \"\"'\n")
	b.WriteString("}\n\n")
	b.WriteString("rerun_ci() {\n")
	b.WriteString("  local repo=\"$1\"\n")
	b.WriteString("  local workflow=\"$2\"\n")
	b.WriteString("  local branch=\"$3\"\n")
	b.WriteString("  local latest_id\n\n")
	b.WriteString("  echo \"==> ${repo}\"\n")
	b.WriteString("  require_secret \"${repo}\"\n")
	b.WriteString("  latest_id=\"$(latest_ci_run_id \"${repo}\" \"${workflow}\" \"${branch}\")\"\n")
	b.WriteString("  if [[ -n \"${latest_id}\" ]]; then\n")
	b.WriteString("    gh run rerun -R \"${repo}\" \"${latest_id}\"\n")
	b.WriteString("    if [[ \"${WATCH:-0}\" == \"1\" ]]; then\n")
	b.WriteString("      gh run watch -R \"${repo}\" \"${latest_id}\" --exit-status\n")
	b.WriteString("    fi\n")
	b.WriteString("    return 0\n")
	b.WriteString("  fi\n\n")
	b.WriteString("  gh workflow run \"${workflow}\" -R \"${repo}\" --ref \"${branch}\"\n")
	b.WriteString("}\n\n")
	b.WriteString("# Check every required secret before triggering any CI run.\n")
	for _, repo := range plan.Repositories {
		fmt.Fprintf(&b, "require_secret %s\n", shellQuote(plan.Organization+"/"+repo.Name))
	}
	b.WriteByte('\n')
	for _, repo := range plan.Repositories {
		fmt.Fprintf(&b, "rerun_ci %s 'ci' 'main'\n", shellQuote(plan.Organization+"/"+repo.Name))
	}
	return File{Path: "rerun-ci.sh", Body: b.String()}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
