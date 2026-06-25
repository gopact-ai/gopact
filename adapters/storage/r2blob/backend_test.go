package r2blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsCheckpointObjectsThroughR2CompatibleS3(t *testing.T) {
	ctx := context.Background()
	client := newFakeR2S3()
	backend, err := NewBackend(
		client,
		"gopact-checkpoints",
		objectblob.WithPrefix("tenant-a"),
		objectblob.WithPageSize(1),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewObjectStore[string](
		backend,
		checkpoint.WithObjectPrefix[string]("agent/prod"),
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}

	for _, checkpoint := range []graph.Checkpoint[string]{
		{
			ID:        "checkpoint-1",
			IDs:       gopact.RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
			ThreadID:  "thread-1",
			Step:      1,
			Node:      "first",
			State:     "one",
			CreatedAt: time.Unix(1, 0).UTC(),
		},
		{
			ID:        "checkpoint-2",
			IDs:       gopact.RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
			ThreadID:  "thread-1",
			Step:      2,
			Node:      "second",
			State:     "two",
			CreatedAt: time.Unix(2, 0).UTC(),
		},
	} {
		if err := store.Put(ctx, checkpoint); err != nil {
			t.Fatalf("Put(%s) error = %v", checkpoint.ID, err)
		}
	}

	latest, ok, err := store.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.ID != "checkpoint-2" || latest.State != "two" || latest.ConfigVersion != "config:v1" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 state two config:v1", latest)
	}
	if client.listCalls < 2 {
		t.Fatalf("listCalls = %d, want paged R2/S3-compatible list to require multiple calls", client.listCalls)
	}
	if client.lastListMaxKeys != 1 {
		t.Fatalf("lastListMaxKeys = %d, want 1", client.lastListMaxKeys)
	}
	for key := range client.objects {
		if !strings.HasPrefix(key, "tenant-a/agent/prod/records/") {
			t.Fatalf("stored key %q does not include adapter prefix", key)
		}
	}
}

func TestClientMapsR2CompatibleNotFound(t *testing.T) {
	ctx := context.Background()
	client := newFakeR2S3()
	adapter, err := NewClient(client, "gopact-checkpoints")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = adapter.GetObject(ctx, "missing.json")
	if !errors.Is(err, objectblob.ErrNotFound) {
		t.Fatalf("GetObject(missing) error = %v, want objectblob.ErrNotFound", err)
	}
}

func TestNewClientRejectsInvalidInputs(t *testing.T) {
	if adapter, err := NewClient(nil, "bucket"); !errors.Is(err, ErrClientRequired) || adapter != nil {
		t.Fatalf("NewClient(nil) adapter=%v err=%v, want ErrClientRequired", adapter, err)
	}
	if adapter, err := NewClient(newFakeR2S3(), ""); !errors.Is(err, ErrBucketRequired) || adapter != nil {
		t.Fatalf("NewClient(empty bucket) adapter=%v err=%v, want ErrBucketRequired", adapter, err)
	}
	if backend, err := NewBackend(nil, "bucket"); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
}

type fakeR2S3 struct {
	mu              sync.Mutex
	objects         map[string]fakeR2S3Object
	listCalls       int
	lastListMaxKeys int32
}

type fakeR2S3Object struct {
	body      []byte
	metadata  map[string]string
	updatedAt time.Time
}

func newFakeR2S3() *fakeR2S3 {
	return &fakeR2S3{objects: make(map[string]fakeR2S3Object)}
}

func (c *fakeR2S3) PutObject(ctx context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	updatedAt := time.Now().UTC()
	c.objects[aws.ToString(input.Key)] = fakeR2S3Object{
		body:      append([]byte(nil), raw...),
		metadata:  copyStringMap(input.Metadata),
		updatedAt: updatedAt,
	}
	return &s3.PutObjectOutput{}, nil
}

func (c *fakeR2S3) GetObject(ctx context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	object, ok := c.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing key"}
	}
	return &s3.GetObjectOutput{
		Body:         io.NopCloser(bytes.NewReader(object.body)),
		LastModified: aws.Time(object.updatedAt),
		Metadata:     copyStringMap(object.metadata),
	}, nil
}

func (c *fakeR2S3) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.listCalls++
	c.lastListMaxKeys = aws.ToInt32(input.MaxKeys)
	keys := c.keysLocked(aws.ToString(input.Prefix))
	start := 0
	if token := aws.ToString(input.ContinuationToken); token != "" {
		for i, key := range keys {
			if key == token {
				start = i + 1
				break
			}
		}
	}
	limit := int(aws.ToInt32(input.MaxKeys))
	if limit <= 0 || limit > len(keys)-start {
		limit = len(keys) - start
	}
	if limit < 0 {
		limit = 0
	}
	pageKeys := keys[start : start+limit]
	contents := make([]types.Object, 0, len(pageKeys))
	for _, key := range pageKeys {
		object := c.objects[key]
		contents = append(contents, types.Object{
			Key:          aws.String(key),
			LastModified: aws.Time(object.updatedAt),
		})
	}
	var next *string
	if start+limit < len(keys) && len(pageKeys) > 0 {
		next = aws.String(pageKeys[len(pageKeys)-1])
	}
	return &s3.ListObjectsV2Output{Contents: contents, NextContinuationToken: next}, nil
}

func (c *fakeR2S3) keysLocked(prefix string) []string {
	keys := make([]string, 0, len(c.objects))
	for key := range c.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
