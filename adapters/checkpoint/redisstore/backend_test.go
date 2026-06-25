package redisstore

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

func TestBackendPersistsRecordsWithRedisCommands(t *testing.T) {
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
	if client.evalCalls != 2 {
		t.Fatalf("evalCalls = %d, want 2 atomic upserts", client.evalCalls)
	}
	if _, ok := client.values["tenant-a:checkpoint:record:Y2hlY2twb2ludC0x"]; !ok {
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

func TestBackendSupportsCustomNotFoundMatcher(t *testing.T) {
	ctx := context.Background()
	providerNotFound := errors.New("provider: redis nil")
	client := newFakeClient()
	client.notFoundErr = providerNotFound
	backend, err := NewBackend(client, WithNotFound(func(err error) bool {
		return errors.Is(err, providerNotFound)
	}))
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
}

type fakeClient struct {
	mu          sync.Mutex
	values      map[string][]byte
	evalCalls   int
	notFoundErr error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		values: make(map[string][]byte),
	}
}

func (c *fakeClient) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	value, ok := c.values[key]
	if !ok {
		if c.notFoundErr != nil {
			return nil, c.notFoundErr
		}
		return nil, ErrNil
	}
	return append([]byte(nil), value...), nil
}

func (c *fakeClient) Eval(ctx context.Context, _ string, keys []string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(keys) != 2 || len(args) != 3 {
		return "", fmt.Errorf("got keys=%d args=%d, want 2 keys and 3 args", len(keys), len(args))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evalCalls++
	recordKey := keys[0]
	threadKey := keys[1]
	threadID := args[0]
	recordID := args[1]
	rawRecord := []byte(args[2])

	var index []string
	if rawIndex, ok := c.values[threadKey]; ok {
		if err := json.Unmarshal(rawIndex, &index); err != nil {
			return "", err
		}
	}
	found := false
	for _, id := range index {
		if id == recordID {
			found = true
			break
		}
	}
	if !found {
		index = append(index, recordID)
	}
	rawIndex, err := json.Marshal(index)
	if err != nil {
		return "", err
	}
	c.values[recordKey] = append([]byte(nil), rawRecord...)
	c.values[threadKey] = rawIndex
	return threadID, nil
}

func (c *fakeClient) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]string, 0, len(c.values))
	for key := range c.values {
		out = append(out, key)
	}
	return out
}
