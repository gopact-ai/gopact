// Package objectstore adapts conditional object clients to gopact.LeaseBackend.
package objectstore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrClientRequired is returned when a backend is created without an object client.
	ErrClientRequired = errors.New("lease objectstore: client is required")
	// ErrNotFound identifies a missing lease object.
	ErrNotFound = errors.New("lease objectstore: not found")
	// ErrPreconditionFailed identifies a failed conditional object write.
	ErrPreconditionFailed = errors.New("lease objectstore: precondition failed")
	// ErrNotFoundMatcherRequired is returned when a not-found matcher is missing.
	ErrNotFoundMatcherRequired = errors.New("lease objectstore: not found matcher is required")
	// ErrPreconditionFailedMatcherRequired is returned when a precondition matcher is missing.
	ErrPreconditionFailedMatcherRequired = errors.New("lease objectstore: precondition failed matcher is required")
	// ErrTokenGeneratorRequired is returned when the backend lacks a lease token generator.
	ErrTokenGeneratorRequired = errors.New("lease objectstore: token generator is required")
	// ErrUnsafePrefix is returned when an object key prefix can escape its namespace.
	ErrUnsafePrefix = errors.New("lease objectstore: unsafe prefix")
	// ErrInvalidLeaseObject is returned when a stored object cannot represent a lease.
	ErrInvalidLeaseObject = errors.New("lease objectstore: invalid lease object")
)

// Client is the minimal conditional object storage contract consumed by Backend.
type Client interface {
	GetObject(ctx context.Context, key string) (Object, error)
	PutObject(ctx context.Context, object Object, precondition Precondition) (Object, error)
	DeleteObject(ctx context.Context, key string, precondition Precondition) error
}

// Object is one provider object payload plus its native CAS version.
type Object struct {
	Key       string
	Data      []byte
	Version   string
	UpdatedAt time.Time
	Metadata  map[string]string
}

// Precondition describes the CAS condition attached to a write/delete.
type Precondition struct {
	IfAbsent  bool
	IfVersion string
}

// Backend persists worker ownership leases through conditional object writes.
type Backend struct {
	client               Client
	prefix               string
	isNotFound           func(error) bool
	isPreconditionFailed func(error) bool
	generateToken        func() (string, error)
	now                  func() time.Time
}

var _ gopact.LeaseBackend = (*Backend)(nil)

// Option configures an object lease backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix               string
	isNotFound           func(error) bool
	isPreconditionFailed func(error) bool
	generateToken        func() (string, error)
	now                  func() time.Time
}

// NewBackend creates a lease backend backed by a conditional object client.
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
	if cfg.isPreconditionFailed == nil {
		return nil, ErrPreconditionFailedMatcherRequired
	}
	if cfg.generateToken == nil {
		return nil, ErrTokenGeneratorRequired
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Backend{
		client:               client,
		prefix:               cfg.prefix,
		isNotFound:           cfg.isNotFound,
		isPreconditionFailed: cfg.isPreconditionFailed,
		generateToken:        cfg.generateToken,
		now:                  cfg.now,
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
	key := b.objectKey(request.Key)
	currentObject, err := b.client.GetObject(ctx, key)
	if b.isNotFound(err) {
		return b.putLease(ctx, key, record, Precondition{IfAbsent: true}, gopact.ErrLeaseConflict)
	}
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease objectstore: get lease: %w", err)
	}
	current, err := decodeLease(currentObject.Data)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	if now.Before(current.ExpiresAt) {
		return gopact.LeaseRecord{}, gopact.ErrLeaseConflict
	}
	if currentObject.Version == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseObject
	}
	return b.putLease(ctx, key, record, Precondition{IfVersion: currentObject.Version}, gopact.ErrLeaseConflict)
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
	now := b.currentTime()
	key := b.objectKey(request.Key)
	currentObject, current, ok, err := b.currentLease(ctx, key)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	if !ok || !now.Before(current.ExpiresAt) || current.Owner != request.Owner || current.Token != request.Token {
		return gopact.LeaseRecord{}, gopact.ErrLeaseNotHeld
	}
	if currentObject.Version == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseObject
	}
	token, err := b.generateToken()
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease objectstore: generate lease token: %w", err)
	}
	if token == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseObject
	}
	current.Token = token
	current.ExpiresAt = now.Add(request.TTL)
	return b.putLease(ctx, key, current, Precondition{IfVersion: currentObject.Version}, gopact.ErrLeaseNotHeld)
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
	now := b.currentTime()
	key := b.objectKey(request.Key)
	currentObject, current, ok, err := b.currentLease(ctx, key)
	if err != nil {
		return err
	}
	if !ok || !now.Before(current.ExpiresAt) || current.Owner != request.Owner || current.Token != request.Token {
		return gopact.ErrLeaseNotHeld
	}
	if currentObject.Version == "" {
		return ErrInvalidLeaseObject
	}
	err = b.client.DeleteObject(ctx, key, Precondition{IfVersion: currentObject.Version})
	if b.isNotFound(err) || b.isPreconditionFailed(err) {
		return gopact.ErrLeaseNotHeld
	}
	if err != nil {
		return fmt.Errorf("lease objectstore: delete lease: %w", err)
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
	_, current, ok, err := b.currentLease(ctx, b.objectKey(key))
	if err != nil {
		return gopact.LeaseRecord{}, false, err
	}
	if !ok || !b.currentTime().Before(current.ExpiresAt) {
		return gopact.LeaseRecord{}, false, nil
	}
	return copyLeaseRecord(current), true, nil
}

func (b *Backend) currentLease(ctx context.Context, key string) (Object, gopact.LeaseRecord, bool, error) {
	object, err := b.client.GetObject(ctx, key)
	if b.isNotFound(err) {
		return Object{}, gopact.LeaseRecord{}, false, nil
	}
	if err != nil {
		return Object{}, gopact.LeaseRecord{}, false, fmt.Errorf("lease objectstore: get lease: %w", err)
	}
	record, err := decodeLease(object.Data)
	if err != nil {
		return Object{}, gopact.LeaseRecord{}, false, err
	}
	return object, record, true, nil
}

func (b *Backend) putLease(ctx context.Context, key string, record gopact.LeaseRecord, precondition Precondition, conflict error) (gopact.LeaseRecord, error) {
	raw, err := encodeLease(record)
	if err != nil {
		return gopact.LeaseRecord{}, err
	}
	_, err = b.client.PutObject(ctx, Object{
		Key:  key,
		Data: raw,
	}, precondition)
	if b.isPreconditionFailed(err) {
		return gopact.LeaseRecord{}, conflict
	}
	if err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease objectstore: put lease: %w", err)
	}
	return copyLeaseRecord(record), nil
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
		return gopact.LeaseRecord{}, fmt.Errorf("lease objectstore: generate lease token: %w", err)
	}
	if token == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseObject
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

func (b *Backend) objectKey(leaseKey string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(leaseKey))
	return joinKey(b.prefix, "leases", encoded+".json")
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", nil
	}
	parts := strings.Split(prefix, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "\\") {
			return "", ErrUnsafePrefix
		}
	}
	return strings.Join(parts, "/"), nil
}

func joinKey(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "/")
}

func encodeLease(record gopact.LeaseRecord) ([]byte, error) {
	raw, err := json.Marshal(copyLeaseRecord(record))
	if err != nil {
		return nil, fmt.Errorf("lease objectstore: encode lease: %w", err)
	}
	return raw, nil
}

func decodeLease(raw []byte) (gopact.LeaseRecord, error) {
	var record gopact.LeaseRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return gopact.LeaseRecord{}, fmt.Errorf("lease objectstore: decode lease: %w", err)
	}
	if record.Key == "" || record.Owner == "" || record.Token == "" {
		return gopact.LeaseRecord{}, ErrInvalidLeaseObject
	}
	return copyLeaseRecord(record), nil
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

func copyMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
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
		return "", fmt.Errorf("lease objectstore: generate random token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
