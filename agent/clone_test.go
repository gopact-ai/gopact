package agent

import (
	"encoding/json"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRequestCloneIsolatesOwnedValues(t *testing.T) {
	request := Request{
		Messages: []gopact.Message{{
			Parts: []gopact.MessagePart{{
				Ref: &gopact.ArtifactRef{URI: "artifact://message"},
			}},
			ToolCalls: []gopact.ToolCall{{
				Arguments: json.RawMessage(`{"value":"original"}`),
			}},
		}},
		Artifacts: []gopact.ArtifactRef{{URI: "artifact://request"}},
		Metadata:  map[string]string{"key": "original"},
	}

	cloned := request.Clone()
	cloned.Messages[0].Parts[0].Ref.URI = "artifact://changed"
	cloned.Messages[0].ToolCalls[0].Arguments[0] = '['
	cloned.Artifacts[0].URI = "artifact://changed"
	cloned.Metadata["key"] = "changed"

	if request.Messages[0].Parts[0].Ref.URI != "artifact://message" ||
		string(request.Messages[0].ToolCalls[0].Arguments) != `{"value":"original"}` ||
		request.Artifacts[0].URI != "artifact://request" ||
		request.Metadata["key"] != "original" {
		t.Fatalf("clone mutated original request: %+v", request)
	}
}

func TestResponseCloneIsolatesOwnedValues(t *testing.T) {
	response := Response{
		Message: gopact.Message{
			Parts: []gopact.MessagePart{{
				Ref: &gopact.ArtifactRef{URI: "artifact://message"},
			}},
		},
		Artifacts: []gopact.ArtifactRef{{URI: "artifact://response"}},
		Metadata:  map[string]string{"key": "original"},
	}

	cloned := response.Clone()
	cloned.Message.Parts[0].Ref.URI = "artifact://changed"
	cloned.Artifacts[0].URI = "artifact://changed"
	cloned.Metadata["key"] = "changed"

	if response.Message.Parts[0].Ref.URI != "artifact://message" ||
		response.Artifacts[0].URI != "artifact://response" ||
		response.Metadata["key"] != "original" {
		t.Fatalf("clone mutated original response: %+v", response)
	}
}

func TestAgentClonesPreserveOwnedContainerNilness(t *testing.T) {
	request := Request{
		Messages:  []gopact.Message{},
		Artifacts: []gopact.ArtifactRef{},
		Metadata:  map[string]string{},
	}.Clone()
	if request.Messages == nil || request.Artifacts == nil || request.Metadata == nil {
		t.Fatalf("request clone collapsed non-nil empty containers: %+v", request)
	}

	response := Response{
		Message: gopact.Message{
			Parts:     []gopact.MessagePart{},
			ToolCalls: []gopact.ToolCall{},
		},
		Artifacts: []gopact.ArtifactRef{},
		Metadata:  map[string]string{},
	}.Clone()
	if response.Message.Parts == nil || response.Message.ToolCalls == nil ||
		response.Artifacts == nil || response.Metadata == nil {
		t.Fatalf("response clone collapsed non-nil empty containers: %+v", response)
	}

	nilRequest := (Request{}).Clone()
	if nilRequest.Messages != nil || nilRequest.Artifacts != nil || nilRequest.Metadata != nil {
		t.Fatalf("request clone allocated nil containers: %+v", nilRequest)
	}
	nilResponse := (Response{}).Clone()
	if nilResponse.Message.Parts != nil || nilResponse.Message.ToolCalls != nil ||
		nilResponse.Artifacts != nil || nilResponse.Metadata != nil {
		t.Fatalf("response clone allocated nil containers: %+v", nilResponse)
	}
}
