package repositorychecks

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentationFilesStayUnderDocExceptReadmes(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}

		slashPath := filepath.ToSlash(path)
		if entry.Name() == "README.md" || strings.HasPrefix(slashPath, "doc/") {
			return nil
		}
		t.Fatalf("%s is a Markdown document outside doc/ and is not a README", slashPath)
		return nil
	})
	if err != nil {
		t.Fatalf("walk markdown docs: %v", err)
	}
}

func TestMarkdownDocsAreBilingual(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}

		body := readTextFile(t, path)
		for _, want := range []string{
			"<!-- gopact:doc-language: zh,en -->",
			"## 中文",
			"## English",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing bilingual documentation marker %q", filepath.ToSlash(path), want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk markdown docs: %v", err)
	}
}

func TestReadmeBadgesAndDocIndexAreConfigured(t *testing.T) {
	readme := readTextFile(t, "README.md")

	for _, want := range []string{
		"https://github.com/gopact-ai/gopact/actions/workflows/ci.yml/badge.svg?branch=main",
		"https://img.shields.io/github/license/gopact-ai/gopact",
		"https://pkg.go.dev/badge/github.com/gopact-ai/gopact.svg",
		"doc/README.md",
		"doc/FEATURES.md",
		"doc/CONTRIBUTING.md",
		"doc/SECURITY.md",
		"doc/CHANGELOG.md",
		"doc/maintainers/repository-governance.md",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing badge or doc index entry %q", want)
		}
	}
}
