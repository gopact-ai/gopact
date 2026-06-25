package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact/sandbox"
)

func TestFileResourceReaderReadsWithinRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "references", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("# Guide\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	reader, err := NewFileResourceReader(root)
	if err != nil {
		t.Fatalf("NewFileResourceReader() error = %v", err)
	}

	content, err := reader.ReadResource(ctx, ResourceReadRequest{
		SkillName: "repo-review",
		Resource:  Resource{Name: "guide", URI: "references/guide.md", MIMEType: "text/markdown"},
	})
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if content.SkillName != "repo-review" || content.Resource.Name != "guide" {
		t.Fatalf("content identity = %+v, want repo-review guide", content)
	}
	if content.URI != "references/guide.md" || content.MIMEType != "text/markdown" {
		t.Fatalf("content uri/mime = %q/%q, want references/guide.md text/markdown", content.URI, content.MIMEType)
	}
	if string(content.Content) != "# Guide\n" || content.Text != "# Guide\n" {
		t.Fatalf("content = %q text = %q, want guide markdown", content.Content, content.Text)
	}
}

func TestFileResourceReaderRejectsPathEscape(t *testing.T) {
	ctx := context.Background()
	reader, err := NewFileResourceReader(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileResourceReader() error = %v", err)
	}

	_, err = reader.ReadResource(ctx, ResourceReadRequest{
		SkillName: "repo-review",
		Resource:  Resource{Name: "secret", URI: "../secret.txt"},
	})
	if !errors.Is(err, ErrResourcePathEscape) {
		t.Fatalf("ReadResource() error = %v, want ErrResourcePathEscape", err)
	}
}

func TestFileResourceReaderRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside secret) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret-link.txt")); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}
	reader, err := NewFileResourceReader(root)
	if err != nil {
		t.Fatalf("NewFileResourceReader() error = %v", err)
	}

	_, err = reader.ReadResource(ctx, ResourceReadRequest{
		SkillName: "repo-review",
		Resource:  Resource{Name: "secret", URI: "secret-link.txt"},
	})
	if !errors.Is(err, ErrResourcePathEscape) {
		t.Fatalf("ReadResource() error = %v, want ErrResourcePathEscape", err)
	}
}

func TestSandboxScriptRunnerExecutesThroughSandbox(t *testing.T) {
	ctx := context.Background()
	manager, err := sandbox.NewLocal(
		sandbox.WithRoot(t.TempDir()),
		sandbox.WithAllowedCommands("echo"),
		sandbox.WithOutputLimit(1024),
	)
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	runner, err := NewSandboxScriptRunner(manager)
	if err != nil {
		t.Fatalf("NewSandboxScriptRunner() error = %v", err)
	}

	result, err := runner.RunScript(ctx, ScriptRunRequest{
		SkillName: "repo-review",
		Script:    Script{Name: "echo", Command: []string{"echo", "hello"}},
		Args:      []string{"world"},
	})
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if result.SkillName != "repo-review" || result.ScriptName != "echo" {
		t.Fatalf("result identity = %+v, want repo-review echo", result)
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "hello world" {
		t.Fatalf("result = %+v, want exit 0 stdout hello world", result)
	}
	if result.Metadata["sandbox_session_id"] == "" {
		t.Fatalf("metadata = %+v, want sandbox session id", result.Metadata)
	}
}

func TestNewFileResourceReaderAndSandboxScriptRunnerRequireDependencies(t *testing.T) {
	if _, err := NewFileResourceReader(""); !errors.Is(err, ErrResourceRootRequired) {
		t.Fatalf("NewFileResourceReader(empty) error = %v, want ErrResourceRootRequired", err)
	}
	if _, err := NewSandboxScriptRunner(nil); !errors.Is(err, ErrSandboxManagerRequired) {
		t.Fatalf("NewSandboxScriptRunner(nil) error = %v, want ErrSandboxManagerRequired", err)
	}
}
