package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestBasicProviderConformance(t *testing.T) {
	model := NewBasicProvider(
		gopact.ModelRequest{Model: "fake"},
		func(_ context.Context, req gopact.ModelRequest, _ ...gopact.ModelCallOption) (gopact.ModelResponse, error) {
			return gopact.ModelResponse{
				Message: gopact.Message{Role: "assistant", Parts: req.Messages[0].Parts},
			}, nil
		},
	)
	gopacttest.RequireModelConformance(t, model)
	err := NewConformanceHarness(ConformanceSpec{
		Cases: StandardConformanceCases(),
	}).Run(context.Background(), model)
	if err != nil {
		t.Fatalf("ConformanceHarness.Run() error = %v", err)
	}
}

func TestRouterUsesFirstAvailableCandidate(t *testing.T) {
	registry := NewRegistry()
	model := NewBasicProvider(
		gopact.ModelRequest{},
		func(_ context.Context, _ gopact.ModelRequest, _ ...gopact.ModelCallOption) (gopact.ModelResponse, error) {
			return gopact.ModelResponse{FinishReason: "stop"}, nil
		},
	)
	if err := registry.Register("fake", model); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	resp, err := NewRouter(registry).Invoke(context.Background(), gopact.ModelRequest{}, []string{"missing", "fake"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("response = %+v, want stop", resp)
	}
}

func TestModelAccumulatorPipelinePassesThroughAssistantText(t *testing.T) {
	acc := NewBasicProvider(gopact.ModelRequest{}, nil).NewAccumulator(context.Background(), gopact.ModelRequest{})
	defer acc.Release()
	pipeline, err := acc.NewProtocolPipeline(nil)
	if err != nil {
		t.Fatalf("NewProtocolPipeline() error = %v", err)
	}
	if err := pipeline.Write(gopact.OutputFrame{Channel: gopact.OutputChannelAssistantText, Text: "hello"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := pipeline.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	resp, err := acc.Response()
	if err != nil {
		t.Fatalf("Response() error = %v", err)
	}
	if got := resp.Message.Parts[0].Text; got != "hello" {
		t.Fatalf("response text = %q, want hello", got)
	}
	if _, ok := resp.Intent.(gopact.FinalIntent); !ok {
		t.Fatalf("intent = %T, want FinalIntent", resp.Intent)
	}
}

func TestModelAccumulatorPipelineRejectsClaimConflict(t *testing.T) {
	acc := NewBasicProvider(gopact.ModelRequest{}, nil).NewAccumulator(context.Background(), gopact.ModelRequest{})
	defer acc.Release()
	_, err := acc.NewProtocolPipeline([]gopact.OutputProtocol{
		testProtocol{name: "a", claims: []gopact.OutputClaim{{
			Channel: gopact.OutputChannelAssistantText,
			Kind:    gopact.OutputClaimChannel,
		}}},
		testProtocol{name: "b", claims: []gopact.OutputClaim{{
			Channel: gopact.OutputChannelAssistantText,
			Kind:    gopact.OutputClaimChannel,
		}}},
	})
	if err == nil {
		t.Fatal("NewProtocolPipeline() error = nil, want claim conflict")
	}
}

func TestProtocolSinkBuildsToolCallIntent(t *testing.T) {
	acc := NewBasicProvider(gopact.ModelRequest{}, nil).NewAccumulator(context.Background(), gopact.ModelRequest{})
	defer acc.Release()
	arguments := json.RawMessage(`{"query":"gopact"}`)
	pipeline, err := acc.NewProtocolPipeline([]gopact.OutputProtocol{
		testProtocol{name: "tool", newDecoder: func(sink gopact.ProtocolSink) gopact.OutputDecoder {
			return testDecoderFunc(func(frame gopact.OutputFrame) error {
				return sink.AddToolCall(gopact.ToolCall{Name: frame.Text, Arguments: arguments})
			})
		}},
	})
	if err != nil {
		t.Fatalf("NewProtocolPipeline() error = %v", err)
	}
	if err := pipeline.Write(gopact.OutputFrame{Channel: gopact.OutputChannelToolCall, Text: "search"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	arguments[0] = '['
	resp, err := acc.Response()
	if err != nil {
		t.Fatalf("Response() error = %v", err)
	}
	_, ok := resp.Intent.(gopact.ToolCallIntent)
	if !ok {
		t.Fatalf("intent = %T, want ToolCallIntent", resp.Intent)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "search" {
		t.Fatalf("message = %+v, want one search tool call", resp.Message)
	}
	if resp.Message.ToolCalls[0].ID != "call-1" || resp.Message.ToolCalls[0].SourceRef != "provider.protocol.tool" {
		t.Fatalf("tool call = %+v, want deterministic id and source", resp.Message.ToolCalls[0])
	}
	if string(resp.Message.ToolCalls[0].Arguments) != `{"query":"gopact"}` {
		t.Fatalf("tool arguments = %s, want accumulator-owned copy", resp.Message.ToolCalls[0].Arguments)
	}
	resp.Message.ToolCalls[0].Arguments[0] = '['
	again, err := acc.Response()
	if err != nil {
		t.Fatalf("second Response() error = %v", err)
	}
	if string(again.Message.ToolCalls[0].Arguments) != `{"query":"gopact"}` {
		t.Fatalf("second response arguments = %s, want caller-isolated copy", again.Message.ToolCalls[0].Arguments)
	}
}

func TestProtocolSinkRejectsTerminalModelEvent(t *testing.T) {
	acc := NewBasicProvider(gopact.ModelRequest{}, nil).NewAccumulator(context.Background(), gopact.ModelRequest{})
	defer acc.Release()
	pipeline, err := acc.NewProtocolPipeline([]gopact.OutputProtocol{
		testProtocol{name: "intent", newDecoder: func(sink gopact.ProtocolSink) gopact.OutputDecoder {
			return testDecoderFunc(func(gopact.OutputFrame) error {
				return sink.EmitModelEvent(gopact.ModelEvent{Type: gopact.ModelEventIntent})
			})
		}},
	})
	if err != nil {
		t.Fatalf("NewProtocolPipeline() error = %v", err)
	}
	if err := pipeline.Write(gopact.OutputFrame{Channel: gopact.OutputChannelReasoning}); err == nil {
		t.Fatal("Write() error = nil, want terminal event rejection")
	}
}

func TestModelAccumulatorReleaseRejectsUse(t *testing.T) {
	acc := NewBasicProvider(gopact.ModelRequest{}, nil).NewAccumulator(context.Background(), gopact.ModelRequest{})
	acc.Release()
	if _, err := acc.Response(); err == nil {
		t.Fatal("Response() error = nil, want released error")
	}
}

type testProtocol struct {
	name       string
	claims     []gopact.OutputClaim
	decoder    gopact.OutputDecoder
	newDecoder func(gopact.ProtocolSink) gopact.OutputDecoder
}

func (p testProtocol) Name() string {
	return p.name
}

func (p testProtocol) Claims() []gopact.OutputClaim {
	return p.claims
}

func (p testProtocol) NewDecoder(sink gopact.ProtocolSink) gopact.OutputDecoder {
	if p.newDecoder != nil {
		return p.newDecoder(sink)
	}
	if p.decoder != nil {
		return p.decoder
	}
	return testDecoderFunc(func(gopact.OutputFrame) error { return nil })
}

type testDecoderFunc func(gopact.OutputFrame) error

func (f testDecoderFunc) Write(frame gopact.OutputFrame) error {
	return f(frame)
}

func (f testDecoderFunc) Close() error {
	return nil
}
