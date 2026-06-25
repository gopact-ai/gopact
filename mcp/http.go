package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultHTTPTransportMaxResponseBytes = 4 << 20
	defaultHTTPProtocolVersion           = "2025-11-25"
	defaultHTTPListenRetryDelay          = time.Second
	streamableHTTPAcceptHeader           = "application/json, text/event-stream"
	httpSessionIDHeader                  = "MCP-Session-Id"
	httpProtocolVersionHeader            = "MCP-Protocol-Version"
	httpLastEventIDHeader                = "Last-Event-ID"
)

var (
	ErrEndpointRequired                  = errors.New("mcp: endpoint is required")
	ErrHTTPClientRequired                = errors.New("mcp: http client is required")
	ErrHTTPListenUnsupported             = errors.New("mcp: http listen stream is not supported")
	ErrHTTPSessionExpired                = errors.New("mcp: http session expired")
	ErrHTTPSessionTerminationUnsupported = errors.New("mcp: http session termination is not supported")
	ErrHTTPStatus                        = errors.New("mcp: http status error")
	ErrUnsupportedContentType            = errors.New("mcp: unsupported content type")
	ErrHTTPResponseBodyTooLarge          = errors.New("mcp: http response body too large")
	ErrNotificationNotAccepted           = errors.New("mcp: notification not accepted")
	ErrUnexpectedJSONRPCResponse         = errors.New("mcp: unexpected json-rpc response")
)

// HTTPStatusError describes a non-2xx HTTP response from a Streamable HTTP endpoint.
type HTTPStatusError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ErrHTTPStatus.Error()
	}
	if e.Body == "" {
		return fmt.Sprintf("%s: %s", ErrHTTPStatus, e.Status)
	}
	return fmt.Sprintf("%s: %s: %s", ErrHTTPStatus, e.Status, strings.TrimSpace(e.Body))
}

func (e *HTTPStatusError) Is(target error) bool {
	return target == ErrHTTPStatus
}

// HTTPTransport speaks MCP Streamable HTTP using one POST per JSON-RPC message.
type HTTPTransport struct {
	mu               sync.Mutex
	endpoint         string
	client           *http.Client
	nextID           int64
	closed           bool
	sessionID        string
	protocolVersion  string
	maxResponseBytes int
	requestHandler   JSONRPCRequestHandler
	notifyHandler    JSONRPCNotificationHandler
}

var _ JSONRPCTransport = (*HTTPTransport)(nil)
var _ JSONRPCNotifier = (*HTTPTransport)(nil)

// HTTPTransportOption configures HTTPTransport.
type HTTPTransportOption func(*HTTPTransport) error

// WithHTTPClient sets the HTTP client used by the transport.
func WithHTTPClient(client *http.Client) HTTPTransportOption {
	return func(t *HTTPTransport) error {
		if client == nil {
			return ErrHTTPClientRequired
		}
		t.client = client
		return nil
	}
}

// WithHTTPTransportMaxResponseBytes sets the maximum accepted HTTP response body size.
func WithHTTPTransportMaxResponseBytes(n int) HTTPTransportOption {
	return func(t *HTTPTransport) error {
		if n > 0 {
			t.maxResponseBytes = n
		}
		return nil
	}
}

// WithHTTPSessionID sets the MCP session id header sent on HTTP requests.
func WithHTTPSessionID(sessionID string) HTTPTransportOption {
	return func(t *HTTPTransport) error {
		t.sessionID = sessionID
		return nil
	}
}

// WithHTTPProtocolVersion sets the MCP protocol version header sent on HTTP requests.
func WithHTTPProtocolVersion(version string) HTTPTransportOption {
	return func(t *HTTPTransport) error {
		t.protocolVersion = version
		return nil
	}
}

// WithHTTPTransportRequestHandler handles inbound server-to-client requests from SSE streams.
func WithHTTPTransportRequestHandler(handler JSONRPCRequestHandler) HTTPTransportOption {
	return func(t *HTTPTransport) error {
		t.requestHandler = handler
		return nil
	}
}

// WithHTTPTransportNotificationHandler handles inbound server-to-client notifications from SSE streams.
func WithHTTPTransportNotificationHandler(handler JSONRPCNotificationHandler) HTTPTransportOption {
	return func(t *HTTPTransport) error {
		t.notifyHandler = handler
		return nil
	}
}

// NewHTTPTransport creates a Streamable HTTP JSON-RPC transport.
func NewHTTPTransport(endpoint string, opts ...HTTPTransportOption) (*HTTPTransport, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, ErrEndpointRequired
	}
	transport := &HTTPTransport{
		endpoint:         endpoint,
		client:           http.DefaultClient,
		protocolVersion:  defaultHTTPProtocolVersion,
		maxResponseBytes: defaultHTTPTransportMaxResponseBytes,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(transport); err != nil {
			return nil, err
		}
	}
	return transport, nil
}

// Call sends one JSON-RPC request with HTTP POST and decodes a JSON response.
func (t *HTTPTransport) Call(ctx context.Context, method string, params any, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	id, err := t.nextRequestID()
	if err != nil {
		return err
	}
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

	response, hadSession, err := t.post(ctx, payload)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	t.captureSessionID(response)

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return t.statusError(response, hadSession)
	}
	mediaType, err := responseMediaType(response)
	if err != nil {
		return err
	}
	switch mediaType {
	case "", "application/json":
		body, err := readLimited(response.Body, t.maxResponseBytes)
		if err != nil {
			return err
		}
		return decodeExpectedRPCResponse(body, id, result)
	case "text/event-stream":
		return t.decodeEventStreamResponse(ctx, response.Body, id, result)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedContentType, mediaType)
	}
}

// Notify sends one JSON-RPC notification with HTTP POST.
func (t *HTTPTransport) Notify(ctx context.Context, method string, params any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.ensureOpen(); err != nil {
		return err
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

	response, hadSession, err := t.post(ctx, payload)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	t.captureSessionID(response)

	if response.StatusCode == http.StatusAccepted {
		return nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return t.statusError(response, hadSession)
	}
	return fmt.Errorf("%w: %s", ErrNotificationNotAccepted, response.Status)
}

// StreamEvent is one SSE event received from a Streamable HTTP listen stream.
type StreamEvent struct {
	ID    string          `json:"id,omitempty"`
	Event string          `json:"event,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`

	retryAfter    time.Duration
	hasRetryAfter bool
}

// ListenOption configures one Streamable HTTP GET listen stream.
type ListenOption func(*listenOptions) error

type listenOptions struct {
	lastEventID string
	retryDelay  time.Duration
	sleep       func(context.Context, time.Duration) error
}

// WithLastEventID sets the Last-Event-ID header used to resume an SSE stream.
func WithLastEventID(id string) ListenOption {
	return func(o *listenOptions) error {
		o.lastEventID = id
		return nil
	}
}

// WithHTTPListenRetryDelay sets the fallback delay between reconnect attempts.
func WithHTTPListenRetryDelay(delay time.Duration) ListenOption {
	return func(o *listenOptions) error {
		if delay >= 0 {
			o.retryDelay = delay
		}
		return nil
	}
}

func withHTTPListenSleep(sleep func(context.Context, time.Duration) error) ListenOption {
	return func(o *listenOptions) error {
		if sleep != nil {
			o.sleep = sleep
		}
		return nil
	}
}

// Listen opens a Streamable HTTP GET SSE stream for server-to-client messages.
func (t *HTTPTransport) Listen(ctx context.Context, opts ...ListenOption) iter.Seq2[StreamEvent, error] {
	return func(yield func(StreamEvent, error) bool) {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			yield(StreamEvent{}, err)
			return
		}
		options, err := newListenOptions(opts)
		if err != nil {
			yield(StreamEvent{}, err)
			return
		}
		_, err = t.listenOnce(ctx, options, yield)
		if err != nil {
			yield(StreamEvent{}, err)
		}
	}
}

// ListenContinuously reconnects a Streamable HTTP GET SSE stream until ctx is canceled.
func (t *HTTPTransport) ListenContinuously(ctx context.Context, opts ...ListenOption) iter.Seq2[StreamEvent, error] {
	return func(yield func(StreamEvent, error) bool) {
		if ctx == nil {
			ctx = context.Background()
		}
		options, err := newListenOptions(opts)
		if err != nil {
			yield(StreamEvent{}, err)
			return
		}

		for {
			if err := ctx.Err(); err != nil {
				return
			}
			state, err := t.listenOnce(ctx, options, yield)
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					yield(StreamEvent{}, err)
				}
				return
			}
			if state.lastEventID != "" {
				options.lastEventID = state.lastEventID
			}
			delay := options.retryDelay
			if state.hasRetryAfter {
				delay = state.retryAfter
			}
			if state.stopped {
				return
			}
			if err := options.sleep(ctx, delay); err != nil {
				return
			}
		}
	}
}

func newListenOptions(opts []ListenOption) (listenOptions, error) {
	options := listenOptions{
		retryDelay: defaultHTTPListenRetryDelay,
		sleep:      sleepContext,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&options); err != nil {
			return listenOptions{}, err
		}
	}
	return options, nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (t *HTTPTransport) listenOnce(ctx context.Context, options listenOptions, yield func(StreamEvent, error) bool) (streamReadState, error) {
	state := streamReadState{}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return state, err
	}
	if err := t.ensureOpen(); err != nil {
		return state, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, t.endpoint, nil)
	if err != nil {
		return state, fmt.Errorf("mcp: create http listen request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	if options.lastEventID != "" {
		request.Header.Set(httpLastEventIDHeader, options.lastEventID)
	}
	hadSession := t.applyHTTPHeaders(request)

	response, err := t.client.Do(request)
	if err != nil {
		return state, fmt.Errorf("mcp: send http listen request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusMethodNotAllowed {
		return state, ErrHTTPListenUnsupported
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return state, t.statusError(response, hadSession)
	}
	mediaType, err := responseMediaType(response)
	if err != nil {
		return state, err
	}
	if mediaType != "text/event-stream" {
		return state, fmt.Errorf("%w: %s", ErrUnsupportedContentType, mediaType)
	}
	return t.readEventStream(ctx, response.Body, func(event StreamEvent) (bool, error) {
		handled, err := t.handleIncomingRequest(ctx, event.Data)
		if err != nil {
			return false, err
		}
		if handled {
			return true, nil
		}
		handled, err = t.handleIncomingNotification(ctx, event.Data)
		if err != nil {
			return false, err
		}
		if handled {
			return true, nil
		}
		return yield(event, nil), nil
	})
}

// TerminateSession sends an MCP Streamable HTTP DELETE request for the active session.
func (t *HTTPTransport) TerminateSession(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := t.ensureOpen(); err != nil {
		return err
	}
	if !t.hasSessionID() {
		return nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.endpoint, nil)
	if err != nil {
		return fmt.Errorf("mcp: create http session termination request: %w", err)
	}
	hadSession := t.applyHTTPHeaders(request)
	response, err := t.client.Do(request)
	if err != nil {
		return fmt.Errorf("mcp: send http session termination request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusMethodNotAllowed {
		return ErrHTTPSessionTerminationUnsupported
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return t.statusError(response, hadSession)
	}
	t.clearSessionID()
	return nil
}

// Close marks the transport closed. It does not close the injected HTTP client.
func (t *HTTPTransport) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

func (t *HTTPTransport) nextRequestID() (int64, error) {
	if t == nil {
		return 0, ErrTransportClosed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, ErrTransportClosed
	}
	t.nextID++
	return t.nextID, nil
}

func (t *HTTPTransport) ensureOpen() error {
	if t == nil {
		return ErrTransportClosed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	return nil
}

func (t *HTTPTransport) post(ctx context.Context, payload []byte) (*http.Response, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, false, fmt.Errorf("mcp: create http request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", streamableHTTPAcceptHeader)
	hadSession := t.applyHTTPHeaders(request)

	response, err := t.client.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("mcp: send http request: %w", err)
	}
	return response, hadSession, nil
}

func (t *HTTPTransport) handleIncomingRequest(ctx context.Context, raw json.RawMessage) (bool, error) {
	var request serverRequest
	if err := json.Unmarshal(raw, &request); err != nil || request.Method == "" || len(request.ID) == 0 {
		return false, nil
	}
	if t.requestHandler == nil {
		return false, nil
	}
	response, ok, err := t.requestHandler.Handle(ctx, append(json.RawMessage(nil), raw...))
	if err != nil {
		return true, fmt.Errorf("mcp: handle inbound json-rpc request: %w", err)
	}
	if !ok {
		return true, nil
	}
	return true, t.postJSONRPCMessage(ctx, response)
}

func (t *HTTPTransport) handleIncomingNotification(ctx context.Context, raw json.RawMessage) (bool, error) {
	var notification serverRequest
	if err := json.Unmarshal(raw, &notification); err != nil || notification.Method == "" || len(notification.ID) != 0 {
		return false, nil
	}
	if t.notifyHandler == nil {
		return false, nil
	}
	ok, err := t.notifyHandler.HandleNotification(ctx, append(json.RawMessage(nil), raw...))
	if err != nil {
		return true, fmt.Errorf("mcp: handle inbound json-rpc notification: %w", err)
	}
	return ok, nil
}

func (t *HTTPTransport) postJSONRPCMessage(ctx context.Context, payload json.RawMessage) error {
	response, hadSession, err := t.post(ctx, payload)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	t.captureSessionID(response)

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return t.statusError(response, hadSession)
	}
	return nil
}

func (t *HTTPTransport) applyHTTPHeaders(request *http.Request) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.protocolVersion != "" {
		request.Header.Set(httpProtocolVersionHeader, t.protocolVersion)
	}
	if t.sessionID != "" {
		request.Header.Set(httpSessionIDHeader, t.sessionID)
		return true
	}
	return false
}

func (t *HTTPTransport) captureSessionID(response *http.Response) {
	sessionID := response.Header.Get(httpSessionIDHeader)
	if sessionID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessionID = sessionID
}

func (t *HTTPTransport) clearSessionID() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessionID = ""
}

func (t *HTTPTransport) hasSessionID() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID != ""
}

func (t *HTTPTransport) statusError(response *http.Response, hadSession bool) error {
	body, err := readLimited(response.Body, t.maxResponseBytes)
	if err != nil {
		return err
	}
	statusErr := &HTTPStatusError{
		StatusCode: response.StatusCode,
		Status:     response.Status,
		Body:       string(body),
	}
	if hadSession && response.StatusCode == http.StatusNotFound {
		t.clearSessionID()
		return fmt.Errorf("%w: %w", ErrHTTPSessionExpired, statusErr)
	}
	return statusErr
}

func responseMediaType(response *http.Response) (string, error) {
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		return "", nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedContentType, contentType)
	}
	return mediaType, nil
}

func decodeExpectedRPCResponse(raw []byte, id int64, result any) error {
	matched, err := decodeRPCStreamMessage(raw, id, result)
	if err != nil {
		return err
	}
	if !matched {
		return ErrUnexpectedJSONRPCResponse
	}
	return nil
}

func decodeRPCStreamMessage(raw []byte, id int64, result any) (bool, error) {
	var response rpcResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, fmt.Errorf("mcp: decode json-rpc response: %w", err)
	}
	if response.Error == nil && len(response.Result) == 0 {
		return false, nil
	}
	if len(response.ID) == 0 || !jsonRPCIDMatches(response.ID, id) {
		return false, fmt.Errorf("%w: id %s", ErrUnexpectedJSONRPCResponse, response.ID)
	}
	if response.Error != nil {
		return true, response.Error
	}
	if result == nil || len(response.Result) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return false, fmt.Errorf("mcp: decode json-rpc result: %w", err)
	}
	return true, nil
}

func (t *HTTPTransport) decodeEventStreamResponse(ctx context.Context, reader io.Reader, id int64, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	options, err := newListenOptions(nil)
	if err != nil {
		return err
	}

	state, matched, err := t.readEventStreamResponse(ctx, reader, id, result)
	if err != nil {
		return err
	}
	for !matched {
		if state.lastEventID == "" {
			return ErrUnexpectedJSONRPCResponse
		}
		options.lastEventID = state.lastEventID
		delay := options.retryDelay
		if state.hasRetryAfter {
			delay = state.retryAfter
		}
		if err := options.sleep(ctx, delay); err != nil {
			return err
		}
		state, matched, err = t.resumeEventStreamResponse(ctx, options, id, result)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *HTTPTransport) readEventStreamResponse(ctx context.Context, reader io.Reader, id int64, result any) (streamReadState, bool, error) {
	matched := false
	state, err := t.readEventStream(ctx, reader, func(event StreamEvent) (bool, error) {
		handled, err := t.handleIncomingRequest(ctx, event.Data)
		if err != nil {
			return false, err
		}
		if handled {
			return true, nil
		}
		handled, err = t.handleIncomingNotification(ctx, event.Data)
		if err != nil {
			return false, err
		}
		if handled {
			return true, nil
		}
		ok, err := decodeRPCStreamMessage(event.Data, id, result)
		if err != nil {
			return false, err
		}
		if !ok {
			return true, nil
		}
		matched = true
		return false, nil
	})
	return state, matched, err
}

func (t *HTTPTransport) resumeEventStreamResponse(ctx context.Context, options listenOptions, id int64, result any) (streamReadState, bool, error) {
	matched := false
	var decodeErr error

	state, err := t.listenOnce(ctx, options, func(event StreamEvent, err error) bool {
		if err != nil {
			decodeErr = err
			return false
		}
		ok, err := decodeRPCStreamMessage(event.Data, id, result)
		if err != nil {
			decodeErr = err
			return false
		}
		if !ok {
			return true
		}
		matched = true
		return false
	})
	if decodeErr != nil {
		return state, matched, decodeErr
	}
	if err != nil {
		return state, matched, err
	}
	return state, matched, nil
}

type streamReadState struct {
	lastEventID   string
	retryAfter    time.Duration
	hasRetryAfter bool
	stopped       bool
}

func (t *HTTPTransport) readEventStream(ctx context.Context, reader io.Reader, handle func(StreamEvent) (bool, error)) (streamReadState, error) {
	limit := t.maxResponseBytes
	if limit <= 0 {
		limit = defaultHTTPTransportMaxResponseBytes
	}
	buffered := bufio.NewReader(io.LimitReader(reader, int64(limit)+1))
	event := StreamEvent{}
	state := streamReadState{}
	totalBytes := 0

	dispatch := func() (bool, error) {
		if event.ID != "" {
			state.lastEventID = event.ID
		}
		if event.hasRetryAfter {
			state.retryAfter = event.retryAfter
			state.hasRetryAfter = true
		}
		if len(event.Data) == 0 {
			event = StreamEvent{}
			return true, nil
		}
		raw := event.Data
		if raw[len(raw)-1] == '\n' {
			raw = raw[:len(raw)-1]
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			event = StreamEvent{}
			return true, nil
		}
		event.Data = append(json.RawMessage(nil), raw...)
		keepGoing, err := handle(event)
		event = StreamEvent{}
		return keepGoing, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		line, err := buffered.ReadString('\n')
		if len(line) > 0 {
			totalBytes += len(line)
			if totalBytes > limit {
				return state, ErrHTTPResponseBodyTooLarge
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				keepGoing, err := dispatch()
				if err != nil {
					return state, err
				}
				if !keepGoing {
					state.stopped = true
					return state, nil
				}
			} else if !strings.HasPrefix(trimmed, ":") {
				field, value, ok := strings.Cut(trimmed, ":")
				if ok && strings.HasPrefix(value, " ") {
					value = value[1:]
				}
				switch field {
				case "data":
					event.Data = append(event.Data, value...)
					event.Data = append(event.Data, '\n')
				case "id":
					event.ID = value
				case "event":
					event.Event = value
				case "retry":
					millis, err := strconv.ParseInt(value, 10, 64)
					if err == nil && millis >= 0 {
						event.retryAfter = time.Duration(millis) * time.Millisecond
						event.hasRetryAfter = true
					}
				}
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			_, dispatchErr := dispatch()
			if dispatchErr != nil {
				return state, dispatchErr
			}
			return state, nil
		}
		return state, fmt.Errorf("mcp: read http event stream: %w", err)
	}
}

func readLimited(reader io.Reader, limit int) ([]byte, error) {
	if limit <= 0 {
		limit = defaultHTTPTransportMaxResponseBytes
	}
	limited := io.LimitReader(reader, int64(limit)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("mcp: read http response body: %w", err)
	}
	if len(body) > limit {
		return nil, ErrHTTPResponseBodyTooLarge
	}
	return body, nil
}
