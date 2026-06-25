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

func TestOTLPHTTPExporterPostsJSONTraceRequest(t *testing.T) {
	ctx := context.Background()
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/traces" {
			t.Fatalf("path = %q, want /v1/traces", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("accept = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q, want Bearer token", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		received <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	exporter, err := NewOTLPHTTPExporter(server.URL,
		WithOTLPHeader("Authorization", "Bearer token"),
		WithOTLPScope("gopact.trace", "v0.1.0"),
		WithOTLPResourceAttribute("deployment.environment", "test"),
	)
	if err != nil {
		t.Fatalf("NewOTLPHTTPExporter() error = %v", err)
	}
	span := SpanRecord{
		ServiceName: "gopact-dev",
		Kind:        SpanKindModel,
		Name:        "model/openrouter",
		Status:      SpanStatusCompleted,
		EventType:   gopact.EventModelProviderAttemptCompleted,
		IDs: gopact.RuntimeIDs{
			RunID:        "run-1",
			ThreadID:     "thread-1",
			CallID:       "0102030405060708",
			ParentCallID: "1112131415161718",
			TraceID:      "00112233445566778899aabbccddeeff",
		},
		Node: "call_model",
		Step: 2,
		Attributes: map[string]string{
			"model.provider": "openrouter",
			"event.type":     string(gopact.EventModelProviderAttemptCompleted),
		},
		CreatedAt: time.Unix(10, 123).UTC(),
	}
	if err := exporter.ExportSpan(ctx, span); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	body := <-received
	resourceSpans := body["resourceSpans"].([]any)
	if len(resourceSpans) != 1 {
		t.Fatalf("resourceSpans count = %d, want 1", len(resourceSpans))
	}
	resourceSpan := resourceSpans[0].(map[string]any)
	resource := resourceSpan["resource"].(map[string]any)
	resourceAttributes := attributesByKey(resource["attributes"].([]any))
	if got := resourceAttributes["service.name"]["stringValue"]; got != "gopact-dev" {
		t.Fatalf("resource service.name = %v, want gopact-dev", got)
	}
	if got := resourceAttributes["deployment.environment"]["stringValue"]; got != "test" {
		t.Fatalf("resource deployment.environment = %v, want test", got)
	}

	scopeSpans := resourceSpan["scopeSpans"].([]any)
	scopeSpan := scopeSpans[0].(map[string]any)
	scope := scopeSpan["scope"].(map[string]any)
	if got := scope["name"]; got != "gopact.trace" {
		t.Fatalf("scope.name = %v, want gopact.trace", got)
	}
	if got := scope["version"]; got != "v0.1.0" {
		t.Fatalf("scope.version = %v, want v0.1.0", got)
	}

	spans := scopeSpan["spans"].([]any)
	otelSpan := spans[0].(map[string]any)
	if got := otelSpan["traceId"]; got != "00112233445566778899aabbccddeeff" {
		t.Fatalf("traceId = %v, want input trace id", got)
	}
	if got := otelSpan["spanId"]; got != "0102030405060708" {
		t.Fatalf("spanId = %v, want call id", got)
	}
	if got := otelSpan["parentSpanId"]; got != "1112131415161718" {
		t.Fatalf("parentSpanId = %v, want parent call id", got)
	}
	if got := otelSpan["name"]; got != "model/openrouter" {
		t.Fatalf("span name = %v, want model/openrouter", got)
	}
	if got := otelSpan["kind"]; got != float64(3) {
		t.Fatalf("kind = %v, want CLIENT kind 3", got)
	}
	if got := otelSpan["startTimeUnixNano"]; got != "10000000123" {
		t.Fatalf("startTimeUnixNano = %v, want decimal string", got)
	}
	if got := otelSpan["endTimeUnixNano"]; got != "10000000123" {
		t.Fatalf("endTimeUnixNano = %v, want decimal string", got)
	}
	status := otelSpan["status"].(map[string]any)
	if got := status["code"]; got != float64(1) {
		t.Fatalf("status.code = %v, want OK 1", got)
	}
	spanAttributes := attributesByKey(otelSpan["attributes"].([]any))
	if got := spanAttributes["gopact.run_id"]["stringValue"]; got != "run-1" {
		t.Fatalf("gopact.run_id = %v, want run-1", got)
	}
	if got := spanAttributes["gopact.step"]["intValue"]; got != "2" {
		t.Fatalf("gopact.step = %v, want int string 2", got)
	}
	if got := spanAttributes["model.provider"]["stringValue"]; got != "openrouter" {
		t.Fatalf("model.provider = %v, want openrouter", got)
	}
}

func TestOTLPHTTPExporterDerivesStableTraceAndSpanIDs(t *testing.T) {
	received := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		received <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	exporter, err := NewOTLPHTTPExporter(server.URL + "/custom/traces")
	if err != nil {
		t.Fatalf("NewOTLPHTTPExporter() error = %v", err)
	}
	span := SpanRecord{
		Name:      "node",
		EventType: gopact.EventNodeStarted,
		IDs:       gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1", TraceID: "trace-1"},
		CreatedAt: time.Unix(20, 0).UTC(),
	}
	if err := exporter.ExportSpan(context.Background(), span); err != nil {
		t.Fatalf("ExportSpan(first) error = %v", err)
	}
	if err := exporter.ExportSpan(context.Background(), span); err != nil {
		t.Fatalf("ExportSpan(second) error = %v", err)
	}

	first := firstOTLPSpan(<-received)
	second := firstOTLPSpan(<-received)
	traceID := first["traceId"].(string)
	spanID := first["spanId"].(string)
	if len(traceID) != 32 || !isLowerHex(traceID) {
		t.Fatalf("traceId = %q, want derived lower hex 16-byte id", traceID)
	}
	if len(spanID) != 16 || !isLowerHex(spanID) {
		t.Fatalf("spanId = %q, want derived lower hex 8-byte id", spanID)
	}
	if second["traceId"] != traceID || second["spanId"] != spanID {
		t.Fatalf("derived ids changed: first=%v second=%v", first, second)
	}
}

func TestOTLPHTTPExporterReturnsPartialSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partialSuccess":{"rejectedSpans":"1","errorMessage":"dropped by collector"}}`))
	}))
	defer server.Close()

	exporter, err := NewOTLPHTTPExporter(server.URL)
	if err != nil {
		t.Fatalf("NewOTLPHTTPExporter() error = %v", err)
	}
	err = exporter.ExportSpan(context.Background(), SpanRecord{Name: "run"})
	if !errors.Is(err, ErrPartialSuccess) {
		t.Fatalf("ExportSpan() error = %v, want ErrPartialSuccess", err)
	}
	if !strings.Contains(err.Error(), "dropped by collector") {
		t.Fatalf("ExportSpan() error = %q, want collector message", err)
	}
}

func TestOTLPHTTPExporterRejectsInvalidInputs(t *testing.T) {
	if exporter, err := NewOTLPHTTPExporter(""); !errors.Is(err, ErrEndpointRequired) || exporter != nil {
		t.Fatalf("NewOTLPHTTPExporter(empty) exporter=%v err=%v, want ErrEndpointRequired", exporter, err)
	}
	if exporter, err := NewOTLPHTTPExporter("ftp://example.com"); !errors.Is(err, ErrInvalidEndpoint) || exporter != nil {
		t.Fatalf("NewOTLPHTTPExporter(ftp) exporter=%v err=%v, want ErrInvalidEndpoint", exporter, err)
	}
	if exporter, err := NewOTLPHTTPExporter("https://example.com", WithOTLPHTTPClient(nil)); !errors.Is(err, ErrHTTPClientRequired) || exporter != nil {
		t.Fatalf("NewOTLPHTTPExporter(nil client) exporter=%v err=%v, want ErrHTTPClientRequired", exporter, err)
	}
	if exporter, err := NewOTLPHTTPExporter("https://example.com", WithOTLPMaxResponseBytes(0)); !errors.Is(err, ErrMaxResponseRequired) || exporter != nil {
		t.Fatalf("NewOTLPHTTPExporter(zero max response) exporter=%v err=%v, want ErrMaxResponseRequired", exporter, err)
	}
	if err := (*OTLPHTTPExporter)(nil).ExportSpan(context.Background(), SpanRecord{Name: "run"}); !errors.Is(err, ErrHTTPExporterRequired) {
		t.Fatalf("nil ExportSpan() error = %v, want ErrHTTPExporterRequired", err)
	}
}

func attributesByKey(items []any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(items))
	for _, item := range items {
		kv := item.(map[string]any)
		value := kv["value"].(map[string]any)
		out[kv["key"].(string)] = value
	}
	return out
}

func firstOTLPSpan(body map[string]any) map[string]any {
	resourceSpans := body["resourceSpans"].([]any)
	scopeSpans := resourceSpans[0].(map[string]any)["scopeSpans"].([]any)
	spans := scopeSpans[0].(map[string]any)["spans"].([]any)
	return spans[0].(map[string]any)
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return false
	}
	return true
}
