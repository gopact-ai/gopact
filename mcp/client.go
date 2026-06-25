package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

var ErrCapabilityNotFound = errors.New("mcp: capability not found")

// Client is the minimal MCP client contract used by adapters and policy wrappers.
type Client interface {
	Tools(ctx context.Context) ([]ToolInfo, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
	Resources(ctx context.Context) ([]Resource, error)
	ReadResource(ctx context.Context, uri string) (ResourceContent, error)
	Prompts(ctx context.Context) ([]Prompt, error)
	GetPrompt(ctx context.Context, name string, args map[string]any) (PromptContent, error)
}

// ToolResult is the provider-neutral result of an MCP tool call.
type ToolResult struct {
	Name      string               `json:"name,omitempty"`
	Content   []gopact.ContentPart `json:"content,omitempty"`
	Artifacts []gopact.ArtifactRef `json:"artifacts,omitempty"`
	Metadata  map[string]any       `json:"metadata,omitempty"`
}

// ResourceContent is the content returned by reading an MCP resource.
type ResourceContent struct {
	URI      string         `json:"uri,omitempty"`
	MIMEType string         `json:"mime_type,omitempty"`
	Text     string         `json:"text,omitempty"`
	Content  []byte         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PromptContent is the prompt payload returned by an MCP prompt.
type PromptContent struct {
	Name     string           `json:"name,omitempty"`
	Messages []gopact.Message `json:"messages,omitempty"`
	Metadata map[string]any   `json:"metadata,omitempty"`
}

// ToolCallRecord records a fake MCP tool call.
type ToolCallRecord struct {
	Name string          `json:"name,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
}

// PromptGetRecord records a fake MCP prompt get.
type PromptGetRecord struct {
	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`
}

// FakeClient is an in-memory MCP client for tests and local examples.
type FakeClient struct {
	ToolsValue       []ToolInfo
	ResourcesValue   []Resource
	PromptsValue     []Prompt
	ToolResults      map[string]ToolResult
	ResourceContents map[string]ResourceContent
	PromptContents   map[string]PromptContent
	ToolCalls        []ToolCallRecord
	ResourceReads    []string
	PromptGets       []PromptGetRecord
}

var _ Client = (*FakeClient)(nil)

// Tools returns configured tool descriptors.
func (c *FakeClient) Tools(ctx context.Context) ([]ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return copyToolInfos(c.ToolsValue), nil
}

// CallTool records and returns a configured tool result.
func (c *FakeClient) CallTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	c.ToolCalls = append(c.ToolCalls, ToolCallRecord{Name: name, Args: append(json.RawMessage(nil), args...)})
	result, ok := c.ToolResults[name]
	if !ok {
		return ToolResult{}, fmt.Errorf("%w: tool %q", ErrCapabilityNotFound, name)
	}
	return copyToolResult(result), nil
}

// Resources returns configured resource descriptors.
func (c *FakeClient) Resources(ctx context.Context) ([]Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return copyResources(c.ResourcesValue), nil
}

// ReadResource records and returns configured resource content.
func (c *FakeClient) ReadResource(ctx context.Context, uri string) (ResourceContent, error) {
	if err := ctx.Err(); err != nil {
		return ResourceContent{}, err
	}
	c.ResourceReads = append(c.ResourceReads, uri)
	content, ok := c.ResourceContents[uri]
	if !ok {
		return ResourceContent{}, fmt.Errorf("%w: resource %q", ErrCapabilityNotFound, uri)
	}
	return copyResourceContent(content), nil
}

// Prompts returns configured prompt descriptors.
func (c *FakeClient) Prompts(ctx context.Context) ([]Prompt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return copyPrompts(c.PromptsValue), nil
}

// GetPrompt records and returns configured prompt content.
func (c *FakeClient) GetPrompt(ctx context.Context, name string, args map[string]any) (PromptContent, error) {
	if err := ctx.Err(); err != nil {
		return PromptContent{}, err
	}
	c.PromptGets = append(c.PromptGets, PromptGetRecord{Name: name, Args: copyAnyMap(args)})
	content, ok := c.PromptContents[name]
	if !ok {
		return PromptContent{}, fmt.Errorf("%w: prompt %q", ErrCapabilityNotFound, name)
	}
	return copyPromptContent(content), nil
}

func copyToolInfos(in []ToolInfo) []ToolInfo {
	out := append([]ToolInfo(nil), in...)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}

func copyResources(in []Resource) []Resource {
	out := append([]Resource(nil), in...)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}

func copyPrompts(in []Prompt) []Prompt {
	out := append([]Prompt(nil), in...)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}

func copyToolResult(in ToolResult) ToolResult {
	in.Content = copyContentParts(in.Content)
	in.Artifacts = append([]gopact.ArtifactRef(nil), in.Artifacts...)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyResourceContent(in ResourceContent) ResourceContent {
	in.Content = append([]byte(nil), in.Content...)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyPromptContent(in PromptContent) PromptContent {
	in.Messages = append([]gopact.Message(nil), in.Messages...)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyContentParts(in []gopact.ContentPart) []gopact.ContentPart {
	out := append([]gopact.ContentPart(nil), in...)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}
