package objectstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func TestBackendPersistsRecordsWithObjectCASIndex(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithPrefix("tenant-a"))
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

	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		ThreadID:  "thread-1",
		Step:      1,
		Node:      "first",
		Phase:     gopact.StepCompleted,
		State:     "one",
		Queue:     []string{"second"},
		CreatedAt: time.Unix(1, 0).UTC(),
		Metadata:  map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-2",
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		ThreadID:  "thread-1",
		Step:      2,
		Node:      "second",
		Phase:     gopact.StepInterrupted,
		State:     "two",
		Pending:   &gopact.InterruptRecord{ID: "interrupt-1", Reason: "approval"},
		Effects:   []gopact.EffectRecord{{ID: "effect-1", Type: "tool_call", ReplayPolicy: gopact.EffectReplayRecordOnly}},
		CreatedAt: time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}
	if _, ok := client.objects["tenant-a/checkpoint/records/Y2hlY2twb2ludC0x.json"]; !ok {
		t.Fatalf("client keys = %+v, want encoded record key with prefix", client.keys())
	}

	restoredBackend, err := NewBackend(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restored, err := checkpoint.NewRowStore[string](
		restoredBackend,
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
	if latest.ID != "checkpoint-2" || latest.Step != 2 || latest.State != "two" {
		t.Fatalf("Latest() = %+v, want checkpoint-2 step 2 state two", latest)
	}
	if latest.Pending == nil || latest.Pending.ID != "interrupt-1" {
		t.Fatalf("Latest().Pending = %+v, want interrupt-1", latest.Pending)
	}
	if len(latest.Effects) != 1 || latest.Effects[0].ID != "effect-1" {
		t.Fatalf("Latest().Effects = %+v, want effect-1", latest.Effects)
	}

	first, ok, err := restored.Get(ctx, "checkpoint-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if first.Node != "first" || first.State != "one" || first.Metadata["source"] != "test" {
		t.Fatalf("Get() = %+v, want first checkpoint with metadata", first)
	}
}

func TestBackendReplacesExistingRecordWithoutDuplicatingIndex(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(newFakeClient())
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewRowStore[string](backend)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}

	for _, state := range []string{"old", "new"} {
		err = store.Put(ctx, graph.Checkpoint[string]{
			ID:        "checkpoint-1",
			ThreadID:  "thread-1",
			Step:      1,
			Node:      "node",
			State:     state,
			CreatedAt: time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("Put(%s) error = %v", state, err)
		}
	}

	list, err := store.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() count = %d, want 1", len(list))
	}
	if list[0].State != "new" {
		t.Fatalf("List()[0].State = %q, want new", list[0].State)
	}
}

func TestBackendRetriesThreadIndexCASConflict(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	client.conflictOnce = true
	backend, err := NewBackend(client, WithMaxIndexCASRetries(2))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := checkpoint.NewRowStore[string](backend)
	if err != nil {
		t.Fatalf("NewRowStore() error = %v", err)
	}

	err = store.Put(ctx, graph.Checkpoint[string]{
		ID:        "checkpoint-1",
		ThreadID:  "thread-1",
		Step:      1,
		State:     "one",
		CreatedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	index := client.threadIndex(t, "checkpoint/threads/dGhyZWFkLTE.json")
	if len(index.IDs) != 2 || index.IDs[0] != "other-checkpoint" || index.IDs[1] != "checkpoint-1" {
		t.Fatalf("thread index = %+v, want conflict writer id then checkpoint-1", index)
	}
	list, err := store.List(ctx, "thread-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "checkpoint-1" {
		t.Fatalf("List() = %+v, want checkpoint-1 and skip orphan index id", list)
	}
}

func TestBackendVerifiesThreadIndexConsistency(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	putRecordObject(t, client, backend.recordKey("checkpoint-1"), checkpoint.Record{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
	})
	putRecordObject(t, client, backend.recordKey("other-thread"), checkpoint.Record{
		ID:       "other-thread",
		ThreadID: "thread-2",
	})
	putThreadIndexObject(t, client, backend.threadKey("thread-1"), threadIndex{
		ThreadID: "thread-1",
		IDs:      []string{"checkpoint-1", "checkpoint-1", "missing", "other-thread"},
	})

	report, err := backend.VerifyIndex(ctx, "thread-1")
	if err != nil {
		t.Fatalf("VerifyIndex() error = %v", err)
	}
	if report.Consistent {
		t.Fatalf("VerifyIndex().Consistent = true, want false")
	}
	if !sameStrings(report.IndexedRecordIDs, []string{"checkpoint-1", "checkpoint-1", "missing", "other-thread"}) {
		t.Fatalf("IndexedRecordIDs = %+v, want original index order", report.IndexedRecordIDs)
	}
	if !sameStrings(report.ValidRecordIDs, []string{"checkpoint-1"}) {
		t.Fatalf("ValidRecordIDs = %+v, want checkpoint-1", report.ValidRecordIDs)
	}
	if !sameStrings(report.DuplicateRecordIDs, []string{"checkpoint-1"}) {
		t.Fatalf("DuplicateRecordIDs = %+v, want checkpoint-1", report.DuplicateRecordIDs)
	}
	if !sameStrings(report.MissingRecordIDs, []string{"missing"}) {
		t.Fatalf("MissingRecordIDs = %+v, want missing", report.MissingRecordIDs)
	}
	if !sameStrings(report.WrongThreadRecordIDs, []string{"other-thread"}) {
		t.Fatalf("WrongThreadRecordIDs = %+v, want other-thread", report.WrongThreadRecordIDs)
	}
}

func TestBackendRepairsThreadIndexConsistency(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	putRecordObject(t, client, backend.recordKey("checkpoint-1"), checkpoint.Record{
		ID:       "checkpoint-1",
		ThreadID: "thread-1",
	})
	putRecordObject(t, client, backend.recordKey("checkpoint-2"), checkpoint.Record{
		ID:       "checkpoint-2",
		ThreadID: "thread-1",
	})
	putRecordObject(t, client, backend.recordKey("other-thread"), checkpoint.Record{
		ID:       "other-thread",
		ThreadID: "thread-2",
	})
	putThreadIndexObject(t, client, backend.threadKey("thread-1"), threadIndex{
		ThreadID: "thread-1",
		IDs:      []string{"checkpoint-1", "checkpoint-1", "missing", "other-thread", "checkpoint-2"},
	})

	report, err := backend.RepairIndex(ctx, "thread-1")
	if err != nil {
		t.Fatalf("RepairIndex() error = %v", err)
	}
	if !report.Repaired {
		t.Fatalf("RepairIndex().Repaired = false, want true")
	}
	if !sameStrings(report.ValidRecordIDs, []string{"checkpoint-1", "checkpoint-2"}) {
		t.Fatalf("ValidRecordIDs = %+v, want checkpoint-1 checkpoint-2", report.ValidRecordIDs)
	}

	index := client.threadIndex(t, "tenant-a/checkpoint/threads/dGhyZWFkLTE.json")
	if !sameStrings(index.IDs, []string{"checkpoint-1", "checkpoint-2"}) {
		t.Fatalf("repaired index IDs = %+v, want valid records only", index.IDs)
	}
	verified, err := backend.VerifyIndex(ctx, "thread-1")
	if err != nil {
		t.Fatalf("VerifyIndex(after repair) error = %v", err)
	}
	if !verified.Consistent {
		t.Fatalf("VerifyIndex(after repair) = %+v, want consistent", verified)
	}
}

func TestBackendReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(newFakeClient())
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	record, ok, err := backend.GetRecord(ctx, "missing")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if ok || record.ID != "" {
		t.Fatalf("GetRecord() = %+v, %v; want zero false", record, ok)
	}
	records, err := backend.ListRecords(ctx, "thread-1")
	if err != nil {
		t.Fatalf("ListRecords() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("ListRecords() count = %d, want 0", len(records))
	}
}

func TestBackendSupportsProviderErrorMatchers(t *testing.T) {
	ctx := context.Background()
	providerNotFound := errors.New("provider: not found")
	providerConflict := errors.New("provider: precondition failed")
	client := newFakeClient()
	client.notFoundErr = providerNotFound
	client.preconditionErr = providerConflict
	backend, err := NewBackend(
		client,
		WithNotFound(func(err error) bool { return errors.Is(err, providerNotFound) }),
		WithPreconditionFailed(func(err error) bool { return errors.Is(err, providerConflict) }),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	record, ok, err := backend.GetRecord(ctx, "missing")
	if err != nil || ok || record.ID != "" {
		t.Fatalf("GetRecord(missing) = %+v, %v, %v; want zero false nil", record, ok, err)
	}
}

func TestNewBackendRejectsInvalidInputs(t *testing.T) {
	if backend, err := NewBackend(nil); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
	if backend, err := NewBackend(newFakeClient(), WithNotFound(nil)); !errors.Is(err, ErrNotFoundMatcherRequired) || backend != nil {
		t.Fatalf("NewBackend(nil matcher) backend=%v err=%v, want ErrNotFoundMatcherRequired", backend, err)
	}
	if backend, err := NewBackend(newFakeClient(), WithPreconditionFailed(nil)); !errors.Is(err, ErrPreconditionFailedMatcherRequired) || backend != nil {
		t.Fatalf("NewBackend(nil precondition matcher) backend=%v err=%v, want ErrPreconditionFailedMatcherRequired", backend, err)
	}
	if backend, err := NewBackend(newFakeClient(), WithMaxIndexCASRetries(0)); !errors.Is(err, ErrIndexCASRetriesRequired) || backend != nil {
		t.Fatalf("NewBackend(zero retries) backend=%v err=%v, want ErrIndexCASRetriesRequired", backend, err)
	}
}

type fakeClient struct {
	mu              sync.Mutex
	objects         map[string]Object
	nextVersion     int
	notFoundErr     error
	preconditionErr error
	conflictOnce    bool
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		objects:         make(map[string]Object),
		nextVersion:     1,
		notFoundErr:     ErrNotFound,
		preconditionErr: ErrPreconditionFailed,
	}
}

func (c *fakeClient) GetObject(ctx context.Context, key string) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	object, ok := c.objects[key]
	if !ok {
		return Object{}, c.notFoundErr
	}
	return copyObject(object), nil
}

func (c *fakeClient) PutObject(ctx context.Context, object Object, precondition Precondition) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists := c.objects[object.Key]
	if precondition.IfAbsent && exists {
		return Object{}, c.preconditionErr
	}
	if precondition.IfVersion != "" && (!exists || current.Version != precondition.IfVersion) {
		return Object{}, c.preconditionErr
	}
	if c.conflictOnce && isThreadIndexKey(object.Key) && (precondition.IfAbsent || precondition.IfVersion != "") {
		c.conflictOnce = false
		c.injectIndexConflictLocked(object.Key)
		return Object{}, c.preconditionErr
	}
	object = copyObject(object)
	object.Version = fmt.Sprintf("v%d", c.nextVersion)
	object.UpdatedAt = time.Unix(int64(c.nextVersion), 0).UTC()
	c.nextVersion++
	c.objects[object.Key] = object
	return copyObject(object), nil
}

func (c *fakeClient) injectIndexConflictLocked(key string) {
	index := threadIndex{ThreadID: "thread-1", IDs: []string{"other-checkpoint"}}
	raw, _ := json.Marshal(index)
	object := Object{
		Key:       key,
		Data:      raw,
		Version:   fmt.Sprintf("v%d", c.nextVersion),
		UpdatedAt: time.Unix(int64(c.nextVersion), 0).UTC(),
	}
	c.nextVersion++
	c.objects[key] = object
}

func (c *fakeClient) threadIndex(t *testing.T, key string) threadIndex {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()

	object, ok := c.objects[key]
	if !ok {
		t.Fatalf("thread index object %q not found; keys=%+v", key, c.keysLocked())
	}
	var index threadIndex
	if err := json.Unmarshal(object.Data, &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	return index
}

func putRecordObject(t *testing.T, client *fakeClient, key string, record checkpoint.Record) {
	t.Helper()
	raw, err := encode(record)
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	if _, err := client.PutObject(context.Background(), Object{Key: key, Data: raw}, Precondition{}); err != nil {
		t.Fatalf("put record object: %v", err)
	}
}

func putThreadIndexObject(t *testing.T, client *fakeClient, key string, index threadIndex) {
	t.Helper()
	raw, err := encode(index)
	if err != nil {
		t.Fatalf("encode thread index: %v", err)
	}
	if _, err := client.PutObject(context.Background(), Object{Key: key, Data: raw}, Precondition{}); err != nil {
		t.Fatalf("put thread index object: %v", err)
	}
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (c *fakeClient) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keysLocked()
}

func (c *fakeClient) keysLocked() []string {
	out := make([]string, 0, len(c.objects))
	for key := range c.objects {
		out = append(out, key)
	}
	return out
}

func copyObject(object Object) Object {
	object.Data = append([]byte(nil), object.Data...)
	object.Metadata = copyStringMap(object.Metadata)
	return object
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
