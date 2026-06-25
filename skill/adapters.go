package skill

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/gopact-ai/gopact/sandbox"
)

var (
	// ErrResourceRootRequired is returned when a file resource reader has no root directory.
	ErrResourceRootRequired = errors.New("skill: resource root is required")
	// ErrResourcePathEscape is returned when a resource path escapes the configured root.
	ErrResourcePathEscape = errors.New("skill: resource path escapes root")
	// ErrSandboxManagerRequired is returned when script execution has no sandbox manager.
	ErrSandboxManagerRequired = errors.New("skill: sandbox manager is required")
)

// FileResourceReader reads skill resources from a local root directory.
type FileResourceReader struct {
	root string
}

// NewFileResourceReader creates a local filesystem skill resource reader.
func NewFileResourceReader(root string) (*FileResourceReader, error) {
	if root == "" {
		return nil, ErrResourceRootRequired
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("skill: resolve resource root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("skill: resolve resource root symlinks: %w", err)
	}
	return &FileResourceReader{root: filepath.Clean(resolved)}, nil
}

var _ ResourceReader = (*FileResourceReader)(nil)

// ReadResource reads a declared resource from the configured root.
func (r *FileResourceReader) ReadResource(ctx context.Context, req ResourceReadRequest) (ResourceContent, error) {
	if err := ctx.Err(); err != nil {
		return ResourceContent{}, err
	}
	resolved, err := r.resolve(resourceKey(req.Resource))
	if err != nil {
		return ResourceContent{}, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return ResourceContent{}, fmt.Errorf("%w: resource %q", ErrNotFound, resourceKey(req.Resource))
		}
		return ResourceContent{}, fmt.Errorf("skill: read resource: %w", err)
	}
	mimeType := req.Resource.MIMEType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(resolved))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	result := ResourceContent{
		SkillName: req.SkillName,
		Resource:  req.Resource,
		URI:       resourceKey(req.Resource),
		MIMEType:  mimeType,
		Content:   append([]byte(nil), content...),
		Metadata:  copyAnyMap(req.Metadata),
	}
	if isTextResource(mimeType) {
		result.Text = string(content)
	}
	return result, nil
}

func (r *FileResourceReader) resolve(uri string) (string, error) {
	if uri == "" {
		return "", ErrResourcePathEscape
	}
	candidate := uri
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.root, candidate)
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("skill: resolve resource: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if resolved != r.root && !strings.HasPrefix(resolved, r.root+string(os.PathSeparator)) {
		return "", ErrResourcePathEscape
	}
	evaluated, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return resolved, nil
		}
		return "", fmt.Errorf("skill: resolve resource symlinks: %w", err)
	}
	evaluated = filepath.Clean(evaluated)
	if evaluated != r.root && !strings.HasPrefix(evaluated, r.root+string(os.PathSeparator)) {
		return "", ErrResourcePathEscape
	}
	return evaluated, nil
}

func isTextResource(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		strings.HasSuffix(mimeType, "+json") ||
		strings.HasSuffix(mimeType, "+xml")
}

// SandboxScriptRunner executes skill scripts through a sandbox manager.
type SandboxScriptRunner struct {
	manager sandbox.Manager
	spec    sandbox.Spec
}

// SandboxScriptRunnerOption configures SandboxScriptRunner.
type SandboxScriptRunnerOption func(*SandboxScriptRunner)

// WithSandboxSpec sets the base sandbox session spec for script runs.
func WithSandboxSpec(spec sandbox.Spec) SandboxScriptRunnerOption {
	return func(r *SandboxScriptRunner) {
		r.spec = copySandboxSpec(spec)
	}
}

// NewSandboxScriptRunner creates a sandbox-backed skill script runner.
func NewSandboxScriptRunner(manager sandbox.Manager, opts ...SandboxScriptRunnerOption) (*SandboxScriptRunner, error) {
	if manager == nil {
		return nil, ErrSandboxManagerRequired
	}
	runner := &SandboxScriptRunner{manager: manager}
	for _, opt := range opts {
		if opt != nil {
			opt(runner)
		}
	}
	return runner, nil
}

var _ ScriptRunner = (*SandboxScriptRunner)(nil)

// RunScript creates a sandbox session, executes the script, and closes the session.
func (r *SandboxScriptRunner) RunScript(ctx context.Context, req ScriptRunRequest) (ScriptResult, error) {
	if err := ctx.Err(); err != nil {
		return ScriptResult{}, err
	}
	spec := copySandboxSpec(r.spec)
	spec.Env = mergeStringMaps(spec.Env, req.Env)
	session, err := r.manager.Create(ctx, spec)
	if err != nil {
		return ScriptResult{}, fmt.Errorf("skill: create sandbox session: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = session.Close(context.WithoutCancel(ctx))
		}
	}()

	command := append([]string(nil), req.Script.Command...)
	command = append(command, req.Args...)
	result, err := session.Exec(ctx, sandbox.ExecRequest{
		Command:  command,
		Stdin:    append([]byte(nil), req.Stdin...),
		Metadata: copyAnyMap(req.Metadata),
	})
	closeErr := session.Close(context.WithoutCancel(ctx))
	closed = true
	if err != nil {
		if closeErr != nil {
			return ScriptResult{}, errors.Join(fmt.Errorf("skill: run script: %w", err), fmt.Errorf("skill: close sandbox session: %w", closeErr))
		}
		return ScriptResult{}, fmt.Errorf("skill: run script: %w", err)
	}
	if closeErr != nil {
		return ScriptResult{}, fmt.Errorf("skill: close sandbox session: %w", closeErr)
	}
	metadata := copyAnyMap(req.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 1)
	}
	metadata["sandbox_session_id"] = session.ID()
	return ScriptResult{
		SkillName:  req.SkillName,
		ScriptName: req.Script.Name,
		ExitCode:   result.ExitCode,
		Stdout:     append([]byte(nil), result.Stdout...),
		Stderr:     append([]byte(nil), result.Stderr...),
		Metadata:   metadata,
	}, nil
}

func copySandboxSpec(in sandbox.Spec) sandbox.Spec {
	in.Env = copyStringMap(in.Env)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		out[key] = value
	}
	return out
}
