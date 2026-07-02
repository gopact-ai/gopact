package repositorychecks

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDeprecationPolicyDocumentedAndIndexed(t *testing.T) {
	policy := readTextFile(t, filepath.Join("doc", "design", "deprecation-policy.md"))

	requiredPhrases := []string{
		"# gopact Public API Deprecation Policy",
		"## Stability Levels",
		"## Deprecation Markers",
		"## Removal Windows",
		"## Compatibility Review",
		"Deprecated:",
		"public-api-boundary.json",
		"public-api-examples.json",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(policy, phrase) {
			t.Fatalf("deprecation policy missing required phrase %q", phrase)
		}
	}

	for _, path := range []string{
		"README.md",
		filepath.Join("doc", "design", "index.md"),
		filepath.Join("doc", "design", "api-ergonomics.md"),
		filepath.Join("doc", "design", "development-plan.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "deprecation-policy.md") {
			t.Fatalf("%s does not reference deprecation-policy.md", path)
		}
	}
}

func TestDeprecationPolicyCoversPublicAPIStabilityStates(t *testing.T) {
	policy := readTextFile(t, filepath.Join("doc", "design", "deprecation-policy.md"))
	manifest := loadPublicAPIBoundaryManifest(t)

	seen := map[string]bool{}
	for _, group := range manifest.Groups {
		seen[group.Stability] = true
	}
	for stability := range seen {
		if !strings.Contains(policy, "`"+stability+"`") {
			t.Fatalf("deprecation policy does not describe public API stability %q", stability)
		}
	}
}
