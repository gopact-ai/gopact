package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const defaultOutputLimit int64 = 1 << 20

var localSessionSeq atomic.Uint64

// Local executes allowed commands and file operations under one root.
type Local struct {
	root            string
	allowedCommands map[string]struct{}
	outputLimit     int64
	defaultTimeout  time.Duration
}

// LocalOption configures Local.
type LocalOption func(*Local) error

// NewLocal creates a local sandbox manager.
func NewLocal(opts ...LocalOption) (*Local, error) {
	local := &Local{
		outputLimit:    defaultOutputLimit,
		defaultTimeout: 30 * time.Second,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(local); err != nil {
			return nil, err
		}
	}
	if local.root == "" {
		return nil, fmt.Errorf("sandbox: root is required")
	}
	root, err := filepath.Abs(local.root)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve root: %w", err)
	}
	local.root = filepath.Clean(root)
	return local, nil
}

// WithRoot sets the filesystem root.
func WithRoot(root string) LocalOption {
	return func(local *Local) error {
		local.root = root
		return nil
	}
}

// WithAllowedCommands replaces the command allowlist.
func WithAllowedCommands(commands ...string) LocalOption {
	return func(local *Local) error {
		local.allowedCommands = make(map[string]struct{}, len(commands))
		for _, command := range commands {
			if command == "" {
				continue
			}
			local.allowedCommands[filepath.Base(command)] = struct{}{}
		}
		return nil
	}
}

// WithOutputLimit sets the maximum stdout+stderr bytes.
func WithOutputLimit(limit int64) LocalOption {
	return func(local *Local) error {
		if limit <= 0 {
			return fmt.Errorf("sandbox: output limit must be positive")
		}
		local.outputLimit = limit
		return nil
	}
}

// WithDefaultTimeout sets the default exec timeout.
func WithDefaultTimeout(timeout time.Duration) LocalOption {
	return func(local *Local) error {
		if timeout <= 0 {
			return fmt.Errorf("sandbox: default timeout must be positive")
		}
		local.defaultTimeout = timeout
		return nil
	}
}

// Create starts a local sandbox session rooted under the configured directory.
func (l *Local) Create(ctx context.Context, spec Spec) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workingDir := l.root
	if spec.WorkingDir != "" {
		resolved, err := l.resolve(spec.WorkingDir)
		if err != nil {
			return nil, err
		}
		workingDir = resolved
	}
	return &localSession{
		id:              "local-" + strconv.FormatUint(localSessionSeq.Add(1), 10),
		root:            l.root,
		workingDir:      workingDir,
		env:             copyMap(spec.Env),
		allowedCommands: l.allowedCommands,
		outputLimit:     firstPositive(spec.Limits.OutputBytes, l.outputLimit),
		defaultTimeout:  firstDuration(spec.Limits.Timeout, l.defaultTimeout),
	}, nil
}

func (l *Local) resolve(path string) (string, error) {
	if path == "" {
		return l.root, nil
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(l.root, candidate)
	}
	cleaned, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve path: %w", err)
	}
	cleaned = filepath.Clean(cleaned)
	if cleaned != l.root && !strings.HasPrefix(cleaned, l.root+string(os.PathSeparator)) {
		return "", ErrPathEscape
	}
	return cleaned, nil
}

type localSession struct {
	id              string
	root            string
	workingDir      string
	env             map[string]string
	allowedCommands map[string]struct{}
	outputLimit     int64
	defaultTimeout  time.Duration
	closed          atomic.Bool
}

func (s *localSession) ID() string {
	return s.id
}

func (s *localSession) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecResult{}, err
	}
	if len(req.Command) == 0 || req.Command[0] == "" {
		return ExecResult{}, ErrCommandDenied
	}
	command := filepath.Base(req.Command[0])
	if _, ok := s.allowedCommands[command]; !ok {
		return ExecResult{}, ErrCommandDenied
	}

	timeout := firstDuration(req.Timeout, s.defaultTimeout)
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startedAt := time.Now()
	cmd := exec.CommandContext(execCtx, req.Command[0], req.Command[1:]...)
	cmd.Dir = s.workingDir
	cmd.Env = envList(s.env)
	cmd.Stdin = bytes.NewReader(req.Stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if execCtx.Err() != nil {
		return ExecResult{}, execCtx.Err()
	}
	if int64(stdout.Len()+stderr.Len()) > s.outputLimit {
		return ExecResult{}, ErrOutputLimit
	}

	result := ExecResult{
		ExitCode: 0,
		Stdout:   append([]byte(nil), stdout.Bytes()...),
		Stderr:   append([]byte(nil), stderr.Bytes()...),
		Usage:    ResourceUsage{Duration: time.Since(startedAt)},
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return ExecResult{}, fmt.Errorf("sandbox: exec command: %w", err)
	}
	return result, nil
}

func (s *localSession) ReadFile(ctx context.Context, path string) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	resolved, err := s.resolve(path)
	if err != nil {
		return File{}, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, ErrFileNotFound
		}
		return File{}, fmt.Errorf("sandbox: read file: %w", err)
	}
	return File{Path: path, Content: content}, nil
}

func (s *localSession) WriteFile(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := s.resolve(file.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("sandbox: create parent directories: %w", err)
	}
	if err := os.WriteFile(resolved, file.Content, 0o644); err != nil {
		return fmt.Errorf("sandbox: write file: %w", err)
	}
	return nil
}

func (s *localSession) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.closed.Store(true)
	return nil
}

func (s *localSession) resolve(path string) (string, error) {
	local := Local{root: s.root}
	return local.resolve(path)
}

func firstPositive(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}
