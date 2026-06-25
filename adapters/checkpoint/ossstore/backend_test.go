package ossstore

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

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsRecordsWithOSSCASIndex(t *testing.T) {
	ctx := context.Background()
	client := newFakeOSS()
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
	if !client.sawForbidOverwrite("tenant-a/checkpoint/threads/dGhyZWFkLTE.json", "true") {
		t.Fatalf("put history = %+v, want thread index create with x-oss-forbid-overwrite true", client.puts)
	}
	if !client.sawIfMatch("tenant-a/checkpoint/threads/dGhyZWFkLTE.json") {
		t.Fatalf("put history = %+v, want thread index update with If-Match etag", client.puts)
	}
}

func TestClientMapsOSSNotFoundAndPreconditionErrors(t *testing.T) {
	ctx := context.Background()
	client := newFakeOSS()
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
	if adapter, err := NewClient(newFakeOSS(), ""); !errors.Is(err, ErrBucketRequired) || adapter != nil {
		t.Fatalf("NewClient(empty bucket) adapter=%v err=%v, want ErrBucketRequired", adapter, err)
	}
	if backend, err := NewBackend(nil, "bucket"); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
}

type fakeOSS struct {
	mu          sync.Mutex
	objects     map[string]fakeOSSObject
	nextVersion int
	puts        []fakeOSSPut
}

type fakeOSSObject struct {
	body      []byte
	etag      string
	metadata  map[string]string
	updatedAt time.Time
}

type fakeOSSPut struct {
	key             string
	forbidOverwrite string
	ifMatch         string
}

func newFakeOSS() *fakeOSS {
	return &fakeOSS{
		objects:     make(map[string]fakeOSSObject),
		nextVersion: 1,
	}
}

func (c *fakeOSS) PutObject(ctx context.Context, request *aliyunoss.PutObjectRequest, _ ...func(*aliyunoss.Options)) (*aliyunoss.PutObjectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	key := stringValue(request.Key)
	current, exists := c.objects[key]
	if stringValue(request.ForbidOverwrite) == "true" && exists {
		return nil, &aliyunoss.ServiceError{StatusCode: 409, Code: "FileAlreadyExists", Message: "object exists"}
	}
	if match := request.Headers["If-Match"]; match != "" && (!exists || current.etag != match) {
		return nil, &aliyunoss.ServiceError{StatusCode: 412, Code: "PreconditionFailed", Message: "etag mismatch"}
	}
	etag := fmt.Sprintf(`"v%d"`, c.nextVersion)
	c.nextVersion++
	updatedAt := time.Now().UTC()
	c.objects[key] = fakeOSSObject{
		body:      append([]byte(nil), raw...),
		etag:      etag,
		metadata:  copyStringMap(request.Metadata),
		updatedAt: updatedAt,
	}
	c.puts = append(c.puts, fakeOSSPut{
		key:             key,
		forbidOverwrite: stringValue(request.ForbidOverwrite),
		ifMatch:         request.Headers["If-Match"],
	})
	return &aliyunoss.PutObjectResult{ETag: ptr(etag)}, nil
}

func (c *fakeOSS) GetObject(ctx context.Context, request *aliyunoss.GetObjectRequest, _ ...func(*aliyunoss.Options)) (*aliyunoss.GetObjectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	object, ok := c.objects[stringValue(request.Key)]
	if !ok {
		return nil, &aliyunoss.ServiceError{StatusCode: 404, Code: "NoSuchKey", Message: "missing key"}
	}
	return &aliyunoss.GetObjectResult{
		Body:         io.NopCloser(bytes.NewReader(object.body)),
		ETag:         ptr(object.etag),
		LastModified: &object.updatedAt,
		Metadata:     copyStringMap(object.metadata),
	}, nil
}

func (c *fakeOSS) sawForbidOverwrite(key string, value string) bool {
	for _, put := range c.puts {
		if put.key == key && put.forbidOverwrite == value {
			return true
		}
	}
	return false
}

func (c *fakeOSS) sawIfMatch(key string) bool {
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
