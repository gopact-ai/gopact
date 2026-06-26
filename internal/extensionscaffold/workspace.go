package extensionscaffold

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepositoryScaffold is one rendered external repository scaffold.
type RepositoryScaffold struct {
	Repository Repository
	Directory  string
	Files      []File
}

// BootstrapWorkspace is a local workspace that binds external scaffolds to the SDK module.
type BootstrapWorkspace struct {
	Scaffolds    []RepositoryScaffold
	GoWork       File
	SyncPlan     File
	SyncScript   File
	SecretScript File
}

// VerificationReport summarizes local conformance verification for a bootstrap workspace.
type VerificationReport struct {
	Results []VerificationResult
}

// VerificationResult records one generated repository verification command.
type VerificationResult struct {
	Repository  string
	Directory   string
	CommandLine string
	Command     []string
	Output      string
	Passed      bool
}

// RenderRepositoriesFromDesign renders every external repository scaffold from the design manifests.
func RenderRepositoriesFromDesign(root string) ([]RepositoryScaffold, error) {
	repos, err := LoadRepositoriesFromDesign(root)
	if err != nil {
		return nil, err
	}
	scaffolds := make([]RepositoryScaffold, 0, len(repos))
	for _, repo := range repos {
		if err := validateRelativePath(repo.Name); err != nil {
			return nil, fmt.Errorf("extensionscaffold: repository %q directory: %w", repo.Name, err)
		}
		files, err := RenderRepository(repo)
		if err != nil {
			return nil, fmt.Errorf("extensionscaffold: render repository %q: %w", repo.Name, err)
		}
		scaffolds = append(scaffolds, RepositoryScaffold{
			Repository: repo,
			Directory:  repo.Name,
			Files:      files,
		})
	}
	return scaffolds, nil
}

// WriteRepositoriesFromDesign writes every external repository scaffold under dir.
func WriteRepositoriesFromDesign(ctx context.Context, root, dir string) ([]RepositoryScaffold, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("extensionscaffold: output directory is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scaffolds, err := RenderRepositoriesFromDesign(root)
	if err != nil {
		return nil, err
	}
	for i := range scaffolds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		repoDir := filepath.Join(dir, filepath.FromSlash(scaffolds[i].Directory))
		files, err := WriteRepository(ctx, repoDir, scaffolds[i].Repository)
		if err != nil {
			return nil, fmt.Errorf("extensionscaffold: write repository %q: %w", scaffolds[i].Repository.Name, err)
		}
		scaffolds[i].Files = files
	}
	return scaffolds, nil
}

// WriteBootstrapWorkspace writes external repository scaffolds and a go.work file for local conformance runs.
func WriteBootstrapWorkspace(ctx context.Context, root, dir string) (BootstrapWorkspace, error) {
	if strings.TrimSpace(dir) == "" {
		return BootstrapWorkspace{}, fmt.Errorf("extensionscaffold: output directory is required")
	}
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return BootstrapWorkspace{}, fmt.Errorf("extensionscaffold: resolve SDK root: %w", err)
	}
	root = absRoot
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return BootstrapWorkspace{}, err
	}

	scaffolds, err := WriteRepositoriesFromDesign(ctx, root, dir)
	if err != nil {
		return BootstrapWorkspace{}, err
	}
	goWork, err := renderGoWorkFile(root, dir, scaffolds)
	if err != nil {
		return BootstrapWorkspace{}, err
	}
	path := filepath.Join(dir, filepath.FromSlash(goWork.Path))
	if err := os.WriteFile(path, []byte(goWork.Body), 0o644); err != nil {
		return BootstrapWorkspace{}, fmt.Errorf("extensionscaffold: write %q: %w", goWork.Path, err)
	}
	plan, err := RenderSyncPlanFromDesign(root)
	if err != nil {
		return BootstrapWorkspace{}, err
	}
	syncPlan, err := renderSyncPlanFile(plan)
	if err != nil {
		return BootstrapWorkspace{}, err
	}
	path = filepath.Join(dir, filepath.FromSlash(syncPlan.Path))
	if err := os.WriteFile(path, []byte(syncPlan.Body), 0o644); err != nil {
		return BootstrapWorkspace{}, fmt.Errorf("extensionscaffold: write %q: %w", syncPlan.Path, err)
	}
	syncScript := renderSyncScriptFile(plan)
	path = filepath.Join(dir, filepath.FromSlash(syncScript.Path))
	if err := os.WriteFile(path, []byte(syncScript.Body), 0o755); err != nil {
		return BootstrapWorkspace{}, fmt.Errorf("extensionscaffold: write %q: %w", syncScript.Path, err)
	}
	secretScript := renderSecretScriptFile(plan)
	path = filepath.Join(dir, filepath.FromSlash(secretScript.Path))
	if err := os.WriteFile(path, []byte(secretScript.Body), 0o755); err != nil {
		return BootstrapWorkspace{}, fmt.Errorf("extensionscaffold: write %q: %w", secretScript.Path, err)
	}
	return BootstrapWorkspace{
		Scaffolds:    scaffolds,
		GoWork:       goWork,
		SyncPlan:     syncPlan,
		SyncScript:   syncScript,
		SecretScript: secretScript,
	}, nil
}

// VerifyBootstrapWorkspace runs every required CI command in each generated repository.
func VerifyBootstrapWorkspace(ctx context.Context, dir string, workspace BootstrapWorkspace) (VerificationReport, error) {
	if strings.TrimSpace(dir) == "" {
		return VerificationReport{}, fmt.Errorf("extensionscaffold: output directory is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return VerificationReport{}, err
	}
	cacheDir := filepath.Join(dir, ".gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return VerificationReport{}, fmt.Errorf("extensionscaffold: create workspace go cache: %w", err)
	}

	resultCount := 0
	for _, scaffold := range workspace.Scaffolds {
		resultCount += len(scaffold.Repository.RequiredCICommands)
	}
	report := VerificationReport{
		Results: make([]VerificationResult, 0, resultCount),
	}
	for _, scaffold := range workspace.Scaffolds {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		repoDir := filepath.Join(dir, filepath.FromSlash(scaffold.Directory))
		if err := ensureVerificationGitRepository(ctx, repoDir); err != nil {
			return report, fmt.Errorf("extensionscaffold: prepare verification repository %q: %w", scaffold.Repository.Name, err)
		}
		for _, commandLine := range scaffold.Repository.RequiredCICommands {
			command, err := commandShell(commandLine)
			if err != nil {
				return report, fmt.Errorf("extensionscaffold: verify repository %q: %w", scaffold.Repository.Name, err)
			}
			cmd := exec.CommandContext(ctx, command[0], command[1:]...)
			cmd.Dir = repoDir
			cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)
			output, err := cmd.CombinedOutput()
			result := VerificationResult{
				Repository:  scaffold.Repository.Name,
				Directory:   repoDir,
				CommandLine: commandLine,
				Command:     command,
				Output:      string(output),
				Passed:      err == nil,
			}
			report.Results = append(report.Results, result)
			if err != nil {
				return report, fmt.Errorf(
					"extensionscaffold: verify repository %q command %q: %w\n%s",
					scaffold.Repository.Name,
					commandLine,
					err,
					strings.TrimSpace(result.Output),
				)
			}
		}
	}
	return report, nil
}

func ensureVerificationGitRepository(ctx context.Context, repoDir string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--is-inside-work-tree")
	if err := cmd.Run(); err != nil {
		initCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "init", "-b", "main")
		if output, initErr := initCmd.CombinedOutput(); initErr != nil {
			return fmt.Errorf("%w\n%s", initErr, strings.TrimSpace(string(output)))
		}
	}
	addCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "add", "-N", ".")
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func commandShell(commandLine string) ([]string, error) {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return nil, fmt.Errorf("empty CI command")
	}
	return []string{"bash", "-lc", commandLine}, nil
}

func renderGoWorkFile(root, _ string, scaffolds []RepositoryScaffold) (File, error) {
	goVersion := goModVersion(root)
	if strings.TrimSpace(goVersion) == "" {
		goVersion = "1.25.11"
		if len(scaffolds) > 0 && strings.TrimSpace(scaffolds[0].Repository.GoVersion) != "" {
			goVersion = scaffolds[0].Repository.GoVersion
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "go %s\n\n", goVersion)
	b.WriteString("use (\n")
	fmt.Fprintf(&b, "\t%s\n", goWorkPath(root))
	for _, scaffold := range scaffolds {
		fmt.Fprintf(&b, "\t%s\n", goWorkPath(scaffold.Directory))
	}
	b.WriteString(")\n")
	return File{Path: "go.work", Body: b.String()}, nil
}

func goWorkPath(path string) string {
	slashPath := filepath.ToSlash(path)
	if strings.HasPrefix(slashPath, ".") || filepath.IsAbs(path) {
		return slashPath
	}
	return "./" + slashPath
}

func goModVersion(root string) string {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return ""
}
