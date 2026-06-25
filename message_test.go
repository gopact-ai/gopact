package gopact

import "testing"

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
