// Package objectblob adapts provider object clients to gopact object/blob ports.
package objectblob

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
)

var (
	ErrClientRequired          = errors.New("objectblob: client is required")
	ErrUnsafeKey               = errors.New("objectblob: unsafe key")
	ErrInvalidPageSize         = errors.New("objectblob: invalid page size")
	ErrNotFound                = errors.New("objectblob: not found")
	ErrNotFoundMatcherRequired = errors.New("objectblob: not found matcher is required")
	ErrInvalidListResponse     = errors.New("objectblob: invalid list response")
)

const defaultPageSize = 1000

// Client is the minimal cloud-object client contract consumed by Backend.
type Client interface {
	PutObject(ctx context.Context, object Object) error
	GetObject(ctx context.Context, key string) (Object, error)
	ListObjects(ctx context.Context, request ListRequest) (ListPage, error)
}

// Object is one provider object payload.
type Object struct {
	Key       string
	Data      []byte
	UpdatedAt time.Time
	Metadata  map[string]string
}

// Info describes one provider object visible in a list page.
type Info struct {
	Key       string
	UpdatedAt time.Time
	Metadata  map[string]string
}

// ListRequest describes one paged provider list request.
type ListRequest struct {
	Prefix    string
	PageToken string
	PageSize  int
}

// ListPage describes one provider list response.
type ListPage struct {
	Objects       []Info
	NextPageToken string
}

// Backend maps a provider object client to checkpoint and TurnLoop blob ports.
type Backend struct {
	client     Client
	prefix     string
	pageSize   int
	isNotFound func(error) bool
}

var _ checkpoint.ObjectBackend = (*Backend)(nil)
var _ gopact.TurnLoopBlobBackend = (*Backend)(nil)

// Option configures Backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix     string
	pageSize   int
	isNotFound func(error) bool
}

// New creates an object client-backed object/blob backend.
func New(client Client, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	cfg := backendConfig{
		pageSize: defaultPageSize,
		isNotFound: func(err error) bool {
			return errors.Is(err, ErrNotFound)
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
	if cfg.pageSize <= 0 {
		return nil, ErrInvalidPageSize
	}
	return &Backend{
		client:     client,
		prefix:     cfg.prefix,
		pageSize:   cfg.pageSize,
		isNotFound: cfg.isNotFound,
	}, nil
}

// WithPrefix scopes all physical object keys under prefix.
func WithPrefix(prefix string) Option {
	return func(cfg *backendConfig) error {
		normalized, err := normalizeRootPrefix(prefix)
		if err != nil {
			return err
		}
		cfg.prefix = normalized
		return nil
	}
}

// WithPageSize sets the provider list page size requested by Backend.
func WithPageSize(size int) Option {
	return func(cfg *backendConfig) error {
		if size <= 0 {
			return ErrInvalidPageSize
		}
		cfg.pageSize = size
		return nil
	}
}

// WithNotFound lets provider adapters map native not-found errors.
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

// PutObject stores or replaces one checkpoint object payload.
func (b *Backend) PutObject(ctx context.Context, key string, data []byte) error {
	physicalKey, err := b.physicalKey(key)
	if err != nil {
		return err
	}
	return b.put(ctx, physicalKey, data)
}

// GetObject returns a copy of one checkpoint object payload.
func (b *Backend) GetObject(ctx context.Context, key string) ([]byte, error) {
	physicalKey, err := b.physicalKey(key)
	if err != nil {
		return nil, err
	}
	raw, err := b.get(ctx, physicalKey)
	if b.isNotFound(err) {
		return nil, checkpoint.ErrObjectNotFound
	}
	return raw, err
}

// ListObjects returns checkpoint object infos whose logical keys are under prefix.
func (b *Backend) ListObjects(ctx context.Context, prefix string) ([]checkpoint.ObjectInfo, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	physicalPrefix, err := b.physicalPrefix(prefix)
	if err != nil {
		return nil, err
	}

	var out []checkpoint.ObjectInfo
	pageToken := ""
	seenTokens := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pageToken != "" {
			if _, ok := seenTokens[pageToken]; ok {
				return nil, fmt.Errorf("%w: repeated page token %q", ErrInvalidListResponse, pageToken)
			}
			seenTokens[pageToken] = struct{}{}
		}
		page, err := b.client.ListObjects(ctx, ListRequest{
			Prefix:    physicalPrefix,
			PageToken: pageToken,
			PageSize:  b.pageSize,
		})
		if b.isNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("objectblob: list objects: %w", err)
		}
		for _, object := range page.Objects {
			logicalKey, ok := b.logicalKey(object.Key)
			if !ok {
				continue
			}
			out = append(out, checkpoint.ObjectInfo{
				Key:       logicalKey,
				UpdatedAt: object.UpdatedAt.UTC(),
				Metadata:  copyMetadata(object.Metadata),
			})
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// PutBlob stores or replaces one TurnLoop blob payload.
func (b *Backend) PutBlob(ctx context.Context, key string, data []byte) error {
	physicalKey, err := b.physicalKey(key)
	if err != nil {
		return err
	}
	return b.put(ctx, physicalKey, data)
}

// GetBlob returns a copy of one TurnLoop blob payload.
func (b *Backend) GetBlob(ctx context.Context, key string) ([]byte, error) {
	physicalKey, err := b.physicalKey(key)
	if err != nil {
		return nil, err
	}
	raw, err := b.get(ctx, physicalKey)
	if b.isNotFound(err) {
		return nil, gopact.ErrTurnLoopBlobNotFound
	}
	return raw, err
}

func (b *Backend) put(ctx context.Context, physicalKey string, data []byte) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.client.PutObject(ctx, Object{
		Key:  physicalKey,
		Data: append([]byte(nil), data...),
	}); err != nil {
		return fmt.Errorf("objectblob: put object: %w", err)
	}
	return nil
}

func (b *Backend) get(ctx context.Context, physicalKey string) ([]byte, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	object, err := b.client.GetObject(ctx, physicalKey)
	if err != nil {
		return nil, fmt.Errorf("objectblob: get object: %w", err)
	}
	return append([]byte(nil), object.Data...), nil
}

func (b *Backend) physicalKey(key string) (string, error) {
	logicalKey, err := normalizeKey(key, false)
	if err != nil {
		return "", err
	}
	return joinKey(b.prefix, logicalKey), nil
}

func (b *Backend) physicalPrefix(prefix string) (string, error) {
	logicalPrefix, err := normalizePrefix(prefix)
	if err != nil {
		return "", err
	}
	if b.prefix == "" {
		return logicalPrefix, nil
	}
	if logicalPrefix == "" {
		return b.prefix + "/", nil
	}
	return b.prefix + "/" + logicalPrefix, nil
}

func (b *Backend) logicalKey(physicalKey string) (string, bool) {
	if b.prefix == "" {
		return physicalKey, true
	}
	prefix := b.prefix + "/"
	if !strings.HasPrefix(physicalKey, prefix) {
		return "", false
	}
	return strings.TrimPrefix(physicalKey, prefix), true
}

func normalizeRootPrefix(prefix string) (string, error) {
	normalized, err := normalizePrefix(prefix)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(normalized, "/"), nil
}

func normalizePrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	hasTrailingSlash := strings.HasSuffix(prefix, "/")
	base := strings.TrimSuffix(prefix, "/")
	normalized, err := normalizeKey(base, false)
	if err != nil {
		return "", err
	}
	if hasTrailingSlash {
		normalized += "/"
	}
	return normalized, nil
}

func normalizeKey(key string, allowEmpty bool) (string, error) {
	if key == "" {
		if allowEmpty {
			return "", nil
		}
		return "", ErrUnsafeKey
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", ErrUnsafeKey
	}
	key = strings.TrimSuffix(key, "/")
	if key == "" {
		if allowEmpty {
			return "", nil
		}
		return "", ErrUnsafeKey
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ErrUnsafeKey
		}
	}
	return strings.Join(parts, "/"), nil
}

func joinKey(prefix string, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}

func copyMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
