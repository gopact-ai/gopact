package sandbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

const (
	// EffectTypeSandboxExec is the standard effect type for replayable sandbox command executions.
	EffectTypeSandboxExec = "sandbox_exec"
	// EffectTypeSandboxFileRead is the standard effect type for replayable sandbox file reads.
	EffectTypeSandboxFileRead = "sandbox_file_read"
	// EffectTypeSandboxFileWrite is the standard effect type for replayable sandbox file writes.
	EffectTypeSandboxFileWrite = "sandbox_file_write"

	// SandboxOperationExec is the sandbox operation name for command execution.
	SandboxOperationExec = "exec"
	// SandboxOperationReadFile is the sandbox operation name for reading a file.
	SandboxOperationReadFile = "read_file"
	// SandboxOperationWriteFile is the sandbox operation name for writing a file.
	SandboxOperationWriteFile = "write_file"

	// EffectReplayMetadataExecResult stores the ExecResult produced by a replayed sandbox command.
	EffectReplayMetadataExecResult = "sandbox_exec_result"
	// EffectReplayMetadataFile stores the File produced or written by sandbox file replay.
	EffectReplayMetadataFile = "sandbox_file"

	// EffectReplayMetadataSessionID stores the session ID used by sandbox replay.
	EffectReplayMetadataSessionID = "sandbox_session_id"

	// EffectMetadataFileContent stores the recorded content for replayable file writes.
	EffectMetadataFileContent = "sandbox_file_content"
	// EffectMetadataFileMIMEType stores the recorded MIME type for replayable file writes.
	EffectMetadataFileMIMEType = "sandbox_file_mime_type"
)

var (
	// ErrReplaySandboxMissing is returned when sandbox replay has no recorded effect.
	ErrReplaySandboxMissing = errors.New("sandbox: replay sandbox effect missing")
	// ErrReplayEffectType is returned when a replay effect type is not supported by sandbox replay.
	ErrReplayEffectType = errors.New("sandbox: replay effect type is not supported")
	// ErrReplayOperationUnsupported is returned when a sandbox replay operation is unsupported.
	ErrReplayOperationUnsupported = errors.New("sandbox: replay operation unsupported")
	// ErrReplayFilePathMissing is returned when file replay has no path.
	ErrReplayFilePathMissing = errors.New("sandbox: replay file path missing")
	// ErrReplayFileContentMissing is returned when file write replay has no content.
	ErrReplayFileContentMissing = errors.New("sandbox: replay file content missing")
)

// ReplayOption configures a sandbox replay handler.
type ReplayOption func(*ReplayHandler)

// ReplayHandler replays idempotent sandbox effects through a Manager.
type ReplayHandler struct {
	manager Manager
	spec    Spec
}

// NewReplayHandler creates an EffectReplayExecutor for sandbox effects.
func NewReplayHandler(manager Manager, opts ...ReplayOption) *ReplayHandler {
	handler := &ReplayHandler{manager: manager}
	for _, opt := range opts {
		if opt != nil {
			opt(handler)
		}
	}
	return handler
}

// WithReplaySpec sets the sandbox session spec used while replaying commands.
func WithReplaySpec(spec Spec) ReplayOption {
	return func(handler *ReplayHandler) {
		handler.spec = copySpec(spec)
	}
}

// ReplayEffect implements gopact.EffectReplayExecutor.
func (h *ReplayHandler) ReplayEffect(ctx context.Context, decision gopact.EffectReplayDecision) (gopact.EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if h == nil || h.manager == nil {
		return gopact.EffectReplayResult{}, errors.New("sandbox: replay manager is nil")
	}
	if decision.Action != gopact.EffectReplayActionReplay || decision.ReplayPolicy != gopact.EffectReplayIdempotent {
		return gopact.EffectReplayResult{}, fmt.Errorf("sandbox: effect %q is not an idempotent replay decision", decision.Effect.ID)
	}
	if !isSupportedReplayEffectType(decision.Effect.Type) {
		return gopact.EffectReplayResult{}, fmt.Errorf("%w: %q", ErrReplayEffectType, decision.Effect.Type)
	}
	if decision.Effect.Sandbox == nil {
		return gopact.EffectReplayResult{}, ErrReplaySandboxMissing
	}
	if err := validateReplayOperation(decision.Effect); err != nil {
		return gopact.EffectReplayResult{}, err
	}

	session, err := h.manager.Create(ctx, copySpec(h.spec))
	if err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("sandbox: replay create session: %w", err)
	}

	metadata, err := h.replay(ctx, session, decision.Effect)
	closeErr := session.Close(ctx)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if closeErr != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("sandbox: replay close session: %w", closeErr)
	}
	return gopact.EffectReplayResult{
		Metadata: metadata,
	}, nil
}

func (h *ReplayHandler) replay(ctx context.Context, session Session, effect gopact.EffectRecord) (map[string]any, error) {
	metadata := map[string]any{
		EffectReplayMetadataSessionID: session.ID(),
	}
	switch effect.Type {
	case EffectTypeSandboxExec:
		result, err := session.Exec(ctx, ExecRequest{
			Command:  append([]string(nil), effect.Sandbox.Command...),
			Metadata: copyAnyMap(effect.Sandbox.Metadata),
		})
		if err != nil {
			return nil, fmt.Errorf("sandbox: replay exec: %w", err)
		}
		metadata[EffectReplayMetadataExecResult] = result
		return metadata, nil
	case EffectTypeSandboxFileRead:
		file, err := session.ReadFile(ctx, effect.Sandbox.Path)
		if err != nil {
			return nil, fmt.Errorf("sandbox: replay read file: %w", err)
		}
		metadata[EffectReplayMetadataFile] = file
		return metadata, nil
	case EffectTypeSandboxFileWrite:
		file, err := replayFile(effect)
		if err != nil {
			return nil, err
		}
		if err := session.WriteFile(ctx, file); err != nil {
			return nil, fmt.Errorf("sandbox: replay write file: %w", err)
		}
		metadata[EffectReplayMetadataFile] = file
		return metadata, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrReplayEffectType, effect.Type)
	}
}

func validateReplayOperation(effect gopact.EffectRecord) error {
	switch effect.Type {
	case EffectTypeSandboxExec:
		if effect.Sandbox.Operation != SandboxOperationExec {
			return fmt.Errorf("%w: %q", ErrReplayOperationUnsupported, effect.Sandbox.Operation)
		}
		if len(effect.Sandbox.Command) == 0 {
			return ErrCommandDenied
		}
	case EffectTypeSandboxFileRead:
		if effect.Sandbox.Operation != SandboxOperationReadFile {
			return fmt.Errorf("%w: %q", ErrReplayOperationUnsupported, effect.Sandbox.Operation)
		}
		if effect.Sandbox.Path == "" {
			return ErrReplayFilePathMissing
		}
	case EffectTypeSandboxFileWrite:
		if effect.Sandbox.Operation != SandboxOperationWriteFile {
			return fmt.Errorf("%w: %q", ErrReplayOperationUnsupported, effect.Sandbox.Operation)
		}
		if effect.Sandbox.Path == "" {
			return ErrReplayFilePathMissing
		}
	default:
		return fmt.Errorf("%w: %q", ErrReplayEffectType, effect.Type)
	}
	return nil
}

func replayFile(effect gopact.EffectRecord) (File, error) {
	content, err := replayFileContent(effect.Metadata)
	if err != nil {
		return File{}, err
	}
	file := File{
		Path:     effect.Sandbox.Path,
		Content:  content,
		Metadata: copyAnyMap(effect.Sandbox.Metadata),
	}
	if mimeType, ok := effect.Metadata[EffectMetadataFileMIMEType].(string); ok {
		file.MIMEType = mimeType
	}
	return file, nil
}

func replayFileContent(metadata map[string]any) ([]byte, error) {
	raw, ok := metadata[EffectMetadataFileContent]
	if !ok {
		return nil, ErrReplayFileContentMissing
	}
	switch content := raw.(type) {
	case []byte:
		return append([]byte(nil), content...), nil
	case string:
		return []byte(content), nil
	default:
		return nil, fmt.Errorf("%w: got %T", ErrReplayFileContentMissing, raw)
	}
}

func isSupportedReplayEffectType(effectType string) bool {
	switch effectType {
	case EffectTypeSandboxExec, EffectTypeSandboxFileRead, EffectTypeSandboxFileWrite:
		return true
	default:
		return false
	}
}

func copySpec(spec Spec) Spec {
	spec.Env = copyMap(spec.Env)
	spec.Metadata = copyAnyMap(spec.Metadata)
	return spec
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
