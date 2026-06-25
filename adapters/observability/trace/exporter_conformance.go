package trace

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

// ErrExporterConformanceFailed is returned when a trace exporter violates the conformance harness.
var ErrExporterConformanceFailed = errors.New("trace: exporter conformance failed")

// ExporterConformanceHarness describes one trace Exporter implementation under test.
type ExporterConformanceHarness struct {
	Exporter Exporter
	Span     SpanRecord
}

// ExporterConformanceResult is the observed result for one exporter contract case.
type ExporterConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckExporterConformance runs reusable trace exporter contract cases.
func CheckExporterConformance(ctx context.Context, harness ExporterConformanceHarness) []ExporterConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	span := harness.Span
	if span.Kind == "" && span.Name == "" && span.EventType == "" {
		span = defaultExporterConformanceSpan()
	}

	return []ExporterConformanceResult{
		checkExporterExportsSpan(ctx, harness.Exporter, copySpan(span)),
		checkExporterCanceledContext(harness.Exporter, copySpan(span)),
		checkExporterDoesNotMutateSpan(ctx, harness.Exporter, copySpan(span)),
	}
}

// RequireExporterConformance fails the test unless exporter satisfies the trace exporter contract.
func RequireExporterConformance(t testing.TB, harness ExporterConformanceHarness) {
	t.Helper()

	for _, result := range CheckExporterConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("exporter conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkExporterExportsSpan(ctx context.Context, exporter Exporter, span SpanRecord) ExporterConformanceResult {
	if exporter == nil {
		return failedExporterConformance("exports-span", errors.New("exporter is nil"))
	}
	if err := exporter.ExportSpan(ctx, span); err != nil {
		return failedExporterConformance("exports-span", err)
	}
	return passedExporterConformance("exports-span")
}

func checkExporterCanceledContext(exporter Exporter, span SpanRecord) ExporterConformanceResult {
	if exporter == nil {
		return failedExporterConformance("respects-canceled-context", errors.New("exporter is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := exporter.ExportSpan(ctx, span); !errors.Is(err, context.Canceled) {
		return failedExporterConformance("respects-canceled-context", fmt.Errorf("ExportSpan canceled context error = %v, want context.Canceled", err))
	}
	return passedExporterConformance("respects-canceled-context")
}

func checkExporterDoesNotMutateSpan(ctx context.Context, exporter Exporter, span SpanRecord) ExporterConformanceResult {
	if exporter == nil {
		return failedExporterConformance("does-not-mutate-span", errors.New("exporter is nil"))
	}
	before := copySpan(span)
	if err := exporter.ExportSpan(ctx, span); err != nil {
		return failedExporterConformance("does-not-mutate-span", err)
	}
	if !reflect.DeepEqual(span, before) {
		return failedExporterConformance("does-not-mutate-span", errors.New("exporter mutated input span"))
	}
	return passedExporterConformance("does-not-mutate-span")
}

func passedExporterConformance(name string) ExporterConformanceResult {
	return ExporterConformanceResult{Case: name, Passed: true}
}

func failedExporterConformance(name string, err error) ExporterConformanceResult {
	return ExporterConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrExporterConformanceFailed, err),
	}
}

func defaultExporterConformanceSpan() SpanRecord {
	return SpanRecord{
		ServiceName: "gopact",
		Kind:        SpanKindRun,
		Name:        "run",
		Status:      SpanStatusCompleted,
		EventType:   gopact.EventRunCompleted,
		IDs:         gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread", TraceID: "gopact-conformance-trace", CallID: "gopact-conformance-call"},
		Attributes:  map[string]string{"conformance": "trace-exporter"},
		CreatedAt:   time.Unix(1, 0),
	}
}
