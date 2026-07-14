package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact/runlog"
)

func TestListSessionRunsHistoryLimit(t *testing.T) {
	log := &pagingRunLog{}
	for sequence := int64(1); sequence <= 3; sequence++ {
		log.records = append(log.records, sessionRunRecord(
			"run-a",
			sequence,
			EventWorkflowStarted,
			time.Date(2026, time.July, 13, 12, 0, int(sequence), 0, time.UTC),
		))
	}
	_, err := ListSessionRuns(context.Background(), log, SessionRunsRequest{SessionID: "session-1", MaxRecords: 2})
	if !errors.Is(err, ErrHistoryLimitExceeded) {
		t.Fatalf("ListSessionRuns() error = %v, want ErrHistoryLimitExceeded", err)
	}
	if len(log.queries) != 1 || log.queries[0].Limit != 3 {
		t.Fatalf("queries = %+v, want limit+1 query", log.queries)
	}
}

func TestSnapshotDefaultHistoryLimit(t *testing.T) {
	log := &pagingRunLog{}
	for sequence := int64(1); sequence <= 10_001; sequence++ {
		log.records = append(log.records, sessionRunRecord(
			"run-1",
			sequence,
			"audit.custom",
			time.Date(2026, time.July, 13, 12, 0, 0, int(sequence), time.UTC),
		))
	}
	_, err := NewRunLogSnapshotStore(log, staticCheckpointHistory{}).Load(
		context.Background(),
		SnapshotRequest{RunID: "run-1"},
	)
	if !errors.Is(err, ErrHistoryLimitExceeded) {
		t.Fatalf("Load() error = %v, want ErrHistoryLimitExceeded", err)
	}
	if len(log.queries) != 1 || log.queries[0].Limit != 10_001 {
		t.Fatalf("queries = %+v, want default limit+1 query", log.queries)
	}
}

func TestSnapshotPagesCheckpointHistory(t *testing.T) {
	timeline := sessionRunRecord("run-1", 1, "audit.custom", time.Now().UTC())
	history := &pagingCheckpointHistory{}
	for version := int64(1); version <= 257; version++ {
		confirmedSequence := version
		if version == 257 {
			confirmedSequence = 1
		}
		history.records = append(history.records, CheckpointRecord{
			ID: fmt.Sprintf("checkpoint:run-1:%d", version), SessionID: "session-1", RunID: "run-1",
			WorkflowName: "example", TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion,
			Version: version, Status: CheckpointRunning, ConfirmedSequence: confirmedSequence,
		})
	}
	snapshot, err := NewRunLogSnapshotStore(
		&pagingRunLog{records: []runlog.Record{timeline}},
		history,
	).Load(context.Background(), SnapshotRequest{RunID: "run-1", Limit: 1})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(history.requests) != 2 || history.requests[0].AfterVersion != 0 || history.requests[0].Limit != 256 ||
		history.requests[1].AfterVersion != 256 || history.requests[1].Limit != 256 {
		t.Fatalf("history requests = %+v, want pages after 0 and 256", history.requests)
	}
	if len(snapshot.Checkpoints) != 1 || snapshot.Checkpoints[0].Version != 257 || !snapshot.Checkpoints[0].Root {
		t.Fatalf("snapshot checkpoints = %+v, want latest root checkpoint version 257", snapshot.Checkpoints)
	}
}

func TestControlPagesHistory(t *testing.T) {
	log := &pagingRunLog{}
	for sequence := int64(1); sequence <= 257; sequence++ {
		record := sessionRunRecord("run-1", sequence, "audit.custom", time.Now().UTC())
		record.RevisionID = fmt.Sprintf("revision-%d", sequence)
		log.records = append(log.records, record)
	}
	compiled := &compiled[string, string]{journal: log}
	record, err := compiled.controlSource(context.Background(), "run-1", "revision-257")
	if err != nil {
		t.Fatalf("controlSource() error = %v", err)
	}
	if record.Sequence != 257 {
		t.Fatalf("controlSource() sequence = %d, want 257", record.Sequence)
	}
	if len(log.queries) != 2 || log.queries[0].After != 0 || log.queries[0].Limit != 256 ||
		log.queries[1].After != 256 || log.queries[1].Limit != 256 {
		t.Fatalf("queries = %+v, want pages after 0 and 256", log.queries)
	}

	history := &pagingCheckpointHistory{}
	for version := int64(1); version <= 257; version++ {
		confirmedSequence := version
		if version == 257 {
			confirmedSequence = 1
		}
		history.records = append(history.records, CheckpointRecord{
			ID: fmt.Sprintf("checkpoint:run-1:%d", version), RunID: "run-1", Version: version,
			ConfirmedSequence: confirmedSequence,
		})
	}
	checkpoint, found, err := checkpointAtSequence(context.Background(), history, "run-1", 1)
	if err != nil {
		t.Fatalf("checkpointAtSequence() error = %v", err)
	}
	if !found || checkpoint.Version != 257 {
		t.Fatalf("checkpointAtSequence() = %+v, %v, want latest version 257", checkpoint, found)
	}
	if len(history.requests) != 2 || history.requests[0].Limit != 256 || history.requests[1].AfterVersion != 256 {
		t.Fatalf("checkpoint requests = %+v, want two pages", history.requests)
	}
}

func TestControlAndCheckpointHistoryLimits(t *testing.T) {
	log := &pagingRunLog{}
	history := &pagingCheckpointHistory{}
	for ordinal := int64(1); ordinal <= 10_001; ordinal++ {
		record := sessionRunRecord("run-1", ordinal, "audit.custom", time.Now().UTC())
		record.RevisionID = fmt.Sprintf("revision-%d", ordinal)
		log.records = append(log.records, record)
		history.records = append(history.records, CheckpointRecord{
			ID: fmt.Sprintf("checkpoint:run-1:%d", ordinal), SessionID: "session-1", RunID: "run-1",
			WorkflowName: "example", TopologyVersion: "topology-v1", SchemaVersion: checkpointSchemaVersion,
			Version: ordinal, Status: CheckpointRunning, ConfirmedSequence: ordinal,
		})
	}
	compiled := &compiled[string, string]{journal: log}
	if _, err := compiled.controlSource(context.Background(), "run-1", "missing"); !errors.Is(err, ErrHistoryLimitExceeded) {
		t.Fatalf("controlSource() error = %v, want ErrHistoryLimitExceeded", err)
	}
	if _, _, err := checkpointAtSequence(context.Background(), history, "run-1", -1); !errors.Is(err, ErrHistoryLimitExceeded) {
		t.Fatalf("checkpointAtSequence() error = %v, want ErrHistoryLimitExceeded", err)
	}
	history.requests = nil
	snapshot := Snapshot{RunMeta: RunMeta{RunID: "run-1"}}
	if err := snapshot.projectCheckpointHistory(context.Background(), history); !errors.Is(err, ErrHistoryLimitExceeded) {
		t.Fatalf("projectCheckpointHistory() error = %v, want ErrHistoryLimitExceeded", err)
	}
}

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

func TestListSessionRunsTreatsRetryAndJumpAsNewRunStarts(t *testing.T) {
	log := runlog.NewMemoryLog()
	t0 := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	retry := sessionRunRecord("run-retry", 1, EventWorkflowRetryStarted, t0)
	retry.SourceRunID, retry.SourceEventSeq, retry.SourceRevisionID = "run-source", 3, "revision-source"
	jump := sessionRunRecord("run-jump", 1, EventWorkflowJumpStarted, t0.Add(time.Hour))
	jump.SourceRunID, jump.SourceEventSeq, jump.SourceRevisionID = "run-source", 4, "revision-source"
	for _, record := range []runlog.Record{retry, jump} {
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	summaries, err := ListSessionRuns(context.Background(), log, SessionRunsRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListSessionRuns() error = %v", err)
	}
	if len(summaries) != 2 || summaries[0].Status != CheckpointRunning || summaries[1].Status != CheckpointRunning {
		t.Fatalf("summaries = %+v, want two independently running control runs", summaries)
	}
}

func TestListSessionRunsRejectsLifecycleEventsAfterTerminal(t *testing.T) {
	terminalEvents := []string{
		EventWorkflowStarted, EventWorkflowResumed, EventWorkflowRetryStarted, EventWorkflowJumpStarted,
		EventWorkflowInterrupted, EventWorkflowCompleted, EventWorkflowFailed, EventWorkflowTerminated, EventWorkflowCanceled,
	}
	for _, eventType := range terminalEvents {
		t.Run(eventType, func(t *testing.T) {
			t0 := time.Now().UTC()
			started := sessionRunRecord("run-a", 1, EventWorkflowStarted, t0)
			completed := sessionRunRecord("run-a", 2, EventWorkflowCompleted, t0.Add(time.Second))
			postTerminal := sessionRunRecord("run-a", 3, eventType, t0.Add(2*time.Second))
			_, err := ListSessionRuns(context.Background(), staticRunLog{records: []runlog.Record{started, completed, postTerminal}}, SessionRunsRequest{SessionID: "session-1"})
			if err == nil {
				t.Fatal("ListSessionRuns() error = nil, want inconsistent lifecycle error")
			}
		})
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
	completed := load()
	if completed.Status != CheckpointCompleted || completed.EndedAt.IsZero() {
		t.Fatalf("completed summary = %+v, want immutable terminal state", completed)
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
	timeline.SourceRunID, timeline.SourceEventSeq, timeline.SourceRevisionID = "source-run", 7, "source-revision"
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
		SourceRunID:       "source-run", SourceEventSeq: 7, SourceRevisionID: "source-revision",
	}
	tests := []struct {
		name    string
		history []CheckpointRecord
		wantErr bool
	}{
		{name: "matching", history: []CheckpointRecord{baseCheckpoint}},
		{
			name: "different source run",
			history: func() []CheckpointRecord {
				record := baseCheckpoint
				record.SourceRunID = "other"
				return []CheckpointRecord{record}
			}(),
			wantErr: true,
		},
		{
			name: "different source event",
			history: func() []CheckpointRecord {
				record := baseCheckpoint
				record.SourceEventSeq++
				return []CheckpointRecord{record}
			}(),
			wantErr: true,
		},
		{
			name: "different source revision",
			history: func() []CheckpointRecord {
				record := baseCheckpoint
				record.SourceRevisionID = "other"
				return []CheckpointRecord{record}
			}(),
			wantErr: true,
		},
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

func TestRunLogSnapshotStoreRejectsInconsistentTimelineSourceLineage(t *testing.T) {
	base := sessionRunRecord("run-1", 1, EventWorkflowStarted, time.Now().UTC())
	base.SourceRunID, base.SourceEventSeq, base.SourceRevisionID = "source-run", 7, "source-revision"
	tests := []struct {
		name   string
		mutate func(*runlog.Record)
	}{
		{name: "source run", mutate: func(record *runlog.Record) { record.SourceRunID = "other" }},
		{name: "source event", mutate: func(record *runlog.Record) { record.SourceEventSeq++ }},
		{name: "source revision", mutate: func(record *runlog.Record) { record.SourceRevisionID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			changed.Sequence = 2
			test.mutate(&changed)
			_, err := NewRunLogSnapshotStore(
				staticRunLog{records: []runlog.Record{base, changed}}, staticCheckpointHistory{},
			).Load(t.Context(), SnapshotRequest{RunID: "run-1"})
			if err == nil {
				t.Fatal("Load() error = nil, want inconsistent lineage")
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

type pagingRunLog struct {
	records []runlog.Record
	queries []runlog.Query
}

func (log *pagingRunLog) Append(context.Context, runlog.Record) error { return nil }

func (log *pagingRunLog) List(_ context.Context, query runlog.Query) ([]runlog.Record, error) {
	log.queries = append(log.queries, query)
	records := make([]runlog.Record, 0, query.Limit)
	for _, record := range log.records {
		if query.SessionID != "" && record.SessionID != query.SessionID {
			continue
		}
		if query.RunID != "" && record.RunID != query.RunID {
			continue
		}
		if record.Sequence <= query.After {
			continue
		}
		records = append(records, record)
		if query.Limit > 0 && len(records) == query.Limit {
			break
		}
	}
	return records, nil
}

type staticCheckpointHistory []CheckpointRecord

type pagingCheckpointHistory struct {
	records  []CheckpointRecord
	requests []CheckpointHistoryRequest
}

func (history *pagingCheckpointHistory) ListCheckpoints(_ context.Context, request CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	history.requests = append(history.requests, request)
	records := make([]CheckpointRecord, 0, request.Limit)
	for _, record := range history.records {
		if record.RunID != request.RunID || record.Version <= request.AfterVersion {
			continue
		}
		records = append(records, record)
		if request.Limit > 0 && len(records) == request.Limit {
			break
		}
	}
	return records, nil
}

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
