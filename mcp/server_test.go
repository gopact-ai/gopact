package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/tools"
)

func TestToolServerHandlesToolsList(t *testing.T) {
	registry := tools.NewRegistry()
	registerMCPTestTool(t, registry, "repo", "status", tools.VisibleTool, "show status", func(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
		return gopact.ToolResult{Content: "clean"}, nil
	})
	registerMCPTestTool(t, registry, "repo", "apply_patch", tools.DeferredTool, "apply patch", func(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
		return gopact.ToolResult{Content: "patched"}, nil
	})
	server, err := NewToolServer(registry)
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	response := callToolServer(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	if response.ID != "1" {
		t.Fatalf("response id = %s, want 1", response.ID)
	}
	var result toolsListResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("result decode error = %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1 visible tool", len(result.Tools))
	}
	if result.Tools[0].Name != "repo.status" || result.Tools[0].Description != "show status" {
		t.Fatalf("tool = %+v, want repo.status", result.Tools[0])
	}
	if result.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("schema = %+v, want object", result.Tools[0].InputSchema)
	}
}

func TestToolServerHandlesToolsCallThroughRegistryMiddleware(t *testing.T) {
	registry := tools.NewRegistry(tools.WithToolMiddleware(func(c *gopact.ToolContext) error {
		c.Args = json.RawMessage(`{"text":"from middleware"}`)
		if err := c.Next(); err != nil {
			return err
		}
		c.Result.Metadata = map[string]any{"middleware": "after"}
		return nil
	}))
	registerMCPTestTool(t, registry, "repo", "echo", tools.VisibleTool, "echo input", func(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
		var input struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return gopact.ToolResult{}, err
		}
		return gopact.ToolResult{Content: input.Text}, nil
	})
	server, err := NewToolServer(registry)
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	response := callToolServer(t, server, `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"repo.echo","arguments":{"text":"original"}}}`)

	if response.Error != nil {
		t.Fatalf("response error = %+v", response.Error)
	}
	var result toolCallResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("result decode error = %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "from middleware" {
		t.Fatalf("content = %+v, want middleware text", result.Content)
	}
	if result.Meta["middleware"] != "after" {
		t.Fatalf("metadata = %+v, want middleware after", result.Meta)
	}
}

func TestToolServerRejectsDeferredToolCall(t *testing.T) {
	registry := tools.NewRegistry()
	registerMCPTestTool(t, registry, "repo", "apply_patch", tools.DeferredTool, "apply patch", func(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
		return gopact.ToolResult{Content: "patched"}, nil
	})
	server, err := NewToolServer(registry)
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	response := callToolServer(t, server, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"repo.apply_patch","arguments":{}}}`)

	if response.Error == nil {
		t.Fatalf("response error = nil, want error")
	}
	if response.Error.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", response.Error.Code)
	}
	if !strings.Contains(response.Error.Message, "tool is not model-visible") {
		t.Fatalf("error message = %q, want tool visibility error", response.Error.Message)
	}
}

func TestToolServerHandlesInitializeResourcesAndPrompts(t *testing.T) {
	server, err := NewToolServer(tools.NewRegistry(), WithToolServerInfo(PeerInfo{Name: "repo-tools", Version: "0.1.0"}))
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	initialize := callToolServer(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	var initResult InitializeResult
	if err := json.Unmarshal(initialize.Result, &initResult); err != nil {
		t.Fatalf("initialize result decode error = %v", err)
	}
	if initResult.ProtocolVersion != "2025-11-25" || initResult.ServerInfo.Name != "repo-tools" {
		t.Fatalf("initialize = %+v, want repo-tools", initResult)
	}
	if _, ok := initResult.Capabilities["tools"]; !ok {
		t.Fatalf("capabilities = %+v, want tools capability", initResult.Capabilities)
	}

	resources := callToolServer(t, server, `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)
	var resourcesResult resourcesListResult
	if err := json.Unmarshal(resources.Result, &resourcesResult); err != nil {
		t.Fatalf("resources result decode error = %v", err)
	}
	if len(resourcesResult.Resources) != 0 {
		t.Fatalf("resources = %+v, want empty", resourcesResult.Resources)
	}

	prompts := callToolServer(t, server, `{"jsonrpc":"2.0","id":3,"method":"prompts/list"}`)
	var promptsResult promptsListResult
	if err := json.Unmarshal(prompts.Result, &promptsResult); err != nil {
		t.Fatalf("prompts result decode error = %v", err)
	}
	if len(promptsResult.Prompts) != 0 {
		t.Fatalf("prompts = %+v, want empty", promptsResult.Prompts)
	}
}

func TestToolServerServeLineSkipsNotificationsAndWritesResponses(t *testing.T) {
	ctx := context.Background()
	registry := tools.NewRegistry()
	registerMCPTestTool(t, registry, "repo", "status", tools.VisibleTool, "show status", func(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
		return gopact.ToolResult{Content: "clean"}, nil
	})
	server, err := NewToolServer(registry)
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	input := strings.NewReader(
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n",
	)
	output := new(bytes.Buffer)
	if err := server.ServeLine(ctx, input, output); err != nil {
		t.Fatalf("ServeLine() error = %v", err)
	}

	scanner := bufio.NewScanner(output)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error = %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("response lines = %v, want one response", lines)
	}
	var response serverRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &response); err != nil {
		t.Fatalf("response decode error = %v", err)
	}
	if response.ID != "1" {
		t.Fatalf("response id = %s, want 1", response.ID)
	}
}

func TestNewToolServerRequiresRegistry(t *testing.T) {
	if _, err := NewToolServer(nil); !errors.Is(err, ErrToolRegistryRequired) {
		t.Fatalf("NewToolServer(nil) error = %v, want ErrToolRegistryRequired", err)
	}
}

func TestToolServerUnknownMethodReturnsJSONRPCError(t *testing.T) {
	server, err := NewToolServer(tools.NewRegistry())
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	response := callToolServer(t, server, `{"jsonrpc":"2.0","id":9,"method":"unknown/method"}`)

	if response.Error == nil || response.Error.Code != -32601 {
		t.Fatalf("response error = %+v, want method not found", response.Error)
	}
}

func registerMCPTestTool(
	t *testing.T,
	registry *tools.Registry,
	namespace string,
	name string,
	visibility tools.Visibility,
	description string,
	invoke func(context.Context, json.RawMessage) (gopact.ToolResult, error),
) {
	t.Helper()
	err := registry.Register(context.Background(), gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{
			Name:        name,
			Description: description,
			InputSchema: gopact.JSONSchema{"type": "object"},
		},
		InvokeFunc: invoke,
	}, tools.RegisterOptions{
		Namespace:  namespace,
		Visibility: visibility,
		Source:     tools.SourceLocal,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

func callToolServer(t *testing.T, server *ToolServer, request string) serverRPCResponse {
	t.Helper()
	raw, ok, err := server.Handle(context.Background(), json.RawMessage(request))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !ok {
		t.Fatalf("Handle() ok = false, want response")
	}
	var response serverRPCResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("response decode error = %v raw=%s", err, raw)
	}
	return response
}

type serverRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func (r *serverRPCResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *RPCError       `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.JSONRPC = raw.JSONRPC
	r.ID = string(raw.ID)
	r.Result = raw.Result
	r.Error = raw.Error
	return nil
}

func TestToolServerCopiesMutableMetadata(t *testing.T) {
	registry := tools.NewRegistry()
	registerMCPTestTool(t, registry, "repo", "echo", tools.VisibleTool, "echo input", func(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
		return gopact.ToolResult{Content: "ok", Metadata: map[string]any{"tags": []any{"a"}}}, nil
	})
	server, err := NewToolServer(registry)
	if err != nil {
		t.Fatalf("NewToolServer() error = %v", err)
	}

	first := callToolServer(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"repo.echo","arguments":{}}}`)
	var firstResult toolCallResult
	if err := json.Unmarshal(first.Result, &firstResult); err != nil {
		t.Fatalf("first result decode error = %v", err)
	}
	firstResult.Meta["changed"] = true

	second := callToolServer(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"repo.echo","arguments":{}}}`)
	var secondResult toolCallResult
	if err := json.Unmarshal(second.Result, &secondResult); err != nil {
		t.Fatalf("second result decode error = %v", err)
	}
	if _, ok := secondResult.Meta["changed"]; ok {
		t.Fatalf("metadata leaked mutation: %+v", secondResult.Meta)
	}
	if !reflect.DeepEqual(secondResult.Meta["tags"], []any{"a"}) {
		t.Fatalf("metadata tags = %+v, want [a]", secondResult.Meta["tags"])
	}
}
