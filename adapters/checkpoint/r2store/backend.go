// Package r2store adapts Cloudflare R2 S3-compatible clients to checkpoint objectstore clients.
//
// R2 is exposed through the S3 API. Hosts should construct an AWS SDK v2 S3
// client with their R2 endpoint, credentials, retry policy, and transport, then
// inject that client here. This package intentionally does not read account IDs,
// credentials, environment variables, or config files.
package r2store

import (
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
	"github.com/gopact-ai/gopact/adapters/checkpoint/s3store"
)

var (
	ErrClientRequired = s3store.ErrClientRequired
	ErrBucketRequired = s3store.ErrBucketRequired
)

// API is the subset of an AWS SDK v2 S3-compatible client consumed by Client.
type API = s3store.API

// Client adapts an injected R2-configured S3-compatible API client and bucket
// to objectstore.Client.
type Client = s3store.Client

// NewClient creates an R2 conditional object adapter.
func NewClient(client API, bucket string) (*Client, error) {
	return s3store.NewClient(client, bucket)
}

// NewBackend creates a checkpoint row backend backed by R2 conditional writes.
func NewBackend(client API, bucket string, opts ...objectstore.Option) (*objectstore.Backend, error) {
	return s3store.NewBackend(client, bucket, opts...)
}
