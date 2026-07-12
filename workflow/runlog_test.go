package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact/runlog"
)

func TestListSessionRunsProjectsRunsInFirstEncounterOrder(t *testing.T) {
	log := runlog.NewMemoryLog()
	times := []time.Time{
		time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 12, 2, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 12, 4, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 12, 5, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 12, 6, 0, 0, 0, time.UTC),
	}
	records := []runlog.Record{
		sessionRunRecord("run-a", 1, EventWorkflowStarted, times[0]),
		sessionRunRecord("run-b", 1, EventWorkflowStarted, times[1]),
		sessionRunRecord("run-child", 1, EventWorkflowStarted, times[2]),
		sessionRunRecord("run-a", 2, "audit.custom", times[3]),
		sessionRunRecord("run-b", 2, EventWorkflowFailed, times[4]),
		sessionRunRecord("run-child", 2, EventWorkflowCompleted, times[5]),
	}
	for _, index := range []int{0, 3} {
		records[index].DefinitionID = "root-a"
	}
	for _, index := range []int{1, 4} {
		records[index].DefinitionID = "root-b"
		records[index].DefinitionVersion = "v2"
	}
	for _, index := range []int{2, 5} {
		records[index].DefinitionID = "child"
		records[index].ParentRunID = "run-a"
	}
	for _, record := range records {
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	summaries, err := ListSessionRuns(context.Background(), log, SessionRunsRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListSessionRuns() error = %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("summaries = %+v, want three runs", summaries)
	}
	if summaries[0].RunID != "run-a" || summaries[0].DefinitionID != "root-a" ||
		summaries[0].Status != CheckpointRunning || summaries[0].StartedAt != times[0] || summaries[0].UpdatedAt != times[3] {
		t.Fatalf("run-a summary = %+v", summaries[0])
	}
	if summaries[1].RunID != "run-b" || summaries[1].DefinitionVersion != "v2" ||
		summaries[1].Status != CheckpointFailed || summaries[1].EndedAt != times[4] {
		t.Fatalf("run-b summary = %+v", summaries[1])
	}
	if summaries[2].RunID != "run-child" || summaries[2].ParentRunID != "run-a" ||
		summaries[2].Status != CheckpointCompleted || summaries[2].EndedAt != times[5] {
		t.Fatalf("child summary = %+v", summaries[2])
	}
	if _, exists := reflect.TypeFor[RunSummary]().FieldByName("Sequence"); exists {
		t.Fatal("RunSummary exposes a fabricated session sequence")
	}
}

func TestListSessionRunsReopensTerminalStatus(t *testing.T) {
	log := runlog.NewMemoryLog()
	t0 := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	for sequence, eventType := range []string{EventWorkflowStarted, EventWorkflowFailed, EventWorkflowRetryStarted} {
		record := sessionRunRecord("run-a", int64(sequence+1), eventType, t0.Add(time.Duration(sequence)*time.Hour))
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	summaries, err := ListSessionRuns(context.Background(), log, SessionRunsRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListSessionRuns() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].Status != CheckpointRunning || !summaries[0].EndedAt.IsZero() {
		t.Fatalf("retry summary = %+v, want reopened running status", summaries)
	}

	for sequence, eventType := range []string{EventWorkflowCompleted, EventWorkflowJumpStarted} {
		record := sessionRunRecord("run-a", int64(sequence+4), eventType, t0.Add(time.Duration(sequence+3)*time.Hour))
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	summaries, err = ListSessionRuns(context.Background(), log, SessionRunsRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("second ListSessionRuns() error = %v", err)
	}
	if summaries[0].Status != CheckpointRunning || !summaries[0].EndedAt.IsZero() {
		t.Fatalf("jump summary = %+v, want reopened running status", summaries[0])
	}
}

func TestListSessionRunsTreatsInterruptedAsRecoverable(t *testing.T) {
	log := runlog.NewMemoryLog()
	t0 := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	appendEvent := func(sequence int64, eventType string) {
		t.Helper()
		record := sessionRunRecord("run-a", sequence, eventType, t0.Add(time.Duration(sequence-1)*time.Hour))
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	load := func() RunSummary {
		t.Helper()
		summaries, err := ListSessionRuns(context.Background(), log, SessionRunsRequest{SessionID: "session-1"})
		if err != nil {
			t.Fatalf("ListSessionRuns() error = %v", err)
		}
		if len(summaries) != 1 {
			t.Fatalf("summaries = %+v, want one run", summaries)
		}
		return summaries[0]
	}

	appendEvent(1, EventWorkflowStarted)
	appendEvent(2, EventWorkflowInterrupted)
	interrupted := load()
	if interrupted.Status != CheckpointInterrupted || !interrupted.EndedAt.IsZero() {
		t.Fatalf("interrupted summary = %+v, want recoverable interrupted status without end time", interrupted)
	}

	appendEvent(3, EventWorkflowResumed)
	resumed := load()
	if resumed.Status != CheckpointRunning || !resumed.EndedAt.IsZero() {
		t.Fatalf("resumed summary = %+v, want running status without end time", resumed)
	}

	appendEvent(4, EventWorkflowCompleted)
	appendEvent(5, EventWorkflowRetryStarted)
	appendEvent(6, EventWorkflowInterrupted)
	reinterrupted := load()
	if reinterrupted.Status != CheckpointInterrupted || !reinterrupted.EndedAt.IsZero() {
		t.Fatalf("reinterrupted summary = %+v, want stale terminal end time cleared", reinterrupted)
	}
}

func TestListSessionRunsValidatesRequestAndLog(t *testing.T) {
	if _, err := ListSessionRuns(context.Background(), runlog.NewMemoryLog(), SessionRunsRequest{}); !errors.Is(err, runlog.ErrInvalidQuery) {
		t.Fatalf("empty session error = %v, want ErrInvalidQuery", err)
	}
	if _, err := ListSessionRuns(context.Background(), nil, SessionRunsRequest{SessionID: "session-1"}); !errors.Is(err, runlog.ErrNilLog) {
		t.Fatalf("nil log error = %v, want ErrNilLog", err)
	}
	empty, err := ListSessionRuns(context.Background(), runlog.NewMemoryLog(), SessionRunsRequest{SessionID: "session-1"})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty summaries = %#v, %v, want initialized empty slice", empty, err)
	}
}

func TestListSessionRunsRejectsInconsistentIdentity(t *testing.T) {
	base := sessionRunRecord("run-a", 1, EventWorkflowStarted, time.Now().UTC())
	tests := []struct {
		name   string
		mutate func(*runlog.Record)
	}{
		{name: "session", mutate: func(record *runlog.Record) { record.SessionID = "session-2" }},
		{name: "definition id", mutate: func(record *runlog.Record) { record.DefinitionID = "other" }},
		{name: "definition version", mutate: func(record *runlog.Record) { record.DefinitionVersion = "v2" }},
		{name: "parent run", mutate: func(record *runlog.Record) { record.ParentRunID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			changed.Sequence = 2
			changed.Timestamp = changed.Timestamp.Add(time.Second)
			test.mutate(&changed)
			_, err := ListSessionRuns(context.Background(), staticRunLog{records: []runlog.Record{base, changed}}, SessionRunsRequest{SessionID: "session-1"})
			if err == nil {
				t.Fatal("ListSessionRuns() error = nil, want inconsistent identity error")
			}
		})
	}
}

func TestRunLogSnapshotStoreValidatesCheckpointSessionIdentity(t *testing.T) {
	timeline := sessionRunRecord("run-1", 1, EventWorkflowStarted, time.Now().UTC())
	timeline.SessionID = "session-a"
	baseCheckpoint := CheckpointRecord{
		ID:                "checkpoint:run-1",
		SessionID:         "session-a",
		RunID:             "run-1",
		WorkflowName:      "example",
		TopologyVersion:   "topology-v1",
		SchemaVersion:     checkpointSchemaVersion,
		Version:           1,
		Status:            CheckpointRunning,
		ConfirmedSequence: 1,
	}
	tests := []struct {
		name    string
		history []CheckpointRecord
		wantErr bool
	}{
		{name: "matching", history: []CheckpointRecord{baseCheckpoint}},
		{
			name: "different run",
			history: func() []CheckpointRecord {
				record := baseCheckpoint
				record.RunID = "run-2"
				return []CheckpointRecord{record}
			}(),
			wantErr: true,
		},
		{
			name: "different session",
			history: func() []CheckpointRecord {
				record := baseCheckpoint
				record.SessionID = "session-b"
				return []CheckpointRecord{record}
			}(),
			wantErr: true,
		},
		{
			name: "empty session",
			history: func() []CheckpointRecord {
				record := baseCheckpoint
				record.SessionID = ""
				return []CheckpointRecord{record}
			}(),
			wantErr: true,
		},
		{
			name: "inconsistent checkpoint history",
			history: func() []CheckpointRecord {
				other := baseCheckpoint
				other.ID = "checkpoint:run-1:v2"
				other.Version = 2
				other.SessionID = "session-b"
				return []CheckpointRecord{baseCheckpoint, other}
			}(),
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := NewRunLogSnapshotStore(
				staticRunLog{records: []runlog.Record{timeline}},
				staticCheckpointHistory(test.history),
			).Load(context.Background(), SnapshotRequest{RunID: "run-1"})
			if test.wantErr {
				if !errors.Is(err, ErrCheckpointMismatch) {
					t.Fatalf("Load() error = %v, want ErrCheckpointMismatch", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if snapshot.RunMeta.SessionID != "session-a" || len(snapshot.Checkpoints) != 1 {
				t.Fatalf("snapshot = %+v, want matching checkpoint projected", snapshot)
			}
		})
	}
}

func TestRunLogSnapshotStoreRetainsCheckpointSessionWhenTimelinePageIsEmpty(t *testing.T) {
	log := runlog.NewMemoryLog()
	record := sessionRunRecord("run-1", 1, EventWorkflowStarted, time.Now().UTC())
	record.SessionID = "session-a"
	if err := log.Append(context.Background(), record); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	history := staticCheckpointHistory{{
		ID: "checkpoint:run-1", SessionID: "session-a", RunID: "run-1", WorkflowName: "example",
		TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion, Version: 1,
		Status: CheckpointRunning, ConfirmedSequence: 1,
	}}

	snapshot, err := NewRunLogSnapshotStore(log, history).Load(
		context.Background(), SnapshotRequest{RunID: "run-1", After: 1},
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Timeline) != 0 || snapshot.RunMeta.SessionID != "session-a" {
		t.Fatalf("snapshot = %+v, want empty timeline with checkpoint session retained", snapshot)
	}
}

type staticRunLog struct {
	records []runlog.Record
}

type staticCheckpointHistory []CheckpointRecord

func (history staticCheckpointHistory) ListCheckpoints(context.Context, CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	return append([]CheckpointRecord(nil), history...), nil
}

func (log staticRunLog) Append(context.Context, runlog.Record) error { return nil }

func (log staticRunLog) List(context.Context, runlog.Query) ([]runlog.Record, error) {
	return append([]runlog.Record(nil), log.records...), nil
}

func sessionRunRecord(runID string, sequence int64, eventType string, timestamp time.Time) runlog.Record {
	return runlog.Record{
		SessionID: "session-1", RunID: runID, Sequence: sequence, EventType: eventType, Source: "test",
		DefinitionID: "root", DefinitionVersion: "v1", Timestamp: timestamp,
	}
}
