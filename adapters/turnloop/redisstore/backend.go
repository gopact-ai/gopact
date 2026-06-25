// Package redisstore adapts Redis GET/SET/EVAL clients to TurnLoop row and CAS backends.
package redisstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrClientRequired is returned when a backend is created without a Redis client.
	ErrClientRequired = errors.New("turnloop redisstore: client is required")
	// ErrNil identifies a Redis nil reply in adapters that expose one as an error.
	ErrNil = errors.New("turnloop redisstore: nil")
	// ErrNotFoundMatcherRequired is returned when a nil-reply matcher is missing.
	ErrNotFoundMatcherRequired = errors.New("turnloop redisstore: not found matcher is required")
	// ErrVersionGeneratorRequired is returned when a CAS backend lacks a version generator.
	ErrVersionGeneratorRequired = errors.New("turnloop redisstore: version generator is required")
	// ErrInvalidCASResult is returned when Redis returns an unexpected CAS script result.
	ErrInvalidCASResult = errors.New("turnloop redisstore: invalid cas result")
)

const redisConflictResult = "__gopact_turnloop_conflict__"

const compareAndSwapScript = `
local current = redis.call("GET", KEYS[1])
if not current then
	if ARGV[1] ~= "" then
		return "` + redisConflictResult + `"
	end
else
	local ok, decoded = pcall(cjson.decode, current)
	if not ok then
		error("turnloop redisstore: decode current state")
	end
	if decoded["version"] ~= ARGV[1] then
		return "` + redisConflictResult + `"
	end
end
redis.call("SET", KEYS[1], ARGV[2])
return ARGV[3]
`

// Client is the minimal Redis command contract consumed by Backend.
type Client interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Eval(ctx context.Context, script string, keys []string, args ...string) (string, error)
}

// Backend persists TurnLoop queue state through Redis string keys.
type Backend struct {
	client          Client
	prefix          string
	isNotFound      func(error) bool
	generateVersion func() (string, error)
}

var _ gopact.TurnLoopRowBackend = (*Backend)(nil)
var _ gopact.TurnLoopVersionedBackend = (*Backend)(nil)

// Option configures a Redis TurnLoop backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix          string
	isNotFound      func(error) bool
	generateVersion func() (string, error)
}

// NewBackend creates a TurnLoop row and CAS backend backed by a Redis client.
func NewBackend(client Client, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	cfg := backendConfig{
		isNotFound: func(err error) bool {
			return errors.Is(err, ErrNil)
		},
		generateVersion: newVersionToken,
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
	if cfg.generateVersion == nil {
		return nil, ErrVersionGeneratorRequired
	}
	return &Backend{
		client:          client,
		prefix:          cfg.prefix,
		isNotFound:      cfg.isNotFound,
		generateVersion: cfg.generateVersion,
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

// WithVersionGenerator overrides CAS version token generation.
func WithVersionGenerator(generate func() (string, error)) Option {
	return func(cfg *backendConfig) error {
		if generate == nil {
			return ErrVersionGeneratorRequired
		}
		cfg.generateVersion = generate
		return nil
	}
}

// UpsertTurnLoopState stores or replaces one TurnLoop state row.
func (b *Backend) UpsertTurnLoopState(ctx context.Context, record gopact.TurnLoopRowRecord) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Key == "" {
		return errors.New("turnloop redisstore: row key is required")
	}
	raw, err := encode(record)
	if err != nil {
		return err
	}
	if err := b.client.Set(ctx, b.physicalKey(record.Key), raw); err != nil {
		return fmt.Errorf("turnloop redisstore: set state: %w", err)
	}
	return nil
}

// GetTurnLoopState returns one TurnLoop state row by key.
func (b *Backend) GetTurnLoopState(ctx context.Context, key string) (gopact.TurnLoopRowRecord, bool, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopRowRecord{}, false, err
	}
	raw, err := b.client.Get(ctx, b.physicalKey(key))
	if b.isNotFound(err) {
		return gopact.TurnLoopRowRecord{}, false, nil
	}
	if err != nil {
		return gopact.TurnLoopRowRecord{}, false, fmt.Errorf("turnloop redisstore: get state: %w", err)
	}
	var record gopact.TurnLoopRowRecord
	if err := decode(raw, &record); err != nil {
		return gopact.TurnLoopRowRecord{}, false, err
	}
	return record, true, nil
}

// GetTurnLoopVersionedState returns one versioned TurnLoop state row by key.
func (b *Backend) GetTurnLoopVersionedState(ctx context.Context, key string) (gopact.TurnLoopVersionedRecord, bool, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	raw, err := b.client.Get(ctx, b.physicalKey(key))
	if b.isNotFound(err) {
		return gopact.TurnLoopVersionedRecord{}, false, nil
	}
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, fmt.Errorf("turnloop redisstore: get versioned state: %w", err)
	}
	var record gopact.TurnLoopVersionedRecord
	if err := decode(raw, &record); err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	return record, true, nil
}

// CompareAndSwapTurnLoopState writes record when expectedVersion matches.
func (b *Backend) CompareAndSwapTurnLoopState(ctx context.Context, record gopact.TurnLoopVersionedRecord, expectedVersion string) (string, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Key == "" {
		return "", errors.New("turnloop redisstore: versioned row key is required")
	}
	version, err := b.generateVersion()
	if err != nil {
		return "", fmt.Errorf("turnloop redisstore: generate version token: %w", err)
	}
	if version == "" {
		return "", ErrInvalidCASResult
	}
	record.Version = version
	raw, err := encode(record)
	if err != nil {
		return "", err
	}
	result, err := b.client.Eval(
		ctx,
		compareAndSwapScript,
		[]string{b.physicalKey(record.Key)},
		expectedVersion,
		string(raw),
		version,
	)
	if err != nil {
		return "", fmt.Errorf("turnloop redisstore: compare and swap state: %w", err)
	}
	if result == redisConflictResult {
		return "", gopact.ErrTurnLoopStoreConflict
	}
	if result == "" {
		return "", ErrInvalidCASResult
	}
	return result, nil
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

func encode(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("turnloop redisstore: encode state: %w", err)
	}
	return raw, nil
}

func decode(raw []byte, dest any) error {
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("turnloop redisstore: decode state: %w", err)
	}
	return nil
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}

func newVersionToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("turnloop redisstore: generate random version: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
