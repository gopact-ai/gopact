package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBackendAcquiresRenewsAndReleasesLease(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(500, 0).UTC())
	client := newFakeLeaseRedisClient()
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
	if _, ok := client.raw("tenant-a:turns/main"); !ok {
		t.Fatalf("redis key tenant-a:turns/main missing after acquire")
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
		newFakeLeaseRedisClient(),
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

func TestBackendRejectsRenewAndReleaseWhenNotHeld(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(700, 0).UTC())
	backend, err := NewBackend(
		newFakeLeaseRedisClient(),
		WithClock(clock.Now),
		WithTokenGenerator(sequenceTokens("token-renew-missing", "token-1", "token-renew-wrong").Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	if _, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		Token: "missing",
		TTL:   time.Second,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(missing) error = %v, want ErrLeaseNotHeld", err)
	}
	lease, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if _, err := backend.RenewLease(ctx, gopact.LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: "wrong-token",
		TTL:   time.Second,
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(wrong token) error = %v, want ErrLeaseNotHeld", err)
	}
	if err := backend.ReleaseLease(ctx, gopact.LeaseReleaseRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: "wrong-token",
	}); !errors.Is(err, gopact.ErrLeaseNotHeld) {
		t.Fatalf("ReleaseLease(wrong token) error = %v, want ErrLeaseNotHeld", err)
	}
}

func TestBackendGetLeaseHandlesProviderNilAndExpiry(t *testing.T) {
	ctx := context.Background()
	providerNil := errors.New("redis nil")
	client := newFakeLeaseRedisClient()
	client.notFoundErr = providerNil
	clock := newFakeClock(time.Unix(800, 0).UTC())
	backend, err := NewBackend(
		client,
		WithNotFound(func(err error) bool {
			return errors.Is(err, providerNil)
		}),
		WithClock(clock.Now),
		WithTokenGenerator(sequenceTokens("token-1").Next),
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}

	if _, ok, err := backend.GetLease(ctx, "missing"); err != nil || ok {
		t.Fatalf("GetLease(missing) ok=%v err=%v, want no lease", ok, err)
	}
	lease, err := backend.AcquireLease(ctx, gopact.LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	clock.Advance(6 * time.Second)
	got, ok, err := backend.GetLease(ctx, lease.Key)
	if err != nil {
		t.Fatalf("GetLease(expired) error = %v", err)
	}
	if ok || got.Key != "" {
		t.Fatalf("GetLease(expired) ok=%v lease=%+v, want no lease", ok, got)
	}
}

func TestBackendComposesWithLeasedTurnLoopStore(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(900, 0).UTC())
	leases, err := NewBackend(
		newFakeLeaseRedisClient(),
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
	client := newFakeLeaseRedisClient()
	if backend, err := NewBackend(client, WithNotFound(nil)); !errors.Is(err, ErrNotFoundMatcherRequired) || backend != nil {
		t.Fatalf("NewBackend(WithNotFound(nil)) backend=%v err=%v, want ErrNotFoundMatcherRequired", backend, err)
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

type fakeLeaseRedisClient struct {
	mu          sync.Mutex
	values      map[string][]byte
	notFoundErr error
}

func newFakeLeaseRedisClient() *fakeLeaseRedisClient {
	return &fakeLeaseRedisClient{
		values:      make(map[string][]byte),
		notFoundErr: ErrNil,
	}
}

func (c *fakeLeaseRedisClient) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.values[key]
	if !ok {
		return nil, c.notFoundErr
	}
	return append([]byte(nil), raw...), nil
}

func (c *fakeLeaseRedisClient) Eval(ctx context.Context, script string, keys []string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(keys) != 1 {
		return "", fmt.Errorf("got %d keys, want 1", len(keys))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	switch script {
	case acquireScript:
		return c.evalAcquire(keys[0], args)
	case renewScript:
		return c.evalRenew(keys[0], args)
	case releaseScript:
		return c.evalRelease(keys[0], args)
	default:
		return "", fmt.Errorf("unexpected script")
	}
}

func (c *fakeLeaseRedisClient) raw(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.values[key]
	return append([]byte(nil), raw...), ok
}

func (c *fakeLeaseRedisClient) evalAcquire(key string, args []string) (string, error) {
	if len(args) != 2 {
		return "", fmt.Errorf("acquire got %d args, want 2", len(args))
	}
	now, err := parseRedisTime(args[0])
	if err != nil {
		return "", err
	}
	if currentRaw, ok := c.values[key]; ok {
		current, err := decodeLeaseRecord(currentRaw)
		if err != nil {
			return "", err
		}
		if now.Before(current.ExpiresAt) {
			return redisConflictResult, nil
		}
	}
	c.values[key] = []byte(args[1])
	return args[1], nil
}

func (c *fakeLeaseRedisClient) evalRenew(key string, args []string) (string, error) {
	if len(args) != 6 {
		return "", fmt.Errorf("renew got %d args, want 6", len(args))
	}
	currentRaw, ok := c.values[key]
	if !ok {
		return redisLeaseNotHeldResult, nil
	}
	current, err := decodeLeaseRecord(currentRaw)
	if err != nil {
		return "", err
	}
	now, err := parseRedisTime(args[3])
	if err != nil {
		return "", err
	}
	if !now.Before(current.ExpiresAt) || current.Owner != args[0] || current.Token != args[1] {
		return redisLeaseNotHeldResult, nil
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, args[4])
	if err != nil {
		return "", err
	}
	current.Token = args[2]
	current.ExpiresAt = expiresAt
	raw, err := encodeLeaseDocument(current)
	if err != nil {
		return "", err
	}
	c.values[key] = raw
	return string(raw), nil
}

func (c *fakeLeaseRedisClient) evalRelease(key string, args []string) (string, error) {
	if len(args) != 3 {
		return "", fmt.Errorf("release got %d args, want 3", len(args))
	}
	currentRaw, ok := c.values[key]
	if !ok {
		return redisLeaseNotHeldResult, nil
	}
	current, err := decodeLeaseRecord(currentRaw)
	if err != nil {
		return "", err
	}
	now, err := parseRedisTime(args[2])
	if err != nil {
		return "", err
	}
	if !now.Before(current.ExpiresAt) || current.Owner != args[0] || current.Token != args[1] {
		return redisLeaseNotHeldResult, nil
	}
	delete(c.values, key)
	return redisLeaseReleasedResult, nil
}

func decodeLeaseRecord(raw []byte) (gopact.LeaseRecord, error) {
	var record gopact.LeaseRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return gopact.LeaseRecord{}, err
	}
	return record, nil
}

func parseRedisTime(value string) (time.Time, error) {
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nanos).UTC(), nil
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
