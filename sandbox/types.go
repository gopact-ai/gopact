// Package sandbox provides controlled local and in-memory execution sessions.
package sandbox

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrPathEscape is returned when a sandbox path escapes its root.
	ErrPathEscape = errors.New("sandbox: path escapes root")
	// ErrCommandDenied is returned when command execution is denied.
	ErrCommandDenied = errors.New("sandbox: command denied")
	// ErrOutputLimit is returned when command output exceeds the configured limit.
	ErrOutputLimit = errors.New("sandbox: output limit exceeded")
	// ErrExecUnsupported is returned when a session does not support command execution.
	ErrExecUnsupported = errors.New("sandbox: exec unsupported")
	// ErrFileNotFound is returned when a sandbox file does not exist.
	ErrFileNotFound = errors.New("sandbox: file not found")
)

// Manager creates sandbox sessions.
type Manager interface {
	Create(ctx context.Context, spec Spec) (Session, error)
}

// Session is a controlled execution and file boundary.
type Session interface {
	ID() string
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
	ReadFile(ctx context.Context, path string) (File, error)
	WriteFile(ctx context.Context, file File) error
	Close(ctx context.Context) error
}

// Spec configures one sandbox session.
type Spec struct {
	WorkingDir string
	Env        map[string]string
	Limits     ResourceLimits
	Metadata   map[string]any
}

// ResourceLimits bounds sandbox execution.
type ResourceLimits struct {
	OutputBytes int64
	Timeout     time.Duration
}

// ExecRequest describes an argv-style command execution.
type ExecRequest struct {
	Command  []string
	Stdin    []byte
	Timeout  time.Duration
	Metadata map[string]any
}

// ExecResult is the normalized command result.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	Usage    ResourceUsage
}

// ResourceUsage captures basic execution usage.
type ResourceUsage struct {
	Duration time.Duration
}

// File is a sandbox file payload.
type File struct {
	Path     string
	Content  []byte
	MIMEType string
	Metadata map[string]any
}
