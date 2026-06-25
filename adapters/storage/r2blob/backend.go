// Package r2blob adapts Cloudflare R2 S3-compatible clients to gopact object/blob ports.
//
// R2 is exposed through the S3 API. Hosts should construct an AWS SDK v2 S3
// client with their R2 endpoint, credentials, retry policy, and transport, then
// inject that client here. This package intentionally does not read account IDs,
// credentials, environment variables, or config files.
package r2blob

import (
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
	"github.com/gopact-ai/gopact/adapters/storage/s3blob"
)

var (
	ErrClientRequired = s3blob.ErrClientRequired
	ErrBucketRequired = s3blob.ErrBucketRequired
)

// API is the subset of an AWS SDK v2 S3-compatible client consumed by Client.
type API = s3blob.API

// Client adapts an injected R2-configured S3-compatible API client and bucket
// to objectblob.Client.
type Client = s3blob.Client

// NewClient creates an R2 object client adapter.
func NewClient(client API, bucket string) (*Client, error) {
	return s3blob.NewClient(client, bucket)
}

// NewBackend creates an R2-backed object/blob backend.
func NewBackend(client API, bucket string, opts ...objectblob.Option) (*objectblob.Backend, error) {
	return s3blob.NewBackend(client, bucket, opts...)
}
