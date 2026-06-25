package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/tools"
)

var ErrToolRegistryRequired = errors.New("mcp: tool registry is required")

// ToolServer exposes a tools.Registry through newline-delimited JSON-RPC.
type ToolServer struct {
	registry        *tools.Registry
	info            PeerInfo
	protocolVersion string
	maxMessageBytes int
}

// ToolServerOption configures ToolServer.
type ToolServerOption func(*ToolServer)

// WithToolServerInfo sets the server info returned by initialize.
func WithToolServerInfo(info PeerInfo) ToolServerOption {
	return func(s *ToolServer) {
		s.info = info
	}
}

// WithToolServerProtocolVersion sets the fallback protocol version returned by initialize.
func WithToolServerProtocolVersion(version string) ToolServerOption {
	return func(s *ToolServer) {
		s.protocolVersion = version
	}
}

// WithToolServerMaxMessageBytes sets the maximum accepted JSON-RPC request size.
func WithToolServerMaxMessageBytes(n int) ToolServerOption {
	return func(s *ToolServer) {
		if n > 0 {
			s.maxMessageBytes = n
		}
	}
}

// NewToolServer creates a JSON-RPC MCP server exposing model-visible registry tools.
func NewToolServer(registry *tools.Registry, opts ...ToolServerOption) (*ToolServer, error) {
	if registry == nil {
		return nil, ErrToolRegistryRequired
	}
	server := &ToolServer{
		registry: registry,
		info: PeerInfo{
			Name: "gopact",
		},
		protocolVersion: "2025-11-25",
		maxMessageBytes: defaultLineTransportMaxMessageBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}
	return server, nil
}

// ServeLine handles newline-delimited JSON-RPC requests until EOF.
func (s *ToolServer) ServeLine(ctx context.Context, reader io.Reader, writer io.Writer) error {
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

// Handle handles one JSON-RPC request. Notifications return ok=false.
func (s *ToolServer) Handle(ctx context.Context, request json.RawMessage) (response json.RawMessage, ok bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if s == nil || s.registry == nil {
		return nil, false, ErrToolRegistryRequired
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

func (s *ToolServer) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "initialize":
		return s.initialize(params)
	case "tools/list":
		return s.listTools(ctx)
	case "tools/call":
		return s.callTool(ctx, params)
	case "resources/list":
		return resourcesListResult{}, nil
	case "prompts/list":
		return promptsListResult{}, nil
	default:
		return nil, &RPCError{Code: -32601, Message: "method not found"}
	}
}

func (s *ToolServer) initialize(params json.RawMessage) (InitializeResult, *RPCError) {
	var input InitializeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return InitializeResult{}, &RPCError{Code: -32602, Message: "invalid initialize params"}
		}
	}
	version := input.ProtocolVersion
	if version == "" {
		version = s.protocolVersion
	}
	return InitializeResult{
		ProtocolVersion: version,
		ServerInfo:      s.info,
		Capabilities: map[string]any{
			"tools": map[string]any{},
		},
	}, nil
}

func (s *ToolServer) listTools(ctx context.Context) (toolsListResult, *RPCError) {
	infos, err := s.registry.Visible(ctx, tools.Scope{})
	if err != nil {
		return toolsListResult{}, &RPCError{Code: -32603, Message: err.Error()}
	}
	result := toolsListResult{
		Tools: make([]mcpToolDescriptor, 0, len(infos)),
	}
	for _, info := range infos {
		result.Tools = append(result.Tools, mcpToolDescriptor{
			Name:        info.Name,
			Description: info.Description,
			InputSchema: copyAnyMap(info.Schema),
			Meta:        toolInfoMetadata(info),
		})
	}
	return result, nil
}

func (s *ToolServer) callTool(ctx context.Context, params json.RawMessage) (toolCallResult, *RPCError) {
	var input toolCallParams
	if err := json.Unmarshal(params, &input); err != nil {
		return toolCallResult{}, &RPCError{Code: -32602, Message: "invalid tool call params"}
	}
	result, err := s.registry.InvokeVisible(ctx, input.Name, input.Arguments, tools.Scope{})
	if err != nil {
		return toolCallResult{}, &RPCError{Code: -32602, Message: err.Error()}
	}
	return toolCallResultFromGopact(result), nil
}

func toolCallResultFromGopact(result gopact.ToolResult) toolCallResult {
	metadata := copyAnyMap(result.Metadata)
	if len(result.Effects) > 0 {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["gopact_effects"] = result.Effects
	}
	if len(result.Events) > 0 {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["gopact_events"] = result.Events
	}
	out := toolCallResult{
		Artifacts: append([]gopact.ArtifactRef(nil), result.Artifacts...),
		Meta:      metadata,
	}
	if result.Content != "" {
		out.Content = []mcpContentPart{{Type: "text", Text: result.Content}}
	}
	return out
}

func toolInfoMetadata(info tools.ToolInfo) map[string]any {
	metadata := copyAnyMap(info.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["namespace"] = info.Namespace
	metadata["source"] = info.Source
	metadata["visibility"] = info.Visibility
	return metadata
}

type serverRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type serverResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func marshalServerResult(id json.RawMessage, result any) (json.RawMessage, error) {
	raw, err := json.Marshal(serverResponse{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), id...),
		Result:  result,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode json-rpc response: %w", err)
	}
	return raw, nil
}

func marshalServerError(id json.RawMessage, code int, message string, data json.RawMessage) (json.RawMessage, error) {
	raw, err := json.Marshal(serverResponse{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), id...),
		Error:   &RPCError{Code: code, Message: message, Data: append(json.RawMessage(nil), data...)},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode json-rpc error response: %w", err)
	}
	return raw, nil
}

func readServerLine(reader *bufio.Reader, maxMessageBytes int) ([]byte, error) {
	var line []byte
	for {
		part, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(line)+len(part) > maxMessageBytes {
			return nil, ErrMessageTooLarge
		}
		line = append(line, part...)
		if !isPrefix {
			break
		}
	}
	return line, nil
}
