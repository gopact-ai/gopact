// Package httpstore adapts an internal HTTP/JSON control plane to TurnLoop row and CAS backends.
package httpstore

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

	"github.com/gopact-ai/gopact"
)

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = 4 << 20

	rowPath       = "/turnloop/state"
	versionedPath = "/turnloop/versioned"
)

var (
	// ErrEndpointRequired is returned when a backend is created without an endpoint.
	ErrEndpointRequired = errors.New("turnloop httpstore: endpoint is required")
	// ErrInvalidEndpoint is returned when the configured endpoint URL is invalid.
	ErrInvalidEndpoint = errors.New("turnloop httpstore: invalid endpoint")
	// ErrHTTPClientRequired is returned when a nil HTTP client is supplied.
	ErrHTTPClientRequired = errors.New("turnloop httpstore: http client is required")
	// ErrUnexpectedStatus is returned when the control plane returns an unexpected status.
	ErrUnexpectedStatus = errors.New("turnloop httpstore: unexpected status")
	// ErrResponseTooLarge is returned when a response exceeds the configured byte limit.
	ErrResponseTooLarge = errors.New("turnloop httpstore: response too large")
	// ErrMaxResponseRequired is returned when max response bytes is not positive.
	ErrMaxResponseRequired = errors.New("turnloop httpstore: max response bytes must be positive")
)

// Backend persists TurnLoop queue state through a host-owned HTTP/JSON control plane.
type Backend struct {
	endpoint         url.URL
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

var _ gopact.TurnLoopRowBackend = (*Backend)(nil)
var _ gopact.TurnLoopVersionedBackend = (*Backend)(nil)

// Option configures an HTTP TurnLoop backend.
type Option func(*backendConfig) error

type backendConfig struct {
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

// NewBackend creates a TurnLoop row and CAS backend backed by endpoint.
//
// The endpoint must speak this minimal protocol:
//   - GET/PUT {endpoint}/turnloop/state?key=...
//   - GET/PUT {endpoint}/turnloop/versioned?key=...&expected_version=...
func NewBackend(endpoint string, opts ...Option) (*Backend, error) {
	parsed, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	cfg := backendConfig{
		client:           &http.Client{Timeout: defaultTimeout},
		headers:          make(http.Header),
		maxResponseBytes: defaultMaxResponseBytes,
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
	return &Backend{
		endpoint:         parsed,
		client:           cfg.client,
		headers:          cfg.headers.Clone(),
		maxResponseBytes: cfg.maxResponseBytes,
	}, nil
}

// WithHTTPClient replaces the default client.
func WithHTTPClient(client *http.Client) Option {
	return func(cfg *backendConfig) error {
		if client == nil {
			return ErrHTTPClientRequired
		}
		cfg.client = client
		return nil
	}
}

// WithHeader adds a header to every backend request.
func WithHeader(key, value string) Option {
	return func(cfg *backendConfig) error {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("turnloop httpstore: header key is required")
		}
		if cfg.headers == nil {
			cfg.headers = make(http.Header)
		}
		cfg.headers.Set(key, value)
		return nil
	}
}

// WithMaxResponseBytes limits response bodies decoded by the backend.
func WithMaxResponseBytes(n int64) Option {
	return func(cfg *backendConfig) error {
		if n <= 0 {
			return ErrMaxResponseRequired
		}
		cfg.maxResponseBytes = n
		return nil
	}
}

// UpsertTurnLoopState stores or replaces one TurnLoop state row.
func (b *Backend) UpsertTurnLoopState(ctx context.Context, record gopact.TurnLoopRowRecord) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Key == "" {
		return errors.New("turnloop httpstore: row key is required")
	}
	_, err := b.do(ctx, http.MethodPut, rowPath, keyQuery(record.Key), record, nil, http.StatusOK, http.StatusNoContent)
	if err != nil {
		return err
	}
	return nil
}

// GetTurnLoopState returns one TurnLoop state row by key.
func (b *Backend) GetTurnLoopState(ctx context.Context, key string) (gopact.TurnLoopRowRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopRowRecord{}, false, err
	}
	var record gopact.TurnLoopRowRecord
	status, err := b.do(ctx, http.MethodGet, rowPath, keyQuery(key), nil, &record, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return gopact.TurnLoopRowRecord{}, false, err
	}
	if status == http.StatusNotFound {
		return gopact.TurnLoopRowRecord{}, false, nil
	}
	return record, true, nil
}

// GetTurnLoopVersionedState returns one versioned TurnLoop state row by key.
func (b *Backend) GetTurnLoopVersionedState(ctx context.Context, key string) (gopact.TurnLoopVersionedRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	var record gopact.TurnLoopVersionedRecord
	status, err := b.do(ctx, http.MethodGet, versionedPath, keyQuery(key), nil, &record, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	if status == http.StatusNotFound {
		return gopact.TurnLoopVersionedRecord{}, false, nil
	}
	return record, true, nil
}

// CompareAndSwapTurnLoopState writes record when expectedVersion matches.
func (b *Backend) CompareAndSwapTurnLoopState(ctx context.Context, record gopact.TurnLoopVersionedRecord, expectedVersion string) (string, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Key == "" {
		return "", errors.New("turnloop httpstore: versioned row key is required")
	}
	query := keyQuery(record.Key)
	query.Set("expected_version", expectedVersion)
	var response versionResponse
	status, err := b.do(ctx, http.MethodPut, versionedPath, query, record, &response, http.StatusOK, http.StatusConflict)
	if err != nil {
		return "", err
	}
	if status == http.StatusConflict {
		return "", gopact.ErrTurnLoopStoreConflict
	}
	return response.Version, nil
}

type versionResponse struct {
	Version string `json:"version"`
}

func parseEndpoint(endpoint string) (url.URL, error) {
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
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return *parsed, nil
}

func keyQuery(key string) url.Values {
	query := make(url.Values)
	query.Set("key", key)
	return query
}

func (b *Backend) do(ctx context.Context, method, resourcePath string, query url.Values, body any, dest any, okStatuses ...int) (int, error) {
	req, err := b.newRequest(ctx, method, resourcePath, query, body)
	if err != nil {
		return 0, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("turnloop httpstore: %s %s: %w", method, resourcePath, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	raw, err := b.readResponse(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if !statusOK(resp.StatusCode, okStatuses) {
		return resp.StatusCode, unexpectedStatusError(method, resourcePath, resp.StatusCode, raw)
	}
	if dest == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || len(bytes.TrimSpace(raw)) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return resp.StatusCode, fmt.Errorf("turnloop httpstore: decode response: %w", err)
	}
	return resp.StatusCode, nil
}

func (b *Backend) newRequest(ctx context.Context, method, resourcePath string, query url.Values, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("turnloop httpstore: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.url(resourcePath, query), reader)
	if err != nil {
		return nil, fmt.Errorf("turnloop httpstore: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range b.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	return req, nil
}

func (b *Backend) url(resourcePath string, query url.Values) string {
	u := b.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + resourcePath
	u.RawQuery = query.Encode()
	return u.String()
}

func (b *Backend) readResponse(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, b.maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("turnloop httpstore: read response: %w", err)
	}
	if int64(len(raw)) > b.maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	return raw, nil
}

func statusOK(status int, okStatuses []int) bool {
	for _, okStatus := range okStatuses {
		if status == okStatus {
			return true
		}
	}
	return false
}

func unexpectedStatusError(method, resourcePath string, status int, raw []byte) error {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return fmt.Errorf("%w: %s %s returned %d", ErrUnexpectedStatus, method, resourcePath, status)
	}
	return fmt.Errorf("%w: %s %s returned %d: %s", ErrUnexpectedStatus, method, resourcePath, status, body)
}
