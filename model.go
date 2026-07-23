package gopact

import (
	"context"
	"encoding/json"
	"iter"
)

// Model is the provider-neutral model protocol.
type Model interface {
	NewRequest(messages ...Message) ModelRequest
	Invoke(context.Context, ModelRequest, ...ModelCallOption) (ModelResponse, error)
}

// StreamingModel is the optional model output streaming protocol.
type StreamingModel interface {
	Model
	InvokeStream(context.Context, ModelRequest, ...ModelCallOption) iter.Seq2[ModelOutputChunk, error]
}

// ModelCallConfig is per-call model execution configuration.
type ModelCallConfig struct {
	ModelEventSinks []ModelEventSink
	Extensions      map[string]any
}

// ModelCallOption mutates per-call model execution configuration.
type ModelCallOption interface {
	ApplyModelCallOption(*ModelCallConfig)
}

type modelCallOptionFunc func(*ModelCallConfig)

func (f modelCallOptionFunc) ApplyModelCallOption(cfg *ModelCallConfig) {
	f(cfg)
}

// ResolveModelCallOptions materializes model call options into one config.
func ResolveModelCallOptions(opts ...ModelCallOption) ModelCallConfig {
	var cfg ModelCallConfig
	for _, opt := range opts {
		if opt != nil {
			opt.ApplyModelCallOption(&cfg)
		}
	}
	return cfg
}

// ModelEventSink receives model process events.
type ModelEventSink interface {
	EmitModelEvent(context.Context, ModelEvent) error
}

// ModelEventHandler adapts a function into a ModelEventSink.
type ModelEventHandler func(context.Context, ModelEvent) error

// EmitModelEvent implements ModelEventSink.
func (h ModelEventHandler) EmitModelEvent(ctx context.Context, event ModelEvent) error {
	if h == nil {
		return nil
	}
	return h(ctx, event)
}

// WithModelEventHandler attaches a model event handler to one call.
func WithModelEventHandler(handler ModelEventHandler) ModelCallOption {
	return WithModelEventSink(handler)
}

// WithModelEventSink attaches a model event sink to one call.
func WithModelEventSink(sink ModelEventSink) ModelCallOption {
	return modelCallOptionFunc(func(cfg *ModelCallConfig) {
		if sink != nil {
			cfg.ModelEventSinks = append(cfg.ModelEventSinks, sink)
		}
	})
}

// Message is a provider-neutral model message.
type Message struct {
	Role  string
	Parts []MessagePart
	// ToolCalls contains canonical calls requested by an assistant message.
	ToolCalls []ToolCall
	// ToolCallID associates a tool-role message with its originating call.
	ToolCallID string
}

// Clone returns a deep copy of the message-owned slices and references.
func (m Message) Clone() Message {
	m.Parts = append([]MessagePart(nil), m.Parts...)
	for index := range m.Parts {
		if m.Parts[index].Ref != nil {
			ref := *m.Parts[index].Ref
			m.Parts[index].Ref = &ref
		}
	}
	if m.ToolCalls != nil {
		calls := make([]ToolCall, len(m.ToolCalls))
		for index, call := range m.ToolCalls {
			call.Arguments = append(json.RawMessage(nil), call.Arguments...)
			calls[index] = call
		}
		m.ToolCalls = calls
	}
	return m
}

// Provider-neutral message roles. Role remains a string so providers and
// applications can define additional roles.
const (
	MessageRoleSystem    = "system"
	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
	MessageRoleTool      = "tool"
)

// MessagePart is one provider-neutral message content part.
type MessagePart struct {
	Type string
	Text string
	Ref  *ArtifactRef
}

// Provider-neutral message part types. MessagePart.Type remains extensible.
const (
	MessagePartTypeText     = "text"
	MessagePartTypeArtifact = "artifact"
)

// UserMessage creates a text user message.
func UserMessage(text string) Message {
	return Message{Role: MessageRoleUser, Parts: []MessagePart{{Type: MessagePartTypeText, Text: text}}}
}

// ArtifactRef is an opaque reference to external content.
type ArtifactRef struct {
	URI    string
	Kind   string
	Digest string
}

// RetryHint describes whether a failed operation may be retried.
type RetryHint struct {
	Retryable bool
	Message   string
}

// ToolSpec describes a model-visible tool.
type ToolSpec struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Metadata    map[string]string
}

// ToolCall is one model-requested tool invocation.
type ToolCall struct {
	ID           string
	Name         string
	Arguments    json.RawMessage
	ArgumentsRef string
	SourceRef    string
}

// ToolOutcome is one closed tool-boundary execution fact.
type ToolOutcome interface {
	isToolOutcome()
	ToolCallID() string
	ToolName() string
}

// ToolResult is the payload of a successful tool invocation.
type ToolResult struct {
	DataRef      string
	ArtifactRefs []ArtifactRef
	EffectRefs   []ArtifactRef
	Preview      string
}

// ToolRejection describes a business, policy, or permission rejection.
type ToolRejection struct {
	Reason    string
	Message   string
	RetryHint *RetryHint
	Ref       string
}

// ToolError describes a classified tool execution failure.
type ToolError struct {
	Kind              string
	Message           string
	RetryableForModel bool
	Feedback          string
	RawRef            string
	PartialRefs       []ArtifactRef
}

// ToolInterrupt describes external input required to continue a tool call.
type ToolInterrupt struct {
	InterruptID         string
	Reason              string
	ResolutionSchemaRef string
	PayloadRef          string
	RunID               string
	CheckpointID        string
}

// ToolResultOutcome reports a successful tool invocation.
type ToolResultOutcome struct {
	CallID string
	Name   string
	Result ToolResult
}

func (ToolResultOutcome) isToolOutcome() {}

// ToolCallID returns the originating call ID.
func (o ToolResultOutcome) ToolCallID() string { return o.CallID }

// ToolName returns the invoked tool name.
func (o ToolResultOutcome) ToolName() string { return o.Name }

// ToolRejectedOutcome reports a rejected tool invocation.
type ToolRejectedOutcome struct {
	CallID    string
	Name      string
	Rejection ToolRejection
}

func (ToolRejectedOutcome) isToolOutcome() {}

// ToolCallID returns the originating call ID.
func (o ToolRejectedOutcome) ToolCallID() string { return o.CallID }

// ToolName returns the invoked tool name.
func (o ToolRejectedOutcome) ToolName() string { return o.Name }

// ToolErrorOutcome reports a failed tool invocation that was classified at the tool boundary.
type ToolErrorOutcome struct {
	CallID string
	Name   string
	Error  ToolError
}

func (ToolErrorOutcome) isToolOutcome() {}

// ToolCallID returns the originating call ID.
func (o ToolErrorOutcome) ToolCallID() string { return o.CallID }

// ToolName returns the invoked tool name.
func (o ToolErrorOutcome) ToolName() string { return o.Name }

// ToolInterruptOutcome reports a tool invocation waiting for external input.
type ToolInterruptOutcome struct {
	CallID    string
	Name      string
	Interrupt ToolInterrupt
}

func (ToolInterruptOutcome) isToolOutcome() {}

// ToolCallID returns the originating call ID.
func (o ToolInterruptOutcome) ToolCallID() string { return o.CallID }

// ToolName returns the invoked tool name.
func (o ToolInterruptOutcome) ToolName() string { return o.Name }

// ToolEventType identifies a tool call observation.
type ToolEventType string

// Tool call event types.
const (
	ToolEventCallStarted  ToolEventType = "call.started"
	ToolEventCallFinished ToolEventType = "call.finished"
)

// ToolEvent is a live, observer-only view of one tool call boundary.
// Call and Outcome are read-only for the duration of EmitToolEvent.
type ToolEvent struct {
	Type    ToolEventType
	Call    ToolCall
	Outcome ToolOutcome
	Err     error
}

// ToolEventSink receives live tool call events.
type ToolEventSink interface {
	EmitToolEvent(context.Context, ToolEvent) error
}

// ToolChoice constrains model tool selection.
type ToolChoice struct {
	Mode string
	Name string
}

// Provider-neutral tool choice modes. ToolChoice.Mode remains extensible.
const (
	ToolChoiceModeAuto     = "auto"
	ToolChoiceModeNone     = "none"
	ToolChoiceModeRequired = "required"
	ToolChoiceModeNamed    = "named"
)

// SchemaRef describes an inline schema or schema reference.
type SchemaRef struct {
	Value json.RawMessage
	URI   string
}

// Modality identifies a model input or output modality.
type Modality string

// Common model modalities. Applications may define additional Modality values.
const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityAudio Modality = "audio"
)

// ReasoningConfig carries provider-neutral reasoning controls.
type ReasoningConfig struct {
	Effort string
}

// Common reasoning effort levels. ReasoningConfig.Effort remains extensible.
const (
	ReasoningEffortLow    = "low"
	ReasoningEffortMedium = "medium"
	ReasoningEffortHigh   = "high"
)

// OutputProtocol describes a model output decoder contract.
type OutputProtocol interface {
	Name() string
	Claims() []OutputClaim
	NewDecoder(ProtocolSink) OutputDecoder
}

// OutputClaim describes model output claimed by a protocol.
type OutputClaim struct {
	Channel OutputChannel
	Kind    OutputClaimKind
	Key     string
}

// OutputChannel identifies a model output stream channel.
type OutputChannel string

// Model output channels.
const (
	OutputChannelAssistantText OutputChannel = "assistant_text"
	OutputChannelReasoning     OutputChannel = "reasoning"
	OutputChannelToolCall      OutputChannel = "tool_call"
)

// OutputClaimKind describes how an output claim matches frames.
type OutputClaimKind string

// Output claim kinds.
const (
	OutputClaimChannel OutputClaimKind = "channel"
	OutputClaimTag     OutputClaimKind = "tag"
)

// ProtocolSink receives decoded protocol facts.
type ProtocolSink interface {
	EmitModelEvent(ModelEvent) error
	AddToolCall(ToolCall) error
}

// OutputDecoder consumes model output frames.
type OutputDecoder interface {
	Write(OutputFrame) error
	Close() error
}

// OutputFrame is one model output protocol frame.
type OutputFrame struct {
	Channel OutputChannel
	Bytes   []byte
	Text    string
}

// ModelRequest is a provider-neutral model request.
type ModelRequest struct {
	Model           string
	Messages        []Message
	Tools           []ToolSpec
	ToolChoice      ToolChoice
	ResponseSchema  SchemaRef
	Modalities      []Modality
	Temperature     *float64
	TopP            *float64
	MaxOutputTokens int
	Reasoning       ReasoningConfig
	Stop            []string
	Seed            *int64
	OutputProtocols []OutputProtocol
	Metadata        map[string]string
	Extensions      map[string]any
}

// WithTools returns a request with replaced tools.
func (r ModelRequest) WithTools(tools ...ToolSpec) ModelRequest {
	r.Tools = append([]ToolSpec(nil), tools...)
	return r
}

// WithTemperature returns a request with temperature set.
func (r ModelRequest) WithTemperature(value float64) ModelRequest {
	v := value
	r.Temperature = &v
	return r
}

// WithReasoning returns a request with reasoning controls set.
func (r ModelRequest) WithReasoning(cfg ReasoningConfig) ModelRequest {
	r.Reasoning = cfg
	return r
}

// ModelResponse is the normalized result of one model call.
type ModelResponse struct {
	Message          Message
	Intent           ModelIntent
	Usage            Usage
	FinishReason     string
	ProviderMetadata map[string]any
}

// ModelIntentType identifies a model terminal decision.
type ModelIntentType string

// Model intent types.
const (
	ModelIntentFinal    ModelIntentType = "final"
	ModelIntentToolCall ModelIntentType = "tool_call"
	ModelIntentRefusal  ModelIntentType = "refusal"
	ModelIntentRepair   ModelIntentType = "repair"
)

// ModelIntent is the closed set of terminal model decisions understood by
// provider normalization and Agent runtimes.
type ModelIntent interface {
	IntentType() ModelIntentType
	isModelIntent()
}

// FinalIntent indicates a final assistant response.
type FinalIntent struct{}

// IntentType returns ModelIntentFinal.
func (FinalIntent) IntentType() ModelIntentType { return ModelIntentFinal }
func (FinalIntent) isModelIntent()              {}

// ToolCallIntent indicates that Message.ToolCalls request tool execution.
// The calls live on Message so the model decision and durable transcript cannot
// carry divergent copies of the same facts.
type ToolCallIntent struct{}

// IntentType returns ModelIntentToolCall.
func (ToolCallIntent) IntentType() ModelIntentType { return ModelIntentToolCall }
func (ToolCallIntent) isModelIntent()              {}

// RefusalIntent reports a model refusal.
type RefusalIntent struct {
	Refusal Refusal
}

// IntentType returns ModelIntentRefusal.
func (RefusalIntent) IntentType() ModelIntentType { return ModelIntentRefusal }
func (RefusalIntent) isModelIntent()              {}

// RepairIntent requests a repaired model context.
type RepairIntent struct {
	Repair RepairRequest
}

// IntentType returns ModelIntentRepair.
func (RepairIntent) IntentType() ModelIntentType { return ModelIntentRepair }
func (RepairIntent) isModelIntent()              {}

// Refusal describes why a model refused.
type Refusal struct {
	Reason  string
	Message Message
	Ref     string
}

// RepairRequest describes a requested repair turn.
type RepairRequest struct {
	Reason  string
	Message Message
	Ref     string
}

// Usage summarizes model token usage.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// ModelOutputChunk is one typed model output stream item.
type ModelOutputChunk struct {
	Text  string
	Parts []MessagePart
}

// ModelEventType identifies a model process event.
type ModelEventType string

// Model event types.
const (
	ModelEventCallStarted             ModelEventType = "call.started"
	ModelEventCallFinished            ModelEventType = "call.finished"
	ModelEventProviderAttemptStarted  ModelEventType = "provider.attempt_started"
	ModelEventProviderAttemptFinished ModelEventType = "provider.attempt_finished"
	ModelEventMessageDelta            ModelEventType = "message.delta"
	ModelEventReasoningDelta          ModelEventType = "reasoning.delta"
	ModelEventToolCallDelta           ModelEventType = "tool_call.delta"
	ModelEventUsage                   ModelEventType = "usage"
	ModelEventFinish                  ModelEventType = "finish"
	ModelEventRefusal                 ModelEventType = "refusal"
	ModelEventIntent                  ModelEventType = "intent"
)

// ModelEvent is a live observation emitted during a model call. Request and
// Response are read-only for the duration of EmitModelEvent. Provider process
// fields remain bounded by the emitting adapter.
type ModelEvent struct {
	Type       ModelEventType
	Source     string
	Bytes      []byte
	Summary    string
	Payload    json.RawMessage
	PayloadRef string
	Request    *ModelRequest
	Response   *ModelResponse
	Err        error
}
