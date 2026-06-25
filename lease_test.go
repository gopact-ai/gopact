package gopact

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryLeaseBackendTransfersExpiredLease(t *testing.T) {
	ctx := context.Background()
	clock := newFakeLeaseClock(time.Unix(100, 0).UTC())
	backend := NewMemoryLeaseBackend(WithMemoryLeaseClock(clock.Now))

	first, err := backend.AcquireLease(ctx, LeaseRequest{
		Key:   "turns/thread-1",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease(first) error = %v", err)
	}
	if first.Owner != "worker-a" || first.Token == "" || first.ExpiresAt != clock.Now().Add(10*time.Second) {
		t.Fatalf("AcquireLease(first) = %+v, want worker-a lease with ttl", first)
	}

	if _, err := backend.AcquireLease(ctx, LeaseRequest{
		Key:   "turns/thread-1",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	}); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("AcquireLease(conflict) error = %v, want ErrLeaseConflict", err)
	}

	clock.Advance(11 * time.Second)
	second, err := backend.AcquireLease(ctx, LeaseRequest{
		Key:   "turns/thread-1",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease(after expiry) error = %v", err)
	}
	if second.Owner != "worker-b" || second.Token == "" || second.Token == first.Token {
		t.Fatalf("AcquireLease(after expiry) = %+v, want transferred worker-b lease", second)
	}
}

func TestMemoryLeaseBackendRenewsOnlyCurrentHolder(t *testing.T) {
	ctx := context.Background()
	clock := newFakeLeaseClock(time.Unix(200, 0).UTC())
	backend := NewMemoryLeaseBackend(WithMemoryLeaseClock(clock.Now))
	lease, err := backend.AcquireLease(ctx, LeaseRequest{
		Key:   "turns/thread-1",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}

	if _, err := backend.RenewLease(ctx, LeaseRenewRequest{
		Key:   lease.Key,
		Owner: "worker-b",
		Token: lease.Token,
		TTL:   10 * time.Second,
	}); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(wrong owner) error = %v, want ErrLeaseNotHeld", err)
	}

	clock.Advance(4 * time.Second)
	renewed, err := backend.RenewLease(ctx, LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   20 * time.Second,
	})
	if err != nil {
		t.Fatalf("RenewLease(current holder) error = %v", err)
	}
	if renewed.Token == lease.Token {
		t.Fatalf("RenewLease() token = %q, want rotated token", renewed.Token)
	}
	if renewed.ExpiresAt != clock.Now().Add(20*time.Second) {
		t.Fatalf("RenewLease().ExpiresAt = %v, want %v", renewed.ExpiresAt, clock.Now().Add(20*time.Second))
	}

	if _, err := backend.RenewLease(ctx, LeaseRenewRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
		TTL:   20 * time.Second,
	}); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("RenewLease(stale token) error = %v, want ErrLeaseNotHeld", err)
	}
}

func TestMemoryLeaseBackendReleasesOnlyCurrentHolder(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryLeaseBackend()
	lease, err := backend.AcquireLease(ctx, LeaseRequest{
		Key:   "turns/thread-1",
		Owner: "worker-a",
		TTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}

	if err := backend.ReleaseLease(ctx, LeaseReleaseRequest{
		Key:   lease.Key,
		Owner: "worker-b",
		Token: lease.Token,
	}); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("ReleaseLease(wrong owner) error = %v, want ErrLeaseNotHeld", err)
	}
	if _, ok, err := backend.GetLease(ctx, lease.Key); err != nil || !ok {
		t.Fatalf("GetLease(after failed release) ok=%v err=%v, want held lease", ok, err)
	}

	if err := backend.ReleaseLease(ctx, LeaseReleaseRequest{
		Key:   lease.Key,
		Owner: lease.Owner,
		Token: lease.Token,
	}); err != nil {
		t.Fatalf("ReleaseLease(current holder) error = %v", err)
	}
	if _, ok, err := backend.GetLease(ctx, lease.Key); err != nil || ok {
		t.Fatalf("GetLease(after release) ok=%v err=%v, want no lease", ok, err)
	}
}

func TestMemoryLeaseBackendCopiesMetadata(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryLeaseBackend()
	metadata := map[string]any{"thread_id": "thread-1"}
	lease, err := backend.AcquireLease(ctx, LeaseRequest{
		Key:      "turns/thread-1",
		Owner:    "worker-a",
		TTL:      time.Minute,
		Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	metadata["thread_id"] = "mutated"
	lease.Metadata["thread_id"] = "mutated-again"

	got, ok, err := backend.GetLease(ctx, lease.Key)
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if !ok {
		t.Fatal("GetLease() ok = false, want true")
	}
	if got.Metadata["thread_id"] != "thread-1" {
		t.Fatalf("GetLease().Metadata = %+v, want copied original metadata", got.Metadata)
	}
}

func TestMemoryLeaseBackendRejectsInvalidRequests(t *testing.T) {
	ctx := context.Background()
	backend := NewMemoryLeaseBackend()
	if _, err := backend.AcquireLease(ctx, LeaseRequest{Owner: "worker-a", TTL: time.Minute}); !errors.Is(err, ErrLeaseKeyRequired) {
		t.Fatalf("AcquireLease(empty key) error = %v, want ErrLeaseKeyRequired", err)
	}
	if _, err := backend.AcquireLease(ctx, LeaseRequest{Key: "turns/thread-1", TTL: time.Minute}); !errors.Is(err, ErrLeaseOwnerRequired) {
		t.Fatalf("AcquireLease(empty owner) error = %v, want ErrLeaseOwnerRequired", err)
	}
	if _, err := backend.AcquireLease(ctx, LeaseRequest{Key: "turns/thread-1", Owner: "worker-a"}); !errors.Is(err, ErrLeaseTTLRequired) {
		t.Fatalf("AcquireLease(empty ttl) error = %v, want ErrLeaseTTLRequired", err)
	}
}

type fakeLeaseClock struct {
	now time.Time
}

func newFakeLeaseClock(now time.Time) *fakeLeaseClock {
	return &fakeLeaseClock{now: now}
}

func (c *fakeLeaseClock) Now() time.Time {
	return c.now
}

func (c *fakeLeaseClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}
