package tui

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestTransferConvertsSurfaceMessageToPayload(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	transfer := NewTransfer()

	payload, err := transfer.Convert(context.Background(), gopact.SurfaceMessage{
		ID:   "surface-1",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Type: gopact.SurfaceMessageApproval,
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "approve tool call"},
			{Type: gopact.SurfacePartStatus, Text: "repo.write"},
		},
		Actions: []gopact.SurfaceAction{
			{ID: "resume-1", Type: gopact.SurfaceActionResume, Label: "Approve"},
			{ID: "cancel-1", Type: gopact.SurfaceActionCancel, Label: "Cancel"},
		},
		Artifacts: []gopact.ArtifactRef{
			{ID: "artifact-1", Name: "patch.diff", URI: "file://patch.diff"},
		},
		SourceEvent: "interrupted",
		Metadata:    map[string]any{"scope": "approval"},
		CreatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if payload.Target != Target {
		t.Fatalf("payload target = %q, want %q", payload.Target, Target)
	}
	got, ok := payload.Data.(Payload)
	if !ok {
		t.Fatalf("payload data type = %T, want tui.Payload", payload.Data)
	}
	want := Payload{
		MessageID:   "surface-1",
		Type:        gopact.SurfaceMessageApproval,
		IDs:         gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Text:        "approve tool call\nrepo.write",
		Actions:     []gopact.SurfaceAction{{ID: "resume-1", Type: gopact.SurfaceActionResume, Label: "Approve"}, {ID: "cancel-1", Type: gopact.SurfaceActionCancel, Label: "Cancel"}},
		Artifacts:   []gopact.ArtifactRef{{ID: "artifact-1", Name: "patch.diff", URI: "file://patch.diff"}},
		SourceEvent: "interrupted",
		Metadata:    map[string]any{"scope": "approval"},
		CreatedAt:   createdAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload data = %+v, want %+v", got, want)
	}
}

func TestChannelWritesPayloadAndStreamsEvents(t *testing.T) {
	var writer bytes.Buffer
	channel, err := NewChannel(&writer, WithEvents(func(_ context.Context) iter.Seq2[gopact.ChannelEvent, error] {
		return func(yield func(gopact.ChannelEvent, error) bool) {
			yield(gopact.ChannelEvent{
				ID:      "event-1",
				Channel: Target,
				Type:    gopact.ChannelEventMessage,
				Text:    "continue",
			}, nil)
		}
	}))
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}

	err = channel.Send(context.Background(), gopact.ChannelPayload{
		Target: Target,
		Data: Payload{
			Text: "approve tool call",
			Actions: []gopact.SurfaceAction{
				{ID: "resume-1", Type: gopact.SurfaceActionResume, Label: "Approve"},
				{ID: "cancel-1", Type: gopact.SurfaceActionCancel},
			},
			Artifacts: []gopact.ArtifactRef{{Name: "patch.diff", URI: "file://patch.diff"}},
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	wantOutput := "approve tool call\nactions: Approve, cancel\nartifacts: patch.diff (file://patch.diff)\n"
	if writer.String() != wantOutput {
		t.Fatalf("writer = %q, want %q", writer.String(), wantOutput)
	}

	events, err := collectEvents(channel.Events(context.Background()))
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-1" || events[0].Channel != Target {
		t.Fatalf("events = %+v, want tui event-1", events)
	}
}

func TestChannelRejectsUnsupportedPayload(t *testing.T) {
	channel, err := NewChannel(&bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewChannel() error = %v", err)
	}

	err = channel.Send(context.Background(), gopact.ChannelPayload{Target: Target, Data: 42})
	if !errors.Is(err, ErrUnsupportedPayload) {
		t.Fatalf("Send() error = %v, want ErrUnsupportedPayload", err)
	}
}

func TestNewChannelRequiresWriter(t *testing.T) {
	if _, err := NewChannel(nil); !errors.Is(err, ErrWriterRequired) {
		t.Fatalf("NewChannel(nil) error = %v, want ErrWriterRequired", err)
	}
}

func collectEvents(seq iter.Seq2[gopact.ChannelEvent, error]) ([]gopact.ChannelEvent, error) {
	var events []gopact.ChannelEvent
	for event, err := range seq {
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}
