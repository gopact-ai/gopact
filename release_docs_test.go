package gopact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseReadinessDocsAreIndexed(t *testing.T) {
	docs := []struct {
		path     string
		sections []string
	}{
		{
			path: filepath.Join("docs", "design", "migration-guide.md"),
			sections: []string{
				"# gopact Migration Guide",
				"## Compatibility Promise",
				"## API Changes",
				"## Adapter Split",
				"## Checkpoint and Resume",
				"## Verification",
			},
		},
		{
			path: filepath.Join("docs", "design", "template-guide.md"),
			sections: []string{
				"# gopact Template Guide",
				"## Template Boundary",
				"## Step Export and Resume",
				"## Events and Verification",
				"## Memory and Side Effects",
				"## Conformance",
			},
		},
	}

	readme := readReleaseDoc(t, "README.md")
	index := readReleaseDoc(t, filepath.Join("docs", "design", "index.md"))
	plan := readReleaseDoc(t, filepath.Join("docs", "design", "development-plan.md"))

	for _, doc := range docs {
		body := readReleaseDoc(t, doc.path)
		for _, section := range doc.sections {
			if !strings.Contains(body, section) {
				t.Fatalf("%s missing section %q", doc.path, section)
			}
		}

		name := filepath.Base(doc.path)
		for _, indexed := range []struct {
			path string
			body string
		}{
			{path: "README.md", body: readme},
			{path: filepath.Join("docs", "design", "index.md"), body: index},
			{path: filepath.Join("docs", "design", "development-plan.md"), body: plan},
		} {
			if !strings.Contains(indexed.body, name) {
				t.Fatalf("%s does not index %s", indexed.path, name)
			}
		}
	}
}

func TestDevAgentProcessRecordsAreDocumented(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "templates.md"),
		filepath.Join("docs", "design", "development-plan.md"),
		filepath.Join("docs", "design", "index.md"),
	} {
		body := readReleaseDoc(t, path)
		if !strings.Contains(body, "RecordProcessRecords") {
			t.Fatalf("%s does not document templates/devagent.RecordProcessRecords", path)
		}
		if !strings.Contains(body, "BuildWorkflowProcessRecords") {
			t.Fatalf("%s does not document templates/devagent.BuildWorkflowProcessRecords", path)
		}
		if !strings.Contains(body, "RecordWorkflowProcessRecords") {
			t.Fatalf("%s does not document templates/devagent.RecordWorkflowProcessRecords", path)
		}
		if !strings.Contains(body, "BuildReleaseBundle") {
			t.Fatalf("%s does not document templates/devagent.BuildReleaseBundle", path)
		}
		if !strings.Contains(body, "ReleaseBundle") {
			t.Fatalf("%s does not document templates/devagent.ReleaseBundle", path)
		}
		if !strings.Contains(body, "RecordReleaseBundleCheck") {
			t.Fatalf("%s does not document templates/devagent.RecordReleaseBundleCheck", path)
		}
		if !strings.Contains(body, "release_bundle") {
			t.Fatalf("%s does not document release_bundle evidence", path)
		}
	}
}

func readReleaseDoc(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
