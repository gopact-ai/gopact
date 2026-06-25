package trace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestLangGraphHTTPExporterPostsEvent(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/runs/events" {
			t.Fatalf("path = %q, want /runs/events", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("accept = %q, want application/json", got)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("x-custom"); got != "custom-value" {
			t.Fatalf("x-custom = %q, want custom-value", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		received <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	exporter, err := NewLangGraphHTTPExporter(server.URL,
		WithLangGraphAPIKey("test-key"),
		WithLangGraphHeader("x-custom", "custom-value"),
	)
	if err != nil {
		t.Fatalf("NewLangGraphHTTPExporter() error = %v", err)
	}
	span := SpanRecord{
		ServiceName: "gopact-dev",
		Kind:        SpanKindModel,
		Name:        "model/openrouter",
		Status:      SpanStatusCompleted,
		EventType:   gopact.EventModelProviderAttemptCompleted,
		IDs: gopact.RuntimeIDs{
			UserID:       "user-1",
			SessionID:    "session-1",
			ThreadID:     "thread-1",
			RunID:        "run-1",
			AgentID:      "agent-1",
			CallID:       "call-1",
			ParentCallID: "parent-call-1",
			TraceID:      "trace-1",
		},
		Node: "call_model",
		Step: 2,
		Attributes: map[string]string{
			"model.provider": "openrouter",
			"model.name":     "openai/gpt-5-mini",
		},
		CreatedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
	}
	if err := exporter.ExportSpan(context.Background(), span); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	body := <-received
	if got := body["thread_id"]; got != "thread-1" {
		t.Fatalf("thread_id = %v, want thread-1", got)
	}
	if got := body["run_id"]; got != "run-1" {
		t.Fatalf("run_id = %v, want run-1", got)
	}
	if got := body["attempt_id"]; got != "call-1" {
		t.Fatalf("attempt_id = %v, want call-1", got)
	}
	if got := body["parent_attempt_id"]; got != "parent-call-1" {
		t.Fatalf("parent_attempt_id = %v, want parent-call-1", got)
	}
	if got := body["event"]; got != string(gopact.EventModelProviderAttemptCompleted) {
		t.Fatalf("event = %v, want model provider attempt completed", got)
	}
	if got := body["name"]; got != "model/openrouter" {
		t.Fatalf("name = %v, want model/openrouter", got)
	}
	if got := body["kind"]; got != "model" {
		t.Fatalf("kind = %v, want model", got)
	}
	if got := body["status"]; got != "completed" {
		t.Fatalf("status = %v, want completed", got)
	}
	if got := body["node"]; got != "call_model" {
		t.Fatalf("node = %v, want call_model", got)
	}
	if got := body["step"]; got != float64(2) {
		t.Fatalf("step = %v, want 2", got)
	}
	namespace := body["namespace"].([]any)
	if len(namespace) != 2 || namespace[0] != "model" || namespace[1] != "call_model" {
		t.Fatalf("namespace = %+v, want model/call_model", namespace)
	}
	if got := body["time"]; got != "2026-06-24T12:00:00Z" {
		t.Fatalf("time = %v, want RFC3339 time", got)
	}
	metadata := body["metadata"].(map[string]any)
	if got := metadata["service_name"]; got != "gopact-dev" {
		t.Fatalf("metadata.service_name = %v, want gopact-dev", got)
	}
	if got := metadata["model.provider"]; got != "openrouter" {
		t.Fatalf("metadata.model.provider = %v, want openrouter", got)
	}
	if got := metadata["model.name"]; got != "openai/gpt-5-mini" {
		t.Fatalf("metadata.model.name = %v, want model name", got)
	}
}

func TestLangGraphHTTPExporterMapsFailedSpan(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter, err := NewLangGraphHTTPExporter(server.URL + "/events")
	if err != nil {
		t.Fatalf("NewLangGraphHTTPExporter() error = %v", err)
	}
	if err := exporter.ExportSpan(context.Background(), SpanRecord{
		Kind:      SpanKindTool,
		Name:      "repo.write",
		Status:    SpanStatusFailed,
		Error:     "permission denied",
		EventType: gopact.EventToolResult,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
	}); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	body := <-received
	if got := body["status"]; got != "failed" {
		t.Fatalf("status = %v, want failed", got)
	}
	if got := body["error"]; got != "permission denied" {
		t.Fatalf("error = %v, want permission denied", got)
	}
	namespace := body["namespace"].([]any)
	if len(namespace) != 1 || namespace[0] != "tool" {
		t.Fatalf("namespace = %+v, want tool", namespace)
	}
}

func TestLangGraphHTTPExporterDerivesDistinctEventIDsForSameAttempt(t *testing.T) {
	received := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter, err := NewLangGraphHTTPExporter(server.URL)
	if err != nil {
		t.Fatalf("NewLangGraphHTTPExporter() error = %v", err)
	}
	base := SpanRecord{
		Kind:      SpanKindModel,
		Name:      "model/openrouter",
		Status:    SpanStatusStarted,
		EventType: gopact.EventModelProviderAttemptStarted,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", CallID: "call-1"},
		Node:      "call_model",
		Step:      1,
		CreatedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
	}
	first := base
	second := base
	second.Status = SpanStatusCompleted
	second.EventType = gopact.EventModelProviderAttemptCompleted
	second.CreatedAt = base.CreatedAt.Add(time.Second)
	if err := exporter.ExportSpan(context.Background(), first); err != nil {
		t.Fatalf("ExportSpan(first) error = %v", err)
	}
	if err := exporter.ExportSpan(context.Background(), second); err != nil {
		t.Fatalf("ExportSpan(second) error = %v", err)
	}

	firstBody := <-received
	secondBody := <-received
	if firstBody["attempt_id"] != "call-1" || secondBody["attempt_id"] != "call-1" {
		t.Fatalf("attempt ids = %v / %v, want same call id", firstBody["attempt_id"], secondBody["attempt_id"])
	}
	if firstBody["event_id"] == secondBody["event_id"] {
		t.Fatalf("event ids are equal for distinct events: %v", firstBody["event_id"])
	}
}

func TestLangGraphHTTPExporterReturnsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		http.Error(w, "langgraph collector rejected event", http.StatusBadRequest)
	}))
	defer server.Close()

	exporter, err := NewLangGraphHTTPExporter(server.URL)
	if err != nil {
		t.Fatalf("NewLangGraphHTTPExporter() error = %v", err)
	}
	err = exporter.ExportSpan(context.Background(), SpanRecord{Name: "run"})
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("ExportSpan() error = %v, want ErrUnexpectedStatus", err)
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "langgraph collector rejected event") {
		t.Fatalf("ExportSpan() error = %q, want status and response body", err)
	}
}

func TestLangGraphHTTPExporterRejectsInvalidInputs(t *testing.T) {
	if exporter, err := NewLangGraphHTTPExporter(""); !errors.Is(err, ErrEndpointRequired) || exporter != nil {
		t.Fatalf("NewLangGraphHTTPExporter(empty) exporter=%v err=%v, want ErrEndpointRequired", exporter, err)
	}
	if exporter, err := NewLangGraphHTTPExporter("ftp://example.com"); !errors.Is(err, ErrInvalidEndpoint) || exporter != nil {
		t.Fatalf("NewLangGraphHTTPExporter(ftp) exporter=%v err=%v, want ErrInvalidEndpoint", exporter, err)
	}
	if exporter, err := NewLangGraphHTTPExporter("https://example.com", WithLangGraphHTTPClient(nil)); !errors.Is(err, ErrHTTPClientRequired) || exporter != nil {
		t.Fatalf("NewLangGraphHTTPExporter(nil client) exporter=%v err=%v, want ErrHTTPClientRequired", exporter, err)
	}
	if exporter, err := NewLangGraphHTTPExporter("https://example.com", WithLangGraphMaxResponseBytes(0)); !errors.Is(err, ErrMaxResponseRequired) || exporter != nil {
		t.Fatalf("NewLangGraphHTTPExporter(zero max response) exporter=%v err=%v, want ErrMaxResponseRequired", exporter, err)
	}
	if err := (*LangGraphHTTPExporter)(nil).ExportSpan(context.Background(), SpanRecord{Name: "run"}); !errors.Is(err, ErrHTTPExporterRequired) {
		t.Fatalf("nil ExportSpan() error = %v, want ErrHTTPExporterRequired", err)
	}
}
