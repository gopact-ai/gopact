// Package s3store adapts AWS SDK v2 S3 clients to checkpoint objectstore clients.
package s3store

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
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
)

var (
	ErrClientRequired = errors.New("checkpoint s3store: client is required")
	ErrBucketRequired = errors.New("checkpoint s3store: bucket is required")
)

// API is the subset of the AWS SDK v2 S3 client consumed by Client.
type API interface {
	PutObject(ctx context.Context, input *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// Client adapts an injected S3 API client and bucket to objectstore.Client.
type Client struct {
	client API
	bucket string
}

var _ objectstore.Client = (*Client)(nil)

// NewClient creates an S3 conditional object adapter.
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

// NewBackend creates a checkpoint row backend backed by S3 conditional writes.
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

// PutObject stores or replaces one S3 object, applying CAS preconditions when requested.
func (c *Client) PutObject(ctx context.Context, object objectstore.Object, precondition objectstore.Precondition) (objectstore.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	input := &s3.PutObjectInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(object.Key),
		Body:     bytes.NewReader(append([]byte(nil), object.Data...)),
		Metadata: copyMetadata(object.Metadata),
	}
	if precondition.IfAbsent {
		input.IfNoneMatch = aws.String("*")
	}
	if precondition.IfVersion != "" {
		input.IfMatch = aws.String(precondition.IfVersion)
	}
	out, err := c.client.PutObject(ctx, input)
	if isS3PreconditionFailed(err) {
		return objectstore.Object{}, objectstore.ErrPreconditionFailed
	}
	if isS3NotFound(err) {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	if err != nil {
		return objectstore.Object{}, fmt.Errorf("checkpoint s3store: put object: %w", err)
	}
	return objectstore.Object{
		Key:      object.Key,
		Data:     append([]byte(nil), object.Data...),
		Version:  aws.ToString(out.ETag),
		Metadata: copyMetadata(object.Metadata),
	}, nil
}

// GetObject reads one S3 object and exposes its ETag as the CAS version.
func (c *Client) GetObject(ctx context.Context, key string) (objectstore.Object, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if isS3NotFound(err) {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	if err != nil {
		return objectstore.Object{}, fmt.Errorf("checkpoint s3store: get object: %w", err)
	}
	var raw []byte
	if out.Body != nil {
		defer func() {
			_ = out.Body.Close()
		}()
		raw, err = io.ReadAll(out.Body)
		if err != nil {
			return objectstore.Object{}, fmt.Errorf("checkpoint s3store: read object body: %w", err)
		}
	}
	return objectstore.Object{
		Key:       key,
		Data:      raw,
		Version:   aws.ToString(out.ETag),
		UpdatedAt: aws.ToTime(out.LastModified).UTC(),
		Metadata:  copyMetadata(out.Metadata),
	}, nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectstore.ErrNotFound) {
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

func isS3PreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, objectstore.ErrPreconditionFailed) {
		return true
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "PreconditionFailed", "ConditionalRequestConflict", "412", "409":
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
