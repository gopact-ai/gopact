// Package extensionscaffold renders external extension repository scaffolds.
package extensionscaffold

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultSDKVersion = "v0.0.0"

const (
	externalRepositoriesManifestPath = "doc/design/external-repositories.json"
	extensionConformanceManifestPath = "doc/design/extension-conformance.json"
	extensionScaffoldSpecPath        = "doc/design/extension-scaffold-spec.json"
	v1MigrationPlanPath              = "doc/design/v1-migration-plan.json"
)

// LoadRepositoriesFromDesign loads external repository scaffold inputs from the design manifests.
func LoadRepositoriesFromDesign(root string) ([]Repository, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	externalRepos, err := loadJSON[designExternalRepositoryManifest](root, externalRepositoriesManifestPath)
	if err != nil {
		return nil, err
	}
	conformance, err := loadJSON[designExtensionConformanceManifest](root, extensionConformanceManifestPath)
	if err != nil {
		return nil, err
	}
	scaffold, err := loadJSON[designExtensionScaffoldSpec](root, extensionScaffoldSpecPath)
	if err != nil {
		return nil, err
	}
	v1Plan, err := loadJSON[designV1MigrationPlan](root, v1MigrationPlanPath)
	if err != nil {
		return nil, err
	}
	if externalRepos.Version != 1 {
		return nil, fmt.Errorf("extensionscaffold: %s version = %d, want 1", externalRepositoriesManifestPath, externalRepos.Version)
	}
	if conformance.Version != 1 {
		return nil, fmt.Errorf("extensionscaffold: %s version = %d, want 1", extensionConformanceManifestPath, conformance.Version)
	}
	if scaffold.Version != 1 {
		return nil, fmt.Errorf("extensionscaffold: %s version = %d, want 1", extensionScaffoldSpecPath, scaffold.Version)
	}
	if v1Plan.Version != 1 {
		return nil, fmt.Errorf("extensionscaffold: %s version = %d, want 1", v1MigrationPlanPath, v1Plan.Version)
	}

	targets := make(map[string]designExtensionConformanceTarget, len(conformance.Targets))
	for _, target := range conformance.Targets {
		if strings.TrimSpace(target.Name) == "" {
			return nil, fmt.Errorf("extensionscaffold: %s contains target with empty name", extensionConformanceManifestPath)
		}
		if _, ok := targets[target.Name]; ok {
			return nil, fmt.Errorf("extensionscaffold: duplicate conformance target %q", target.Name)
		}
		targets[target.Name] = target
	}
	migrationsByTarget, err := v1MigrationsByTarget(v1Plan, targets)
	if err != nil {
		return nil, err
	}

	scaffoldRepos := make(map[string]designExtensionScaffoldRepository, len(scaffold.Repositories))
	for _, repo := range scaffold.Repositories {
		if strings.TrimSpace(repo.Name) == "" {
			return nil, fmt.Errorf("extensionscaffold: %s contains repository with empty name", extensionScaffoldSpecPath)
		}
		if _, ok := scaffoldRepos[repo.Name]; ok {
			return nil, fmt.Errorf("extensionscaffold: duplicate scaffold repository %q", repo.Name)
		}
		scaffoldRepos[repo.Name] = repo
	}

	goVersion, err := firstValue(conformance.SDKCompatibility.GoVersions, "sdk_compatibility.go_versions")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(conformance.SDKCompatibility.Module) == "" {
		return nil, fmt.Errorf("extensionscaffold: sdk_compatibility.module is required")
	}

	repos := make([]Repository, 0, len(externalRepos.Repositories))
	for _, manifestRepo := range externalRepos.Repositories {
		scaffoldRepo, ok := scaffoldRepos[manifestRepo.Name]
		if !ok {
			return nil, fmt.Errorf("extensionscaffold: repository %q missing scaffold spec", manifestRepo.Name)
		}
		if scaffoldRepo.ModulePath != manifestRepo.ModulePath {
			return nil, fmt.Errorf("extensionscaffold: repository %q scaffold module_path = %q, want %q", manifestRepo.Name, scaffoldRepo.ModulePath, manifestRepo.ModulePath)
		}
		scaffoldTargets := make(map[string]designExtensionScaffoldTarget, len(scaffoldRepo.Targets))
		for _, target := range scaffoldRepo.Targets {
			if strings.TrimSpace(target.Name) == "" {
				return nil, fmt.Errorf("extensionscaffold: repository %q has scaffold target with empty name", manifestRepo.Name)
			}
			if _, ok := scaffoldTargets[target.Name]; ok {
				return nil, fmt.Errorf("extensionscaffold: repository %q has duplicate scaffold target %q", manifestRepo.Name, target.Name)
			}
			scaffoldTargets[target.Name] = target
		}

		repo := Repository{
			Name:               manifestRepo.Name,
			Kind:               repositoryKind(manifestRepo.Route),
			ModulePath:         manifestRepo.ModulePath,
			SDKModule:          conformance.SDKCompatibility.Module,
			SDKVersion:         DefaultSDKVersion,
			GoVersion:          goVersion,
			RequiredFiles:      append([]string(nil), manifestRepo.RequiredFiles...),
			RequiredCICommands: append([]string(nil), manifestRepo.RequiredCICommands...),
		}
		for _, targetName := range manifestRepo.ExtensionTargets {
			conformanceTarget, ok := targets[targetName]
			if !ok {
				return nil, fmt.Errorf("extensionscaffold: repository %q references unknown conformance target %q", manifestRepo.Name, targetName)
			}
			scaffoldTarget, ok := scaffoldTargets[targetName]
			if !ok {
				return nil, fmt.Errorf("extensionscaffold: repository %q target %q missing scaffold target spec", manifestRepo.Name, targetName)
			}
			repo.Targets = append(repo.Targets, Target{
				Name:               targetName,
				Kind:               conformanceTarget.Kind,
				PackagePath:        scaffoldTarget.PackagePath,
				MinimalExamplePath: scaffoldTarget.MinimalExamplePath,
				SourcePaths:        append([]string(nil), conformanceTarget.SourcePaths...),
				Migrations:         migrationsForSourcePaths(conformanceTarget.SourcePaths, migrationsByTarget[targetName]),
				ConformanceSuites:  append([]string(nil), conformanceTarget.ConformanceSuites...),
				RequiredExamples:   append([]string(nil), conformanceTarget.RequiredExamples...),
			})
			for _, path := range conformanceTarget.SourcePaths {
				repo.SourcePaths = appendUnique(repo.SourcePaths, path)
			}
		}
		if err := validateRepository(repo); err != nil {
			return nil, fmt.Errorf("extensionscaffold: repository %q: %w", manifestRepo.Name, err)
		}
		repos = append(repos, repo)
	}
	return repos, nil
}

func v1MigrationsByTarget(plan designV1MigrationPlan, targets map[string]designExtensionConformanceTarget) (map[string][]Migration, error) {
	out := map[string][]Migration{}
	for _, migration := range plan.RepositoryMigrations {
		if strings.TrimSpace(migration.SourcePath) == "" {
			return nil, fmt.Errorf("extensionscaffold: v1 migration source_path is required")
		}
		if strings.TrimSpace(migration.Action) == "" {
			return nil, fmt.Errorf("extensionscaffold: v1 migration for %q action is required", migration.SourcePath)
		}
		if strings.TrimSpace(migration.V1Condition) == "" {
			return nil, fmt.Errorf("extensionscaffold: v1 migration for %q v1_condition is required", migration.SourcePath)
		}
		if strings.TrimSpace(migration.ExtensionTarget) == "" {
			continue
		}
		target, ok := targets[migration.ExtensionTarget]
		if !ok {
			return nil, fmt.Errorf("extensionscaffold: v1 migration target %q is not a conformance target", migration.ExtensionTarget)
		}
		if !containsString(target.SourcePaths, migration.SourcePath) {
			return nil, fmt.Errorf("extensionscaffold: v1 migration target %q source_path %q is not in conformance source_paths", migration.ExtensionTarget, migration.SourcePath)
		}
		out[migration.ExtensionTarget] = append(out[migration.ExtensionTarget], Migration{
			SourcePath:  migration.SourcePath,
			Action:      migration.Action,
			V1Condition: migration.V1Condition,
		})
	}
	return out, nil
}

func migrationsForSourcePaths(sourcePaths []string, migrations []Migration) []Migration {
	byPath := map[string]Migration{}
	for _, migration := range migrations {
		byPath[migration.SourcePath] = migration
	}
	out := make([]Migration, 0, len(migrations))
	for _, path := range sourcePaths {
		if migration, ok := byPath[path]; ok {
			out = append(out, migration)
		}
	}
	return out
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func loadJSON[T any](root, slashPath string) (T, error) {
	var out T
	path := filepath.Join(root, filepath.FromSlash(slashPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("extensionscaffold: read %s: %w", slashPath, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("extensionscaffold: decode %s: %w", slashPath, err)
	}
	return out, nil
}

func firstValue(values []string, field string) (string, error) {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("extensionscaffold: %s is required", field)
}

func repositoryKind(route string) string {
	kind := strings.TrimSuffix(route, "-repo")
	if kind != route && kind != "" {
		return kind
	}
	return route
}

func appendUnique(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

type designExternalRepositoryManifest struct {
	Version           int                        `json:"version"`
	Organization      string                     `json:"organization"`
	DefaultVisibility string                     `json:"default_visibility"`
	BootstrapSequence []string                   `json:"bootstrap_sequence"`
	Repositories      []designExternalRepository `json:"repositories"`
}

type designExternalRepository struct {
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

type designExtensionConformanceManifest struct {
	Version          int                                `json:"version"`
	SDKCompatibility designSDKCompatibility             `json:"sdk_compatibility"`
	Targets          []designExtensionConformanceTarget `json:"targets"`
}

type designSDKCompatibility struct {
	Module     string   `json:"module"`
	GoVersions []string `json:"go_versions"`
}

type designExtensionConformanceTarget struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	SourcePaths       []string `json:"source_paths"`
	ConformanceSuites []string `json:"conformance_suites"`
	RequiredExamples  []string `json:"required_examples"`
}

type designExtensionScaffoldSpec struct {
	Version      int                                 `json:"version"`
	Repositories []designExtensionScaffoldRepository `json:"repositories"`
}

type designExtensionScaffoldRepository struct {
	Name       string                          `json:"name"`
	ModulePath string                          `json:"module_path"`
	Targets    []designExtensionScaffoldTarget `json:"targets"`
}

type designExtensionScaffoldTarget struct {
	Name               string `json:"name"`
	PackagePath        string `json:"package_path"`
	MinimalExamplePath string `json:"minimal_example_path"`
}

type designV1MigrationPlan struct {
	Version              int                           `json:"version"`
	RepositoryMigrations []designV1RepositoryMigration `json:"repository_migrations"`
}

type designV1RepositoryMigration struct {
	SourcePath      string `json:"source_path"`
	Action          string `json:"action"`
	ExtensionTarget string `json:"extension_target"`
	V1Condition     string `json:"v1_condition"`
}
