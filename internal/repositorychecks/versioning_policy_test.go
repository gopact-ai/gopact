package gopact

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVersioningPolicyDocumentedAndIndexed(t *testing.T) {
	policyPath := filepath.Join("docs", "design", "versioning-policy.md")
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
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "development-plan.md"),
		filepath.Join("docs", "design", "deprecation-policy.md"),
		filepath.Join("docs", "design", "migration-guide.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "versioning-policy.md") {
			t.Fatalf("%s does not reference versioning-policy.md", path)
		}
	}
}
