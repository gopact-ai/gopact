package trace

import (
	"context"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestCheckExporterConformancePassesMemoryExporter(t *testing.T) {
	harness := ExporterConformanceHarness{
		Exporter: NewMemoryExporter(),
		Span:     exporterConformanceSpan(),
	}

	results := CheckExporterConformance(context.Background(), harness)
	if failed := failedExporterConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckExporterConformance() failed cases: %v", failed)
	}
	RequireExporterConformance(t, harness)
}

func TestCheckExporterConformanceReportsCanceledContext(t *testing.T) {
	harness := ExporterConformanceHarness{
		Exporter: ignoringCanceledContextExporter{},
		Span:     exporterConformanceSpan(),
	}

	results := CheckExporterConformance(context.Background(), harness)
	if !hasFailedExporterConformanceCase(results, "respects-canceled-context") {
		t.Fatalf("CheckExporterConformance() did not report canceled context failure: %+v", results)
	}
}

func TestCheckExporterConformanceReportsSpanMutation(t *testing.T) {
	harness := ExporterConformanceHarness{
		Exporter: mutatingSpanExporter{},
		Span:     exporterConformanceSpan(),
	}

	results := CheckExporterConformance(context.Background(), harness)
	if !hasFailedExporterConformanceCase(results, "does-not-mutate-span") {
		t.Fatalf("CheckExporterConformance() did not report span mutation failure: %+v", results)
	}
}

func exporterConformanceSpan() SpanRecord {
	return SpanRecord{
		ServiceName: "gopact-test",
		Kind:        SpanKindRun,
		Name:        "run",
		Status:      SpanStatusCompleted,
		EventType:   gopact.EventRunCompleted,
		IDs:         gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", TraceID: "trace-1", CallID: "call-1"},
		Attributes:  map[string]string{"keep": "original"},
		CreatedAt:   time.Unix(1, 0),
	}
}

func failedExporterConformanceCases(results []ExporterConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedExporterConformanceCase(results []ExporterConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

type ignoringCanceledContextExporter struct{}

func (ignoringCanceledContextExporter) ExportSpan(_ context.Context, _ SpanRecord) error {
	return nil
}

type mutatingSpanExporter struct{}

func (mutatingSpanExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	span.Attributes["keep"] = "changed"
	return nil
}
