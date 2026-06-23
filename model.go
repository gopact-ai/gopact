package gopact

import "context"

// ChatModel 是 agent 对模型 provider 的最小依赖契约。
type ChatModel interface {
	Generate(ctx context.Context, request ModelRequest) (Message, error)
}

// ModelRequest 向模型 provider 传递消息、工具 schema，以及可选的结构化输出提示。
type ModelRequest struct {
	Messages       []Message      `json:"messages"`
	Tools          []ToolSpec     `json:"tools,omitempty"`
	ResponseSchema JSONSchema     `json:"response_schema,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}
