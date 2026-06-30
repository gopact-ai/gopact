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

func TestObjectToolSpecBuildsRequiredStringInputSchema(t *testing.T) {
	spec := ObjectToolSpec(
		"uppercase",
		"Uppercase a text string.",
		RequiredStringField("text", "Text to uppercase."),
		StringField("locale", "Locale hint."),
	)

	if spec.Name != "uppercase" || spec.Description != "Uppercase a text string." {
		t.Fatalf("tool = %+v, want name and description", spec)
	}
	if err := ValidateJSONSchemaValue(spec.InputSchema, map[string]any{"text": "gopact", "locale": "en"}); err != nil {
		t.Fatalf("ValidateJSONSchemaValue(valid) error = %v", err)
	}
	if err := ValidateJSONSchemaValue(spec.InputSchema, map[string]any{"text": "gopact"}); err != nil {
		t.Fatalf("ValidateJSONSchemaValue(without optional locale) error = %v", err)
	}
	if err := ValidateJSONSchemaValue(spec.InputSchema, map[string]any{}); err == nil {
		t.Fatal("ValidateJSONSchemaValue(missing text) error = nil, want required field error")
	}
}

func TestToolResultCarriesArtifacts(t *testing.T) {
	result := ToolResult{
		Content: "created",
		Artifacts: []ArtifactRef{
			{ID: "artifact-1", Scope: ArtifactScopeRun},
		},
	}

	if len(result.Artifacts) != 1 || result.Artifacts[0].ID != "artifact-1" {
		t.Fatalf("Artifacts = %+v", result.Artifacts)
	}
}

func TestToolResultOmitsEmptyCommit(t *testing.T) {
	data, err := json.Marshal(ToolResult{Content: "ok"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(data) != `{"content":"ok"}` {
		t.Fatalf("Marshal(ToolResult) = %s, want no empty commit object", data)
	}
}
