// Package gopacttest provides small helpers for testing gopact event streams.
package gopacttest

import (
	"iter"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

// CollectEvents drains an event stream until completion or the first error.
func CollectEvents(seq iter.Seq2[gopact.Event, error]) ([]gopact.Event, error) {
	var events []gopact.Event
	for event, err := range seq {
		events = append(events, event)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}

// EventTypes extracts event types in order.
func EventTypes(events []gopact.Event) []gopact.EventType {
	types := make([]gopact.EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

// FilterByRunID returns events for one run id while preserving input order.
func FilterByRunID(events []gopact.Event, runID string) []gopact.Event {
	var filtered []gopact.Event
	for _, event := range events {
		if event.RuntimeIDs().RunID == runID {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

// RequireEventTypes fails the test unless event types match exactly.
func RequireEventTypes(t testing.TB, events []gopact.Event, want ...gopact.EventType) {
	t.Helper()

	got := EventTypes(events)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}
