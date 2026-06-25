package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestManagerConnectAndListNamespacedCapabilities(t *testing.T) {
	ctx := context.Background()
	manager := NewManager()
	server := FakeServer{
		NameValue: "git",
		ToolsValue: []ToolInfo{
			{Name: "status", Description: "shows status", Schema: gopact.JSONSchema{"type": "object"}},
		},
		ResourcesValue: []Resource{{URI: "repo://README.md", Name: "README"}},
		PromptsValue:   []Prompt{{Name: "review", Description: "review prompt"}},
	}

	if err := manager.Connect(ctx, server); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	tools, err := manager.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if got := toolNames(tools); !reflect.DeepEqual(got, []string{"git.status"}) {
		t.Fatalf("Tools() names = %v, want [git.status]", got)
	}

	resources, err := manager.Resources(ctx)
	if err != nil {
		t.Fatalf("Resources() error = %v", err)
	}
	if len(resources) != 1 || resources[0].Server != "git" {
		t.Fatalf("Resources() = %+v", resources)
	}

	prompts, err := manager.Prompts(ctx)
	if err != nil {
		t.Fatalf("Prompts() error = %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "git.review" {
		t.Fatalf("Prompts() = %+v", prompts)
	}
}

func TestManagerRejectsInvalidServer(t *testing.T) {
	ctx := context.Background()
	manager := NewManager()

	if err := manager.Connect(ctx, nil); err == nil {
		t.Fatal("Connect() error = nil, want nil server error")
	}
	if err := manager.Connect(ctx, FakeServer{}); err == nil {
		t.Fatal("Connect() error = nil, want missing name error")
	}
}

func TestManagerRejectsDuplicateServer(t *testing.T) {
	ctx := context.Background()
	manager := NewManager()
	server := FakeServer{NameValue: "git"}

	if err := manager.Connect(ctx, server); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	err := manager.Connect(ctx, server)
	if !errors.Is(err, ErrServerExists) {
		t.Fatalf("Connect() error = %v, want %v", err, ErrServerExists)
	}
}

func toolNames(tools []ToolInfo) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
