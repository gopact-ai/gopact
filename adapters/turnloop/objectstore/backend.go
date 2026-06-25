// Package objectstore adapts conditional object clients to TurnLoop CAS backends.
package objectstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	ErrClientRequired                    = errors.New("turnloop objectstore: client is required")
	ErrNotFound                          = errors.New("turnloop objectstore: not found")
	ErrPreconditionFailed                = errors.New("turnloop objectstore: precondition failed")
	ErrNotFoundMatcherRequired           = errors.New("turnloop objectstore: not found matcher is required")
	ErrPreconditionFailedMatcherRequired = errors.New("turnloop objectstore: precondition failed matcher is required")
	ErrUnsafePrefix                      = errors.New("turnloop objectstore: unsafe prefix")
	ErrInvalidObject                     = errors.New("turnloop objectstore: invalid object")
)

// Client is the minimal conditional object storage contract consumed by Backend.
type Client interface {
	GetObject(ctx context.Context, key string) (Object, error)
	PutObject(ctx context.Context, object Object, precondition Precondition) (Object, error)
}

// Object is one provider object payload plus its native CAS version.
type Object struct {
	Key       string
	Data      []byte
	Version   string
	UpdatedAt time.Time
	Metadata  map[string]string
}

// Precondition describes the CAS condition attached to a write.
type Precondition struct {
	IfAbsent  bool
	IfVersion string
}

// Backend persists TurnLoop queue state through conditional object writes.
type Backend struct {
	client               Client
	prefix               string
	isNotFound           func(error) bool
	isPreconditionFailed func(error) bool
}

var _ gopact.TurnLoopVersionedBackend = (*Backend)(nil)

// Option configures an object TurnLoop backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix               string
	isNotFound           func(error) bool
	isPreconditionFailed func(error) bool
}

// NewBackend creates a TurnLoop CAS backend backed by a conditional object client.
func NewBackend(client Client, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	cfg := backendConfig{
		isNotFound: func(err error) bool {
			return errors.Is(err, ErrNotFound)
		},
		isPreconditionFailed: func(err error) bool {
			return errors.Is(err, ErrPreconditionFailed)
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
	if cfg.isPreconditionFailed == nil {
		return nil, ErrPreconditionFailedMatcherRequired
	}
	return &Backend{
		client:               client,
		prefix:               cfg.prefix,
		isNotFound:           cfg.isNotFound,
		isPreconditionFailed: cfg.isPreconditionFailed,
	}, nil
}

// WithPrefix scopes all physical object keys under prefix.
func WithPrefix(prefix string) Option {
	return func(cfg *backendConfig) error {
		normalized, err := normalizePrefix(prefix)
		if err != nil {
			return err
		}
		cfg.prefix = normalized
		return nil
	}
}

// WithNotFound lets provider adapters map native object not-found errors.
func WithNotFound(match func(error) bool) Option {
	return func(cfg *backendConfig) error {
		if match == nil {
			return ErrNotFoundMatcherRequired
		}
		cfg.isNotFound = func(err error) bool {
			return errors.Is(err, ErrNotFound) || match(err)
		}
		return nil
	}
}

// WithPreconditionFailed lets provider adapters map native conditional-write failures.
func WithPreconditionFailed(match func(error) bool) Option {
	return func(cfg *backendConfig) error {
		if match == nil {
			return ErrPreconditionFailedMatcherRequired
		}
		cfg.isPreconditionFailed = func(err error) bool {
			return errors.Is(err, ErrPreconditionFailed) || match(err)
		}
		return nil
	}
}

// GetTurnLoopVersionedState returns one versioned TurnLoop state by key.
func (b *Backend) GetTurnLoopVersionedState(ctx context.Context, key string) (gopact.TurnLoopVersionedRecord, bool, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	object, err := b.client.GetObject(ctx, b.objectKey(key))
	if b.isNotFound(err) {
		return gopact.TurnLoopVersionedRecord{}, false, nil
	}
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, fmt.Errorf("turnloop objectstore: get state: %w", err)
	}
	if object.Version == "" {
		return gopact.TurnLoopVersionedRecord{}, false, ErrInvalidObject
	}
	record, err := decodeRecord(object.Data)
	if err != nil {
		return gopact.TurnLoopVersionedRecord{}, false, err
	}
	record.Version = object.Version
	return record, true, nil
}

// CompareAndSwapTurnLoopState writes record when expectedVersion matches.
func (b *Backend) CompareAndSwapTurnLoopState(ctx context.Context, record gopact.TurnLoopVersionedRecord, expectedVersion string) (string, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if record.Key == "" {
		return "", errors.New("turnloop objectstore: versioned key is required")
	}
	record.Version = ""
	raw, err := encode(record)
	if err != nil {
		return "", err
	}
	precondition := Precondition{IfAbsent: true}
	if expectedVersion != "" {
		precondition = Precondition{IfVersion: expectedVersion}
	}
	object, err := b.client.PutObject(ctx, Object{
		Key:  b.objectKey(record.Key),
		Data: raw,
	}, precondition)
	if b.isPreconditionFailed(err) {
		return "", gopact.ErrTurnLoopStoreConflict
	}
	if err != nil {
		return "", fmt.Errorf("turnloop objectstore: compare and swap state: %w", err)
	}
	if object.Version == "" {
		return "", ErrInvalidObject
	}
	return object.Version, nil
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", nil
	}
	for _, part := range strings.Split(prefix, "/") {
		if part == "" || part == "." || part == ".." || strings.Contains(part, `\`) {
			return "", fmt.Errorf("%w: %q", ErrUnsafePrefix, prefix)
		}
	}
	return prefix, nil
}

func (b *Backend) objectKey(key string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(key))
	return joinKey(b.prefix, "turnloop", "versioned", encoded+".json")
}

func joinKey(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

func encode(record gopact.TurnLoopVersionedRecord) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("turnloop objectstore: encode state: %w", err)
	}
	return raw, nil
}

func decodeRecord(raw []byte) (gopact.TurnLoopVersionedRecord, error) {
	var record gopact.TurnLoopVersionedRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return gopact.TurnLoopVersionedRecord{}, fmt.Errorf("turnloop objectstore: decode state: %w", err)
	}
	return record, nil
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}
