package gcsstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/adapters/checkpoint/objectstore"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsRecordsWithGCSGenerationCASIndex(t *testing.T) {
	ctx := context.Background()
	bucket := newFakeGCSCheckpointBucket()
	backend, err := NewBackendWithBucket(bucket, objectstore.WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackendWithBucket() error = %v", err)
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

	indexKey := "tenant-a/checkpoint/threads/dGhyZWFkLTE.json"
	if !bucket.sawDoesNotExist(indexKey) {
		t.Fatalf("put history = %+v, want thread index create with DoesNotExist", bucket.puts)
	}
	if !bucket.sawGenerationMatch(indexKey) {
		t.Fatalf("put history = %+v, want thread index update with GenerationMatch", bucket.puts)
	}
}

func TestClientMapsGCSNotFoundAndPreconditionErrors(t *testing.T) {
	ctx := context.Background()
	bucket := newFakeGCSCheckpointBucket()
	adapter, err := NewClientWithBucket(bucket)
	if err != nil {
		t.Fatalf("NewClientWithBucket() error = %v", err)
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

type fakeGCSCheckpointBucket struct {
	mu          sync.Mutex
	objects     map[string]fakeGCSCheckpointObject
	nextVersion int
	puts        []fakeGCSPut
}

type fakeGCSCheckpointObject struct {
	body      []byte
	version   string
	metadata  map[string]string
	updatedAt time.Time
}

type fakeGCSPut struct {
	key             string
	doesNotExist    bool
	generationMatch string
}

func newFakeGCSCheckpointBucket() *fakeGCSCheckpointBucket {
	return &fakeGCSCheckpointBucket{
		objects:     make(map[string]fakeGCSCheckpointObject),
		nextVersion: 1,
	}
}

func (b *fakeGCSCheckpointBucket) PutObject(ctx context.Context, object objectstore.Object, precondition objectstore.Precondition) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	current, exists := b.objects[object.Key]
	if precondition.IfAbsent && exists {
		return objectstore.Object{}, objectstore.ErrPreconditionFailed
	}
	if precondition.IfVersion != "" && (!exists || current.version != precondition.IfVersion) {
		return objectstore.Object{}, objectstore.ErrPreconditionFailed
	}
	version := fmt.Sprintf("%d", b.nextVersion)
	b.nextVersion++
	updatedAt := time.Now().UTC()
	b.objects[object.Key] = fakeGCSCheckpointObject{
		body:      append([]byte(nil), object.Data...),
		version:   version,
		metadata:  copyStringMap(object.Metadata),
		updatedAt: updatedAt,
	}
	b.puts = append(b.puts, fakeGCSPut{
		key:             object.Key,
		doesNotExist:    precondition.IfAbsent,
		generationMatch: precondition.IfVersion,
	})
	return objectstore.Object{
		Key:      object.Key,
		Data:     append([]byte(nil), object.Data...),
		Version:  version,
		Metadata: copyStringMap(object.Metadata),
	}, nil
}

func (b *fakeGCSCheckpointBucket) GetObject(ctx context.Context, key string) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	object, ok := b.objects[key]
	if !ok {
		return objectstore.Object{}, objectstore.ErrNotFound
	}
	return objectstore.Object{
		Key:       key,
		Data:      append([]byte(nil), object.body...),
		Version:   object.version,
		UpdatedAt: object.updatedAt,
		Metadata:  copyStringMap(object.metadata),
	}, nil
}

func (b *fakeGCSCheckpointBucket) sawDoesNotExist(key string) bool {
	for _, put := range b.puts {
		if put.key == key && put.doesNotExist {
			return true
		}
	}
	return false
}

func (b *fakeGCSCheckpointBucket) sawGenerationMatch(key string) bool {
	for _, put := range b.puts {
		if put.key == key && strings.TrimSpace(put.generationMatch) != "" {
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
