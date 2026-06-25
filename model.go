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
	IDs            RuntimeIDs     `json:"ids,omitempty"`
	Model          string         `json:"model,omitempty"`
	Messages       []Message      `json:"messages"`
	Tools          []ToolSpec     `json:"tools,omitempty"`
	ResponseSchema JSONSchema     `json:"response_schema,omitempty"`
	RouteHint      string         `json:"route_hint,omitempty"`
	Capabilities   []Capability   `json:"capabilities,omitempty"`
	Budget         Budget         `json:"budget,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
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
