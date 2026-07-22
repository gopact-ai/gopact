package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// ModelAccumulator collects one provider call into normalized model facts.
type ModelAccumulator struct {
	ctx       context.Context
	req       gopact.ModelRequest
	sinks     []gopact.ModelEventSink
	parts     []gopact.MessagePart
	toolCalls []gopact.ToolCall
	released  bool
}

// NewAccumulator creates a per-call accumulator.
func (p *BasicProvider) NewAccumulator(ctx context.Context, req gopact.ModelRequest, sinks ...gopact.ModelEventSink) *ModelAccumulator {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ModelAccumulator{
		ctx:   ctx,
		req:   req,
		sinks: append([]gopact.ModelEventSink(nil), sinks...),
	}
}

// Release releases per-call accumulator state.
func (a *ModelAccumulator) Release() {
	if a == nil {
		return
	}
	a.parts = nil
	a.toolCalls = nil
	a.released = true
}

// AddNativeEvent records a bounded provider-native observation.
func (a *ModelAccumulator) AddNativeEvent(event gopact.ModelEvent) error {
	if a == nil || a.released {
		return errors.New("provider: accumulator is released")
	}
	switch event.Type {
	case gopact.ModelEventIntent, gopact.ModelEventMessageDelta:
		return fmt.Errorf("provider: native event %q is not accepted by accumulator", event.Type)
	}
	return a.emit(event)
}

// ReasoningDelta records provider reasoning bytes.
func (a *ModelAccumulator) ReasoningDelta(bytes []byte) error {
	return a.emit(gopact.ModelEvent{
		Type:   gopact.ModelEventReasoningDelta,
		Source: "provider",
		Bytes:  append([]byte(nil), bytes...),
	})
}

// ToolCallDelta records provider tool-call bytes.
func (a *ModelAccumulator) ToolCallDelta(id string, bytes []byte) error {
	return a.emit(gopact.ModelEvent{
		Type:    gopact.ModelEventToolCallDelta,
		Source:  "provider",
		Summary: id,
		Bytes:   append([]byte(nil), bytes...),
	})
}

// NewProtocolPipeline creates a per-call protocol pipeline.
func (a *ModelAccumulator) NewProtocolPipeline(protocols []gopact.OutputProtocol) (*ProtocolPipeline, error) {
	if a == nil || a.released {
		return nil, errors.New("provider: accumulator is released")
	}
	claims := map[string]string{}
	decoders := make([]gopact.OutputDecoder, 0, len(protocols))
	for _, protocol := range protocols {
		if protocol == nil {
			return nil, errors.New("provider: output protocol is nil")
		}
		name := protocol.Name()
		if name == "" {
			return nil, errors.New("provider: output protocol name is required")
		}
		if err := registerProtocolClaims(claims, name, protocol.Claims()); err != nil {
			return nil, err
		}
		decoders = append(decoders, protocol.NewDecoder(protocolSink{acc: a, source: "provider.protocol." + name}))
	}
	return &ProtocolPipeline{acc: a, decoders: decoders, claimed: claims}, nil
}

func registerProtocolClaims(claims map[string]string, name string, declared []gopact.OutputClaim) error {
	for _, claim := range declared {
		key := string(claim.Channel) + "\x00" + string(claim.Kind) + "\x00" + claim.Key
		if owner, ok := claims[key]; ok {
			return fmt.Errorf("provider: output protocol claim conflict between %q and %q", owner, name)
		}
		claims[key] = name
	}
	return nil
}

// Response returns the normalized model response.
func (a *ModelAccumulator) Response() (gopact.ModelResponse, error) {
	if a == nil || a.released {
		return gopact.ModelResponse{}, errors.New("provider: accumulator is released")
	}
	var intent gopact.ModelIntent = gopact.FinalIntent{}
	message := gopact.Message{Role: gopact.MessageRoleAssistant, Parts: append([]gopact.MessagePart(nil), a.parts...)}
	if len(a.toolCalls) > 0 {
		message.ToolCalls = normalizeToolCalls(a.toolCalls)
		intent = gopact.ToolCallIntent{}
	}
	return gopact.ModelResponse{
		Message: message,
		Intent:  intent,
	}, nil
}

func normalizeToolCalls(source []gopact.ToolCall) []gopact.ToolCall {
	calls := append([]gopact.ToolCall(nil), source...)
	for index := range calls {
		calls[index].Arguments = append(json.RawMessage(nil), calls[index].Arguments...)
		if calls[index].ID == "" {
			calls[index].ID = fmt.Sprintf("call-%d", index+1)
		}
		if calls[index].SourceRef == "" {
			calls[index].SourceRef = "provider.protocol"
		}
	}
	return calls
}

func (a *ModelAccumulator) emit(event gopact.ModelEvent) error {
	if a == nil || a.released {
		return errors.New("provider: accumulator is released")
	}
	for _, sink := range a.sinks {
		if sink == nil {
			continue
		}
		if err := sink.EmitModelEvent(a.ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// ProtocolPipeline routes output frames to protocols or visible response text.
type ProtocolPipeline struct {
	acc      *ModelAccumulator
	decoders []gopact.OutputDecoder
	claimed  map[string]string
	closed   bool
}

// Write routes one provider output frame.
func (p *ProtocolPipeline) Write(frame gopact.OutputFrame) error {
	if p == nil || p.closed {
		return errors.New("provider: protocol pipeline is closed")
	}
	for _, decoder := range p.decoders {
		if decoder == nil {
			continue
		}
		if err := decoder.Write(frame); err != nil {
			return err
		}
	}
	_, channelClaimed := p.claimed[string(frame.Channel)+"\x00"+string(gopact.OutputClaimChannel)+"\x00"]
	if !channelClaimed && frame.Channel == gopact.OutputChannelAssistantText {
		text := frame.Text
		if text == "" && len(frame.Bytes) > 0 {
			text = string(frame.Bytes)
		}
		if text != "" {
			p.acc.parts = append(p.acc.parts, gopact.MessagePart{Type: gopact.MessagePartTypeText, Text: text})
		}
	}
	return nil
}

// Close closes all protocol decoders.
func (p *ProtocolPipeline) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	for _, decoder := range p.decoders {
		if decoder == nil {
			continue
		}
		if err := decoder.Close(); err != nil {
			return err
		}
	}
	return nil
}

type protocolSink struct {
	acc    *ModelAccumulator
	source string
}

func (s protocolSink) EmitModelEvent(event gopact.ModelEvent) error {
	if event.Source != "" || event.PayloadRef != "" {
		return errors.New("provider: protocol event must not set source or payload ref")
	}
	switch event.Type {
	case gopact.ModelEventProviderAttemptStarted,
		gopact.ModelEventProviderAttemptFinished,
		gopact.ModelEventMessageDelta,
		gopact.ModelEventUsage,
		gopact.ModelEventFinish,
		gopact.ModelEventRefusal,
		gopact.ModelEventIntent:
		return fmt.Errorf("provider: protocol event %q is not accepted by accumulator", event.Type)
	}
	event.Source = s.source
	return s.acc.emit(event)
}

func (s protocolSink) AddToolCall(call gopact.ToolCall) error {
	if call.ID != "" || call.SourceRef != "" || call.ArgumentsRef != "" {
		return errors.New("provider: protocol tool call must not set id or refs")
	}
	if call.Name == "" {
		return errors.New("provider: protocol tool call name is required")
	}
	if s.source != "" {
		call.SourceRef = s.source
	}
	call.Arguments = append(json.RawMessage(nil), call.Arguments...)
	s.acc.toolCalls = append(s.acc.toolCalls, call)
	return nil
}
