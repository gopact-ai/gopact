package sandbox

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalReadWriteFileWithinRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	manager, err := NewLocal(WithRoot(root), WithAllowedCommands("echo"))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := session.WriteFile(ctx, File{Path: "notes/result.txt", Content: []byte("hello")}); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := session.ReadFile(ctx, "notes/result.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got.Content) != "hello" {
		t.Fatalf("ReadFile() content = %q, want hello", got.Content)
	}
}

func TestLocalRejectsPathEscape(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err = session.WriteFile(ctx, File{Path: "../escape.txt", Content: []byte("nope")})
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("WriteFile() error = %v, want %v", err, ErrPathEscape)
	}
}

func TestLocalExecAllowedCommand(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"), WithOutputLimit(1024))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	result, err := session.Exec(ctx, ExecRequest{Command: []string{"echo", "hello"}})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "hello" {
		t.Fatalf("Exec() = %+v", result)
	}
}

func TestLocalExecRejectsDisallowedCommand(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = session.Exec(ctx, ExecRequest{Command: []string{"sh", "-c", "echo unsafe"}})
	if !errors.Is(err, ErrCommandDenied) {
		t.Fatalf("Exec() error = %v, want %v", err, ErrCommandDenied)
	}
}

func TestLocalExecEnforcesTimeout(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("sleep"), WithDefaultTimeout(10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = session.Exec(ctx, ExecRequest{Command: []string{"sleep", "1"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Exec() error = %v, want deadline exceeded", err)
	}
}

func TestLocalExecEnforcesOutputLimit(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"), WithOutputLimit(4))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = session.Exec(ctx, ExecRequest{Command: []string{"echo", "hello"}})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("Exec() error = %v, want %v", err, ErrOutputLimit)
	}
}

func TestLocalRejectsWorkingDirOutsideRoot(t *testing.T) {
	root := t.TempDir()
	manager, err := NewLocal(WithRoot(root))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}

	_, err = manager.Create(context.Background(), Spec{WorkingDir: filepath.Dir(root)})
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("Create() error = %v, want %v", err, ErrPathEscape)
	}
}
