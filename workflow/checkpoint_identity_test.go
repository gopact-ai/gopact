package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowResumeRestoresCheckpointSessionID(t *testing.T) {
	wf, store := interruptedSessionWorkflow(t, "restore-session")
	interruptSessionWorkflow(t, wf, "run-1", "session-1")

	info, err := wf.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID:       "run-1",
		Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if info.SessionID != "session-1" || info.RunID != "run-1" {
		t.Fatalf("resumed RunInfo = %+v, want session-1/run-1", info)
	}
	record, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if record.SessionID != "session-1" || record.RunID != "run-1" || record.Status != CheckpointCompleted {
		t.Fatalf("final checkpoint = %+v, want completed session-1/run-1", record)
	}
}

func TestWorkflowResumeRejectsExplicitSessionIDMismatchBeforeMutation(t *testing.T) {
	wf, store := interruptedSessionWorkflow(t, "reject-session")
	interruptSessionWorkflow(t, wf, "run-1", "session-1")
	before, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	_, err = wf.Invoke(context.Background(), "ignored",
		WithResume(ResumeRequest{
			RunID:       "run-1",
			Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
		}),
		gopact.WithSessionID("session-2"),
	)
	if !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("resume Invoke() error = %v, want ErrCheckpointMismatch", err)
	}
	after, loadErr := store.Load(context.Background(), "run-1")
	if loadErr != nil {
		t.Fatalf("Load() after mismatch error = %v", loadErr)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("checkpoint mutated after mismatch:\n before = %+v\n after  = %+v", before, after)
	}
}

func TestWorkflowResumeAcceptsMatchingExplicitSessionID(t *testing.T) {
	wf, _ := interruptedSessionWorkflow(t, "matching-session")
	interruptSessionWorkflow(t, wf, "run-1", "session-1")

	info, err := wf.Invoke(context.Background(), "ignored",
		WithResume(ResumeRequest{
			RunID:       "run-1",
			Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
		}),
		gopact.WithSessionID("session-1"),
	)
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if info.SessionID != "session-1" || info.RunID != "run-1" {
		t.Fatalf("resumed RunInfo = %+v, want session-1/run-1", info)
	}
}

func TestWorkflowResumeRejectsCheckpointSourceLineageMismatchBeforeMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CheckpointRecord)
	}{
		{name: "source run", mutate: func(record *CheckpointRecord) { record.SourceRunID = "other" }},
		{name: "source event", mutate: func(record *CheckpointRecord) { record.SourceEventSeq = 1 }},
		{name: "source revision", mutate: func(record *CheckpointRecord) { record.SourceRevisionID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wf, store := interruptedSessionWorkflow(t, "source-lineage-"+strings.ReplaceAll(test.name, " ", "-"))
			interruptSessionWorkflow(t, wf, "run-1", "session-1")
			record, err := store.Load(t.Context(), "run-1")
			if err != nil {
				t.Fatal(err)
			}
			payload, err := decodeCheckpointPayload[RunInfo](record.Payload)
			if err != nil {
				t.Fatal(err)
			}
			meta := payload.meta()
			meta.SourceRunID, meta.SourceEventSeq, meta.SourceRevisionID = "source-run", 7, "source-revision"
			record.Payload, err = encodeCheckpointPayloadWithMeta(payload.state(), payload.Outputs, payload.NextStep, meta)
			if err != nil {
				t.Fatal(err)
			}
			record.SourceRunID, record.SourceEventSeq, record.SourceRevisionID = "source-run", 7, "source-revision"
			test.mutate(&record)
			store.restore(record)
			before, err := store.Load(t.Context(), "run-1")
			if err != nil {
				t.Fatal(err)
			}

			_, err = wf.Invoke(t.Context(), "ignored", WithResume(ResumeRequest{RunID: "run-1"}))
			if !errors.Is(err, ErrCheckpointMismatch) {
				t.Fatalf("Resume() error = %v, want ErrCheckpointMismatch", err)
			}
			after, loadErr := store.Load(t.Context(), "run-1")
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("checkpoint mutated after lineage mismatch\nbefore: %+v\nafter: %+v", before, after)
			}
		})
	}
}

func TestWorkflowResumeRejectsSchemaV1Checkpoint(t *testing.T) {
	wf, store := interruptedSessionWorkflow(t, "schema-v1")
	interruptSessionWorkflow(t, wf, "run-1", "session-1")
	record, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	record.SchemaVersion = checkpointSchemaVersion - 1
	store.restore(record)
	wf = newInterruptedSessionWorkflow("schema-v1", store)

	_, err = wf.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID:       "run-1",
		Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
	}))
	if !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("resume Invoke() error = %v, want ErrCheckpointMismatch", err)
	}
}

func TestWorkflowResumeRejectsCheckpointWithoutSessionID(t *testing.T) {
	wf, store := interruptedSessionWorkflow(t, "missing-session")
	interruptSessionWorkflow(t, wf, "run-1", "session-1")
	record, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	record.SessionID = ""
	store.restore(record)
	wf = newInterruptedSessionWorkflow("missing-session", store)

	_, err = wf.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID:       "run-1",
		Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
	}))
	if !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("resume Invoke() error = %v, want ErrCheckpointMismatch", err)
	}
}

func TestWorkflowCheckpointedAgentContextSurvivesInterrupt(t *testing.T) {
	store := NewMemoryCheckpointer()
	buildCalls := 0
	wf := New[string, string]("checkpointed-agent-context", WithCheckpointer(store))
	build := wf.Node("build", func(_ context.Context, input string) (gopact.ModelRequest, error) {
		buildCalls++
		return gopact.ModelRequest{Messages: []gopact.Message{gopact.UserMessage(input)}}, nil
	})
	approve := wf.Node("approve", func(_ context.Context, request gopact.ModelRequest) (string, error) {
		return request.Messages[0].Parts[0].Text, nil
	})
	approve.Guard(BeforeRun("approval", GuardFunc[gopact.ModelRequest, string](
		func(context.Context, GuardContext[gopact.ModelRequest, string]) (GuardDecision[gopact.ModelRequest, string], error) {
			return GuardInterrupt[gopact.ModelRequest, string]{Request: InterruptRequest{ID: "approval-1"}}, nil
		},
	)))
	wf.Entry(build)
	wf.Edge(build, approve)
	wf.Exit(approve)

	_, err := wf.Invoke(context.Background(), "original user text",
		gopact.WithRunID("run-1"), gopact.WithSessionID("session-1"))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	got, err := wf.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID:       "run-1",
		Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "original user text" {
		t.Fatalf("resume Invoke() = %q, want original user text", got)
	}
	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", buildCalls)
	}
}

func interruptedSessionWorkflow(t *testing.T, name string) (*Workflow[string, RunInfo], *MemoryCheckpointer) {
	t.Helper()
	store := NewMemoryCheckpointer()
	return newInterruptedSessionWorkflow(name, store), store
}

func newInterruptedSessionWorkflow(name string, store Checkpointer) *Workflow[string, RunInfo] {
	wf := New[string, RunInfo](name, WithCheckpointer(store))
	node := wf.Node("node", func(ctx context.Context, _ string) (RunInfo, error) {
		return RunInfoFromContext(ctx), nil
	})
	node.Guard(BeforeRun("approval", GuardFunc[string, RunInfo](
		func(context.Context, GuardContext[string, RunInfo]) (GuardDecision[string, RunInfo], error) {
			return GuardInterrupt[string, RunInfo]{Request: InterruptRequest{ID: "approval-1"}}, nil
		},
	)))
	wf.Entry(node)
	wf.Exit(node)
	return wf
}

func interruptSessionWorkflow(t *testing.T, wf *Workflow[string, RunInfo], runID, sessionID string) {
	t.Helper()
	_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID(runID), gopact.WithSessionID(sessionID))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
}

func TestWorkflowResumeRejectsTopologyVersionMismatchBeforeExecution(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	createPendingIdentityCheckpoint(t, store, pendingCheckpointRequest{workflow: "topology", node: "first", runID: "topology-run"})
	expireRecordingLease(t, store, "topology-run")
	bodyRuns := 0
	second := New[int, int]("topology", WithCheckpointer(store))
	secondNode := second.Node("second", func(_ context.Context, input int) (int, error) {
		bodyRuns++
		return input, nil
	})
	second.Entry(secondNode)
	second.Exit(secondNode)
	secondCompiled, err := second.compile()
	if err != nil {
		t.Fatalf("second Compile() error = %v", err)
	}
	_, err = secondCompiled.Invoke(context.Background(), 1, WithResume(ResumeRequest{RunID: "topology-run"}))
	if !errors.Is(err, ErrCheckpointMismatch) || !strings.Contains(err.Error(), "topology version") {
		t.Fatalf("resume error = %v, want ErrCheckpointMismatch for topology version", err)
	}
	if bodyRuns != 0 {
		t.Fatalf("second body runs = %d, want 0", bodyRuns)
	}
}

func TestWorkflowResumeRejectsCheckpointSchemaMismatchBeforeExecution(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	bodyRuns := 0
	createPendingIdentityCheckpoint(t, store, pendingCheckpointRequest{
		workflow: "schema", node: "node", runID: "schema-run",
		run: func(_ context.Context, input int) (int, error) {
			bodyRuns++
			return input, nil
		},
	})
	record := store.records["schema-run"]
	payload, err := decodeCheckpointPayload[int](record.Payload)
	if err != nil {
		t.Fatalf("decodeCheckpointPayload() error = %v", err)
	}
	meta := payload.meta()
	meta.SchemaVersion++
	meta.LeaseExpiresAt = time.Now().Add(-time.Second)
	record.Payload, err = encodeCheckpointPayloadWithMeta(payload.state(), payload.Outputs, payload.NextStep, meta)
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	store.records["schema-run"] = record
	wf := New[int, int]("schema", WithCheckpointer(store))
	node := wf.Node("node", func(_ context.Context, input int) (int, error) {
		bodyRuns++
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), 1, WithResume(ResumeRequest{RunID: "schema-run"}))
	if !errors.Is(err, ErrCheckpointMismatch) || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("resume error = %v, want ErrCheckpointMismatch for schema version", err)
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs = %d, want 0", bodyRuns)
	}
}

func TestWorkflowResumeRejectsExplicitTopologyVersionMismatch(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	createPendingIdentityCheckpoint(t, store, pendingCheckpointRequest{
		workflow: "versioned", node: "node", runID: "versioned-run", options: []BuildOption{WithTopologyVersion("v1")},
	})
	expireRecordingLease(t, store, "versioned-run")
	bodyRuns := 0
	wf := New[int, int]("versioned", WithCheckpointer(store), WithTopologyVersion("v2"))
	node := wf.Node("node", func(_ context.Context, input int) (int, error) {
		bodyRuns++
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), 1, WithResume(ResumeRequest{RunID: "versioned-run"}))
	if !errors.Is(err, ErrCheckpointMismatch) || !strings.Contains(err.Error(), "topology version") {
		t.Fatalf("resume error = %v, want ErrCheckpointMismatch for explicit topology version", err)
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs = %d, want 0", bodyRuns)
	}
}

func TestWorkflowCompileRejectsEmptyTopologyVersion(t *testing.T) {
	wf := New[int, int]("empty-version", WithTopologyVersion(""))
	node := wf.Node("node", func(_ context.Context, input int) (int, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	_, err := wf.compile()
	if err == nil || !strings.Contains(err.Error(), "topology version") {
		t.Fatalf("Compile() error = %v, want empty topology version", err)
	}
}

type pendingCheckpointRequest struct {
	workflow string
	node     string
	runID    string
	options  []BuildOption
	run      func(context.Context, int) (int, error)
}

func createPendingIdentityCheckpoint(t *testing.T, store *recordingCheckpointer, req pendingCheckpointRequest) *compiled[int, int] {
	t.Helper()
	options := append([]BuildOption{WithCheckpointer(store)}, req.options...)
	wf := New[int, int](req.workflow, options...)
	run := req.run
	if run == nil {
		run = func(_ context.Context, input int) (int, error) { return input, nil }
	}
	node := wf.Node(req.node, run)
	wf.Entry(node)
	wf.Exit(node)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	sinkErr := errors.New("sink failed")
	_, err = compiled.Invoke(context.Background(), 1, gopact.WithRunID(req.runID), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeStarted {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	return compiled
}
