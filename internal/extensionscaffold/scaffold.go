package extensionscaffold

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var errInvalidRepository = errors.New("extensionscaffold: invalid repository")

// Repository describes one external gopact extension repository to scaffold.
type Repository struct {
	Name               string
	Kind               string
	ModulePath         string
	SDKModule          string
	SDKVersion         string
	GoVersion          string
	SourcePaths        []string
	Targets            []Target
	RequiredFiles      []string
	RequiredCICommands []string
}

// Target describes one extension conformance target in an external repository.
type Target struct {
	Name               string
	Kind               string
	PackagePath        string
	MinimalExamplePath string
	SourcePaths        []string
	ConformanceSuites  []string
	RequiredExamples   []string
}

type conformanceHelperReference struct {
	Suite      string
	ImportPath string
	Function   string
}

var conformanceHelperCatalog = map[string][]conformanceHelperReference{
	"channel-transfer": {
		{Suite: "channel-transfer", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequireTransferConformance"},
	},
	"gopacttest-channel-conformance": {
		{Suite: "gopacttest-channel-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequireChannelConformance"},
	},
	"gopacttest-checkpoint-store-conformance": {
		{Suite: "gopacttest-checkpoint-store-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest/checkpointconformance", Function: "RequireCheckpointStoreConformance"},
	},
	"gopacttest-extension-scaffold-conformance": {
		{Suite: "gopacttest-extension-scaffold-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequireExtensionScaffoldConformance"},
	},
	"gopacttest-provider-conformance": {
		{Suite: "gopacttest-provider-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest/providerconformance", Function: "RequireProviderConformance"},
	},
	"gopacttest-react-deferred-memory-queue-conformance": {
		{Suite: "gopacttest-react-deferred-memory-queue-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest/reactconformance", Function: "RequireDeferredMemoryWorkQueueConformance"},
	},
	"gopacttest-template-trajectory-conformance": {
		{Suite: "gopacttest-template-trajectory-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequireTemplateTrajectoryConformance"},
	},
	"gopacttest-turnloop-store-conformance": {
		{Suite: "gopacttest-turnloop-store-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequireTurnLoopStoreConformance"},
	},
	"gopacttest-verification-evidence-conformance": {
		{Suite: "gopacttest-verification-evidence-conformance", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequireVerificationEvidenceConformance"},
	},
	"json-schema-validator": {
		{Suite: "json-schema-validator", ImportPath: "github.com/gopact-ai/gopact/gopacttest", Function: "RequirePortableJSONSchemaValidatorConformance"},
	},
	"trace-exporter-conformance": {
		{Suite: "trace-exporter-conformance", ImportPath: "github.com/gopact-ai/gopact/adapters/observability/trace", Function: "RequireExporterConformance"},
	},
}

// File is one rendered scaffold file.
type File struct {
	Path string
	Body string
}

// RenderRepository renders deterministic scaffold files for one external extension repository.
func RenderRepository(repo Repository) ([]File, error) {
	if err := validateRepository(repo); err != nil {
		return nil, err
	}

	renderers := map[string]func(Repository) string{
		"go.mod":                   renderGoMod,
		"README.md":                renderReadme,
		"CONFORMANCE.md":           renderConformance,
		"examples/minimal_test.go": renderMinimalTest,
		".github/workflows/ci.yml": renderCIWorkflow,
	}
	files := make([]File, 0, len(repo.RequiredFiles))
	for _, path := range repo.RequiredFiles {
		render, ok := renderers[path]
		if !ok {
			return nil, fmt.Errorf("%w: unsupported required file %q", errInvalidRepository, path)
		}
		files = append(files, File{Path: path, Body: render(repo)})
	}
	return files, nil
}

// WriteRepository writes rendered scaffold files under dir and returns the files written.
func WriteRepository(ctx context.Context, dir string, repo Repository) ([]File, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := RenderRepository(repo)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateRelativePath(file.Path); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("extensionscaffold: create directory for %q: %w", file.Path, err)
		}
		if err := os.WriteFile(path, []byte(file.Body), 0o644); err != nil {
			return nil, fmt.Errorf("extensionscaffold: write %q: %w", file.Path, err)
		}
	}
	return files, nil
}

func validateRepository(repo Repository) error {
	for field, value := range map[string]string{
		"name":        repo.Name,
		"kind":        repo.Kind,
		"module_path": repo.ModulePath,
		"sdk_module":  repo.SDKModule,
		"sdk_version": repo.SDKVersion,
		"go_version":  repo.GoVersion,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", errInvalidRepository, field)
		}
	}
	if len(repo.RequiredFiles) == 0 {
		return fmt.Errorf("%w: required files are required", errInvalidRepository)
	}
	if len(repo.RequiredCICommands) == 0 {
		return fmt.Errorf("%w: required CI commands are required", errInvalidRepository)
	}
	if len(repo.Targets) == 0 {
		return fmt.Errorf("%w: targets are required", errInvalidRepository)
	}
	for _, file := range repo.RequiredFiles {
		if err := validateRelativePath(file); err != nil {
			return err
		}
	}
	for _, target := range repo.Targets {
		if strings.TrimSpace(target.Name) == "" {
			return fmt.Errorf("%w: target name is required", errInvalidRepository)
		}
		if strings.TrimSpace(target.Kind) == "" {
			return fmt.Errorf("%w: target %q kind is required", errInvalidRepository, target.Name)
		}
		if strings.TrimSpace(target.PackagePath) == "" {
			return fmt.Errorf("%w: target %q package path is required", errInvalidRepository, target.Name)
		}
		if err := validateRelativePath(target.PackagePath); err != nil {
			return err
		}
		if strings.TrimSpace(target.MinimalExamplePath) == "" {
			return fmt.Errorf("%w: target %q minimal example path is required", errInvalidRepository, target.Name)
		}
		if err := validateRelativePath(target.MinimalExamplePath); err != nil {
			return err
		}
	}
	return nil
}

func validateRelativePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: path is required", errInvalidRepository)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return fmt.Errorf("%w: unsafe path %q", errInvalidRepository, path)
	}
	return nil
}

func renderGoMod(repo Repository) string {
	return fmt.Sprintf("module %s\n\ngo %s\n\nrequire %s %s\n", repo.ModulePath, repo.GoVersion, repo.SDKModule, repo.SDKVersion)
}

func renderReadme(repo Repository) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", repo.Name)
	fmt.Fprintf(&b, "This repository implements a gopact %s outside the core SDK.\n\n", repo.Kind)
	b.WriteString("## Compatibility\n\n")
	fmt.Fprintf(&b, "- SDK module: `%s`\n", repo.SDKModule)
	fmt.Fprintf(&b, "- Supported Go versions: `%s`\n", repo.GoVersion)
	fmt.Fprintf(&b, "- Extension kind: `%s`\n", repo.Kind)
	fmt.Fprintf(&b, "- Core source paths replaced or extended: `%s`\n\n", strings.Join(repo.SourcePaths, "`, `"))
	b.WriteString("## Installation\n\n")
	fmt.Fprintf(&b, "```bash\ngo get %s\n```\n\n", repo.ModulePath)
	b.WriteString("## Usage\n\n")
	b.WriteString("Keep credentials, endpoints, clients, stores, loggers, and policies host-owned and pass them through typed constructors or options.\n\n")
	b.WriteString("```go\n// Replace this block with the smallest constructor-based setup for this extension.\n```\n\n")
	b.WriteString("## Conformance\n\n")
	b.WriteString("Run the offline conformance suite:\n\n```bash\n")
	for _, command := range repo.RequiredCICommands {
		fmt.Fprintln(&b, command)
	}
	b.WriteString("```\n\n")
	b.WriteString("The default offline suite includes `gopacttest.RequireExtensionScaffoldConformance` so repository layout, host-owned config notes, CONFORMANCE commands, and examples stay aligned with the scaffold contract.\n\n")
	b.WriteString("## Targets\n\n")
	for _, target := range repo.Targets {
		fmt.Fprintf(&b, "- `%s` (%s): package `%s`\n", target.Name, target.Kind, target.PackagePath)
	}
	b.WriteString("\n## Security\n\n")
	b.WriteString("Document the trust boundary, secret ownership, outbound network behavior, persistence behavior, and redaction policy. Secrets must stay in host-owned clients, secret providers, or transport adapters.\n")
	return b.String()
}

func renderConformance(repo Repository) string {
	var b strings.Builder
	b.WriteString("# gopact Extension Conformance\n\n")
	b.WriteString("This file documents the compatibility contract for this external gopact extension repository.\n\n")
	b.WriteString("## Extension Targets\n\n")
	for _, target := range repo.Targets {
		fmt.Fprintf(&b, "### %s\n\n", target.Name)
		fmt.Fprintf(&b, "- Kind: `%s`\n", target.Kind)
		fmt.Fprintf(&b, "- Module path: `%s`\n", repo.ModulePath)
		fmt.Fprintf(&b, "- Package path: `%s`\n", target.PackagePath)
		fmt.Fprintf(&b, "- Core paths replaced or extended: `%s`\n", strings.Join(target.SourcePaths, "`, `"))
		b.WriteString("- Required suites:\n")
		for _, suite := range target.ConformanceSuites {
			fmt.Fprintf(&b, "  - `%s`\n", suite)
		}
		helperReferences := conformanceHelperReferencesForTarget(target)
		if len(helperReferences) > 0 {
			b.WriteString("- Available helper references:\n")
			for _, reference := range helperReferences {
				fmt.Fprintf(&b, "  - `%s`\n", reference.String())
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("## CI Commands\n\n")
	b.WriteString("The default external repository CI must run:\n\n```bash\n")
	for _, command := range repo.RequiredCICommands {
		fmt.Fprintln(&b, command)
	}
	b.WriteString("```\n\n")
	b.WriteString("The default offline suite calls `gopacttest.RequireExtensionScaffoldConformance` with the repository module path, required scaffold files, and already-observed file contents.\n\n")
	b.WriteString("## Integration Tests\n\n")
	b.WriteString("- Build tag: `<integration tag, if any>`\n")
	b.WriteString("- Required host-owned credentials or clients: `<none by default>`\n")
	b.WriteString("- Services touched: `<none by default>`\n\n")
	b.WriteString("## Security Boundary\n\n")
	b.WriteString("The SDK core remains configuration-file free. Host applications inject configuration through typed constructors, options, clients, providers, adapters, or plugins.\n")
	return b.String()
}

func renderMinimalTest(repo Repository) string {
	var b strings.Builder
	b.WriteString("package examples\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"os\"\n")
	b.WriteString("\t\"path/filepath\"\n")
	b.WriteString("\t\"strings\"\n")
	b.WriteString("\t\"testing\"\n\n")
	fmt.Fprintf(&b, "\t%s\n", strconv.Quote(repo.SDKModule+"/gopacttest"))
	b.WriteString(")\n\n")
	b.WriteString("func TestExtensionScaffoldConformance(t *testing.T) {\n")
	b.WriteString("\trequiredFiles := []string{\n")
	for _, path := range repo.RequiredFiles {
		fmt.Fprintf(&b, "\t\t%s,\n", strconv.Quote(path))
	}
	b.WriteString("\t}\n\tfiles := make(map[string]string, len(requiredFiles))\n")
	b.WriteString("\tfor _, path := range requiredFiles {\n")
	b.WriteString("\t\tfiles[path] = readRepositoryFile(t, path)\n")
	b.WriteString("\t}\n\n")
	b.WriteString("\tgopacttest.RequireExtensionScaffoldConformance(t, gopacttest.ExtensionScaffoldConformanceHarness{\n")
	fmt.Fprintf(&b, "\t\tModulePath:       %s,\n", strconv.Quote(repo.ModulePath))
	fmt.Fprintf(&b, "\t\tModulePathPrefix: %s,\n", strconv.Quote(modulePathPrefix(repo.ModulePath)))
	b.WriteString("\t\tRequiredFiles:    requiredFiles,\n")
	b.WriteString("\t\tFiles:            files,\n")
	b.WriteString("\t})\n")
	b.WriteString("}\n\n")
	helperReferences := conformanceHelperReferences(repo)
	if len(helperReferences) > 0 {
		b.WriteString("func TestRequiredConformanceHelperReferences(t *testing.T) {\n")
		b.WriteString("\tconformance := readRepositoryFile(t, \"CONFORMANCE.md\")\n")
		b.WriteString("\thelperReferences := []string{\n")
		for _, reference := range helperReferences {
			fmt.Fprintf(&b, "\t\t%s,\n", strconv.Quote(reference.String()))
		}
		b.WriteString("\t}\n")
		b.WriteString("\tfor _, helper := range helperReferences {\n")
		b.WriteString("\t\tif !strings.Contains(conformance, helper) {\n")
		b.WriteString("\t\t\tt.Fatalf(\"CONFORMANCE.md missing helper reference %s\", helper)\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
		b.WriteString("}\n\n")
	}
	b.WriteString("func readRepositoryFile(t *testing.T, path string) string {\n")
	b.WriteString("\tt.Helper()\n")
	b.WriteString("\trepoPath := filepath.FromSlash(path)\n")
	b.WriteString("\tif strings.HasPrefix(path, \"examples/\") {\n")
	b.WriteString("\t\trepoPath = filepath.Base(repoPath)\n")
	b.WriteString("\t} else {\n")
	b.WriteString("\t\trepoPath = filepath.Join(\"..\", repoPath)\n")
	b.WriteString("\t}\n")
	b.WriteString("\tbody, err := os.ReadFile(repoPath)\n")
	b.WriteString("\tif err != nil {\n")
	b.WriteString("\t\tt.Fatalf(\"read %s: %v\", path, err)\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn string(body)\n")
	b.WriteString("}\n")
	return b.String()
}

func renderCIWorkflow(repo Repository) string {
	var b strings.Builder
	b.WriteString("name: ci\n\n")
	b.WriteString("on:\n  pull_request:\n  push:\n    branches:\n      - main\n\n")
	b.WriteString("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n")
	b.WriteString("      - name: Checkout\n        uses: actions/checkout@v4\n\n")
	b.WriteString("      - name: Set up Go\n        uses: actions/setup-go@v5\n        with:\n")
	fmt.Fprintf(&b, "          go-version: %s\n", strconv.Quote(repo.GoVersion))
	b.WriteString("          cache: true\n\n")
	for i, command := range repo.RequiredCICommands {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "      - name: %s\n        run: %s\n", ciStepName(command), command)
	}
	return b.String()
}

func ciStepName(command string) string {
	switch command {
	case "git diff --check":
		return "Check formatting whitespace"
	case "go test -count=1 ./...":
		return "Test"
	case "go vet ./...":
		return "Vet"
	default:
		return command
	}
}

func modulePathPrefix(modulePath string) string {
	index := strings.LastIndex(modulePath, "/")
	if index < 0 {
		return ""
	}
	return modulePath[:index+1]
}

func conformanceHelperReferences(repo Repository) []conformanceHelperReference {
	var out []conformanceHelperReference
	seen := make(map[string]bool)
	for _, target := range repo.Targets {
		for _, reference := range conformanceHelperReferencesForTarget(target) {
			key := reference.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, reference)
		}
	}
	return out
}

func conformanceHelperReferencesForTarget(target Target) []conformanceHelperReference {
	var out []conformanceHelperReference
	seen := make(map[string]bool)
	for _, suite := range target.ConformanceSuites {
		for _, reference := range conformanceHelperCatalog[suite] {
			key := reference.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, reference)
		}
	}
	return out
}

func (r conformanceHelperReference) String() string {
	return r.ImportPath + "." + r.Function
}
