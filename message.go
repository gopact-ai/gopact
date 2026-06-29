package gopact

import (
	"strings"
	"text/template"
)

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

// NewMessage creates a text message for the given role.
func NewMessage(role Role, content string) Message {
	return Message{Role: role, Content: content}
}

// SystemMessage creates a system text message.
func SystemMessage(content string) Message {
	return NewMessage(RoleSystem, content)
}

// UserMessage creates a user text message.
func UserMessage(content string) Message {
	return NewMessage(RoleUser, content)
}

// AssistantMessage creates an assistant text message.
func AssistantMessage(content string) Message {
	return NewMessage(RoleAssistant, content)
}

// ToolMessage creates a tool text message tied to a tool call.
func ToolMessage(toolCallID, content string) Message {
	message := NewMessage(RoleTool, content)
	message.ToolCallID = toolCallID
	return message
}

// NewMessageFromTemplate renders a text/template into a message.
func NewMessageFromTemplate(role Role, text string, data any) (Message, error) {
	tmpl, err := template.New("message").Option("missingkey=error").Parse(text)
	if err != nil {
		return Message{}, err
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return Message{}, err
	}
	return NewMessage(role, b.String()), nil
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
