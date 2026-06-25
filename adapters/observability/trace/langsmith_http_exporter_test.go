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

func TestLangSmithHTTPExporterPostsRun(t *testing.T) {
	ctx := context.Background()
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/runs" {
			t.Fatalf("path = %q, want /runs", r.URL.Path)
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

	exporter, err := NewLangSmithHTTPExporter(server.URL,
		WithLangSmithAPIKey("test-key"),
		WithLangSmithProject("gopact-dev"),
		WithLangSmithHeader("x-custom", "custom-value"),
	)
	if err != nil {
		t.Fatalf("NewLangSmithHTTPExporter() error = %v", err)
	}
	span := SpanRecord{
		ServiceName: "gopact",
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
			CallID:       "550e8400-e29b-41d4-a716-446655440000",
			ParentCallID: "550e8400-e29b-41d4-a716-446655440001",
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
	if err := exporter.ExportSpan(ctx, span); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	body := <-received
	if got := body["id"]; got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("id = %v, want call id", got)
	}
	if got := body["trace_id"]; got == "" {
		t.Fatalf("trace_id = %v, want derived trace id", got)
	}
	if got := body["parent_run_id"]; got != "550e8400-e29b-41d4-a716-446655440001" {
		t.Fatalf("parent_run_id = %v, want parent call id", got)
	}
	if got := body["name"]; got != "model/openrouter" {
		t.Fatalf("name = %v, want model/openrouter", got)
	}
	if got := body["run_type"]; got != "llm" {
		t.Fatalf("run_type = %v, want llm", got)
	}
	if got := body["session_name"]; got != "gopact-dev" {
		t.Fatalf("session_name = %v, want project name", got)
	}
	if got := body["start_time"]; got != "2026-06-24T12:00:00Z" {
		t.Fatalf("start_time = %v, want RFC3339 time", got)
	}
	if got := body["end_time"]; got != "2026-06-24T12:00:00Z" {
		t.Fatalf("end_time = %v, want terminal RFC3339 time", got)
	}
	extra := body["extra"].(map[string]any)
	metadata := extra["metadata"].(map[string]any)
	if got := metadata["thread_id"]; got != "thread-1" {
		t.Fatalf("metadata.thread_id = %v, want thread-1", got)
	}
	if got := metadata["session_id"]; got != "session-1" {
		t.Fatalf("metadata.session_id = %v, want session-1", got)
	}
	if got := metadata["model.provider"]; got != "openrouter" {
		t.Fatalf("metadata.model.provider = %v, want openrouter", got)
	}
	tags := body["tags"].([]any)
	if len(tags) != 3 || tags[0] != "gopact" || tags[1] != "model" || tags[2] != "completed" {
		t.Fatalf("tags = %+v, want stable gopact/model/completed tags", tags)
	}
}

func TestLangSmithHTTPExporterMapsFailedSpan(t *testing.T) {
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

	exporter, err := NewLangSmithHTTPExporter(server.URL + "/api/runs")
	if err != nil {
		t.Fatalf("NewLangSmithHTTPExporter() error = %v", err)
	}
	if err := exporter.ExportSpan(context.Background(), SpanRecord{
		Kind:      SpanKindTool,
		Name:      "repo.write",
		Status:    SpanStatusFailed,
		Error:     "permission denied",
		EventType: gopact.EventToolResult,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"},
	}); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	body := <-received
	if got := body["run_type"]; got != "tool" {
		t.Fatalf("run_type = %v, want tool", got)
	}
	if got := body["error"]; got != "permission denied" {
		t.Fatalf("error = %v, want permission denied", got)
	}
	if _, ok := body["end_time"]; !ok {
		t.Fatalf("end_time missing for failed span")
	}
}

func TestLangSmithHTTPExporterDerivesDistinctRunIDsWithoutCallID(t *testing.T) {
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

	exporter, err := NewLangSmithHTTPExporter(server.URL)
	if err != nil {
		t.Fatalf("NewLangSmithHTTPExporter() error = %v", err)
	}
	base := SpanRecord{
		Kind:      SpanKindNode,
		Name:      "call_model",
		Status:    SpanStatusCompleted,
		EventType: gopact.EventNodeCompleted,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		CreatedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
	}
	first := base
	first.Step = 1
	second := base
	second.Step = 2
	if err := exporter.ExportSpan(context.Background(), first); err != nil {
		t.Fatalf("ExportSpan(first) error = %v", err)
	}
	if err := exporter.ExportSpan(context.Background(), second); err != nil {
		t.Fatalf("ExportSpan(second) error = %v", err)
	}

	firstBody := <-received
	secondBody := <-received
	if firstBody["id"] == secondBody["id"] {
		t.Fatalf("derived ids are equal: %v", firstBody["id"])
	}
}

func TestLangSmithHTTPExporterReturnsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		http.Error(w, "langsmith rejected run", http.StatusBadRequest)
	}))
	defer server.Close()

	exporter, err := NewLangSmithHTTPExporter(server.URL)
	if err != nil {
		t.Fatalf("NewLangSmithHTTPExporter() error = %v", err)
	}
	err = exporter.ExportSpan(context.Background(), SpanRecord{Name: "run"})
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("ExportSpan() error = %v, want ErrUnexpectedStatus", err)
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "langsmith rejected run") {
		t.Fatalf("ExportSpan() error = %q, want status and response body", err)
	}
}

func TestLangSmithHTTPExporterRejectsInvalidInputs(t *testing.T) {
	if exporter, err := NewLangSmithHTTPExporter(""); !errors.Is(err, ErrEndpointRequired) || exporter != nil {
		t.Fatalf("NewLangSmithHTTPExporter(empty) exporter=%v err=%v, want ErrEndpointRequired", exporter, err)
	}
	if exporter, err := NewLangSmithHTTPExporter("ftp://example.com"); !errors.Is(err, ErrInvalidEndpoint) || exporter != nil {
		t.Fatalf("NewLangSmithHTTPExporter(ftp) exporter=%v err=%v, want ErrInvalidEndpoint", exporter, err)
	}
	if exporter, err := NewLangSmithHTTPExporter("https://example.com", WithLangSmithHTTPClient(nil)); !errors.Is(err, ErrHTTPClientRequired) || exporter != nil {
		t.Fatalf("NewLangSmithHTTPExporter(nil client) exporter=%v err=%v, want ErrHTTPClientRequired", exporter, err)
	}
	if exporter, err := NewLangSmithHTTPExporter("https://example.com", WithLangSmithMaxResponseBytes(0)); !errors.Is(err, ErrMaxResponseRequired) || exporter != nil {
		t.Fatalf("NewLangSmithHTTPExporter(zero max response) exporter=%v err=%v, want ErrMaxResponseRequired", exporter, err)
	}
	if err := (*LangSmithHTTPExporter)(nil).ExportSpan(context.Background(), SpanRecord{Name: "run"}); !errors.Is(err, ErrHTTPExporterRequired) {
		t.Fatalf("nil ExportSpan() error = %v, want ErrHTTPExporterRequired", err)
	}
}
