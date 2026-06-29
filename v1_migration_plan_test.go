package gopact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestV1MigrationPlanCoversRepositoryBoundaryMoves(t *testing.T) {
	boundary := loadRepositoryBoundaryManifest(t)
	repositories := loadExternalRepositoryManifest(t)
	plan := loadV1MigrationPlan(t)

	if plan.Scope != "v1-core-boundary" {
		t.Fatalf("v1 migration plan scope = %q, want v1-core-boundary", plan.Scope)
	}
	for _, path := range []string{
		"docs/design/repository-boundary.json",
		"docs/design/extension-conformance.json",
		"docs/design/external-repositories.json",
		"docs/design/public-api-boundary.json",
		"docs/design/migration-guide.md",
		"docs/design/versioning-policy.md",
	} {
		if !slices.Contains(plan.SourceManifests, path) {
			t.Fatalf("v1 migration plan source_manifests missing %q", path)
		}
	}
	for _, gate := range []string{
		"core-ci-gates",
		"external-repository-readiness",
		"extension-conformance",
		"public-api-examples",
		"migration-guide",
	} {
		if !slices.Contains(plan.ReleaseGateConditions, gate) {
			t.Fatalf("v1 migration plan release_gate_conditions missing %q", gate)
		}
	}

	owners := map[string]externalRepository{}
	for _, repo := range repositories.Repositories {
		for _, target := range repo.ExtensionTargets {
			owners[target] = repo
		}
	}
	requiredMigrations := map[string]repositoryBoundaryEntry{}
	for _, entry := range boundary.Entries {
		if entry.Disposition != "move-to-adapter-repo" &&
			entry.Disposition != "move-to-template-repo" &&
			entry.Disposition != "remove-before-v1" {
			continue
		}
		requiredMigrations[entry.Path] = entry
	}

	migrations := map[string]v1RepositoryMigration{}
	for _, migration := range plan.RepositoryMigrations {
		if migration.SourcePath == "" {
			t.Fatal("v1 repository migration source_path is empty")
		}
		if migrations[migration.SourcePath].SourcePath != "" {
			t.Fatalf("v1 repository migration %q is duplicated", migration.SourcePath)
		}
		if requiredMigrations[migration.SourcePath].Path == "" {
			t.Fatalf("v1 repository migration %q is not a move/remove repository boundary path", migration.SourcePath)
		}
		if strings.TrimSpace(migration.V1Condition) == "" {
			t.Fatalf("v1 repository migration %q v1_condition is empty", migration.SourcePath)
		}
		for _, required := range []string{"extension-conformance", "external-repository-readiness", "core-ci-gates"} {
			if !slices.Contains(migration.Verification, required) {
				t.Fatalf("v1 repository migration %q verification missing %q", migration.SourcePath, required)
			}
		}
		migrations[migration.SourcePath] = migration
	}

	for _, entry := range boundary.Entries {
		if entry.Disposition != "move-to-adapter-repo" &&
			entry.Disposition != "move-to-template-repo" &&
			entry.Disposition != "remove-before-v1" {
			continue
		}
		migration, ok := migrations[entry.Path]
		if !ok {
			t.Fatalf("v1 migration plan missing repository boundary path %q", entry.Path)
		}
		if migration.Action != entry.Disposition {
			t.Fatalf("v1 migration plan path %q action = %q, want %q", entry.Path, migration.Action, entry.Disposition)
		}
		if entry.Disposition == "remove-before-v1" {
			if migration.ExtensionTarget != "" || migration.ExternalRepository != "" || migration.ExternalModule != "" {
				t.Fatalf("v1 migration plan remove path %q must not declare external target/repository/module", entry.Path)
			}
			continue
		}
		if migration.ExtensionTarget != entry.Target {
			t.Fatalf("v1 migration plan path %q extension_target = %q, want %q", entry.Path, migration.ExtensionTarget, entry.Target)
		}
		owner := owners[entry.Target]
		if owner.Name == "" {
			t.Fatalf("repository boundary target %q has no external repository owner", entry.Target)
		}
		if migration.ExternalRepository != owner.Name {
			t.Fatalf("v1 migration plan path %q external_repository = %q, want %q", entry.Path, migration.ExternalRepository, owner.Name)
		}
		if migration.ExternalModule != owner.ModulePath {
			t.Fatalf("v1 migration plan path %q external_module = %q, want %q", entry.Path, migration.ExternalModule, owner.ModulePath)
		}
	}
}

func TestV1MigrationPlanDeclaresReleaseGateChecks(t *testing.T) {
	plan := loadV1MigrationPlan(t)
	coreGates := loadCoreCIGatesManifest(t)
	knownEvidenceTypes := map[string]bool{
		"ci_gate":                       true,
		"command":                       true,
		"external_repository_readiness": true,
		"file_snapshot":                 true,
	}

	checks := map[string]v1ReleaseGateCheck{}
	for _, check := range plan.ReleaseGateChecks {
		if check.ID == "" {
			t.Fatal("v1 release gate check id is empty")
		}
		if checks[check.ID].ID != "" {
			t.Fatalf("v1 release gate check %q is duplicated", check.ID)
		}
		if !slices.Contains(plan.ReleaseGateConditions, check.ID) {
			t.Fatalf("v1 release gate check %q is not listed in release_gate_conditions", check.ID)
		}
		if check.RequiredStatus != "passed" {
			t.Fatalf("v1 release gate check %q required_status = %q, want passed", check.ID, check.RequiredStatus)
		}
		if check.BlockingStatus != "failed" {
			t.Fatalf("v1 release gate check %q blocking_status = %q, want failed", check.ID, check.BlockingStatus)
		}
		if len(check.EvidenceTypes) == 0 {
			t.Fatalf("v1 release gate check %q evidence_types is empty", check.ID)
		}
		for _, evidenceType := range check.EvidenceTypes {
			if !knownEvidenceTypes[evidenceType] {
				t.Fatalf("v1 release gate check %q evidence type %q is not known to release evidence", check.ID, evidenceType)
			}
		}
		if len(check.SourceManifests) == 0 {
			t.Fatalf("v1 release gate check %q source_manifests is empty", check.ID)
		}
		for _, path := range check.SourceManifests {
			if _, err := os.Stat(filepath.Clean(path)); err != nil {
				t.Fatalf("v1 release gate check %q source_manifest %q: %v", check.ID, path, err)
			}
		}
		if len(check.RequiredCheckIDs) == 0 {
			t.Fatalf("v1 release gate check %q required_check_ids is empty", check.ID)
		}
		checkIDSeen := map[string]bool{}
		for _, id := range check.RequiredCheckIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				t.Fatalf("v1 release gate check %q required_check_ids contains empty id", check.ID)
			}
			if checkIDSeen[id] {
				t.Fatalf("v1 release gate check %q required_check_ids duplicates %q", check.ID, id)
			}
			checkIDSeen[id] = true
		}
		if slices.Contains(check.EvidenceTypes, "command") && !hasRequiredCheckIDPrefix(check, "command:") {
			t.Fatalf("v1 release gate check %q declares command evidence without command:* required_check_ids", check.ID)
		}
		if slices.Contains(check.EvidenceTypes, "external_repository_readiness") &&
			!slices.Contains(check.RequiredCheckIDs, "external-repositories:gopact-ai") {
			t.Fatalf("v1 release gate check %q missing external-repositories:gopact-ai required_check_ids entry", check.ID)
		}
		if slices.Contains(check.EvidenceTypes, "file_snapshot") {
			for _, path := range check.SourceManifests {
				id := "file-snapshot:" + path
				if !slices.Contains(check.RequiredCheckIDs, id) {
					t.Fatalf("v1 release gate check %q missing %q required_check_ids entry", check.ID, id)
				}
			}
		}
		if strings.TrimSpace(check.BlockerSummary) == "" {
			t.Fatalf("v1 release gate check %q blocker_summary is empty", check.ID)
		}
		checks[check.ID] = check
	}
	coreGateCheck := checks["core-ci-gates"]
	if coreGateCheck.ID == "" {
		t.Fatal("v1 release gate checks missing core-ci-gates")
	}
	if !slices.Contains(coreGateCheck.RequiredCheckIDs, "ci-gates") {
		t.Fatal("core-ci-gates release check missing ci-gates required_check_ids entry")
	}
	gotCIGates := slices.Clone(coreGateCheck.RequiredCIGates)
	wantCIGates := slices.Clone(coreGates.RequiredGates)
	slices.Sort(gotCIGates)
	slices.Sort(wantCIGates)
	if !slices.Equal(gotCIGates, wantCIGates) {
		t.Fatalf("core-ci-gates release check required_ci_gates = %v, want %v", gotCIGates, wantCIGates)
	}
	for _, command := range coreGates.RequiredCommands {
		id := "command:" + command
		if !slices.Contains(coreGateCheck.RequiredCheckIDs, id) {
			t.Fatalf("core-ci-gates release check missing %q required_check_ids entry", id)
		}
	}

	externalGateCheck := checks["external-repository-readiness"]
	if externalGateCheck.ID == "" {
		t.Fatal("v1 release gate checks missing external-repository-readiness")
	}
	for _, evidenceType := range []string{"external_repository_readiness", "ci_gate"} {
		if !slices.Contains(externalGateCheck.EvidenceTypes, evidenceType) {
			t.Fatalf("external-repository-readiness evidence_types = %v, want %q", externalGateCheck.EvidenceTypes, evidenceType)
		}
	}
	for _, id := range []string{"external-repositories:gopact-ai", "external-ci:gopact-ai"} {
		if !slices.Contains(externalGateCheck.RequiredCheckIDs, id) {
			t.Fatalf("external-repository-readiness required_check_ids missing %q", id)
		}
	}
	gotExternalCIGates := slices.Clone(externalGateCheck.RequiredCIGates)
	wantExternalCIGates := []string{"unit", "vet", "whitespace"}
	slices.Sort(gotExternalCIGates)
	slices.Sort(wantExternalCIGates)
	if !slices.Equal(gotExternalCIGates, wantExternalCIGates) {
		t.Fatalf("external-repository-readiness required_ci_gates = %v, want %v", gotExternalCIGates, wantExternalCIGates)
	}

	for _, condition := range plan.ReleaseGateConditions {
		if checks[condition].ID == "" {
			t.Fatalf("v1 release_gate_conditions entry %q has no release_gate_checks entry", condition)
		}
	}
	for _, migration := range plan.RepositoryMigrations {
		for _, verification := range migration.Verification {
			if checks[verification].ID == "" {
				t.Fatalf("v1 repository migration %q verification %q has no release gate check", migration.SourcePath, verification)
			}
		}
	}
	for _, transition := range plan.PublicAPITransitions {
		key := publicAPITransitionKey(transition.Category, transition.SourceFile)
		for _, verification := range transition.Verification {
			if checks[verification].ID == "" {
				t.Fatalf("v1 public api transition %q verification %q has no release gate check", key, verification)
			}
		}
	}
}

func TestV1MigrationPlanRecordsExternalizedSources(t *testing.T) {
	plan := loadV1MigrationPlan(t)

	migrations := map[string]v1RepositoryMigration{}
	for _, candidate := range plan.RepositoryMigrations {
		migrations[candidate.SourcePath] = candidate
	}

	for _, sourcePath := range []string{
		"adapters/model/openaicompatible",
		"adapters/devagent/gitdiff",
		"adapters/devagent/cireview",
		"adapters/devagent/channelreview",
		"adapters/devagent/modelreview",
		"adapters/checkpoint/sqlstore",
		"adapters/checkpoint/redisstore",
		"adapters/checkpoint/gcsstore",
		"adapters/checkpoint/r2store",
		"adapters/checkpoint/s3store",
	} {
		migration := migrations[sourcePath]
		if migration.SourcePath == "" {
			t.Fatalf("v1 migration plan missing %s", sourcePath)
		}
		if !migration.CoreSourceRemoved {
			t.Fatalf("%s migration core_source_removed = false, want true", sourcePath)
		}
		if strings.TrimSpace(migration.ExternalSourceRef) == "" {
			t.Fatalf("%s migration external_source_ref is empty", sourcePath)
		}
		if _, err := os.Stat(filepath.FromSlash(migration.SourcePath)); !os.IsNotExist(err) {
			t.Fatalf("%s source path still exists in core repo: %v", sourcePath, err)
		}
	}
}

func TestV1MigrationPlanCoversTransitionalPublicAPI(t *testing.T) {
	boundary := loadPublicAPIBoundaryManifest(t)
	plan := loadV1MigrationPlan(t)

	requiredTransitions := map[string]publicAPIBoundaryGroup{}
	for _, group := range boundary.Groups {
		if group.Stability != "transitional" {
			continue
		}
		requiredTransitions[publicAPITransitionKey(group.Category, group.SourceFile)] = group
	}
	transitions := map[string]v1PublicAPITransition{}
	for _, transition := range plan.PublicAPITransitions {
		key := publicAPITransitionKey(transition.Category, transition.SourceFile)
		if transition.Category == "" {
			t.Fatal("v1 public api transition category is empty")
		}
		if transition.SourceFile == "" {
			t.Fatalf("v1 public api transition for category %q source_file is empty", transition.Category)
		}
		if transitions[key].SourceFile != "" {
			t.Fatalf("v1 public api transition %q is duplicated", key)
		}
		if requiredTransitions[key].SourceFile == "" {
			t.Fatalf("v1 public api transition %q is not a transitional public api group", key)
		}
		if !v1PublicAPIActionAllowed(transition.Action) {
			t.Fatalf("v1 public api transition %q has invalid action %q", key, transition.Action)
		}
		if !v1PublicAPITargetStateAllowed(transition.TargetState) {
			t.Fatalf("v1 public api transition %q has invalid target_state %q", key, transition.TargetState)
		}
		if strings.TrimSpace(transition.V1Condition) == "" {
			t.Fatalf("v1 public api transition %q v1_condition is empty", key)
		}
		if len(transition.Symbols) == 0 {
			t.Fatalf("v1 public api transition %q symbols is empty", key)
		}
		for _, required := range []string{"public-api-boundary", "public-api-examples", "migration-guide", "core-ci-gates"} {
			if !slices.Contains(transition.Verification, required) {
				t.Fatalf("v1 public api transition %q verification missing %q", key, required)
			}
		}
		transitions[key] = transition
	}

	for _, group := range boundary.Groups {
		if group.Stability != "transitional" {
			continue
		}
		key := publicAPITransitionKey(group.Category, group.SourceFile)
		transition, ok := transitions[key]
		if !ok {
			t.Fatalf("v1 migration plan missing transitional public api group %q", key)
		}
		wantSymbols := publicAPIGroupSymbolNames(group)
		gotSymbols := slices.Clone(transition.Symbols)
		slices.Sort(gotSymbols)
		if !slices.Equal(gotSymbols, wantSymbols) {
			t.Fatalf("v1 public api transition %q symbols = %v, want %v", key, gotSymbols, wantSymbols)
		}
	}
}

func TestV1MigrationPlanIsIndexed(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "development-plan.md"),
		filepath.Join("docs", "design", "migration-guide.md"),
		filepath.Join("docs", "design", "versioning-policy.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "v1-migration-plan.json") {
			t.Fatalf("%s does not reference v1-migration-plan.json", path)
		}
		if !strings.Contains(content, "release_gate_checks") {
			t.Fatalf("%s does not document v1 release_gate_checks", path)
		}
		if !strings.Contains(content, "required_check_ids") {
			t.Fatalf("%s does not document v1 release gate required_check_ids", path)
		}
	}
}

type v1MigrationPlan struct {
	Version               int                     `json:"version"`
	Scope                 string                  `json:"scope"`
	SourceManifests       []string                `json:"source_manifests"`
	ReleaseGateConditions []string                `json:"release_gate_conditions"`
	ReleaseGateChecks     []v1ReleaseGateCheck    `json:"release_gate_checks"`
	RepositoryMigrations  []v1RepositoryMigration `json:"repository_migrations"`
	PublicAPITransitions  []v1PublicAPITransition `json:"public_api_transitions"`
}

type v1ReleaseGateCheck struct {
	ID               string   `json:"id"`
	EvidenceTypes    []string `json:"evidence_types"`
	SourceManifests  []string `json:"source_manifests"`
	RequiredCheckIDs []string `json:"required_check_ids"`
	RequiredCIGates  []string `json:"required_ci_gates,omitempty"`
	RequiredStatus   string   `json:"required_status"`
	BlockingStatus   string   `json:"blocking_status"`
	BlockerSummary   string   `json:"blocker_summary"`
}

type v1RepositoryMigration struct {
	SourcePath         string   `json:"source_path"`
	Action             string   `json:"action"`
	ExtensionTarget    string   `json:"extension_target,omitempty"`
	ExternalRepository string   `json:"external_repository,omitempty"`
	ExternalModule     string   `json:"external_module,omitempty"`
	ExternalSourceRef  string   `json:"external_source_ref,omitempty"`
	CoreSourceRemoved  bool     `json:"core_source_removed,omitempty"`
	V1Condition        string   `json:"v1_condition"`
	Verification       []string `json:"verification"`
}

type v1PublicAPITransition struct {
	Category     string   `json:"category"`
	SourceFile   string   `json:"source_file"`
	Symbols      []string `json:"symbols"`
	Action       string   `json:"action"`
	TargetState  string   `json:"target_state"`
	V1Condition  string   `json:"v1_condition"`
	Verification []string `json:"verification"`
}

func loadV1MigrationPlan(t *testing.T) v1MigrationPlan {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "design", "v1-migration-plan.json"))
	if err != nil {
		t.Fatalf("read v1 migration plan: %v", err)
	}
	var plan v1MigrationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode v1 migration plan: %v", err)
	}
	if plan.Version != 1 {
		t.Fatalf("v1 migration plan version = %d, want 1", plan.Version)
	}
	return plan
}

func publicAPIGroupSymbolNames(group publicAPIBoundaryGroup) []string {
	names := make([]string, 0, len(group.Symbols))
	for _, symbol := range group.Symbols {
		names = append(names, symbol.Name)
	}
	slices.Sort(names)
	return names
}

func publicAPITransitionKey(category, sourceFile string) string {
	return category + ":" + sourceFile
}

func hasRequiredCheckIDPrefix(check v1ReleaseGateCheck, prefix string) bool {
	for _, id := range check.RequiredCheckIDs {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

func v1PublicAPIActionAllowed(action string) bool {
	switch action {
	case "stabilize-core", "keep-reference", "move-to-adapter-repo", "move-to-template-repo", "remove-before-v1":
		return true
	default:
		return false
	}
}

func v1PublicAPITargetStateAllowed(state string) bool {
	switch state {
	case "stable", "experimental", "externalized", "removed":
		return true
	default:
		return false
	}
}
