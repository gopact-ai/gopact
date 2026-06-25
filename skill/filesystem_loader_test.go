package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFilesystemLoaderLoadsSkillMetadataResourcesAndScripts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	skillDir := filepath.Join(root, "repo-review")
	writeTestFile(t, filepath.Join(skillDir, "SKILL.md"), `---
name: repo-review
description: Review repository changes.
version: 0.1.0
tags:
  - code
  - review
---

# Repo Review
`)
	writeTestFile(t, filepath.Join(skillDir, "references", "guide.md"), "# Guide\n")
	writeTestFile(t, filepath.Join(skillDir, "assets", "example.json"), "{}\n")
	writeTestFile(t, filepath.Join(skillDir, "scripts", "lint.sh"), "#!/bin/sh\necho lint\n")
	writeTestFile(t, filepath.Join(root, "notes.txt"), "not a skill\n")

	loader, err := NewFilesystemLoader(root)
	if err != nil {
		t.Fatalf("NewFilesystemLoader() error = %v", err)
	}

	skills, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %+v, want one skill", skills)
	}
	got := skills[0]
	if got.Name != "repo-review" || got.Description != "Review repository changes." || got.Version != "0.1.0" {
		t.Fatalf("skill = %+v, want repo-review descriptor", got)
	}
	if !reflect.DeepEqual(got.Metadata["tags"], []string{"code", "review"}) {
		t.Fatalf("tags metadata = %#v, want [code review]", got.Metadata["tags"])
	}
	wantResources := []Resource{
		{Name: "instructions", URI: "repo-review/SKILL.md", MIMEType: "text/markdown"},
		{Name: "example.json", URI: "repo-review/assets/example.json", MIMEType: "application/json"},
		{Name: "guide.md", URI: "repo-review/references/guide.md", MIMEType: "text/markdown"},
	}
	if !reflect.DeepEqual(got.Resources, wantResources) {
		t.Fatalf("resources = %#v, want %#v", got.Resources, wantResources)
	}
	wantScripts := []Script{{Name: "lint.sh", Command: []string{"repo-review/scripts/lint.sh"}}}
	if !reflect.DeepEqual(got.Scripts, wantScripts) {
		t.Fatalf("scripts = %#v, want %#v", got.Scripts, wantScripts)
	}
}

func TestFilesystemLoaderRegistersSkills(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "repo-review", "SKILL.md"), `---
name: repo-review
description: Review repository changes.
---
`)
	loader, err := NewFilesystemLoader(root)
	if err != nil {
		t.Fatalf("NewFilesystemLoader() error = %v", err)
	}
	registry := NewRegistry()

	if err := loader.Register(ctx, registry); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	skill, err := registry.Get(ctx, "repo-review")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if skill.Name != "repo-review" {
		t.Fatalf("registered skill = %+v, want repo-review", skill)
	}
}

func TestFilesystemLoaderRejectsInvalidManifest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "broken", "SKILL.md"), `---
name: broken
---
`)
	loader, err := NewFilesystemLoader(root)
	if err != nil {
		t.Fatalf("NewFilesystemLoader() error = %v", err)
	}

	_, err = loader.Load(ctx)
	if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("Load() error = %v, want ErrManifestInvalid", err)
	}
}

func TestNewFilesystemLoaderRequiresRoot(t *testing.T) {
	if _, err := NewFilesystemLoader(""); !errors.Is(err, ErrLoaderRootRequired) {
		t.Fatalf("NewFilesystemLoader(empty) error = %v, want ErrLoaderRootRequired", err)
	}
}

func TestFilesystemLoaderHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "repo-review", "SKILL.md"), `---
name: repo-review
description: Review repository changes.
---
`)
	loader, err := NewFilesystemLoader(root)
	if err != nil {
		t.Fatalf("NewFilesystemLoader() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = loader.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load(canceled) error = %v, want context.Canceled", err)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
