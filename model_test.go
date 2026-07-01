package gopact

import (
	"context"
	"iter"
	"testing"
)

func TestAdaptResponseModelReturnsChatMessage(t *testing.T) {
	model := &responseModelStub{
		response: ModelResponse{
			Message: Message{Role: RoleAssistant, Content: "hello"},
		},
	}
	chat := AdaptResponseModel(model)

	request := NewModelRequest(
		WithMessages(Message{Role: RoleUser, Content: "hi"}),
		WithModel("default-model"),
		WithMaxOutputTokens(7),
	)
	message, err := chat.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if message.Role != RoleAssistant || message.Text() != "hello" {
		t.Fatalf("message = %+v, want assistant hello", message)
	}
	if len(model.requests) != 1 || model.requests[0].Messages[0].Text() != "hi" {
		t.Fatalf("requests = %+v, want forwarded request", model.requests)
	}
	if model.requests[0].Model != "default-model" || model.requests[0].Budget.MaxOutputTokens != 7 {
		t.Fatalf("request options = %+v, want model and max output tokens", model.requests[0])
	}
}

func TestAdaptStreamingModelPreservesStream(t *testing.T) {
	wantMessage := Message{Role: RoleAssistant, Content: "streamed"}
	model := &responseModelStub{
		response: ModelResponse{Message: wantMessage},
		events: []Event{
			{Type: EventModelRoutePlanned},
			{Type: EventModelMessage, Message: &wantMessage},
		},
	}
	chat := AdaptStreamingModel(model)
	streamer, ok := chat.(StreamingModel)
	if !ok {
		t.Fatalf("AdaptStreamingModel() = %T, want StreamingModel", chat)
	}

	var got []Event
	request := NewModelRequest(WithModel("stream-model"), EnableStreaming())
	for event, err := range streamer.Stream(context.Background(), request) {
		if err != nil {
			t.Fatalf("Stream() error = %v", err)
		}
		got = append(got, event)
	}
	if len(got) != 2 {
		t.Fatalf("streamed events = %d, want 2", len(got))
	}
	if got[0].Type != EventModelRoutePlanned || got[1].Type != EventModelMessage {
		t.Fatalf("streamed event types = %s/%s", got[0].Type, got[1].Type)
	}
	if len(model.requests) != 1 || model.requests[0].Model != "stream-model" || len(model.requests[0].Capabilities) != 1 || model.requests[0].Capabilities[0] != CapabilityStreaming {
		t.Fatalf("stream request = %+v, want stream model and streaming capability", model.requests)
	}

	message, err := chat.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if message.Text() != "streamed" {
		t.Fatalf("Generate() message = %+v, want streamed", message)
	}
}

func TestNewModelRequestAppliesOptionsWithoutAliasingMutableFields(t *testing.T) {
	metadata := map[string]any{"trace": "original"}
	messages := []Message{{
		Role:  RoleUser,
		Parts: []ContentPart{TextPart("hi")},
	}}
	tools := []ToolSpec{{
		Name:        "lookup",
		InputSchema: JSONSchema{"type": "object"},
	}}

	request := NewModelRequest(
		WithMessages(messages...),
		WithTools(tools...),
		WithModelRequestIDs(RuntimeIDs{RunID: "run-1"}),
		WithModel("default-model"),
		WithResponseSchema(JSONSchema{"type": "object"}),
		WithRouteHint("fast"),
		WithMaxInputTokens(10),
		WithMaxOutputTokens(7),
		WithMaxCostUSD(0.01),
		WithTemperature(0.2),
		WithTopP(0.9),
		WithThinkingType("enabled"),
		WithReasoningEffort("high"),
		RequireTool("lookup"),
		EnableToolCalling(),
		EnableToolCalling(),
		WithMetadata(metadata),
	)

	messages[0].Parts[0].Text = "changed"
	tools[0].InputSchema["type"] = "changed"
	metadata["trace"] = "changed"

	if request.Model != "default-model" || request.IDs.RunID != "run-1" || request.RouteHint != "fast" {
		t.Fatalf("request identity = %+v, want model/id/route", request)
	}
	if request.Messages[0].Text() != "hi" || request.Tools[0].InputSchema["type"] != "object" || request.Metadata["trace"] != "original" {
		t.Fatalf("request aliases mutable input: %+v", request)
	}
	if request.Budget.MaxInputTokens != 10 || request.Budget.MaxOutputTokens != 7 || request.Budget.MaxCostUSD != 0.01 {
		t.Fatalf("budget = %+v, want configured limits", request.Budget)
	}
	if request.Temperature == nil || *request.Temperature != 0.2 || request.TopP == nil || *request.TopP != 0.9 {
		t.Fatalf("sampling = temperature %#v top_p %#v, want 0.2/0.9", request.Temperature, request.TopP)
	}
	if request.ThinkingType != "enabled" || request.ReasoningEffort != "high" {
		t.Fatalf("reasoning = %q/%q, want enabled/high", request.ThinkingType, request.ReasoningEffort)
	}
	if request.ToolChoice.Mode != ToolChoiceModeNamed || request.ToolChoice.Name != "lookup" {
		t.Fatalf("tool choice = %+v, want named lookup", request.ToolChoice)
	}
	if len(request.Capabilities) != 1 || request.Capabilities[0] != CapabilityToolCalling {
		t.Fatalf("capabilities = %+v, want one tool calling capability", request.Capabilities)
	}
}

func TestModelRequestToolChoiceOptions(t *testing.T) {
	tests := []struct {
		name string
		opt  ModelRequestOption
		want ToolChoice
	}{
		{name: "auto", opt: WithAutoToolChoice(), want: ToolChoice{Mode: ToolChoiceModeAuto}},
		{name: "required", opt: RequireToolCall(), want: ToolChoice{Mode: ToolChoiceModeRequired}},
		{name: "none", opt: DisableToolCalls(), want: ToolChoice{Mode: ToolChoiceModeNone}},
		{name: "named", opt: RequireTool("lookup"), want: ToolChoice{Mode: ToolChoiceModeNamed, Name: "lookup"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := NewModelRequest(tt.opt)
			if request.ToolChoice != tt.want {
				t.Fatalf("tool choice = %+v, want %+v", request.ToolChoice, tt.want)
			}
			if tt.want.Mode != ToolChoiceModeNone && (len(request.Capabilities) != 1 || request.Capabilities[0] != CapabilityToolCalling) {
				t.Fatalf("capabilities = %+v, want tool calling capability", request.Capabilities)
			}
		})
	}
}

func TestApplyModelRequestOptionsCopiesSamplingPointers(t *testing.T) {
	temperature := 0.2
	topP := 0.9
	original := ModelRequest{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		Temperature: &temperature,
		TopP:        &topP,
	}

	got := ApplyModelRequestOptions(original)
	temperature = 1
	topP = 1

	if got.Temperature == nil || *got.Temperature != 0.2 || got.TopP == nil || *got.TopP != 0.9 {
		t.Fatalf("sampling = temperature %#v top_p %#v, want copied 0.2/0.9", got.Temperature, got.TopP)
	}
}

type responseModelStub struct {
	response ModelResponse
	err      error
	events   []Event
	requests []ModelRequest
}

func (s *responseModelStub) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return ModelResponse{}, err
	}
	s.requests = append(s.requests, request)
	if s.err != nil {
		return ModelResponse{}, s.err
	}
	return s.response, nil
}

func (s *responseModelStub) Stream(ctx context.Context, request ModelRequest) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		s.requests = append(s.requests, request)
		if err := ctx.Err(); err != nil {
			yield(Event{Type: EventModelProviderAttemptFailed, IDs: request.IDs, Err: err}, err)
			return
		}
		for _, event := range s.events {
			if !yield(event, nil) {
				return
			}
		}
	}
}
