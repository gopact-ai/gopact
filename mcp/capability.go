package mcp

import (
	"context"
	"errors"

	"github.com/gopact-ai/gopact"
)

var (
	ErrSamplingHandlerRequired            = errors.New("mcp: sampling handler is required")
	ErrElicitationHandlerRequired         = errors.New("mcp: elicitation handler is required")
	ErrElicitationCompleteHandlerRequired = errors.New("mcp: elicitation complete handler is required")
	ErrElicitationCompleteIDRequired      = errors.New("mcp: elicitation complete id is required")
)

// SamplingHandler handles server-initiated sampling/createMessage requests.
type SamplingHandler interface {
	CreateMessage(ctx context.Context, request SamplingRequest) (SamplingResponse, error)
}

// SamplingHandlerFunc adapts a function into a SamplingHandler.
type SamplingHandlerFunc func(ctx context.Context, request SamplingRequest) (SamplingResponse, error)

// CreateMessage calls f.
func (f SamplingHandlerFunc) CreateMessage(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
	if f == nil {
		return SamplingResponse{}, ErrSamplingHandlerRequired
	}
	return f(ctx, request)
}

// SamplingRequest is the provider-neutral shape of an MCP sampling/createMessage request.
type SamplingRequest struct {
	Messages         []gopact.Message `json:"messages,omitempty"`
	ModelPreferences ModelPreferences `json:"modelPreferences,omitempty"`
	SystemPrompt     string           `json:"systemPrompt,omitempty"`
	IncludeContext   string           `json:"includeContext,omitempty"`
	MaxTokens        int              `json:"maxTokens,omitempty"`
	Tools            []ToolInfo       `json:"tools,omitempty"`
	ToolChoice       ToolChoice       `json:"toolChoice,omitempty"`
	Metadata         map[string]any   `json:"_meta,omitempty"`
}

// ModelPreferences captures MCP's advisory model selection hints.
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         float64     `json:"costPriority,omitempty"`
	SpeedPriority        float64     `json:"speedPriority,omitempty"`
	IntelligencePriority float64     `json:"intelligencePriority,omitempty"`
}

// ModelHint is an advisory model name or family hint.
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// ToolChoiceMode controls whether sampling may use tools.
type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceNone     ToolChoiceMode = "none"
)

// ToolChoice is the MCP sampling tool-choice setting.
type ToolChoice struct {
	Mode ToolChoiceMode `json:"mode,omitempty"`
}

// SamplingResponse is the provider-neutral result of a sampling request.
type SamplingResponse struct {
	Role       gopact.Role          `json:"role,omitempty"`
	Content    []gopact.ContentPart `json:"content,omitempty"`
	Model      string               `json:"model,omitempty"`
	StopReason string               `json:"stopReason,omitempty"`
	Metadata   map[string]any       `json:"_meta,omitempty"`
}

// ElicitationMode identifies how a server wants to collect user input.
type ElicitationMode string

const (
	ElicitationForm ElicitationMode = "form"
	ElicitationURL  ElicitationMode = "url"
)

// ElicitationHandler handles server-initiated elicitation/create requests.
type ElicitationHandler interface {
	Elicit(ctx context.Context, request ElicitationRequest) (ElicitationResponse, error)
}

// ElicitationHandlerFunc adapts a function into an ElicitationHandler.
type ElicitationHandlerFunc func(ctx context.Context, request ElicitationRequest) (ElicitationResponse, error)

// Elicit calls f.
func (f ElicitationHandlerFunc) Elicit(ctx context.Context, request ElicitationRequest) (ElicitationResponse, error) {
	if f == nil {
		return ElicitationResponse{}, ErrElicitationHandlerRequired
	}
	return f(ctx, request)
}

// ElicitationRequest is the provider-neutral shape of an MCP elicitation/create request.
type ElicitationRequest struct {
	Mode            ElicitationMode   `json:"mode,omitempty"`
	Message         string            `json:"message,omitempty"`
	RequestedSchema gopact.JSONSchema `json:"requestedSchema,omitempty"`
	URL             string            `json:"url,omitempty"`
	ElicitationID   string            `json:"elicitationId,omitempty"`
	Metadata        map[string]any    `json:"_meta,omitempty"`
}

// ElicitationAction records the user's response to an elicitation request.
type ElicitationAction string

const (
	ElicitationAccept  ElicitationAction = "accept"
	ElicitationDecline ElicitationAction = "decline"
	ElicitationCancel  ElicitationAction = "cancel"
)

// ElicitationResponse is the result of an elicitation request.
type ElicitationResponse struct {
	Action   ElicitationAction `json:"action,omitempty"`
	Content  map[string]any    `json:"content,omitempty"`
	Metadata map[string]any    `json:"_meta,omitempty"`
}

// ElicitationCompleteHandler handles URL-mode elicitation completion notifications.
type ElicitationCompleteHandler interface {
	Complete(ctx context.Context, notification ElicitationCompleteNotification) error
}

// ElicitationCompleteHandlerFunc adapts a function into an ElicitationCompleteHandler.
type ElicitationCompleteHandlerFunc func(ctx context.Context, notification ElicitationCompleteNotification) error

// Complete calls f.
func (f ElicitationCompleteHandlerFunc) Complete(ctx context.Context, notification ElicitationCompleteNotification) error {
	if f == nil {
		return ErrElicitationCompleteHandlerRequired
	}
	return f(ctx, notification)
}

// ElicitationCompleteNotification reports that a URL-mode elicitation finished out of band.
type ElicitationCompleteNotification struct {
	ElicitationID string         `json:"elicitationId,omitempty"`
	Metadata      map[string]any `json:"_meta,omitempty"`
}

func copySamplingRequest(in SamplingRequest) SamplingRequest {
	in.Messages = copyGopactMessages(in.Messages)
	in.ModelPreferences = copyModelPreferences(in.ModelPreferences)
	in.Tools = copyToolInfos(in.Tools)
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyModelPreferences(in ModelPreferences) ModelPreferences {
	in.Hints = append([]ModelHint(nil), in.Hints...)
	return in
}

func copyElicitationRequest(in ElicitationRequest) ElicitationRequest {
	in.RequestedSchema = gopact.JSONSchema(copyAnyMap(in.RequestedSchema))
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyElicitationCompleteNotification(in ElicitationCompleteNotification) ElicitationCompleteNotification {
	in.Metadata = copyAnyMap(in.Metadata)
	return in
}

func copyGopactMessages(in []gopact.Message) []gopact.Message {
	out := append([]gopact.Message(nil), in...)
	for i := range out {
		out[i].Parts = copyContentParts(out[i].Parts)
		out[i].ToolCalls = copyGopactToolCalls(out[i].ToolCalls)
	}
	return out
}

func copyGopactToolCalls(in []gopact.ToolCall) []gopact.ToolCall {
	out := append([]gopact.ToolCall(nil), in...)
	for i := range out {
		out[i].Arguments = append([]byte(nil), out[i].Arguments...)
	}
	return out
}
