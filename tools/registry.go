// Package tools provides the tool visibility registry.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrToolExists is returned when registering a duplicate tool name.
	ErrToolExists = errors.New("tools: tool already exists")
	// ErrToolNotFound is returned when a requested tool does not exist.
	ErrToolNotFound = errors.New("tools: tool not found")
	// ErrToolNotVisible is returned when a hidden tool is requested as model-visible.
	ErrToolNotVisible = errors.New("tools: tool is not model-visible")
)

// Visibility controls whether a tool is immediately visible to a model.
type Visibility string

const (
	// VisibleTool marks a tool as immediately available to model planning.
	VisibleTool Visibility = "visible"
	// DeferredTool marks a tool as hidden until promoted.
	DeferredTool Visibility = "deferred"
)

// Source records where a tool came from.
type Source string

const (
	// SourceLocal identifies a tool registered directly by the host.
	SourceLocal Source = "local"
	// SourceMCP identifies a tool discovered through MCP.
	SourceMCP Source = "mcp"
	// SourceA2A identifies a tool discovered through an A2A agent.
	SourceA2A Source = "a2a"
	// SourceSkill identifies a tool exposed by a skill.
	SourceSkill Source = "skill"
)

// Scope is reserved for per-run visibility decisions.
type Scope struct {
	IDs      gopact.RuntimeIDs
	Metadata map[string]any
}

// RegisterOptions configures tool registration.
type RegisterOptions struct {
	Namespace  string
	Visibility Visibility
	Source     Source
	Metadata   map[string]any
}

// SearchQuery searches visible and deferred tools.
type SearchQuery struct {
	Text  string
	Scope Scope
	Limit int
}

// ToolInfo is a model-facing tool descriptor.
type ToolInfo struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Description string            `json:"description,omitempty"`
	Schema      gopact.JSONSchema `json:"schema,omitempty"`
	Source      Source            `json:"source,omitempty"`
	Visibility  Visibility        `json:"visibility"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

type entry struct {
	tool gopact.Tool
	info ToolInfo
}

// Registry stores tool metadata and visibility.
type Registry struct {
	mu          sync.RWMutex
	tools       map[string]entry
	promoted    map[string]struct{}
	middlewares []gopact.ToolHandler
	pluginHost  *gopact.PluginHost
}

// RegistryOption configures a tool registry.
type RegistryOption func(*Registry)

// NewRegistry creates an empty tool registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	registry := &Registry{
		tools:    make(map[string]entry),
		promoted: make(map[string]struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(registry)
		}
	}
	return registry
}

// WithToolMiddleware wraps tool invocations made through the registry.
func WithToolMiddleware(middlewares ...gopact.ToolHandler) RegistryOption {
	return func(registry *Registry) {
		for _, middleware := range middlewares {
			if middleware != nil {
				registry.middlewares = append(registry.middlewares, middleware)
			}
		}
	}
}

// WithPluginHost attaches tool middleware installed by plugins.
func WithPluginHost(host *gopact.PluginHost) RegistryOption {
	return func(registry *Registry) {
		registry.pluginHost = host
	}
}

// Register registers a tool with visible or deferred visibility.
func (r *Registry) Register(ctx context.Context, tool gopact.Tool, opts RegisterOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tool == nil {
		return errors.New("tools: tool is nil")
	}
	if opts.Namespace == "" {
		return errors.New("tools: namespace is required")
	}
	if opts.Visibility != VisibleTool && opts.Visibility != DeferredTool {
		return errors.New("tools: visibility is invalid")
	}
	spec, err := tool.Spec(ctx)
	if err != nil {
		return fmt.Errorf("tools: tool spec: %w", err)
	}
	name := QualifiedName(opts.Namespace, spec.Name)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tools == nil {
		r.tools = make(map[string]entry)
	}
	if r.promoted == nil {
		r.promoted = make(map[string]struct{})
	}
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("%w: %s", ErrToolExists, name)
	}
	r.tools[name] = entry{
		tool: tool,
		info: ToolInfo{
			Name:        name,
			Namespace:   opts.Namespace,
			Description: spec.Description,
			Schema:      spec.InputSchema,
			Source:      opts.Source,
			Visibility:  opts.Visibility,
			Metadata:    opts.Metadata,
		},
	}
	return nil
}

// Visible returns current model-visible tools.
func (r *Registry) Visible(ctx context.Context, scope Scope) ([]ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = scope
	return r.filter(func(info ToolInfo) bool {
		return info.Visibility == VisibleTool || r.isPromoted(info.Name)
	}), nil
}

// Deferred returns tools that are not currently model-visible.
func (r *Registry) Deferred(ctx context.Context, scope Scope) ([]ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = scope
	return r.filter(func(info ToolInfo) bool {
		return info.Visibility == DeferredTool && !r.isPromoted(info.Name)
	}), nil
}

// Search searches name and description across visible and deferred tools.
func (r *Registry) Search(ctx context.Context, query SearchQuery) ([]ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text := strings.ToLower(strings.TrimSpace(query.Text))
	results := r.filter(func(info ToolInfo) bool {
		if text == "" {
			return true
		}
		return strings.Contains(strings.ToLower(info.Name), text) ||
			strings.Contains(strings.ToLower(info.Description), text)
	})
	if query.Limit > 0 && len(results) > query.Limit {
		results = results[:query.Limit]
	}
	return results, nil
}

// Promote makes deferred tools visible.
func (r *Registry) Promote(ctx context.Context, names []string, scope Scope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = scope
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.promoted == nil {
		r.promoted = make(map[string]struct{})
	}
	for _, name := range names {
		if _, ok := r.tools[name]; !ok {
			return fmt.Errorf("%w: %s", ErrToolNotFound, name)
		}
		r.promoted[name] = struct{}{}
	}
	return nil
}

// Invoke calls a registered tool through registry middleware.
func (r *Registry) Invoke(ctx context.Context, name string, args json.RawMessage, scope Scope) (gopact.ToolResult, error) {
	return r.invoke(ctx, name, args, scope, false)
}

// InvokeVisible calls a model-visible registered tool through registry middleware.
func (r *Registry) InvokeVisible(ctx context.Context, name string, args json.RawMessage, scope Scope) (gopact.ToolResult, error) {
	return r.invoke(ctx, name, args, scope, true)
}

func (r *Registry) invoke(ctx context.Context, name string, args json.RawMessage, scope Scope, requireVisible bool) (gopact.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ToolResult{}, err
	}
	if r == nil {
		return gopact.ToolResult{}, errors.New("tools: registry is nil")
	}

	r.mu.RLock()
	entry, ok := r.tools[name]
	visible := false
	if ok {
		visible = entry.info.Visibility == VisibleTool || r.isPromoted(name)
	}
	middlewares := r.toolMiddlewareChain()
	r.mu.RUnlock()
	if !ok {
		return gopact.ToolResult{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	if requireVisible && !visible {
		return gopact.ToolResult{}, fmt.Errorf("%w: %s", ErrToolNotVisible, name)
	}

	toolCtx := gopact.NewToolContext(ctx, gopact.ToolContextOptions{
		Name: name,
		Spec: gopact.ToolSpec{
			Name:        entry.info.Name,
			Description: entry.info.Description,
			InputSchema: entry.info.Schema,
		},
		IDs:      scope.IDs,
		Args:     args,
		Metadata: mergeMetadata(entry.info.Metadata, scope.Metadata),
	})
	final := func(c *gopact.ToolContext) error {
		result, err := entry.tool.Invoke(gopact.ContextWithRuntimeIDs(c.Context, c.IDs), c.Args)
		if err != nil {
			return err
		}
		c.Result = result
		return nil
	}
	handler := gopact.ComposeToolHandler(final, middlewares...)
	if err := handler(toolCtx); err != nil {
		toolCtx.Result.Effects = append(toolCtx.Result.Effects, toolCtx.Effects...)
		toolCtx.Result.Events = append(toolCtx.Result.Events, toolCtx.Events...)
		return toolCtx.Result, err
	}
	effect, err := toolEffect(name, entry.info, toolCtx.Args, scope, toolCtx.Result)
	if err != nil {
		return toolCtx.Result, err
	}
	toolCtx.Result.Effects = append(toolCtx.Result.Effects, effect)
	toolCtx.Result.Effects = append(toolCtx.Result.Effects, toolCtx.Effects...)
	toolCtx.Result.Events = append(toolCtx.Result.Events, toolCtx.Events...)
	return toolCtx.Result, nil
}

func toolEffect(name string, info ToolInfo, args json.RawMessage, scope Scope, result gopact.ToolResult) (gopact.EffectRecord, error) {
	metadata := map[string]any{
		"source":     info.Source,
		"visibility": info.Visibility,
	}
	if len(info.Metadata) > 0 {
		metadata["tool_metadata"] = copyMetadata(info.Metadata)
	}

	commit := result.Commit
	replayPolicy := gopact.EffectReplayPolicy("")
	if commit != nil {
		replayPolicy = commit.ReplayPolicy
	}
	if replayPolicy == "" {
		replayPolicy = gopact.EffectReplayRecordOnly
		if commit != nil && commit.IdempotencyKey != "" {
			replayPolicy = gopact.EffectReplayIdempotent
		}
	}
	if replayPolicy == gopact.EffectReplayIdempotent {
		if commit == nil || commit.IdempotencyKey == "" {
			return gopact.EffectRecord{}, errors.New("tools: idempotent tool commit requires idempotency key")
		}
		metadata[EffectMetadataToolArgs] = replayableToolArgs(args)
	}
	idempotencyKey := ""
	if commit != nil {
		idempotencyKey = commit.IdempotencyKey
		if len(commit.Metadata) > 0 {
			metadata["tool_commit_metadata"] = copyMetadata(commit.Metadata)
		}
	}

	return gopact.EffectRecord{
		ID:             scope.IDs.CallID,
		Type:           EffectTypeToolCall,
		Target:         name,
		Applied:        true,
		ReplayPolicy:   replayPolicy,
		IdempotencyKey: idempotencyKey,
		Metadata:       metadata,
	}, nil
}

func replayableToolArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), args...)
}

func copyMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeMetadata(base map[string]any, overlays ...map[string]any) map[string]any {
	out := copyMetadata(base)
	for _, overlay := range overlays {
		if len(overlay) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(overlay))
		}
		for key, value := range overlay {
			out[key] = value
		}
	}
	return out
}

func (r *Registry) toolMiddlewareChain() []gopact.ToolHandler {
	var middlewares []gopact.ToolHandler
	middlewares = append(middlewares, r.middlewares...)
	if r.pluginHost != nil {
		middlewares = append(middlewares, r.pluginHost.ToolMiddlewares()...)
	}
	return middlewares
}

// QualifiedName returns the registry-visible tool name.
func QualifiedName(namespace, name string) string {
	return namespace + "." + name
}

func (r *Registry) filter(keep func(ToolInfo) bool) []ToolInfo {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]ToolInfo, 0, len(r.tools))
	for _, entry := range r.tools {
		if keep(entry.info) {
			infos = append(infos, entry.info)
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}

func (r *Registry) isPromoted(name string) bool {
	_, ok := r.promoted[name]
	return ok
}
