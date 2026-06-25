package gopact

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrLeaseKeyRequired   = errors.New("gopact: lease key is required")
	ErrLeaseOwnerRequired = errors.New("gopact: lease owner is required")
	ErrLeaseTokenRequired = errors.New("gopact: lease token is required")
	ErrLeaseTTLRequired   = errors.New("gopact: lease ttl is required")
	ErrLeaseConflict      = errors.New("gopact: lease conflict")
	ErrLeaseNotHeld       = errors.New("gopact: lease not held")
)

// LeaseRecord describes one worker ownership lease.
type LeaseRecord struct {
	Key        string         `json:"key"`
	Owner      string         `json:"owner"`
	Token      string         `json:"token"`
	AcquiredAt time.Time      `json:"acquired_at"`
	ExpiresAt  time.Time      `json:"expires_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// LeaseRequest requests ownership of a lease key for a bounded duration.
type LeaseRequest struct {
	Key      string
	Owner    string
	TTL      time.Duration
	Metadata map[string]any
}

// LeaseRenewRequest renews an already-held lease.
type LeaseRenewRequest struct {
	Key   string
	Owner string
	Token string
	TTL   time.Duration
}

// LeaseReleaseRequest releases an already-held lease.
type LeaseReleaseRequest struct {
	Key   string
	Owner string
	Token string
}

// LeaseBackend is the minimal worker-ownership port used by distributed loops.
type LeaseBackend interface {
	AcquireLease(ctx context.Context, request LeaseRequest) (LeaseRecord, error)
	RenewLease(ctx context.Context, request LeaseRenewRequest) (LeaseRecord, error)
	ReleaseLease(ctx context.Context, request LeaseReleaseRequest) error
	GetLease(ctx context.Context, key string) (LeaseRecord, bool, error)
}

// MemoryLeaseBackend is an in-process lease backend for tests and local development.
type MemoryLeaseBackend struct {
	mu     sync.Mutex
	now    func() time.Time
	leases map[string]LeaseRecord
}

var _ LeaseBackend = (*MemoryLeaseBackend)(nil)

// MemoryLeaseOption configures a MemoryLeaseBackend.
type MemoryLeaseOption func(*MemoryLeaseBackend)

// NewMemoryLeaseBackend creates an empty in-process lease backend.
func NewMemoryLeaseBackend(opts ...MemoryLeaseOption) *MemoryLeaseBackend {
	backend := &MemoryLeaseBackend{
		now:    time.Now,
		leases: make(map[string]LeaseRecord),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(backend)
		}
	}
	if backend.now == nil {
		backend.now = time.Now
	}
	return backend
}

// WithMemoryLeaseClock overrides the clock used by MemoryLeaseBackend.
func WithMemoryLeaseClock(now func() time.Time) MemoryLeaseOption {
	return func(backend *MemoryLeaseBackend) {
		if now != nil {
			backend.now = now
		}
	}
}

// AcquireLease acquires key for owner unless a non-expired lease is held.
func (b *MemoryLeaseBackend) AcquireLease(ctx context.Context, request LeaseRequest) (LeaseRecord, error) {
	if err := checkLeaseContext(ctx); err != nil {
		return LeaseRecord{}, err
	}
	if err := validateLeaseAcquire(request); err != nil {
		return LeaseRecord{}, err
	}
	if b == nil {
		return LeaseRecord{}, errors.New("gopact: lease backend is nil")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.currentTime()
	if b.leases == nil {
		b.leases = make(map[string]LeaseRecord)
	}
	if current, ok := b.leases[request.Key]; ok && !leaseExpired(current, now) {
		return LeaseRecord{}, ErrLeaseConflict
	}
	token, err := newLeaseToken()
	if err != nil {
		return LeaseRecord{}, err
	}
	record := LeaseRecord{
		Key:        request.Key,
		Owner:      request.Owner,
		Token:      token,
		AcquiredAt: now,
		ExpiresAt:  now.Add(request.TTL),
		Metadata:   copyAnyMap(request.Metadata),
	}
	b.leases[request.Key] = copyLeaseRecord(record)
	return copyLeaseRecord(record), nil
}

// RenewLease extends a lease only if owner and token match the current holder.
func (b *MemoryLeaseBackend) RenewLease(ctx context.Context, request LeaseRenewRequest) (LeaseRecord, error) {
	if err := checkLeaseContext(ctx); err != nil {
		return LeaseRecord{}, err
	}
	if err := validateLeaseRenew(request); err != nil {
		return LeaseRecord{}, err
	}
	if b == nil {
		return LeaseRecord{}, errors.New("gopact: lease backend is nil")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.currentTime()
	current, ok := b.leases[request.Key]
	if !ok || leaseExpired(current, now) || current.Owner != request.Owner || current.Token != request.Token {
		if ok && leaseExpired(current, now) {
			delete(b.leases, request.Key)
		}
		return LeaseRecord{}, ErrLeaseNotHeld
	}
	token, err := newLeaseToken()
	if err != nil {
		return LeaseRecord{}, err
	}
	current.Token = token
	current.ExpiresAt = now.Add(request.TTL)
	b.leases[request.Key] = copyLeaseRecord(current)
	return copyLeaseRecord(current), nil
}

// ReleaseLease releases a lease only if owner and token match the current holder.
func (b *MemoryLeaseBackend) ReleaseLease(ctx context.Context, request LeaseReleaseRequest) error {
	if err := checkLeaseContext(ctx); err != nil {
		return err
	}
	if err := validateLeaseRelease(request); err != nil {
		return err
	}
	if b == nil {
		return errors.New("gopact: lease backend is nil")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.currentTime()
	current, ok := b.leases[request.Key]
	if !ok || leaseExpired(current, now) || current.Owner != request.Owner || current.Token != request.Token {
		if ok && leaseExpired(current, now) {
			delete(b.leases, request.Key)
		}
		return ErrLeaseNotHeld
	}
	delete(b.leases, request.Key)
	return nil
}

// GetLease returns the current non-expired lease for key.
func (b *MemoryLeaseBackend) GetLease(ctx context.Context, key string) (LeaseRecord, bool, error) {
	if err := checkLeaseContext(ctx); err != nil {
		return LeaseRecord{}, false, err
	}
	if key == "" {
		return LeaseRecord{}, false, ErrLeaseKeyRequired
	}
	if b == nil {
		return LeaseRecord{}, false, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	current, ok := b.leases[key]
	if !ok {
		return LeaseRecord{}, false, nil
	}
	if leaseExpired(current, b.currentTime()) {
		delete(b.leases, key)
		return LeaseRecord{}, false, nil
	}
	return copyLeaseRecord(current), true, nil
}

func validateLeaseAcquire(request LeaseRequest) error {
	if request.Key == "" {
		return ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return ErrLeaseOwnerRequired
	}
	if request.TTL <= 0 {
		return ErrLeaseTTLRequired
	}
	return nil
}

func validateLeaseRenew(request LeaseRenewRequest) error {
	if request.Key == "" {
		return ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return ErrLeaseOwnerRequired
	}
	if request.Token == "" {
		return ErrLeaseTokenRequired
	}
	if request.TTL <= 0 {
		return ErrLeaseTTLRequired
	}
	return nil
}

func validateLeaseRelease(request LeaseReleaseRequest) error {
	if request.Key == "" {
		return ErrLeaseKeyRequired
	}
	if request.Owner == "" {
		return ErrLeaseOwnerRequired
	}
	if request.Token == "" {
		return ErrLeaseTokenRequired
	}
	return nil
}

func checkLeaseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	return ctx.Err()
}

func (b *MemoryLeaseBackend) currentTime() time.Time {
	if b.now == nil {
		return time.Now().UTC()
	}
	return b.now().UTC()
}

func leaseExpired(record LeaseRecord, now time.Time) bool {
	return !now.Before(record.ExpiresAt)
}

func copyLeaseRecord(record LeaseRecord) LeaseRecord {
	record.Metadata = copyAnyMap(record.Metadata)
	return record
}

func newLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("gopact: generate lease token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
