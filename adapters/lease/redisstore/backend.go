// Package redisstore adapts Redis GET/EVAL clients to gopact.LeaseBackend.
package redisstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrClientRequired is returned when a backend is created without a Redis client.
	ErrClientRequired = errors.New("lease redisstore: client is required")
	// ErrNil identifies a Redis nil reply in adapters that expose one as an error.
	ErrNil = errors.New("lease redisstore: nil")
	// ErrNotFoundMatcherRequired is returned when a nil-reply matcher is missing.
	ErrNotFoundMatcherRequired = errors.New("lease redisstore: not found matcher is required")
	// ErrTokenGeneratorRequired is returned when the backend lacks a lease token generator.
	ErrTokenGeneratorRequired = errors.New("lease redisstore: token generator is required")
	// ErrInvalidLeaseResult is returned when Redis returns an unexpected lease script result.
	ErrInvalidLeaseResult = errors.New("lease redisstore: invalid lease result")
)

const (
	redisConflictResult      = "__gopact_lease_conflict__"
	redisLeaseNotHeldResult  = "__gopact_lease_not_held__"
	redisLeaseReleasedResult = "__gopact_lease_released__"
)

const acquireScript = `
local current = redis.call("GET", KEYS[1])
if current then
	local ok, decoded = pcall(cjson.decode, current)
	if not ok then
		error("lease redisstore: decode current lease")
	end
	local expires = decoded["expires_at_unix_nano"]
	if not expires then
		error("lease redisstore: missing expires_at_unix_nano")
	end
	if expires > ARGV[1] then
		return "` + redisConflictResult + `"
	end
end
redis.call("SET", KEYS[1], ARGV[2])
return ARGV[2]
`

const renewScript = `
local current = redis.call("GET", KEYS[1])
if not current then
	return "` + redisLeaseNotHeldResult + `"
end
local ok, decoded = pcall(cjson.decode, current)
if not ok then
	error("lease redisstore: decode current lease")
end
local expires = decoded["expires_at_unix_nano"]
if not expires then
	error("lease redisstore: missing expires_at_unix_nano")
end
if expires <= ARGV[4] or decoded["owner"] ~= ARGV[1] or decoded["token"] ~= ARGV[2] then
	return "` + redisLeaseNotHeldResult + `"
end
decoded["token"] = ARGV[3]
decoded["expires_at"] = ARGV[5]
decoded["expires_at_unix_nano"] = ARGV[6]
local updated = cjson.encode(decoded)
redis.call("SET", KEYS[1], updated)
return updated
`

const releaseScript = `
local current = redis.call("GET", KEYS[1])
if not current then
	return "` + redisLeaseNotHeldResult + `"
end
local ok, decoded = pcall(cjson.decode, current)
if not ok then
	error("lease redisstore: decode current lease")
end
local expires = decoded["expires_at_unix_nano"]
if not expires then
	error("lease redisstore: missing expires_at_unix_nano")
end
if expires <= ARGV[3] or decoded["owner"] ~= ARGV[1] or decoded["token"] ~= ARGV[2] then
	return "` + redisLeaseNotHeldResult + `"
end
redis.call("DEL", KEYS[1])
return "` + redisLeaseReleasedResult + `"
`

// Client is the minimal Redis command contract consumed by Backend.
type Client interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Eval(ctx context.Context, script string, keys []string, args ...string) (string, error)
}

// Backend persists worker ownership leases through Redis string keys.
type Backend struct {
	client        Client
	prefix        string
	isNotFound    func(error) bool
	generateToken func() (string, error)
	now           func() time.Time
}

var _ gopact.LeaseBackend = (*Backend)(nil)

// Option configures a Redis lease backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix        string
	isNotFound    func(error) bool
	generateToken func() (string, error)
	now           func() time.Time
}

// NewBackend creates a lease backend backed by a Redis client.
func NewBackend(client Client, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	cfg := backendConfig{
		isNotFound: func(err error) bool {
			return errors.Is(err, ErrNil)
		},
		generateToken: newLeaseToken,
		now:           time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.isNotFound == nil {
		return nil, ErrNotFoundMatcherRequired
	}
	if cfg.generateToken == nil {
		return nil, ErrTokenGeneratorRequired
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Backend{
		client:        client,
		prefix:        cfg.prefix,
		isNotFound:    cfg.isNotFound,
		generateToken: cfg.generateToken,
		now:           cfg.now,
	}, nil
}

// WithPrefix scopes all physical Redis keys under prefix.
func WithPrefix(prefix string) Option {
	return func(cfg *backendConfig) error {
		cfg.prefix = normalizePrefix(prefix)
		return nil
	}
}

// WithNotFound lets provider adapters map native Redis nil errors.
func WithNotFound(match func(error) bool) Option {
	return func(cfg *backendConfig) error {
		if match == nil {
			return ErrNotFoundMatcherRequired
		}
		cfg.isNotFound = func(err error) bool {
			return errors.Is(err, ErrNil) || match(err)
		}
		return nil
	}
}

// WithTokenGenerator overrides lease token generation.
func WithTokenGenerator(generate func() (string, error)) Option {
	return func(cfg *backendConfig) error {
		if generate == nil {
			return ErrTokenGeneratorRequired
		}
		cfg.generateToken = generate
		return nil
	}
}

// WithClock overrides the clock used for lease expiry checks.
func WithClock(now func() time.Time) Option {
	return func(cfg *backendConfig) error {
		if now != nil {
			cfg.now = now
		}
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
	now := b.currentTime()
	record, err := b.newLeaseRecord(request, now)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	raw, err := encodeLeaseDocument(record)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	result, err := b.client.Eval(
		ctx,
		acquireScript,
		[]string{b.physicalKey(request.Key)},
		formatRedisTime(now),
		string(raw),
	)
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease redisstore: acquire lease: %w", err)
	}
	if result == redisConflictResult {
		return gopact.LeaseRecord{}, gopact.ErrLeaseConflict
	}
	if result == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseResult
	}
	return decodeLeaseDocument([]byte(result))
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
	token, err := b.generateToken()
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease redisstore: generate lease token: %w", err)
	}
	if token == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseResult
	}
	now := b.currentTime()
	expiresAt := now.Add(request.TTL)
	result, err := b.client.Eval(
		ctx,
		renewScript,
		[]string{b.physicalKey(request.Key)},
		request.Owner,
		request.Token,
		token,
		formatRedisTime(now),
		formatJSONTime(expiresAt),
		formatRedisTime(expiresAt),
	)
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease redisstore: renew lease: %w", err)
	}
	if result == redisLeaseNotHeldResult {
		return gopact.LeaseRecord{}, gopact.ErrLeaseNotHeld
	}
	if result == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseResult
	}
	return decodeLeaseDocument([]byte(result))
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
	result, err := b.client.Eval(
		ctx,
		releaseScript,
		[]string{b.physicalKey(request.Key)},
		request.Owner,
		request.Token,
		formatRedisTime(b.currentTime()),
	)
	if err != nil {
		return fmt.Errorf("lease redisstore: release lease: %w", err)
	}
	switch result {
	case redisLeaseReleasedResult:
		return nil
	case redisLeaseNotHeldResult:
		return gopact.ErrLeaseNotHeld
	default:
		return fmt.Errorf("%w: %q", ErrInvalidLeaseResult, result)
	}
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
	raw, err := b.client.Get(ctx, b.physicalKey(key))
	if b.isNotFound(err) {
		return gopact.LeaseRecord{}, false, nil
	}
	if err != nil {
		return gopact.LeaseRecord{}, false, fmt.Errorf("lease redisstore: get lease: %w", err)
	}
	record, err := decodeLeaseDocument(raw)
	if err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	if !b.currentTime().Before(record.ExpiresAt) {
		return gopact.LeaseRecord{}, false, nil
	}
	return record, true, nil
}

type leaseDocument struct {
	gopact.LeaseRecord
	ExpiresAtUnixNano string `json:"expires_at_unix_nano"`
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

func (b *Backend) newLeaseRecord(request gopact.LeaseRequest, now time.Time) (gopact.LeaseRecord, error) {
	token, err := b.generateToken()
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease redisstore: generate lease token: %w", err)
	}
	if token == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseResult
	}
	return gopact.LeaseRecord{
		Key:        request.Key,
		Owner:      request.Owner,
		Token:      token,
		AcquiredAt: now,
		ExpiresAt:  now.Add(request.TTL),
		Metadata:   copyAnyMap(request.Metadata),
	}, nil
}

func (b *Backend) currentTime() time.Time {
	if b.now == nil {
		return time.Now().UTC()
	}
	return b.now().UTC()
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || strings.HasSuffix(prefix, ":") {
		return prefix
	}
	return prefix + ":"
}

func (b *Backend) physicalKey(key string) string {
	return b.prefix + key
}

func encodeLeaseDocument(record gopact.LeaseRecord) ([]byte, error) {
	document := leaseDocument{
		LeaseRecord:       copyLeaseRecord(record),
		ExpiresAtUnixNano: formatRedisTime(record.ExpiresAt),
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("lease redisstore: encode lease: %w", err)
	}
	return raw, nil
}

func decodeLeaseDocument(raw []byte) (gopact.LeaseRecord, error) {
	var document leaseDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease redisstore: decode lease: %w", err)
	}
	return copyLeaseRecord(document.LeaseRecord), nil
}

func formatRedisTime(value time.Time) string {
	return fmt.Sprintf("%020d", value.UTC().UnixNano())
}

func formatJSONTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func copyLeaseRecord(record gopact.LeaseRecord) gopact.LeaseRecord {
	record.Metadata = copyAnyMap(record.Metadata)
	return record
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}

func newLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("lease redisstore: generate random token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
