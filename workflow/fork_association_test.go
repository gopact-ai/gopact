package workflow

import (
	"context"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type countingAssociatingSink struct {
	log         runlog.Log
	count       *int
	association runlog.Association
}

func (sink countingAssociatingSink) Associate(association runlog.Association) gopact.EventSink {
	*sink.count++
	sink.association = association
	return sink
}

func (sink countingAssociatingSink) Emit(ctx context.Context, event gopact.Event) error {
	record := runlog.RecordFromEvent(event)
	record.SourceRunID = sink.association.SourceRunID
	record.SourceEventSeq = sink.association.SourceEventSeq
	return sink.log.Append(ctx, record)
}

func TestSnapshotForkAssociatesParentSinkOnceWithoutPropagatingToChild(t *testing.T) {
	checkpoints := NewMemoryCheckpointer()
	log := runlog.NewMemoryLog()
	associations := 0
	sink := countingAssociatingSink{log: log, count: &associations}

	childStore := NewMemoryStore()
	child := New[string, string]("child", WithStrictCheckpointer(childStore), WithStrictJournal(childStore))
	childNode := child.Node("work", func(_ context.Context, input string) (string, error) { return input + "!", nil })
	child.Entry(childNode)
	child.Exit(childNode)
	parent := New[string, string]("parent", WithCheckpointer(checkpoints))
	invokable := parent.AddInvokable("child", child)
	parent.Entry(invokable)
	parent.Exit(invokable)

	if _, err := parent.Invoke(t.Context(), "source", gopact.WithRunID("source-run"), gopact.WithEventSink(sink)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewRunLogSnapshotStore(log, checkpoints).Load(t.Context(), SnapshotRequest{RunID: "source-run"})
	if err != nil {
		t.Fatal(err)
	}
	associations = 0
	if _, err := snapshot.Fork(t.Context(), parent, ForkRequest{
		SourceRunID: "source-run", FromEventSeq: 1,
		Patch: ForkPatch{WorkflowInput: &InputPatch{Value: "fork"}},
	}, gopact.WithRunID("fork-parent"), gopact.WithEventSink(sink)); err != nil {
		t.Fatal(err)
	}
	if associations != 1 {
		t.Fatalf("Associate() calls = %d, want 1", associations)
	}

	records, err := log.List(t.Context(), runlog.Query{})
	if err != nil {
		t.Fatal(err)
	}
	parentRecords := 0
	childRecords := 0
	childRunID := ""
	for _, record := range records {
		if record.RunID == "fork-parent" {
			parentRecords++
			assertForkParentAssociation(t, record)
			continue
		}
		if record.ParentRunID == "fork-parent" {
			childRecords++
			childRunID = record.RunID
			assertChildSourceAssociationEmpty(t, record)
		}
	}
	if parentRecords == 0 || childRecords == 0 {
		t.Fatalf("records = %+v, want parent and child records", records)
	}
	childCheckpoint, err := childStore.Load(t.Context(), childRunID)
	if err != nil {
		t.Fatal(err)
	}
	if childCheckpoint.SourceRunID != "" || childCheckpoint.SourceEventSeq != 0 || childCheckpoint.SourceRevisionID != "" {
		t.Fatalf("child checkpoint lineage = %q/%d/%q, want empty source lineage", childCheckpoint.SourceRunID, childCheckpoint.SourceEventSeq, childCheckpoint.SourceRevisionID)
	}
}

func assertForkParentAssociation(t *testing.T, record runlog.Record) {
	t.Helper()
	if record.SourceRunID != "source-run" || record.SourceEventSeq != 1 {
		t.Fatalf("parent record lineage = %q/%d, want source-run/1", record.SourceRunID, record.SourceEventSeq)
	}
}

func assertChildSourceAssociationEmpty(t *testing.T, record runlog.Record) {
	t.Helper()
	if record.SourceRunID != "" || record.SourceEventSeq != 0 {
		t.Fatalf("child record lineage = %q/%d, want empty source association", record.SourceRunID, record.SourceEventSeq)
	}
}
