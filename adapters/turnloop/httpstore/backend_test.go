package httpstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestBackendPersistsTurnLoopStateWithHTTP(t *testing.T) {
	ctx := context.Background()
	server := newTestControlPlane(t, "Authorization", "Bearer test-token")
	defer server.Close()

	backend, err := NewBackend(server.URL, WithHeader("Authorization", "Bearer test-token"))
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

	restoredBackend, err := NewBackend(server.URL, WithHeader("Authorization", "Bearer test-token"))
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

func TestVersionedBackendPersistsTurnLoopStateWithHTTP(t *testing.T) {
	ctx := context.Background()
	server := newTestControlPlane(t, "", "")
	defer server.Close()

	backend, err := NewBackend(server.URL)
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

	restoredBackend, err := NewBackend(server.URL)
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
	if !ok || got.InputSeq != 2 {
		t.Fatalf("Load(restored) ok=%v state=%+v, want latest state", ok, got)
	}
}

func TestVersionedBackendMapsConflictStatus(t *testing.T) {
	ctx := context.Background()
	server := newTestControlPlane(t, "", "")
	defer server.Close()

	backend, err := NewBackend(server.URL)
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
	server := newTestControlPlane(t, "", "")
	defer server.Close()

	backend, err := NewBackend(server.URL)
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

func TestNewBackendRejectsInvalidEndpoint(t *testing.T) {
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
}

func TestBackendRejectsUnexpectedStatus(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	backend, err := NewBackend(server.URL)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	_, _, err = backend.GetTurnLoopState(ctx, "turns/main")
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("GetTurnLoopState() error = %v, want ErrUnexpectedStatus", err)
	}
}

type testControlPlane struct {
	t                   *testing.T
	requiredHeaderKey   string
	requiredHeaderValue string

	mu        sync.Mutex
	rows      map[string]gopact.TurnLoopRowRecord
	versioned map[string]gopact.TurnLoopVersionedRecord
	version   int
}

func newTestControlPlane(t *testing.T, headerKey, headerValue string) *httptest.Server {
	t.Helper()
	plane := &testControlPlane{
		t:                   t,
		requiredHeaderKey:   headerKey,
		requiredHeaderValue: headerValue,
		rows:                make(map[string]gopact.TurnLoopRowRecord),
		versioned:           make(map[string]gopact.TurnLoopVersionedRecord),
	}
	return httptest.NewServer(plane)
}

func (p *testControlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.requiredHeaderKey != "" && r.Header.Get(p.requiredHeaderKey) != p.requiredHeaderValue {
		http.Error(w, "missing header", http.StatusUnauthorized)
		return
	}

	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/turnloop/state":
		p.putRow(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/turnloop/state":
		p.getRow(w, r)
	case r.Method == http.MethodPut && r.URL.Path == "/turnloop/versioned":
		p.putVersioned(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/turnloop/versioned":
		p.getVersioned(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *testControlPlane) putRow(w http.ResponseWriter, r *http.Request) {
	key := requestKey(r)
	var record gopact.TurnLoopRowRecord
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if key == "" || record.Key != key {
		http.Error(w, fmt.Sprintf("key mismatch: query=%q record=%q", key, record.Key), http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	p.rows[key] = record
	p.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (p *testControlPlane) getRow(w http.ResponseWriter, r *http.Request) {
	key := requestKey(r)
	p.mu.Lock()
	record, ok := p.rows[key]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, record)
}

func (p *testControlPlane) putVersioned(w http.ResponseWriter, r *http.Request) {
	key := requestKey(r)
	expectedVersion := r.URL.Query().Get("expected_version")
	var record gopact.TurnLoopVersionedRecord
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if key == "" || record.Key != key {
		http.Error(w, fmt.Sprintf("key mismatch: query=%q record=%q", key, record.Key), http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	current, ok := p.versioned[key]
	if !ok {
		if expectedVersion != "" {
			http.Error(w, "version conflict", http.StatusConflict)
			return
		}
	} else if current.Version != expectedVersion {
		http.Error(w, "version conflict", http.StatusConflict)
		return
	}

	p.version++
	record.Version = fmt.Sprintf("v%d", p.version)
	p.versioned[key] = record
	writeJSON(w, versionResponse{Version: record.Version})
}

func (p *testControlPlane) getVersioned(w http.ResponseWriter, r *http.Request) {
	key := requestKey(r)
	p.mu.Lock()
	record, ok := p.versioned[key]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, record)
}

func requestKey(r *http.Request) string {
	return r.URL.Query().Get("key")
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
