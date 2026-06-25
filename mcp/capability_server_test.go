package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCapabilityServerHandlesSamplingCreateMessage(t *testing.T) {
	server := NewCapabilityServer(WithSamplingHandler(SamplingHandlerFunc(func(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
		if len(request.Messages) != 1 || request.Messages[0].Role != gopact.RoleUser || request.Messages[0].Text() != "ping" {
			t.Fatalf("messages = %+v, want user ping", request.Messages)
		}
		if request.MaxTokens != 32 || request.ToolChoice.Mode != ToolChoiceNone {
			t.Fatalf("sampling request = %+v, want max tokens 32 and no tools", request)
		}
		return SamplingResponse{
			Role:       gopact.RoleAssistant,
			Content:    []gopact.ContentPart{gopact.TextPart("pong")},
			Model:      "test-model",
			StopReason: "endTurn",
		}, nil
	})))

	response := callCapabilityServer(t, server, `{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{"messages":[{"role":"user","content":{"type":"text","text":"ping"}}],"maxTokens":32,"toolChoice":{"mode":"none"}}}`)

	if response.Error != nil {
		t.Fatalf("response error = %+v", response.Error)
	}
	var result struct {
		Role       string         `json:"role"`
		Content    mcpContentPart `json:"content"`
		Model      string         `json:"model"`
		StopReason string         `json:"stopReason"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("result decode error = %v", err)
	}
	if result.Role != "assistant" || result.Content.Text != "pong" || result.Model != "test-model" || result.StopReason != "endTurn" {
		t.Fatalf("result = %+v, want assistant pong", result)
	}
}

func TestCapabilityServerHandlesElicitationCreate(t *testing.T) {
	server := NewCapabilityServer(WithElicitationHandler(ElicitationHandlerFunc(func(ctx context.Context, request ElicitationRequest) (ElicitationResponse, error) {
		if request.Mode != ElicitationForm || request.Message != "GitHub username?" {
			t.Fatalf("elicitation request = %+v, want form message", request)
		}
		if request.RequestedSchema["type"] != "object" {
			t.Fatalf("requested schema = %+v, want object", request.RequestedSchema)
		}
		return ElicitationResponse{
			Action:  ElicitationAccept,
			Content: map[string]any{"name": "octocat"},
		}, nil
	})))

	response := callCapabilityServer(t, server, `{"jsonrpc":"2.0","id":"elicit","method":"elicitation/create","params":{"mode":"form","message":"GitHub username?","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}}}}}`)

	if response.Error != nil {
		t.Fatalf("response error = %+v", response.Error)
	}
	var result ElicitationResponse
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("result decode error = %v", err)
	}
	if result.Action != ElicitationAccept || result.Content["name"] != "octocat" {
		t.Fatalf("result = %+v, want accept octocat", result)
	}
}

func TestCapabilityServerHandlesElicitationCompleteNotification(t *testing.T) {
	var got ElicitationCompleteNotification
	server := NewCapabilityServer(WithElicitationCompleteHandler(ElicitationCompleteHandlerFunc(func(ctx context.Context, notification ElicitationCompleteNotification) error {
		got = notification
		return nil
	})))

	ok, err := server.HandleNotification(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/elicitation/complete","params":{"elicitationId":"elicit-1","_meta":{"source":"browser"}}}`))
	if err != nil {
		t.Fatalf("HandleNotification() error = %v", err)
	}
	if !ok {
		t.Fatalf("HandleNotification() ok = false, want true")
	}
	if got.ElicitationID != "elicit-1" || got.Metadata["source"] != "browser" {
		t.Fatalf("notification = %+v, want elicit-1 browser", got)
	}
}

func TestCapabilityServerUnsupportedCapabilityReturnsMethodNotFound(t *testing.T) {
	server := NewCapabilityServer()

	response := callCapabilityServer(t, server, `{"jsonrpc":"2.0","id":2,"method":"sampling/createMessage","params":{}}`)

	if response.Error == nil {
		t.Fatalf("response error = nil, want method not found")
	}
	if response.Error.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", response.Error.Code)
	}
	if !strings.Contains(response.Error.Message, "method not found") {
		t.Fatalf("error message = %q, want method not found", response.Error.Message)
	}
}

func callCapabilityServer(t *testing.T, server *CapabilityServer, request string) serverRPCResponse {
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
