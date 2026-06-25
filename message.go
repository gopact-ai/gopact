package gopact

import "strings"

// Role 标识 agent 对话中消息的来源。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 是 provider-neutral 的对话上下文单元。
type Message struct {
	Role       Role          `json:"role"`
	Content    string        `json:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

// Text 返回面向展示的文本内容。Parts 存在时不会混入旧 Content 字段。
func (m Message) Text() string {
	if len(m.Parts) == 0 {
		return m.Content
	}

	var b strings.Builder
	for _, part := range m.Parts {
		if part.Type == ContentPartText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// ToolCall 描述模型请求的一次工具调用。
type ToolCall struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Arguments []byte `json:"arguments,omitempty"`
}
