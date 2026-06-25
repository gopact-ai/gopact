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

func TestHTTPExporterPostsSpanRecord(t *testing.T) {
	ctx := context.Background()
	received := make(chan SpanRecord, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q, want Bearer token", got)
		}
		var span SpanRecord
		if err := json.NewDecoder(r.Body).Decode(&span); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		received <- span
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	exporter, err := NewHTTPExporter(server.URL, WithHeader("Authorization", "Bearer token"))
	if err != nil {
		t.Fatalf("NewHTTPExporter() error = %v", err)
	}
	span := SpanRecord{
		ServiceName: "gopact-dev",
		Kind:        SpanKindModel,
		Name:        "model/openrouter",
		Status:      SpanStatusCompleted,
		EventType:   gopact.EventModelProviderAttemptCompleted,
		IDs:         gopact.RuntimeIDs{RunID: "run-1", TraceID: "trace-1"},
		Node:        "call_model",
		Step:        2,
		Attributes:  map[string]string{"model.provider": "openrouter"},
		CreatedAt:   time.Unix(10, 0).UTC(),
	}
	if err := exporter.ExportSpan(ctx, span); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	got := <-received
	if got.Name != span.Name || got.ServiceName != span.ServiceName || got.IDs.RunID != "run-1" {
		t.Fatalf("received span = %+v, want posted span", got)
	}
	if got.Attributes["model.provider"] != "openrouter" {
		t.Fatalf("received attributes = %+v, want model provider", got.Attributes)
	}
}

func TestHTTPExporterReturnsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "collector down", http.StatusBadGateway)
	}))
	defer server.Close()

	exporter, err := NewHTTPExporter(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPExporter() error = %v", err)
	}
	err = exporter.ExportSpan(context.Background(), SpanRecord{Name: "run"})
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("ExportSpan() error = %v, want ErrUnexpectedStatus", err)
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "collector down") {
		t.Fatalf("ExportSpan() error = %q, want status and response body", err)
	}
}

func TestHTTPExporterLimitsResponseBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, strings.Repeat("x", 64), http.StatusInternalServerError)
	}))
	defer server.Close()

	exporter, err := NewHTTPExporter(server.URL, WithMaxResponseBytes(8))
	if err != nil {
		t.Fatalf("NewHTTPExporter() error = %v", err)
	}
	err = exporter.ExportSpan(context.Background(), SpanRecord{Name: "run"})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("ExportSpan() error = %v, want ErrResponseTooLarge", err)
	}
}

func TestHTTPExporterRejectsInvalidInputs(t *testing.T) {
	if exporter, err := NewHTTPExporter(""); !errors.Is(err, ErrEndpointRequired) || exporter != nil {
		t.Fatalf("NewHTTPExporter(empty) exporter=%v err=%v, want ErrEndpointRequired", exporter, err)
	}
	if exporter, err := NewHTTPExporter("ftp://example.com/traces"); !errors.Is(err, ErrInvalidEndpoint) || exporter != nil {
		t.Fatalf("NewHTTPExporter(ftp) exporter=%v err=%v, want ErrInvalidEndpoint", exporter, err)
	}
	if exporter, err := NewHTTPExporter("https://example.com/traces", WithHTTPClient(nil)); !errors.Is(err, ErrHTTPClientRequired) || exporter != nil {
		t.Fatalf("NewHTTPExporter(nil client) exporter=%v err=%v, want ErrHTTPClientRequired", exporter, err)
	}
	if exporter, err := NewHTTPExporter("https://example.com/traces", WithMaxResponseBytes(0)); !errors.Is(err, ErrMaxResponseRequired) || exporter != nil {
		t.Fatalf("NewHTTPExporter(zero max response) exporter=%v err=%v, want ErrMaxResponseRequired", exporter, err)
	}
	if err := (*HTTPExporter)(nil).ExportSpan(context.Background(), SpanRecord{Name: "run"}); !errors.Is(err, ErrHTTPExporterRequired) {
		t.Fatalf("nil ExportSpan() error = %v, want ErrHTTPExporterRequired", err)
	}
}
