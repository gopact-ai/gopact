package gopact

import (
	"encoding/json"
	"testing"
)

func TestMessageCloneIsolatesCanonicalToolTranscriptFacts(t *testing.T) {
	ref := &ArtifactRef{URI: "artifact://source"}
	call := ToolCall{
		ID:        "call-1",
		Name:      "search",
		Arguments: json.RawMessage(`{"query":"gopact"}`),
	}
	original := Message{
		Role:      MessageRoleAssistant,
		Parts:     []MessagePart{{Type: MessagePartTypeArtifact, Ref: ref}},
		ToolCalls: []ToolCall{call},
	}
	cloned := original.Clone()
	cloned.Parts[0].Ref.URI = "artifact://mutated"
	cloned.ToolCalls[0].ID = "mutated"
	cloned.ToolCalls[0].Arguments[0] = '['

	if original.Parts[0].Ref.URI != "artifact://source" || original.ToolCalls[0].ID != call.ID ||
		string(original.ToolCalls[0].Arguments) != `{"query":"gopact"}` {
		t.Fatalf("clone mutated original message: %+v", original)
	}
}

func TestMessageClonePreservesOwnedContainerNilness(t *testing.T) {
	empty := Message{
		Parts:     []MessagePart{},
		ToolCalls: []ToolCall{{Arguments: json.RawMessage{}}},
	}.Clone()
	if empty.Parts == nil || empty.ToolCalls == nil || empty.ToolCalls[0].Arguments == nil {
		t.Fatalf("clone collapsed non-nil empty containers: %+v", empty)
	}

	nilMessage := (Message{}).Clone()
	if nilMessage.Parts != nil || nilMessage.ToolCalls != nil {
		t.Fatalf("clone allocated nil containers: %+v", nilMessage)
	}
}
