package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultLangGraphEventsPath       = "/runs/events"
	defaultLangGraphMaxResponseBytes = 1 << 20
)

// LangGraphHTTPExporter posts span records as LangGraph-style run event payloads.
type LangGraphHTTPExporter struct {
	endpoint         url.URL
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

var _ Exporter = (*LangGraphHTTPExporter)(nil)

// LangGraphHTTPExporterOption configures a LangGraph HTTP exporter.
type LangGraphHTTPExporterOption func(*langGraphHTTPExporterConfig) error

type langGraphHTTPExporterConfig struct {
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
	apiKey           string
}

// NewLangGraphHTTPExporter creates an exporter backed by a host-owned LangGraph-style collector.
func NewLangGraphHTTPExporter(endpoint string, opts ...LangGraphHTTPExporterOption) (*LangGraphHTTPExporter, error) {
	parsed, err := parseHTTPEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	parsed = withDefaultLangGraphEventsPath(parsed)
	cfg := langGraphHTTPExporterConfig{
		client:           &http.Client{Timeout: defaultHTTPTimeout},
		headers:          make(http.Header),
		maxResponseBytes: defaultLangGraphMaxResponseBytes,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.client == nil {
		return nil, ErrHTTPClientRequired
	}
	if cfg.maxResponseBytes <= 0 {
		return nil, ErrMaxResponseRequired
	}
	if strings.TrimSpace(cfg.apiKey) != "" {
		cfg.headers.Set("x-api-key", strings.TrimSpace(cfg.apiKey))
	}
	return &LangGraphHTTPExporter{
		endpoint:         parsed,
		client:           cfg.client,
		headers:          cfg.headers.Clone(),
		maxResponseBytes: cfg.maxResponseBytes,
	}, nil
}

// WithLangGraphHTTPClient replaces the default HTTP client.
func WithLangGraphHTTPClient(client *http.Client) LangGraphHTTPExporterOption {
	return func(cfg *langGraphHTTPExporterConfig) error {
		if client == nil {
			return ErrHTTPClientRequired
		}
		cfg.client = client
		return nil
	}
}

// WithLangGraphHeader adds a header to every LangGraph-style request.
func WithLangGraphHeader(key, value string) LangGraphHTTPExporterOption {
	return func(cfg *langGraphHTTPExporterConfig) error {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("trace: header key is required")
		}
		if cfg.headers == nil {
			cfg.headers = make(http.Header)
		}
		cfg.headers.Set(key, value)
		return nil
	}
}

// WithLangGraphAPIKey sets the x-api-key header used by host-owned collectors.
func WithLangGraphAPIKey(apiKey string) LangGraphHTTPExporterOption {
	return func(cfg *langGraphHTTPExporterConfig) error {
		cfg.apiKey = strings.TrimSpace(apiKey)
		return nil
	}
}

// WithLangGraphMaxResponseBytes limits collector response bodies read by the exporter.
func WithLangGraphMaxResponseBytes(n int64) LangGraphHTTPExporterOption {
	return func(cfg *langGraphHTTPExporterConfig) error {
		if n <= 0 {
			return ErrMaxResponseRequired
		}
		cfg.maxResponseBytes = n
		return nil
	}
}

// ExportSpan posts one span record as a LangGraph-style event.
func (e *LangGraphHTTPExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if e == nil || e.client == nil || e.endpoint.Scheme == "" {
		return ErrHTTPExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(langGraphEventFromSpan(copySpan(span)))
	if err != nil {
		return fmt.Errorf("trace: encode langgraph event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("trace: create langgraph export request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for key, values := range e.headers {
		for _, value := range values {
			req.Header.Set(key, value)
		}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("trace: post langgraph event: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := readLimitedResponse(resp.Body, e.maxResponseBytes)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unexpectedHTTPStatus(resp.StatusCode, body)
	}
	return nil
}

type langGraphEvent struct {
	EventID         string         `json:"event_id,omitempty"`
	ThreadID        string         `json:"thread_id,omitempty"`
	RunID           string         `json:"run_id,omitempty"`
	AttemptID       string         `json:"attempt_id,omitempty"`
	ParentAttemptID string         `json:"parent_attempt_id,omitempty"`
	TraceID         string         `json:"trace_id,omitempty"`
	UserID          string         `json:"user_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	AgentID         string         `json:"agent_id,omitempty"`
	AppID           string         `json:"app_id,omitempty"`
	Event           string         `json:"event"`
	Name            string         `json:"name,omitempty"`
	Kind            string         `json:"kind,omitempty"`
	Status          string         `json:"status,omitempty"`
	Namespace       []string       `json:"namespace,omitempty"`
	Node            string         `json:"node,omitempty"`
	Step            int            `json:"step,omitempty"`
	Time            time.Time      `json:"time,omitempty"`
	Error           string         `json:"error,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

func langGraphEventFromSpan(span SpanRecord) langGraphEvent {
	createdAt := span.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	createdAt = createdAt.UTC()
	ids := span.IDs
	event := langGraphEvent{
		EventID:         langGraphEventID(span),
		ThreadID:        ids.ThreadID,
		RunID:           ids.RunID,
		AttemptID:       ids.CallID,
		ParentAttemptID: ids.ParentCallID,
		TraceID:         ids.TraceID,
		UserID:          ids.UserID,
		SessionID:       ids.SessionID,
		AgentID:         ids.AgentID,
		AppID:           ids.AppID,
		Event:           string(span.EventType),
		Name:            langGraphName(span),
		Kind:            string(span.Kind),
		Status:          string(span.Status),
		Namespace:       langGraphNamespace(span),
		Node:            span.Node,
		Step:            span.Step,
		Time:            createdAt,
		Error:           span.Error,
		Metadata:        langGraphMetadata(span),
	}
	if event.Event == "" {
		event.Event = "event"
	}
	return event
}

func withDefaultLangGraphEventsPath(endpoint url.URL) url.URL {
	if endpoint.Path == "" || endpoint.Path == "/" {
		endpoint.Path = defaultLangGraphEventsPath
	}
	return endpoint
}

func langGraphName(span SpanRecord) string {
	if span.Name != "" {
		return span.Name
	}
	if span.EventType != "" {
		return string(span.EventType)
	}
	return "gopact.event"
}

func langGraphNamespace(span SpanRecord) []string {
	namespace := make([]string, 0, 2)
	if span.Kind != "" {
		namespace = append(namespace, string(span.Kind))
	}
	if span.Node != "" {
		namespace = append(namespace, span.Node)
	}
	return namespace
}

func langGraphMetadata(span SpanRecord) map[string]any {
	metadata := make(map[string]any)
	ids := span.IDs
	addStringMetadata(metadata, "user_id", ids.UserID)
	addStringMetadata(metadata, "session_id", ids.SessionID)
	addStringMetadata(metadata, "thread_id", ids.ThreadID)
	addStringMetadata(metadata, "run_id", ids.RunID)
	addStringMetadata(metadata, "agent_id", ids.AgentID)
	addStringMetadata(metadata, "app_id", ids.AppID)
	addStringMetadata(metadata, "call_id", ids.CallID)
	addStringMetadata(metadata, "parent_call_id", ids.ParentCallID)
	addStringMetadata(metadata, "trace_id", ids.TraceID)
	addStringMetadata(metadata, "service_name", span.ServiceName)
	addStringMetadata(metadata, "event_type", string(span.EventType))
	addStringMetadata(metadata, "kind", string(span.Kind))
	addStringMetadata(metadata, "status", string(span.Status))
	addStringMetadata(metadata, "node", span.Node)
	if span.Step != 0 {
		metadata["step"] = span.Step
	}
	for key, value := range span.Attributes {
		if key == "" {
			continue
		}
		metadata[key] = value
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func langGraphEventID(span SpanRecord) string {
	seed := strings.Join([]string{
		span.IDs.CallID,
		span.IDs.TraceID,
		span.IDs.RunID,
		span.IDs.ThreadID,
		string(span.EventType),
		span.Name,
		span.Node,
		fmt.Sprintf("%d", span.Step),
		span.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, "|")
	return langSmithID(seed)
}
