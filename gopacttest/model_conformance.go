// Package gopacttest provides reusable conformance helpers for gopact extensions.
package gopacttest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gopact-ai/gopact"
)

// RequireModelConformance verifies the minimal core Model contract.
func RequireModelConformance(t *testing.T, model gopact.Model) {
	t.Helper()
	if model == nil {
		t.Fatal("model is nil")
	}
	req := model.NewRequest(gopact.UserMessage("hello"))
	if len(req.Messages) != 1 {
		t.Fatalf("NewRequest messages = %d, want 1", len(req.Messages))
	}
	req.Messages[0].Parts[0].Text = "mutated"
	next := model.NewRequest(gopact.UserMessage("hello"))
	if len(next.Messages) != 1 || next.Messages[0].Parts[0].Text != "hello" {
		t.Fatalf("NewRequest did not isolate message mutation: %+v", next.Messages)
	}
	toolMessage := gopact.Message{
		Role: gopact.MessageRoleAssistant,
		ToolCalls: []gopact.ToolCall{{
			ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"query":"hello"}`),
		}},
	}
	toolRequest := model.NewRequest(toolMessage)
	if len(toolRequest.Messages) != 1 || len(toolRequest.Messages[0].ToolCalls) != 1 {
		t.Fatalf("NewRequest tool calls = %+v, want one call", toolRequest.Messages)
	}
	toolRequest.Messages[0].ToolCalls[0].ID = "mutated"
	toolRequest.Messages[0].ToolCalls[0].Arguments[0] = '['
	isolated := model.NewRequest(toolMessage)
	if len(isolated.Messages) != 1 || len(isolated.Messages[0].ToolCalls) != 1 ||
		isolated.Messages[0].ToolCalls[0].ID != "call-1" ||
		string(isolated.Messages[0].ToolCalls[0].Arguments) != `{"query":"hello"}` {
		t.Fatalf("NewRequest did not isolate tool call mutation: %+v", isolated.Messages)
	}
	if _, err := model.Invoke(context.Background(), next); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
}
