package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type controlStarter func(*testing.T, *Workflow[string, string], *Node[string, string], *MemoryStore, runlog.Record) error

func TestSourceLineageSurvivesInterruptResume(t *testing.T) {
	tests := []struct {
		name  string
		start controlStarter
	}{
		{
			name: "retry",
			start: func(t *testing.T, wf *Workflow[string, string], _ *Node[string, string], _ *MemoryStore, source runlog.Record) error {
				t.Helper()
				_, err := wf.Retry(t.Context(), RetryRequest{
					RunID: "source-run", NodeID: source.NodeID, NodeExecutionVersion: source.NodeExecutionVersion,
				}, gopact.WithRunID("target-run"))
				return err
			},
		},
		{
			name: "jump",
			start: func(t *testing.T, wf *Workflow[string, string], node *Node[string, string], _ *MemoryStore, source runlog.Record) error {
				t.Helper()
				_, err := wf.JumpTo(t.Context(), node, JumpRequest{
					RunID: "source-run", FromRevisionID: source.RevisionID,
				}, "jumped", gopact.WithRunID("target-run"))
				return err
			},
		},
		{
			name: "fork",
			start: func(t *testing.T, wf *Workflow[string, string], _ *Node[string, string], store *MemoryStore, _ runlog.Record) error {
				t.Helper()
				snapshot, err := NewRunLogSnapshotStore(store, store).Load(t.Context(), SnapshotRequest{RunID: "source-run"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = snapshot.Fork(t.Context(), wf, ForkRequest{
					SourceRunID: "source-run", FromEventSeq: 1,
					Patch: ForkPatch{WorkflowInput: &InputPatch{Value: "forked"}},
				}, gopact.WithRunID("target-run"))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSourceLineageSurvivesInterruptResume(t, tt.name, tt.start)
		})
	}
}

func TestExternalAssociatingSinkRestoresSourceLineageOnResume(t *testing.T) {
	store := NewMemoryStore()
	external := runlog.NewMemoryLog()
	associations := 0
	sink := countingAssociatingSink{log: external, count: &associations}
	interruptEnabled := false
	wf := New[string, string]("external-lineage-resume", WithStrictCheckpointer(store), WithStrictJournal(store))
	node := wf.Node("node", func(_ context.Context, input string) (string, error) {
		if !interruptEnabled {
			return "", errors.New("source failed")
		}
		return input, nil
	})
	node.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			if interruptEnabled {
				return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1"}}, nil
			}
			return GuardAllow[string, string]{}, nil
		},
	)))
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("source-run")); err == nil {
		t.Fatal("source Invoke() error = nil, want failure")
	}
	source := nodeStartedRecord(t, store, "source-run")
	interruptEnabled = true

	_, err := wf.Retry(t.Context(), RetryRequest{
		RunID: "source-run", NodeID: source.NodeID, NodeExecutionVersion: source.NodeExecutionVersion,
	}, gopact.WithRunID("target-run"), gopact.WithEventSink(sink))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Retry() error = %v, want InterruptError", err)
	}
	if associations != 1 {
		t.Fatalf("initial Associate() calls = %d, want 1", associations)
	}
	associations = 0
	if _, err := wf.Invoke(t.Context(), "ignored",
		WithResume(ResumeRequest{
			RunID:       "target-run",
			Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
		}),
		gopact.WithEventSink(sink),
	); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if associations != 1 {
		t.Fatalf("resume Associate() calls = %d, want 1", associations)
	}

	records, err := external.List(t.Context(), runlog.Query{RunID: "target-run"})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.SourceRunID != "source-run" || record.SourceEventSeq != source.Sequence ||
			record.SourceRevisionID != source.RevisionID {
			t.Fatalf("external record %d lineage = %q/%d/%q, want source-run/%d/%q", record.Sequence, record.SourceRunID, record.SourceEventSeq, record.SourceRevisionID, source.Sequence, source.RevisionID)
		}
	}
	if _, err := NewRunLogSnapshotStore(external, store).Load(t.Context(), SnapshotRequest{RunID: "target-run"}); err != nil {
		t.Fatalf("Snapshot Load() error = %v", err)
	}
}

func testSourceLineageSurvivesInterruptResume(t *testing.T, mode string, start controlStarter) {
	t.Helper()
	store := NewMemoryStore()
	interruptEnabled := false
	wf := New[string, string]("lineage-resume-"+mode, WithStrictCheckpointer(store), WithStrictJournal(store))
	node := wf.Node("node", func(_ context.Context, input string) (string, error) {
		if input == "fail" && !interruptEnabled {
			return "", errors.New("source failed")
		}
		return input + "!", nil
	})
	node.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			if interruptEnabled {
				return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1"}}, nil
			}
			return GuardAllow[string, string]{}, nil
		},
	)))
	wf.Entry(node)
	wf.Exit(node)

	sourceInput := "fail"
	if mode == "fork" {
		sourceInput = "source"
	}
	_, sourceErr := wf.Invoke(t.Context(), sourceInput,
		gopact.WithSessionID("session-1"), gopact.WithRunID("source-run"))
	if mode == "fork" && sourceErr != nil {
		t.Fatal(sourceErr)
	}
	if mode != "fork" && sourceErr == nil {
		t.Fatal("source Invoke() error = nil, want failed source")
	}
	source := nodeStartedRecord(t, store, "source-run")
	interruptEnabled = true

	var interrupted InterruptError
	if err := start(t, wf, node, store, source); !errors.As(err, &interrupted) {
		t.Fatalf("start error = %v, want InterruptError", err)
	}
	if _, err := wf.Invoke(t.Context(), "ignored", WithResume(ResumeRequest{
		RunID: "target-run",
		Resolutions: []InterruptResolution{{
			InterruptID: "approval-1", PayloadRef: "artifact://approved",
		}},
	})); err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}

	expectedSequence := source.Sequence
	expectedRevision := source.RevisionID
	if mode == "fork" {
		expectedSequence = 1
		expectedRevision = ""
	}
	assertPersistedSourceLineage(t, store, "target-run", expectedSequence, expectedRevision)
}

func assertPersistedSourceLineage(t *testing.T, store *MemoryStore, runID string, sourceSeq int64, sourceRev string) {
	t.Helper()
	checkpoint, err := store.Load(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.SourceRunID != "source-run" || checkpoint.SourceEventSeq != sourceSeq ||
		checkpoint.SourceRevisionID != sourceRev {
		t.Fatalf("checkpoint lineage = %q/%d/%q, want source-run/%d/%q", checkpoint.SourceRunID, checkpoint.SourceEventSeq, checkpoint.SourceRevisionID, sourceSeq, sourceRev)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	resumed := false
	for _, record := range records {
		if record.EventType == EventCheckpointLoaded {
			resumed = true
		}
		if record.SourceRunID != "source-run" || record.SourceEventSeq != sourceSeq ||
			record.SourceRevisionID != sourceRev {
			t.Fatalf("record %d (%s) lineage = %q/%d/%q, want source-run/%d/%q", record.Sequence, record.EventType, record.SourceRunID, record.SourceEventSeq, record.SourceRevisionID, sourceSeq, sourceRev)
		}
	}
	if !resumed {
		t.Fatal("checkpoint.loaded record not found")
	}
}
