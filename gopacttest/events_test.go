package gopacttest

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCollectEventsReturnsEventsAndError(t *testing.T) {
	wantErr := errors.New("boom")
	events, err := CollectEvents(func(yield func(gopact.Event, error) bool) {
		yield(gopact.Event{Type: gopact.EventRunStarted}, nil)
		yield(gopact.Event{Type: gopact.EventRunFailed, Err: wantErr}, wantErr)
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("CollectEvents() error = %v, want %v", err, wantErr)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
}

func TestFilterByRunIDPreservesMatchingOrder(t *testing.T) {
	events := []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "a"}},
		{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "b"}},
		{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "a"}},
	}

	got := FilterByRunID(events, "a")
	want := []gopact.Event{events[0], events[2]}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterByRunID() = %+v, want %+v", got, want)
	}
}

func TestRequireEventTypesPassesForExactTypes(t *testing.T) {
	RequireEventTypes(t, []gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventRunCompleted},
	}, gopact.EventRunStarted, gopact.EventRunCompleted)
}

func TestEventTypesReturnsTypes(t *testing.T) {
	got := EventTypes([]gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventRunCompleted},
	})
	want := []gopact.EventType{gopact.EventRunStarted, gopact.EventRunCompleted}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EventTypes() = %v, want %v", got, want)
	}
}

func TestCollectEventsStopsWhenConsumerStops(t *testing.T) {
	seq := func(yield func(gopact.Event, error) bool) {
		if !yield(gopact.Event{Type: gopact.EventRunStarted}, nil) {
			return
		}
		yield(gopact.Event{Type: gopact.EventRunCompleted}, nil)
	}

	var got []gopact.Event
	for event, err := range seqWithContext(context.Background(), seq) {
		if err != nil {
			t.Fatalf("unexpected error = %v", err)
		}
		got = append(got, event)
		break
	}

	if len(got) != 1 {
		t.Fatalf("collected count = %d, want 1", len(got))
	}
}

func seqWithContext(ctx context.Context, seq iter.Seq2[gopact.Event, error]) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(gopact.Event{Type: gopact.EventRunCanceled, Err: err}, err)
			return
		}
		for event, err := range seq {
			if !yield(event, err) {
				return
			}
		}
	}
}
