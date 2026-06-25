package trace

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestPluginExportsSpanRecordsFromRuntimeEvents(t *testing.T) {
	ctx := context.Background()
	exporter := NewMemoryExporter()
	plugin := NewPlugin(exporter, WithServiceName("gopact-dev"))
	host := gopact.NewPluginHost()

	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	descriptors := host.PluginDescriptors()
	if len(descriptors) != 1 {
		t.Fatalf("PluginDescriptors() count = %d, want 1", len(descriptors))
	}
	descriptor := descriptors[0]
	if descriptor.Name != PluginName {
		t.Fatalf("descriptor.Name = %q, want %q", descriptor.Name, PluginName)
	}
	if !descriptor.HasCapability(gopact.PluginCapabilityTelemetry) ||
		!descriptor.HasCapability(gopact.PluginCapabilityEventSubscriber) {
		t.Fatalf("descriptor.Capabilities = %v, want telemetry and event subscriber", descriptor.Capabilities)
	}

	createdAt := time.Date(2026, 6, 24, 9, 30, 0, 0, time.UTC)
	ids := gopact.RuntimeIDs{
		UserID:    "user-1",
		SessionID: "session-1",
		ThreadID:  "thread-1",
		RunID:     "run-1",
		AgentID:   "agent-1",
		CallID:    "call-1",
		TraceID:   "trace-1",
	}
	events := []gopact.Event{
		{
			Type:      gopact.EventRunStarted,
			IDs:       ids,
			CreatedAt: createdAt,
		},
		{
			Type:      gopact.EventNodeStarted,
			IDs:       ids,
			Node:      "call_model",
			Step:      1,
			CreatedAt: createdAt.Add(time.Second),
		},
		{
			Type: gopact.EventModelProviderAttemptStarted,
			IDs:  ids,
			Node: "call_model",
			Step: 1,
			ModelRoute: &gopact.ModelRoute{
				RouteName: "default",
				Provider:  "openrouter",
				Model:     "gpt-5",
				Attempt:   2,
			},
			CreatedAt: createdAt.Add(2 * time.Second),
		},
		{
			Type: gopact.EventToolCall,
			IDs:  ids,
			Node: "call_tool",
			Step: 2,
			ToolCall: &gopact.ToolCall{
				ID:   "tool-call-1",
				Name: "repo.read",
			},
			CreatedAt: createdAt.Add(3 * time.Second),
		},
		{
			Type:      gopact.EventRunCompleted,
			IDs:       ids,
			CreatedAt: createdAt.Add(4 * time.Second),
		},
	}
	for _, event := range events {
		if err := host.Publish(ctx, event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.Type, err)
		}
	}

	spans := exporter.Spans()
	if len(spans) != len(events) {
		t.Fatalf("spans count = %d, want %d: %+v", len(spans), len(events), spans)
	}
	expected := []SpanRecord{
		{
			ServiceName: "gopact-dev",
			Kind:        SpanKindRun,
			Name:        "run",
			Status:      SpanStatusStarted,
			EventType:   gopact.EventRunStarted,
			IDs:         ids,
			CreatedAt:   createdAt,
			Attributes:  map[string]string{"event.type": string(gopact.EventRunStarted)},
		},
		{
			ServiceName: "gopact-dev",
			Kind:        SpanKindNode,
			Name:        "call_model",
			Status:      SpanStatusStarted,
			EventType:   gopact.EventNodeStarted,
			IDs:         ids,
			Node:        "call_model",
			Step:        1,
			CreatedAt:   createdAt.Add(time.Second),
			Attributes: map[string]string{
				"event.type": string(gopact.EventNodeStarted),
				"node":       "call_model",
			},
		},
		{
			ServiceName: "gopact-dev",
			Kind:        SpanKindModel,
			Name:        "model/openrouter",
			Status:      SpanStatusStarted,
			EventType:   gopact.EventModelProviderAttemptStarted,
			IDs:         ids,
			Node:        "call_model",
			Step:        1,
			CreatedAt:   createdAt.Add(2 * time.Second),
			Attributes: map[string]string{
				"event.type":     string(gopact.EventModelProviderAttemptStarted),
				"node":           "call_model",
				"model.route":    "default",
				"model.provider": "openrouter",
				"model.name":     "gpt-5",
				"model.attempt":  "2",
			},
		},
		{
			ServiceName: "gopact-dev",
			Kind:        SpanKindTool,
			Name:        "repo.read",
			Status:      SpanStatusStarted,
			EventType:   gopact.EventToolCall,
			IDs:         ids,
			Node:        "call_tool",
			Step:        2,
			CreatedAt:   createdAt.Add(3 * time.Second),
			Attributes: map[string]string{
				"event.type": string(gopact.EventToolCall),
				"node":       "call_tool",
				"tool.name":  "repo.read",
			},
		},
		{
			ServiceName: "gopact-dev",
			Kind:        SpanKindRun,
			Name:        "run",
			Status:      SpanStatusCompleted,
			EventType:   gopact.EventRunCompleted,
			IDs:         ids,
			CreatedAt:   createdAt.Add(4 * time.Second),
			Attributes:  map[string]string{"event.type": string(gopact.EventRunCompleted)},
		},
	}
	if !reflect.DeepEqual(spans, expected) {
		t.Fatalf("spans = %+v, want %+v", spans, expected)
	}
}

func TestPluginExportsFailureStatusAndError(t *testing.T) {
	ctx := context.Background()
	exporter := NewMemoryExporter()
	host := gopact.NewPluginHost()
	if err := host.Install(ctx, NewPlugin(exporter)); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	wantErr := errors.New("model failed")
	if err := host.Publish(ctx, gopact.Event{
		Type: gopact.EventNodeFailed,
		IDs:  gopact.RuntimeIDs{RunID: "run-1"},
		Node: "call_model",
		Step: 3,
		Err:  wantErr,
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	spans := exporter.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans count = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Kind != SpanKindNode || span.Name != "call_model" || span.Status != SpanStatusFailed {
		t.Fatalf("span = %+v, want failed node span", span)
	}
	if span.Error != wantErr.Error() {
		t.Fatalf("span.Error = %q, want %q", span.Error, wantErr.Error())
	}
}

func TestPluginPropagatesExporterError(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("export failed")
	host := gopact.NewPluginHost()
	if err := host.Install(ctx, NewPlugin(ExporterFunc(func(_ context.Context, _ SpanRecord) error {
		return wantErr
	}))); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	err := host.Publish(ctx, gopact.Event{Type: gopact.EventRunStarted})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want %v", err, wantErr)
	}
}

func TestMemoryExporterReturnsCopiedSpans(t *testing.T) {
	exporter := NewMemoryExporter()
	if err := exporter.ExportSpan(context.Background(), SpanRecord{
		Kind:       SpanKindRun,
		Name:       "run",
		Status:     SpanStatusStarted,
		Attributes: map[string]string{"event.type": string(gopact.EventRunStarted)},
	}); err != nil {
		t.Fatalf("ExportSpan() error = %v", err)
	}

	first := exporter.Spans()
	first[0].Attributes["event.type"] = "mutated"
	second := exporter.Spans()
	if second[0].Attributes["event.type"] != string(gopact.EventRunStarted) {
		t.Fatalf("Spans() returned shared attributes: %+v", second[0].Attributes)
	}
}
