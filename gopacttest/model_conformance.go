// Package gopacttest provides reusable conformance helpers for gopact extensions.
package gopacttest

import (
	"context"
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
	if _, err := model.Invoke(context.Background(), next); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
}
