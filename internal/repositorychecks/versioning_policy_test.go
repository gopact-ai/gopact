package repositorychecks

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersioningPolicyDocumentedAndIndexed(t *testing.T) {
	policyPath := filepath.Join("doc", "design", "versioning-policy.md")
	policy := readTextFile(t, policyPath)

	for _, phrase := range []string{
		"# gopact Versioning Policy",
		"## Module Version",
		"## Stability States",
		"## Release Gates",
		"## External Extensions",
		"## Schema Versions",
		"semver",
		"`v1`",
		"`major`",
		"`minor`",
		"`patch`",
		"public-api-boundary.json",
		"core-ci-gates.json",
		"extension-conformance.json",
		"external-repositories.json",
		"RunExport",
		"StepExport",
		"CheckpointRecord",
	} {
		if !strings.Contains(policy, phrase) {
			t.Fatalf("versioning policy missing required phrase %q", phrase)
		}
	}

	for _, path := range []string{
		"README.md",
		filepath.Join("doc", "design", "index.md"),
		filepath.Join("doc", "design", "development-plan.md"),
		filepath.Join("doc", "design", "deprecation-policy.md"),
		filepath.Join("doc", "design", "migration-guide.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "versioning-policy.md") {
			t.Fatalf("%s does not reference versioning-policy.md", path)
		}
	}
}

func TestAgentScaffoldFallbackUsesLatestReleasedCoreTag(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command(git, "tag", "--list", "v0.0.*", "--sort=v:refname")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list release tags: %v\n%s", err, out)
	}
	tags := strings.Fields(string(out))
	if len(tags) == 0 {
		t.Fatal("no v0.0.x release tags found")
	}
	latest := tags[len(tags)-1]

	mainGo := readTextFile(t, filepath.Join("cmd", "gopact", "main.go"))
	want := `fallbackSDKVersion = "` + latest + `"`
	if !strings.Contains(mainGo, want) {
		t.Fatalf("gopact agent init fallback SDK version must use latest released core tag %s", latest)
	}
}
