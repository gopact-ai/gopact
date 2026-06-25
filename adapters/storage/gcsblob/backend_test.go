package gcsblob

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/adapters/storage/objectblob"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsCheckpointObjectsThroughGCS(t *testing.T) {
	ctx := context.Background()
	bucket := newFakeGCSBlobBucket()
	backend, err := NewBackendWithBucket(
		bucket,
		objectblob.WithPrefix("tenant-a"),
		objectblob.WithPageSize(1),
	)
	if err != nil {
		t.Fatalf("NewBackendWithBucket() error = %v", err)
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
	if bucket.listCalls < 2 {
		t.Fatalf("listCalls = %d, want paged GCS list to require multiple calls", bucket.listCalls)
	}
	if bucket.lastPageSize != 1 {
		t.Fatalf("lastPageSize = %d, want 1", bucket.lastPageSize)
	}
	for key := range bucket.objects {
		if !strings.HasPrefix(key, "tenant-a/agent/prod/records/") {
			t.Fatalf("stored key %q does not include adapter prefix", key)
		}
	}
}

func TestClientMapsGCSNotFound(t *testing.T) {
	ctx := context.Background()
	bucket := newFakeGCSBlobBucket()
	adapter, err := NewClientWithBucket(bucket)
	if err != nil {
		t.Fatalf("NewClientWithBucket() error = %v", err)
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
	if adapter, err := NewClient(fakeStorageClient{}, ""); !errors.Is(err, ErrBucketRequired) || adapter != nil {
		t.Fatalf("NewClient(empty bucket) adapter=%v err=%v, want ErrBucketRequired", adapter, err)
	}
	if adapter, err := NewClientWithBucket(nil); !errors.Is(err, ErrBucketAPIRequired) || adapter != nil {
		t.Fatalf("NewClientWithBucket(nil) adapter=%v err=%v, want ErrBucketAPIRequired", adapter, err)
	}
	if backend, err := NewBackend(nil, "bucket"); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
}

type fakeStorageClient struct{}

func (fakeStorageClient) Bucket(string) *storage.BucketHandle {
	return nil
}

type fakeGCSBlobBucket struct {
	mu           sync.Mutex
	objects      map[string]fakeGCSBlobObject
	listCalls    int
	lastPageSize int
}

type fakeGCSBlobObject struct {
	body      []byte
	metadata  map[string]string
	updatedAt time.Time
}

func newFakeGCSBlobBucket() *fakeGCSBlobBucket {
	return &fakeGCSBlobBucket{objects: make(map[string]fakeGCSBlobObject)}
}

func (b *fakeGCSBlobBucket) PutObject(ctx context.Context, object objectblob.Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.objects[object.Key] = fakeGCSBlobObject{
		body:      append([]byte(nil), object.Data...),
		metadata:  copyStringMap(object.Metadata),
		updatedAt: time.Now().UTC(),
	}
	return nil
}

func (b *fakeGCSBlobBucket) GetObject(ctx context.Context, key string) (objectblob.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectblob.Object{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	object, ok := b.objects[key]
	if !ok {
		return objectblob.Object{}, objectblob.ErrNotFound
	}
	return objectblob.Object{
		Key:       key,
		Data:      append([]byte(nil), object.body...),
		UpdatedAt: object.updatedAt,
		Metadata:  copyStringMap(object.metadata),
	}, nil
}

func (b *fakeGCSBlobBucket) ListObjects(ctx context.Context, request objectblob.ListRequest) (objectblob.ListPage, error) {
	if err := ctx.Err(); err != nil {
		return objectblob.ListPage{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.listCalls++
	b.lastPageSize = request.PageSize
	keys := b.keysLocked(request.Prefix)
	start := 0
	if request.PageToken != "" {
		for i, key := range keys {
			if key == request.PageToken {
				start = i + 1
				break
			}
		}
	}
	limit := request.PageSize
	if limit <= 0 || limit > len(keys)-start {
		limit = len(keys) - start
	}
	if limit < 0 {
		limit = 0
	}
	pageKeys := keys[start : start+limit]
	objects := make([]objectblob.Info, 0, len(pageKeys))
	for _, key := range pageKeys {
		object := b.objects[key]
		objects = append(objects, objectblob.Info{
			Key:       key,
			UpdatedAt: object.updatedAt,
			Metadata:  copyStringMap(object.metadata),
		})
	}
	next := ""
	if start+limit < len(keys) && len(pageKeys) > 0 {
		next = pageKeys[len(pageKeys)-1]
	}
	return objectblob.ListPage{Objects: objects, NextPageToken: next}, nil
}

func (b *fakeGCSBlobBucket) keysLocked(prefix string) []string {
	keys := make([]string, 0, len(b.objects))
	for key := range b.objects {
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
