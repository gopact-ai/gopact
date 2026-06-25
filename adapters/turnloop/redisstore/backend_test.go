package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestBackendPersistsTurnLoopStateWithRedisCommands(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := gopact.NewRowTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore() error = %v", err)
	}
	state := gopact.TurnLoopState{
		Pending: []gopact.TurnInputRecord{
			{
				ID:    "turn-input:1",
				Kind:  gopact.TurnInputUser,
				Input: "queued",
				IDs:   gopact.RuntimeIDs{ThreadID: "thread-1", RunID: "run-1"},
			},
		},
		PendingEvents: []gopact.Event{
			{Type: gopact.EventTurnInputReceived, IDs: gopact.RuntimeIDs{ThreadID: "thread-1"}},
		},
		Interrupted: &gopact.TurnInputRecord{
			ID:    "turn-input:2",
			Kind:  gopact.TurnInputUser,
			Input: "question",
		},
		InputSeq: 2,
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, ok := client.values["tenant-a:turns/main"]; !ok {
		t.Fatalf("client keys = %+v, want tenant-a:turns/main", client.keys())
	}

	restoredBackend, err := NewBackend(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restoredStore, err := gopact.NewRowTurnLoopStore(restoredBackend, "turns/main")
	if err != nil {
		t.Fatalf("NewRowTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restoredStore.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if got.InputSeq != 2 || len(got.Pending) != 1 || got.Pending[0].Input != "queued" {
		t.Fatalf("Load() = %+v, want queued pending state", got)
	}
	if len(got.PendingEvents) != 1 || got.PendingEvents[0].Type != gopact.EventTurnInputReceived {
		t.Fatalf("Load().PendingEvents = %+v, want turn_input_received", got.PendingEvents)
	}
	if got.Interrupted == nil || got.Interrupted.Input != "question" {
		t.Fatalf("Load().Interrupted = %+v, want question", got.Interrupted)
	}
}

func TestVersionedBackendPersistsTurnLoopStateWithRedisCAS(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithVersionGenerator(sequenceVersions("v1", "v2")))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	store, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore() error = %v", err)
	}
	if _, ok, err := store.Load(ctx); err != nil || ok {
		t.Fatalf("Load(empty) ok=%v err=%v, want empty store", ok, err)
	}
	if err := store.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := store.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}
	if client.evalCalls != 2 {
		t.Fatalf("evalCalls = %d, want 2 CAS script calls", client.evalCalls)
	}

	restored, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restored.Load(ctx)
	if err != nil {
		t.Fatalf("Load(restored) error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("Load(restored) ok=%v state=%+v, want latest state", ok, got)
	}
}

func TestVersionedBackendDetectsStaleSaveAcrossStores(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithVersionGenerator(sequenceVersions("v1", "v2")))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	first, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(first) error = %v", err)
	}
	second, err := gopact.NewVersionedTurnLoopStore(backend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(second) error = %v", err)
	}
	if _, ok, err := first.Load(ctx); err != nil || ok {
		t.Fatalf("first Load() ok=%v err=%v, want empty store", ok, err)
	}
	if _, ok, err := second.Load(ctx); err != nil || ok {
		t.Fatalf("second Load() ok=%v err=%v, want empty store", ok, err)
	}
	if err := first.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := second.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); !errors.Is(err, gopact.ErrTurnLoopStoreConflict) {
		t.Fatalf("second Save() error = %v, want ErrTurnLoopStoreConflict", err)
	}
}

func TestBackendReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	backend, err := NewBackend(newFakeClient())
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	row, ok, err := backend.GetTurnLoopState(ctx, "missing")
	if err != nil {
		t.Fatalf("GetTurnLoopState() error = %v", err)
	}
	if ok || row.Key != "" {
		t.Fatalf("GetTurnLoopState() = %+v, %v; want zero false", row, ok)
	}
	versioned, ok, err := backend.GetTurnLoopVersionedState(ctx, "missing")
	if err != nil {
		t.Fatalf("GetTurnLoopVersionedState() error = %v", err)
	}
	if ok || versioned.Key != "" || versioned.Version != "" {
		t.Fatalf("GetTurnLoopVersionedState() = %+v, %v; want zero false", versioned, ok)
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

	row, ok, err := backend.GetTurnLoopState(ctx, "missing")
	if err != nil || ok || row.Key != "" {
		t.Fatalf("GetTurnLoopState(missing) = %+v, %v, %v; want zero false nil", row, ok, err)
	}
}

func TestNewBackendRejectsInvalidInputs(t *testing.T) {
	if backend, err := NewBackend(nil); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
	if backend, err := NewBackend(newFakeClient(), WithNotFound(nil)); !errors.Is(err, ErrNotFoundMatcherRequired) || backend != nil {
		t.Fatalf("NewBackend(nil matcher) backend=%v err=%v, want ErrNotFoundMatcherRequired", backend, err)
	}
	if backend, err := NewBackend(newFakeClient(), WithVersionGenerator(nil)); !errors.Is(err, ErrVersionGeneratorRequired) || backend != nil {
		t.Fatalf("NewBackend(nil generator) backend=%v err=%v, want ErrVersionGeneratorRequired", backend, err)
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

func (c *fakeClient) Set(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.values[key] = append([]byte(nil), value...)
	return nil
}

func (c *fakeClient) Eval(ctx context.Context, _ string, keys []string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(keys) != 1 || len(args) != 3 {
		return "", fmt.Errorf("got keys=%d args=%d, want 1 key and 3 args", len(keys), len(args))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evalCalls++
	key := keys[0]
	expectedVersion := args[0]
	raw := []byte(args[1])
	nextVersion := args[2]
	currentRaw, ok := c.values[key]
	if !ok {
		if expectedVersion != "" {
			return redisConflictResult, nil
		}
	} else {
		var current gopact.TurnLoopVersionedRecord
		if err := json.Unmarshal(currentRaw, &current); err != nil {
			return "", err
		}
		if current.Version != expectedVersion {
			return redisConflictResult, nil
		}
	}

	var next gopact.TurnLoopVersionedRecord
	if err := json.Unmarshal(raw, &next); err != nil {
		return "", err
	}
	if next.Version != nextVersion {
		return "", fmt.Errorf("record version = %q, want %q", next.Version, nextVersion)
	}
	c.values[key] = append([]byte(nil), raw...)
	return nextVersion, nil
}

func (c *fakeClient) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, 0, len(c.values))
	for key := range c.values {
		keys = append(keys, key)
	}
	return keys
}

func sequenceVersions(values ...string) func() (string, error) {
	i := 0
	return func() (string, error) {
		if i >= len(values) {
			return "", errors.New("no version left")
		}
		value := values[i]
		i++
		return value, nil
	}
}
