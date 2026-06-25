// Package ossblob adapts Alibaba Cloud OSS SDK v2 clients to gopact object/blob ports.
package ossblob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
)

var (
	ErrClientRequired = errors.New("ossblob: client is required")
	ErrBucketRequired = errors.New("ossblob: bucket is required")
)

// API is the subset of the Alibaba Cloud OSS SDK v2 client consumed by Client.
type API interface {
	PutObject(ctx context.Context, request *aliyunoss.PutObjectRequest, optFns ...func(*aliyunoss.Options)) (*aliyunoss.PutObjectResult, error)
	GetObject(ctx context.Context, request *aliyunoss.GetObjectRequest, optFns ...func(*aliyunoss.Options)) (*aliyunoss.GetObjectResult, error)
	ListObjectsV2(ctx context.Context, request *aliyunoss.ListObjectsV2Request, optFns ...func(*aliyunoss.Options)) (*aliyunoss.ListObjectsV2Result, error)
}

// Client adapts an injected OSS API client and bucket to objectblob.Client.
type Client struct {
	client API
	bucket string
}

var _ objectblob.Client = (*Client)(nil)

// NewClient creates an OSS object client adapter.
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

// NewBackend creates an OSS-backed object/blob backend.
func NewBackend(client API, bucket string, opts ...objectblob.Option) (*objectblob.Backend, error) {
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

// PutObject stores or replaces one object in OSS.
func (c *Client) PutObject(ctx context.Context, object objectblob.Object) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := c.client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket:   ptr(c.bucket),
		Key:      ptr(object.Key),
		Body:     bytes.NewReader(append([]byte(nil), object.Data...)),
		Metadata: copyMetadata(object.Metadata),
	})
	if err != nil {
		return fmt.Errorf("ossblob: put object: %w", err)
	}
	return nil
}

// GetObject reads one object from OSS.
func (c *Client) GetObject(ctx context.Context, key string) (objectblob.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectblob.Object{}, err
	}
	out, err := c.client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: ptr(c.bucket),
		Key:    ptr(key),
	})
	if isOSSNotFound(err) {
		return objectblob.Object{}, objectblob.ErrNotFound
	}
	if err != nil {
		return objectblob.Object{}, fmt.Errorf("ossblob: get object: %w", err)
	}
	var raw []byte
	if out.Body != nil {
		defer func() {
			_ = out.Body.Close()
		}()
		raw, err = io.ReadAll(out.Body)
		if err != nil {
			return objectblob.Object{}, fmt.Errorf("ossblob: read object body: %w", err)
		}
	}
	return objectblob.Object{
		Key:       key,
		Data:      raw,
		UpdatedAt: timeValue(out.LastModified),
		Metadata:  copyMetadata(out.Metadata),
	}, nil
}

// ListObjects returns one page of OSS objects.
func (c *Client) ListObjects(ctx context.Context, request objectblob.ListRequest) (objectblob.ListPage, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectblob.ListPage{}, err
	}
	out, err := c.client.ListObjectsV2(ctx, &aliyunoss.ListObjectsV2Request{
		Bucket:            ptr(c.bucket),
		Prefix:            optionalString(request.Prefix),
		ContinuationToken: optionalString(request.PageToken),
		MaxKeys:           maxKeys(request.PageSize),
	})
	if isOSSNotFound(err) {
		return objectblob.ListPage{}, objectblob.ErrNotFound
	}
	if err != nil {
		return objectblob.ListPage{}, fmt.Errorf("ossblob: list objects: %w", err)
	}
	objects := make([]objectblob.Info, 0, len(out.Contents))
	for _, object := range out.Contents {
		objects = append(objects, objectblob.Info{
			Key:       stringValue(object.Key),
			UpdatedAt: timeValue(object.LastModified),
		})
	}
	return objectblob.ListPage{
		Objects:       objects,
		NextPageToken: stringValue(out.NextContinuationToken),
	}, nil
}

func maxKeys(size int) int32 {
	if size <= 0 {
		return 0
	}
	const maxInt32 = int64(1<<31 - 1)
	if int64(size) > maxInt32 {
		return int32(maxInt32)
	}
	return int32(size)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return ptr(value)
}

func isOSSNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectblob.ErrNotFound) {
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
