package gopact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeprecationPolicyDocumentedAndIndexed(t *testing.T) {
	policy := readTextFile(t, filepath.Join("docs", "design", "deprecation-policy.md"))

	requiredPhrases := []string{
		"# gopact Public API 废弃策略",
		"## 稳定性等级",
		"## 废弃标记",
		"## 移除窗口",
		"## 兼容性审查",
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
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "api-ergonomics.md"),
		filepath.Join("docs", "design", "development-plan.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "deprecation-policy.md") {
			t.Fatalf("%s does not reference deprecation-policy.md", path)
		}
	}
}

func TestDeprecationPolicyCoversPublicAPIStabilityStates(t *testing.T) {
	policy := readTextFile(t, filepath.Join("docs", "design", "deprecation-policy.md"))
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

func readTextFile(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
