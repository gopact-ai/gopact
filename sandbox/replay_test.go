package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestReplayHandlerExecutesRecordedCommand(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"), WithOutputLimit(1024))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-1",
					Type:           EffectTypeSandboxExec,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:echo",
					Sandbox: &gopact.SandboxEffect{
						Operation: SandboxOperationExec,
						Command:   []string{"echo", "hello"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	result, ok := results[0].Metadata[EffectReplayMetadataExecResult].(ExecResult)
	if !ok {
		t.Fatalf("exec result metadata = %#v, want sandbox.ExecResult", results[0].Metadata[EffectReplayMetadataExecResult])
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "hello" {
		t.Fatalf("exec result = %+v, want echo output", result)
	}
	sessionID, ok := results[0].Metadata[EffectReplayMetadataSessionID].(string)
	if !ok || sessionID == "" {
		t.Fatalf("session id metadata = %#v, want non-empty string", results[0].Metadata[EffectReplayMetadataSessionID])
	}
}

func TestReplayHandlerCanBeRegisteredInEffectReplayRegistry(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"), WithOutputLimit(1024))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	replayRegistry := gopact.NewEffectReplayRegistry()
	if err := replayRegistry.Register(EffectTypeSandboxExec, NewReplayHandler(manager)); err != nil {
		t.Fatalf("Register(replay handler) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-1",
					Type:           EffectTypeSandboxExec,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:registry",
					Sandbox: &gopact.SandboxEffect{
						Operation: SandboxOperationExec,
						Command:   []string{"echo", "registry"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, replayRegistry)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	result := results[0].Metadata[EffectReplayMetadataExecResult].(ExecResult)
	if strings.TrimSpace(string(result.Stdout)) != "registry" {
		t.Fatalf("exec stdout = %q, want registry", result.Stdout)
	}
}

func TestReplayHandlerWritesRecordedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	manager, err := NewLocal(WithRoot(root))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-file-1",
					Type:           EffectTypeSandboxFileWrite,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:file-write",
					Sandbox: &gopact.SandboxEffect{
						Operation: SandboxOperationWriteFile,
						Path:      "notes/out.txt",
					},
					Metadata: map[string]any{
						EffectMetadataFileContent:  []byte("hello file"),
						EffectMetadataFileMIMEType: "text/plain",
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "notes", "out.txt"))
	if err != nil {
		t.Fatalf("ReadFile(replayed output) error = %v", err)
	}
	if string(raw) != "hello file" {
		t.Fatalf("replayed file content = %q, want hello file", raw)
	}
	file, ok := results[0].Metadata[EffectReplayMetadataFile].(File)
	if !ok {
		t.Fatalf("file metadata = %#v, want sandbox.File", results[0].Metadata[EffectReplayMetadataFile])
	}
	if file.Path != "notes/out.txt" || file.MIMEType != "text/plain" || string(file.Content) != "hello file" {
		t.Fatalf("file metadata = %+v, want replayed file", file)
	}
}

func TestReplayHandlerReadsRecordedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "in.txt"), []byte("read me"), 0o644); err != nil {
		t.Fatalf("WriteFile(seed) error = %v", err)
	}
	manager, err := NewLocal(WithRoot(root))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-file-1",
					Type:           EffectTypeSandboxFileRead,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:file-read",
					Sandbox: &gopact.SandboxEffect{
						Operation: SandboxOperationReadFile,
						Path:      "notes/in.txt",
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	file, ok := results[0].Metadata[EffectReplayMetadataFile].(File)
	if !ok {
		t.Fatalf("file metadata = %#v, want sandbox.File", results[0].Metadata[EffectReplayMetadataFile])
	}
	if file.Path != "notes/in.txt" || string(file.Content) != "read me" {
		t.Fatalf("file metadata = %+v, want read file", file)
	}
}

func TestReplayHandlerRejectsWriteFileWithoutRecordedContent(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-file-1",
					Type:           EffectTypeSandboxFileWrite,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:file-write",
					Sandbox: &gopact.SandboxEffect{
						Operation: SandboxOperationWriteFile,
						Path:      "notes/out.txt",
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err = gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if !errors.Is(err, ErrReplayFileContentMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayFileContentMissing", err)
	}
}

func TestReplayHandlerRejectsMissingSandboxEffect(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-1",
					Type:           EffectTypeSandboxExec,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:missing",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err = gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if !errors.Is(err, ErrReplaySandboxMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplaySandboxMissing", err)
	}
}

func TestReplayHandlerRejectsNonSandboxExecEffect(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "effect-1",
					Type:           "tool_call",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:wrong-type",
					Sandbox: &gopact.SandboxEffect{
						Operation: SandboxOperationExec,
						Command:   []string{"echo", "hello"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err = gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if !errors.Is(err, ErrReplayEffectType) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayEffectType", err)
	}
}

func TestReplayHandlerRejectsUnsupportedOperation(t *testing.T) {
	ctx := context.Background()
	manager, err := NewLocal(WithRoot(t.TempDir()), WithAllowedCommands("echo"))
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "sandbox-1",
					Type:           EffectTypeSandboxExec,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "sandbox:unsupported",
					Sandbox: &gopact.SandboxEffect{
						Operation: "write_file",
						Path:      "out.txt",
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err = gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(manager))
	if !errors.Is(err, ErrReplayOperationUnsupported) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayOperationUnsupported", err)
	}
}
