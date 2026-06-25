package gopact

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLeasedTurnLoopStoreAcquiresLeaseBeforeSave(t *testing.T) {
	ctx := context.Background()
	clock := newFakeLeaseClock(time.Unix(300, 0).UTC())
	leases := NewMemoryLeaseBackend(WithMemoryLeaseClock(clock.Now))
	base := NewMemoryTurnLoopStore()

	store, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore() error = %v", err)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	lease, ok, err := leases.GetLease(ctx, "turns/main")
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if !ok || lease.Owner != "worker-a" || lease.Token == "" {
		t.Fatalf("GetLease() ok=%v lease=%+v, want worker-a ownership", ok, lease)
	}
	got, ok, err := base.Load(ctx)
	if err != nil {
		t.Fatalf("base Load() error = %v", err)
	}
	if !ok || got.InputSeq != 1 {
		t.Fatalf("base Load() ok=%v state=%+v, want saved state", ok, got)
	}
}

func TestLeasedTurnLoopStoreBlocksCompetingOwner(t *testing.T) {
	ctx := context.Background()
	leases := NewMemoryLeaseBackend()
	base := NewMemoryTurnLoopStore()
	first, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(first) error = %v", err)
	}
	second, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(second) error = %v", err)
	}

	if err := first.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if _, _, err := second.Load(ctx); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("second Load() error = %v, want ErrLeaseConflict", err)
	}
}

func TestLeasedTurnLoopStorePreventsSaveAfterLeaseLost(t *testing.T) {
	ctx := context.Background()
	clock := newFakeLeaseClock(time.Unix(400, 0).UTC())
	leases := NewMemoryLeaseBackend(WithMemoryLeaseClock(clock.Now))
	base := NewMemoryTurnLoopStore()
	first, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(first) error = %v", err)
	}
	second, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(second) error = %v", err)
	}

	if err := first.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save(initial) error = %v", err)
	}
	clock.Advance(11 * time.Second)
	if err := second.Save(ctx, TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("second Save(after expiry) error = %v", err)
	}
	if err := first.Save(ctx, TurnLoopState{InputSeq: 3}); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("first Save(stale) error = %v, want ErrLeaseConflict", err)
	}

	got, ok, err := base.Load(ctx)
	if err != nil {
		t.Fatalf("base Load() error = %v", err)
	}
	if !ok || got.InputSeq != 2 {
		t.Fatalf("base Load() ok=%v state=%+v, want worker-b state preserved", ok, got)
	}
}

func TestLeasedTurnLoopStoreReleaseAllowsTransfer(t *testing.T) {
	ctx := context.Background()
	leases := NewMemoryLeaseBackend()
	base := NewMemoryTurnLoopStore()
	first, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(first) error = %v", err)
	}
	second, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(second) error = %v", err)
	}

	if err := first.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := first.Release(ctx); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := second.Save(ctx, TurnLoopState{InputSeq: 2}); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
}

func TestLeasedTurnLoopStoreAutoRenewsHeldLease(t *testing.T) {
	ctx := context.Background()
	leases := newCountingLeaseBackend(NewMemoryLeaseBackend())
	base := NewMemoryTurnLoopStore()
	observer := newCollectingLeaseObserver()

	store, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Second,
	}, WithLeasedTurnLoopRenewalInterval(5*time.Millisecond), WithLeasedTurnLoopObserver(observer))
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(ctx); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if err := store.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	initial, ok, err := leases.GetLease(ctx, "turns/main")
	if err != nil {
		t.Fatalf("GetLease(initial) error = %v", err)
	}
	if !ok {
		t.Fatal("GetLease(initial) ok = false, want held lease")
	}

	renewed := leases.WaitRenew(t, time.Second)
	if renewed.Token == initial.Token {
		t.Fatalf("renewed token = %q, want token rotation from %q", renewed.Token, initial.Token)
	}
	if event := observer.WaitType(t, LeaseEventRenewed, time.Second); event.Key != "turns/main" || event.Owner != "worker-a" {
		t.Fatalf("renewed event = %+v, want turns/main owned by worker-a", event)
	}
}

func TestLeasedTurnLoopStoreObservesLeaseConflict(t *testing.T) {
	ctx := context.Background()
	leases := NewMemoryLeaseBackend()
	base := NewMemoryTurnLoopStore()
	observer := newCollectingLeaseObserver()
	first, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(first) error = %v", err)
	}
	second, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-b",
		TTL:   time.Minute,
	}, WithLeasedTurnLoopObserver(observer))
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore(second) error = %v", err)
	}

	if err := first.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if _, _, err := second.Load(ctx); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("second Load() error = %v, want ErrLeaseConflict", err)
	}
	if !observer.HasType(LeaseEventConflict) {
		t.Fatalf("lease observer events = %+v, want %s", observer.Events(), LeaseEventConflict)
	}
}

func TestLeasedTurnLoopStoreCloseStopsRenewalAndReleasesLease(t *testing.T) {
	ctx := context.Background()
	leases := newCountingLeaseBackend(NewMemoryLeaseBackend())
	base := NewMemoryTurnLoopStore()
	observer := newCollectingLeaseObserver()

	store, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{
		Key:   "turns/main",
		Owner: "worker-a",
		TTL:   time.Second,
	}, WithLeasedTurnLoopRenewalInterval(5*time.Millisecond), WithLeasedTurnLoopObserver(observer))
	if err != nil {
		t.Fatalf("NewLeasedTurnLoopStore() error = %v", err)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	leases.WaitRenew(t, time.Second)

	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, ok, err := leases.GetLease(ctx, "turns/main"); err != nil || ok {
		t.Fatalf("GetLease(after Close) ok=%v err=%v, want released lease", ok, err)
	}
	if !observer.HasType(LeaseEventRenewalStopped) {
		t.Fatalf("lease observer events = %+v, want %s", observer.Events(), LeaseEventRenewalStopped)
	}
	if err := store.Save(ctx, TurnLoopState{InputSeq: 2}); !errors.Is(err, ErrLeasedTurnLoopStoreClosed) {
		t.Fatalf("Save(after Close) error = %v, want ErrLeasedTurnLoopStoreClosed", err)
	}
}

func TestNewLeasedTurnLoopStoreRejectsInvalidInputs(t *testing.T) {
	leases := NewMemoryLeaseBackend()
	base := NewMemoryTurnLoopStore()
	valid := LeaseRequest{Key: "turns/main", Owner: "worker-a", TTL: time.Minute}

	if store, err := NewLeasedTurnLoopStore(nil, leases, valid); !errors.Is(err, ErrTurnLoopStoreRequired) || store != nil {
		t.Fatalf("NewLeasedTurnLoopStore(nil store) store=%v err=%v, want ErrTurnLoopStoreRequired", store, err)
	}
	if store, err := NewLeasedTurnLoopStore(base, nil, valid); !errors.Is(err, ErrTurnLoopLeaseBackendRequired) || store != nil {
		t.Fatalf("NewLeasedTurnLoopStore(nil leases) store=%v err=%v, want ErrTurnLoopLeaseBackendRequired", store, err)
	}
	if store, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{Owner: "worker-a", TTL: time.Minute}); !errors.Is(err, ErrLeaseKeyRequired) || store != nil {
		t.Fatalf("NewLeasedTurnLoopStore(empty key) store=%v err=%v, want ErrLeaseKeyRequired", store, err)
	}
	if store, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{Key: "turns/main", TTL: time.Minute}); !errors.Is(err, ErrLeaseOwnerRequired) || store != nil {
		t.Fatalf("NewLeasedTurnLoopStore(empty owner) store=%v err=%v, want ErrLeaseOwnerRequired", store, err)
	}
	if store, err := NewLeasedTurnLoopStore(base, leases, LeaseRequest{Key: "turns/main", Owner: "worker-a"}); !errors.Is(err, ErrLeaseTTLRequired) || store != nil {
		t.Fatalf("NewLeasedTurnLoopStore(empty ttl) store=%v err=%v, want ErrLeaseTTLRequired", store, err)
	}
	if store, err := NewLeasedTurnLoopStore(base, leases, valid, WithLeasedTurnLoopRenewalInterval(0)); !errors.Is(err, ErrLeaseRenewalIntervalRequired) || store != nil {
		t.Fatalf("NewLeasedTurnLoopStore(empty renewal interval) store=%v err=%v, want ErrLeaseRenewalIntervalRequired", store, err)
	}
}

type countingLeaseBackend struct {
	LeaseBackend

	mu      sync.Mutex
	renewed []LeaseRecord
	renewCh chan LeaseRecord
}

func newCountingLeaseBackend(next LeaseBackend) *countingLeaseBackend {
	return &countingLeaseBackend{
		LeaseBackend: next,
		renewCh:      make(chan LeaseRecord, 16),
	}
}

func (b *countingLeaseBackend) RenewLease(ctx context.Context, request LeaseRenewRequest) (LeaseRecord, error) {
	record, err := b.LeaseBackend.RenewLease(ctx, request)
	if err != nil {
		return LeaseRecord{}, err
	}
	b.mu.Lock()
	b.renewed = append(b.renewed, record)
	b.mu.Unlock()
	select {
	case b.renewCh <- record:
	default:
	}
	return record, nil
}

func (b *countingLeaseBackend) WaitRenew(t *testing.T, timeout time.Duration) LeaseRecord {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case record := <-b.renewCh:
		return record
	case <-timer.C:
		t.Fatalf("timed out waiting for lease renewal")
		return LeaseRecord{}
	}
}

type collectingLeaseObserver struct {
	mu      sync.Mutex
	events  []LeaseEvent
	eventCh chan LeaseEvent
}

func newCollectingLeaseObserver() *collectingLeaseObserver {
	return &collectingLeaseObserver{eventCh: make(chan LeaseEvent, 32)}
}

func (o *collectingLeaseObserver) ObserveLease(ctx context.Context, event LeaseEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
	select {
	case o.eventCh <- event:
	default:
	}
}

func (o *collectingLeaseObserver) Events() []LeaseEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]LeaseEvent(nil), o.events...)
}

func (o *collectingLeaseObserver) HasType(eventType LeaseEventType) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, event := range o.events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func (o *collectingLeaseObserver) WaitType(t *testing.T, eventType LeaseEventType, timeout time.Duration) LeaseEvent {
	t.Helper()
	if o.HasType(eventType) {
		for _, event := range o.Events() {
			if event.Type == eventType {
				return event
			}
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event := <-o.eventCh:
			if event.Type == eventType {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for lease event %s; events=%+v", eventType, o.Events())
			return LeaseEvent{}
		}
	}
}
