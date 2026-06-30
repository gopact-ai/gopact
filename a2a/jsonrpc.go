package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/gopact-ai/gopact"
)

const (
	defaultJSONRPCMaxResponseBytes int64 = 4 << 20

	jsonRPCVersion = "2.0"

	jsonRPCMethodSendMessage          = "SendMessage"
	jsonRPCMethodSendStreamingMessage = "SendStreamingMessage"
	jsonRPCMethodCancelTask           = "CancelTask"

	jsonRPCAgentCardPath   = "/.well-known/agent-card.json"
	jsonRPCGopactExtension = "x-gopact"
)

var (
	// ErrJSONRPCEndpointRequired is returned when a JSON-RPC A2A agent has no endpoint.
	ErrJSONRPCEndpointRequired = errors.New("a2a: json-rpc endpoint is required")
	// ErrJSONRPCError wraps a JSON-RPC error response.
	ErrJSONRPCError = errors.New("a2a: json-rpc error")
)

// JSONRPCAgentOption configures a JSON-RPC A2A agent client.
type JSONRPCAgentOption func(*JSONRPCAgent) error

// JSONRPCAgent is a JSON-RPC 2.0 + SSE adapter for the A2A Agent contract.
type JSONRPCAgent struct {
	endpoint         string
	client           *http.Client
	card             AgentCard
	headers          http.Header
	maxResponseBytes int64
}

var (
	_ Agent          = (*JSONRPCAgent)(nil)
	_ StreamingAgent = (*JSONRPCAgent)(nil)
	_ Discoverer     = (*JSONRPCAgent)(nil)
	_ CardLister     = (*JSONRPCAgent)(nil)
)

// NewJSONRPCAgent creates a JSON-RPC A2A agent client.
func NewJSONRPCAgent(endpoint string, opts ...JSONRPCAgentOption) (*JSONRPCAgent, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, ErrJSONRPCEndpointRequired
	}
	agent := &JSONRPCAgent{
		endpoint:         endpoint,
		client:           http.DefaultClient,
		card:             AgentCard{URL: endpoint},
		headers:          make(http.Header),
		maxResponseBytes: defaultJSONRPCMaxResponseBytes,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(agent); err != nil {
			return nil, err
		}
	}
	if agent.client == nil {
		return nil, errors.New("a2a: json-rpc http client is required")
	}
	if agent.maxResponseBytes <= 0 {
		return nil, errors.New("a2a: json-rpc max response bytes must be positive")
	}
	return agent, nil
}

// WithJSONRPCClient sets the HTTP client used by a JSON-RPC A2A agent.
func WithJSONRPCClient(client *http.Client) JSONRPCAgentOption {
	return func(agent *JSONRPCAgent) error {
		if client == nil {
			return errors.New("a2a: json-rpc http client is required")
		}
		agent.client = client
		return nil
	}
}

// WithJSONRPCAgentCard sets the local card metadata exposed by Card.
func WithJSONRPCAgentCard(card AgentCard) JSONRPCAgentOption {
	return func(agent *JSONRPCAgent) error {
		card = copyAgentCard(card)
		if card.URL == "" {
			card.URL = agent.endpoint
		}
		agent.card = card
		return nil
	}
}

// WithJSONRPCHeader adds a static header to every JSON-RPC HTTP request.
func WithJSONRPCHeader(key string, value string) JSONRPCAgentOption {
	return func(agent *JSONRPCAgent) error {
		if key == "" {
			return errors.New("a2a: json-rpc http header key is required")
		}
		agent.headers.Set(key, value)
		return nil
	}
}

// WithJSONRPCMaxResponseBytes bounds non-stream response bodies and SSE data size.
func WithJSONRPCMaxResponseBytes(n int64) JSONRPCAgentOption {
	return func(agent *JSONRPCAgent) error {
		if n <= 0 {
			return errors.New("a2a: json-rpc max response bytes must be positive")
		}
		agent.maxResponseBytes = n
		return nil
	}
}

// Card returns configured card metadata for model-visible tool specs.
func (a *JSONRPCAgent) Card() AgentCard {
	if a == nil {
		return AgentCard{}
	}
	card := copyAgentCard(a.card)
	if card.URL == "" {
		card.URL = a.endpoint
	}
	return card
}

// Discover fetches an agent card from the well-known JSON-RPC A2A endpoint.
func (a *JSONRPCAgent) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if a == nil {
		return DiscoveryResult{}, ErrJSONRPCEndpointRequired
	}
	endpoint := a.endpoint
	if query.URL != "" {
		endpoint = strings.TrimRight(strings.TrimSpace(query.URL), "/")
	}
	if endpoint == "" {
		return DiscoveryResult{}, ErrJSONRPCEndpointRequired
	}
	var card AgentCard
	if err := a.doJSON(ctx, http.MethodGet, endpoint+jsonRPCAgentCardPath, nil, "application/json", &card); err != nil {
		return DiscoveryResult{}, err
	}
	if card.URL == "" {
		card.URL = endpoint
	}
	if !matchesRemoteDiscoveryQuery(card, query) {
		return DiscoveryResult{}, ErrAgentNotFound
	}
	return DiscoveryResult{Card: copyAgentCard(card)}, nil
}

// ListCards returns the JSON-RPC endpoint's well-known agent card.
func (a *JSONRPCAgent) ListCards(ctx context.Context) ([]AgentCard, error) {
	result, err := a.Discover(ctx, DiscoveryQuery{})
	if err != nil {
		return nil, err
	}
	return []AgentCard{copyAgentCard(result.Card)}, nil
}

// Send invokes the A2A SendMessage operation over JSON-RPC.
func (a *JSONRPCAgent) Send(ctx context.Context, task Task) (Result, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if a == nil {
		return Result{}, ErrJSONRPCEndpointRequired
	}
	task = taskWithContextAuth(ctx, task)
	params := jsonRPCSendMessageRequestFromTask(task)
	var result jsonRPCSendMessageResponse
	if err := a.call(ctx, jsonRPCMethodSendMessage, jsonRPCRequestID(task.IDs, task.ID), params, &result); err != nil {
		return Result{}, err
	}
	return result.toResult(task), nil
}

// Stream invokes the A2A SendStreamingMessage operation over JSON-RPC and decodes SSE updates.
func (a *JSONRPCAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if a == nil {
			yield(failedTaskEvent(task, ErrJSONRPCEndpointRequired), ErrJSONRPCEndpointRequired)
			return
		}
		task = taskWithContextAuth(ctx, task)
		params := jsonRPCSendMessageRequestFromTask(task)
		req := jsonRPCRequest{
			JSONRPC: jsonRPCVersion,
			ID:      jsonRPCRequestID(task.IDs, task.ID),
			Method:  jsonRPCMethodSendStreamingMessage,
			Params:  mustRawJSON(params),
		}
		resp, err := a.do(ctx, http.MethodPost, a.endpoint, req, "text/event-stream")
		if err != nil {
			yield(failedTaskEvent(task, err), err)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if err := a.checkStatus(resp); err != nil {
			yield(failedTaskEvent(task, err), err)
			return
		}
		a.scanSSE(resp.Body, task, yield)
	}
}

// Cancel invokes the A2A CancelTask operation over JSON-RPC.
func (a *JSONRPCAgent) Cancel(ctx context.Context, taskID string) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if a == nil {
		return ErrJSONRPCEndpointRequired
	}
	if taskID == "" {
		return ErrTaskIDRequired
	}
	var result jsonRPCSendMessageResponse
	return a.call(ctx, jsonRPCMethodCancelTask, taskID, jsonRPCCancelTaskRequest{ID: taskID}, &result)
}

func (a *JSONRPCAgent) call(ctx context.Context, method string, id string, params any, output any) error {
	req := jsonRPCRequest{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Method:  method,
		Params:  mustRawJSON(params),
	}
	var resp jsonRPCResponse
	if err := a.doJSON(ctx, http.MethodPost, a.endpoint, req, "application/json", &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error.err()
	}
	if output == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Result, output); err != nil {
		return fmt.Errorf("a2a: decode json-rpc result: %w", err)
	}
	return nil
}

func (a *JSONRPCAgent) doJSON(ctx context.Context, method string, url string, input any, accept string, output any) error {
	resp, err := a.do(ctx, method, url, input, accept)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if err := a.checkStatus(resp); err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	return decodeLimitedJSON(resp.Body, a.maxResponseBytes, output)
}

func (a *JSONRPCAgent) do(ctx context.Context, method string, url string, input any, accept string) (*http.Response, error) {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("a2a: encode json-rpc request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("a2a: create json-rpc request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	for key, values := range a.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: json-rpc http request: %w", err)
	}
	return resp, nil
}

func (a *JSONRPCAgent) checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var remote httpErrorResponse
	_ = decodeLimitedJSON(resp.Body, a.maxResponseBytes, &remote)
	if remote.Error != "" {
		return fmt.Errorf("%w: %s: %s", ErrHTTPStatus, resp.Status, remote.Error)
	}
	return fmt.Errorf("%w: %s", ErrHTTPStatus, resp.Status)
}

func (a *JSONRPCAgent) scanSSE(body io.Reader, task Task, yield func(TaskEvent, error) bool) {
	scanner := bufio.NewScanner(body)
	maxLine := int(a.maxResponseBytes)
	if maxLine < 64*1024 {
		maxLine = 64 * 1024
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
	var data bytes.Buffer
	flush := func() bool {
		if data.Len() == 0 {
			return true
		}
		raw := bytes.TrimSpace(data.Bytes())
		data.Reset()
		if len(raw) == 0 {
			return true
		}
		event, err := jsonRPCStreamEvent(raw, task)
		if err != nil {
			yield(failedTaskEvent(task, err), err)
			return false
		}
		return yield(event, nil)
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if !flush() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if !flush() {
		return
	}
	if err := scanner.Err(); err != nil {
		wrapped := fmt.Errorf("a2a: read json-rpc sse event: %w", err)
		yield(failedTaskEvent(task, wrapped), wrapped)
	}
}

// NewJSONRPCHandler exposes an A2A agent through a JSON-RPC 2.0 + SSE API.
func NewJSONRPCHandler(agent Agent) http.Handler {
	h := &jsonRPCHandler{agent: agent}
	mux := http.NewServeMux()
	mux.HandleFunc(jsonRPCAgentCardPath, h.card)
	mux.HandleFunc("/", h.rpc)
	return mux
}

type jsonRPCHandler struct {
	agent Agent
}

func (h *jsonRPCHandler) card(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if h.agent == nil {
		writeHTTPError(w, http.StatusInternalServerError, ErrAgentNotFound)
		return
	}
	writeHTTPJSON(w, http.StatusOK, h.agent.Card())
}

func (h *jsonRPCHandler) rpc(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if h.agent == nil {
		writeHTTPError(w, http.StatusInternalServerError, ErrAgentNotFound)
		return
	}
	var req jsonRPCRequest
	if err := decodeHTTPJSON(r, &req); err != nil {
		writeHTTPJSON(w, http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: jsonRPCVersion,
			Error:   jsonRPCErrorInvalidRequest(err),
		})
		return
	}
	if req.JSONRPC != "" && req.JSONRPC != jsonRPCVersion {
		h.writeError(w, req.ID, jsonRPCErrorInvalidRequest(errors.New("a2a: unsupported json-rpc version")))
		return
	}
	switch req.Method {
	case jsonRPCMethodSendMessage:
		h.send(w, r, req)
	case jsonRPCMethodSendStreamingMessage:
		h.stream(w, r, req)
	case jsonRPCMethodCancelTask:
		h.cancel(w, r, req)
	default:
		h.writeError(w, req.ID, &jsonRPCError{Code: -32601, Message: "method not found"})
	}
}

func (h *jsonRPCHandler) send(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	_, task, err := jsonRPCDecodeSendMessageRequest(req.Params)
	if err != nil {
		h.writeError(w, req.ID, jsonRPCErrorInvalidRequest(err))
		return
	}
	ctx := requestContextWithTaskAuth(r.Context(), task)
	result, err := h.agent.Send(ctx, task)
	if err != nil {
		h.writeError(w, req.ID, jsonRPCErrorRemote(err))
		return
	}
	h.writeResult(w, req.ID, jsonRPCSendMessageResponseFromResult(task, result))
}

func (h *jsonRPCHandler) stream(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	_, task, err := jsonRPCDecodeSendMessageRequest(req.Params)
	if err != nil {
		h.writeError(w, req.ID, jsonRPCErrorInvalidRequest(err))
		return
	}
	streamer, ok := h.agent.(StreamingAgent)
	if !ok {
		h.writeError(w, req.ID, jsonRPCErrorRemote(ErrStreamNotSupported))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	ctx := requestContextWithTaskAuth(r.Context(), task)
	for event, err := range streamer.Stream(ctx, task) {
		event = event.WithDefaults(task)
		var frame jsonRPCResponse
		if err != nil {
			frame = jsonRPCResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Error: jsonRPCErrorRemote(err)}
		} else {
			frame = jsonRPCResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Result: mustRawJSON(jsonRPCStreamResponseFromEvent(event))}
		}
		if encodeErr := writeJSONRPCSSE(w, frame); encodeErr != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if err != nil {
			return
		}
	}
}

func (h *jsonRPCHandler) cancel(w http.ResponseWriter, r *http.Request, req jsonRPCRequest) {
	var params jsonRPCCancelTaskRequest
	if len(req.Params) != 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			h.writeError(w, req.ID, jsonRPCErrorInvalidRequest(err))
			return
		}
	}
	taskID := firstNonEmpty(params.ID, params.TaskID)
	if taskID == "" {
		h.writeError(w, req.ID, jsonRPCErrorInvalidRequest(ErrTaskIDRequired))
		return
	}
	if err := h.agent.Cancel(r.Context(), taskID); err != nil {
		h.writeError(w, req.ID, jsonRPCErrorRemote(err))
		return
	}
	h.writeResult(w, req.ID, jsonRPCSendMessageResponse{
		Task: &jsonRPCTask{
			ID: taskID,
			Status: jsonRPCTaskStatus{
				State: "TASK_STATE_CANCELED",
			},
		},
	})
}

func (h *jsonRPCHandler) writeResult(w http.ResponseWriter, id string, result any) {
	writeHTTPJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  mustRawJSON(result),
	})
}

func (h *jsonRPCHandler) writeError(w http.ResponseWriter, id string, rpcErr *jsonRPCError) {
	writeHTTPJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   rpcErr,
	})
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (e *jsonRPCError) err() error {
	if e == nil {
		return nil
	}
	if e.Message == "" {
		return fmt.Errorf("%w: code %d", ErrJSONRPCError, e.Code)
	}
	return fmt.Errorf("%w: code %d: %s", ErrJSONRPCError, e.Code, e.Message)
}

func jsonRPCErrorInvalidRequest(err error) *jsonRPCError {
	return &jsonRPCError{Code: -32600, Message: err.Error()}
}

func jsonRPCErrorRemote(err error) *jsonRPCError {
	return &jsonRPCError{Code: -32000, Message: err.Error()}
}

type jsonRPCSendMessageRequest struct {
	Message  jsonRPCMessage `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type jsonRPCMessage struct {
	MessageID string         `json:"messageId,omitempty"`
	TaskID    string         `json:"taskId,omitempty"`
	ContextID string         `json:"contextId,omitempty"`
	Role      string         `json:"role,omitempty"`
	Parts     []jsonRPCPart  `json:"parts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type jsonRPCPart struct {
	Text      string         `json:"text,omitempty"`
	URL       string         `json:"url,omitempty"`
	FileName  string         `json:"filename,omitempty"`
	MediaType string         `json:"mediaType,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type jsonRPCGopactMetadata struct {
	IDs  gopact.RuntimeIDs `json:"ids,omitempty"`
	Auth *Auth             `json:"auth,omitempty"`
}

type jsonRPCSendMessageResponse struct {
	Task    *jsonRPCTask    `json:"task,omitempty"`
	Message *jsonRPCMessage `json:"message,omitempty"`
	Result  *Result         `json:"result,omitempty"`
}

type jsonRPCTask struct {
	ID        string            `json:"id,omitempty"`
	ContextID string            `json:"contextId,omitempty"`
	Status    jsonRPCTaskStatus `json:"status,omitempty"`
	Artifacts []jsonRPCArtifact `json:"artifacts,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

type jsonRPCTaskStatus struct {
	State   string          `json:"state,omitempty"`
	Message *jsonRPCMessage `json:"message,omitempty"`
}

type jsonRPCArtifact struct {
	ArtifactID string         `json:"artifactId,omitempty"`
	Name       string         `json:"name,omitempty"`
	URI        string         `json:"uri,omitempty"`
	Parts      []jsonRPCPart  `json:"parts,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type jsonRPCStreamResponse struct {
	Event          *TaskEvent             `json:"event,omitempty"`
	Task           *jsonRPCTask           `json:"task,omitempty"`
	Message        *jsonRPCMessage        `json:"message,omitempty"`
	StatusUpdate   *jsonRPCStatusUpdate   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *jsonRPCArtifactUpdate `json:"artifactUpdate,omitempty"`
}

type jsonRPCStatusUpdate struct {
	TaskID string            `json:"taskId,omitempty"`
	Status jsonRPCTaskStatus `json:"status,omitempty"`
}

type jsonRPCArtifactUpdate struct {
	TaskID   string          `json:"taskId,omitempty"`
	Artifact jsonRPCArtifact `json:"artifact,omitempty"`
}

type jsonRPCCancelTaskRequest struct {
	ID     string `json:"id,omitempty"`
	TaskID string `json:"taskId,omitempty"`
}

func jsonRPCSendMessageRequestFromTask(task Task) jsonRPCSendMessageRequest {
	metadata := copyAnyMap(task.Metadata)
	gopactMeta := jsonRPCGopactMetadata{IDs: task.IDs}
	if task.Auth != nil {
		auth := copyAuth(*task.Auth)
		gopactMeta.Auth = &auth
	}
	if !gopactMeta.IDs.IsZero() || gopactMeta.Auth != nil {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata[jsonRPCGopactExtension] = gopactMeta
	}
	return jsonRPCSendMessageRequest{
		Message: jsonRPCMessage{
			MessageID: taskID(task),
			TaskID:    task.ID,
			Role:      "ROLE_USER",
			Parts:     []jsonRPCPart{{Text: task.Input}},
		},
		Metadata: metadata,
	}
}

func jsonRPCDecodeSendMessageRequest(raw json.RawMessage) (jsonRPCSendMessageRequest, Task, error) {
	var params jsonRPCSendMessageRequest
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return jsonRPCSendMessageRequest{}, Task{}, fmt.Errorf("a2a: decode SendMessage params: %w", err)
		}
	}
	task := Task{
		ID:       firstNonEmpty(params.Message.TaskID, params.Message.MessageID),
		Input:    jsonRPCMessageText(params.Message),
		Metadata: copyAnyMap(params.Metadata),
	}
	var gopactMeta jsonRPCGopactMetadata
	if readJSONRPCMetadata(params.Metadata, jsonRPCGopactExtension, &gopactMeta) {
		task.IDs = gopactMeta.IDs
		if gopactMeta.Auth != nil {
			auth := copyAuth(*gopactMeta.Auth)
			task.Auth = &auth
		}
	}
	if task.Metadata != nil {
		delete(task.Metadata, jsonRPCGopactExtension)
		if len(task.Metadata) == 0 {
			task.Metadata = nil
		}
	}
	return params, task, nil
}

func jsonRPCSendMessageResponseFromResult(task Task, result Result) jsonRPCSendMessageResponse {
	if result.TaskID == "" {
		result.TaskID = taskID(task)
	}
	response := jsonRPCSendMessageResponse{
		Task: &jsonRPCTask{
			ID:        result.TaskID,
			ContextID: task.IDs.ThreadID,
			Status: jsonRPCTaskStatus{
				State: "TASK_STATE_COMPLETED",
			},
			Artifacts: jsonRPCArtifactsFromResult(result),
			Metadata:  copyAnyMap(result.Metadata),
		},
	}
	if result.Output != "" {
		response.Task.Status.Message = &jsonRPCMessage{
			Role:  "ROLE_AGENT",
			Parts: []jsonRPCPart{{Text: result.Output}},
		}
	}
	return response
}

func (r jsonRPCSendMessageResponse) toResult(defaultTask Task) Result {
	if r.Result != nil {
		return copyResult(*r.Result)
	}
	if r.Task != nil {
		result := Result{
			TaskID:    firstNonEmpty(r.Task.ID, taskID(defaultTask)),
			Output:    jsonRPCOutputFromTask(*r.Task),
			Artifacts: artifactRefsFromJSONRPCTask(*r.Task),
			Metadata:  copyAnyMap(r.Task.Metadata),
		}
		return copyResult(result)
	}
	if r.Message != nil {
		return Result{TaskID: taskID(defaultTask), Output: jsonRPCMessageText(*r.Message)}
	}
	return Result{TaskID: taskID(defaultTask)}
}

func jsonRPCStreamResponseFromEvent(event TaskEvent) jsonRPCStreamResponse {
	response := jsonRPCStreamResponse{Event: &event}
	if len(event.Artifacts) > 0 {
		response.ArtifactUpdate = &jsonRPCArtifactUpdate{
			TaskID:   event.TaskID,
			Artifact: jsonRPCArtifactFromRef(event.Artifacts[0], event.Message),
		}
	}
	if event.Status != "" || event.Message != "" {
		response.StatusUpdate = &jsonRPCStatusUpdate{
			TaskID: event.TaskID,
			Status: jsonRPCTaskStatus{
				State: jsonRPCTaskState(event.Status),
			},
		}
		if event.Message != "" {
			response.StatusUpdate.Status.Message = &jsonRPCMessage{
				Role:  "ROLE_AGENT",
				Parts: []jsonRPCPart{{Text: event.Message}},
			}
		}
	}
	if event.Result != nil {
		response.Task = jsonRPCSendMessageResponseFromResult(Task{ID: event.TaskID, IDs: event.IDs}, *event.Result).Task
	}
	return response
}

func jsonRPCStreamEvent(raw []byte, defaultTask Task) (TaskEvent, error) {
	payload := raw
	var envelope jsonRPCResponse
	if err := json.Unmarshal(raw, &envelope); err == nil && (envelope.JSONRPC == jsonRPCVersion || envelope.Result != nil || envelope.Error != nil) {
		if envelope.Error != nil {
			return failedTaskEvent(defaultTask, envelope.Error.err()), envelope.Error.err()
		}
		payload = envelope.Result
	}
	var response jsonRPCStreamResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return TaskEvent{}, fmt.Errorf("a2a: decode json-rpc stream response: %w", err)
	}
	if response.Event != nil {
		return response.Event.WithDefaults(defaultTask), nil
	}
	if response.StatusUpdate != nil {
		event := TaskEvent{
			TaskID:  firstNonEmpty(response.StatusUpdate.TaskID, taskID(defaultTask)),
			IDs:     defaultTask.IDs,
			Status:  taskStatusFromJSONRPCState(response.StatusUpdate.Status.State),
			Message: jsonRPCStatusMessage(response.StatusUpdate.Status),
		}
		return event.WithDefaults(defaultTask), nil
	}
	if response.ArtifactUpdate != nil {
		event := TaskEvent{
			TaskID:    firstNonEmpty(response.ArtifactUpdate.TaskID, taskID(defaultTask)),
			IDs:       defaultTask.IDs,
			Artifacts: []gopact.ArtifactRef{artifactRefFromJSONRPCArtifact(response.ArtifactUpdate.Artifact)},
		}
		return event.WithDefaults(defaultTask), nil
	}
	if response.Task != nil {
		result := jsonRPCSendMessageResponse{Task: response.Task}.toResult(defaultTask)
		event := TaskEvent{
			TaskID:    result.TaskID,
			IDs:       defaultTask.IDs,
			Status:    taskStatusFromJSONRPCState(response.Task.Status.State),
			Message:   jsonRPCStatusMessage(response.Task.Status),
			Result:    &result,
			Artifacts: result.Artifacts,
		}
		return event.WithDefaults(defaultTask), nil
	}
	if response.Message != nil {
		event := TaskEvent{
			TaskID:  taskID(defaultTask),
			IDs:     defaultTask.IDs,
			Message: jsonRPCMessageText(*response.Message),
		}
		return event.WithDefaults(defaultTask), nil
	}
	return failedTaskEvent(defaultTask, errors.New("a2a: empty json-rpc stream response")), errors.New("a2a: empty json-rpc stream response")
}

func jsonRPCArtifactsFromResult(result Result) []jsonRPCArtifact {
	refs := copyArtifactRefs(result.Artifacts)
	if len(refs) == 0 && result.Output == "" {
		return nil
	}
	if len(refs) == 0 {
		return []jsonRPCArtifact{{
			ArtifactID: result.TaskID,
			Parts:      []jsonRPCPart{{Text: result.Output}},
		}}
	}
	out := make([]jsonRPCArtifact, 0, len(refs))
	for i, ref := range refs {
		text := ""
		if i == 0 {
			text = result.Output
		}
		out = append(out, jsonRPCArtifactFromRef(ref, text))
	}
	return out
}

func jsonRPCArtifactFromRef(ref gopact.ArtifactRef, text string) jsonRPCArtifact {
	artifact := jsonRPCArtifact{
		ArtifactID: ref.ID,
		Name:       ref.Name,
		URI:        ref.URI,
		Metadata:   copyAnyMap(ref.Metadata),
	}
	if text != "" {
		artifact.Parts = append(artifact.Parts, jsonRPCPart{Text: text})
	}
	if ref.URI != "" {
		artifact.Parts = append(artifact.Parts, jsonRPCPart{
			URL:       ref.URI,
			FileName:  ref.Name,
			MediaType: ref.MIMEType,
		})
	}
	return artifact
}

func artifactRefsFromJSONRPCTask(task jsonRPCTask) []gopact.ArtifactRef {
	if len(task.Artifacts) == 0 {
		return nil
	}
	refs := make([]gopact.ArtifactRef, 0, len(task.Artifacts))
	for _, artifact := range task.Artifacts {
		ref := artifactRefFromJSONRPCArtifact(artifact)
		if ref.ID != "" || ref.Name != "" || ref.URI != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func artifactRefFromJSONRPCArtifact(artifact jsonRPCArtifact) gopact.ArtifactRef {
	ref := gopact.ArtifactRef{
		ID:       artifact.ArtifactID,
		Name:     artifact.Name,
		URI:      artifact.URI,
		Metadata: copyAnyMap(artifact.Metadata),
	}
	for _, part := range artifact.Parts {
		if ref.URI == "" && part.URL != "" {
			ref.URI = part.URL
		}
		if ref.Name == "" && part.FileName != "" {
			ref.Name = part.FileName
		}
		if ref.MIMEType == "" && part.MediaType != "" {
			ref.MIMEType = part.MediaType
		}
	}
	return ref
}

func jsonRPCOutputFromTask(task jsonRPCTask) string {
	if msg := jsonRPCStatusMessage(task.Status); msg != "" {
		return msg
	}
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Text != "" {
				return part.Text
			}
		}
	}
	return ""
}

func jsonRPCMessageText(message jsonRPCMessage) string {
	var parts []string
	for _, part := range message.Parts {
		if part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func jsonRPCStatusMessage(status jsonRPCTaskStatus) string {
	if status.Message == nil {
		return ""
	}
	return jsonRPCMessageText(*status.Message)
}

func jsonRPCTaskState(status TaskStatus) string {
	switch status {
	case TaskStatusSubmitted:
		return "TASK_STATE_SUBMITTED"
	case TaskStatusRunning:
		return "TASK_STATE_WORKING"
	case TaskStatusCompleted:
		return "TASK_STATE_COMPLETED"
	case TaskStatusFailed:
		return "TASK_STATE_FAILED"
	case TaskStatusCanceled:
		return "TASK_STATE_CANCELED"
	default:
		return ""
	}
}

func taskStatusFromJSONRPCState(state string) TaskStatus {
	switch state {
	case "TASK_STATE_SUBMITTED":
		return TaskStatusSubmitted
	case "TASK_STATE_WORKING", "TASK_STATE_RUNNING":
		return TaskStatusRunning
	case "TASK_STATE_COMPLETED":
		return TaskStatusCompleted
	case "TASK_STATE_FAILED", "TASK_STATE_REJECTED":
		return TaskStatusFailed
	case "TASK_STATE_CANCELED":
		return TaskStatusCanceled
	default:
		return ""
	}
}

func readJSONRPCMetadata(metadata map[string]any, key string, out any) bool {
	value, ok := metadata[key]
	if !ok {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

func jsonRPCRequestID(ids gopact.RuntimeIDs, taskID string) string {
	if taskID != "" {
		return taskID
	}
	if ids.CallID != "" {
		return ids.CallID
	}
	if ids.RunID != "" {
		return ids.RunID
	}
	return "request"
}

func mustRawJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func writeJSONRPCSSE(w io.Writer, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("a2a: encode json-rpc sse event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return fmt.Errorf("a2a: write json-rpc sse event: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
