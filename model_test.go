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

	message, err := chat.Generate(context.Background(), ModelRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, WithModel("default-model"), WithMaxOutputTokens(7))
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
	for event, err := range streamer.Stream(context.Background(), ModelRequest{}, WithModel("stream-model"), EnableStreaming()) {
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

type responseModelStub struct {
	response ModelResponse
	err      error
	events   []Event
	requests []ModelRequest
}

func (s *responseModelStub) Generate(ctx context.Context, request ModelRequest, opts ...ModelOption) (ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return ModelResponse{}, err
	}
	request = ApplyModelOptions(request, opts...)
	s.requests = append(s.requests, request)
	if s.err != nil {
		return ModelResponse{}, s.err
	}
	return s.response, nil
}

func (s *responseModelStub) Stream(ctx context.Context, request ModelRequest, opts ...ModelOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		request = ApplyModelOptions(request, opts...)
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
