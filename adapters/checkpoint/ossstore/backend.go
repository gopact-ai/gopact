// Package ossstore adapts Alibaba Cloud OSS SDK v2 clients to checkpoint objectstore clients.
package ossstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
)

var (
	ErrClientRequired = errors.New("checkpoint ossstore: client is required")
	ErrBucketRequired = errors.New("checkpoint ossstore: bucket is required")
)

// API is the subset of the Alibaba Cloud OSS SDK v2 client consumed by Client.
type API interface {
	PutObject(ctx context.Context, request *aliyunoss.PutObjectRequest, optFns ...func(*aliyunoss.Options)) (*aliyunoss.PutObjectResult, error)
	GetObject(ctx context.Context, request *aliyunoss.GetObjectRequest, optFns ...func(*aliyunoss.Options)) (*aliyunoss.GetObjectResult, error)
}

// Client adapts an injected OSS API client and bucket to objectstore.Client.
type Client struct {
	client API
	bucket string
}

var _ objectstore.Client = (*Client)(nil)

// NewClient creates an OSS conditional object adapter.
func NewClient(client API, bucket string) (*Client, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, ErrBucketRequired
	}
	return &Client{client: client, bucket: bucket}, nil
}

// NewBackend creates a checkpoint row backend backed by OSS conditional writes.
func NewBackend(client API, bucket string, opts ...objectstore.Option) (*objectstore.Backend, error) {
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

// PutObject stores or replaces one OSS object, applying CAS preconditions when requested.
func (c *Client) PutObject(ctx context.Context, object objectstore.Object, precondition objectstore.Precondition) (objectstore.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	request := &aliyunoss.PutObjectRequest{
		Bucket:   ptr(c.bucket),
		Key:      ptr(object.Key),
		Body:     bytes.NewReader(append([]byte(nil), object.Data...)),
		Metadata: copyMetadata(object.Metadata),
	}
	if precondition.IfAbsent {
		request.ForbidOverwrite = ptr("true")
	}
	if precondition.IfVersion != "" {
		request.Headers = map[string]string{
			"If-Match": precondition.IfVersion,
		}
	}
	out, err := c.client.PutObject(ctx, request)
	if isOSSPreconditionFailed(err) {
		return objectstore.Object{}, objectstore.ErrPreconditionFailed
	}
	if isOSSNotFound(err) {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	if err != nil {
		return objectstore.Object{}, fmt.Errorf("checkpoint ossstore: put object: %w", err)
	}
	return objectstore.Object{
		Key:      object.Key,
		Data:     append([]byte(nil), object.Data...),
		Version:  stringValue(out.ETag),
		Metadata: copyMetadata(object.Metadata),
	}, nil
}

// GetObject reads one OSS object and exposes its ETag as the CAS version.
func (c *Client) GetObject(ctx context.Context, key string) (objectstore.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	out, err := c.client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: ptr(c.bucket),
		Key:    ptr(key),
	})
	if isOSSNotFound(err) {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	if err != nil {
		return objectstore.Object{}, fmt.Errorf("checkpoint ossstore: get object: %w", err)
	}
	var raw []byte
	if out.Body != nil {
		defer func() {
			_ = out.Body.Close()
		}()
		raw, err = io.ReadAll(out.Body)
		if err != nil {
			return objectstore.Object{}, fmt.Errorf("checkpoint ossstore: read object body: %w", err)
		}
	}
	return objectstore.Object{
		Key:       key,
		Data:      raw,
		Version:   stringValue(out.ETag),
		UpdatedAt: timeValue(out.LastModified),
		Metadata:  copyMetadata(out.Metadata),
	}, nil
}

func isOSSNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectstore.ErrNotFound) {
		return true
	}
	var serviceErr *aliyunoss.ServiceError
	if !errors.As(err, &serviceErr) {
		return false
	}
	switch serviceErr.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return true
	default:
		return serviceErr.StatusCode == 404
	}
}

func isOSSPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectstore.ErrPreconditionFailed) {
		return true
	}
	var serviceErr *aliyunoss.ServiceError
	if !errors.As(err, &serviceErr) {
		return false
	}
	switch serviceErr.Code {
	case "PreconditionFailed", "FileAlreadyExists", "ObjectAlreadyExists":
		return true
	default:
		return serviceErr.StatusCode == 409 || serviceErr.StatusCode == 412
	}
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}

func ptr[T any](value T) *T {
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
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
