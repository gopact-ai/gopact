// Package redisstore adapts Redis GET/EVAL clients to checkpoint.RowBackend.
package redisstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact/checkpoint"
)

var (
	ErrClientRequired          = errors.New("checkpoint redisstore: client is required")
	ErrNil                     = errors.New("checkpoint redisstore: nil")
	ErrNotFoundMatcherRequired = errors.New("checkpoint redisstore: not found matcher is required")
	ErrInvalidUpsertResult     = errors.New("checkpoint redisstore: invalid upsert result")
)

const upsertRecordScript = `
local raw_index = redis.call("GET", KEYS[2])
local index = {}
if raw_index then
	local ok, decoded = pcall(cjson.decode, raw_index)
	if not ok then
		error("checkpoint redisstore: decode thread index")
	end
	index = decoded
end
local found = false
for _, id in ipairs(index) do
	if id == ARGV[2] then
		found = true
		break
	end
end
if not found then
	table.insert(index, ARGV[2])
end
redis.call("SET", KEYS[1], ARGV[3])
redis.call("SET", KEYS[2], cjson.encode(index))
return ARGV[2]
`

// Client is the minimal Redis command contract consumed by Backend.
type Client interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Eval(ctx context.Context, script string, keys []string, args ...string) (string, error)
}

// Backend persists checkpoint records through Redis string keys.
type Backend struct {
	client     Client
	prefix     string
	isNotFound func(error) bool
}

var _ checkpoint.RowBackend = (*Backend)(nil)

// Option configures a Redis checkpoint backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix     string
	isNotFound func(error) bool
}

// NewBackend creates a checkpoint row backend backed by a Redis client.
func NewBackend(client Client, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	cfg := backendConfig{
		isNotFound: func(err error) bool {
			return errors.Is(err, ErrNil)
		},
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
	return &Backend{
		client:     client,
		prefix:     cfg.prefix,
		isNotFound: cfg.isNotFound,
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

// UpsertRecord stores or replaces one checkpoint record and its thread index atomically.
func (b *Backend) UpsertRecord(ctx context.Context, record checkpoint.Record) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.ID == "" {
		return errors.New("checkpoint redisstore: record id is required")
	}
	raw, err := encode(record)
	if err != nil {
		return err
	}
	result, err := b.client.Eval(
		ctx,
		upsertRecordScript,
		[]string{b.recordKey(record.ID), b.threadKey(record.ThreadID)},
		record.ThreadID,
		record.ID,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("checkpoint redisstore: upsert record: %w", err)
	}
	if result == "" {
		return ErrInvalidUpsertResult
	}
	return nil
}

// GetRecord returns one checkpoint record by id.
func (b *Backend) GetRecord(ctx context.Context, id string) (checkpoint.Record, bool, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return checkpoint.Record{}, false, err
	}
	raw, err := b.client.Get(ctx, b.recordKey(id))
	if b.isNotFound(err) {
		return checkpoint.Record{}, false, nil
	}
	if err != nil {
		return checkpoint.Record{}, false, fmt.Errorf("checkpoint redisstore: get record: %w", err)
	}
	record, err := decodeRecord(raw)
	if err != nil {
		return checkpoint.Record{}, false, err
	}
	return record, true, nil
}

// ListRecords returns checkpoint records for one thread.
func (b *Backend) ListRecords(ctx context.Context, threadID string) ([]checkpoint.Record, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rawIndex, err := b.client.Get(ctx, b.threadKey(threadID))
	if b.isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint redisstore: get thread index: %w", err)
	}
	var ids []string
	if err := decode(rawIndex, &ids); err != nil {
		return nil, fmt.Errorf("checkpoint redisstore: decode thread index: %w", err)
	}
	records := make([]checkpoint.Record, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		raw, err := b.client.Get(ctx, b.recordKey(id))
		if b.isNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("checkpoint redisstore: get indexed record %q: %w", id, err)
		}
		record, err := decodeRecord(raw)
		if err != nil {
			return nil, err
		}
		if record.ThreadID != threadID {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || strings.HasSuffix(prefix, ":") {
		return prefix
	}
	return prefix + ":"
}

func (b *Backend) recordKey(id string) string {
	return b.prefix + "checkpoint:record:" + encodeKeyPart(id)
}

func (b *Backend) threadKey(threadID string) string {
	return b.prefix + "checkpoint:thread:" + encodeKeyPart(threadID)
}

func encode(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("checkpoint redisstore: encode record: %w", err)
	}
	return raw, nil
}

func decodeRecord(raw []byte) (checkpoint.Record, error) {
	var record checkpoint.Record
	if err := decode(raw, &record); err != nil {
		return checkpoint.Record{}, fmt.Errorf("checkpoint redisstore: decode record: %w", err)
	}
	return record, nil
}

func decode(raw []byte, dest any) error {
	if err := json.Unmarshal(raw, dest); err != nil {
		return err
	}
	return nil
}

func encodeKeyPart(part string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(part))
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}
