package repositorychecks

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRepositoryBoundaryManifestCoversExtensionPackages(t *testing.T) {
	manifest := loadRepositoryBoundaryManifest(t)
	entries := make(map[string]repositoryBoundaryEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.Path == "" {
			t.Fatal("repository boundary entry path is empty")
		}
		if _, ok := repositoryBoundaryDispositions[entry.Disposition]; !ok {
			t.Fatalf("repository boundary entry %q has invalid disposition %q", entry.Path, entry.Disposition)
		}
		if strings.TrimSpace(entry.Rationale) == "" {
			t.Fatalf("repository boundary entry %q rationale is empty", entry.Path)
		}
		if entries[entry.Path].Path != "" {
			t.Fatalf("repository boundary entry %q is duplicated", entry.Path)
		}
		entries[entry.Path] = entry
	}

	for _, path := range repositoryBoundaryRequiredPaths(t) {
		if entries[path].Path == "" {
			t.Fatalf("repository boundary manifest is missing %q", path)
		}
	}
}

func TestRepositoryBoundaryReferenceOnlyPackagesStayLightweight(t *testing.T) {
	manifest := loadRepositoryBoundaryManifest(t)

	if manifest.ReferencePolicy.ImportPolicy != "stdlib-or-gopact-only" {
		t.Fatalf("repository boundary reference policy import_policy = %q, want stdlib-or-gopact-only", manifest.ReferencePolicy.ImportPolicy)
	}
	if strings.TrimSpace(manifest.ReferencePolicy.Rationale) == "" {
		t.Fatal("repository boundary reference policy rationale is empty")
	}

	for _, entry := range manifest.Entries {
		if entry.Disposition != "reference-only" {
			continue
		}
		for _, imp := range packageImports(t, entry.Path) {
			if isStandardLibraryImport(imp) || strings.HasPrefix(imp, "github.com/gopact-ai/gopact") {
				continue
			}
			t.Fatalf("reference-only package %q imports external dependency %q", entry.Path, imp)
		}
	}
}

type repositoryBoundaryManifest struct {
	Version         int                       `json:"version"`
	ReferencePolicy repositoryReferencePolicy `json:"reference_policy"`
	Entries         []repositoryBoundaryEntry `json:"entries"`
}

type repositoryReferencePolicy struct {
	ImportPolicy string `json:"import_policy"`
	Rationale    string `json:"rationale"`
}

type repositoryBoundaryEntry struct {
	Path        string `json:"path"`
	Disposition string `json:"disposition"`
	Target      string `json:"target,omitempty"`
	Rationale   string `json:"rationale"`
}

var repositoryBoundaryDispositions = map[string]struct{}{
	"keep-in-core":          {},
	"reference-only":        {},
	"move-to-adapter-repo":  {},
	"move-to-template-repo": {},
	"remove-before-v1":      {},
}

func loadRepositoryBoundaryManifest(t *testing.T) repositoryBoundaryManifest {
	t.Helper()

	raw := readFile(t, filepath.Join("doc", "design", "repository-boundary.json"))
	var manifest repositoryBoundaryManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode repository boundary manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("repository boundary manifest version = %d, want 1", manifest.Version)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("repository boundary manifest entries are empty")
	}
	return manifest
}

func repositoryBoundaryRequiredPaths(t *testing.T) []string {
	t.Helper()

	paths := []string{
		"a2a",
		"mcp",
		"provider",
	}
	paths = append(paths, repositoryBoundaryPackageDirs(t, "adapters")...)
	paths = append(paths, repositoryBoundaryPackageDirs(t, "templates")...)
	sort.Strings(paths)
	return paths
}

func repositoryBoundaryPackageDirs(t *testing.T, root string) []string {
	t.Helper()

	seen := map[string]struct{}{}
	base := repoRoot(t)
	err := filepath.WalkDir(repoPath(t, root), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if name == "testdata" {
			return filepath.SkipDir
		}
		goFiles, err := filepath.Glob(filepath.Join(path, "*.go"))
		if err != nil {
			return err
		}
		for _, goFile := range goFiles {
			if strings.HasSuffix(goFile, "_test.go") {
				continue
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(rel)] = struct{}{}
			break
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s packages: %v", root, err)
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func packageImports(t *testing.T, dir string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(repoPath(t, dir), "*.go"))
	if err != nil {
		t.Fatalf("glob go files for %q: %v", dir, err)
	}
	seen := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports for %q: %v", file, err)
		}
		for _, spec := range parsed.Imports {
			seen[strings.Trim(spec.Path.Value, `"`)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for imp := range seen {
		out = append(out, imp)
	}
	sort.Strings(out)
	return out
}

func isStandardLibraryImport(path string) bool {
	first, _, ok := strings.Cut(path, "/")
	if !ok {
		first = path
	}
	return !strings.Contains(first, ".")
}
