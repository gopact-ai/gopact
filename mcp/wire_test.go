package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestLineTransportCallWritesJSONRPCRequestAndDecodesResponse(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}` + "\n"
	writer := new(bytes.Buffer)
	transport, err := NewLineTransport(strings.NewReader(response), writer)
	if err != nil {
		t.Fatalf("NewLineTransport() error = %v", err)
	}

	var got struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := transport.Call(context.Background(), "tools/list", map[string]any{"cursor": ""}, &got); err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if len(got.Tools) != 1 || got.Tools[0].Name != "git.status" {
		t.Fatalf("result = %+v, want git.status", got)
	}

	var request struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      int64          `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(writer.Bytes()), &request); err != nil {
		t.Fatalf("written request is not json: %v", err)
	}
	if request.JSONRPC != "2.0" || request.ID != 1 || request.Method != "tools/list" {
		t.Fatalf("request = %+v, want jsonrpc 2.0 id 1 tools/list", request)
	}
	if request.Params["cursor"] != "" {
		t.Fatalf("params = %+v, want cursor", request.Params)
	}
}

func TestLineTransportCallHandlesInterleavedCapabilityRequest(t *testing.T) {
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":"sample-1","method":"sampling/createMessage","params":{"messages":[{"role":"user","content":{"type":"text","text":"ping"}}],"maxTokens":16}}` + "\n" +
			`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}` + "\n",
	)
	writer := new(bytes.Buffer)
	capabilities := NewCapabilityServer(WithSamplingHandler(SamplingHandlerFunc(func(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
		if len(request.Messages) != 1 || request.Messages[0].Text() != "ping" {
			t.Fatalf("sampling messages = %+v, want ping", request.Messages)
		}
		return SamplingResponse{
			Role:    gopact.RoleAssistant,
			Content: []gopact.ContentPart{gopact.TextPart("pong")},
			Model:   "test-model",
		}, nil
	})))
	transport, err := NewLineTransport(input, writer, WithLineTransportRequestHandler(capabilities))
	if err != nil {
		t.Fatalf("NewLineTransport() error = %v", err)
	}

	var got struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := transport.Call(context.Background(), "tools/list", nil, &got); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "git.status" {
		t.Fatalf("result = %+v, want git.status", got)
	}

	lines := strings.Split(strings.TrimSpace(writer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("written lines = %v, want request and capability response", lines)
	}
	var request struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &request); err != nil {
		t.Fatalf("client request decode error = %v", err)
	}
	if request.Method != "tools/list" {
		t.Fatalf("client request method = %q, want tools/list", request.Method)
	}
	var response struct {
		ID     string `json:"id"`
		Result struct {
			Role    string         `json:"role"`
			Content mcpContentPart `json:"content"`
			Model   string         `json:"model"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &response); err != nil {
		t.Fatalf("capability response decode error = %v", err)
	}
	if response.ID != "sample-1" || response.Result.Content.Text != "pong" || response.Result.Model != "test-model" {
		t.Fatalf("capability response = %+v, want sample pong", response)
	}
}

func TestLineTransportCallHandlesElicitationCompleteNotification(t *testing.T) {
	input := strings.NewReader(
		`{"jsonrpc":"2.0","method":"notifications/elicitation/complete","params":{"elicitationId":"elicit-1"}}` + "\n" +
			`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}` + "\n",
	)
	writer := new(bytes.Buffer)
	var got ElicitationCompleteNotification
	capabilities := NewCapabilityServer(WithElicitationCompleteHandler(ElicitationCompleteHandlerFunc(func(ctx context.Context, notification ElicitationCompleteNotification) error {
		got = notification
		return nil
	})))
	transport, err := NewLineTransport(input, writer, WithLineTransportNotificationHandler(capabilities))
	if err != nil {
		t.Fatalf("NewLineTransport() error = %v", err)
	}

	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := transport.Call(context.Background(), "tools/list", nil, &result); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if got.ElicitationID != "elicit-1" {
		t.Fatalf("completion notification = %+v, want elicit-1", got)
	}
	lines := strings.Split(strings.TrimSpace(writer.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("written lines = %v, want only client request", lines)
	}
}

func TestLineTransportCallMapsJSONRPCError(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found","data":{"method":"tools/list"}}}` + "\n"
	transport, err := NewLineTransport(strings.NewReader(response), io.Discard)
	if err != nil {
		t.Fatalf("NewLineTransport() error = %v", err)
	}

	err = transport.Call(context.Background(), "tools/list", nil, nil)
	if !errors.Is(err, ErrJSONRPC) {
		t.Fatalf("Call() error = %v, want ErrJSONRPC", err)
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("Call() error type = %T, want *RPCError", err)
	}
	if rpcErr.Code != -32601 || rpcErr.Message != "method not found" {
		t.Fatalf("RPCError = %+v, want method not found", rpcErr)
	}
}

func TestLineTransportNotifyWritesJSONRPCNotification(t *testing.T) {
	writer := new(bytes.Buffer)
	transport, err := NewLineTransport(strings.NewReader(""), writer)
	if err != nil {
		t.Fatalf("NewLineTransport() error = %v", err)
	}

	if err := transport.Notify(context.Background(), "notifications/initialized", nil); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	var notification struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(writer.Bytes()), &notification); err != nil {
		t.Fatalf("written notification is not json: %v", err)
	}
	if notification.JSONRPC != "2.0" || notification.Method != "notifications/initialized" {
		t.Fatalf("notification = %+v, want initialized notification", notification)
	}
	if len(notification.ID) != 0 {
		t.Fatalf("notification id = %s, want omitted", notification.ID)
	}
}

func TestNewLineTransportRequiresIO(t *testing.T) {
	if _, err := NewLineTransport(nil, io.Discard); !errors.Is(err, ErrReaderRequired) {
		t.Fatalf("NewLineTransport(nil, writer) error = %v, want ErrReaderRequired", err)
	}
	if _, err := NewLineTransport(strings.NewReader(""), nil); !errors.Is(err, ErrWriterRequired) {
		t.Fatalf("NewLineTransport(reader, nil) error = %v, want ErrWriterRequired", err)
	}
}

func TestLineTransportCloseCallsCloserAndRejectsFurtherCalls(t *testing.T) {
	closer := &recordingCloser{}
	transport, err := NewLineTransport(
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`+"\n"),
		io.Discard,
		WithLineTransportCloser(closer),
	)
	if err != nil {
		t.Fatalf("NewLineTransport() error = %v", err)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closer.closed {
		t.Fatalf("closer closed = false, want true")
	}
	if err := transport.Call(context.Background(), "tools/list", nil, nil); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Call() after Close error = %v, want ErrTransportClosed", err)
	}
}

func TestJSONRPCClientToolsMapsMCPToolDescriptors(t *testing.T) {
	transport := &recordingTransport{
		results: map[string]json.RawMessage{
			"tools/list": json.RawMessage(`{"tools":[{"name":"git.status","description":"show status","inputSchema":{"type":"object","properties":{"short":{"type":"boolean"}}},"_meta":{"scope":"repo"}}]}`),
		},
	}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	tools, err := client.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Name != "git.status" || tools[0].Description != "show status" {
		t.Fatalf("tool = %+v, want git.status descriptor", tools[0])
	}
	if tools[0].Schema["type"] != "object" {
		t.Fatalf("schema = %+v, want object schema", tools[0].Schema)
	}
	if tools[0].Metadata["scope"] != "repo" {
		t.Fatalf("metadata = %+v, want scope repo", tools[0].Metadata)
	}
	if got := transport.calls[0].method; got != "tools/list" {
		t.Fatalf("method = %q, want tools/list", got)
	}
}

func TestJSONRPCClientCallToolMapsContentAndArguments(t *testing.T) {
	transport := &recordingTransport{
		results: map[string]json.RawMessage{
			"tools/call": json.RawMessage(`{"content":[{"type":"text","text":"clean"},{"type":"image","uri":"file://plot.png","mimeType":"image/png"}],"_meta":{"duration_ms":12}}`),
		},
	}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	result, err := client.CallTool(context.Background(), "git.status", json.RawMessage(`{"short":true}`))
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	if len(result.Content) != 2 {
		t.Fatalf("content len = %d, want 2", len(result.Content))
	}
	if result.Content[0].Type != gopact.ContentPartText || result.Content[0].Text != "clean" {
		t.Fatalf("text content = %+v, want clean text", result.Content[0])
	}
	if result.Content[1].Type != gopact.ContentPartImage || result.Content[1].URI != "file://plot.png" || result.Content[1].MIMEType != "image/png" {
		t.Fatalf("image content = %+v, want file image", result.Content[1])
	}
	if result.Metadata["duration_ms"].(float64) != 12 {
		t.Fatalf("metadata = %+v, want duration", result.Metadata)
	}

	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(transport.calls[0].params, &params); err != nil {
		t.Fatalf("params decode error = %v", err)
	}
	if params.Name != "git.status" || string(params.Arguments) != `{"short":true}` {
		t.Fatalf("params = %+v args=%s, want git.status short true", params, params.Arguments)
	}
}

func TestJSONRPCClientReadResourceDecodesTextAndBlobContent(t *testing.T) {
	blob := base64.StdEncoding.EncodeToString([]byte("binary"))
	transport := &recordingTransport{
		results: map[string]json.RawMessage{
			"resources/read": json.RawMessage(`{"contents":[{"uri":"repo://README.md","mimeType":"text/markdown","text":"hello"},{"uri":"repo://data.bin","mimeType":"application/octet-stream","blob":"` + blob + `"}]}`),
		},
	}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	content, err := client.ReadResource(context.Background(), "repo://README.md")
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}

	if content.URI != "repo://README.md" || content.MIMEType != "text/markdown" || content.Text != "hello" {
		t.Fatalf("content = %+v, want text README", content)
	}
	if content.Metadata["content_count"].(float64) != 2 {
		t.Fatalf("metadata = %+v, want content_count 2", content.Metadata)
	}

	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(transport.calls[0].params, &params); err != nil {
		t.Fatalf("params decode error = %v", err)
	}
	if params.URI != "repo://README.md" {
		t.Fatalf("params uri = %q, want repo://README.md", params.URI)
	}
}

func TestJSONRPCClientGetPromptMapsMessages(t *testing.T) {
	transport := &recordingTransport{
		results: map[string]json.RawMessage{
			"prompts/get": json.RawMessage(`{"messages":[{"role":"user","content":{"type":"text","text":"review this"}},{"role":"assistant","content":[{"type":"text","text":"ok"}]}],"_meta":{"source":"server"}}`),
		},
	}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	prompt, err := client.GetPrompt(context.Background(), "git.review", map[string]any{"scope": "diff"})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}

	want := []gopact.Message{
		{Role: gopact.RoleUser, Parts: []gopact.ContentPart{gopact.TextPart("review this")}},
		{Role: gopact.RoleAssistant, Parts: []gopact.ContentPart{gopact.TextPart("ok")}},
	}
	if !reflect.DeepEqual(prompt.Messages, want) {
		t.Fatalf("messages = %+v, want %+v", prompt.Messages, want)
	}
	if prompt.Metadata["source"] != "server" {
		t.Fatalf("metadata = %+v, want source server", prompt.Metadata)
	}
}

func TestJSONRPCClientInitializeSendsHandshakeAndInitializedNotification(t *testing.T) {
	transport := &recordingTransport{
		results: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2025-11-25","serverInfo":{"name":"repo","version":"0.1.0"},"capabilities":{"tools":{}}}`),
		},
	}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	result, err := client.Initialize(context.Background(), InitializeParams{
		ProtocolVersion: "2025-11-25",
		ClientInfo:      PeerInfo{Name: "gopact", Version: "0.1.0"},
		Capabilities:    map[string]any{"roots": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if result.ProtocolVersion != "2025-11-25" || result.ServerInfo.Name != "repo" {
		t.Fatalf("Initialize() = %+v, want repo server", result)
	}
	if got := transport.calls[0].method; got != "initialize" {
		t.Fatalf("method = %q, want initialize", got)
	}
	if got := transport.notifications[0].method; got != "notifications/initialized" {
		t.Fatalf("notification method = %q, want notifications/initialized", got)
	}
}

func TestJSONRPCClientInitializeRequiresNotificationTransport(t *testing.T) {
	transport := &callOnlyTransport{
		results: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2025-11-25"}`),
		},
	}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	_, err = client.Initialize(context.Background(), InitializeParams{ProtocolVersion: "2025-11-25"})
	if !errors.Is(err, ErrNotifierRequired) {
		t.Fatalf("Initialize() error = %v, want ErrNotifierRequired", err)
	}
}

func TestNewJSONRPCClientRequiresTransport(t *testing.T) {
	if _, err := NewJSONRPCClient(nil); !errors.Is(err, ErrTransportRequired) {
		t.Fatalf("NewJSONRPCClient(nil) error = %v, want ErrTransportRequired", err)
	}
}

func TestJSONRPCClientCloseDelegatesTransport(t *testing.T) {
	transport := &recordingTransport{}
	client, err := NewJSONRPCClient(transport)
	if err != nil {
		t.Fatalf("NewJSONRPCClient() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !transport.closed {
		t.Fatalf("transport closed = false, want true")
	}
}

type recordedCall struct {
	method string
	params json.RawMessage
}

type recordedNotification struct {
	method string
	params json.RawMessage
}

type recordingTransport struct {
	calls         []recordedCall
	notifications []recordedNotification
	results       map[string]json.RawMessage
	err           error
	closed        bool
}

func (t *recordingTransport) Call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.err != nil {
		return t.err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	t.calls = append(t.calls, recordedCall{method: method, params: raw})
	if result == nil {
		return nil
	}
	return json.Unmarshal(t.results[method], result)
}

func (t *recordingTransport) Close() error {
	t.closed = true
	return nil
}

func (t *recordingTransport) Notify(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	t.notifications = append(t.notifications, recordedNotification{method: method, params: raw})
	return nil
}

type callOnlyTransport struct {
	results map[string]json.RawMessage
}

func (t *callOnlyTransport) Call(ctx context.Context, method string, _ any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(t.results[method], result)
}

func (t *callOnlyTransport) Close() error {
	return nil
}

type recordingCloser struct {
	closed bool
}

func (c *recordingCloser) Close() error {
	c.closed = true
	return nil
}
