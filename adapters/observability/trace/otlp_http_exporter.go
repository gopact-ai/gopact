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
	"sort"
	"strconv"
	"strings"

	"github.com/gopact-ai/gopact"
)

const (
	defaultOTLPTracePath        = "/v1/traces"
	defaultOTLPMaxResponseBytes = 4 << 20
	defaultOTLPScopeName        = PluginName
	defaultOTLPScopeVersion     = "v0.1.0"

	otlpSpanKindInternal = 1
	otlpSpanKindClient   = 3

	otlpStatusOK    = 1
	otlpStatusError = 2
)

// ErrPartialSuccess is returned when an OTLP collector reports partial trace ingestion.
var ErrPartialSuccess = errors.New("trace: otlp partial success")

// OTLPHTTPExporter posts span records as OTLP/HTTP JSON trace requests.
type OTLPHTTPExporter struct {
	endpoint         url.URL
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
	scopeName        string
	scopeVersion     string
	resourceAttrs    map[string]string
}

var _ Exporter = (*OTLPHTTPExporter)(nil)

// OTLPHTTPExporterOption configures an OTLP/HTTP trace exporter.
type OTLPHTTPExporterOption func(*otlpHTTPExporterConfig) error

type otlpHTTPExporterConfig struct {
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
	scopeName        string
	scopeVersion     string
	resourceAttrs    map[string]string
}

// NewOTLPHTTPExporter creates an exporter backed by an OTLP/HTTP JSON endpoint.
func NewOTLPHTTPExporter(endpoint string, opts ...OTLPHTTPExporterOption) (*OTLPHTTPExporter, error) {
	parsed, err := parseHTTPEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	parsed = withDefaultOTLPTracePath(parsed)
	cfg := otlpHTTPExporterConfig{
		client:           &http.Client{Timeout: defaultHTTPTimeout},
		headers:          make(http.Header),
		maxResponseBytes: defaultOTLPMaxResponseBytes,
		scopeName:        defaultOTLPScopeName,
		scopeVersion:     defaultOTLPScopeVersion,
		resourceAttrs:    make(map[string]string),
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
	return &OTLPHTTPExporter{
		endpoint:         parsed,
		client:           cfg.client,
		headers:          cfg.headers.Clone(),
		maxResponseBytes: cfg.maxResponseBytes,
		scopeName:        cfg.scopeName,
		scopeVersion:     cfg.scopeVersion,
		resourceAttrs:    copyStringMap(cfg.resourceAttrs),
	}, nil
}

// WithOTLPHTTPClient replaces the default HTTP client.
func WithOTLPHTTPClient(client *http.Client) OTLPHTTPExporterOption {
	return func(cfg *otlpHTTPExporterConfig) error {
		if client == nil {
			return ErrHTTPClientRequired
		}
		cfg.client = client
		return nil
	}
}

// WithOTLPHeader adds a header to every OTLP request.
func WithOTLPHeader(key, value string) OTLPHTTPExporterOption {
	return func(cfg *otlpHTTPExporterConfig) error {
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

// WithOTLPMaxResponseBytes limits collector response bodies read by the exporter.
func WithOTLPMaxResponseBytes(n int64) OTLPHTTPExporterOption {
	return func(cfg *otlpHTTPExporterConfig) error {
		if n <= 0 {
			return ErrMaxResponseRequired
		}
		cfg.maxResponseBytes = n
		return nil
	}
}

// WithOTLPScope sets the OTLP instrumentation scope name and version.
func WithOTLPScope(name, version string) OTLPHTTPExporterOption {
	return func(cfg *otlpHTTPExporterConfig) error {
		name = strings.TrimSpace(name)
		if name != "" {
			cfg.scopeName = name
		}
		cfg.scopeVersion = strings.TrimSpace(version)
		return nil
	}
}

// WithOTLPResourceAttribute adds a static OTLP resource attribute.
func WithOTLPResourceAttribute(key, value string) OTLPHTTPExporterOption {
	return func(cfg *otlpHTTPExporterConfig) error {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("trace: resource attribute key is required")
		}
		if cfg.resourceAttrs == nil {
			cfg.resourceAttrs = make(map[string]string)
		}
		cfg.resourceAttrs[key] = value
		return nil
	}
}

// ExportSpan posts one span record as an OTLP ExportTraceServiceRequest.
func (e *OTLPHTTPExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if e == nil || e.client == nil || e.endpoint.Scheme == "" {
		return ErrHTTPExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(e.traceRequest(copySpan(span)))
	if err != nil {
		return fmt.Errorf("trace: encode otlp trace request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("trace: create otlp export request: %w", err)
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
		return fmt.Errorf("trace: post otlp span: %w", err)
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
	if err := checkOTLPPartialSuccess(body); err != nil {
		return err
	}
	return nil
}

func (e *OTLPHTTPExporter) traceRequest(span SpanRecord) otlpExportTraceServiceRequest {
	return otlpExportTraceServiceRequest{
		ResourceSpans: []otlpResourceSpans{
			{
				Resource: otlpResource{
					Attributes: e.resourceAttributes(span),
				},
				ScopeSpans: []otlpScopeSpans{
					{
						Scope: otlpScope{
							Name:    e.scopeName,
							Version: e.scopeVersion,
						},
						Spans: []otlpSpan{otlpSpanFromRecord(span)},
					},
				},
			},
		},
	}
}

func (e *OTLPHTTPExporter) resourceAttributes(span SpanRecord) []otlpKeyValue {
	attrs := copyStringMap(e.resourceAttrs)
	if attrs == nil {
		attrs = make(map[string]string)
	}
	if span.ServiceName != "" {
		attrs["service.name"] = span.ServiceName
	} else if attrs["service.name"] == "" {
		attrs["service.name"] = defaultServiceName
	}
	return otlpStringAttributes(attrs)
}

func otlpSpanFromRecord(span SpanRecord) otlpSpan {
	out := otlpSpan{
		TraceID:      otlpTraceID(span),
		SpanID:       otlpSpanID(span),
		ParentSpanID: otlpParentSpanID(span),
		Name:         otlpSpanName(span),
		Kind:         otlpSpanKind(span.Kind),
		Attributes:   otlpSpanAttributes(span),
		Status:       otlpStatus(span),
	}
	if !span.CreatedAt.IsZero() {
		nano := strconv.FormatInt(span.CreatedAt.UnixNano(), 10)
		out.StartTimeUnixNano = nano
		out.EndTimeUnixNano = nano
	}
	return out
}

func otlpSpanName(span SpanRecord) string {
	if span.Name != "" {
		return span.Name
	}
	if span.EventType != "" {
		return string(span.EventType)
	}
	return "gopact.span"
}

func otlpSpanKind(kind SpanKind) int {
	switch kind {
	case SpanKindModel, SpanKindTool, SpanKindSandbox, SpanKindA2A:
		return otlpSpanKindClient
	default:
		return otlpSpanKindInternal
	}
}

func otlpStatus(span SpanRecord) *otlpStatusValue {
	switch span.Status {
	case SpanStatusCompleted:
		return &otlpStatusValue{Code: otlpStatusOK}
	case SpanStatusFailed, SpanStatusCanceled, SpanStatusInterrupted:
		status := &otlpStatusValue{Code: otlpStatusError}
		if span.Error != "" {
			status.Message = span.Error
		}
		return status
	default:
		if span.Error != "" {
			return &otlpStatusValue{Code: otlpStatusError, Message: span.Error}
		}
		return nil
	}
}

func otlpSpanAttributes(span SpanRecord) []otlpKeyValue {
	attrs := copyStringMap(span.Attributes)
	if attrs == nil {
		attrs = make(map[string]string)
	}
	if span.Kind != "" {
		attrs["gopact.kind"] = string(span.Kind)
	}
	if span.Status != "" {
		attrs["gopact.status"] = string(span.Status)
	}
	if span.EventType != "" {
		attrs["gopact.event_type"] = string(span.EventType)
	}
	if span.Node != "" {
		attrs["gopact.node"] = span.Node
	}
	if span.Error != "" {
		attrs["gopact.error"] = span.Error
	}
	addRuntimeIDAttributes(attrs, span.IDs)

	out := otlpStringAttributes(attrs)
	if span.Step != 0 {
		out = append(out, otlpIntAttribute("gopact.step", int64(span.Step)))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}

func addRuntimeIDAttributes(attrs map[string]string, ids gopact.RuntimeIDs) {
	add := func(key, value string) {
		if value != "" {
			attrs[key] = value
		}
	}
	add("gopact.app_id", ids.AppID)
	add("gopact.user_id", ids.UserID)
	add("gopact.session_id", ids.SessionID)
	add("gopact.thread_id", ids.ThreadID)
	add("gopact.run_id", ids.RunID)
	add("gopact.agent_id", ids.AgentID)
	add("gopact.call_id", ids.CallID)
	add("gopact.parent_call_id", ids.ParentCallID)
	add("gopact.trace_id", ids.TraceID)
}

func otlpStringAttributes(attrs map[string]string) []otlpKeyValue {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]otlpKeyValue, 0, len(keys))
	for _, key := range keys {
		out = append(out, otlpStringAttribute(key, attrs[key]))
	}
	return out
}

func otlpStringAttribute(key, value string) otlpKeyValue {
	return otlpKeyValue{Key: key, Value: map[string]string{"stringValue": value}}
}

func otlpIntAttribute(key string, value int64) otlpKeyValue {
	return otlpKeyValue{Key: key, Value: map[string]string{"intValue": strconv.FormatInt(value, 10)}}
}

func otlpTraceID(span SpanRecord) string {
	if id, ok := validOTLPHexID(span.IDs.TraceID, 16); ok {
		return id
	}
	return hashOTLPID(16,
		span.IDs.TraceID,
		span.IDs.RunID,
		span.IDs.ThreadID,
		span.IDs.SessionID,
		span.IDs.UserID,
		span.ServiceName,
	)
}

func otlpSpanID(span SpanRecord) string {
	if id, ok := validOTLPHexID(span.IDs.CallID, 8); ok {
		return id
	}
	return hashOTLPID(8,
		span.IDs.CallID,
		span.IDs.RunID,
		span.Node,
		strconv.Itoa(span.Step),
		string(span.EventType),
		span.Name,
		strconv.FormatInt(span.CreatedAt.UnixNano(), 10),
	)
}

func otlpParentSpanID(span SpanRecord) string {
	if span.IDs.ParentCallID == "" {
		return ""
	}
	if id, ok := validOTLPHexID(span.IDs.ParentCallID, 8); ok {
		return id
	}
	return hashOTLPID(8, span.IDs.ParentCallID, span.IDs.RunID)
}

func validOTLPHexID(value string, bytes int) (string, bool) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(value) != bytes*2 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	if strings.Trim(value, "0") == "" {
		return "", false
	}
	return value, true
}

func hashOTLPID(bytes int, parts ...string) string {
	seed := strings.Join(parts, "\x00")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:bytes])
}

func withDefaultOTLPTracePath(endpoint url.URL) url.URL {
	if endpoint.Path == "" || endpoint.Path == "/" {
		endpoint.Path = defaultOTLPTracePath
	}
	return endpoint
}

func checkOTLPPartialSuccess(body []byte) error {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	var resp otlpExportTraceServiceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("trace: decode otlp response: %w", err)
	}
	if resp.PartialSuccess == nil {
		return nil
	}
	rejected, err := parseOTLPInt64(resp.PartialSuccess.RejectedSpans)
	if err != nil {
		return fmt.Errorf("trace: decode otlp partial success: %w", err)
	}
	if rejected <= 0 {
		return nil
	}
	if resp.PartialSuccess.ErrorMessage == "" {
		return fmt.Errorf("%w: rejected spans %d", ErrPartialSuccess, rejected)
	}
	return fmt.Errorf("%w: rejected spans %d: %s", ErrPartialSuccess, rejected, resp.PartialSuccess.ErrorMessage)
}

func parseOTLPInt64(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return 0, nil
		}
		return strconv.ParseInt(text, 10, 64)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, err
	}
	return number.Int64()
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type otlpExportTraceServiceRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans,omitempty"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource,omitempty"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans,omitempty"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope,omitempty"`
	Spans []otlpSpan `json:"spans,omitempty"`
}

type otlpScope struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type otlpSpan struct {
	TraceID           string           `json:"traceId,omitempty"`
	SpanID            string           `json:"spanId,omitempty"`
	ParentSpanID      string           `json:"parentSpanId,omitempty"`
	Name              string           `json:"name,omitempty"`
	Kind              int              `json:"kind,omitempty"`
	StartTimeUnixNano string           `json:"startTimeUnixNano,omitempty"`
	EndTimeUnixNano   string           `json:"endTimeUnixNano,omitempty"`
	Attributes        []otlpKeyValue   `json:"attributes,omitempty"`
	Status            *otlpStatusValue `json:"status,omitempty"`
}

type otlpStatusValue struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type otlpKeyValue struct {
	Key   string            `json:"key,omitempty"`
	Value map[string]string `json:"value,omitempty"`
}

type otlpExportTraceServiceResponse struct {
	PartialSuccess *otlpPartialSuccess `json:"partialSuccess,omitempty"`
}

type otlpPartialSuccess struct {
	RejectedSpans json.RawMessage `json:"rejectedSpans,omitempty"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
}
