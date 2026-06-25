package mcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/gopact-ai/gopact"
)

const (
	defaultLineTransportMaxMessageBytes = 4 << 20
)

var (
	ErrTransportRequired = errors.New("mcp: transport is required")
	ErrReaderRequired    = errors.New("mcp: reader is required")
	ErrWriterRequired    = errors.New("mcp: writer is required")
	ErrTransportClosed   = errors.New("mcp: transport is closed")
	ErrMessageTooLarge   = errors.New("mcp: message too large")
	ErrJSONRPC           = errors.New("mcp: json-rpc error")
	ErrNotifierRequired  = errors.New("mcp: notifier is required")
)

// JSONRPCTransport is the minimal request/response wire port used by JSONRPCClient.
type JSONRPCTransport interface {
	Call(ctx context.Context, method string, params any, result any) error
	Close() error
}

// JSONRPCNotifier is implemented by transports that can send JSON-RPC notifications.
type JSONRPCNotifier interface {
	Notify(ctx context.Context, method string, params any) error
}

// JSONRPCRequestHandler handles inbound JSON-RPC requests received while a transport is reading.
type JSONRPCRequestHandler interface {
	Handle(ctx context.Context, request json.RawMessage) (response json.RawMessage, ok bool, err error)
}

// JSONRPCNotificationHandler handles inbound JSON-RPC notifications received while reading.
type JSONRPCNotificationHandler interface {
	HandleNotification(ctx context.Context, notification json.RawMessage) (ok bool, err error)
}

// RPCError is a JSON-RPC error response.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ErrJSONRPC.Error()
	}
	if e.Message == "" {
		return fmt.Sprintf("%s: code %d", ErrJSONRPC, e.Code)
	}
	return fmt.Sprintf("%s: code %d: %s", ErrJSONRPC, e.Code, e.Message)
}

func (e *RPCError) Is(target error) bool {
	return target == ErrJSONRPC
}

// LineTransport speaks newline-delimited JSON-RPC 2.0 over an io.Reader/io.Writer pair.
type LineTransport struct {
	mu              sync.Mutex
	reader          *bufio.Reader
	writer          io.Writer
	closer          io.Closer
	nextID          int64
	closed          bool
	maxMessageBytes int
	requestHandler  JSONRPCRequestHandler
	notifyHandler   JSONRPCNotificationHandler
}

// LineTransportOption configures a LineTransport.
type LineTransportOption func(*LineTransport)

// WithLineTransportCloser sets the closer called by Close.
func WithLineTransportCloser(closer io.Closer) LineTransportOption {
	return func(t *LineTransport) {
		t.closer = closer
	}
}

// WithLineTransportMaxMessageBytes sets the maximum accepted JSON-RPC message size.
func WithLineTransportMaxMessageBytes(n int) LineTransportOption {
	return func(t *LineTransport) {
		if n > 0 {
			t.maxMessageBytes = n
		}
	}
}

// WithLineTransportRequestHandler handles inbound server-to-client requests while waiting for responses.
func WithLineTransportRequestHandler(handler JSONRPCRequestHandler) LineTransportOption {
	return func(t *LineTransport) {
		t.requestHandler = handler
	}
}

// WithLineTransportNotificationHandler handles inbound server-to-client notifications while reading.
func WithLineTransportNotificationHandler(handler JSONRPCNotificationHandler) LineTransportOption {
	return func(t *LineTransport) {
		t.notifyHandler = handler
	}
}

// NewLineTransport creates a newline-delimited JSON-RPC transport.
func NewLineTransport(reader io.Reader, writer io.Writer, opts ...LineTransportOption) (*LineTransport, error) {
	if reader == nil {
		return nil, ErrReaderRequired
	}
	if writer == nil {
		return nil, ErrWriterRequired
	}
	transport := &LineTransport{
		reader:          bufio.NewReader(reader),
		writer:          writer,
		maxMessageBytes: defaultLineTransportMaxMessageBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(transport)
		}
	}
	return transport, nil
}

// Call sends one JSON-RPC request and decodes the matching response.
func (t *LineTransport) Call(ctx context.Context, method string, params any, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrTransportClosed
	}
	t.nextID++
	id := t.nextID
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("mcp: encode json-rpc request: %w", err)
	}
	if err := t.writeMessage(payload); err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := t.readLine()
		if err != nil {
			return err
		}
		handled, err := t.handleIncomingRequest(ctx, line)
		if err != nil {
			return err
		}
		if handled {
			continue
		}
		handled, err = t.handleIncomingNotification(ctx, line)
		if err != nil {
			return err
		}
		if handled {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			return fmt.Errorf("mcp: decode json-rpc response: %w", err)
		}
		if len(response.ID) == 0 {
			continue
		}
		if !jsonRPCIDMatches(response.ID, id) {
			return fmt.Errorf("mcp: unexpected json-rpc response id %s", response.ID)
		}
		if response.Error != nil {
			return response.Error
		}
		if result == nil || len(response.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("mcp: decode json-rpc result: %w", err)
		}
		return nil
	}
}

// Notify sends one JSON-RPC notification with no id and waits for no response.
func (t *LineTransport) Notify(ctx context.Context, method string, params any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrTransportClosed
	}
	notification := rpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("mcp: encode json-rpc notification: %w", err)
	}
	return t.writeMessage(payload)
}

func (t *LineTransport) handleIncomingRequest(ctx context.Context, line json.RawMessage) (bool, error) {
	var request serverRequest
	if err := json.Unmarshal(line, &request); err != nil || request.Method == "" || len(request.ID) == 0 {
		return false, nil
	}
	if t.requestHandler == nil {
		return false, nil
	}
	response, ok, err := t.requestHandler.Handle(ctx, append(json.RawMessage(nil), line...))
	if err != nil {
		return true, fmt.Errorf("mcp: handle inbound json-rpc request: %w", err)
	}
	if !ok {
		return true, nil
	}
	return true, t.writeMessage(response)
}

func (t *LineTransport) handleIncomingNotification(ctx context.Context, line json.RawMessage) (bool, error) {
	var notification serverRequest
	if err := json.Unmarshal(line, &notification); err != nil || notification.Method == "" || len(notification.ID) != 0 {
		return false, nil
	}
	if t.notifyHandler == nil {
		return false, nil
	}
	ok, err := t.notifyHandler.HandleNotification(ctx, append(json.RawMessage(nil), line...))
	if err != nil {
		return true, fmt.Errorf("mcp: handle inbound json-rpc notification: %w", err)
	}
	return ok, nil
}

// Close marks the transport closed and closes the configured closer.
func (t *LineTransport) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.closer == nil {
		return nil
	}
	if err := t.closer.Close(); err != nil {
		return fmt.Errorf("mcp: close transport: %w", err)
	}
	return nil
}

func (t *LineTransport) writeMessage(payload []byte) error {
	if len(payload) > t.maxMessageBytes {
		return ErrMessageTooLarge
	}
	payload = append(payload, '\n')
	if _, err := t.writer.Write(payload); err != nil {
		return fmt.Errorf("mcp: write json-rpc message: %w", err)
	}
	return nil
}

func (t *LineTransport) readLine() ([]byte, error) {
	var line []byte
	for {
		part, isPrefix, err := t.reader.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("mcp: read json-rpc response: %w", err)
		}
		if len(line)+len(part) > t.maxMessageBytes {
			return nil, ErrMessageTooLarge
		}
		line = append(line, part...)
		if !isPrefix {
			break
		}
	}
	return line, nil
}

func jsonRPCIDMatches(raw json.RawMessage, id int64) bool {
	var gotNumber int64
	if err := json.Unmarshal(raw, &gotNumber); err == nil {
		return gotNumber == id
	}
	var gotString string
	if err := json.Unmarshal(raw, &gotString); err == nil {
		return gotString == fmt.Sprintf("%d", id)
	}
	return false
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// JSONRPCClient maps the MCP client contract onto JSON-RPC method calls.
type JSONRPCClient struct {
	transport JSONRPCTransport
}

var _ Client = (*JSONRPCClient)(nil)

// NewJSONRPCClient creates an MCP client backed by a JSON-RPC transport.
func NewJSONRPCClient(transport JSONRPCTransport) (*JSONRPCClient, error) {
	if transport == nil {
		return nil, ErrTransportRequired
	}
	return &JSONRPCClient{transport: transport}, nil
}

// Close closes the underlying transport.
func (c *JSONRPCClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// PeerInfo identifies one MCP peer in initialize payloads.
type PeerInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// InitializeParams is the client initialize request payload.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ClientInfo      PeerInfo       `json:"clientInfo,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	Meta            map[string]any `json:"_meta,omitempty"`
}

// InitializeResult is the server initialize response payload.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ServerInfo      PeerInfo       `json:"serverInfo,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	Instructions    string         `json:"instructions,omitempty"`
	Meta            map[string]any `json:"_meta,omitempty"`
}

// Initialize performs the legacy MCP initialize handshake and sends notifications/initialized.
func (c *JSONRPCClient) Initialize(ctx context.Context, params InitializeParams) (InitializeResult, error) {
	if err := c.requireTransport(); err != nil {
		return InitializeResult{}, err
	}
	var result InitializeResult
	if err := c.transport.Call(ctx, "initialize", params, &result); err != nil {
		return InitializeResult{}, err
	}
	notifier, ok := c.transport.(JSONRPCNotifier)
	if !ok {
		return InitializeResult{}, ErrNotifierRequired
	}
	if err := notifier.Notify(ctx, "notifications/initialized", nil); err != nil {
		return InitializeResult{}, err
	}
	result.Capabilities = copyAnyMap(result.Capabilities)
	result.Meta = copyAnyMap(result.Meta)
	return result, nil
}

// Tools lists MCP tools through tools/list.
func (c *JSONRPCClient) Tools(ctx context.Context) ([]ToolInfo, error) {
	if err := c.requireTransport(); err != nil {
		return nil, err
	}
	var result toolsListResult
	if err := c.transport.Call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	tools := make([]ToolInfo, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, ToolInfo{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      gopact.JSONSchema(copyAnyMap(tool.InputSchema)),
			Metadata:    copyAnyMap(tool.Meta),
		})
	}
	return tools, nil
}

// CallTool invokes an MCP tool through tools/call.
func (c *JSONRPCClient) CallTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	if err := c.requireTransport(); err != nil {
		return ToolResult{}, err
	}
	params := toolCallParams{Name: name}
	if len(args) > 0 {
		params.Arguments = append(json.RawMessage(nil), args...)
	}
	var result toolCallResult
	if err := c.transport.Call(ctx, "tools/call", params, &result); err != nil {
		return ToolResult{}, err
	}
	metadata := copyAnyMap(result.Meta)
	if result.IsError {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["is_error"] = true
	}
	return ToolResult{
		Name:      name,
		Content:   contentPartsFromMCP(result.Content),
		Artifacts: append([]gopact.ArtifactRef(nil), result.Artifacts...),
		Metadata:  metadata,
	}, nil
}

// Resources lists MCP resources through resources/list.
func (c *JSONRPCClient) Resources(ctx context.Context) ([]Resource, error) {
	if err := c.requireTransport(); err != nil {
		return nil, err
	}
	var result resourcesListResult
	if err := c.transport.Call(ctx, "resources/list", nil, &result); err != nil {
		return nil, err
	}
	resources := make([]Resource, 0, len(result.Resources))
	for _, resource := range result.Resources {
		resources = append(resources, Resource{
			URI:      resource.URI,
			Name:     resource.Name,
			MIMEType: resource.mimeType(),
			Metadata: copyAnyMap(resource.Meta),
		})
	}
	return resources, nil
}

// ReadResource reads one MCP resource through resources/read.
func (c *JSONRPCClient) ReadResource(ctx context.Context, uri string) (ResourceContent, error) {
	if err := c.requireTransport(); err != nil {
		return ResourceContent{}, err
	}
	var result resourceReadResult
	if err := c.transport.Call(ctx, "resources/read", resourceReadParams{URI: uri}, &result); err != nil {
		return ResourceContent{}, err
	}
	if len(result.Contents) == 0 {
		return ResourceContent{URI: uri, Metadata: copyAnyMap(result.Meta)}, nil
	}
	content, err := result.Contents[0].resourceContent()
	if err != nil {
		return ResourceContent{}, err
	}
	if content.URI == "" {
		content.URI = uri
	}
	content.Metadata = mergeAnyMaps(result.Meta, content.Metadata)
	if len(result.Contents) > 1 {
		if content.Metadata == nil {
			content.Metadata = make(map[string]any)
		}
		content.Metadata["content_count"] = float64(len(result.Contents))
	}
	return content, nil
}

// Prompts lists MCP prompts through prompts/list.
func (c *JSONRPCClient) Prompts(ctx context.Context) ([]Prompt, error) {
	if err := c.requireTransport(); err != nil {
		return nil, err
	}
	var result promptsListResult
	if err := c.transport.Call(ctx, "prompts/list", nil, &result); err != nil {
		return nil, err
	}
	prompts := make([]Prompt, 0, len(result.Prompts))
	for _, prompt := range result.Prompts {
		prompts = append(prompts, Prompt{
			Name:        prompt.Name,
			Description: prompt.Description,
			Metadata:    copyAnyMap(prompt.Meta),
		})
	}
	return prompts, nil
}

// GetPrompt gets one MCP prompt through prompts/get.
func (c *JSONRPCClient) GetPrompt(ctx context.Context, name string, args map[string]any) (PromptContent, error) {
	if err := c.requireTransport(); err != nil {
		return PromptContent{}, err
	}
	var result promptGetResult
	if err := c.transport.Call(ctx, "prompts/get", promptGetParams{Name: name, Arguments: copyAnyMap(args)}, &result); err != nil {
		return PromptContent{}, err
	}
	messages := make([]gopact.Message, 0, len(result.Messages))
	for _, message := range result.Messages {
		converted, err := message.message()
		if err != nil {
			return PromptContent{}, err
		}
		messages = append(messages, converted)
	}
	return PromptContent{
		Name:     name,
		Messages: messages,
		Metadata: copyAnyMap(result.Meta),
	}, nil
}

func (c *JSONRPCClient) requireTransport() error {
	if c == nil || c.transport == nil {
		return ErrTransportRequired
	}
	return nil
}

type toolsListResult struct {
	Tools []mcpToolDescriptor `json:"tools"`
}

type mcpToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content   []mcpContentPart     `json:"content,omitempty"`
	Artifacts []gopact.ArtifactRef `json:"artifacts,omitempty"`
	IsError   bool                 `json:"isError,omitempty"`
	Meta      map[string]any       `json:"_meta,omitempty"`
}

type resourcesListResult struct {
	Resources []mcpResourceDescriptor `json:"resources"`
}

type mcpResourceDescriptor struct {
	URI         string         `json:"uri"`
	Name        string         `json:"name,omitempty"`
	MIMEType    string         `json:"mimeType,omitempty"`
	MIMETypeAlt string         `json:"mime_type,omitempty"`
	Description string         `json:"description,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

func (r mcpResourceDescriptor) mimeType() string {
	if r.MIMEType != "" {
		return r.MIMEType
	}
	return r.MIMETypeAlt
}

type resourceReadParams struct {
	URI string `json:"uri"`
}

type resourceReadResult struct {
	Contents []mcpResourceContent `json:"contents,omitempty"`
	Meta     map[string]any       `json:"_meta,omitempty"`
}

type mcpResourceContent struct {
	URI         string         `json:"uri,omitempty"`
	MIMEType    string         `json:"mimeType,omitempty"`
	MIMETypeAlt string         `json:"mime_type,omitempty"`
	Text        string         `json:"text,omitempty"`
	Blob        string         `json:"blob,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

func (c mcpResourceContent) mimeType() string {
	if c.MIMEType != "" {
		return c.MIMEType
	}
	return c.MIMETypeAlt
}

func (c mcpResourceContent) resourceContent() (ResourceContent, error) {
	out := ResourceContent{
		URI:      c.URI,
		MIMEType: c.mimeType(),
		Text:     c.Text,
		Metadata: copyAnyMap(c.Meta),
	}
	if c.Blob != "" {
		content, err := base64.StdEncoding.DecodeString(c.Blob)
		if err != nil {
			return ResourceContent{}, fmt.Errorf("mcp: decode resource blob: %w", err)
		}
		out.Content = content
	}
	return out, nil
}

type promptsListResult struct {
	Prompts []mcpPromptDescriptor `json:"prompts"`
}

type mcpPromptDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

type promptGetParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type promptGetResult struct {
	Messages []mcpPromptMessage `json:"messages,omitempty"`
	Meta     map[string]any     `json:"_meta,omitempty"`
}

type mcpPromptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content,omitempty"`
}

func (m mcpPromptMessage) message() (gopact.Message, error) {
	parts, err := parseMCPContentParts(m.Content)
	if err != nil {
		return gopact.Message{}, err
	}
	return gopact.Message{
		Role:  gopact.Role(m.Role),
		Parts: parts,
	}, nil
}

type mcpContentPart struct {
	Type        string              `json:"type"`
	Text        string              `json:"text,omitempty"`
	URI         string              `json:"uri,omitempty"`
	Name        string              `json:"name,omitempty"`
	Data        string              `json:"data,omitempty"`
	MIMEType    string              `json:"mimeType,omitempty"`
	MIMETypeAlt string              `json:"mime_type,omitempty"`
	Resource    *mcpResourceContent `json:"resource,omitempty"`
	Meta        map[string]any      `json:"_meta,omitempty"`
}

func (p mcpContentPart) mimeType() string {
	if p.MIMEType != "" {
		return p.MIMEType
	}
	return p.MIMETypeAlt
}

func contentPartsFromMCP(parts []mcpContentPart) []gopact.ContentPart {
	out := make([]gopact.ContentPart, 0, len(parts))
	for _, part := range parts {
		out = append(out, contentPartFromMCP(part))
	}
	return out
}

func contentPartFromMCP(part mcpContentPart) gopact.ContentPart {
	mimeType := part.mimeType()
	switch part.Type {
	case "text":
		return gopact.TextPart(part.Text)
	case "image":
		return contentPartWithMetadata(gopact.ImagePart(contentURI(part.URI, mimeType, part.Data), mimeType), part)
	case "audio":
		return contentPartWithMetadata(gopact.AudioPart(contentURI(part.URI, mimeType, part.Data), mimeType), part)
	case "resource":
		if part.Resource != nil {
			return contentPartWithMetadata(gopact.FilePart(part.Resource.URI, part.Resource.mimeType()), part)
		}
		return contentPartWithMetadata(gopact.FilePart(part.URI, mimeType), part)
	default:
		return contentPartWithMetadata(gopact.ContentPart{
			Type:     gopact.ContentPartType(part.Type),
			Text:     part.Text,
			URI:      part.URI,
			MIMEType: mimeType,
			Name:     part.Name,
		}, part)
	}
}

func contentURI(uri, mimeType, data string) string {
	if uri != "" || data == "" {
		return uri
	}
	if mimeType == "" {
		return "data:;base64," + data
	}
	return "data:" + mimeType + ";base64," + data
}

func contentPartWithMetadata(out gopact.ContentPart, part mcpContentPart) gopact.ContentPart {
	out.Name = part.Name
	out.Metadata = copyAnyMap(part.Meta)
	return out
}

func parseMCPContentParts(raw json.RawMessage) ([]gopact.ContentPart, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []gopact.ContentPart{gopact.TextPart(text)}, nil
	}
	var single mcpContentPart
	if err := json.Unmarshal(raw, &single); err == nil && single.Type != "" {
		return []gopact.ContentPart{contentPartFromMCP(single)}, nil
	}
	var multiple []mcpContentPart
	if err := json.Unmarshal(raw, &multiple); err != nil {
		return nil, fmt.Errorf("mcp: decode prompt content: %w", err)
	}
	return contentPartsFromMCP(multiple), nil
}

func mergeAnyMaps(first, second map[string]any) map[string]any {
	if len(first) == 0 {
		return copyAnyMap(second)
	}
	out := copyAnyMap(first)
	for key, value := range second {
		out[key] = value
	}
	return out
}
