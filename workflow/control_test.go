package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

func TestWorkflowRetryCreatesNewRunFromFailedSource(t *testing.T) {
	store := NewMemoryStore()
	calls := 0
	wf := New[string, string]("retry-new-run", WithStore(store))
	node := wf.Node("node", func(_ context.Context, input string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("failed once")
		}
		return input + "-retried", nil
	})
	wf.Entry(node)
	wf.Exit(node)

	_, err := wf.Invoke(t.Context(), "input", gopact.WithSessionID("session-1"), gopact.WithRunID("source-run"))
	if err == nil {
		t.Fatal("Invoke() error = nil, want source failure")
	}
	source := nodeStartedRecord(t, store, "source-run")
	sourceFacts := loadRunFacts(t, store, "source-run")

	got, err := wf.Retry(t.Context(), RetryRequest{
		RunID: "source-run", NodeID: source.NodeID, NodeExecutionVersion: source.NodeExecutionVersion,
	}, gopact.WithRunID("retry-run"))
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if got != "input-retried" {
		t.Fatalf("Retry() = %q, want input-retried", got)
	}
	assertTerminalSourceUnchanged(t, store, "source-run", CheckpointFailed)
	assertRunFactsUnchanged(t, store, "source-run", sourceFacts)
	assertControlLineage(t, store, "retry-run", "session-1", source)

	retryRecords, err := store.List(t.Context(), runlog.Query{RunID: "retry-run"})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range retryRecords {
		if record.SourceRunID != "source-run" || record.SourceEventSeq != source.Sequence || record.SourceRevisionID != source.RevisionID {
			t.Fatalf("retry record lineage = %q/%d/%q, want source-run/%d/%q", record.SourceRunID, record.SourceEventSeq, record.SourceRevisionID, source.Sequence, source.RevisionID)
		}
		if record.EventType == EventNodeStarted && record.NodeExecutionVersion != 1 {
			t.Fatalf("retry NodeExecutionVersion = %d, want 1 in new Run", record.NodeExecutionVersion)
		}
	}
}

func TestWorkflowJumpToCreatesNewRunFromFailedSource(t *testing.T) {
	store := NewMemoryStore()
	wf := New[string, string]("jump-new-run", WithStore(store))
	node := wf.Node("node", func(_ context.Context, input string) (string, error) {
		if input == "fail" {
			return "", errors.New("failed")
		}
		return input + "!", nil
	})
	wf.Entry(node)
	wf.Exit(node)

	if _, err := wf.Invoke(t.Context(), "fail", gopact.WithSessionID("session-1"), gopact.WithRunID("source-run")); err == nil {
		t.Fatal("Invoke() error = nil, want source failure")
	}
	source := nodeStartedRecord(t, store, "source-run")
	sourceFacts := loadRunFacts(t, store, "source-run")

	got, err := wf.JumpTo(t.Context(), node, JumpRequest{
		RunID: "source-run", FromRevisionID: source.RevisionID,
	}, "jumped", gopact.WithRunID("jump-run"))
	if err != nil {
		t.Fatalf("JumpTo() error = %v", err)
	}
	if got != "jumped!" {
		t.Fatalf("JumpTo() = %q, want jumped!", got)
	}
	assertTerminalSourceUnchanged(t, store, "source-run", CheckpointFailed)
	assertRunFactsUnchanged(t, store, "source-run", sourceFacts)
	assertControlLineage(t, store, "jump-run", "session-1", source)
}

func TestWorkflowRetryGeneratesNewRunIDByDefault(t *testing.T) {
	store := NewMemoryStore()
	calls := 0
	wf := New[string, string]("retry-generated-run", WithStore(store))
	node := wf.Node("node", func(_ context.Context, input string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("failed once")
		}
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("source-run")); err == nil {
		t.Fatal("Invoke() error = nil, want source failure")
	}
	source := nodeStartedRecord(t, store, "source-run")

	if _, err := wf.Retry(t.Context(), RetryRequest{
		RunID: "source-run", NodeID: source.NodeID, NodeExecutionVersion: source.NodeExecutionVersion,
	}, gopact.WithIDGenerator(gopact.IDKindRun, func() (string, error) { return "generated-run", nil })); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	assertControlLineage(t, store, "generated-run", source.SessionID, source)
}

func TestWorkflowControlRejectsSourceRunIDReuse(t *testing.T) {
	store := NewMemoryStore()
	wf := New[string, string]("control-same-run", WithStore(store))
	node := wf.Node("node", func(context.Context, string) (string, error) {
		return "", errors.New("failed")
	})
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("source-run")); err == nil {
		t.Fatal("Invoke() error = nil, want source failure")
	}
	source := nodeStartedRecord(t, store, "source-run")

	_, err := wf.Retry(t.Context(), RetryRequest{
		RunID: "source-run", NodeID: source.NodeID, NodeExecutionVersion: source.NodeExecutionVersion,
	}, gopact.WithRunID("source-run"))
	if err == nil {
		t.Fatal("Retry() error = nil, want source RunID reuse rejection")
	}
	_, err = wf.JumpTo(t.Context(), node, JumpRequest{
		RunID: "source-run", FromRevisionID: source.RevisionID,
	}, "input", gopact.WithRunID("source-run"))
	if err == nil {
		t.Fatal("JumpTo() error = nil, want source RunID reuse rejection")
	}
	assertTerminalSourceUnchanged(t, store, "source-run", CheckpointFailed)
}

func TestWorkflowTerminalResumeReturnsCheckpointConflict(t *testing.T) {
	statuses := []CheckpointStatus{CheckpointCompleted, CheckpointFailed, CheckpointTerminated, CheckpointCanceled}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			wf, store, _ := createTerminalRun(t, status)
			before := loadRunFacts(t, store, "source-run")
			_, err := wf.Invoke(t.Context(), "ignored", WithResume(ResumeRequest{RunID: "source-run"}))
			if !errors.Is(err, ErrCheckpointConflict) {
				t.Fatalf("terminal Resume() error = %v, want ErrCheckpointConflict", err)
			}
			assertRunFactsUnchanged(t, store, "source-run", before)
		})
	}
}

func TestWorkflowTerminalResumeRejectsMalformedPendingEventWithoutWrites(t *testing.T) {
	statuses := []CheckpointStatus{CheckpointCompleted, CheckpointFailed, CheckpointTerminated, CheckpointCanceled}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			wf, store, _ := createTerminalRun(t, status)
			injectTerminalPendingEvent(t, store, status)
			before := loadRunFacts(t, store, "source-run")
			_, err := wf.Invoke(t.Context(), "ignored", WithResume(ResumeRequest{RunID: "source-run"}))
			if !errors.Is(err, ErrCheckpointConflict) {
				t.Fatalf("terminal Resume() error = %v, want ErrCheckpointConflict", err)
			}
			assertRunFactsUnchanged(t, store, "source-run", before)
		})
	}
}

func TestWorkflowTerminalResumeIgnoresInvalidPayloadAndLineageWithoutWrites(t *testing.T) {
	statuses := []CheckpointStatus{CheckpointCompleted, CheckpointFailed, CheckpointTerminated, CheckpointCanceled}
	corruptions := []struct {
		name   string
		mutate func(*CheckpointRecord)
	}{
		{name: "corrupt payload", mutate: func(record *CheckpointRecord) { record.Payload = []byte("{") }},
		{name: "source mismatch", mutate: func(record *CheckpointRecord) {
			record.SourceRunID, record.SourceEventSeq, record.SourceRevisionID = "other-run", 7, "other-revision"
		}},
	}
	for _, status := range statuses {
		for _, corruption := range corruptions {
			t.Run(string(status)+"/"+corruption.name, func(t *testing.T) {
				wf, store, _ := createTerminalRun(t, status)
				record, err := store.Load(t.Context(), "source-run")
				if err != nil {
					t.Fatal(err)
				}
				corruption.mutate(&record)
				store.MemoryCheckpointer.restore(record)
				before := loadRunFacts(t, store, "source-run")

				_, err = wf.Invoke(t.Context(), "ignored", WithResume(ResumeRequest{RunID: "source-run"}))
				if !errors.Is(err, ErrCheckpointConflict) {
					t.Fatalf("terminal Resume() error = %v, want ErrCheckpointConflict", err)
				}
				assertRunFactsUnchanged(t, store, "source-run", before)
			})
		}
	}
}

func injectTerminalPendingEvent(t *testing.T, store *MemoryStore, status CheckpointStatus) {
	t.Helper()
	record, err := store.Load(t.Context(), "source-run")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decodeCheckpointPayload[string](record.Payload)
	if err != nil {
		t.Fatal(err)
	}
	sequence := record.ConfirmedSequence + 1
	meta := payload.meta()
	meta.PendingTerm = status
	meta.PendingEvent = &gopact.Event{Sequence: sequence, Type: EventWorkflowCompleted}
	record.Payload, err = encodeCheckpointPayloadWithMeta(payload.state(), payload.Outputs, payload.NextStep, meta)
	if err != nil {
		t.Fatal(err)
	}
	record.PendingSequence = sequence
	store.MemoryCheckpointer.restore(record)
}

func TestWorkflowRetryAndJumpRejectNonFailedTerminalSource(t *testing.T) {
	statuses := []CheckpointStatus{CheckpointCompleted, CheckpointTerminated, CheckpointCanceled}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			wf, store, node := createTerminalRun(t, status)
			source := nodeStartedRecord(t, store, "source-run")

			_, retryErr := wf.Retry(t.Context(), RetryRequest{
				RunID: "source-run", NodeID: source.NodeID, NodeExecutionVersion: source.NodeExecutionVersion,
			}, gopact.WithRunID("retry-run"))
			if retryErr == nil {
				t.Fatal("Retry() error = nil, want non-failed terminal rejection")
			}
			_, jumpErr := wf.JumpTo(t.Context(), node, JumpRequest{
				RunID: "source-run", FromRevisionID: source.RevisionID,
			}, "jumped", gopact.WithRunID("jump-run"))
			if jumpErr == nil {
				t.Fatal("JumpTo() error = nil, want non-failed terminal rejection")
			}
		})
	}
}

func createTerminalRun(t *testing.T, status CheckpointStatus) (*Workflow[string, string], *MemoryStore, *Node[string, string]) {
	t.Helper()
	store := NewMemoryStore()
	started := make(chan struct{})
	wf := New[string, string]("terminal-"+string(status), WithStore(store))
	node := wf.Node("node", func(ctx context.Context, input string) (string, error) {
		switch status {
		case CheckpointFailed:
			return "", errors.New("failed")
		case CheckpointTerminated, CheckpointCanceled:
			return waitForTerminalCancellation(ctx, started)
		default:
			return input, nil
		}
	})
	wf.Entry(node)
	wf.Exit(node)
	if status == CheckpointCompleted || status == CheckpointFailed {
		_, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("source-run"))
		if status == CheckpointCompleted && err != nil {
			t.Fatal(err)
		}
		if status == CheckpointFailed && err == nil {
			t.Fatal("Invoke() error = nil, want failure")
		}
	} else {
		finishActiveTerminalRun(t, wf, started, status)
	}
	assertTerminalSourceUnchanged(t, store, "source-run", status)
	return wf, store, node
}

func waitForTerminalCancellation(ctx context.Context, started chan<- struct{}) (string, error) {
	close(started)
	<-ctx.Done()
	return "", ctx.Err()
}

func finishActiveTerminalRun(t *testing.T, wf *Workflow[string, string], started <-chan struct{}, status CheckpointStatus) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := wf.Invoke(ctx, "input", gopact.WithRunID("source-run"))
		done <- err
	}()
	<-started
	if status == CheckpointTerminated {
		if err := wf.Terminate("source-run"); err != nil {
			t.Fatal(err)
		}
	} else {
		cancel()
	}
	if err := <-done; err == nil {
		t.Fatal("terminal Invoke() error = nil")
	}
	cancel()
}

func nodeStartedRecord(t *testing.T, store *MemoryStore, runID string) runlog.Record {
	t.Helper()
	records, err := store.List(t.Context(), runlog.Query{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.EventType == EventNodeStarted {
			return record
		}
	}
	t.Fatalf("node started record for %q not found", runID)
	return runlog.Record{}
}

func assertTerminalSourceUnchanged(t *testing.T, store *MemoryStore, runID string, expected CheckpointStatus) {
	t.Helper()
	record, err := store.Load(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != expected {
		t.Fatalf("source status = %q, want %q", record.Status, expected)
	}
}

func assertControlLineage(t *testing.T, store *MemoryStore, runID, sessionID string, source runlog.Record) {
	t.Helper()
	record, err := store.Load(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if record.SessionID != sessionID || record.RunID != runID || record.SourceRunID != source.RunID ||
		record.SourceEventSeq != source.Sequence || record.SourceRevisionID != source.RevisionID {
		t.Fatalf("checkpoint lineage = %+v, want session/run/source lineage", record)
	}
}

type runFacts struct {
	head        CheckpointRecord
	checkpoints []CheckpointRecord
	records     []runlog.Record
}

func loadRunFacts(t *testing.T, store *MemoryStore, runID string) runFacts {
	t.Helper()
	head, err := store.Load(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoints, err := store.ListCheckpoints(t.Context(), CheckpointHistoryRequest{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	return runFacts{head: head, checkpoints: checkpoints, records: records}
}

func assertRunFactsUnchanged(t *testing.T, store *MemoryStore, runID string, expected runFacts) {
	t.Helper()
	if actual := loadRunFacts(t, store, runID); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("source run facts changed\nactual:   %+v\nexpected: %+v", actual, expected)
	}
}
