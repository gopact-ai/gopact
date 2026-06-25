package trace

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyExporterDenySkipsExport(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryExporter()
	var calls int
	exporter, err := NewPolicyExporter(
		base,
		gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			calls++
			if req.Boundary != gopact.PolicyBoundaryExporter {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryExporter)
			}
			if req.Action != gopact.PolicyActionExport {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionExport)
			}
			if req.IDs.RunID != "run-1" {
				t.Fatalf("IDs = %+v, want run-1", req.IDs)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindSpan || input.Span.Name != "node:plan" {
				t.Fatalf("policy input = %+v, want span node:plan", input)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "export blocked"}, nil
		}),
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-1"}),
	)
	if err != nil {
		t.Fatalf("NewPolicyExporter() error = %v", err)
	}

	err = exporter.ExportSpan(ctx, SpanRecord{
		Kind: SpanKindNode,
		Name: "node:plan",
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("ExportSpan() error = %v, want ErrPolicyDenied", err)
	}
	if calls != 1 {
		t.Fatalf("policy calls = %d, want 1", calls)
	}
	if spans := base.Spans(); len(spans) != 0 {
		t.Fatalf("base spans = %+v, want none", spans)
	}
}

func TestPolicyExporterPublishesEventsAndAllowsExport(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryExporter()
	var events []gopact.Event
	exporter, err := NewPolicyExporter(
		base,
		gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Boundary != gopact.PolicyBoundaryExporter {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryExporter)
			}
			if req.Action != gopact.PolicyActionExport {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionExport)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
		}),
		WithPolicyEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyExporter() error = %v", err)
	}

	if err := exporter.ExportSpan(ctx, SpanRecord{Kind: SpanKindRun, Name: "run", Attributes: map[string]string{"provider": "openai"}}); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}
	spans := base.Spans()
	if len(spans) != 1 || spans[0].Name != "run" {
		t.Fatalf("spans = %+v, want exported run span", spans)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 events", events)
	}
	if events[0].Type != gopact.EventPolicyRequested || events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %q, %q", events[0].Type, events[1].Type)
	}
	if events[1].PolicyDecision == nil || events[1].PolicyDecision.Action != gopact.PolicyAllow {
		t.Fatalf("decision event = %+v, want allow decision", events[1])
	}
}

func TestPolicyExporterReviewReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryExporter()
	exporter, err := NewPolicyExporter(base, gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review export"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyExporter() error = %v", err)
	}

	err = exporter.ExportSpan(ctx, SpanRecord{Kind: SpanKindPolicy, Name: "policy"})
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("ExportSpan() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("ExportSpan() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryExporter) {
		t.Fatalf("RequiredBy = %q, want exporter", interruptErr.Record.RequiredBy)
	}
	if spans := base.Spans(); len(spans) != 0 {
		t.Fatalf("base spans = %+v, want none", spans)
	}
}

func TestNewPolicyExporterRequiresDependencies(t *testing.T) {
	if _, err := NewPolicyExporter(nil, gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})); !errors.Is(err, ErrExporterRequired) {
		t.Fatalf("NewPolicyExporter(nil, policy) error = %v, want ErrExporterRequired", err)
	}
	if _, err := NewPolicyExporter(NewMemoryExporter(), nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyExporter(exporter, nil) error = %v, want ErrPolicyRequired", err)
	}
}

func TestPolicyExporterRejectsMissingPolicyAtExport(t *testing.T) {
	exporter := &PolicyExporter{next: NewMemoryExporter()}
	err := exporter.ExportSpan(context.Background(), SpanRecord{Name: "run"})
	if !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("ExportSpan() error = %v, want ErrPolicyRequired", err)
	}
}
