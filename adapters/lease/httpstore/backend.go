// Package httpstore adapts an internal HTTP/JSON control plane to gopact.LeaseBackend.
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

	leasePath        = "/leases"
	leaseAcquirePath = "/leases/acquire"
	leaseRenewPath   = "/leases/renew"
	leaseReleasePath = "/leases/release"
)

var (
	// ErrEndpointRequired is returned when a backend is created without an endpoint.
	ErrEndpointRequired = errors.New("lease httpstore: endpoint is required")
	// ErrInvalidEndpoint is returned when the configured endpoint URL is invalid.
	ErrInvalidEndpoint = errors.New("lease httpstore: invalid endpoint")
	// ErrHTTPClientRequired is returned when a nil HTTP client is supplied.
	ErrHTTPClientRequired = errors.New("lease httpstore: http client is required")
	// ErrUnexpectedStatus is returned when the control plane returns an unexpected status.
	ErrUnexpectedStatus = errors.New("lease httpstore: unexpected status")
	// ErrResponseTooLarge is returned when a response exceeds the configured byte limit.
	ErrResponseTooLarge = errors.New("lease httpstore: response too large")
	// ErrMaxResponseRequired is returned when max response bytes is not positive.
	ErrMaxResponseRequired = errors.New("lease httpstore: max response bytes must be positive")
	// ErrInvalidLeaseResult is returned when the control plane response cannot represent a lease result.
	ErrInvalidLeaseResult = errors.New("lease httpstore: invalid lease result")
)

// Backend persists worker ownership leases through a host-owned HTTP/JSON control plane.
type Backend struct {
	endpoint         url.URL
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

var _ gopact.LeaseBackend = (*Backend)(nil)

// Option configures an HTTP lease backend.
type Option func(*backendConfig) error

type backendConfig struct {
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

// NewBackend creates a lease backend backed by endpoint.
//
// The endpoint must speak this minimal protocol:
//   - POST {endpoint}/leases/acquire?key=...
//   - POST {endpoint}/leases/renew?key=...
//   - POST {endpoint}/leases/release?key=...
//   - GET  {endpoint}/leases?key=...
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
			return errors.New("lease httpstore: header key is required")
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

// AcquireLease acquires key for owner unless a non-expired lease is held.
func (b *Backend) AcquireLease(ctx context.Context, request gopact.LeaseRequest) (gopact.LeaseRecord, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return gopact.LeaseRecord{}, err
	}
	if err := validateAcquire(request); err != nil {
		return gopact.LeaseRecord{}, err
	}
	var record gopact.LeaseRecord
	status, err := b.do(
		ctx,
		http.MethodPost,
		leaseAcquirePath,
		keyQuery(request.Key),
		request,
		&record,
		http.StatusOK,
		http.StatusCreated,
		http.StatusConflict,
	)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	if status == http.StatusConflict {
		return gopact.LeaseRecord{}, gopact.ErrLeaseConflict
	}
	if err := validateLeaseRecord(record, request.Key, request.Owner); err != nil {
		return gopact.LeaseRecord{}, err
	}
	return record, nil
}

// RenewLease extends a lease only if owner and token match the current holder.
func (b *Backend) RenewLease(ctx context.Context, request gopact.LeaseRenewRequest) (gopact.LeaseRecord, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return gopact.LeaseRecord{}, err
	}
	if err := validateRenew(request); err != nil {
		return gopact.LeaseRecord{}, err
	}
	var record gopact.LeaseRecord
	status, err := b.do(
		ctx,
		http.MethodPost,
		leaseRenewPath,
		keyQuery(request.Key),
		request,
		&record,
		http.StatusOK,
		http.StatusNotFound,
		http.StatusConflict,
	)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	if status == http.StatusNotFound || status == http.StatusConflict {
		return gopact.LeaseRecord{}, gopact.ErrLeaseNotHeld
	}
	if err := validateLeaseRecord(record, request.Key, request.Owner); err != nil {
		return gopact.LeaseRecord{}, err
	}
	return record, nil
}

// ReleaseLease releases a lease only if owner and token match the current holder.
func (b *Backend) ReleaseLease(ctx context.Context, request gopact.LeaseReleaseRequest) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRelease(request); err != nil {
		return err
	}
	status, err := b.do(
		ctx,
		http.MethodPost,
		leaseReleasePath,
		keyQuery(request.Key),
		request,
		nil,
		http.StatusOK,
		http.StatusNoContent,
		http.StatusNotFound,
		http.StatusConflict,
	)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status == http.StatusConflict {
		return gopact.ErrLeaseNotHeld
	}
	return nil
}

// GetLease returns the current non-expired lease for key.
func (b *Backend) GetLease(ctx context.Context, key string) (gopact.LeaseRecord, bool, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	if key == "" {
		return gopact.LeaseRecord{}, false, gopact.ErrLeaseKeyRequired
	}
	var record gopact.LeaseRecord
	status, err := b.do(ctx, http.MethodGet, leasePath, keyQuery(key), nil, &record, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	if status == http.StatusNotFound {
		return gopact.LeaseRecord{}, false, nil
	}
	if err := validateLeaseRecord(record, key, ""); err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	return record, true, nil
}

func validateAcquire(request gopact.LeaseRequest) error {
	if request.Key == "" {
		return gopact.ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return gopact.ErrLeaseOwnerRequired
	}
	if request.TTL <= 0 {
		return gopact.ErrLeaseTTLRequired
	}
	return nil
}

func validateRenew(request gopact.LeaseRenewRequest) error {
	if request.Key == "" {
		return gopact.ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return gopact.ErrLeaseOwnerRequired
	}
	if request.Token == "" {
		return gopact.ErrLeaseTokenRequired
	}
	if request.TTL <= 0 {
		return gopact.ErrLeaseTTLRequired
	}
	return nil
}

func validateRelease(request gopact.LeaseReleaseRequest) error {
	if request.Key == "" {
		return gopact.ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return gopact.ErrLeaseOwnerRequired
	}
	if request.Token == "" {
		return gopact.ErrLeaseTokenRequired
	}
	return nil
}

func validateLeaseRecord(record gopact.LeaseRecord, key string, owner string) error {
	if record.Key != key {
		return fmt.Errorf("%w: key mismatch", ErrInvalidLeaseResult)
	}
	if owner != "" && record.Owner != owner {
		return fmt.Errorf("%w: owner mismatch", ErrInvalidLeaseResult)
	}
	if record.Owner == "" || record.Token == "" {
		return ErrInvalidLeaseResult
	}
	return nil
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
		return 0, fmt.Errorf("lease httpstore: %s %s: %w", method, resourcePath, err)
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
		return resp.StatusCode, fmt.Errorf("lease httpstore: decode response: %w", err)
	}
	return resp.StatusCode, nil
}

func (b *Backend) newRequest(ctx context.Context, method, resourcePath string, query url.Values, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("lease httpstore: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.url(resourcePath, query), reader)
	if err != nil {
		return nil, fmt.Errorf("lease httpstore: create request: %w", err)
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
		return nil, fmt.Errorf("lease httpstore: read response: %w", err)
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

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}
