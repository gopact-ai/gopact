package gopact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExternalRepositoryManifestCoversRoadmapRepositories(t *testing.T) {
	roadmap := loadExternalIntegrationRoadmap(t)
	conformance := loadExtensionConformanceManifest(t)
	manifest := loadExternalRepositoryManifest(t)

	if manifest.Organization != conformance.Scaffold.RepositoryOwner {
		t.Fatalf("external repositories organization = %q, want %q", manifest.Organization, conformance.Scaffold.RepositoryOwner)
	}
	if manifest.ModulePathPrefix != conformance.Scaffold.ModulePathPrefix {
		t.Fatalf("external repositories module_path_prefix = %q, want %q", manifest.ModulePathPrefix, conformance.Scaffold.ModulePathPrefix)
	}
	if manifest.DefaultVisibility != "private" {
		t.Fatalf("external repositories default_visibility = %q, want private", manifest.DefaultVisibility)
	}

	repos := map[string]externalRepository{}
	targetOwners := map[string]string{}
	conformanceTargets := map[string]bool{}
	for _, target := range conformance.Targets {
		conformanceTargets[target.Name] = true
	}

	for _, repo := range manifest.Repositories {
		if repo.Name == "" {
			t.Fatal("external repository name is empty")
		}
		if repos[repo.Name].Name != "" {
			t.Fatalf("external repository %q is duplicated", repo.Name)
		}
		if !strings.HasPrefix(repo.Name, "gopact-") {
			t.Fatalf("external repository %q must use gopact-* naming", repo.Name)
		}
		if repo.Visibility != "private" {
			t.Fatalf("external repository %q visibility = %q, want private", repo.Name, repo.Visibility)
		}
		if repo.ModulePath != manifest.ModulePathPrefix+repo.Name {
			t.Fatalf("external repository %q module_path = %q, want %q", repo.Name, repo.ModulePath, manifest.ModulePathPrefix+repo.Name)
		}
		if repo.Route == "" {
			t.Fatalf("external repository %q route is empty", repo.Name)
		}
		if !slices.Contains(roadmap.AllowedRoutes, repo.Route) {
			t.Fatalf("external repository %q route %q is not allowed by roadmap", repo.Name, repo.Route)
		}
		if repo.ScaffoldStatus != "ready-to-create" {
			t.Fatalf("external repository %q scaffold_status = %q, want ready-to-create", repo.Name, repo.ScaffoldStatus)
		}
		if !repo.HostOwnedConfig {
			t.Fatalf("external repository %q must keep config host-owned", repo.Name)
		}
		for _, file := range conformance.Scaffold.RequiredFiles {
			if !slices.Contains(repo.RequiredFiles, file) {
				t.Fatalf("external repository %q required_files missing %q", repo.Name, file)
			}
		}
		for _, command := range conformance.RequiredCICommands {
			if !slices.Contains(repo.RequiredCICommands, command) {
				t.Fatalf("external repository %q required_ci_commands missing %q", repo.Name, command)
			}
		}
		for _, target := range repo.ExtensionTargets {
			if !conformanceTargets[target] {
				t.Fatalf("external repository %q references unknown extension target %q", repo.Name, target)
			}
			if owner := targetOwners[target]; owner != "" {
				t.Fatalf("extension target %q assigned to both %q and %q", target, owner, repo.Name)
			}
			targetOwners[target] = repo.Name
		}
		repos[repo.Name] = repo
	}

	for _, entry := range roadmap.Entries {
		repo, ok := repos[entry.TargetRepo]
		if !ok {
			t.Fatalf("external repositories manifest missing roadmap target_repo %q", entry.TargetRepo)
		}
		if repo.Route != entry.Route {
			t.Fatalf("external repository %q route = %q, want %q", repo.Name, repo.Route, entry.Route)
		}
		for _, target := range entry.ExtensionTargets {
			if owner := targetOwners[target]; owner != entry.TargetRepo {
				t.Fatalf("roadmap entry %q target %q assigned to repo %q, want %q", entry.ID, target, owner, entry.TargetRepo)
			}
		}
	}

	for _, target := range conformance.Targets {
		if owner := targetOwners[target.Name]; owner == "" {
			t.Fatalf("extension target %q is not assigned to an external repository", target.Name)
		}
	}
}

func TestExternalRepositoryManifestDefinesBootstrapSequence(t *testing.T) {
	manifest := loadExternalRepositoryManifest(t)

	for _, required := range []string{
		"create-private-repos",
		"copy-scaffold-files",
		"wire-conformance-suites",
		"enable-private-ci",
		"publish-compatibility-matrix",
	} {
		if !slices.Contains(manifest.BootstrapSequence, required) {
			t.Fatalf("external repositories bootstrap_sequence missing %q", required)
		}
	}
}

func TestExtensionScaffoldSpecCoversExternalRepositories(t *testing.T) {
	repositories := loadExternalRepositoryManifest(t)
	conformance := loadExtensionConformanceManifest(t)
	spec := loadExtensionScaffoldSpec(t)

	for _, path := range []string{
		"docs/design/external-repositories.json",
		"docs/design/extension-conformance.json",
		"docs/design/extension-repository-template.md",
		"docs/design/extension-conformance-template.md",
		"docs/design/extension-ci-workflow.yml",
		"docs/design/v1-migration-plan.json",
	} {
		if !slices.Contains(spec.SourceManifests, path) {
			t.Fatalf("extension scaffold spec source_manifests missing %q", path)
		}
	}

	fileTemplates := map[string]extensionScaffoldFile{}
	for _, file := range spec.FileTemplates {
		if file.Path == "" {
			t.Fatal("extension scaffold file template path is empty")
		}
		if fileTemplates[file.Path].Path != "" {
			t.Fatalf("extension scaffold file template %q is duplicated", file.Path)
		}
		if file.TemplatePath == "" && len(file.ContentRules) == 0 {
			t.Fatalf("extension scaffold file template %q must define template_path or content_rules", file.Path)
		}
		if file.TemplatePath != "" {
			if _, err := os.Stat(filepath.Clean(file.TemplatePath)); err != nil {
				t.Fatalf("extension scaffold file template %q template_path %q: %v", file.Path, file.TemplatePath, err)
			}
		}
		fileTemplates[file.Path] = file
	}
	for _, required := range conformance.Scaffold.RequiredFiles {
		if fileTemplates[required].Path == "" {
			t.Fatalf("extension scaffold spec file_templates missing required file %q", required)
		}
	}

	repoSpecs := map[string]extensionScaffoldRepository{}
	for _, repo := range spec.Repositories {
		if repo.Name == "" {
			t.Fatal("extension scaffold repository name is empty")
		}
		if repoSpecs[repo.Name].Name != "" {
			t.Fatalf("extension scaffold repository %q is duplicated", repo.Name)
		}
		if repo.ModulePath != repositories.ModulePathPrefix+repo.Name {
			t.Fatalf("extension scaffold repository %q module_path = %q, want %q", repo.Name, repo.ModulePath, repositories.ModulePathPrefix+repo.Name)
		}
		if len(repo.Targets) == 0 {
			t.Fatalf("extension scaffold repository %q targets is empty", repo.Name)
		}
		for _, target := range repo.Targets {
			if target.Name == "" {
				t.Fatalf("extension scaffold repository %q has target with empty name", repo.Name)
			}
			if target.PackagePath == "" {
				t.Fatalf("extension scaffold repository %q target %q package_path is empty", repo.Name, target.Name)
			}
			if strings.HasPrefix(target.PackagePath, ".") || strings.Contains(target.PackagePath, "..") {
				t.Fatalf("extension scaffold repository %q target %q has unsafe package_path %q", repo.Name, target.Name, target.PackagePath)
			}
			if target.MinimalExamplePath == "" {
				t.Fatalf("extension scaffold repository %q target %q minimal_example_path is empty", repo.Name, target.Name)
			}
		}
		repoSpecs[repo.Name] = repo
	}

	for _, repo := range repositories.Repositories {
		scaffoldRepo, ok := repoSpecs[repo.Name]
		if !ok {
			t.Fatalf("extension scaffold spec missing repository %q", repo.Name)
		}
		targets := map[string]extensionScaffoldTarget{}
		for _, target := range scaffoldRepo.Targets {
			targets[target.Name] = target
		}
		for _, target := range repo.ExtensionTargets {
			if targets[target].Name == "" {
				t.Fatalf("extension scaffold spec repository %q missing target %q", repo.Name, target)
			}
		}
	}
}

func TestExtensionScaffoldSpecIsIndexed(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "development-plan.md"),
		filepath.Join("docs", "design", "versioning-policy.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "extension-scaffold-spec.json") {
			t.Fatalf("%s does not reference extension-scaffold-spec.json", path)
		}
	}
}

func TestExtensionScaffoldMaterializerIsDocumented(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "development-plan.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "internal/extensionscaffold") {
			t.Fatalf("%s does not reference internal/extensionscaffold", path)
		}
		if !strings.Contains(content, "LoadRepositoriesFromDesign") {
			t.Fatalf("%s does not reference LoadRepositoriesFromDesign", path)
		}
		if !strings.Contains(content, "v1-migration-plan.json") {
			t.Fatalf("%s does not reference v1-migration-plan.json", path)
		}
		if !strings.Contains(content, "V1 Migration Ownership") {
			t.Fatalf("%s does not reference V1 Migration Ownership", path)
		}
		if !strings.Contains(content, "WriteRepositoriesFromDesign") {
			t.Fatalf("%s does not reference WriteRepositoriesFromDesign", path)
		}
		if !strings.Contains(content, "RenderSyncPlanFromDesign") {
			t.Fatalf("%s does not reference RenderSyncPlanFromDesign", path)
		}
		if !strings.Contains(content, "cmd/gopact-extscaffold") {
			t.Fatalf("%s does not reference cmd/gopact-extscaffold", path)
		}
		if !strings.Contains(content, "go.work") {
			t.Fatalf("%s does not reference go.work", path)
		}
		if !strings.Contains(content, "sync-plan.json") {
			t.Fatalf("%s does not reference sync-plan.json", path)
		}
		if !strings.Contains(content, "-verify") {
			t.Fatalf("%s does not reference -verify", path)
		}
		if !strings.Contains(content, "-plan-json") {
			t.Fatalf("%s does not reference -plan-json", path)
		}
		if !strings.Contains(content, "-remote-status-json") {
			t.Fatalf("%s does not reference -remote-status-json", path)
		}
	}
}

type externalRepositoryManifest struct {
	Version           int                  `json:"version"`
	Organization      string               `json:"organization"`
	DefaultVisibility string               `json:"default_visibility"`
	ModulePathPrefix  string               `json:"module_path_prefix"`
	BootstrapSequence []string             `json:"bootstrap_sequence"`
	Repositories      []externalRepository `json:"repositories"`
}

type externalRepository struct {
	Name               string   `json:"name"`
	Route              string   `json:"route"`
	Visibility         string   `json:"visibility"`
	ModulePath         string   `json:"module_path"`
	ScaffoldStatus     string   `json:"scaffold_status"`
	HostOwnedConfig    bool     `json:"host_owned_config"`
	ExtensionTargets   []string `json:"extension_targets"`
	RequiredFiles      []string `json:"required_files"`
	RequiredCICommands []string `json:"required_ci_commands"`
}

type extensionScaffoldSpec struct {
	Version         int                           `json:"version"`
	SourceManifests []string                      `json:"source_manifests"`
	FileTemplates   []extensionScaffoldFile       `json:"file_templates"`
	Repositories    []extensionScaffoldRepository `json:"repositories"`
}

type extensionScaffoldFile struct {
	Path         string   `json:"path"`
	TemplatePath string   `json:"template_path"`
	ContentRules []string `json:"content_rules"`
}

type extensionScaffoldRepository struct {
	Name       string                    `json:"name"`
	ModulePath string                    `json:"module_path"`
	Targets    []extensionScaffoldTarget `json:"targets"`
}

type extensionScaffoldTarget struct {
	Name               string `json:"name"`
	PackagePath        string `json:"package_path"`
	MinimalExamplePath string `json:"minimal_example_path"`
}

func loadExternalRepositoryManifest(t *testing.T) externalRepositoryManifest {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "design", "external-repositories.json"))
	if err != nil {
		t.Fatalf("read external repositories manifest: %v", err)
	}
	var manifest externalRepositoryManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode external repositories manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("external repositories manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}

func loadExtensionScaffoldSpec(t *testing.T) extensionScaffoldSpec {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "design", "extension-scaffold-spec.json"))
	if err != nil {
		t.Fatalf("read extension scaffold spec: %v", err)
	}
	var spec extensionScaffoldSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode extension scaffold spec: %v", err)
	}
	if spec.Version != 1 {
		t.Fatalf("extension scaffold spec version = %d, want 1", spec.Version)
	}
	return spec
}
