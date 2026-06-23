package gopact

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolFuncInvokesAndExposesSpec(t *testing.T) {
	ctx := context.Background()
	tool := ToolFunc{
		SpecValue: ToolSpec{
			Name:        "echo",
			Description: "returns the input text",
			InputSchema: JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
		InvokeFunc: func(ctx context.Context, args json.RawMessage) (ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: input.Text}, nil
		},
	}

	spec, err := tool.Spec(ctx)
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}
	if spec.Name != "echo" {
		t.Fatalf("Spec().Name = %q, want echo", spec.Name)
	}

	result, err := tool.Invoke(ctx, json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("Invoke().Content = %q, want hello", result.Content)
	}
}
