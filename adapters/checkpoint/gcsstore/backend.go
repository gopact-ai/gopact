// Package gcsstore adapts Google Cloud Storage clients to checkpoint objectstore clients.
package gcsstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrClientRequired    = errors.New("checkpoint gcsstore: storage client is required")
	ErrBucketRequired    = errors.New("checkpoint gcsstore: bucket is required")
	ErrBucketAPIRequired = errors.New("checkpoint gcsstore: bucket api is required")
)

// StorageClient is the subset of the GCS SDK client consumed by NewClient.
type StorageClient interface {
	Bucket(name string) *storage.BucketHandle
}

// Bucket is a bucket-scoped conditional object API. It is useful for tests and
// for hosts that wrap the official GCS SDK before handing it to gopact.
type Bucket interface {
	PutObject(ctx context.Context, object objectstore.Object, precondition objectstore.Precondition) (objectstore.Object, error)
	GetObject(ctx context.Context, key string) (objectstore.Object, error)
}

// Client adapts a bucket-scoped GCS API to objectstore.Client.
type Client struct {
	bucket Bucket
}

var _ objectstore.Client = (*Client)(nil)

// NewClient creates a GCS conditional object adapter from an injected SDK client and bucket.
func NewClient(client StorageClient, bucket string) (*Client, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, ErrBucketRequired
	}
	handle := client.Bucket(bucket)
	if handle == nil {
		return nil, ErrBucketAPIRequired
	}
	return NewClientWithBucket(&sdkBucket{handle: handle})
}

// NewClientWithBucket creates a GCS conditional object adapter from a bucket-scoped API.
func NewClientWithBucket(bucket Bucket) (*Client, error) {
	if bucket == nil {
		return nil, ErrBucketAPIRequired
	}
	return &Client{bucket: bucket}, nil
}

// NewBackend creates a checkpoint row backend backed by GCS conditional writes.
func NewBackend(client StorageClient, bucket string, opts ...objectstore.Option) (*objectstore.Backend, error) {
	adapter, err := NewClient(client, bucket)
	if err != nil {
		return nil, err
	}
	backend, err := objectstore.NewBackend(adapter, opts...)
	if err != nil {
		return nil, err
	}
	return backend, nil
}

// NewBackendWithBucket creates a checkpoint row backend from a bucket-scoped GCS API.
func NewBackendWithBucket(bucket Bucket, opts ...objectstore.Option) (*objectstore.Backend, error) {
	adapter, err := NewClientWithBucket(bucket)
	if err != nil {
		return nil, err
	}
	backend, err := objectstore.NewBackend(adapter, opts...)
	if err != nil {
		return nil, err
	}
	return backend, nil
}

// PutObject stores or replaces one GCS object, applying CAS preconditions when requested.
func (c *Client) PutObject(ctx context.Context, object objectstore.Object, precondition objectstore.Precondition) (objectstore.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	stored, err := c.bucket.PutObject(ctx, object, precondition)
	if isGCSPreconditionFailed(err) {
		return objectstore.Object{}, objectstore.ErrPreconditionFailed
	}
	if isGCSNotFound(err) {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	if err != nil {
		return objectstore.Object{}, fmt.Errorf("checkpoint gcsstore: put object: %w", err)
	}
	return stored, nil
}

// GetObject reads one GCS object and exposes its generation as the CAS version.
func (c *Client) GetObject(ctx context.Context, key string) (objectstore.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	object, err := c.bucket.GetObject(ctx, key)
	if isGCSNotFound(err) {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	if err != nil {
		return objectstore.Object{}, fmt.Errorf("checkpoint gcsstore: get object: %w", err)
	}
	return object, nil
}

type sdkBucket struct {
	handle *storage.BucketHandle
}

func (b *sdkBucket) PutObject(ctx context.Context, object objectstore.Object, precondition objectstore.Precondition) (objectstore.Object, error) {
	obj := b.handle.Object(object.Key)
	var err error
	if obj, err = applyPrecondition(obj, precondition); err != nil {
		return objectstore.Object{}, err
	}
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	writer := obj.NewWriter(writeCtx)
	writer.Metadata = copyMetadata(object.Metadata)
	if _, err := io.Copy(writer, bytes.NewReader(append([]byte(nil), object.Data...))); err != nil {
		cancel()
		return objectstore.Object{}, err
	}
	if err := writer.Close(); err != nil {
		return objectstore.Object{}, err
	}
	attrs := writer.Attrs()
	stored := objectstore.Object{
		Key:      object.Key,
		Data:     append([]byte(nil), object.Data...),
		Metadata: copyMetadata(object.Metadata),
	}
	if attrs != nil {
		stored.Version = generationVersion(attrs.Generation)
		stored.UpdatedAt = attrs.Updated.UTC()
		stored.Metadata = copyMetadata(attrs.Metadata)
	}
	return stored, nil
}

func (b *sdkBucket) GetObject(ctx context.Context, key string) (objectstore.Object, error) {
	reader, err := b.handle.Object(key).NewReader(ctx)
	if err != nil {
		return objectstore.Object{}, err
	}
	defer func() {
		_ = reader.Close()
	}()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return objectstore.Object{}, err
	}
	return objectstore.Object{
		Key:       key,
		Data:      raw,
		Version:   generationVersion(reader.Attrs.Generation),
		UpdatedAt: reader.Attrs.LastModified.UTC(),
		Metadata:  copyMetadata(reader.Metadata()),
	}, nil
}

func applyPrecondition(obj *storage.ObjectHandle, precondition objectstore.Precondition) (*storage.ObjectHandle, error) {
	var cond storage.Conditions
	if precondition.IfAbsent {
		cond.DoesNotExist = true
	}
	if precondition.IfVersion != "" {
		generation, err := strconv.ParseInt(precondition.IfVersion, 10, 64)
		if err != nil || generation <= 0 {
			return nil, fmt.Errorf("checkpoint gcsstore: invalid generation %q: %w", precondition.IfVersion, objectstore.ErrPreconditionFailed)
		}
		cond.GenerationMatch = generation
	}
	if !cond.DoesNotExist && cond.GenerationMatch == 0 {
		return obj, nil
	}
	return obj.If(cond), nil
}

func generationVersion(generation int64) string {
	if generation <= 0 {
		return ""
	}
	return strconv.FormatInt(generation, 10)
}

func isGCSNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectstore.ErrNotFound) ||
		errors.Is(err, storage.ErrObjectNotExist) ||
		errors.Is(err, storage.ErrBucketNotExist) {
		return true
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == 404 {
		return true
	}
	return status.Code(err) == codes.NotFound
}

func isGCSPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectstore.ErrPreconditionFailed) {
		return true
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && (apiErr.Code == 409 || apiErr.Code == 412) {
		return true
	}
	code := status.Code(err)
	return code == codes.AlreadyExists || code == codes.Aborted || code == codes.FailedPrecondition
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
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
