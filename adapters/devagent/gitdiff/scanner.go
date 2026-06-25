// Package gitdiff captures git working tree or staged diffs for Dev Agent evidence.
package gitdiff

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gopact-ai/gopact/gopacttest"
	"github.com/gopact-ai/gopact/templates/devagent"
)

var (
	// ErrRepoRequired is returned when a scanner has no repository path.
	ErrRepoRequired = errors.New("gitdiff: repo is required")
	// ErrRunnerRequired is returned when a scanner has no git command runner.
	ErrRunnerRequired = errors.New("gitdiff: runner is required")
)

// Runner executes a git command in dir and returns stdout, stderr, and an execution error.
type Runner func(ctx context.Context, dir string, args ...string) ([]byte, []byte, error)

// Option configures a Scanner.
type Option func(*Scanner) error

// WithRunner sets the git command runner.
func WithRunner(runner Runner) Option {
	return func(s *Scanner) error {
		if runner == nil {
			return ErrRunnerRequired
		}
		s.runner = runner
		return nil
	}
}

// WithStaged makes the scanner capture staged changes through git diff --cached.
func WithStaged(staged bool) Option {
	return func(s *Scanner) error {
		s.staged = staged
		return nil
	}
}

// Scanner captures git diffs and maps them to Dev Agent evidence inputs.
type Scanner struct {
	repo   string
	runner Runner
	staged bool
}

// Result contains both the verification diff snapshot and patch proposal views.
type Result struct {
	Diff  gopacttest.DiffSnapshot
	Patch devagent.PatchProposal
}

// New creates a git diff scanner for repo.
func New(repo string, opts ...Option) (*Scanner, error) {
	if strings.TrimSpace(repo) == "" {
		return nil, ErrRepoRequired
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return nil, fmt.Errorf("gitdiff: resolve repo: %w", err)
	}
	scanner := &Scanner{
		repo:   abs,
		runner: runGit,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(scanner); err != nil {
			return nil, err
		}
	}
	if scanner.runner == nil {
		return nil, ErrRunnerRequired
	}
	return scanner, nil
}

// Scan captures the configured git diff.
func (s *Scanner) Scan(ctx context.Context) (Result, error) {
	if s == nil || strings.TrimSpace(s.repo) == "" {
		return Result{}, ErrRepoRequired
	}
	if s.runner == nil {
		return Result{}, ErrRunnerRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	diffArgs := s.gitArgs("--binary")
	diffRaw, stderr, err := s.runner(ctx, s.repo, diffArgs...)
	if err != nil {
		scanErr := commandError("git diff", stderr, err)
		result := s.result("", nil, 0, 0)
		result.Diff.Err = scanErr
		return result, fmt.Errorf("gitdiff: capture diff: %w", scanErr)
	}

	numstatArgs := s.gitArgs("--numstat")
	numstatRaw, stderr, err := s.runner(ctx, s.repo, numstatArgs...)
	if err != nil {
		scanErr := commandError("git diff --numstat", stderr, err)
		result := s.result(string(diffRaw), nil, 0, 0)
		result.Diff.Err = scanErr
		return result, fmt.Errorf("gitdiff: capture numstat: %w", scanErr)
	}

	files, insertions, deletions := parseNumstat(string(numstatRaw))
	return s.result(string(diffRaw), files, insertions, deletions), nil
}

func (s *Scanner) gitArgs(extra string) []string {
	args := []string{"diff"}
	if s.staged {
		args = append(args, "--cached")
	}
	args = append(args, "--no-ext-diff", extra)
	return args
}

func (s *Scanner) result(diff string, files []string, insertions int, deletions int) Result {
	ref := "git:worktree"
	summary := "git working tree diff"
	if s.staged {
		ref = "git:staged"
		summary = "git staged diff"
	}
	metadata := map[string]any{
		"source": "gitdiff",
		"repo":   s.repo,
		"staged": s.staged,
	}
	diffSnapshot := gopacttest.DiffSnapshot{
		ID:         "git-diff",
		Name:       "git diff",
		Ref:        ref,
		Diff:       diff,
		Files:      append([]string(nil), files...),
		Insertions: insertions,
		Deletions:  deletions,
		Metadata:   metadata,
	}
	if strings.TrimSpace(diff) == "" {
		diffSnapshot.Skipped = true
		diffSnapshot.Summary = "no " + summary
	}

	patchFiles := make([]devagent.PatchFile, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		patchFiles = append(patchFiles, devagent.PatchFile{
			Path:   file,
			Intent: "observed git diff",
		})
	}
	return Result{
		Diff: diffSnapshot,
		Patch: devagent.PatchProposal{
			ID:      "git-diff",
			Summary: summary,
			Diff:    diff,
			Files:   patchFiles,
			Metadata: map[string]any{
				"source": "gitdiff",
				"repo":   s.repo,
				"staged": s.staged,
			},
		},
	}
}

func parseNumstat(raw string) ([]string, int, int) {
	var files []string
	var insertions int
	var deletions int
	seen := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if n, ok := parseNumstatCount(fields[0]); ok {
			insertions += n
		}
		if n, ok := parseNumstatCount(fields[1]); ok {
			deletions += n
		}
		file := normalizeNumstatPath(strings.Join(fields[2:], " "))
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		files = append(files, file)
	}
	return files, insertions, deletions
}

func parseNumstatCount(raw string) (int, bool) {
	if raw == "-" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

func normalizeNumstatPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.Trim(path, "\"")
	if path == "" || path == "/dev/null" || path == "dev/null" {
		return ""
	}
	if strings.Contains(path, " => ") {
		parts := strings.Split(path, " => ")
		path = parts[len(parts)-1]
	}
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return strings.TrimPrefix(path, "./")
}

func commandError(command string, stderr []byte, err error) error {
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%s: %s: %w", command, msg, err)
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	stdout, err := cmd.Output()
	if err == nil {
		return stdout, nil, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return stdout, exitErr.Stderr, err
	}
	return stdout, nil, err
}
