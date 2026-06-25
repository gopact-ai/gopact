package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestHTTPTransportCallPostsJSONRPCRequestAndDecodesJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/json") || !strings.Contains(got, "text/event-stream") {
			t.Fatalf("Accept = %q, want application/json and text/event-stream", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}

		var request struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      int64          `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("request decode error = %v", err)
		}
		if request.JSONRPC != "2.0" || request.ID != 1 || request.Method != "tools/list" {
			t.Fatalf("request = %+v, want jsonrpc 2.0 id 1 tools/list", request)
		}
		if request.Params["cursor"] != "" {
			t.Fatalf("params = %+v, want cursor", request.Params)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}`))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
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
}

func TestHTTPTransportCallDecodesEventStreamResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: prime\n"))
		_, _ = w.Write([]byte("data:\n\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"message":"working"}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}` + "\n\n"))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
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
}

func TestHTTPTransportCallHandlesInterleavedCapabilityRequest(t *testing.T) {
	var requestCount atomic.Int32
	responseBodies := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"sample-1","method":"sampling/createMessage","params":{"messages":[{"role":"user","content":{"type":"text","text":"ping"}}],"maxTokens":16}}` + "\n\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}` + "\n\n"))
		case 2:
			if r.Method != http.MethodPost {
				t.Fatalf("response method = %s, want POST", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read response body error = %v", err)
			}
			responseBodies <- string(body)
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

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
	transport, err := NewHTTPTransport(server.URL, WithHTTPTransportRequestHandler(capabilities))
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
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

	var response struct {
		ID     string `json:"id"`
		Result struct {
			Content mcpContentPart `json:"content"`
			Model   string         `json:"model"`
		} `json:"result"`
	}
	select {
	case body := <-responseBodies:
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatalf("capability response decode error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for capability response POST")
	}
	if response.ID != "sample-1" || response.Result.Content.Text != "pong" || response.Result.Model != "test-model" {
		t.Fatalf("capability response = %+v, want sample pong", response)
	}
}

func TestHTTPTransportCallResumesEventStreamResponseWithGET(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: request-event-1\n"))
			_, _ = w.Write([]byte("retry: 0\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress"}` + "\n\n"))
		case 2:
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if got := r.Header.Get("Last-Event-ID"); got != "request-event-1" {
				t.Fatalf("Last-Event-ID = %q, want request-event-1", got)
			}
			if got := r.Header.Get("Accept"); !strings.Contains(got, "text/event-stream") {
				t.Fatalf("Accept = %q, want text/event-stream", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: request-event-2\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"git.status","description":"show status"}]}}` + "\n\n"))
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
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
	if count := requestCount.Load(); count != 2 {
		t.Fatalf("request count = %d, want 2", count)
	}
}

func TestHTTPTransportNotifyPostsJSONRPCNotificationAndAccepts202(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/json") || !strings.Contains(got, "text/event-stream") {
			t.Fatalf("Accept = %q, want application/json and text/event-stream", got)
		}

		var notification struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
			t.Fatalf("notification decode error = %v", err)
		}
		if notification.JSONRPC != "2.0" || notification.Method != "notifications/initialized" {
			t.Fatalf("notification = %+v, want initialized notification", notification)
		}
		if len(notification.ID) != 0 {
			t.Fatalf("notification id = %s, want omitted", notification.ID)
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	if err := transport.Notify(context.Background(), "notifications/initialized", nil); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
}

func TestHTTPTransportListenGETStreamsServerMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/event-stream") {
			t.Fatalf("Accept = %q, want text/event-stream", got)
		}
		if got := r.Header.Get("Last-Event-ID"); got != "event-0" {
			t.Fatalf("Last-Event-ID = %q, want event-0", got)
		}
		if got := r.Header.Get("MCP-Protocol-Version"); got != "2025-11-25" {
			t.Fatalf("MCP-Protocol-Version = %q, want 2025-11-25", got)
		}
		if got := r.Header.Get("MCP-Session-Id"); got != "session-1" {
			t.Fatalf("MCP-Session-Id = %q, want session-1", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: event-1\n"))
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}` + "\n\n"))
		_, _ = w.Write([]byte("id: event-2\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}` + "\n\n"))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(
		server.URL,
		WithHTTPSessionID("session-1"),
		WithHTTPProtocolVersion("2025-11-25"),
	)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	var events []StreamEvent
	for event, err := range transport.Listen(context.Background(), WithLastEventID("event-0")) {
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].ID != "event-1" || events[0].Event != "message" {
		t.Fatalf("event[0] = %+v, want event-1 message", events[0])
	}
	if !strings.Contains(string(events[0].Data), "notifications/tools/list_changed") {
		t.Fatalf("event[0].Data = %s, want list changed notification", events[0].Data)
	}
	if events[1].ID != "event-2" || !strings.Contains(string(events[1].Data), "notifications/progress") {
		t.Fatalf("event[1] = %+v data=%s, want progress event", events[1], events[1].Data)
	}
}

func TestHTTPTransportListenHandlesRequestsAndYieldsNotifications(t *testing.T) {
	var requestCount atomic.Int32
	responseBodies := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"message":"working"}}` + "\n\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"sample-1","method":"sampling/createMessage","params":{"messages":[{"role":"user","content":{"type":"text","text":"ping"}}],"maxTokens":16}}` + "\n\n"))
		case 2:
			if r.Method != http.MethodPost {
				t.Fatalf("response method = %s, want POST", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read response body error = %v", err)
			}
			responseBodies <- string(body)
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

	capabilities := NewCapabilityServer(WithSamplingHandler(SamplingHandlerFunc(func(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
		return SamplingResponse{
			Role:    gopact.RoleAssistant,
			Content: []gopact.ContentPart{gopact.TextPart("pong")},
			Model:   "test-model",
		}, nil
	})))
	transport, err := NewHTTPTransport(server.URL, WithHTTPTransportRequestHandler(capabilities))
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	var events []StreamEvent
	for event, err := range transport.Listen(context.Background()) {
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		events = append(events, event)
	}
	if len(events) != 1 || !strings.Contains(string(events[0].Data), "notifications/progress") {
		t.Fatalf("events = %+v, want progress notification only", events)
	}

	select {
	case body := <-responseBodies:
		if !strings.Contains(body, `"id":"sample-1"`) || !strings.Contains(body, `"pong"`) {
			t.Fatalf("capability response body = %s, want sample pong", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for capability response POST")
	}
}

func TestHTTPTransportListenHandlesElicitationCompleteNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/elicitation/complete","params":{"elicitationId":"elicit-1","_meta":{"source":"browser"}}}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"message":"still visible"}}` + "\n\n"))
	}))
	defer server.Close()

	var got ElicitationCompleteNotification
	capabilities := NewCapabilityServer(WithElicitationCompleteHandler(ElicitationCompleteHandlerFunc(func(ctx context.Context, notification ElicitationCompleteNotification) error {
		got = notification
		return nil
	})))
	transport, err := NewHTTPTransport(server.URL, WithHTTPTransportNotificationHandler(capabilities))
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	var events []StreamEvent
	for event, err := range transport.Listen(context.Background()) {
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		events = append(events, event)
	}
	if got.ElicitationID != "elicit-1" || got.Metadata["source"] != "browser" {
		t.Fatalf("completion notification = %+v, want elicit-1 browser", got)
	}
	if len(events) != 1 || !strings.Contains(string(events[0].Data), "notifications/progress") {
		t.Fatalf("events = %+v, want progress notification only", events)
	}
}

func TestHTTPTransportListenReturnsUnsupportedOn405(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	var gotErr error
	for _, err := range transport.Listen(context.Background()) {
		gotErr = err
		break
	}
	if !errors.Is(gotErr, ErrHTTPListenUnsupported) {
		t.Fatalf("Listen() error = %v, want ErrHTTPListenUnsupported", gotErr)
	}
}

func TestHTTPTransportListenContinuouslyReconnectsWithLastEventIDAndRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requestCount atomic.Int32
	var sleptMillis atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if got := r.Header.Get("Last-Event-ID"); got != "" {
				t.Fatalf("initial Last-Event-ID = %q, want empty", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: event-1\n"))
			_, _ = w.Write([]byte("retry: 7\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}` + "\n\n"))
		case 2:
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if got := r.Header.Get("Last-Event-ID"); got != "event-1" {
				t.Fatalf("resumed Last-Event-ID = %q, want event-1", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: event-2\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress"}` + "\n\n"))
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	var events []StreamEvent
	for event, err := range transport.ListenContinuously(
		ctx,
		withHTTPListenSleep(func(ctx context.Context, delay time.Duration) error {
			sleptMillis.Add(delay.Milliseconds())
			return nil
		}),
	) {
		if err != nil {
			t.Fatalf("ListenContinuously() error = %v", err)
		}
		events = append(events, event)
		if len(events) == 2 {
			cancel()
		}
	}

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].ID != "event-1" || events[1].ID != "event-2" {
		t.Fatalf("event ids = %q, %q; want event-1, event-2", events[0].ID, events[1].ID)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
	if got := sleptMillis.Load(); got != 7 {
		t.Fatalf("slept ms = %d, want 7", got)
	}
}

func TestHTTPTransportListenContinuouslyStopsWhenConsumerBreaks(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		if got := requestCount.Add(1); got > 1 {
			t.Fatalf("request count = %d, want no reconnect after consumer break", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: event-1\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/progress"}` + "\n\n"))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	for event, err := range transport.ListenContinuously(
		context.Background(),
		WithHTTPListenRetryDelay(0),
	) {
		if err != nil {
			t.Fatalf("ListenContinuously() error = %v", err)
		}
		if event.ID != "event-1" {
			t.Fatalf("event id = %q, want event-1", event.ID)
		}
		break
	}

	if got := requestCount.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestHTTPTransportStoresSessionIDFromInitializeResponse(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("MCP-Session-Id", "session-from-init")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25"}}`))
		case 2:
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if got := r.Header.Get("MCP-Session-Id"); got != "session-from-init" {
				t.Fatalf("MCP-Session-Id = %q, want session-from-init", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n\n"))
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}
	if err := transport.Call(context.Background(), "initialize", InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("Initialize Call() error = %v", err)
	}

	var events []StreamEvent
	for event, err := range transport.Listen(context.Background()) {
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		events = append(events, event)
	}
	if len(events) != 1 || !strings.Contains(string(events[0].Data), "notifications/initialized") {
		t.Fatalf("events = %+v, want initialized notification", events)
	}
}

func TestHTTPTransportTerminateSessionSendsDELETEAndClearsSession(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if r.Method != http.MethodDelete {
				t.Fatalf("method = %s, want DELETE", r.Method)
			}
			if got := r.Header.Get("MCP-Session-Id"); got != "session-1" {
				t.Fatalf("MCP-Session-Id = %q, want session-1", got)
			}
			if got := r.Header.Get("MCP-Protocol-Version"); got != "2025-11-25" {
				t.Fatalf("MCP-Protocol-Version = %q, want 2025-11-25", got)
			}
			w.WriteHeader(http.StatusNoContent)
		case 2:
			if got := r.Header.Get("MCP-Session-Id"); got != "" {
				t.Fatalf("MCP-Session-Id after terminate = %q, want empty", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25"}}`))
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, WithHTTPSessionID("session-1"))
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	if err := transport.TerminateSession(context.Background()); err != nil {
		t.Fatalf("TerminateSession() error = %v", err)
	}
	if err := transport.Call(context.Background(), "initialize", InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("Call() after TerminateSession error = %v", err)
	}
}

func TestHTTPTransportTerminateSessionReturnsUnsupportedOn405(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, WithHTTPSessionID("session-1"))
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	err = transport.TerminateSession(context.Background())
	if !errors.Is(err, ErrHTTPSessionTerminationUnsupported) {
		t.Fatalf("TerminateSession() error = %v, want ErrHTTPSessionTerminationUnsupported", err)
	}
}

func TestHTTPTransportCallClearsSessionOn404WithSession(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestCount.Add(1) {
		case 1:
			if got := r.Header.Get("MCP-Session-Id"); got != "expired-session" {
				t.Fatalf("MCP-Session-Id = %q, want expired-session", got)
			}
			http.Error(w, "session expired", http.StatusNotFound)
		case 2:
			if got := r.Header.Get("MCP-Session-Id"); got != "" {
				t.Fatalf("MCP-Session-Id after 404 = %q, want empty", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"protocolVersion":"2025-11-25"}}`))
		default:
			t.Fatalf("unexpected request %d", requestCount.Load())
		}
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, WithHTTPSessionID("expired-session"))
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	err = transport.Call(context.Background(), "tools/list", nil, nil)
	if !errors.Is(err, ErrHTTPSessionExpired) {
		t.Fatalf("Call() error = %v, want ErrHTTPSessionExpired", err)
	}
	if err := transport.Call(context.Background(), "initialize", InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("Call() after session expired error = %v", err)
	}
}

func TestHTTPTransportCallMapsJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
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

func TestHTTPTransportCallReturnsHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	err = transport.Call(context.Background(), "tools/list", nil, nil)
	if !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("Call() error = %v, want ErrHTTPStatus", err)
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("Call() error type = %T, want *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusForbidden || !strings.Contains(statusErr.Body, "forbidden") {
		t.Fatalf("HTTPStatusError = %+v, want 403 forbidden", statusErr)
	}
}

func TestNewHTTPTransportRequiresEndpoint(t *testing.T) {
	if _, err := NewHTTPTransport(""); !errors.Is(err, ErrEndpointRequired) {
		t.Fatalf("NewHTTPTransport(\"\") error = %v, want ErrEndpointRequired", err)
	}
}

func TestHTTPTransportCloseRejectsFurtherCalls(t *testing.T) {
	transport, err := NewHTTPTransport("https://example.com/mcp")
	if err != nil {
		t.Fatalf("NewHTTPTransport() error = %v", err)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transport.Call(context.Background(), "tools/list", nil, nil); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Call() after Close error = %v, want ErrTransportClosed", err)
	}
}
