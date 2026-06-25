package objectstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBackendAcquiresRenewsAndReleasesLeaseWithObjectCAS(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(500, 0).UTC())
	client := newFakeObjectClient()
	backend, err := NewBackend(
		client,
		WithPrefix("tenant-a"),
		WithClock(clock.Now),
		WithTokenGenerator(sequenceTokens("token-1", "token-conflict", "token-2", "token-stale").Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	lease, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:      "turns/main",
		Owner:    "worker-a",
		TTL:      10 * time.Second,
		Metadata: map[string]any{"thread_id": "thread-1"},
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if lease.Token != "token-1" || lease.Owner != "worker-a" ||
		!lease.AcquiredAt.Equal(clock.Now()) || !lease.ExpiresAt.Equal(clock.Now().Add(10*time.Second)) {
		t.Fatalf("AcquireLease() = %+v, want worker-a lease", lease)
	}
	if keys := client.keys(); len(keys) != 1 || !strings.HasPrefix(keys[0], "tenant-a/leases/") {
		t.Fatalf("stored keys = %v, want tenant-a/leases prefix", keys)
	}
	if _, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   lease.Key,
		Owner: "worker-b",
		TTL:   10 * time.Second,
	}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("AcquireLease(conflict) error = %v, want ErrLeaseConflict", err)
	}

	clock.Advance(2 * time.Second)
	renewed, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   20 * time.Second,
	})
	if err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if renewed.Token != "token-2" || !renewed.AcquiredAt.Equal(lease.AcquiredAt) ||
		!renewed.ExpiresAt.Equal(clock.Now().Add(20*time.Second)) {
		t.Fatalf("RenewLease() = %+v, want rotated token and new ttl", renewed)
	}
	if _, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   20 * time.Second,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(stale token) error = %v, want ErrLeaseNotHeld", err)
	}

	got, ok, err := backend.GetLease(ctx, lease.Key)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if !ok || got.Metadata["thread_id"] != "thread-1" {
		t.Fatalf("GetLease() ok=%v lease=%+v, want current metadata", ok, got)
	}

	if err := backend.ReleaseLease(ctx, gopact.LeaseReleaseRequest{
		Key:   renewed.Key,
		Owner: "worker-b",
		Token: renewed.Token,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("ReleaseLease(wrong owner) error = %v, want ErrLeaseNotHeld", err)
	}
	if err := backend.ReleaseLease(ctx, gopact.LeaseReleaseRequest{
		Key:   renewed.Key,
		Owner: renewed.Owner,
		Token: renewed.Token,
	}); err != nil {
		t.Fatalf("ReleaseLease() error = %v", err)
	}
	if _, ok, err := backend.GetLease(ctx, lease.Key); err != nil || ok {
		t.Fatalf("GetLease(after release) ok=%v err=%v, want no lease", ok, err)
	}
}

func TestBackendTransfersExpiredLease(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(600, 0).UTC())
	backend, err := NewBackend(
		newFakeObjectClient(),
		WithClock(clock.Now),
		WithTokenGenerator(sequenceTokens("token-1", "token-2").Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	first, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease(first) error = %v", err)
	}

	clock.Advance(11 * time.Second)
	second, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease(second) error = %v", err)
	}
	if second.Owner != "worker-b" || second.Token != "token-2" || second.Token == first.Token {
		t.Fatalf("AcquireLease(second) = %+v, want transferred worker-b lease", second)
	}
}

func TestBackendMapsObjectRacesToLeaseErrors(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(700, 0).UTC())
	client := newFakeObjectClient()
	client.failNextPut = true
	backend, err := NewBackend(
		client,
		WithClock(clock.Now),
		WithTokenGenerator(sequenceTokens("token-1", "token-2", "token-3").Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	if _, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Second,
	}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("AcquireLease(precondition race) error = %v, want ErrLeaseConflict", err)
	}

	lease, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	client.failNextPut = true
	if _, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   time.Second,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(precondition race) error = %v, want ErrLeaseNotHeld", err)
	}
	client.failNextDelete = true
	if err := backend.ReleaseLease(ctx, gopact.LeaseReleaseRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("ReleaseLease(precondition race) error = %v, want ErrLeaseNotHeld", err)
	}
}

func TestBackendSupportsProviderErrorMatchers(t *testing.T) {
	ctx := context.Background()
	providerNotFound := errors.New("provider: missing")
	providerPreconditionFailed := errors.New("provider: precondition failed")
	client := newFakeObjectClient()
	client.notFoundErr = providerNotFound
	client.preconditionErr = providerPreconditionFailed
	client.failNextPut = true

	backend, err := NewBackend(
		client,
		WithNotFound(func(err error) bool {
			return errors.Is(err, providerNotFound)
		}),
		WithPreconditionFailed(func(err error) bool {
			return errors.Is(err, providerPreconditionFailed)
		}),
		WithClock(newFakeClock(time.Unix(800, 0).UTC()).Now),
		WithTokenGenerator(sequenceTokens("token-1").Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if _, ok, err := backend.GetLease(ctx, "missing"); err != nil || ok {
		t.Fatalf("GetLease(missing) ok=%v err=%v, want no lease", ok, err)
	}
	if _, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Second,
	}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("AcquireLease(custom precondition) error = %v, want ErrLeaseConflict", err)
	}
}

func TestBackendComposesWithLeasedTurnLoopStore(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(900, 0).UTC())
	leases, err := NewBackend(
		newFakeObjectClient(),
		WithClock(clock.Now),
		WithTokenGenerator(sequenceTokens(
			"token-1",
			"token-conflict",
			"token-first-renew",
			"token-second-conflict",
			"token-2",
			"token-first-stale",
		).Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	base := gopact.NewMemoryTurnLoopStore()
	first, err := gopact.NewLeasedTurnLoopStore(base, leases, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(first) error = %v", err)
	}
	second, err := gopact.NewLeasedTurnLoopStore(base, leases, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(second) error = %v", err)
	}

	if err := first.Save(ctx, gopact.TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := second.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("second Save(conflict) error = %v, want ErrLeaseConflict", err)
	}
	clock.Advance(11 * time.Second)
	if err := second.Save(ctx, gopact.TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("second Save(after expiry) error = %v", err)
	}
	if err := first.Save(ctx, gopact.TurnLoopState{InputSeq: 3}); !errors.Is(err, gopact.ErrLeaseConflict) {
		t.Fatalf("first Save(stale) error = %v, want ErrLeaseConflict", err)
	}
	got, ok, err := base.Load(ctx)
	if err != nil {
		t.Fatalf("base Load() error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("base Load() ok=%v state=%+v, want worker-b state", ok, got)
	}
}

func TestNewBackendRejectsInvalidInputs(t *testing.T) {
	if backend, err := NewBackend(nil); !errors.Is(err, ErrClientRequired) || backend != nil {
		t.Fatalf("NewBackend(nil) backend=%v err=%v, want ErrClientRequired", backend, err)
	}
	client := newFakeObjectClient()
	if backend, err := NewBackend(client, WithPrefix("../escape")); !errors.Is(err, ErrUnsafePrefix) || backend != nil {
		t.Fatalf("NewBackend(unsafe prefix) backend=%v err=%v, want ErrUnsafePrefix", backend, err)
	}
	if backend, err := NewBackend(client, WithNotFound(nil)); !errors.Is(err, ErrNotFoundMatcherRequired) || backend != nil {
		t.Fatalf("NewBackend(WithNotFound(nil)) backend=%v err=%v, want ErrNotFoundMatcherRequired", backend, err)
	}
	if backend, err := NewBackend(client, WithPreconditionFailed(nil)); !errors.Is(err, ErrPreconditionFailedMatcherRequired) || backend != nil {
		t.Fatalf("NewBackend(WithPreconditionFailed(nil)) backend=%v err=%v, want ErrPreconditionFailedMatcherRequired", backend, err)
	}
	if backend, err := NewBackend(client, WithTokenGenerator(nil)); !errors.Is(err, ErrTokenGeneratorRequired) || backend != nil {
		t.Fatalf("NewBackend(WithTokenGenerator(nil)) backend=%v err=%v, want ErrTokenGeneratorRequired", backend, err)
	}

	backend, err := NewBackend(client, WithTokenGenerator(sequenceTokens("token-1").Next))
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if _, err := backend.AcquireLease(context.Background(), gopact.LeaseRequest{}); !errors.Is(err, gopact.ErrLeaseKeyRequired) {
		t.Fatalf("AcquireLease(empty) error = %v, want ErrLeaseKeyRequired", err)
	}
	if _, err := backend.RenewLease(context.Background(), gopact.LeaseRenewRequest{Key: "k", Owner: "o"}); !errors.Is(err, gopact.ErrLeaseTokenRequired) {
		t.Fatalf("RenewLease(empty token) error = %v, want ErrLeaseTokenRequired", err)
	}
	if err := backend.ReleaseLease(context.Background(), gopact.LeaseReleaseRequest{Key: "k", Owner: "o"}); !errors.Is(err, gopact.ErrLeaseTokenRequired) {
		t.Fatalf("ReleaseLease(empty token) error = %v, want ErrLeaseTokenRequired", err)
	}
	if _, ok, err := backend.GetLease(context.Background(), ""); !errors.Is(err, gopact.ErrLeaseKeyRequired) || ok {
		t.Fatalf("GetLease(empty) ok=%v err=%v, want ErrLeaseKeyRequired", ok, err)
	}
}

type fakeObjectClient struct {
	mu              sync.Mutex
	objects         map[string]Object
	versionSeq      int
	failNextPut     bool
	failNextDelete  bool
	notFoundErr     error
	preconditionErr error
}

func newFakeObjectClient() *fakeObjectClient {
	return &fakeObjectClient{
		objects:         make(map[string]Object),
		notFoundErr:     ErrNotFound,
		preconditionErr: ErrPreconditionFailed,
	}
}

func (c *fakeObjectClient) GetObject(ctx context.Context, key string) (Object, error) {
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

func (c *fakeObjectClient) PutObject(ctx context.Context, object Object, precondition Precondition) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNextPut {
		c.failNextPut = false
		return Object{}, c.preconditionErr
	}
	current, ok := c.objects[object.Key]
	switch {
	case precondition.IfAbsent:
		if ok {
			return Object{}, c.preconditionErr
		}
	case precondition.IfVersion != "":
		if !ok || current.Version != precondition.IfVersion {
			return Object{}, c.preconditionErr
		}
	default:
		return Object{}, errors.New("missing precondition")
	}
	c.versionSeq++
	object = copyObject(object)
	object.Version = fmt.Sprintf("v%d", c.versionSeq)
	object.UpdatedAt = time.Now().UTC()
	c.objects[object.Key] = object
	return copyObject(object), nil
}

func (c *fakeObjectClient) DeleteObject(ctx context.Context, key string, precondition Precondition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNextDelete {
		c.failNextDelete = false
		return c.preconditionErr
	}
	current, ok := c.objects[key]
	if !ok {
		return c.notFoundErr
	}
	if precondition.IfVersion == "" || current.Version != precondition.IfVersion {
		return c.preconditionErr
	}
	delete(c.objects, key)
	return nil
}

func (c *fakeObjectClient) keys() []string {
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
	object.Metadata = copyMetadata(object.Metadata)
	return object
}

type tokenSequence struct {
	mu     sync.Mutex
	tokens []string
}

func sequenceTokens(tokens ...string) *tokenSequence {
	return &tokenSequence{tokens: append([]string(nil), tokens...)}
}

func (s *tokenSequence) Next() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tokens) == 0 {
		return "", errors.New("token sequence exhausted")
	}
	token := s.tokens[0]
	s.tokens = s.tokens[1:]
	return token, nil
}

type fakeClock struct {
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}
