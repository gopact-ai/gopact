package gopact

import (
	"context"
	"encoding/json"
	"errors"
)

// JSONSchema 是工具和响应契约使用的可移植 schema 表示。
// Provider adapter 可以把它翻译成各自原生的工具 schema 格式。
type JSONSchema map[string]any

// ToolSpec 是暴露给模型的可调用能力描述。
type ToolSpec struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InputSchema JSONSchema `json:"input_schema,omitempty"`
}

// ToolField describes one object property in a tool input schema.
type ToolField struct {
	Name     string
	Schema   JSONSchema
	Required bool
}

// RequiredStringField creates a required string property for ObjectToolSpec.
func RequiredStringField(name, description string) ToolField {
	schema := JSONSchema{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return ToolField{Name: name, Schema: schema, Required: true}
}

// ObjectToolSpec creates a ToolSpec whose input schema is a JSON object.
func ObjectToolSpec(name, description string, fields ...ToolField) ToolSpec {
	properties := map[string]any{}
	var required []string
	for _, field := range fields {
		if field.Name == "" {
			continue
		}
		properties[field.Name] = copyJSONSchema(field.Schema)
		if field.Required {
			required = append(required, field.Name)
		}
	}
	schema := JSONSchema{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return ToolSpec{Name: name, Description: description, InputSchema: schema}
}

// ToolResult 是工具返回的标准化结果。
type ToolResult struct {
	Content   string         `json:"content,omitempty"`
	Artifacts []ArtifactRef  `json:"artifacts,omitempty"`
	Effects   []EffectRecord `json:"effects,omitempty"`
	Events    []Event        `json:"events,omitempty"`
	Commit    *ToolCommit    `json:"commit,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ToolCommit describes the default tool_call effect committed by a tool invocation.
type ToolCommit struct {
	ReplayPolicy   EffectReplayPolicy `json:"replay_policy,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	Metadata       map[string]any     `json:"metadata,omitempty"`
}

// Tool 是暴露给 agent 的核心能力接口。
type Tool interface {
	Spec(ctx context.Context) (ToolSpec, error)
	Invoke(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// ToolFunc 把普通 Go 函数适配成 Tool 实现。
type ToolFunc struct {
	SpecValue  ToolSpec
	InvokeFunc func(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// Spec 返回暴露给模型 provider 的工具 schema。
func (t ToolFunc) Spec(_ context.Context) (ToolSpec, error) {
	if t.SpecValue.Name == "" {
		return ToolSpec{}, errors.New("gopact: tool name is required")
	}
	return t.SpecValue, nil
}

// Invoke 执行被适配的函数。
func (t ToolFunc) Invoke(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.InvokeFunc == nil {
		return ToolResult{}, errors.New("gopact: tool invoke function is nil")
	}
	return t.InvokeFunc(ctx, args)
}
