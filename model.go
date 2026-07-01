package gopact

import (
	"context"
	"errors"
	"iter"
)

// ChatModel 是 agent 对模型 provider 的最小依赖契约。
type ChatModel interface {
	Generate(ctx context.Context, request ModelRequest) (Message, error)
}

// ResponseModel returns a full model response with route, usage, and middleware events.
type ResponseModel interface {
	Generate(ctx context.Context, request ModelRequest) (ModelResponse, error)
}

// StreamingModel streams model invocation events, including route and fallback attempts.
type StreamingModel interface {
	Stream(ctx context.Context, request ModelRequest) iter.Seq2[Event, error]
}

// StreamingResponseModel is the adapter input for models that expose both response and stream APIs.
type StreamingResponseModel interface {
	ResponseModel
	StreamingModel
}

var errNilResponseModel = errors.New("gopact: response model is nil")

// AdaptResponseModel adapts a full response model to the minimal ChatModel contract.
func AdaptResponseModel(model ResponseModel) ChatModel {
	return responseModelAdapter{model: model}
}

// AdaptStreamingModel adapts a streaming response model to ChatModel while preserving Stream.
func AdaptStreamingModel(model StreamingResponseModel) ChatModel {
	return streamingModelAdapter{model: model}
}

type responseModelAdapter struct {
	model ResponseModel
}

func (a responseModelAdapter) Generate(ctx context.Context, request ModelRequest) (Message, error) {
	if a.model == nil {
		return Message{}, errNilResponseModel
	}
	response, err := a.model.Generate(ctx, request)
	if err != nil {
		return Message{}, err
	}
	return response.Message, nil
}

type streamingModelAdapter struct {
	model StreamingResponseModel
}

func (a streamingModelAdapter) Generate(ctx context.Context, request ModelRequest) (Message, error) {
	if a.model == nil {
		return Message{}, errNilResponseModel
	}
	response, err := a.model.Generate(ctx, request)
	if err != nil {
		return Message{}, err
	}
	return response.Message, nil
}

func (a streamingModelAdapter) Stream(ctx context.Context, request ModelRequest) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if a.model == nil {
			err := errNilResponseModel
			yield(Event{Type: EventModelProviderAttemptFailed, IDs: request.IDs, Err: err, CreatedAt: now()}, err)
			return
		}
		for event, err := range a.model.Stream(ctx, request) {
			if !yield(event, err) {
				return
			}
		}
	}
}

// ModelRequest 向模型 provider 传递消息、工具 schema，以及可选的结构化输出提示。
type ModelRequest struct {
	IDs             RuntimeIDs     `json:"ids,omitempty"`
	Model           string         `json:"model,omitempty"`
	Messages        []Message      `json:"messages"`
	Tools           []ToolSpec     `json:"tools,omitempty"`
	ToolChoice      ToolChoice     `json:"tool_choice,omitempty"`
	ResponseSchema  JSONSchema     `json:"response_schema,omitempty"`
	RouteHint       string         `json:"route_hint,omitempty"`
	Capabilities    []Capability   `json:"capabilities,omitempty"`
	Budget          Budget         `json:"budget,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	TopP            *float64       `json:"top_p,omitempty"`
	ThinkingType    string         `json:"thinking_type,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ModelRequestOption configures a ModelRequest without exposing provider-specific request shapes.
type ModelRequestOption interface {
	ApplyModelRequestOption(*ModelRequest)
}

// ModelRequestOptionFunc adapts a function into a ModelRequestOption.
type ModelRequestOptionFunc func(*ModelRequest)

func (f ModelRequestOptionFunc) ApplyModelRequestOption(request *ModelRequest) {
	if f != nil {
		f(request)
	}
}

// NewModelRequest creates a provider-neutral model request from request options.
func NewModelRequest(opts ...ModelRequestOption) ModelRequest {
	return ApplyModelRequestOptions(ModelRequest{}, opts...)
}

// ApplyModelRequestOptions applies request options to a mutable-safe request copy.
func ApplyModelRequestOptions(request ModelRequest, opts ...ModelRequestOption) ModelRequest {
	request = copyModelRequest(request)
	for _, opt := range opts {
		if opt != nil {
			opt.ApplyModelRequestOption(&request)
		}
	}
	return request
}

func WithMessages(messages ...Message) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Messages = copyMessages(messages)
	})
}

func WithTools(tools ...ToolSpec) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Tools = copyToolSpecs(tools)
	})
}

func WithToolChoice(choice ToolChoice) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.ToolChoice = choice
		if choice.Mode != "" && choice.Mode != ToolChoiceModeNone {
			addCapability(request, CapabilityToolCalling)
		}
	})
}

func WithAutoToolChoice() ModelRequestOption {
	return WithToolChoice(ToolChoice{Mode: ToolChoiceModeAuto})
}

func RequireToolCall() ModelRequestOption {
	return WithToolChoice(ToolChoice{Mode: ToolChoiceModeRequired})
}

func RequireTool(name string) ModelRequestOption {
	return WithToolChoice(ToolChoice{Mode: ToolChoiceModeNamed, Name: name})
}

func DisableToolCalls() ModelRequestOption {
	return WithToolChoice(ToolChoice{Mode: ToolChoiceModeNone})
}

func WithModelRequestIDs(ids RuntimeIDs) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.IDs = ids
	})
}

func WithModel(model string) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Model = model
	})
}

func WithResponseSchema(schema JSONSchema) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.ResponseSchema = copyJSONSchema(schema)
	})
}

func WithRouteHint(route string) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.RouteHint = route
	})
}

func WithBudget(budget Budget) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Budget = budget
	})
}

func WithMaxInputTokens(tokens int) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Budget.MaxInputTokens = tokens
	})
}

func WithMaxOutputTokens(tokens int) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Budget.MaxOutputTokens = tokens
	})
}

func WithMaxCostUSD(cost float64) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Budget.MaxCostUSD = cost
	})
}

func WithTemperature(temperature float64) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Temperature = &temperature
	})
}

func WithTopP(topP float64) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.TopP = &topP
	})
}

func WithThinkingType(thinkingType string) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.ThinkingType = thinkingType
	})
}

func WithReasoningEffort(effort string) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.ReasoningEffort = effort
	})
}

func WithCapabilities(capabilities ...Capability) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Capabilities = append([]Capability(nil), capabilities...)
	})
}

func EnableCapability(capability Capability) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		addCapability(request, capability)
	})
}

func EnableStreaming() ModelRequestOption {
	return EnableCapability(CapabilityStreaming)
}

func EnableToolCalling() ModelRequestOption {
	return EnableCapability(CapabilityToolCalling)
}

func EnableJSONSchema() ModelRequestOption {
	return EnableCapability(CapabilityJSONSchema)
}

func EnableVision() ModelRequestOption {
	return EnableCapability(CapabilityVision)
}

func EnableReasoning() ModelRequestOption {
	return EnableCapability(CapabilityReasoning)
}

func EnableStructuredOutput() ModelRequestOption {
	return EnableCapability(CapabilityStructuredOutput)
}

func WithMetadata(metadata map[string]any) ModelRequestOption {
	return ModelRequestOptionFunc(func(request *ModelRequest) {
		request.Metadata = copyAnyMap(metadata)
	})
}

// Capability describes a hard capability a model request or route candidate must support.
type Capability string

const (
	CapabilityToolCalling      Capability = "tool_calling"
	CapabilityStreaming        Capability = "streaming"
	CapabilityJSONSchema       Capability = "json_schema"
	CapabilityVision           Capability = "vision"
	CapabilityReasoning        Capability = "reasoning"
	CapabilityStructuredOutput Capability = "structured_output"
)

// ToolChoice controls whether and how a provider should select tools.
type ToolChoice struct {
	Mode ToolChoiceMode `json:"mode,omitempty"`
	Name string         `json:"name,omitempty"`
}

// ToolChoiceMode is provider-neutral; adapters translate it to native API shapes.
type ToolChoiceMode string

const (
	ToolChoiceModeAuto     ToolChoiceMode = "auto"
	ToolChoiceModeRequired ToolChoiceMode = "required"
	ToolChoiceModeNone     ToolChoiceMode = "none"
	ToolChoiceModeNamed    ToolChoiceMode = "named"
)

func addCapability(request *ModelRequest, capability Capability) {
	for _, existing := range request.Capabilities {
		if existing == capability {
			return
		}
	}
	request.Capabilities = append(request.Capabilities, capability)
}

// Budget carries model-call budget hints.
type Budget struct {
	MaxInputTokens  int     `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int     `json:"max_output_tokens,omitempty"`
	MaxCostUSD      float64 `json:"max_cost_usd,omitempty"`
}

// Usage captures normalized model usage and cost metadata.
type Usage struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// ModelRoute records how a model request was routed.
type ModelRoute struct {
	RouteName     string         `json:"route_name,omitempty"`
	Provider      string         `json:"provider,omitempty"`
	Model         string         `json:"model,omitempty"`
	Endpoint      string         `json:"endpoint,omitempty"`
	Attempt       int            `json:"attempt,omitempty"`
	ConfigVersion string         `json:"config_version,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// ModelResponse is a provider-neutral model response with normalized route and usage metadata.
type ModelResponse struct {
	Message  Message        `json:"message"`
	Route    ModelRoute     `json:"route,omitempty"`
	Usage    Usage          `json:"usage,omitempty"`
	Events   []Event        `json:"events,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
