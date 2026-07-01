package repositorychecks

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEcosystemTopologyDefinesOfficialRepositories(t *testing.T) {
	manifest := loadEcosystemTopology(t)
	if manifest.Policy.OfficialRepositoryCount != 3 {
		t.Fatalf("official_repository_count = %d, want 3", manifest.Policy.OfficialRepositoryCount)
	}
	if manifest.Policy.ExtensionLayout != "single-ext-repository-with-go-submodules" {
		t.Fatalf("extension_layout = %q", manifest.Policy.ExtensionLayout)
	}
	if manifest.Policy.ExamplesLayout != "single-examples-repository" {
		t.Fatalf("examples_layout = %q", manifest.Policy.ExamplesLayout)
	}
	if manifest.Policy.LegacyExternalScaffoldStatus != "migration-history" {
		t.Fatalf("legacy_external_scaffold_status = %q", manifest.Policy.LegacyExternalScaffoldStatus)
	}

	names := make([]string, 0, len(manifest.Repositories))
	for _, repo := range manifest.Repositories {
		if repo.Name == "" || repo.Role == "" || repo.CIMode != "mock-only" {
			t.Fatalf("invalid repository entry: %+v", repo)
		}
		names = append(names, repo.Name)
	}
	slices.Sort(names)
	want := []string{"gopact", "gopact-examples", "gopact-ext"}
	if !slices.Equal(names, want) {
		t.Fatalf("repositories = %v, want %v", names, want)
	}
}

func TestEcosystemTopologyIsDocumented(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "index.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "ecosystem-topology.json") {
			t.Fatalf("%s does not reference ecosystem-topology.json", path)
		}
	}
}

type ecosystemTopologyManifest struct {
	Version      int                           `json:"version"`
	Policy       ecosystemTopologyPolicy       `json:"policy"`
	Repositories []ecosystemTopologyRepository `json:"repositories"`
}

type ecosystemTopologyPolicy struct {
	OfficialRepositoryCount      int    `json:"official_repository_count"`
	ExtensionLayout              string `json:"extension_layout"`
	ExamplesLayout               string `json:"examples_layout"`
	LegacyExternalScaffoldStatus string `json:"legacy_external_scaffold_status"`
}

type ecosystemTopologyRepository struct {
	Name             string `json:"name"`
	Role             string `json:"role"`
	ModulePath       string `json:"module_path,omitempty"`
	ModulePathPrefix string `json:"module_path_prefix,omitempty"`
	CIMode           string `json:"ci_mode"`
	LocalIntegration string `json:"local_integration,omitempty"`
	Description      string `json:"description"`
}

func loadEcosystemTopology(t *testing.T) ecosystemTopologyManifest {
	t.Helper()

	raw := readFile(t, filepath.Join("docs", "design", "ecosystem-topology.json"))
	var manifest ecosystemTopologyManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode ecosystem topology manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("ecosystem topology version = %d, want 1", manifest.Version)
	}
	if len(manifest.Repositories) != manifest.Policy.OfficialRepositoryCount {
		t.Fatalf("repositories length = %d, want %d", len(manifest.Repositories), manifest.Policy.OfficialRepositoryCount)
	}
	return manifest
}
