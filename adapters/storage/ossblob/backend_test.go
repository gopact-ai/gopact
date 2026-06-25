package ossblob

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

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsCheckpointObjectsThroughOSS(t *testing.T) {
	ctx := context.Background()
	client := newFakeOSS()
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

	restored, err := checkpoint.NewObjectStore[string](
		backend,
		checkpoint.WithObjectPrefix[string]("agent/prod"),
		checkpoint.WithConfigVersion[string]("config:v1"),
	)
	if err != nil {
		t.Fatalf("NewObjectStore(restored) error = %v", err)
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

	list, err := restored.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "checkpoint-1" || list[1].ID != "checkpoint-2" {
		t.Fatalf("List() = %+v, want checkpoints in created order", list)
	}
	if client.listCalls < 2 {
		t.Fatalf("listCalls = %d, want paged OSS list to require multiple calls", client.listCalls)
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

func TestClientMapsOSSNotFound(t *testing.T) {
	ctx := context.Background()
	client := newFakeOSS()
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
	if adapter, err := NewClient(newFakeOSS(), ""); !errors.Is(err, ErrBucketRequired) || adapter != nil {
		t.Fatalf("NewClient(empty bucket) adapter=%v err=%v, want ErrBucketRequired", adapter, err)
	}
	if backend, err := NewBackend(nil, "bucket"); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
}

type fakeOSS struct {
	mu              sync.Mutex
	objects         map[string]fakeOSSObject
	listCalls       int
	lastListMaxKeys int32
}

type fakeOSSObject struct {
	body      []byte
	metadata  map[string]string
	updatedAt time.Time
}

func newFakeOSS() *fakeOSS {
	return &fakeOSS{objects: make(map[string]fakeOSSObject)}
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

	updatedAt := time.Now().UTC()
	c.objects[stringValue(request.Key)] = fakeOSSObject{
		body:      append([]byte(nil), raw...),
		metadata:  copyStringMap(request.Metadata),
		updatedAt: updatedAt,
	}
	return &aliyunoss.PutObjectResult{}, nil
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
		LastModified: &object.updatedAt,
		Metadata:     copyStringMap(object.metadata),
	}, nil
}

func (c *fakeOSS) ListObjectsV2(ctx context.Context, request *aliyunoss.ListObjectsV2Request, _ ...func(*aliyunoss.Options)) (*aliyunoss.ListObjectsV2Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.listCalls++
	c.lastListMaxKeys = request.MaxKeys
	keys := c.keysLocked(stringValue(request.Prefix))
	start := 0
	if token := stringValue(request.ContinuationToken); token != "" {
		for i, key := range keys {
			if key == token {
				start = i + 1
				break
			}
		}
	}
	limit := int(request.MaxKeys)
	if limit <= 0 || limit > len(keys)-start {
		limit = len(keys) - start
	}
	if limit < 0 {
		limit = 0
	}
	pageKeys := keys[start : start+limit]
	contents := make([]aliyunoss.ObjectProperties, 0, len(pageKeys))
	for _, key := range pageKeys {
		object := c.objects[key]
		contents = append(contents, aliyunoss.ObjectProperties{
			Key:          ptr(key),
			LastModified: &object.updatedAt,
		})
	}
	var next *string
	if start+limit < len(keys) && len(pageKeys) > 0 {
		next = ptr(pageKeys[len(pageKeys)-1])
	}
	return &aliyunoss.ListObjectsV2Result{
		Contents:              contents,
		NextContinuationToken: next,
	}, nil
}

func (c *fakeOSS) keysLocked(prefix string) []string {
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
