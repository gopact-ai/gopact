package trace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultLangSmithRunsPath        = "/runs"
	defaultLangSmithMaxResponseSize = 1 << 20
)

// LangSmithHTTPExporter posts span records as LangSmith-style run payloads.
type LangSmithHTTPExporter struct {
	endpoint         url.URL
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
	project          string
}

var _ Exporter = (*LangSmithHTTPExporter)(nil)

// LangSmithHTTPExporterOption configures a LangSmith HTTP exporter.
type LangSmithHTTPExporterOption func(*langSmithHTTPExporterConfig) error

type langSmithHTTPExporterConfig struct {
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
	project          string
	apiKey           string
}

// NewLangSmithHTTPExporter creates an exporter backed by a host-owned LangSmith-compatible endpoint.
func NewLangSmithHTTPExporter(endpoint string, opts ...LangSmithHTTPExporterOption) (*LangSmithHTTPExporter, error) {
	parsed, err := parseHTTPEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	parsed = withDefaultLangSmithRunsPath(parsed)
	cfg := langSmithHTTPExporterConfig{
		client:           &http.Client{Timeout: defaultHTTPTimeout},
		headers:          make(http.Header),
		maxResponseBytes: defaultLangSmithMaxResponseSize,
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
	return &LangSmithHTTPExporter{
		endpoint:         parsed,
		client:           cfg.client,
		headers:          cfg.headers.Clone(),
		maxResponseBytes: cfg.maxResponseBytes,
		project:          strings.TrimSpace(cfg.project),
	}, nil
}

// WithLangSmithHTTPClient replaces the default HTTP client.
func WithLangSmithHTTPClient(client *http.Client) LangSmithHTTPExporterOption {
	return func(cfg *langSmithHTTPExporterConfig) error {
		if client == nil {
			return ErrHTTPClientRequired
		}
		cfg.client = client
		return nil
	}
}

// WithLangSmithHeader adds a header to every LangSmith request.
func WithLangSmithHeader(key, value string) LangSmithHTTPExporterOption {
	return func(cfg *langSmithHTTPExporterConfig) error {
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

// WithLangSmithAPIKey sets the x-api-key header used by LangSmith-compatible collectors.
func WithLangSmithAPIKey(apiKey string) LangSmithHTTPExporterOption {
	return func(cfg *langSmithHTTPExporterConfig) error {
		cfg.apiKey = strings.TrimSpace(apiKey)
		return nil
	}
}

// WithLangSmithProject sets the LangSmith project/session name attached to exported runs.
func WithLangSmithProject(project string) LangSmithHTTPExporterOption {
	return func(cfg *langSmithHTTPExporterConfig) error {
		cfg.project = strings.TrimSpace(project)
		return nil
	}
}

// WithLangSmithMaxResponseBytes limits collector response bodies read by the exporter.
func WithLangSmithMaxResponseBytes(n int64) LangSmithHTTPExporterOption {
	return func(cfg *langSmithHTTPExporterConfig) error {
		if n <= 0 {
			return ErrMaxResponseRequired
		}
		cfg.maxResponseBytes = n
		return nil
	}
}

// ExportSpan posts one span record as a LangSmith-style run.
func (e *LangSmithHTTPExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if e == nil || e.client == nil || e.endpoint.Scheme == "" {
		return ErrHTTPExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(e.run(copySpan(span)))
	if err != nil {
		return fmt.Errorf("trace: encode langsmith run: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("trace: create langsmith export request: %w", err)
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
		return fmt.Errorf("trace: post langsmith run: %w", err)
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

type langSmithRun struct {
	ID          string         `json:"id"`
	TraceID     string         `json:"trace_id,omitempty"`
	ParentRunID string         `json:"parent_run_id,omitempty"`
	Name        string         `json:"name"`
	RunType     string         `json:"run_type"`
	SessionName string         `json:"session_name,omitempty"`
	StartTime   time.Time      `json:"start_time"`
	EndTime     *time.Time     `json:"end_time,omitempty"`
	Error       string         `json:"error,omitempty"`
	Inputs      map[string]any `json:"inputs,omitempty"`
	Outputs     map[string]any `json:"outputs,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
}

func (e *LangSmithHTTPExporter) run(span SpanRecord) langSmithRun {
	start := span.CreatedAt
	if start.IsZero() {
		start = time.Now().UTC()
	}
	start = start.UTC()
	project := e.project
	if project == "" {
		project = span.ServiceName
	}
	run := langSmithRun{
		ID:          langSmithID(langSmithRunSeed(span)),
		TraceID:     langSmithID(firstNonEmpty(span.IDs.TraceID, span.IDs.RunID, span.IDs.ThreadID, span.Name)),
		ParentRunID: langSmithOptionalID(span.IDs.ParentCallID),
		Name:        langSmithRunName(span),
		RunType:     langSmithRunType(span.Kind),
		SessionName: project,
		StartTime:   start,
		Inputs: map[string]any{
			"event_type": string(span.EventType),
			"kind":       string(span.Kind),
		},
		Outputs: map[string]any{
			"status": string(span.Status),
		},
		Extra: map[string]any{
			"metadata": langSmithMetadata(span),
		},
		Tags: langSmithTags(span),
	}
	if span.Node != "" {
		run.Inputs["node"] = span.Node
	}
	if span.Step != 0 {
		run.Inputs["step"] = span.Step
	}
	if span.Error != "" {
		run.Error = span.Error
		run.Outputs["error"] = span.Error
	}
	if langSmithTerminal(span.Status) {
		run.EndTime = &start
	}
	return run
}

func withDefaultLangSmithRunsPath(endpoint url.URL) url.URL {
	if endpoint.Path == "" || endpoint.Path == "/" {
		endpoint.Path = defaultLangSmithRunsPath
	}
	return endpoint
}

func langSmithRunName(span SpanRecord) string {
	if span.Name != "" {
		return span.Name
	}
	if span.EventType != "" {
		return string(span.EventType)
	}
	return "gopact.run"
}

func langSmithRunType(kind SpanKind) string {
	switch kind {
	case SpanKindModel:
		return "llm"
	case SpanKindTool, SpanKindSandbox, SpanKindA2A:
		return "tool"
	case SpanKindMemory:
		return "retriever"
	default:
		return "chain"
	}
}

func langSmithTerminal(status SpanStatus) bool {
	switch status {
	case SpanStatusCompleted, SpanStatusFailed, SpanStatusCanceled, SpanStatusInterrupted:
		return true
	default:
		return false
	}
}

func langSmithTags(span SpanRecord) []string {
	tags := []string{"gopact"}
	if span.Kind != "" {
		tags = append(tags, string(span.Kind))
	}
	if span.Status != "" {
		tags = append(tags, string(span.Status))
	}
	return tags
}

func langSmithMetadata(span SpanRecord) map[string]any {
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

func langSmithRunSeed(span SpanRecord) string {
	if strings.TrimSpace(span.IDs.CallID) != "" {
		return span.IDs.CallID
	}
	parts := []string{
		span.IDs.TraceID,
		span.IDs.RunID,
		span.IDs.ThreadID,
		string(span.EventType),
		span.Name,
		span.Node,
		fmt.Sprintf("%d", span.Step),
	}
	if !span.CreatedAt.IsZero() {
		parts = append(parts, span.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	return strings.Join(parts, "|")
}

func addStringMetadata(metadata map[string]any, key, value string) {
	if value != "" {
		metadata[key] = value
	}
}

func langSmithID(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		seed = "gopact"
	}
	if isCanonicalUUID(seed) {
		return strings.ToLower(seed)
	}
	sum := sha256.Sum256([]byte(seed))
	raw := append([]byte(nil), sum[:16]...)
	raw[6] = (raw[6] & 0x0f) | 0x50
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(raw[0:4]),
		hex.EncodeToString(raw[4:6]),
		hex.EncodeToString(raw[6:8]),
		hex.EncodeToString(raw[8:10]),
		hex.EncodeToString(raw[10:16]),
	)
}

func langSmithOptionalID(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	return langSmithID(seed)
}

func isCanonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
			continue
		}
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
