package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/gopact-ai/gopact"
)

// CapabilityServer handles MCP server-to-client capability requests.
type CapabilityServer struct {
	sampling            SamplingHandler
	elicitation         ElicitationHandler
	elicitationComplete ElicitationCompleteHandler
	maxMessageBytes     int
}

// CapabilityServerOption configures CapabilityServer.
type CapabilityServerOption func(*CapabilityServer)

// WithSamplingHandler installs a handler for sampling/createMessage requests.
func WithSamplingHandler(handler SamplingHandler) CapabilityServerOption {
	return func(s *CapabilityServer) {
		if handler != nil {
			s.sampling = handler
		}
	}
}

// WithElicitationHandler installs a handler for elicitation/create requests.
func WithElicitationHandler(handler ElicitationHandler) CapabilityServerOption {
	return func(s *CapabilityServer) {
		if handler != nil {
			s.elicitation = handler
		}
	}
}

// WithElicitationCompleteHandler installs a handler for notifications/elicitation/complete.
func WithElicitationCompleteHandler(handler ElicitationCompleteHandler) CapabilityServerOption {
	return func(s *CapabilityServer) {
		if handler != nil {
			s.elicitationComplete = handler
		}
	}
}

// WithCapabilityServerMaxMessageBytes sets the maximum accepted JSON-RPC request size.
func WithCapabilityServerMaxMessageBytes(n int) CapabilityServerOption {
	return func(s *CapabilityServer) {
		if n > 0 {
			s.maxMessageBytes = n
		}
	}
}

// NewCapabilityServer creates a server for MCP client-side capabilities.
func NewCapabilityServer(opts ...CapabilityServerOption) *CapabilityServer {
	server := &CapabilityServer{
		maxMessageBytes: defaultLineTransportMaxMessageBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}
	return server
}

// ServeLine handles newline-delimited JSON-RPC requests until EOF.
func (s *CapabilityServer) ServeLine(ctx context.Context, reader io.Reader, writer io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil {
		return ErrReaderRequired
	}
	if writer == nil {
		return ErrWriterRequired
	}
	buffered := bufio.NewReader(reader)
	for {
		line, err := readServerLine(buffered, s.maxMessageBytes)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		response, ok, err := s.Handle(ctx, line)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		response = append(response, '\n')
		if _, err := writer.Write(response); err != nil {
			return fmt.Errorf("mcp: write json-rpc response: %w", err)
		}
	}
}

// Handle handles one MCP server-to-client JSON-RPC request. Notifications return ok=false.
func (s *CapabilityServer) Handle(ctx context.Context, request json.RawMessage) (response json.RawMessage, ok bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if s == nil {
		s = NewCapabilityServer()
	}

	var message serverRequest
	if err := json.Unmarshal(request, &message); err != nil {
		raw, marshalErr := marshalServerError(nil, -32700, "parse error", nil)
		if marshalErr != nil {
			return nil, false, marshalErr
		}
		return raw, true, nil
	}
	if len(message.ID) == 0 {
		return nil, false, nil
	}

	result, rpcErr := s.dispatch(ctx, message.Method, message.Params)
	if rpcErr != nil {
		raw, err := marshalServerError(message.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		return raw, true, err
	}
	raw, err := marshalServerResult(message.ID, result)
	return raw, true, err
}

// HandleNotification handles one MCP server-to-client JSON-RPC notification.
func (s *CapabilityServer) HandleNotification(ctx context.Context, notification json.RawMessage) (ok bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s == nil {
		s = NewCapabilityServer()
	}

	var message serverRequest
	if err := json.Unmarshal(notification, &message); err != nil {
		return false, fmt.Errorf("mcp: decode json-rpc notification: %w", err)
	}
	if len(message.ID) != 0 {
		return false, nil
	}

	switch message.Method {
	case "notifications/elicitation/complete":
		if s.elicitationComplete == nil {
			return false, nil
		}
		parsed, err := elicitationCompleteNotificationFromParams(message.Params)
		if err != nil {
			return true, err
		}
		if err := s.elicitationComplete.Complete(ctx, parsed); err != nil {
			return true, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func (s *CapabilityServer) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "sampling/createMessage":
		if s.sampling == nil {
			return nil, &RPCError{Code: -32601, Message: "method not found"}
		}
		request, rpcErr := samplingRequestFromParams(params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		response, err := s.sampling.CreateMessage(ctx, request)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return samplingResultFromResponse(response), nil
	case "elicitation/create":
		if s.elicitation == nil {
			return nil, &RPCError{Code: -32601, Message: "method not found"}
		}
		request, rpcErr := elicitationRequestFromParams(params)
		if rpcErr != nil {
			return nil, rpcErr
		}
		response, err := s.elicitation.Elicit(ctx, request)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return response, nil
	default:
		return nil, &RPCError{Code: -32601, Message: "method not found"}
	}
}

type samplingCreateMessageParams struct {
	Messages         []mcpPromptMessage  `json:"messages,omitempty"`
	ModelPreferences ModelPreferences    `json:"modelPreferences,omitempty"`
	SystemPrompt     string              `json:"systemPrompt,omitempty"`
	IncludeContext   string              `json:"includeContext,omitempty"`
	MaxTokens        int                 `json:"maxTokens,omitempty"`
	Tools            []mcpToolDescriptor `json:"tools,omitempty"`
	ToolChoice       ToolChoice          `json:"toolChoice,omitempty"`
	Meta             map[string]any      `json:"_meta,omitempty"`
}

func samplingRequestFromParams(params json.RawMessage) (SamplingRequest, *RPCError) {
	var input samplingCreateMessageParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return SamplingRequest{}, &RPCError{Code: -32602, Message: "invalid sampling params"}
		}
	}
	messages := make([]gopact.Message, 0, len(input.Messages))
	for _, message := range input.Messages {
		converted, err := message.message()
		if err != nil {
			return SamplingRequest{}, &RPCError{Code: -32602, Message: "invalid sampling message"}
		}
		messages = append(messages, converted)
	}
	toolInfos := make([]ToolInfo, 0, len(input.Tools))
	for _, tool := range input.Tools {
		toolInfos = append(toolInfos, ToolInfo{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      gopact.JSONSchema(copyAnyMap(tool.InputSchema)),
			Metadata:    copyAnyMap(tool.Meta),
		})
	}
	return SamplingRequest{
		Messages:         messages,
		ModelPreferences: input.ModelPreferences,
		SystemPrompt:     input.SystemPrompt,
		IncludeContext:   input.IncludeContext,
		MaxTokens:        input.MaxTokens,
		Tools:            toolInfos,
		ToolChoice:       input.ToolChoice,
		Metadata:         copyAnyMap(input.Meta),
	}, nil
}

type samplingCreateMessageResult struct {
	Role       string         `json:"role,omitempty"`
	Content    any            `json:"content,omitempty"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stopReason,omitempty"`
	Meta       map[string]any `json:"_meta,omitempty"`
}

func samplingResultFromResponse(response SamplingResponse) samplingCreateMessageResult {
	return samplingCreateMessageResult{
		Role:       string(response.Role),
		Content:    samplingContentToMCP(response.Content),
		Model:      response.Model,
		StopReason: response.StopReason,
		Meta:       copyAnyMap(response.Metadata),
	}
}

func samplingContentToMCP(parts []gopact.ContentPart) any {
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return contentPartToMCP(parts[0])
	default:
		out := make([]mcpContentPart, 0, len(parts))
		for _, part := range parts {
			out = append(out, contentPartToMCP(part))
		}
		return out
	}
}

func contentPartToMCP(part gopact.ContentPart) mcpContentPart {
	return mcpContentPart{
		Type:     string(part.Type),
		Text:     part.Text,
		URI:      part.URI,
		Name:     part.Name,
		MIMEType: part.MIMEType,
		Meta:     copyAnyMap(part.Metadata),
	}
}

func elicitationRequestFromParams(params json.RawMessage) (ElicitationRequest, *RPCError) {
	var input ElicitationRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return ElicitationRequest{}, &RPCError{Code: -32602, Message: "invalid elicitation params"}
		}
	}
	return copyElicitationRequest(input), nil
}

func elicitationCompleteNotificationFromParams(params json.RawMessage) (ElicitationCompleteNotification, error) {
	var input ElicitationCompleteNotification
	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return ElicitationCompleteNotification{}, fmt.Errorf("mcp: decode elicitation complete notification: %w", err)
		}
	}
	if input.ElicitationID == "" {
		return ElicitationCompleteNotification{}, ErrElicitationCompleteIDRequired
	}
	return copyElicitationCompleteNotification(input), nil
}
