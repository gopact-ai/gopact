package gopact

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrTurnLoopStoreRequired is returned when a leased TurnLoop store is created without a store.
	ErrTurnLoopStoreRequired = errors.New("gopact: turn loop store is required")
	// ErrTurnLoopLeaseBackendRequired is returned when a leased TurnLoop store is created without leases.
	ErrTurnLoopLeaseBackendRequired = errors.New("gopact: turn loop lease backend is required")
	// ErrLeaseRenewalIntervalRequired is returned when auto-renew is enabled without a positive interval.
	ErrLeaseRenewalIntervalRequired = errors.New("gopact: turn loop lease renewal interval is required")
	// ErrLeasedTurnLoopStoreClosed is returned when a closed leased TurnLoop store is used.
	ErrLeasedTurnLoopStoreClosed = errors.New("gopact: leased turn loop store closed")
)

// LeaseEventType describes SDK-level lease lifecycle observations.
type LeaseEventType string

const (
	LeaseEventAcquired       LeaseEventType = "lease_acquired"
	LeaseEventRenewed        LeaseEventType = "lease_renewed"
	LeaseEventReleased       LeaseEventType = "lease_released"
	LeaseEventConflict       LeaseEventType = "lease_conflict"
	LeaseEventRenewFailed    LeaseEventType = "lease_renew_failed"
	LeaseEventReleaseFailed  LeaseEventType = "lease_release_failed"
	LeaseEventRenewalStarted LeaseEventType = "lease_renewal_started"
	LeaseEventRenewalStopped LeaseEventType = "lease_renewal_stopped"
)

// LeaseEvent is emitted by leased SDK components for ownership observability.
type LeaseEvent struct {
	Type      LeaseEventType `json:"type"`
	Key       string         `json:"key,omitempty"`
	Owner     string         `json:"owner,omitempty"`
	ExpiresAt time.Time      `json:"expires_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	Err       error          `json:"-"`
}

// LeaseObserver receives lease lifecycle observations.
type LeaseObserver interface {
	ObserveLease(ctx context.Context, event LeaseEvent)
}

// LeaseObserverFunc adapts a function to LeaseObserver.
type LeaseObserverFunc func(ctx context.Context, event LeaseEvent)

// ObserveLease calls f when f is not nil.
func (f LeaseObserverFunc) ObserveLease(ctx context.Context, event LeaseEvent) {
	if f != nil {
		f(ctx, event)
	}
}

// LeasedTurnLoopStoreOption configures a LeasedTurnLoopStore.
type LeasedTurnLoopStoreOption func(*leasedTurnLoopStoreConfig) error

type leasedTurnLoopStoreConfig struct {
	renewalInterval time.Duration
	observer        LeaseObserver
}

// WithLeasedTurnLoopRenewalInterval enables background lease renewal.
func WithLeasedTurnLoopRenewalInterval(interval time.Duration) LeasedTurnLoopStoreOption {
	return func(cfg *leasedTurnLoopStoreConfig) error {
		if interval <= 0 {
			return ErrLeaseRenewalIntervalRequired
		}
		cfg.renewalInterval = interval
		return nil
	}
}

// WithLeasedTurnLoopObserver installs a lease lifecycle observer.
func WithLeasedTurnLoopObserver(observer LeaseObserver) LeasedTurnLoopStoreOption {
	return func(cfg *leasedTurnLoopStoreConfig) error {
		cfg.observer = observer
		return nil
	}
}

// LeasedTurnLoopStore wraps a TurnLoopStore with worker ownership checks.
//
// By default, callers get an SDK-level atomic guard: Load and Save must acquire
// or renew the configured lease before touching the underlying TurnLoop state.
// WithLeasedTurnLoopRenewalInterval enables a small SDK-owned renewal worker
// for long turns without introducing business scheduling policy.
type LeasedTurnLoopStore struct {
	mu              sync.Mutex
	store           TurnLoopStore
	leases          LeaseBackend
	request         LeaseRequest
	current         LeaseRecord
	renewalInterval time.Duration
	observer        LeaseObserver
	renewCancel     context.CancelFunc
	renewDone       <-chan struct{}
	closed          bool
}

var _ TurnLoopStore = (*LeasedTurnLoopStore)(nil)

// NewLeasedTurnLoopStore creates a TurnLoop store that gates Load and Save on a lease.
func NewLeasedTurnLoopStore(store TurnLoopStore, leases LeaseBackend, request LeaseRequest, opts ...LeasedTurnLoopStoreOption) (*LeasedTurnLoopStore, error) {
	if store == nil {
		return nil, ErrTurnLoopStoreRequired
	}
	if leases == nil {
		return nil, ErrTurnLoopLeaseBackendRequired
	}
	if err := validateLeaseAcquire(request); err != nil {
		return nil, err
	}
	cfg := leasedTurnLoopStoreConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	request.Metadata = copyAnyMap(request.Metadata)
	return &LeasedTurnLoopStore{
		store:           store,
		leases:          leases,
		request:         request,
		renewalInterval: cfg.renewalInterval,
		observer:        cfg.observer,
	}, nil
}

// Load acquires or renews ownership, then loads the wrapped TurnLoop state.
func (s *LeasedTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnLoopState{}, false, err
	}
	if s == nil {
		return TurnLoopState{}, false, nil
	}

	s.mu.Lock()

	record, eventType, err := s.ensureLeaseLocked(ctx)
	if err != nil {
		s.mu.Unlock()
		s.observeLeaseError(ctx, err)
		return TurnLoopState{}, false, fmt.Errorf("gopact: acquire turn loop lease: %w", err)
	}
	state, ok, err := s.store.Load(ctx)
	s.mu.Unlock()
	s.observeLeaseRecord(ctx, eventType, record)
	if err != nil {
		return TurnLoopState{}, false, fmt.Errorf("gopact: load leased turn loop store: %w", err)
	}
	return state, ok, nil
}

// Save acquires or renews ownership, then saves the wrapped TurnLoop state.
func (s *LeasedTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return nil
	}

	s.mu.Lock()

	record, eventType, err := s.ensureLeaseLocked(ctx)
	if err != nil {
		s.mu.Unlock()
		s.observeLeaseError(ctx, err)
		return fmt.Errorf("gopact: acquire turn loop lease: %w", err)
	}
	if err := s.store.Save(ctx, state); err != nil {
		s.mu.Unlock()
		s.observeLeaseRecord(ctx, eventType, record)
		return fmt.Errorf("gopact: save leased turn loop store: %w", err)
	}
	s.mu.Unlock()
	s.observeLeaseRecord(ctx, eventType, record)
	return nil
}

// Release releases the current lease held by this store instance.
func (s *LeasedTurnLoopStore) Release(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return nil
	}

	if err := s.stopRenewal(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	if s.current.Token == "" {
		s.mu.Unlock()
		return nil
	}
	current := copyLeaseRecord(s.current)
	s.current = LeaseRecord{}
	s.mu.Unlock()

	err := s.leases.ReleaseLease(ctx, LeaseReleaseRequest{
		Key:   current.Key,
		Owner: current.Owner,
		Token: current.Token,
	})
	if err != nil {
		s.observe(ctx, LeaseEvent{
			Type:  LeaseEventReleaseFailed,
			Key:   current.Key,
			Owner: current.Owner,
			Err:   err,
		})
		return fmt.Errorf("gopact: release turn loop lease: %w", err)
	}
	s.observe(ctx, LeaseEvent{
		Type:  LeaseEventReleased,
		Key:   current.Key,
		Owner: current.Owner,
	})
	return nil
}

// Close stops background renewal and releases the currently held lease.
func (s *LeasedTurnLoopStore) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	return s.Release(ctx)
}

func (s *LeasedTurnLoopStore) stopRenewal(ctx context.Context) error {
	s.mu.Lock()
	done := s.stopRenewalLocked()
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *LeasedTurnLoopStore) stopRenewalLocked() <-chan struct{} {
	if s.renewCancel == nil {
		return nil
	}
	cancel := s.renewCancel
	done := s.renewDone
	s.renewCancel = nil
	s.renewDone = nil
	cancel()
	return done
}

func (s *LeasedTurnLoopStore) startRenewalLocked(ctx context.Context) {
	if s.renewalInterval <= 0 || s.current.Token == "" || s.renewCancel != nil {
		return
	}
	renewCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	s.renewCancel = cancel
	s.renewDone = done
	current := copyLeaseRecord(s.current)
	interval := s.renewalInterval
	go s.renewLoop(renewCtx, done, current, interval)
}

func (s *LeasedTurnLoopStore) renewLoop(ctx context.Context, done chan<- struct{}, initial LeaseRecord, interval time.Duration) {
	defer close(done)
	s.observe(context.WithoutCancel(ctx), LeaseEvent{
		Type:      LeaseEventRenewalStarted,
		Key:       initial.Key,
		Owner:     initial.Owner,
		ExpiresAt: initial.ExpiresAt,
	})
	defer s.observe(context.WithoutCancel(ctx), LeaseEvent{
		Type:  LeaseEventRenewalStopped,
		Key:   initial.Key,
		Owner: initial.Owner,
	})

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.renewOnce(ctx) {
				return
			}
		}
	}
}

func (s *LeasedTurnLoopStore) renewOnce(ctx context.Context) bool {
	s.mu.Lock()
	current := copyLeaseRecord(s.current)
	if current.Token == "" {
		s.mu.Unlock()
		return false
	}
	request := LeaseRenewRequest{
		Key:   current.Key,
		Owner: current.Owner,
		Token: current.Token,
		TTL:   s.request.TTL,
	}
	renewed, err := s.leases.RenewLease(ctx, request)
	if err != nil {
		if errors.Is(err, ErrLeaseNotHeld) {
			s.current = LeaseRecord{}
		}
		s.mu.Unlock()
		s.observe(context.WithoutCancel(ctx), LeaseEvent{
			Type:  LeaseEventRenewFailed,
			Key:   request.Key,
			Owner: request.Owner,
			Err:   err,
		})
		return !errors.Is(err, ErrLeaseNotHeld)
	}

	s.current = copyLeaseRecord(renewed)
	s.mu.Unlock()
	s.observe(context.WithoutCancel(ctx), LeaseEvent{
		Type:      LeaseEventRenewed,
		Key:       renewed.Key,
		Owner:     renewed.Owner,
		ExpiresAt: renewed.ExpiresAt,
	})
	return true
}

func (s *LeasedTurnLoopStore) observeLeaseError(ctx context.Context, err error) {
	if errors.Is(err, ErrLeaseConflict) {
		s.observe(ctx, LeaseEvent{
			Type:  LeaseEventConflict,
			Key:   s.request.Key,
			Owner: s.request.Owner,
			Err:   err,
		})
		return
	}
	if errors.Is(err, ErrLeaseNotHeld) {
		s.observe(ctx, LeaseEvent{
			Type:  LeaseEventRenewFailed,
			Key:   s.request.Key,
			Owner: s.request.Owner,
			Err:   err,
		})
	}
}

func (s *LeasedTurnLoopStore) observe(ctx context.Context, event LeaseEvent) {
	if s == nil || s.observer == nil {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now()
	}
	event.Metadata = copyAnyMap(event.Metadata)
	s.observer.ObserveLease(ctx, event)
}

func (s *LeasedTurnLoopStore) observeLeaseRecord(ctx context.Context, eventType LeaseEventType, record LeaseRecord) {
	if eventType == "" {
		return
	}
	s.observe(ctx, LeaseEvent{
		Type:      eventType,
		Key:       record.Key,
		Owner:     record.Owner,
		ExpiresAt: record.ExpiresAt,
	})
}

func (s *LeasedTurnLoopStore) ensureLeaseLocked(ctx context.Context) (LeaseRecord, LeaseEventType, error) {
	if s.closed {
		return LeaseRecord{}, "", ErrLeasedTurnLoopStoreClosed
	}
	if s.current.Token != "" {
		renewed, err := s.leases.RenewLease(ctx, LeaseRenewRequest{
			Key:   s.current.Key,
			Owner: s.current.Owner,
			Token: s.current.Token,
			TTL:   s.request.TTL,
		})
		if err == nil {
			s.current = copyLeaseRecord(renewed)
			s.startRenewalLocked(ctx)
			return copyLeaseRecord(renewed), LeaseEventRenewed, nil
		}
		s.current = LeaseRecord{}
		_ = s.stopRenewalLocked()
		if !errors.Is(err, ErrLeaseNotHeld) {
			return LeaseRecord{}, "", err
		}
	}

	acquired, err := s.leases.AcquireLease(ctx, s.request)
	if err != nil {
		return LeaseRecord{}, "", err
	}
	s.current = copyLeaseRecord(acquired)
	s.startRenewalLocked(ctx)
	return copyLeaseRecord(acquired), LeaseEventAcquired, nil
}
