package runlog

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestErrHistoryCompactedIsStableSentinel(t *testing.T) {
	err := fmt.Errorf("store: %w", ErrHistoryCompacted)
	if !errors.Is(err, ErrHistoryCompacted) {
		t.Fatalf("errors.Is(%v, ErrHistoryCompacted) = false", err)
	}
}

func TestMemoryLogAppendListAndConflict(t *testing.T) {
	log := NewMemoryLog()
	timestamp := time.Date(2026, 7, 10, 1, 2, 3, 0, time.UTC)
	record := Record{
		SessionID: "session-1",
		RunID:     "run-1",
		Sequence:  1,
		EventType: "run.started",
		Source:    "agent",
		Payload:   []byte(`{"state":"running"}`),
		Timestamp: timestamp,
		Metadata:  map[string]string{"agent": "planner"},
	}
	if err := log.Append(context.Background(), record); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := log.Append(context.Background(), record); err != nil {
		t.Fatalf("idempotent Append() error = %v", err)
	}
	record.Payload[0] = 'x'
	record.Metadata["agent"] = "mutated"

	records, err := log.List(context.Background(), Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || string(records[0].Payload) != `{"state":"running"}` ||
		records[0].Metadata["agent"] != "planner" {
		t.Fatalf("records = %+v, want defensively copied record", records)
	}
	records[0].Payload[0] = 'y'
	records[0].Metadata["agent"] = "caller mutation"
	records, err = log.List(context.Background(), Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("second List() error = %v", err)
	}
	if string(records[0].Payload) != `{"state":"running"}` || records[0].Metadata["agent"] != "planner" {
		t.Fatalf("stored record was mutated: %+v", records[0])
	}

	conflict := records[0]
	conflict.EventType = "run.completed"
	if err := log.Append(context.Background(), conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting Append() error = %v, want ErrConflict", err)
	}
}

func TestMemoryLogAppendRejectsMissingSessionID(t *testing.T) {
	record := testRecord("run-1", 1, "missing session")
	record.SessionID = ""
	if err := NewMemoryLog().Append(context.Background(), record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Append() error = %v, want ErrInvalidRecord", err)
	}
}

func TestMemoryLogAppendRejectsSourceRevisionWithoutSource(t *testing.T) {
	record := testRecord("run-1", 1, "revision without source")
	record.SourceRevisionID = "source-revision"
	if err := NewMemoryLog().Append(context.Background(), record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Append() error = %v, want ErrInvalidRecord", err)
	}
}

func TestMemoryLogListPreservesGlobalOrderAndQueryBounds(t *testing.T) {
	log := NewMemoryLog()
	for _, record := range []Record{
		testRecord("run-1", 1, "one"),
		testRecord("run-2", 1, "two"),
		testRecord("run-1", 2, "three"),
	} {
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	all, err := log.List(context.Background(), Query{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 || all[0].Summary != "one" || all[1].Summary != "two" || all[2].Summary != "three" {
		t.Fatalf("all records = %+v, want global append order", all)
	}
	bounded, err := log.List(context.Background(), Query{RunID: "run-1", After: 1, Limit: 1})
	if err != nil {
		t.Fatalf("bounded List() error = %v", err)
	}
	if len(bounded) != 1 || bounded[0].Sequence != 2 {
		t.Fatalf("bounded records = %+v, want run-1 sequence 2", bounded)
	}
}

func TestMemoryLogListFiltersSessionAndRunIntersection(t *testing.T) {
	log := NewMemoryLog()
	records := []Record{
		testRecord("run-a", 1, "a-1"),
		testRecord("run-x", 1, "x-1"),
		testRecord("run-b", 1, "b-1"),
		testRecord("run-a", 2, "a-2"),
	}
	records[1].SessionID = "session-2"
	for _, record := range records {
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	session, err := log.List(context.Background(), Query{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("session List() error = %v", err)
	}
	if len(session) != 3 || session[0].RunID != "run-a" || session[0].Sequence != 1 ||
		session[1].RunID != "run-b" || session[1].Sequence != 1 ||
		session[2].RunID != "run-a" || session[2].Sequence != 2 {
		t.Fatalf("session records = %+v, want append-ordered per-run sequences", session)
	}

	run, err := log.List(context.Background(), Query{SessionID: "session-1", RunID: "run-a"})
	if err != nil {
		t.Fatalf("session/run List() error = %v", err)
	}
	if len(run) != 2 || run[0].Summary != "a-1" || run[1].Summary != "a-2" {
		t.Fatalf("session/run records = %+v, want run-a intersection", run)
	}

	all, err := log.List(context.Background(), Query{})
	if err != nil {
		t.Fatalf("empty List() error = %v", err)
	}
	if len(all) != 4 || all[0].Summary != "a-1" || all[1].Summary != "x-1" ||
		all[2].Summary != "b-1" || all[3].Summary != "a-2" {
		t.Fatalf("all records = %+v, want global append order", all)
	}
}

func TestMemoryLogListSessionAfterRequiresRun(t *testing.T) {
	log := NewMemoryLog()
	for _, record := range []Record{testRecord("run-a", 1, "a-1"), testRecord("run-a", 2, "a-2")} {
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	if _, err := log.List(context.Background(), Query{SessionID: "session-1", After: 1}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("session-only List() error = %v, want ErrInvalidQuery", err)
	}
	records, err := log.List(context.Background(), Query{SessionID: "session-1", RunID: "run-a", After: 1})
	if err != nil {
		t.Fatalf("session/run List() error = %v", err)
	}
	if len(records) != 1 || records[0].Sequence != 2 {
		t.Fatalf("records = %+v, want run-a sequence 2", records)
	}
}

func TestSinkProjectsEventLineage(t *testing.T) {
	log := NewMemoryLog()
	event := gopact.Event{
		SessionID:   "session-1",
		RunID:       "child-1",
		ParentRunID: "parent-1",
		Sequence:    2,
		Type:        "agent.completed",
		Source:      "agent.react",
		Timestamp:   time.Now().UTC(),
		Summary:     "done",
		Payload:     []byte(`{"ok":true}`),
		PayloadRef:  "artifact://event",
	}
	if err := NewSink(log).Emit(context.Background(), event); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	event.Payload[0] = 'x'
	records, err := log.List(context.Background(), Query{RunID: "child-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].SessionID != "session-1" || records[0].ParentRunID != "parent-1" ||
		records[0].EventType != "agent.completed" || string(records[0].Payload) != `{"ok":true}` {
		t.Fatalf("records = %+v, want projected event lineage", records)
	}
}

func TestSinkAssociatesForkSourceWithoutChangingEventEnvelope(t *testing.T) {
	log := NewMemoryLog()
	sink := NewSink(log).Associate(Association{SourceRunID: "source-1", SourceEventSeq: 7})
	event := gopact.Event{
		SessionID: "session-1", RunID: "fork-1", Sequence: 1, Type: "agent.started",
		Source: "planner", Timestamp: time.Now().UTC(),
	}
	if err := sink.Emit(context.Background(), event); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	records, err := log.List(context.Background(), Query{RunID: "fork-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].SourceRunID != "source-1" || records[0].SourceEventSeq != 7 {
		t.Fatalf("records = %+v, want fork source association", records)
	}
	if event.ParentRunID != "" || event.SessionID != "session-1" {
		t.Fatalf("event lineage = %+v, want independent root run", event)
	}
}

func testRecord(runID string, sequence int64, summary string) Record {
	return Record{
		SessionID: "session-1",
		RunID:     runID,
		Sequence:  sequence,
		EventType: "test.event",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Summary:   summary,
	}
}
