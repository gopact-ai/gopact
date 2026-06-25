package httpstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestBackendAcquiresRenewsAndReleasesLeaseWithHTTP(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(500, 0).UTC())
	server := newTestLeaseControlPlane(t, clock, "Authorization", "Bearer test-token")
	defer server.Close()

	backend, err := NewBackend(server.URL, WithHeader("Authorization", "Bearer test-token"))
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
	if lease.Owner != "worker-a" || lease.Token != "token-1" ||
		!lease.AcquiredAt.Equal(clock.Now()) || !lease.ExpiresAt.Equal(clock.Now().Add(10*time.Second)) {
		t.Fatalf("AcquireLease() = %+v, want worker-a lease", lease)
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
	server := newTestLeaseControlPlane(t, clock, "", "")
	defer server.Close()

	backend, err := NewBackend(server.URL)
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

func TestBackendComposesWithLeasedTurnLoopStore(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock(time.Unix(700, 0).UTC())
	server := newTestLeaseControlPlane(t, clock, "", "")
	defer server.Close()

	leases, err := NewBackend(server.URL)
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

func TestBackendReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	server := newTestLeaseControlPlane(t, newFakeClock(time.Unix(800, 0).UTC()), "", "")
	defer server.Close()

	backend, err := NewBackend(server.URL)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	lease, ok, err := backend.GetLease(ctx, "missing")
	if err != nil {
		t.Fatalf("GetLease() error = %v", err)
	}
	if ok || lease.Key != "" {
		t.Fatalf("GetLease() = %+v, %v; want zero false", lease, ok)
	}
}

func TestBackendValidatesInputsAndOptions(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  error
	}{
		{name: "empty endpoint", endpoint: "", wantErr: ErrEndpointRequired},
		{name: "missing host", endpoint: "http://", wantErr: ErrInvalidEndpoint},
		{name: "unsupported scheme", endpoint: "ftp://example.com", wantErr: ErrInvalidEndpoint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewBackend(tt.endpoint)
			if !errors.Is(err, tt.wantErr) || backend != nil {
				t.Fatalf("NewBackend(%q) backend=%v err=%v, want %v", tt.endpoint, backend, err, tt.wantErr)
			}
		})
	}

	server := newTestLeaseControlPlane(t, newFakeClock(time.Unix(900, 0).UTC()), "", "")
	defer server.Close()
	if backend, err := NewBackend(server.URL, WithHTTPClient(nil)); !errors.Is(err, ErrHTTPClientRequired) || backend != nil {
		t.Fatalf("NewBackend(WithHTTPClient(nil)) backend=%v err=%v, want ErrHTTPClientRequired", backend, err)
	}
	if backend, err := NewBackend(server.URL, WithMaxResponseBytes(0)); !errors.Is(err, ErrMaxResponseRequired) || backend != nil {
		t.Fatalf("NewBackend(WithMaxResponseBytes(0)) backend=%v err=%v, want ErrMaxResponseRequired", backend, err)
	}
	if backend, err := NewBackend(server.URL, WithHeader(" ", "value")); err == nil || backend != nil {
		t.Fatalf("NewBackend(WithHeader(empty)) backend=%v err=%v, want error", backend, err)
	}

	backend, err := NewBackend(server.URL)
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

func TestBackendRejectsUnexpectedStatusAndOversizedResponse(t *testing.T) {
	ctx := context.Background()
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	backend, err := NewBackend(errorServer.URL)
	if err != nil {
		t.Fatalf("NewBackend(errorServer) error = %v", err)
	}
	_, _, err = backend.GetLease(ctx, "turns/main")
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("GetLease(unexpected status) error = %v, want ErrUnexpectedStatus", err)
	}

	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, gopact.LeaseRecord{Key: "turns/main", Owner: "worker-a", Token: strings.Repeat("x", 64)})
	}))
	defer largeServer.Close()
	limited, err := NewBackend(largeServer.URL, WithMaxResponseBytes(8))
	if err != nil {
		t.Fatalf("NewBackend(largeServer) error = %v", err)
	}
	_, _, err = limited.GetLease(ctx, "turns/main")
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("GetLease(large response) error = %v, want ErrResponseTooLarge", err)
	}
}

type testLeaseControlPlane struct {
	t                   *testing.T
	clock               *fakeClock
	requiredHeaderKey   string
	requiredHeaderValue string

	mu      sync.Mutex
	leases  map[string]gopact.LeaseRecord
	tokenID int
}

func newTestLeaseControlPlane(t *testing.T, clock *fakeClock, headerKey, headerValue string) *httptest.Server {
	t.Helper()
	plane := &testLeaseControlPlane{
		t:                   t,
		clock:               clock,
		requiredHeaderKey:   headerKey,
		requiredHeaderValue: headerValue,
		leases:              make(map[string]gopact.LeaseRecord),
	}
	return httptest.NewServer(plane)
}

func (p *testLeaseControlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.requiredHeaderKey != "" && r.Header.Get(p.requiredHeaderKey) != p.requiredHeaderValue {
		http.Error(w, "missing header", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/leases/acquire":
		p.acquire(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/leases/renew":
		p.renew(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/leases/release":
		p.release(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/leases":
		p.get(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *testLeaseControlPlane) acquire(w http.ResponseWriter, r *http.Request) {
	var request gopact.LeaseRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	if !sameQueryKey(w, r, request.Key) {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.Now()
	if current, ok := p.leases[request.Key]; ok && now.Before(current.ExpiresAt) {
		http.Error(w, "lease conflict", http.StatusConflict)
		return
	}
	record := gopact.LeaseRecord{
		Key:        request.Key,
		Owner:      request.Owner,
		Token:      p.nextToken(),
		AcquiredAt: now,
		ExpiresAt:  now.Add(request.TTL),
		Metadata:   copyAnyMap(request.Metadata),
	}
	p.leases[request.Key] = record
	writeJSON(w, record)
}

func (p *testLeaseControlPlane) renew(w http.ResponseWriter, r *http.Request) {
	var request gopact.LeaseRenewRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	if !sameQueryKey(w, r, request.Key) {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.Now()
	current, ok := p.leases[request.Key]
	if !ok || !now.Before(current.ExpiresAt) || current.Owner != request.Owner || current.Token != request.Token {
		http.Error(w, "lease not held", http.StatusConflict)
		return
	}
	current.Token = p.nextToken()
	current.ExpiresAt = now.Add(request.TTL)
	p.leases[request.Key] = current
	writeJSON(w, current)
}

func (p *testLeaseControlPlane) release(w http.ResponseWriter, r *http.Request) {
	var request gopact.LeaseReleaseRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	if !sameQueryKey(w, r, request.Key) {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.Now()
	current, ok := p.leases[request.Key]
	if !ok || !now.Before(current.ExpiresAt) || current.Owner != request.Owner || current.Token != request.Token {
		http.Error(w, "lease not held", http.StatusConflict)
		return
	}
	delete(p.leases, request.Key)
	w.WriteHeader(http.StatusNoContent)
}

func (p *testLeaseControlPlane) get(w http.ResponseWriter, r *http.Request) {
	key := requestKey(r)
	p.mu.Lock()
	defer p.mu.Unlock()
	current, ok := p.leases[key]
	if !ok || !p.clock.Now().Before(current.ExpiresAt) {
		delete(p.leases, key)
		http.NotFound(w, r)
		return
	}
	writeJSON(w, current)
}

func (p *testLeaseControlPlane) nextToken() string {
	p.tokenID++
	return fmt.Sprintf("token-%d", p.tokenID)
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func sameQueryKey(w http.ResponseWriter, r *http.Request, bodyKey string) bool {
	queryKey := requestKey(r)
	if queryKey == "" || queryKey != bodyKey {
		http.Error(w, fmt.Sprintf("key mismatch: query=%q body=%q", queryKey, bodyKey), http.StatusBadRequest)
		return false
	}
	return true
}

func requestKey(r *http.Request) string {
	return r.URL.Query().Get("key")
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
