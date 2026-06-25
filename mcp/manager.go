// Package mcp provides minimal MCP-like manager and client contracts.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/gopact-ai/gopact"
)

var ErrServerExists = errors.New("mcp: server already exists")

// Server exposes MCP-like tools, resources, and prompts.
type Server interface {
	Name() string
	Tools(ctx context.Context) ([]ToolInfo, error)
	Resources(ctx context.Context) ([]Resource, error)
	Prompts(ctx context.Context) ([]Prompt, error)
}

// ToolInfo is a namespaced tool descriptor.
type ToolInfo struct {
	Name        string
	Server      string
	Description string
	Schema      gopact.JSONSchema
	Metadata    map[string]any
}

// Resource is a namespaced MCP resource descriptor.
type Resource struct {
	URI      string
	Name     string
	Server   string
	MIMEType string
	Metadata map[string]any
}

// Prompt is a namespaced MCP prompt descriptor.
type Prompt struct {
	Name        string
	Server      string
	Description string
	Metadata    map[string]any
}

// Manager stores connected MCP servers.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]Server
}

// NewManager creates an empty MCP manager.
func NewManager() *Manager {
	return &Manager{servers: make(map[string]Server)}
}

func (m *Manager) Connect(ctx context.Context, server Server) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if server == nil {
		return errors.New("mcp: server is nil")
	}
	name := server.Name()
	if name == "" {
		return errors.New("mcp: server name is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.servers == nil {
		m.servers = make(map[string]Server)
	}
	if _, ok := m.servers[name]; ok {
		return fmt.Errorf("%w: %s", ErrServerExists, name)
	}
	m.servers[name] = server
	return nil
}

func (m *Manager) Tools(ctx context.Context) ([]ToolInfo, error) {
	servers := m.snapshot()
	var tools []ToolInfo
	for name, server := range servers {
		list, err := server.Tools(ctx)
		if err != nil {
			return nil, fmt.Errorf("mcp: list tools for %q: %w", name, err)
		}
		for _, tool := range list {
			tool.Server = name
			tool.Name = qualify(name, tool.Name)
			tools = append(tools, tool)
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func (m *Manager) Resources(ctx context.Context) ([]Resource, error) {
	servers := m.snapshot()
	var resources []Resource
	for name, server := range servers {
		list, err := server.Resources(ctx)
		if err != nil {
			return nil, fmt.Errorf("mcp: list resources for %q: %w", name, err)
		}
		for _, resource := range list {
			resource.Server = name
			resources = append(resources, resource)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].URI < resources[j].URI })
	return resources, nil
}

func (m *Manager) Prompts(ctx context.Context) ([]Prompt, error) {
	servers := m.snapshot()
	var prompts []Prompt
	for name, server := range servers {
		list, err := server.Prompts(ctx)
		if err != nil {
			return nil, fmt.Errorf("mcp: list prompts for %q: %w", name, err)
		}
		for _, prompt := range list {
			prompt.Server = name
			prompt.Name = qualify(name, prompt.Name)
			prompts = append(prompts, prompt)
		}
	}
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })
	return prompts, nil
}

func (m *Manager) snapshot() map[string]Server {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]Server, len(m.servers))
	for name, server := range m.servers {
		out[name] = server
	}
	return out
}

func qualify(namespace, name string) string {
	return namespace + "." + name
}

// FakeServer is an in-memory MCP server for tests.
type FakeServer struct {
	NameValue      string
	ToolsValue     []ToolInfo
	ResourcesValue []Resource
	PromptsValue   []Prompt
}

func (s FakeServer) Name() string { return s.NameValue }

func (s FakeServer) Tools(ctx context.Context) ([]ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]ToolInfo(nil), s.ToolsValue...), nil
}

func (s FakeServer) Resources(ctx context.Context) ([]Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]Resource(nil), s.ResourcesValue...), nil
}

func (s FakeServer) Prompts(ctx context.Context) ([]Prompt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]Prompt(nil), s.PromptsValue...), nil
}
