// Package s3blob adapts AWS SDK v2 S3 clients to gopact object/blob ports.
package s3blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
)

var (
	ErrClientRequired = errors.New("s3blob: client is required")
	ErrBucketRequired = errors.New("s3blob: bucket is required")
)

// API is the subset of the AWS SDK v2 S3 client consumed by Client.
type API interface {
	PutObject(ctx context.Context, input *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// Client adapts an injected S3 API client and bucket to objectblob.Client.
type Client struct {
	client API
	bucket string
}

var _ objectblob.Client = (*Client)(nil)

// NewClient creates an S3 object client adapter.
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

// NewBackend creates an S3-backed object/blob backend.
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

// PutObject stores or replaces one object in S3.
func (c *Client) PutObject(ctx context.Context, object objectblob.Object) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(object.Key),
		Body:     bytes.NewReader(append([]byte(nil), object.Data...)),
		Metadata: copyMetadata(object.Metadata),
	})
	if err != nil {
		return fmt.Errorf("s3blob: put object: %w", err)
	}
	return nil
}

// GetObject reads one object from S3.
func (c *Client) GetObject(ctx context.Context, key string) (objectblob.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectblob.Object{}, err
	}
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if isS3NotFound(err) {
		return objectblob.Object{}, objectblob.ErrNotFound
	}
	if err != nil {
		return objectblob.Object{}, fmt.Errorf("s3blob: get object: %w", err)
	}
	var raw []byte
	if out.Body != nil {
		defer func() {
			_ = out.Body.Close()
		}()
		raw, err = io.ReadAll(out.Body)
		if err != nil {
			return objectblob.Object{}, fmt.Errorf("s3blob: read object body: %w", err)
		}
	}
	return objectblob.Object{
		Key:       key,
		Data:      raw,
		UpdatedAt: aws.ToTime(out.LastModified).UTC(),
		Metadata:  copyMetadata(out.Metadata),
	}, nil
}

// ListObjects returns one page of S3 objects.
func (c *Client) ListObjects(ctx context.Context, request objectblob.ListRequest) (objectblob.ListPage, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectblob.ListPage{}, err
	}
	out, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:            aws.String(c.bucket),
		Prefix:            aws.String(request.Prefix),
		ContinuationToken: optionalString(request.PageToken),
		MaxKeys:           maxKeys(request.PageSize),
	})
	if isS3NotFound(err) {
		return objectblob.ListPage{}, objectblob.ErrNotFound
	}
	if err != nil {
		return objectblob.ListPage{}, fmt.Errorf("s3blob: list objects: %w", err)
	}
	objects := make([]objectblob.Info, 0, len(out.Contents))
	for _, object := range out.Contents {
		objects = append(objects, objectblob.Info{
			Key:       aws.ToString(object.Key),
			UpdatedAt: aws.ToTime(object.LastModified).UTC(),
		})
	}
	return objectblob.ListPage{
		Objects:       objects,
		NextPageToken: aws.ToString(out.NextContinuationToken),
	}, nil
}

func maxKeys(size int) *int32 {
	if size <= 0 {
		return nil
	}
	const maxInt32 = int64(1<<31 - 1)
	if int64(size) > maxInt32 {
		return aws.Int32(int32(maxInt32))
	}
	return aws.Int32(int32(size))
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectblob.ErrNotFound) {
		return true
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "NoSuchKey", "NoSuchBucket", "NotFound", "404":
		return true
	default:
		return false
	}
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
