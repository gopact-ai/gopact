package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type componentEventRecorder struct {
	err         error
	modelEvents []gopact.ModelEvent
	toolEvents  []gopact.ToolEvent
}

func (*componentEventRecorder) Emit(context.Context, gopact.Event) error { return nil }

func (r *componentEventRecorder) EmitModelEvent(_ context.Context, event gopact.ModelEvent) error {
	r.modelEvents = append(r.modelEvents, event)
	return r.err
}

func (r *componentEventRecorder) EmitToolEvent(_ context.Context, event gopact.ToolEvent) error {
	r.toolEvents = append(r.toolEvents, event)
	return r.err
}

func TestWorkflowDeliversComponentEventsWithoutPersistingBusinessValues(t *testing.T) {
	const runID = "component-events-run"
	const secret = "component-secret-4f7ca9"
	store := NewMemoryStore()
	recorder := &componentEventRecorder{}
	wf := New[string, string]("component-events", WithStore(store))
	node := wf.Node("component", func(ctx context.Context, input string) (string, error) {
		request := gopact.ModelRequest{Messages: []gopact.Message{gopact.UserMessage(secret)}}
		if err := EmitModelEvent(ctx, gopact.ModelEvent{Type: gopact.ModelEventCallStarted, Request: &request}); err != nil {
			return "", err
		}
		call := gopact.ToolCall{ID: "call-1", Name: "search", Arguments: json.RawMessage(`{"query":"` + secret + `"}`)}
		if err := EmitToolEvent(ctx, gopact.ToolEvent{Type: gopact.ToolEventCallStarted, Call: call}); err != nil {
			return "", err
		}
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)

	output, err := wf.Invoke(t.Context(), "ok", gopact.WithRunID(runID), gopact.WithEventSink(recorder))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if output != "ok" || len(recorder.modelEvents) != 1 || len(recorder.toolEvents) != 1 {
		t.Fatalf("output/events = %q/%d/%d, want ok/1/1", output, len(recorder.modelEvents), len(recorder.toolEvents))
	}
	if recorder.modelEvents[0].Request.Messages[0].Parts[0].Text != secret ||
		!bytes.Contains(recorder.toolEvents[0].Call.Arguments, []byte(secret)) {
		t.Fatal("component sink did not receive typed business values")
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: runID})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("persisted event contains component business value: %s", encoded)
		}
	}
}

func TestWorkflowComponentEventDeliveryPolicy(t *testing.T) {
	errSink := errors.New("component sink failed")
	tests := []struct {
		name    string
		option  func(gopact.EventSink) gopact.RunOption
		wantErr bool
	}{
		{name: "best effort", option: gopact.WithEventSink},
		{name: "strict", option: gopact.WithStrictEventSink, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &componentEventRecorder{err: errSink}
			wf := New[string, string]("component-delivery")
			node := wf.Node("model", func(ctx context.Context, input string) (string, error) {
				request := gopact.ModelRequest{}
				if err := EmitModelEvent(ctx, gopact.ModelEvent{Type: gopact.ModelEventCallStarted, Request: &request}); err != nil {
					return "", err
				}
				return input, nil
			})
			wf.Entry(node)
			wf.Exit(node)

			_, err := wf.Invoke(t.Context(), "ok", test.option(recorder))
			if test.wantErr && !errors.Is(err, errSink) {
				t.Fatalf("Invoke() error = %v, want %v", err, errSink)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Invoke() error = %v, want nil", err)
			}
		})
	}
}
