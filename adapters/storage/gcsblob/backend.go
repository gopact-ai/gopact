// Package gcsblob adapts Google Cloud Storage clients to gopact object/blob ports.
package gcsblob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrClientRequired    = errors.New("gcsblob: storage client is required")
	ErrBucketRequired    = errors.New("gcsblob: bucket is required")
	ErrBucketAPIRequired = errors.New("gcsblob: bucket api is required")
)

// StorageClient is the subset of the GCS SDK client consumed by NewClient.
type StorageClient interface {
	Bucket(name string) *storage.BucketHandle
}

// Bucket is a bucket-scoped object API. It is useful for tests and for hosts
// that wrap the official GCS SDK before handing it to gopact.
type Bucket interface {
	PutObject(ctx context.Context, object objectblob.Object) error
	GetObject(ctx context.Context, key string) (objectblob.Object, error)
	ListObjects(ctx context.Context, request objectblob.ListRequest) (objectblob.ListPage, error)
}

// Client adapts a bucket-scoped GCS API to objectblob.Client.
type Client struct {
	bucket Bucket
}

var _ objectblob.Client = (*Client)(nil)

// NewClient creates a GCS object client adapter from an injected SDK client and bucket.
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

// NewClientWithBucket creates a GCS object client adapter from a bucket-scoped API.
func NewClientWithBucket(bucket Bucket) (*Client, error) {
	if bucket == nil {
		return nil, ErrBucketAPIRequired
	}
	return &Client{bucket: bucket}, nil
}

// NewBackend creates a GCS-backed object/blob backend from an injected SDK client and bucket.
func NewBackend(client StorageClient, bucket string, opts ...objectblob.Option) (*objectblob.Backend, error) {
	adapter, err := NewClient(client, bucket)
	if err != nil {
		return nil, err
	}
	backend, err := objectblob.New(adapter, opts...)
	if err != nil {
		return nil, err
	}
	return backend, nil
}

// NewBackendWithBucket creates a GCS-backed object/blob backend from a bucket-scoped API.
func NewBackendWithBucket(bucket Bucket, opts ...objectblob.Option) (*objectblob.Backend, error) {
	adapter, err := NewClientWithBucket(bucket)
	if err != nil {
		return nil, err
	}
	backend, err := objectblob.New(adapter, opts...)
	if err != nil {
		return nil, err
	}
	return backend, nil
}

// PutObject stores or replaces one object in GCS.
func (c *Client) PutObject(ctx context.Context, object objectblob.Object) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.bucket.PutObject(ctx, object); err != nil {
		return fmt.Errorf("gcsblob: put object: %w", err)
	}
	return nil
}

// GetObject reads one object from GCS.
func (c *Client) GetObject(ctx context.Context, key string) (objectblob.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectblob.Object{}, err
	}
	object, err := c.bucket.GetObject(ctx, key)
	if isGCSNotFound(err) {
		return objectblob.Object{}, objectblob.ErrNotFound
	}
	if err != nil {
		return objectblob.Object{}, fmt.Errorf("gcsblob: get object: %w", err)
	}
	return object, nil
}

// ListObjects returns one page of GCS objects.
func (c *Client) ListObjects(ctx context.Context, request objectblob.ListRequest) (objectblob.ListPage, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectblob.ListPage{}, err
	}
	page, err := c.bucket.ListObjects(ctx, request)
	if isGCSNotFound(err) {
		return objectblob.ListPage{}, objectblob.ErrNotFound
	}
	if err != nil {
		return objectblob.ListPage{}, fmt.Errorf("gcsblob: list objects: %w", err)
	}
	return page, nil
}

type sdkBucket struct {
	handle *storage.BucketHandle
}

func (b *sdkBucket) PutObject(ctx context.Context, object objectblob.Object) error {
	obj := b.handle.Object(object.Key)
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	writer := obj.NewWriter(writeCtx)
	writer.Metadata = copyMetadata(object.Metadata)
	if _, err := io.Copy(writer, bytes.NewReader(append([]byte(nil), object.Data...))); err != nil {
		cancel()
		return err
	}
	return writer.Close()
}

func (b *sdkBucket) GetObject(ctx context.Context, key string) (objectblob.Object, error) {
	reader, err := b.handle.Object(key).NewReader(ctx)
	if err != nil {
		return objectblob.Object{}, err
	}
	defer func() {
		_ = reader.Close()
	}()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return objectblob.Object{}, err
	}
	return objectblob.Object{
		Key:       key,
		Data:      raw,
		UpdatedAt: reader.Attrs.LastModified.UTC(),
		Metadata:  copyMetadata(reader.Metadata()),
	}, nil
}

func (b *sdkBucket) ListObjects(ctx context.Context, request objectblob.ListRequest) (objectblob.ListPage, error) {
	iter := b.handle.Objects(ctx, &storage.Query{Prefix: request.Prefix})
	pager := iterator.NewPager(iter, request.PageSize, request.PageToken)
	var attrs []*storage.ObjectAttrs
	next, err := pager.NextPage(&attrs)
	if err != nil {
		return objectblob.ListPage{}, err
	}
	objects := make([]objectblob.Info, 0, len(attrs))
	for _, attr := range attrs {
		if attr == nil {
			continue
		}
		objects = append(objects, objectblob.Info{
			Key:       attr.Name,
			UpdatedAt: attr.Updated.UTC(),
			Metadata:  copyMetadata(attr.Metadata),
		})
	}
	return objectblob.ListPage{Objects: objects, NextPageToken: next}, nil
}

func isGCSNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectblob.ErrNotFound) ||
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
