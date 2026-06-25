package objectstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBackendPersistsTurnLoopStateWithObjectCAS(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client, WithPrefix("tenant-a"))
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
	if _, ok := client.objects["tenant-a/turnloop/versioned/dHVybnMvbWFpbg.json"]; !ok {
		t.Fatalf("client keys = %+v, want encoded versioned key with prefix", client.keys())
	}

	restoredBackend, err := NewBackend(client, WithPrefix("tenant-a"))
	if err != nil {
		t.Fatalf("NewBackend(restored) error = %v", err)
	}
	restored, err := gopact.NewVersionedTurnLoopStore(restoredBackend, "turns/main")
	if err != nil {
		t.Fatalf("NewVersionedTurnLoopStore(restored) error = %v", err)
	}
	got, ok, err := restored.Load(ctx)
	if err != nil {
		t.Fatalf("Load(restored) error = %v", err)
	}
	if !ok {
		t.Fatal("Load(restored) ok = false, want true")
	}
	if got.InputSeq != 2 || len(got.Pending) != 1 || got.Pending[0].Input != "queued" {
		t.Fatalf("Load(restored) = %+v, want queued pending state", got)
	}
	if len(got.PendingEvents) != 1 || got.PendingEvents[0].Type != gopact.EventTurnInputReceived {
		t.Fatalf("Load(restored).PendingEvents = %+v, want turn_input_received", got.PendingEvents)
	}
	if got.Interrupted == nil || got.Interrupted.Input != "question" {
		t.Fatalf("Load(restored).Interrupted = %+v, want question", got.Interrupted)
	}
}

func TestBackendDetectsStaleSaveAcrossStores(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()
	backend, err := NewBackend(client)
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

	record, ok, err := backend.GetTurnLoopVersionedState(ctx, "missing")
	if err != nil {
		t.Fatalf("GetTurnLoopVersionedState() error = %v", err)
	}
	if ok || record.Key != "" || record.Version != "" {
		t.Fatalf("GetTurnLoopVersionedState() = %+v, %v; want zero false", record, ok)
	}
}

func TestBackendSupportsProviderErrorMatchers(t *testing.T) {
	ctx := context.Background()
	providerNotFound := errors.New("provider: object missing")
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
	record, ok, err := backend.GetTurnLoopVersionedState(ctx, "missing")
	if err != nil || ok || record.Key != "" {
		t.Fatalf("GetTurnLoopVersionedState(missing) = %+v, %v, %v; want zero false nil", record, ok, err)
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
	if backend, err := NewBackend(newFakeClient(), WithPrefix("../bad")); !errors.Is(err, ErrUnsafePrefix) || backend != nil {
		t.Fatalf("NewBackend(unsafe prefix) backend=%v err=%v, want ErrUnsafePrefix", backend, err)
	}
}

type fakeClient struct {
	mu              sync.Mutex
	objects         map[string]Object
	nextVersion     int
	notFoundErr     error
	preconditionErr error
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
	object = copyObject(object)
	object.Version = fmt.Sprintf("v%d", c.nextVersion)
	object.UpdatedAt = time.Unix(int64(c.nextVersion), 0).UTC()
	c.nextVersion++
	c.objects[object.Key] = object
	return copyObject(object), nil
}

func (c *fakeClient) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

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
