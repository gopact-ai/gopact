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
)

const (
	defaultHTTPMaxResponseBytes int64 = 4 << 20

	httpPathCard       = "/.well-known/agent-card.json"
	httpPathTaskSend   = "/a2a/task/send"
	httpPathTaskStream = "/a2a/task/stream"
	httpPathTaskCancel = "/a2a/task/cancel"
	httpPathHealth     = "/healthz"
	httpPathReady      = "/readyz"
)

var (
	// ErrHTTPEndpointRequired is returned when an HTTP A2A agent has no endpoint.
	ErrHTTPEndpointRequired = errors.New("a2a: http endpoint is required")
	// ErrHTTPRegistryURLRequired is returned when an HTTP A2A registry has no URL.
	ErrHTTPRegistryURLRequired = errors.New("a2a: http registry url is required")
	// ErrHTTPStatus wraps non-success HTTP responses from an A2A endpoint.
	ErrHTTPStatus = errors.New("a2a: http status error")
)

// HTTPAgentOption configures an HTTP A2A agent client.
type HTTPAgentOption func(*HTTPAgent) error

// HTTPAgent is a small HTTP/JSON adapter for the A2A Agent contract.
type HTTPAgent struct {
	endpoint         string
	client           *http.Client
	card             AgentCard
	headers          http.Header
	maxResponseBytes int64
}

// HTTPRegistry fetches agent cards from one HTTP JSON registry document.
type HTTPRegistry struct {
	url    string
	client *HTTPAgent
}

var (
	_ Agent          = (*HTTPAgent)(nil)
	_ StreamingAgent = (*HTTPAgent)(nil)
	_ Discoverer     = (*HTTPAgent)(nil)
	_ CardLister     = (*HTTPAgent)(nil)
	_ Discoverer     = (*HTTPRegistry)(nil)
	_ CardLister     = (*HTTPRegistry)(nil)
)

// NewHTTPAgent creates an HTTP A2A agent client.
func NewHTTPAgent(endpoint string, opts ...HTTPAgentOption) (*HTTPAgent, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, ErrHTTPEndpointRequired
	}
	agent := &HTTPAgent{
		endpoint:         endpoint,
		client:           http.DefaultClient,
		card:             AgentCard{URL: endpoint},
		headers:          make(http.Header),
		maxResponseBytes: defaultHTTPMaxResponseBytes,
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
		return nil, errors.New("a2a: http client is required")
	}
	if agent.maxResponseBytes <= 0 {
		return nil, errors.New("a2a: max response bytes must be positive")
	}
	return agent, nil
}

// NewHTTPCardListers creates HTTP card listers for mesh bootstrap.
func NewHTTPCardListers(endpoints []string, opts ...HTTPAgentOption) ([]CardLister, error) {
	listers := make([]CardLister, 0, len(endpoints))
	for _, endpoint := range endpoints {
		agent, err := NewHTTPAgent(endpoint, opts...)
		if err != nil {
			return nil, err
		}
		listers = append(listers, agent)
	}
	return listers, nil
}

// NewHTTPRegistry creates a registry backed by one HTTP JSON agent-card document.
func NewHTTPRegistry(url string, opts ...HTTPAgentOption) (*HTTPRegistry, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, ErrHTTPRegistryURLRequired
	}
	client, err := NewHTTPAgent(url, opts...)
	if err != nil {
		return nil, err
	}
	return &HTTPRegistry{url: url, client: client}, nil
}

// NewHTTPRegistryHandler exposes any CardLister as an HTTP agent-card registry.
func NewHTTPRegistryHandler(lister CardLister) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		if lister == nil {
			writeHTTPError(w, http.StatusInternalServerError, ErrDiscovererRequired)
			return
		}
		cards, err := lister.ListCards(r.Context())
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, fileDiscoveryDocument{Agents: cards})
	})
}

// WithHTTPClient sets the HTTP client used by an HTTP A2A agent.
func WithHTTPClient(client *http.Client) HTTPAgentOption {
	return func(agent *HTTPAgent) error {
		if client == nil {
			return errors.New("a2a: http client is required")
		}
		agent.client = client
		return nil
	}
}

// WithHTTPAgentCard sets the local card metadata exposed by Card.
func WithHTTPAgentCard(card AgentCard) HTTPAgentOption {
	return func(agent *HTTPAgent) error {
		card = copyAgentCard(card)
		if card.URL == "" {
			card.URL = agent.endpoint
		}
		agent.card = card
		return nil
	}
}

// WithHTTPHeader adds a static header to every HTTP request.
func WithHTTPHeader(key string, value string) HTTPAgentOption {
	return func(agent *HTTPAgent) error {
		if key == "" {
			return errors.New("a2a: http header key is required")
		}
		agent.headers.Set(key, value)
		return nil
	}
}

// WithHTTPMaxResponseBytes bounds non-stream response bodies and stream line size.
func WithHTTPMaxResponseBytes(n int64) HTTPAgentOption {
	return func(agent *HTTPAgent) error {
		if n <= 0 {
			return errors.New("a2a: max response bytes must be positive")
		}
		agent.maxResponseBytes = n
		return nil
	}
}

// Card returns configured card metadata for model-visible tool specs.
func (a *HTTPAgent) Card() AgentCard {
	if a == nil {
		return AgentCard{}
	}
	card := copyAgentCard(a.card)
	if card.URL == "" {
		card.URL = a.endpoint
	}
	return card
}

// Discover fetches an agent card from the HTTP endpoint.
func (a *HTTPAgent) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if a == nil {
		return DiscoveryResult{}, ErrHTTPEndpointRequired
	}
	endpoint := a.endpoint
	if query.URL != "" {
		endpoint = strings.TrimRight(strings.TrimSpace(query.URL), "/")
	}
	if endpoint == "" {
		return DiscoveryResult{}, ErrHTTPEndpointRequired
	}
	var card AgentCard
	if err := a.doJSON(ctx, http.MethodGet, endpoint+httpPathCard, nil, &card); err != nil {
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

// ListCards returns the HTTP endpoint's well-known agent card.
func (a *HTTPAgent) ListCards(ctx context.Context) ([]AgentCard, error) {
	result, err := a.Discover(ctx, DiscoveryQuery{})
	if err != nil {
		return nil, err
	}
	return []AgentCard{copyAgentCard(result.Card)}, nil
}

// ListCards returns all cards from the HTTP registry document in document order.
func (r *HTTPRegistry) ListCards(ctx context.Context) ([]AgentCard, error) {
	doc, err := r.readDocument(ctx)
	if err != nil {
		return nil, err
	}
	cards := make([]AgentCard, 0, len(doc.Agents))
	for _, card := range doc.Agents {
		if card.Name == "" {
			return nil, ErrCardNameRequired
		}
		cards = append(cards, copyAgentCard(card))
	}
	return cards, nil
}

// Discover returns the first registry card matching the discovery query.
func (r *HTTPRegistry) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	if r == nil || r.url == "" || r.client == nil {
		return DiscoveryResult{}, ErrHTTPRegistryURLRequired
	}
	if !hasDiscoveryCriteria(query) {
		return DiscoveryResult{}, ErrDiscoveryRequired
	}
	doc, err := r.readDocument(ctx)
	if err != nil {
		return DiscoveryResult{}, err
	}
	for _, card := range doc.Agents {
		if !matchesDiscoveryQuery(card, query) {
			continue
		}
		if card.Name == "" {
			return DiscoveryResult{}, ErrCardNameRequired
		}
		return DiscoveryResult{
			Card:     copyAgentCard(card),
			Metadata: map[string]any{"source": "http_registry"},
		}, nil
	}
	return DiscoveryResult{}, ErrAgentNotFound
}

// Send posts one task to the HTTP endpoint.
func (a *HTTPAgent) Send(ctx context.Context, task Task) (Result, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if a == nil {
		return Result{}, ErrHTTPEndpointRequired
	}
	task = taskWithContextAuth(ctx, task)
	var result Result
	if err := a.doJSON(ctx, http.MethodPost, a.endpoint+httpPathTaskSend, task, &result); err != nil {
		return Result{}, err
	}
	return copyResult(result), nil
}

// Stream posts one task and decodes JSONL task events from the HTTP endpoint.
func (a *HTTPAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if a == nil {
			yield(failedTaskEvent(task, ErrHTTPEndpointRequired), ErrHTTPEndpointRequired)
			return
		}
		task = taskWithContextAuth(ctx, task)
		resp, err := a.do(ctx, http.MethodPost, a.endpoint+httpPathTaskStream, task)
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

		scanner := bufio.NewScanner(resp.Body)
		maxLine := int(a.maxResponseBytes)
		if maxLine < 64*1024 {
			maxLine = 64 * 1024
		}
		scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var frame httpTaskEventFrame
			if err := json.Unmarshal(line, &frame); err != nil {
				wrapped := fmt.Errorf("a2a: decode stream event: %w", err)
				yield(failedTaskEvent(task, wrapped), wrapped)
				return
			}
			event := frame.Event.WithDefaults(task)
			if frame.Error != "" {
				err := errors.New(frame.Error)
				if !yield(event, err) {
					return
				}
				return
			}
			if !yield(event, nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			wrapped := fmt.Errorf("a2a: read stream event: %w", err)
			yield(failedTaskEvent(task, wrapped), wrapped)
		}
	}
}

// Cancel posts a task cancellation request to the HTTP endpoint.
func (a *HTTPAgent) Cancel(ctx context.Context, taskID string) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if a == nil {
		return ErrHTTPEndpointRequired
	}
	if taskID == "" {
		return ErrTaskIDRequired
	}
	var out struct{}
	return a.doJSON(ctx, http.MethodPost, a.endpoint+httpPathTaskCancel, httpCancelRequest{TaskID: taskID}, &out)
}

func (r *HTTPRegistry) readDocument(ctx context.Context) (fileDiscoveryDocument, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return fileDiscoveryDocument{}, err
	}
	if r == nil || r.url == "" || r.client == nil {
		return fileDiscoveryDocument{}, ErrHTTPRegistryURLRequired
	}
	resp, err := r.client.do(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return fileDiscoveryDocument{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if err := r.client.checkStatus(resp); err != nil {
		return fileDiscoveryDocument{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, r.client.maxResponseBytes+1))
	if err != nil {
		return fileDiscoveryDocument{}, fmt.Errorf("a2a: read http registry: %w", err)
	}
	if int64(len(raw)) > r.client.maxResponseBytes {
		return fileDiscoveryDocument{}, errors.New("a2a: http response too large")
	}
	return decodeDiscoveryDocument(raw, "http registry")
}

func (a *HTTPAgent) doJSON(ctx context.Context, method string, url string, input any, output any) error {
	resp, err := a.do(ctx, method, url, input)
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

func (a *HTTPAgent) do(ctx context.Context, method string, url string, input any) (*http.Response, error) {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("a2a: encode http request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("a2a: create http request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for key, values := range a.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a: http request: %w", err)
	}
	return resp, nil
}

func (a *HTTPAgent) checkStatus(resp *http.Response) error {
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

// HTTPHandlerOption configures an HTTP A2A server handler.
type HTTPHandlerOption func(*httpHandler)

// WithHTTPHandlerAgentCard sets the card metadata exposed by the HTTP handler.
func WithHTTPHandlerAgentCard(card AgentCard) HTTPHandlerOption {
	return func(handler *httpHandler) {
		handler.cardValue = copyAgentCard(card)
		handler.hasCard = true
	}
}

// NewHTTPHandler exposes an A2A agent through a minimal HTTP JSON/JSONL API.
func NewHTTPHandler(agent Agent, opts ...HTTPHandlerOption) http.Handler {
	h := &httpHandler{agent: agent}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(httpPathCard, h.card)
	mux.HandleFunc(httpPathTaskSend, h.send)
	mux.HandleFunc(httpPathTaskStream, h.stream)
	mux.HandleFunc(httpPathTaskCancel, h.cancel)
	mux.HandleFunc(httpPathHealth, h.health)
	mux.HandleFunc(httpPathReady, h.ready)
	return mux
}

type httpHandler struct {
	agent     Agent
	cardValue AgentCard
	hasCard   bool
}

func (h *httpHandler) health(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeHTTPJSON(w, http.StatusOK, httpStatusResponse{Status: "ok"})
}

func (h *httpHandler) ready(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if h.agent == nil {
		writeHTTPJSON(w, http.StatusServiceUnavailable, httpStatusResponse{Status: "not_ready"})
		return
	}
	writeHTTPJSON(w, http.StatusOK, httpStatusResponse{Status: "ready"})
}

func (h *httpHandler) card(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if h.agent == nil {
		writeHTTPError(w, http.StatusInternalServerError, ErrAgentNotFound)
		return
	}
	if h.hasCard {
		writeHTTPJSON(w, http.StatusOK, copyAgentCard(h.cardValue))
		return
	}
	writeHTTPJSON(w, http.StatusOK, h.agent.Card())
}

func (h *httpHandler) send(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if h.agent == nil {
		writeHTTPError(w, http.StatusInternalServerError, ErrAgentNotFound)
		return
	}
	var task Task
	if err := decodeHTTPJSON(r, &task); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	ctx := requestContextWithTaskAuth(r.Context(), task)
	result, err := h.agent.Send(ctx, task)
	if err != nil {
		writeHTTPError(w, http.StatusBadGateway, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *httpHandler) stream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if h.agent == nil {
		writeHTTPError(w, http.StatusInternalServerError, ErrAgentNotFound)
		return
	}
	var task Task
	if err := decodeHTTPJSON(r, &task); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	streamer, ok := h.agent.(StreamingAgent)
	if !ok {
		writeHTTPError(w, http.StatusNotImplemented, ErrStreamNotSupported)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	ctx := requestContextWithTaskAuth(r.Context(), task)
	for event, err := range streamer.Stream(ctx, task) {
		frame := httpTaskEventFrame{Event: event.WithDefaults(task)}
		if err != nil {
			frame.Error = err.Error()
		}
		if encodeErr := encoder.Encode(frame); encodeErr != nil {
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

func (h *httpHandler) cancel(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if h.agent == nil {
		writeHTTPError(w, http.StatusInternalServerError, ErrAgentNotFound)
		return
	}
	var req httpCancelRequest
	if err := decodeHTTPJSON(r, &req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	if req.TaskID == "" {
		writeHTTPError(w, http.StatusBadRequest, ErrTaskIDRequired)
		return
	}
	if err := h.agent.Cancel(r.Context(), req.TaskID); err != nil {
		writeHTTPError(w, http.StatusBadGateway, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, struct{}{})
}

type httpCancelRequest struct {
	TaskID string `json:"task_id"`
}

type httpTaskEventFrame struct {
	Event TaskEvent `json:"event"`
	Error string    `json:"error,omitempty"`
}

type httpErrorResponse struct {
	Error string `json:"error"`
}

type httpStatusResponse struct {
	Status string `json:"status"`
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeHTTPError(w, http.StatusMethodNotAllowed, fmt.Errorf("a2a: method %s not allowed", r.Method))
	return false
}

func decodeHTTPJSON(r *http.Request, output any) error {
	defer func() {
		_ = r.Body.Close()
	}()
	decoder := json.NewDecoder(io.LimitReader(r.Body, defaultHTTPMaxResponseBytes+1))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("a2a: decode http request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("a2a: decode http request: multiple json values")
		}
		return fmt.Errorf("a2a: decode http request: %w", err)
	}
	return nil
}

func decodeLimitedJSON(body io.Reader, limit int64, output any) error {
	limited := io.LimitReader(body, limit+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("a2a: read http response: %w", err)
	}
	if int64(len(raw)) > limit {
		return errors.New("a2a: http response too large")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("a2a: decode http response: %w", err)
	}
	return nil
}

func writeHTTPJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeHTTPError(w http.ResponseWriter, status int, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	writeHTTPJSON(w, status, httpErrorResponse{Error: message})
}

func taskWithContextAuth(ctx context.Context, task Task) Task {
	task = copyTask(task)
	if task.Auth == nil {
		if auth, ok := AuthFromContext(ctx); ok && !auth.IsZero() {
			task.Auth = &auth
		}
	}
	return task
}

func requestContextWithTaskAuth(ctx context.Context, task Task) context.Context {
	if task.Auth == nil || task.Auth.IsZero() {
		return ctx
	}
	return ContextWithAuth(ctx, *task.Auth)
}

func copyResult(result Result) Result {
	result.Artifacts = copyArtifactRefs(result.Artifacts)
	result.Metadata = copyAnyMap(result.Metadata)
	return result
}
