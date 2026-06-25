package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

var ErrExtensionScaffoldConformanceFailed = errors.New("gopacttest: extension scaffold conformance failed")

// ExtensionScaffoldConformanceHarness describes one scaffolded extension repository under test.
type ExtensionScaffoldConformanceHarness struct {
	ModulePath       string
	ModulePathPrefix string
	RequiredFiles    []string
	Files            map[string]string
}

// ExtensionScaffoldConformanceResult is the observed result for one scaffold repository contract case.
type ExtensionScaffoldConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckExtensionScaffoldConformance runs reusable offline checks for scaffolded extension repositories.
func CheckExtensionScaffoldConformance(ctx context.Context, harness ExtensionScaffoldConformanceHarness) []ExtensionScaffoldConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []ExtensionScaffoldConformanceResult{failedExtensionScaffoldConformance("context", err)}
	}

	return []ExtensionScaffoldConformanceResult{
		checkExtensionScaffoldModulePath(harness),
		checkExtensionScaffoldRequiredFiles(harness.RequiredFiles, harness.Files),
		checkExtensionScaffoldGoMod(harness.ModulePath, harness.Files),
		checkExtensionScaffoldReadme(harness.Files),
		checkExtensionScaffoldConformanceDoc(harness.Files),
		checkExtensionScaffoldMinimalExample(harness.Files),
	}
}

// RequireExtensionScaffoldConformance fails the test unless the scaffold satisfies the extension repository contract.
func RequireExtensionScaffoldConformance(t testing.TB, harness ExtensionScaffoldConformanceHarness) {
	t.Helper()

	for _, result := range CheckExtensionScaffoldConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("extension scaffold conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkExtensionScaffoldModulePath(harness ExtensionScaffoldConformanceHarness) ExtensionScaffoldConformanceResult {
	if harness.ModulePath == "" {
		return failedExtensionScaffoldConformance("module-path", errors.New("module path is empty"))
	}
	if harness.ModulePathPrefix != "" && !strings.HasPrefix(harness.ModulePath, harness.ModulePathPrefix) {
		return failedExtensionScaffoldConformance("module-path", fmt.Errorf("module path %q does not use prefix %q", harness.ModulePath, harness.ModulePathPrefix))
	}
	return passedExtensionScaffoldConformance("module-path")
}

func checkExtensionScaffoldRequiredFiles(required []string, files map[string]string) ExtensionScaffoldConformanceResult {
	if len(required) == 0 {
		return failedExtensionScaffoldConformance("required-files", errors.New("required files is empty"))
	}
	for _, path := range required {
		if path == "" {
			return failedExtensionScaffoldConformance("required-files", errors.New("required file path is empty"))
		}
		if _, ok := files[path]; !ok {
			return failedExtensionScaffoldConformance("required-files", fmt.Errorf("missing required file %q", path))
		}
	}
	return passedExtensionScaffoldConformance("required-files")
}

func checkExtensionScaffoldGoMod(modulePath string, files map[string]string) ExtensionScaffoldConformanceResult {
	body, ok := files["go.mod"]
	if !ok {
		return passedExtensionScaffoldConformance("go-mod")
	}
	if strings.TrimSpace(body) == "" {
		return failedExtensionScaffoldConformance("go-mod", errors.New("go.mod is empty"))
	}
	lines := strings.Split(body, "\n")
	if !slices.Contains(lines, "module "+modulePath) {
		return failedExtensionScaffoldConformance("go-mod", fmt.Errorf("go.mod missing module %q", modulePath))
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "go ") {
			return passedExtensionScaffoldConformance("go-mod")
		}
	}
	return failedExtensionScaffoldConformance("go-mod", errors.New("go.mod missing go version"))
}

func checkExtensionScaffoldReadme(files map[string]string) ExtensionScaffoldConformanceResult {
	body, ok := files["README.md"]
	if !ok {
		return passedExtensionScaffoldConformance("readme")
	}
	if strings.TrimSpace(body) == "" {
		return failedExtensionScaffoldConformance("readme", errors.New("README.md is empty"))
	}
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "host-owned") && !strings.Contains(lower, "host owned") {
		return failedExtensionScaffoldConformance("readme", errors.New("README must document host-owned config"))
	}
	return passedExtensionScaffoldConformance("readme")
}

func checkExtensionScaffoldConformanceDoc(files map[string]string) ExtensionScaffoldConformanceResult {
	body, ok := files["CONFORMANCE.md"]
	if !ok {
		return passedExtensionScaffoldConformance("conformance-doc")
	}
	if strings.TrimSpace(body) == "" {
		return failedExtensionScaffoldConformance("conformance-doc", errors.New("CONFORMANCE.md is empty"))
	}
	for _, command := range []string{"git diff --check", "go test -count=1 ./...", "go vet ./..."} {
		if !strings.Contains(body, command) {
			return failedExtensionScaffoldConformance("conformance-doc", fmt.Errorf("CONFORMANCE.md missing command %q", command))
		}
	}
	return passedExtensionScaffoldConformance("conformance-doc")
}

func checkExtensionScaffoldMinimalExample(files map[string]string) ExtensionScaffoldConformanceResult {
	body, ok := files["examples/minimal_test.go"]
	if !ok {
		return passedExtensionScaffoldConformance("minimal-example")
	}
	if strings.TrimSpace(body) == "" {
		return failedExtensionScaffoldConformance("minimal-example", errors.New("minimal example is empty"))
	}
	if !strings.Contains(body, "package ") {
		return failedExtensionScaffoldConformance("minimal-example", errors.New("minimal example must be Go code with a package clause"))
	}
	return passedExtensionScaffoldConformance("minimal-example")
}

func passedExtensionScaffoldConformance(name string) ExtensionScaffoldConformanceResult {
	return ExtensionScaffoldConformanceResult{Case: name, Passed: true}
}

func failedExtensionScaffoldConformance(name string, err error) ExtensionScaffoldConformanceResult {
	return ExtensionScaffoldConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrExtensionScaffoldConformanceFailed, err),
	}
}
