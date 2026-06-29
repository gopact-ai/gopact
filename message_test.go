package gopact

import "testing"

func TestMessageConstructors(t *testing.T) {
	tests := []struct {
		name       string
		message    Message
		wantRole   Role
		wantText   string
		wantToolID string
	}{
		{name: "generic", message: NewMessage(RoleSystem, "rules"), wantRole: RoleSystem, wantText: "rules"},
		{name: "system", message: SystemMessage("rules"), wantRole: RoleSystem, wantText: "rules"},
		{name: "user", message: UserMessage("hello"), wantRole: RoleUser, wantText: "hello"},
		{name: "assistant", message: AssistantMessage("hi"), wantRole: RoleAssistant, wantText: "hi"},
		{name: "tool", message: ToolMessage("call-1", "done"), wantRole: RoleTool, wantText: "done", wantToolID: "call-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.message.Role != tt.wantRole || tt.message.Text() != tt.wantText || tt.message.ToolCallID != tt.wantToolID {
				t.Fatalf("message = %+v", tt.message)
			}
		})
	}
}

func TestNewMessageFromTemplate(t *testing.T) {
	message, err := NewMessageFromTemplate(RoleSystem, "You are a {{.Style}} assistant.", map[string]string{
		"Style": "concise",
	})
	if err != nil {
		t.Fatalf("NewMessageFromTemplate() error = %v", err)
	}
	if message.Role != RoleSystem || message.Text() != "You are a concise assistant." {
		t.Fatalf("message = %+v", message)
	}
}

func TestNewMessageFromTemplateReturnsParseError(t *testing.T) {
	if _, err := NewMessageFromTemplate(RoleUser, "{{", nil); err == nil {
		t.Fatal("NewMessageFromTemplate() error = nil, want parse error")
	}
}

func TestNewMessageFromTemplateReturnsExecuteError(t *testing.T) {
	if _, err := NewMessageFromTemplate(RoleUser, "hello {{.Name}}", map[string]string{}); err == nil {
		t.Fatal("NewMessageFromTemplate() error = nil, want execute error")
	}
}

func TestMessageTextReturnsContentWhenPartsAreEmpty(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: "legacy content"}

	if got := msg.Text(); got != "legacy content" {
		t.Fatalf("Text() = %q, want legacy content", got)
	}
}

func TestMessageTextConcatenatesTextParts(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Parts: []ContentPart{
			TextPart("hello"),
			TextPart(" "),
			TextPart("world"),
		},
	}

	if got := msg.Text(); got != "hello world" {
		t.Fatalf("Text() = %q, want hello world", got)
	}
}

func TestMessageTextIgnoresNonTextParts(t *testing.T) {
	msg := Message{
		Content: "legacy fallback should not be mixed",
		Parts: []ContentPart{
			ImagePart("file://image.png", "image/png"),
			TextPart("visible"),
			ReasoningPart("hidden reasoning"),
		},
	}

	if got := msg.Text(); got != "visible" {
		t.Fatalf("Text() = %q, want only text parts", got)
	}
}

func TestMessageTextReturnsEmptyWhenPartsContainNoText(t *testing.T) {
	msg := Message{
		Content: "legacy fallback should not be used when parts are present",
		Parts: []ContentPart{
			ImagePart("file://image.png", "image/png"),
		},
	}

	if got := msg.Text(); got != "" {
		t.Fatalf("Text() = %q, want empty text", got)
	}
}
