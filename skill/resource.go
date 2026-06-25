package skill

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// ResourceReader reads declared skill resources.
type ResourceReader interface {
	ReadResource(ctx context.Context, req ResourceReadRequest) (ResourceContent, error)
}

// ScriptRunner executes declared skill scripts through an injected runtime.
type ScriptRunner interface {
	RunScript(ctx context.Context, req ScriptRunRequest) (ScriptResult, error)
}

// ResourceReadRequest identifies a skill resource to read.
type ResourceReadRequest struct {
	SkillName string         `json:"skill_name,omitempty"`
	Resource  Resource       `json:"resource,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ResourceContent is the content returned by a skill resource reader.
type ResourceContent struct {
	SkillName string         `json:"skill_name,omitempty"`
	Resource  Resource       `json:"resource,omitempty"`
	URI       string         `json:"uri,omitempty"`
	MIMEType  string         `json:"mime_type,omitempty"`
	Text      string         `json:"text,omitempty"`
	Content   []byte         `json:"content,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ScriptRunRequest identifies a skill script execution.
type ScriptRunRequest struct {
	SkillName string            `json:"skill_name,omitempty"`
	Script    Script            `json:"script,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Stdin     []byte            `json:"stdin,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// ScriptResult is the provider-neutral result of a skill script execution.
type ScriptResult struct {
	SkillName  string               `json:"skill_name,omitempty"`
	ScriptName string               `json:"script_name,omitempty"`
	ExitCode   int                  `json:"exit_code,omitempty"`
	Stdout     []byte               `json:"stdout,omitempty"`
	Stderr     []byte               `json:"stderr,omitempty"`
	Artifacts  []gopact.ArtifactRef `json:"artifacts,omitempty"`
	Metadata   map[string]any       `json:"metadata,omitempty"`
}

// ResourceReadRecord records a fake skill resource read.
type ResourceReadRecord struct {
	Request ResourceReadRequest `json:"request,omitempty"`
}

// ScriptRunRecord records a fake skill script run.
type ScriptRunRecord struct {
	Request ScriptRunRequest `json:"request,omitempty"`
}

// FakeResourceReader is an in-memory resource reader for tests and examples.
type FakeResourceReader struct {
	Contents map[string]ResourceContent
	Reads    []ResourceReadRecord
}

// FakeScriptRunner is an in-memory script runner for tests and examples.
type FakeScriptRunner struct {
	Results map[string]ScriptResult
	Runs    []ScriptRunRecord
}

var (
	_ ResourceReader = (*FakeResourceReader)(nil)
	_ ScriptRunner   = (*FakeScriptRunner)(nil)
)

// ReadResource records and returns configured resource content.
func (r *FakeResourceReader) ReadResource(ctx context.Context, req ResourceReadRequest) (ResourceContent, error) {
	if err := ctx.Err(); err != nil {
		return ResourceContent{}, err
	}
	r.Reads = append(r.Reads, ResourceReadRecord{Request: copyResourceReadRequest(req)})
	key := resourceKey(req.Resource)
	content, ok := r.Contents[key]
	if !ok {
		return ResourceContent{}, fmt.Errorf("%w: resource %q", ErrNotFound, key)
	}
	return copyResourceContent(content), nil
}

// RunScript records and returns a configured script result.
func (r *FakeScriptRunner) RunScript(ctx context.Context, req ScriptRunRequest) (ScriptResult, error) {
	if err := ctx.Err(); err != nil {
		return ScriptResult{}, err
	}
	r.Runs = append(r.Runs, ScriptRunRecord{Request: copyScriptRunRequest(req)})
	key := scriptKey(req.Script)
	result, ok := r.Results[key]
	if !ok {
		return ScriptResult{}, fmt.Errorf("%w: script %q", ErrNotFound, key)
	}
	return copyScriptResult(result), nil
}

func resourceKey(resource Resource) string {
	if resource.URI != "" {
		return resource.URI
	}
	return resource.Name
}

func scriptKey(script Script) string {
	return script.Name
}

func copyResourceReadRequest(in ResourceReadRequest) ResourceReadRequest {
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyResourceContent(in ResourceContent) ResourceContent {
	in.Content = append([]byte(nil), in.Content...)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyScriptRunRequest(in ScriptRunRequest) ScriptRunRequest {
	in.Script.Command = append([]string(nil), in.Script.Command...)
	in.Args = append([]string(nil), in.Args...)
	in.Env = copyStringMap(in.Env)
	in.Stdin = append([]byte(nil), in.Stdin...)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyScriptResult(in ScriptResult) ScriptResult {
	in.Stdout = append([]byte(nil), in.Stdout...)
	in.Stderr = append([]byte(nil), in.Stderr...)
	in.Artifacts = append([]gopact.ArtifactRef(nil), in.Artifacts...)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
