package repositorychecks

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPublicAPIExamplesManifestCoversRequiredEntrypoints(t *testing.T) {
	manifest := loadPublicAPIExamplesManifest(t)
	boundary := loadPublicAPIBoundaryManifest(t)
	examples := rootExampleFunctions(t)

	if manifest.Scope != "root-public-api-ergonomics" {
		t.Fatalf("public api examples scope = %q, want root-public-api-ergonomics", manifest.Scope)
	}
	if strings.TrimSpace(manifest.Policy) == "" {
		t.Fatal("public api examples policy is empty")
	}
	if len(manifest.RequiredExamples) == 0 {
		t.Fatal("public api examples required_examples is empty")
	}

	symbols := publicAPISymbolEntries(boundary)
	for _, example := range manifest.RequiredExamples {
		if example.Name == "" {
			t.Fatal("public api examples entry has empty example name")
		}
		if !strings.HasPrefix(example.Name, "Example") {
			t.Fatalf("public api example %q must use a Go Example function name", example.Name)
		}
		if !examples[example.Name] {
			t.Fatalf("public api example %q is required by manifest but no root Example function exists", example.Name)
		}
		if len(example.Categories) == 0 {
			t.Fatalf("public api example %q categories is empty", example.Name)
		}
		for _, category := range example.Categories {
			if !isAllowedPublicAPICategory(category) {
				t.Fatalf("public api example %q category %q is not recognized", example.Name, category)
			}
		}
		if len(example.Symbols) == 0 {
			t.Fatalf("public api example %q symbols is empty", example.Name)
		}
		for _, symbol := range example.Symbols {
			entry, ok := symbols[symbol]
			if !ok {
				t.Fatalf("public api example %q references unknown root symbol %q", example.Name, symbol)
			}
			if !slices.Contains(example.Categories, entry.Category) {
				t.Fatalf("public api example %q references %q category %q outside %v", example.Name, symbol, entry.Category, example.Categories)
			}
		}
		if strings.TrimSpace(example.Rationale) == "" {
			t.Fatalf("public api example %q rationale is empty", example.Name)
		}
	}
}

type publicAPIExamplesManifest struct {
	Version          int                     `json:"version"`
	Scope            string                  `json:"scope"`
	Policy           string                  `json:"policy"`
	RequiredExamples []publicAPIExampleEntry `json:"required_examples"`
}

type publicAPIExampleEntry struct {
	Name       string   `json:"name"`
	Categories []string `json:"categories"`
	Symbols    []string `json:"symbols"`
	Rationale  string   `json:"rationale"`
}

func loadPublicAPIExamplesManifest(t *testing.T) publicAPIExamplesManifest {
	t.Helper()

	raw := readFile(t, filepath.Join("docs", "design", "public-api-examples.json"))
	var manifest publicAPIExamplesManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode public api examples manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("public api examples manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}

func publicAPISymbolEntries(manifest publicAPIBoundaryManifest) map[string]publicAPIBoundaryEntry {
	entries := map[string]publicAPIBoundaryEntry{}
	for _, group := range manifest.Groups {
		for _, symbol := range group.Symbols {
			entries[symbol.Name] = publicAPIBoundaryEntry{
				Name:       symbol.Name,
				Kind:       symbol.Kind,
				Category:   group.Category,
				Stability:  group.Stability,
				SourceFile: group.SourceFile,
				Rationale:  group.Rationale,
			}
		}
	}
	return entries
}

func rootExampleFunctions(t *testing.T) map[string]bool {
	t.Helper()

	files, err := os.ReadDir(repoRoot(t))
	if err != nil {
		t.Fatalf("read root package files: %v", err)
	}
	examples := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, repoPath(t, name), nil, 0)
		if err != nil {
			t.Fatalf("parse root package test file %q: %v", name, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Example") {
				examples[fn.Name.Name] = true
			}
		}
	}
	return examples
}
