package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout          = 30 * time.Second
	defaultHTTPMaxResponseBytes = 1 << 20
)

var (
	// ErrHTTPExporterRequired is returned when a policy wrapper is created without an HTTP exporter.
	ErrHTTPExporterRequired = errors.New("trace: http exporter is required")
	// ErrEndpointRequired is returned when an HTTP exporter is created without an endpoint.
	ErrEndpointRequired = errors.New("trace: endpoint is required")
	// ErrInvalidEndpoint is returned when the configured exporter endpoint URL is invalid.
	ErrInvalidEndpoint = errors.New("trace: invalid endpoint")
	// ErrHTTPClientRequired is returned when a nil HTTP client is supplied.
	ErrHTTPClientRequired = errors.New("trace: http client is required")
	// ErrUnexpectedStatus is returned when the collector returns an unexpected HTTP status.
	ErrUnexpectedStatus = errors.New("trace: unexpected status")
	// ErrResponseTooLarge is returned when a collector response exceeds the configured byte limit.
	ErrResponseTooLarge = errors.New("trace: response too large")
	// ErrMaxResponseRequired is returned when max response bytes is not positive.
	ErrMaxResponseRequired = errors.New("trace: max response bytes must be positive")
)

// HTTPExporter posts span records as JSON to a host-owned collector endpoint.
type HTTPExporter struct {
	endpoint         url.URL
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

var _ Exporter = (*HTTPExporter)(nil)

// HTTPExporterOption configures an HTTP trace exporter.
type HTTPExporterOption func(*httpExporterConfig) error

type httpExporterConfig struct {
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

// NewHTTPExporter creates an exporter backed by an HTTP/JSON collector.
func NewHTTPExporter(endpoint string, opts ...HTTPExporterOption) (*HTTPExporter, error) {
	parsed, err := parseHTTPEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	cfg := httpExporterConfig{
		client:           &http.Client{Timeout: defaultHTTPTimeout},
		headers:          make(http.Header),
		maxResponseBytes: defaultHTTPMaxResponseBytes,
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
	return &HTTPExporter{
		endpoint:         parsed,
		client:           cfg.client,
		headers:          cfg.headers.Clone(),
		maxResponseBytes: cfg.maxResponseBytes,
	}, nil
}

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(client *http.Client) HTTPExporterOption {
	return func(cfg *httpExporterConfig) error {
		if client == nil {
			return ErrHTTPClientRequired
		}
		cfg.client = client
		return nil
	}
}

// WithHeader adds a header to every exporter request.
func WithHeader(key, value string) HTTPExporterOption {
	return func(cfg *httpExporterConfig) error {
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

// WithMaxResponseBytes limits collector response bodies read by the exporter.
func WithMaxResponseBytes(n int64) HTTPExporterOption {
	return func(cfg *httpExporterConfig) error {
		if n <= 0 {
			return ErrMaxResponseRequired
		}
		cfg.maxResponseBytes = n
		return nil
	}
}

// ExportSpan posts one provider-neutral span record as JSON.
func (e *HTTPExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if e == nil || e.client == nil || e.endpoint.Scheme == "" {
		return ErrHTTPExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(copySpan(span))
	if err != nil {
		return fmt.Errorf("trace: encode span: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("trace: create export request: %w", err)
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
		return fmt.Errorf("trace: post span: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := e.readResponse(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unexpectedHTTPStatus(resp.StatusCode, body)
	}
	return nil
}

func parseHTTPEndpoint(endpoint string) (url.URL, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return url.URL{}, ErrEndpointRequired
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return url.URL{}, fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return url.URL{}, fmt.Errorf("%w: %q", ErrInvalidEndpoint, endpoint)
	}
	parsed.Fragment = ""
	return *parsed, nil
}

func (e *HTTPExporter) readResponse(body io.Reader) ([]byte, error) {
	return readLimitedResponse(body, e.maxResponseBytes)
}

func readLimitedResponse(body io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(body, maxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("trace: read export response: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, ErrResponseTooLarge
	}
	return raw, nil
}

func unexpectedHTTPStatus(status int, body []byte) error {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Errorf("%w: status %d", ErrUnexpectedStatus, status)
	}
	return fmt.Errorf("%w: status %d: %s", ErrUnexpectedStatus, status, string(body))
}
