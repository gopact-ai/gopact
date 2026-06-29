package repositorychecks

import (
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

func TestOpenSourceGovernanceDocsArePresent(t *testing.T) {
	docs := []struct {
		path     string
		sections []string
	}{
		{
			path: "LICENSE",
			sections: []string{
				"MIT License",
				"Permission is hereby granted",
			},
		},
		{
			path: "CONTRIBUTING.md",
			sections: []string{
				"# Contributing to gopact",
				"## Development Setup",
				"## Verification",
				"## Pull Request Checklist",
			},
		},
		{
			path: "SECURITY.md",
			sections: []string{
				"# Security Policy",
				"## Supported Versions",
				"## Reporting a Vulnerability",
			},
		},
		{
			path: "CHANGELOG.md",
			sections: []string{
				"# Changelog",
				"## Unreleased",
			},
		},
	}

	readme := readReleaseDoc(t, "README.md")
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
			{path: filepath.Join("docs", "design", "development-plan.md"), body: plan},
		} {
			if !strings.Contains(indexed.body, name) {
				t.Fatalf("%s does not index %s", indexed.path, name)
			}
		}
	}

	if !strings.Contains(plan, "LICENSE") {
		t.Fatal("development plan does not track LICENSE release requirement")
	}
}

func TestReadmeHasPublicSDKEntryPath(t *testing.T) {
	body := readReleaseDoc(t, "README.md")
	requirements := []string{
		"## 安装",
		"go get github.com/gopact-ai/gopact",
		"## 快速开始",
		"Example_graphRun",
		"go test -run Example_graphRun",
		"## 核心概念",
		"## 当前稳定性",
		"## 文档地图",
		"docs/design/index.md",
		"docs/design/public-api-examples.json",
		"## 贡献与安全",
		"CONTRIBUTING.md",
		"SECURITY.md",
		"CHANGELOG.md",
	}

	for _, requirement := range requirements {
		if !strings.Contains(body, requirement) {
			t.Fatalf("README.md missing public SDK entry requirement %q", requirement)
		}
	}
}

func TestReadmeKeepsInternalCapabilityLedgerInDesignDocs(t *testing.T) {
	body := readReleaseDoc(t, "README.md")

	for _, forbidden := range []string{
		"## 当前形态",
		"`gopacttest`：",
		"`templates/react`：",
		"`templates/devagent`：",
		"`cmd/gopact-extscaffold`：",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("README.md still exposes internal capability ledger entry %q", forbidden)
		}
	}

	for _, requirement := range []string{
		"docs/design/templates.md",
		"docs/design/modules.md",
		"docs/design/external-integration-roadmap.json",
	} {
		if !strings.Contains(body, requirement) {
			t.Fatalf("README.md missing design-doc handoff %q", requirement)
		}
	}

	if lineCount := strings.Count(body, "\n") + 1; lineCount > 130 {
		t.Fatalf("README.md has %d lines, want <= 130 after moving capability ledger to design docs", lineCount)
	}
}

func TestDevAgentProcessRecordsAreDocumented(t *testing.T) {
	for _, path := range []string{
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

	return readTextFile(t, path)
}
