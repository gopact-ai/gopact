package s3store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsRecordsWithS3CASIndex(t *testing.T) {
	ctx := context.Background()
	client := newFakeS3()
	backend, err := NewBackend(client, "gopact-checkpoints", objectstore.WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewRowStore[string](
		backend,
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
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

	restored, err := checkpoint.NewRowStore[string](
		backend,
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewRowStore(restored) error = %v", err)
	}
	latest, ok, err := restored.Latest(ctx, "thread-1")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if latest.ID != "checkpoint-2" || latest.State != "two" || latest.ConfigVersion != "config:v1" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 state two config:v1", latest)
	}
	if !client.sawIfNoneMatch("tenant-a/checkpoint/threads/dGhyZWFkLTE.json", "*") {
		t.Fatalf("put history = %+v, want thread index create with IfNoneMatch *", client.puts)
	}
	if !client.sawIfMatch("tenant-a/checkpoint/threads/dGhyZWFkLTE.json") {
		t.Fatalf("put history = %+v, want thread index update with IfMatch etag", client.puts)
	}
}

func TestClientMapsS3NotFoundAndPreconditionErrors(t *testing.T) {
	ctx := context.Background()
	client := newFakeS3()
	adapter, err := NewClient(client, "gopact-checkpoints")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := adapter.GetObject(ctx, "missing.json"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("GetObject(missing) error = %v, want ErrNotFound", err)
	}
	_, err = adapter.PutObject(ctx, objectstore.Object{Key: "index.json", Data: []byte("{}")}, objectstore.Precondition{IfAbsent: true})
	if err != nil {
		t.Fatalf("PutObject(first) error = %v", err)
	}
	_, err = adapter.PutObject(ctx, objectstore.Object{Key: "index.json", Data: []byte("{}")}, objectstore.Precondition{IfAbsent: true})
	if !errors.Is(err, objectstore.ErrPreconditionFailed) {
		t.Fatalf("PutObject(conflict) error = %v, want ErrPreconditionFailed", err)
	}
}

func TestNewClientRejectsInvalidInputs(t *testing.T) {
	if adapter, err := NewClient(nil, "bucket"); !errors.Is(err, ErrClientRequired) || adapter != nil {
		t.Fatalf("NewClient(nil) adapter=%v err=%v, want ErrClientRequired", adapter, err)
	}
	if adapter, err := NewClient(newFakeS3(), ""); !errors.Is(err, ErrBucketRequired) || adapter != nil {
		t.Fatalf("NewClient(empty bucket) adapter=%v err=%v, want ErrBucketRequired", adapter, err)
	}
	if backend, err := NewBackend(nil, "bucket"); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
}

type fakeS3 struct {
	mu          sync.Mutex
	objects     map[string]fakeS3Object
	nextVersion int
	puts        []fakeS3Put
}

type fakeS3Object struct {
	body      []byte
	etag      string
	metadata  map[string]string
	updatedAt time.Time
}

type fakeS3Put struct {
	key         string
	ifNoneMatch string
	ifMatch     string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{
		objects:     make(map[string]fakeS3Object),
		nextVersion: 1,
	}
}

func (c *fakeS3) PutObject(ctx context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	key := aws.ToString(input.Key)
	current, exists := c.objects[key]
	if aws.ToString(input.IfNoneMatch) == "*" && exists {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "object exists"}
	}
	if match := aws.ToString(input.IfMatch); match != "" && (!exists || current.etag != match) {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "etag mismatch"}
	}
	etag := fmt.Sprintf(`"v%d"`, c.nextVersion)
	c.nextVersion++
	updatedAt := time.Now().UTC()
	c.objects[key] = fakeS3Object{
		body:      append([]byte(nil), raw...),
		etag:      etag,
		metadata:  copyStringMap(input.Metadata),
		updatedAt: updatedAt,
	}
	c.puts = append(c.puts, fakeS3Put{
		key:         key,
		ifNoneMatch: aws.ToString(input.IfNoneMatch),
		ifMatch:     aws.ToString(input.IfMatch),
	})
	return &s3.PutObjectOutput{ETag: aws.String(etag)}, nil
}

func (c *fakeS3) GetObject(ctx context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
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
		ETag:         aws.String(object.etag),
		LastModified: aws.Time(object.updatedAt),
		Metadata:     copyStringMap(object.metadata),
	}, nil
}

func (c *fakeS3) sawIfNoneMatch(key string, value string) bool {
	for _, put := range c.puts {
		if put.key == key && put.ifNoneMatch == value {
			return true
		}
	}
	return false
}

func (c *fakeS3) sawIfMatch(key string) bool {
	for _, put := range c.puts {
		if put.key == key && strings.HasPrefix(put.ifMatch, `"v`) {
			return true
		}
	}
	return false
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
