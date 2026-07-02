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

func TestPublicAPIBoundaryManifestCoversRootTopLevelExports(t *testing.T) {
	exports := rootTopLevelExports(t)
	manifest := loadPublicAPIBoundaryManifest(t)

	if manifest.Scope != "root-top-level" {
		t.Fatalf("public api boundary scope = %q, want root-top-level", manifest.Scope)
	}
	for _, category := range manifest.AllowedCategories {
		if !isAllowedPublicAPICategory(category) {
			t.Fatalf("public api boundary allowed category %q is not recognized", category)
		}
	}

	entries := map[string]publicAPIBoundaryEntry{}
	for _, group := range manifest.Groups {
		if group.Category == "" {
			t.Fatalf("public api boundary group for %q category is empty", group.SourceFile)
		}
		if !slices.Contains(manifest.AllowedCategories, group.Category) {
			t.Fatalf("public api boundary group for %q category %q is not allowed", group.SourceFile, group.Category)
		}
		if group.Stability != "stable" && group.Stability != "experimental" && group.Stability != "transitional" {
			t.Fatalf("public api boundary group for %q stability %q is invalid", group.SourceFile, group.Stability)
		}
		if group.SourceFile == "" {
			t.Fatalf("public api boundary group for category %q source_file is empty", group.Category)
		}
		if strings.TrimSpace(group.Rationale) == "" {
			t.Fatalf("public api boundary group for %q rationale is empty", group.SourceFile)
		}
		if len(group.Symbols) == 0 {
			t.Fatalf("public api boundary group for %q symbols is empty", group.SourceFile)
		}
		for _, symbol := range group.Symbols {
			if symbol.Name == "" {
				t.Fatalf("public api boundary group for %q has empty symbol name", group.SourceFile)
			}
			if symbol.Kind != "const" && symbol.Kind != "var" && symbol.Kind != "type" && symbol.Kind != "func" {
				t.Fatalf("public api boundary symbol %q kind %q is invalid", symbol.Name, symbol.Kind)
			}
			if entries[symbol.Name].Name != "" {
				t.Fatalf("public api boundary symbol %q is duplicated", symbol.Name)
			}
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

	for _, symbol := range exports {
		entry, ok := entries[symbol.name]
		if !ok {
			t.Fatalf("public api boundary manifest missing root export %q (%s in %s)", symbol.name, symbol.kind, symbol.file)
		}
		if entry.Kind != symbol.kind {
			t.Fatalf("public api boundary entry %q kind = %q, want %q", symbol.name, entry.Kind, symbol.kind)
		}
	}
	for _, entry := range entries {
		symbol, ok := exports[entry.Name]
		if !ok {
			t.Fatalf("public api boundary entry %q is not an exported root symbol", entry.Name)
		}
		if entry.SourceFile != "" && entry.SourceFile != symbol.file {
			t.Fatalf("public api boundary entry %q source_file = %q, want %q", entry.Name, entry.SourceFile, symbol.file)
		}
	}
}

func TestPublicAPIBoundaryCoversRootExportedMethodsByReceiver(t *testing.T) {
	methods := rootExportedMethods(t)
	manifest := loadPublicAPIBoundaryManifest(t)

	if manifest.MethodPolicy.Scope != "exported-root-receiver-methods" {
		t.Fatalf("public api boundary method policy scope = %q, want exported-root-receiver-methods", manifest.MethodPolicy.Scope)
	}
	if manifest.MethodPolicy.Coverage != "inherited-from-receiver-type" {
		t.Fatalf("public api boundary method policy coverage = %q, want inherited-from-receiver-type", manifest.MethodPolicy.Coverage)
	}
	if strings.TrimSpace(manifest.MethodPolicy.Rationale) == "" {
		t.Fatal("public api boundary method policy rationale is empty")
	}

	entries := publicAPISymbolEntries(manifest)
	for _, method := range methods {
		entry, ok := entries[method.receiver]
		if !ok {
			t.Fatalf("public api boundary missing receiver type %q for exported method %s.%s in %s", method.receiver, method.receiver, method.name, method.file)
		}
		if entry.Kind != "type" {
			t.Fatalf("public api boundary receiver %q kind = %q, want type", method.receiver, entry.Kind)
		}
		if !slices.Contains(manifest.AllowedCategories, entry.Category) {
			t.Fatalf("public api boundary receiver %q category %q is not allowed", method.receiver, entry.Category)
		}
	}
}

type publicAPIBoundaryManifest struct {
	Version           int                      `json:"version"`
	Scope             string                   `json:"scope"`
	AllowedCategories []string                 `json:"allowed_categories"`
	MethodPolicy      publicAPIMethodPolicy    `json:"method_policy"`
	Groups            []publicAPIBoundaryGroup `json:"groups"`
}

type publicAPIMethodPolicy struct {
	Scope     string `json:"scope"`
	Coverage  string `json:"coverage"`
	Rationale string `json:"rationale"`
}

type publicAPIBoundaryGroup struct {
	Category   string                    `json:"category"`
	Stability  string                    `json:"stability"`
	SourceFile string                    `json:"source_file"`
	Rationale  string                    `json:"rationale"`
	Symbols    []publicAPIBoundarySymbol `json:"symbols"`
}

type publicAPIBoundarySymbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type publicAPIBoundaryEntry struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Category   string `json:"category"`
	Stability  string `json:"stability"`
	SourceFile string `json:"source_file"`
	Rationale  string `json:"rationale"`
}

type rootExportSymbol struct {
	name string
	kind string
	file string
}

type rootExportedMethod struct {
	receiver string
	name     string
	file     string
}

func loadPublicAPIBoundaryManifest(t *testing.T) publicAPIBoundaryManifest {
	t.Helper()

	raw := readFile(t, filepath.Join("doc", "design", "public-api-boundary.json"))
	var manifest publicAPIBoundaryManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode public api boundary manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("public api boundary manifest version = %d, want 1", manifest.Version)
	}
	if len(manifest.AllowedCategories) == 0 {
		t.Fatal("public api boundary allowed_categories is empty")
	}
	return manifest
}

func rootTopLevelExports(t *testing.T) map[string]rootExportSymbol {
	t.Helper()

	files, err := os.ReadDir(repoRoot(t))
	if err != nil {
		t.Fatalf("read root package files: %v", err)
	}
	exports := map[string]rootExportSymbol{}
	fset := token.NewFileSet()
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, repoPath(t, name), nil, 0)
		if err != nil {
			t.Fatalf("parse root package file %q: %v", name, err)
		}
		for _, decl := range parsed.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil && decl.Name.IsExported() {
					exports[decl.Name.Name] = rootExportSymbol{
						name: decl.Name.Name,
						kind: "func",
						file: name,
					}
				}
			case *ast.GenDecl:
				kind := publicAPITokenKind(decl.Tok)
				if kind == "" {
					continue
				}
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() {
							exports[spec.Name.Name] = rootExportSymbol{
								name: spec.Name.Name,
								kind: "type",
								file: name,
							}
						}
					case *ast.ValueSpec:
						for _, ident := range spec.Names {
							if ident.IsExported() {
								exports[ident.Name] = rootExportSymbol{
									name: ident.Name,
									kind: kind,
									file: name,
								}
							}
						}
					}
				}
			}
		}
	}
	return exports
}

func rootExportedMethods(t *testing.T) []rootExportedMethod {
	t.Helper()

	files, err := os.ReadDir(repoRoot(t))
	if err != nil {
		t.Fatalf("read root package files: %v", err)
	}
	var methods []rootExportedMethod
	fset := token.NewFileSet()
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, repoPath(t, name), nil, 0)
		if err != nil {
			t.Fatalf("parse root package file %q: %v", name, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() || len(fn.Recv.List) == 0 {
				continue
			}
			receiver := rootReceiverTypeName(fn.Recv.List[0].Type)
			if receiver == "" || !ast.IsExported(receiver) {
				continue
			}
			methods = append(methods, rootExportedMethod{
				receiver: receiver,
				name:     fn.Name.Name,
				file:     name,
			})
		}
	}
	return methods
}

func rootReceiverTypeName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return rootReceiverTypeName(expr.X)
	case *ast.IndexExpr:
		return rootReceiverTypeName(expr.X)
	case *ast.IndexListExpr:
		return rootReceiverTypeName(expr.X)
	default:
		return ""
	}
}

func publicAPITokenKind(tok token.Token) string {
	switch tok {
	case token.CONST:
		return "const"
	case token.VAR:
		return "var"
	case token.TYPE:
		return "type"
	default:
		return ""
	}
}

func isAllowedPublicAPICategory(category string) bool {
	switch category {
	case "core-contract",
		"runtime-facade",
		"typed-option",
		"middleware-plugin",
		"export-import-resume",
		"verification",
		"reference-implementation",
		"transitional":
		return true
	default:
		return false
	}
}
