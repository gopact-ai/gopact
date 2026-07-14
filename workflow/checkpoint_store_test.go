package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

var _ Store = (*MemoryStore)(nil)

func TestCheckpointRecordLeaseDurationIsNotPersisted(t *testing.T) {
	record := CheckpointRecord{LeaseDuration: time.Minute}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("LeaseDuration")) {
		t.Fatalf("json = %s, want LeaseDuration omitted", encoded)
	}
}

func TestCheckpointRecordRejectsPartialSourceLineage(t *testing.T) {
	now := time.Now().UTC()
	base := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		CreatedAt: now, UpdatedAt: now,
	}
	tests := []struct {
		name   string
		mutate func(*CheckpointRecord)
	}{
		{name: "run only", mutate: func(record *CheckpointRecord) { record.SourceRunID = "source" }},
		{name: "event only", mutate: func(record *CheckpointRecord) { record.SourceEventSeq = 1 }},
		{name: "revision only", mutate: func(record *CheckpointRecord) { record.SourceRevisionID = "revision" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := base
			test.mutate(&record)
			if err := validateCheckpointRecord(record); !errors.Is(err, ErrInvalidCheckpoint) {
				t.Fatalf("validateCheckpointRecord() error = %v, want ErrInvalidCheckpoint", err)
			}
		})
	}
}

func TestMemoryCheckpointerPreservesVersionHistory(t *testing.T) {
	store := NewMemoryCheckpointer()
	now := time.Now().UTC()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example", TopologyVersion: "topology-v1",
		SchemaVersion: checkpointSchemaVersion, Version: 1, Status: CheckpointRunning, ReplayStatus: ReplayUnknown,
		Payload: []byte(`{"state":"ready"}`), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(context.Background(), record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	record.ConfirmedSequence = 1
	record.ReplayStatus = ReplaySafe
	if err := store.Save(context.Background(), record, record.Version); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	history, err := store.ListCheckpoints(context.Background(), CheckpointHistoryRequest{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(history) != 2 || history[0].SessionID != "session-1" || history[1].SessionID != "session-1" ||
		history[0].Version != 1 || history[1].Version != 2 || history[1].ConfirmedSequence != 1 {
		t.Fatalf("history = %+v, want versions 1 and 2", history)
	}
	loaded, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.SessionID != "session-1" {
		t.Fatalf("Load().SessionID = %q, want session-1", loaded.SessionID)
	}
	history[1].Payload[0] = 'x'
	again, err := store.ListCheckpoints(context.Background(), CheckpointHistoryRequest{RunID: "run-1", AfterVersion: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Payload[0] == 'x' {
		t.Fatalf("history = %+v, want defensive copy", again)
	}
}

func TestMemoryCheckpointerRejectsChangedSessionID(t *testing.T) {
	tests := []struct {
		name string
		act  func(context.Context, *MemoryCheckpointer, CheckpointRecord) error
	}{
		{
			name: "save",
			act: func(ctx context.Context, store *MemoryCheckpointer, record CheckpointRecord) error {
				record.SessionID = "session-2"
				return store.Save(ctx, record, record.Version)
			},
		},
		{
			name: "finish",
			act: func(ctx context.Context, store *MemoryCheckpointer, record CheckpointRecord) error {
				record.SessionID = "session-2"
				record.Status = CheckpointCompleted
				return store.Finish(ctx, record, record.Version)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryCheckpointer()
			now := time.Now().UTC()
			record := CheckpointRecord{
				ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
				TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
				Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
				CreatedAt: now, UpdatedAt: now,
			}
			if err := store.Create(context.Background(), record); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if err := tt.act(context.Background(), store, record); !errors.Is(err, ErrCheckpointMismatch) {
				t.Fatalf("operation error = %v, want ErrCheckpointMismatch", err)
			}
		})
	}
}

func TestMemoryCheckpointerTerminalRecordIsImmutable(t *testing.T) {
	store := NewMemoryCheckpointer()
	now := time.Now().UTC()
	record := CheckpointRecord{
		ID: "checkpoint:run-1", SessionID: "session-1", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ReplayStatus: ReplayUnknown, Payload: []byte(`{"state":"ready"}`),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	record.Status = CheckpointCompleted
	if err := store.Finish(t.Context(), record, record.Version); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(t.Context(), "run-1")
	if err != nil {
		t.Fatal(err)
	}

	candidate := before
	candidate.Status = CheckpointRunning
	if err := store.Save(t.Context(), candidate, candidate.Version); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("Save() error = %v, want ErrCheckpointConflict", err)
	}
	candidate.OwnerID = "owner-2"
	candidate.ClaimSequence++
	candidate.LeaseDuration = time.Minute
	if err := store.Claim(t.Context(), candidate, candidate.Version); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("Claim() error = %v, want ErrCheckpointConflict", err)
	}
	after, err := store.Load(t.Context(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("terminal checkpoint changed\nafter:  %+v\nbefore: %+v", after, before)
	}
}

type emptyCheckpointHistory struct{}

func (emptyCheckpointHistory) ListCheckpoints(context.Context, CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	return nil, nil
}
