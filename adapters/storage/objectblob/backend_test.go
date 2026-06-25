package objectblob

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsCheckpointObjectsAcrossPagedClient(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := New(client, WithPrefix("tenant-a"), WithPageSize(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
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
		t.Fatalf("listCalls = %d, want paged list to require multiple calls", client.listCalls)
	}
	for key := range client.objects {
		if !strings.HasPrefix(key, "tenant-a/agent/prod/records/") {
			t.Fatalf("stored key %q does not include adapter prefix", key)
		}
	}
}

func TestBackendPersistsTurnLoopBlobWithPrefix(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := New(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	store, err := gopact.NewBlobTurnLoopStore(backend, "turns/main.json")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore() error = %v", err)
	}
	state := gopact.TurnLoopState{
		Pending: []gopact.TurnInputRecord{
			{ID: "turn-input:1", Kind: gopact.TurnInputUser, Input: "queued"},
		},
		Interrupted: &gopact.TurnInputRecord{ID: "turn-input:2", Input: "question"},
		InputSeq:    2,
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, ok := client.objects["tenant-a/turns/main.json"]; !ok {
		t.Fatalf("client keys = %+v, want tenant-a/turns/main.json", client.keys())
	}
	restored, err := gopact.NewBlobTurnLoopStore(backend, "turns/main.json")
	if err != nil {
		t.Fatalf("NewBlobTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restored.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if got.InputSeq != 2 || len(got.Pending) != 1 || got.Pending[0].Input != "queued" {
		t.Fatalf("Load() = %+v, want queued pending state", got)
	}
	if got.Interrupted == nil || got.Interrupted.Input != "question" {
		t.Fatalf("Load().Interrupted = %+v, want question", got.Interrupted)
	}
}

func TestBackendMapsNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	backend, err := New(newFakeClient())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := backend.GetObject(ctx, "missing/object.json"); !errors.Is(err, checkpoint.ErrObjectNotFound) {
		t.Fatalf("GetObject(missing) error = %v, want ErrObjectNotFound", err)
	}
	if _, err := backend.GetBlob(ctx, "missing/blob.json"); !errors.Is(err, gopact.ErrTurnLoopBlobNotFound) {
		t.Fatalf("GetBlob(missing) error = %v, want ErrTurnLoopBlobNotFound", err)
	}
}

func TestBackendSupportsCustomNotFoundMatcher(t *testing.T) {
	ctx := context.Background()
	providerNotFound := errors.New("provider: no such key")
	client := newFakeClient()
	client.notFoundErr = providerNotFound
	backend, err := New(client, WithNotFound(func(err error) bool {
		return errors.Is(err, providerNotFound)
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := backend.GetObject(ctx, "missing/object.json"); !errors.Is(err, checkpoint.ErrObjectNotFound) {
		t.Fatalf("GetObject(missing) error = %v, want ErrObjectNotFound", err)
	}
}

func TestBackendRejectsUnsafeKeysAndPrefix(t *testing.T) {
	ctx := context.Background()
	if backend, err := New(newFakeClient(), WithPrefix("../escape")); !errors.Is(err, ErrUnsafeKey) || backend != nil {
		t.Fatalf("New(unsafe prefix) backend=%v err=%v, want ErrUnsafeKey", backend, err)
	}
	backend, err := New(newFakeClient(), WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := backend.PutObject(ctx, "../escape.json", []byte("bad")); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("PutObject(escape) error = %v, want ErrUnsafeKey", err)
	}
	if err := backend.PutBlob(ctx, "turns/../../escape.json", []byte("bad")); !errors.Is(err, ErrUnsafeKey) {
		t.Fatalf("PutBlob(escape) error = %v, want ErrUnsafeKey", err)
	}
}

func TestNewRejectsInvalidInputs(t *testing.T) {
	if backend, err := New(nil); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("New(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
	if backend, err := New(newFakeClient(), WithPageSize(0)); !errors.Is(err, ErrInvalidPageSize) || backend != nil {
		t.Fatalf("New(page size 0) backend=%v err=%v, want ErrInvalidPageSize", backend, err)
	}
}

type fakeClient struct {
	mu          sync.Mutex
	objects     map[string]Object
	listCalls   int
	notFoundErr error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		objects: make(map[string]Object),
	}
}

func (c *fakeClient) PutObject(ctx context.Context, object Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	object.Data = append([]byte(nil), object.Data...)
	object.Metadata = copyMetadata(object.Metadata)
	if object.UpdatedAt.IsZero() {
		object.UpdatedAt = time.Now().UTC()
	}
	c.objects[object.Key] = object
	return nil
}

func (c *fakeClient) GetObject(ctx context.Context, key string) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	object, ok := c.objects[key]
	if !ok {
		if c.notFoundErr != nil {
			return Object{}, c.notFoundErr
		}
		return Object{}, ErrNotFound
	}
	object.Data = append([]byte(nil), object.Data...)
	object.Metadata = copyMetadata(object.Metadata)
	return object, nil
}

func (c *fakeClient) ListObjects(ctx context.Context, request ListRequest) (ListPage, error) {
	if err := ctx.Err(); err != nil {
		return ListPage{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.listCalls++
	keys := c.keysLocked(request.Prefix)
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
	objects := make([]Info, 0, len(pageKeys))
	for _, key := range pageKeys {
		object := c.objects[key]
		objects = append(objects, Info{
			Key:       key,
			UpdatedAt: object.UpdatedAt,
			Metadata:  copyMetadata(object.Metadata),
		})
	}
	next := ""
	if start+limit < len(keys) && len(pageKeys) > 0 {
		next = pageKeys[len(pageKeys)-1]
	}
	return ListPage{Objects: objects, NextPageToken: next}, nil
}

func (c *fakeClient) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keysLocked("")
}

func (c *fakeClient) keysLocked(prefix string) []string {
	keys := make([]string, 0, len(c.objects))
	for key := range c.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
