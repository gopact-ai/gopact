package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

func TestWorkflowInvokeDirectly(t *testing.T) {
	wf := New[string, int]("direct")
	length := testNode(wf, "length", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(length)
	wf.Exit(length)

	got, err := wf.Invoke(context.Background(), "gopact")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 6 {
		t.Fatalf("Invoke() = %d, want 6", got)
	}
}

func TestInterruptBatchResumesAfterStrictObserverFailureWithoutRerunningGuard(t *testing.T) {
	store := NewMemoryStore()
	guardRuns := 0
	build := func() *Workflow[string, string] {
		wf := New[string, string]("durable-interrupt-batch", WithStore(store))
		wait := testNode(wf, "wait", func(_ context.Context, input string) (string, error) { return input, nil })
		wait.Guard(BeforeRun("approval", GuardFunc[string, string](func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			guardRuns++
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1", Subject: "approve"}}, nil
		})))
		wf.Entry(wait)
		wf.Exit(wait)
		return wf
	}

	observerFailed := false
	_, err := build().Invoke(context.Background(), "input", gopact.WithRunID("durable-interrupt-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventGuardInterrupted && !observerFailed {
				observerFailed = true
				return errors.New("observer unavailable")
			}
			return nil
		}))
	if err == nil || !strings.Contains(err.Error(), "observer unavailable") {
		t.Fatalf("first Invoke() error = %v, want observer failure", err)
	}

	_, err = build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "durable-interrupt-run"}))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("resume Invoke() error = %v, want InterruptError", err)
	}
	if guardRuns != 1 {
		t.Fatalf("guard runs = %d, want 1", guardRuns)
	}
	records, err := store.List(context.Background(), runlog.Query{RunID: "durable-interrupt-run"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	counts := map[string]int{}
	for _, record := range records {
		counts[record.EventType]++
	}
	if counts[EventGuardInterrupted] != 1 || counts[EventWorkflowInterrupted] != 1 {
		t.Fatalf("interrupt event counts = %v, want exactly one of each", counts)
	}
	checkpoint, err := store.Load(context.Background(), "durable-interrupt-run")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if checkpoint.Status != CheckpointInterrupted || checkpoint.OwnerID != "" {
		t.Fatalf("checkpoint = status %q owner %q, want interrupted and unowned", checkpoint.Status, checkpoint.OwnerID)
	}
}

func TestWorkflowInvokeDirectlyConcurrent(t *testing.T) {
	wf := New[string, int]("direct-concurrent")
	length := testNode(wf, "length", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(length)
	wf.Exit(length)

	const invocations = 16
	errs := make(chan error, invocations)
	var calls sync.WaitGroup
	for range invocations {
		calls.Add(1)
		go func() {
			defer calls.Done()
			got, err := wf.Invoke(context.Background(), "gopact")
			if err != nil {
				errs <- err
				return
			}
			if got != 6 {
				errs <- fmt.Errorf("Invoke() = %d, want 6", got)
			}
		}()
	}
	calls.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestWorkflowRunInfoCarriesSessionID(t *testing.T) {
	wf := New[string, RunInfo]("session-info")
	info := testNode(wf, "info", func(ctx context.Context, _ string) (RunInfo, error) {
		return RunInfoFromContext(ctx), nil
	})
	wf.Entry(info)
	wf.Exit(info)

	generated, err := wf.Invoke(context.Background(), "input")
	if err != nil {
		t.Fatalf("generated Invoke() error = %v", err)
	}
	if generated.SessionID == "" || generated.RunID == "" {
		t.Fatalf("generated RunInfo = %+v, want session and run IDs", generated)
	}
	for prefix, id := range map[string]string{
		"session-":  generated.SessionID,
		"workflow-": generated.RunID,
	} {
		value, ok := strings.CutPrefix(id, prefix)
		if !ok {
			t.Fatalf("generated ID %q does not have prefix %q", id, prefix)
		}
		if _, err := uuid.Parse(value); err != nil {
			t.Fatalf("generated ID %q does not contain a UUID: %v", id, err)
		}
	}

	explicit, err := wf.Invoke(context.Background(), "input", gopact.WithSessionID("session-explicit"))
	if err != nil {
		t.Fatalf("explicit Invoke() error = %v", err)
	}
	if explicit.SessionID != "session-explicit" {
		t.Fatalf("explicit SessionID = %q, want session-explicit", explicit.SessionID)
	}
}

func TestWorkflowUsesCustomIDGenerator(t *testing.T) {
	generated := map[gopact.IDKind]string{
		IDKindSession: "custom-session",
		IDKindRun:     "custom-run",
		IDKindOwner:   "custom-owner",
	}
	calls := map[gopact.IDKind]int{}
	generator := func(kind gopact.IDKind) gopact.IDGenerator {
		return func() (string, error) {
			calls[kind]++
			return generated[kind], nil
		}
	}
	wf := New[string, RunInfo]("custom-ids",
		WithIDGenerator(IDKindSession, generator(IDKindSession)),
		WithIDGenerator(IDKindRun, generator(IDKindRun)),
		WithIDGenerator(IDKindOwner, generator(IDKindOwner)),
	)
	info := testNode(wf, "info", func(ctx context.Context, _ string) (RunInfo, error) {
		return RunInfoFromContext(ctx), nil
	})
	wf.Entry(info)
	wf.Exit(info)

	got, err := wf.Invoke(context.Background(), "input")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got.SessionID != generated[IDKindSession] || got.RunID != generated[IDKindRun] {
		t.Fatalf("RunInfo = %+v, want generated session and run IDs", got)
	}
	for _, kind := range []gopact.IDKind{IDKindSession, IDKindRun, IDKindOwner} {
		if calls[kind] != 1 {
			t.Fatalf("generator calls for %q = %d, want 1", kind, calls[kind])
		}
	}

	_, err = wf.Invoke(
		context.Background(),
		"input",
		gopact.WithSessionID("explicit-session"),
		gopact.WithRunID("explicit-run"),
	)
	if err != nil {
		t.Fatalf("explicit Invoke() error = %v", err)
	}
	if calls[IDKindSession] != 1 || calls[IDKindRun] != 1 || calls[IDKindOwner] != 2 {
		t.Fatalf("generator calls after explicit IDs = %+v, want only owner called again", calls)
	}
}

func TestWorkflowRunIDGeneratorOverridesOneWorkflowGenerator(t *testing.T) {
	workflowRunCalls := 0
	runOptionCalls := 0
	wf := New[string, RunInfo]("run-id-override",
		WithIDGenerator(IDKindSession, func() (string, error) { return "session-global", nil }),
		WithIDGenerator(IDKindRun, func() (string, error) {
			workflowRunCalls++
			return "run-global", nil
		}),
	)
	info := testNode(wf, "info", func(ctx context.Context, _ string) (RunInfo, error) {
		return RunInfoFromContext(ctx), nil
	})
	wf.Entry(info)
	wf.Exit(info)

	got, err := wf.Invoke(context.Background(), "input", gopact.WithIDGenerator(IDKindRun, func() (string, error) {
		runOptionCalls++
		return "run-local", nil
	}))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got.SessionID != "session-global" || got.RunID != "run-local" {
		t.Fatalf("RunInfo = %+v, want workflow session and per-run run ID", got)
	}
	if workflowRunCalls != 0 || runOptionCalls != 1 {
		t.Fatalf("generator calls = workflow:%d run-option:%d, want 0/1", workflowRunCalls, runOptionCalls)
	}
}

func TestWorkflowIDGeneratorRejectsInvalidID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "too long", id: strings.Repeat("x", 192)},
		{name: "invalid utf8", id: string([]byte{0xff})},
		{name: "nul", id: "invalid\x00id"},
		{name: "trailing space", id: "invalid "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wf := New[string, string]("invalid-id", WithIDGenerator(IDKindSession, func() (string, error) {
				return test.id, nil
			}))
			node := testNode(wf, "node", func(_ context.Context, input string) (string, error) {
				return input, nil
			})
			wf.Entry(node)
			wf.Exit(node)

			if _, err := wf.Invoke(context.Background(), "input"); err == nil ||
				!strings.Contains(err.Error(), "generate session id") {
				t.Fatalf("Invoke() error = %v, want invalid generated session ID", err)
			}
		})
	}
}

func TestWorkflowIDGeneratorReturnsError(t *testing.T) {
	generatorErr := errors.New("id source unavailable")
	wf := New[string, string]("generator-error", WithIDGenerator(IDKindSession, func() (string, error) {
		return "", generatorErr
	}))
	node := testNode(wf, "node", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)

	if _, err := wf.Invoke(context.Background(), "input"); !errors.Is(err, generatorErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, generatorErr)
	}
}

func TestWorkflowCompileRejectsNilIDGenerator(t *testing.T) {
	wf := New[string, string]("nil-generator", WithIDGenerator(IDKindRun, nil))
	node := testNode(wf, "node", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)

	if err := wf.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want nil ID generator error")
	}
}

func TestWorkflowConcurrentInvocationsGenerateUniqueSessionIDs(t *testing.T) {
	wf := New[string, RunInfo]("concurrent-sessions")
	info := testNode(wf, "info", func(ctx context.Context, _ string) (RunInfo, error) {
		return RunInfoFromContext(ctx), nil
	})
	wf.Entry(info)
	wf.Exit(info)

	const invocations = 64
	type result struct {
		info RunInfo
		err  error
	}
	results := make(chan result, invocations)
	var calls sync.WaitGroup
	for range invocations {
		calls.Go(func() {
			got, invokeErr := wf.Invoke(context.Background(), "input")
			results <- result{info: got, err: invokeErr}
		})
	}
	calls.Wait()
	close(results)

	sessionIDs := make(map[string]struct{}, invocations)
	for result := range results {
		if result.err != nil {
			t.Fatalf("Invoke() error = %v", result.err)
		}
		if result.info.SessionID == "" {
			t.Fatal("SessionID = empty")
		}
		if _, exists := sessionIDs[result.info.SessionID]; exists {
			t.Fatalf("duplicate SessionID = %q", result.info.SessionID)
		}
		sessionIDs[result.info.SessionID] = struct{}{}
	}
	if len(sessionIDs) != invocations {
		t.Fatalf("unique SessionIDs = %d, want %d", len(sessionIDs), invocations)
	}
}

func TestWorkflowInvokeExecutesTypedNodes(t *testing.T) {
	wf := New[string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "gopact")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 6 {
		t.Fatalf("Invoke() = %d, want 6", got)
	}
}

func TestWorkflowInvokeFollowsEdges(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	report := testNode(wf, "report", func(_ context.Context, input int) (string, error) {
		return "len=" + string(rune('0'+input)), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "len=3" {
		t.Fatalf("Invoke() = %q, want len=3", got)
	}
}

func TestWorkflowEdgeIsIdempotent(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	reportRuns := 0
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		reportRuns++
		return input + "!", nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "abc!" || reportRuns != 1 {
		t.Fatalf("Invoke() = %q, report runs = %d, want abc! and 1", got, reportRuns)
	}
}

func TestWorkflowAddInvokable(t *testing.T) {
	wf := New[string, int]("example")
	plan := testInvokable(wf, "plan", gopact.InvokableFunc[string, int](
		func(_ context.Context, input string, _ ...gopact.RunOption) (int, error) {
			return len(input), nil
		},
	))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "workflow")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 8 {
		t.Fatalf("Invoke() = %d, want 8", got)
	}
}

func TestWorkflowInvokeEmitsEvents(t *testing.T) {
	wf := New[string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	var got []string
	_, err = compiled.Invoke(context.Background(), "abc", gopact.WithStrictEventSink(gopact.EventSinkFunc(
		func(_ context.Context, event gopact.Event) error {
			got = append(got, event.Type)
			return nil
		},
	)))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	want := []string{
		EventWorkflowStarted,
		EventNodeStarted,
		EventNodeCompleted,
		EventWorkflowCompleted,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestWorkflowBestEffortEventSinkDoesNotFailRun(t *testing.T) {
	wf := New[string, int]("best-effort-events")
	length := testNode(wf, "length", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(length)
	wf.Exit(length)

	got, err := wf.Invoke(context.Background(), "gopact", gopact.WithEventHandler(
		func(context.Context, gopact.Event) error { return errors.New("sink failed") },
	))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 6 {
		t.Fatalf("Invoke() = %d, want 6", got)
	}
}

func TestWorkflowUsesMemoryCheckpointerByDefault(t *testing.T) {
	wf := New[string, string]("default-memory")
	echo := testNode(wf, "echo", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(echo)
	wf.Exit(echo)

	sinkErr := errors.New("sink failed")
	_, err := wf.Invoke(context.Background(), "value",
		gopact.WithRunID("default-memory-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeCompleted {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("first Invoke() error = %v, want %v", err, sinkErr)
	}

	got, err := wf.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "default-memory-run"}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "value" {
		t.Fatalf("resume Invoke() = %q, want value", got)
	}
}

func TestWorkflowDefaultMemoryStoreProvidesSnapshot(t *testing.T) {
	wf := New[string, string]("default-history")
	echo := testNode(wf, "echo", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(echo)
	wf.Exit(echo)

	if _, err := wf.Invoke(context.Background(), "value", gopact.WithRunID("default-history-run")); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	snapshot, err := wf.Snapshot(context.Background(), SnapshotRequest{RunID: "default-history-run"})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.RunMeta.RunID != "default-history-run" || len(snapshot.Timeline) < 4 || len(snapshot.Checkpoints) == 0 {
		t.Fatalf("Snapshot() = %+v, want default in-memory history", snapshot)
	}
	if snapshot.Timeline[0].EventType != EventWorkflowStarted ||
		snapshot.Timeline[len(snapshot.Timeline)-1].EventType != EventWorkflowCompleted {
		t.Fatalf("timeline = %+v, want workflow start through completion", snapshot.Timeline)
	}
}

func TestWorkflowAcceptsSharedMemoryStoreDirectly(t *testing.T) {
	store := NewMemoryStore()
	wf := New[string, string]("shared-memory", WithStore(storeWithCheckpointer(store)))
	echo := testNode(wf, "echo", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(echo)
	wf.Exit(echo)

	if _, err := wf.Invoke(context.Background(), "value", gopact.WithRunID("shared-memory-run")); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if _, err := wf.Snapshot(context.Background(), SnapshotRequest{RunID: "shared-memory-run"}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
}

func TestWorkflowExternalStoreFailureStopsRun(t *testing.T) {
	storeErr := errors.New("store unavailable")
	store := failingWorkflowStore{err: storeErr}
	tests := []struct {
		name   string
		option BuildOption
	}{
		{name: "checkpointer", option: WithStore(storeWithCheckpointer(store))},
		{name: "journal", option: WithStore(storeWithCheckpointer(store))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wf := New[string, string]("authoritative-"+test.name, test.option)
			node := testNode(wf, "node", func(_ context.Context, input string) (string, error) {
				return input, nil
			})
			wf.Entry(node)
			wf.Exit(node)

			_, err := wf.Invoke(context.Background(), "input")
			if !errors.Is(err, storeErr) {
				t.Fatalf("Invoke() error = %v, want %v", err, storeErr)
			}
		})
	}
}

func TestWorkflowStrictExternalStoreFailureStopsRun(t *testing.T) {
	storeErr := errors.New("store unavailable")
	store := failingWorkflowStore{err: storeErr}
	tests := []struct {
		name   string
		option BuildOption
	}{
		{name: "checkpointer", option: WithStore(storeWithCheckpointer(store))},
		{name: "journal", option: WithStore(storeWithCheckpointer(store))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wf := New[string, string]("strict-store-"+test.name, test.option)
			plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
				return input, nil
			})
			wf.Entry(plan)
			wf.Exit(plan)

			_, err := wf.Invoke(context.Background(), "input")
			if !errors.Is(err, storeErr) {
				t.Fatalf("Invoke() error = %v, want %v", err, storeErr)
			}
		})
	}
}

func TestWorkflowEventEnvelopeIsAccurate(t *testing.T) {
	compiled := testCompiledWorkflow(t)
	var events []gopact.Event
	_, err := compiled.Invoke(context.Background(), "abc",
		gopact.WithSessionID("session-1"),
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("events = 0, want workflow events")
	}
	for i, event := range events {
		if event.SessionID != "session-1" || event.RunID != "run-1" || event.ParentRunID != "" ||
			event.DefinitionID == "" || event.DefinitionVersion == "" || event.RevisionID == "" ||
			event.Sequence != int64(i+1) || event.Source == "" || event.Origin == "" || event.Timestamp.IsZero() {
			t.Fatalf("event[%d] = %+v, want accurate envelope", i, event)
		}
	}
}

func TestWorkflowNodeExecutionVersionsFollowRunTimeline(t *testing.T) {
	wf := New[int, int]("node-versions")
	step := testNode(wf, "step", func(_ context.Context, input int) (int, error) {
		return input + 1, nil
	})
	step.Route(func(_ context.Context, output int) (Dispatch, error) {
		if output < 3 {
			return step.Once(step, output), nil
		}
		return step.Stop(), nil
	})
	wf.Entry(step)
	wf.Edge(step, step)
	wf.Exit(step)

	var starts []gopact.Event
	for _, err := range wf.InvokeStream(context.Background(), 0,
		gopact.WithRunID("version-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeStarted {
				starts = append(starts, event)
			}
			return nil
		}),
	) {
		if err != nil {
			t.Fatalf("InvokeStream() error = %v", err)
		}
	}
	if len(starts) != 3 {
		t.Fatalf("node starts = %d, want 3", len(starts))
	}
	seenAttempts := make(map[string]struct{}, len(starts))
	seenActivations := make(map[string]struct{}, len(starts))
	for i, event := range starts {
		wantVersion := int64(i + 1)
		if event.NodeID != "step" || event.ActivationID == "" || event.AttemptID == "" ||
			event.NodeExecutionVersion != wantVersion || event.Origin != "natural" {
			t.Fatalf("start[%d] = %+v, want step version %d natural attempt", i, event, wantVersion)
		}
		if _, exists := seenAttempts[event.AttemptID]; exists {
			t.Fatalf("attempt id %q was reused", event.AttemptID)
		}
		seenAttempts[event.AttemptID] = struct{}{}
		if _, exists := seenActivations[event.ActivationID]; exists {
			t.Fatalf("activation id %q was reused", event.ActivationID)
		}
		seenActivations[event.ActivationID] = struct{}{}
	}

	var nextRun gopact.Event
	for _, err := range wf.InvokeStream(context.Background(), 2,
		gopact.WithRunID("version-run-2"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeStarted && nextRun.Type == "" {
				nextRun = event
			}
			return nil
		}),
	) {
		if err != nil {
			t.Fatalf("second InvokeStream() error = %v", err)
		}
	}
	if nextRun.NodeExecutionVersion != 1 || nextRun.ActivationID == starts[0].ActivationID ||
		nextRun.AttemptID == starts[0].AttemptID {
		t.Fatalf("second run start = %+v, want reset version and distinct identities", nextRun)
	}
}

func TestWorkflowChildRunIsolation(t *testing.T) {
	var childConfig gopact.RunConfig
	ownerGenerator := func() (string, error) { return "owner-local", nil }
	wf := New[string, string]("parent")
	child := wf.AddInvokable("child", gopact.InvokableFunc[string, string](func(_ context.Context, input string, options ...gopact.RunOption) (string, error) {
		childConfig = gopact.ResolveRunOptions(options...)
		return input + "!", nil
	}))
	wf.Entry(child)
	wf.Exit(child)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	output, err := compiled.Invoke(context.Background(), "input",
		gopact.WithSessionID("session-parent"),
		gopact.WithRunID("parent-run"),
		gopact.WithIDGenerator(gopact.IDKindOwner, ownerGenerator),
		gopact.WithStrictEventHandler(func(context.Context, gopact.Event) error { return nil }),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if output != "input!" {
		t.Fatalf("Invoke() = %q, want input!", output)
	}
	if childConfig.SessionID != "session-parent" ||
		childConfig.RunID == "" || childConfig.RunID == "parent-run" ||
		childConfig.Lineage.ParentRunID != "parent-run" || childConfig.Lineage.Depth != 2 ||
		len(childConfig.EventSinks) != 1 || len(childConfig.Extensions) != 0 {
		t.Fatalf("child config = %+v, want isolated child identity and inherited sink", childConfig)
	}
	if _, ok := childConfig.IDGenerator(gopact.IDKindOwner); !ok {
		t.Fatal("child config did not inherit the per-run owner generator")
	}
}

func TestWorkflowChildRunInheritsContextCancellationAndDeadline(t *testing.T) {
	started := make(chan struct{})
	childDeadline := make(chan time.Time, 1)
	wf := New[string, string]("parent")
	child := wf.AddInvokable("child", gopact.InvokableFunc[string, string](func(ctx context.Context, input string, _ ...gopact.RunOption) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return "", errors.New("child context has no deadline")
		}
		childDeadline <- deadline
		close(started)
		watchdog := time.NewTimer(2 * time.Second)
		defer watchdog.Stop()
		select {
		case <-ctx.Done():
			return input, ctx.Err()
		case <-watchdog.C:
			err := errors.New("child context cancellation was not propagated")
			return "", err
		}
	}))
	wf.Entry(child)
	wf.Exit(child)

	deadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	_, err := wf.Invoke(ctx, "input", gopact.WithSessionID("session-parent"), gopact.WithRunID("parent-run"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Invoke() error = %v, want context.Canceled", err)
	}
	if got := <-childDeadline; !got.Equal(deadline) {
		t.Fatalf("child deadline = %v, want %v", got, deadline)
	}
}

func TestWorkflowChildOptionsRejectMissingSession(t *testing.T) {
	execution := workflowExecution[string, string]{runID: "parent-run", depth: 1}
	config := gopact.ResolveRunOptions(execution.childOptions(context.Background(), activation{id: "child"}, 1, 1)...)
	if err := config.RunConfigError(); !errors.Is(err, gopact.ErrRunConfig) {
		t.Fatalf("RunConfigError() = %v, want ErrRunConfig", err)
	}
}

func TestWorkflowEmitRejectsRuntimeIdentity(t *testing.T) {
	ctx := context.WithValue(context.Background(), eventEmitterContextKey{}, eventEmitter(func(context.Context, gopact.Event) error {
		return nil
	}))
	tests := []struct {
		name  string
		event gopact.Event
	}{
		{name: "session id", event: gopact.Event{SessionID: "session-1", Type: "audit.custom"}},
		{name: "run id", event: gopact.Event{RunID: "run-1", Type: "audit.custom"}},
		{name: "parent run id", event: gopact.Event{ParentRunID: "parent-1", Type: "audit.custom"}},
		{name: "definition id", event: gopact.Event{DefinitionID: "workflow", Type: "audit.custom"}},
		{name: "definition version", event: gopact.Event{DefinitionVersion: "v1", Type: "audit.custom"}},
		{name: "node id", event: gopact.Event{NodeID: "node", Type: "audit.custom"}},
		{name: "activation id", event: gopact.Event{ActivationID: "activation-1", Type: "audit.custom"}},
		{name: "attempt id", event: gopact.Event{AttemptID: "attempt-1", Type: "audit.custom"}},
		{name: "revision id", event: gopact.Event{RevisionID: "revision-1", Type: "audit.custom"}},
		{name: "node execution version", event: gopact.Event{NodeExecutionVersion: 1, Type: "audit.custom"}},
		{name: "execution epoch", event: gopact.Event{ExecutionEpoch: 1, Type: "audit.custom"}},
		{name: "source revision id", event: gopact.Event{SourceRevisionID: "source-revision-1", Type: "audit.custom"}},
		{name: "origin", event: gopact.Event{Origin: "natural", Type: "audit.custom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Emit(ctx, tt.event); err == nil {
				t.Fatal("Emit() error = nil, want runtime identity error")
			}
		})
	}
}

func TestWorkflowCustomEventPayloadBoundary(t *testing.T) {
	atLimit := []byte(`"` + strings.Repeat("x", maxWorkflowEventPayloadBytes-2) + `"`)
	if err := validateCustomEvent(nil, gopact.Event{Type: "audit.custom", Payload: atLimit}); err != nil {
		t.Fatalf("validateCustomEvent() at limit error = %v", err)
	}
	tests := []struct {
		name    string
		event   gopact.Event
		message string
	}{
		{name: "missing type", event: gopact.Event{}, message: "event type is required"},
		{name: "invalid json", event: gopact.Event{Type: "audit.custom", Payload: []byte(`{`)}, message: "payload is invalid JSON"},
		{name: "oversized", event: gopact.Event{Type: "audit.custom", Payload: append(atLimit, ' ')}, message: "payload is too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCustomEvent(nil, tt.event)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("validateCustomEvent() error = %v, want %q", err, tt.message)
			}
		})
	}
}

func TestWorkflowCheckpointPayloadBoundary(t *testing.T) {
	oversized := strings.Repeat("x", maxWorkflowCheckpointPayloadBytes+1)
	if _, err := encodeCheckpointPayloadWithMeta(runState{}, []string{oversized}, 1, checkpointPayloadMeta{}); err == nil || !strings.Contains(err.Error(), "payload is too large") {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v, want payload limit", err)
	}
	if _, err := decodeCheckpointPayload[string]([]byte(oversized)); err == nil || !strings.Contains(err.Error(), "payload is too large") {
		t.Fatalf("decodeCheckpointPayload() error = %v, want payload limit", err)
	}
}

func TestWorkflowRouteOnceDispatchesCustomPayload(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input + "!", nil
	})
	plan.Route(func(_ context.Context, _ int) (Dispatch, error) {
		return plan.Once(report, "len=3"), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "len=3!" {
		t.Fatalf("Invoke() = %q, want len=3!", got)
	}
}

func TestWorkflowRouteEmptyEachDoesNotUseDefaultEdges(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input + "!", nil
	})
	plan.Route(func(_ context.Context, _ string) (Dispatch, error) {
		return plan.Each(report), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc")
	if err == nil {
		t.Fatal("Invoke() error = nil, want no committed outputs")
	}
}

func TestWorkflowRouteRejectsUndeclaredTarget(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Route(func(_ context.Context, _ string) (Dispatch, error) {
		return plan.To(report), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc")
	if err == nil {
		t.Fatal("Invoke() error = nil, want undeclared target error")
	}
}

func TestWorkflowRouteRejectsMismatchedDispatchSource(t *testing.T) {
	tests := []struct {
		name  string
		route func(*Node[string, string], *Node[string, string]) Dispatch
		want  string
	}{
		{name: "foreign source", route: func(_ *Node[string, string], report *Node[string, string]) Dispatch { return report.To(report) }, want: "belongs to source"},
		{name: "mixed sources", route: func(plan, report *Node[string, string]) Dispatch { return plan.To(report).And(report.To(report)) }, want: "mixes sources"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := New[string, string]("example")
			plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) { return input, nil })
			report := testNode(wf, "report", func(_ context.Context, input string) (string, error) { return input, nil })
			plan.Route(func(_ context.Context, _ string) (Dispatch, error) { return tt.route(plan, report), nil })
			wf.Entry(plan)
			wf.Edge(plan, report)
			wf.Exit(report)

			compiled, err := wf.compile()
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			_, err = compiled.Invoke(context.Background(), "abc")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Invoke() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWorkflowRouteSupportsSettleAny(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Route(func(_ context.Context, _ string) (Dispatch, error) {
		return plan.To(report).WithSettle(SettleAny()), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	output, err := compiled.Invoke(context.Background(), "abc")
	if err != nil || output != "abc" {
		t.Fatalf("Invoke() = %q, %v, want abc", output, err)
	}
}

func TestWorkflowRouteRejectsInvalidSettleQuorum(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Route(func(_ context.Context, _ string) (Dispatch, error) {
		return plan.To(report).WithSettle(SettleQuorum(0)), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc")
	if err == nil {
		t.Fatal("Invoke() error = nil, want invalid settle quorum")
	}
}

func TestWorkflowRouteSupportsTypedIterReplay(t *testing.T) {
	wf := New[string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, _ string) ([]int, error) {
		return []int{1}, nil
	})
	score := testNode(wf, "score", func(_ context.Context, input int) (int, error) {
		return input, nil
	})
	plan.Route(func(_ context.Context, output []int) (Dispatch, error) {
		return plan.EachIter(score, func(context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				for _, value := range output {
					if !yield(value, nil) {
						return
					}
				}
			}
		}, WithIterReplay(func() int { return 0 }, func(context.Context, int) iter.Seq2[int, error] {
			return nil
		})), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, score)
	wf.Exit(score)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	output, err := compiled.Invoke(context.Background(), "abc")
	if err != nil || output != 1 {
		t.Fatalf("Invoke() = %d, %v, want 1", output, err)
	}
}

func TestWorkflowMergeCollectsFanOutContributions(t *testing.T) {
	wf := New[[]string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input []string) ([]string, error) {
		return input, nil
	})
	score := testNode(wf, "score", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	total := testMerge(wf, "total", func(_ context.Context, in Inputs) (int, error) {
		values, err := in.All(score)
		if err != nil {
			return 0, err
		}
		var sum int
		for _, value := range values {
			sum += value
		}
		return sum, nil
	})
	plan.Route(func(_ context.Context, output []string) (Dispatch, error) {
		return plan.Each(score, output...), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, score)
	wf.Edge(score, total)
	wf.Exit(total)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), []string{"a", "bbb", "cc"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 6 {
		t.Fatalf("Invoke() = %d, want 6", got)
	}
}

func TestWorkflowDoesNotMaterializeUnselectedEmptyMerge(t *testing.T) {
	var mergeRuns int
	wf := New[string, string]("exclusive-route")
	choose := testNode(wf, "choose", func(_ context.Context, input string) (string, error) { return input, nil })
	finish := testNode(wf, "finish", func(_ context.Context, input string) (string, error) { return input, nil })
	unused := testMerge(wf, "unused", func(context.Context, Inputs) (string, error) {
		mergeRuns++
		return "unused", nil
	})
	choose.Route(func(_ context.Context, output string) (Dispatch, error) {
		return choose.Once(finish, output), nil
	})
	wf.Entry(choose)
	wf.Edge(choose, finish)
	wf.Edge(choose, unused)
	wf.Exit(finish)
	wf.Exit(unused)

	got, err := wf.Invoke(context.Background(), "done")
	if err != nil {
		t.Fatal(err)
	}
	if got != "done" || mergeRuns != 0 {
		t.Fatalf("Invoke() = %q, merge runs = %d, want selected output and no empty merge", got, mergeRuns)
	}
}

func TestWorkflowCompileRejectsMergeWithoutInputEdge(t *testing.T) {
	wf := New[string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	total := testMerge(wf, "total", func(_ context.Context, _ Inputs) (int, error) {
		return 0, nil
	})
	wf.Entry(plan)
	wf.Exit(total)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want merge input edge error")
	}
}

func TestWorkflowCompileRejectsMergeEntry(t *testing.T) {
	wf := New[Inputs, int]("example")
	total := testMerge(wf, "total", func(_ context.Context, _ Inputs) (int, error) {
		return 0, nil
	})
	wf.Entry(total)
	wf.Exit(total)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want merge entry error")
	}
}

func TestWorkflowCompileRejectsMultiInputWithoutJoinOrMerge(t *testing.T) {
	wf := New[string, string]("example")
	left := testNode(wf, "left", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	right := testNode(wf, "right", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(left)
	wf.Edge(left, report)
	wf.Edge(right, report)
	wf.Exit(report)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want multi-input join error")
	}
}

func TestWorkflowJoinBuildsTargetInput(t *testing.T) {
	wf := New[[]string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input []string) ([]string, error) {
		return input, nil
	})
	score := testNode(wf, "score", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	report := testNode(wf, "report", func(_ context.Context, input []int) (string, error) {
		return string(rune('0' + len(input))), nil
	})
	report.Join(func(_ context.Context, in Inputs) ([]int, error) {
		values, err := in.All(score)
		if err != nil {
			return nil, err
		}
		out := make([]int, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out, nil
	})
	plan.Route(func(_ context.Context, output []string) (Dispatch, error) {
		return plan.Each(score, output...), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, score)
	wf.Edge(score, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), []string{"a", "bbb"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "2" {
		t.Fatalf("Invoke() = %q, want 2", got)
	}
}

func TestWorkflowJoinMaterializesAfterOptionalEdgeClosesAtZero(t *testing.T) {
	compiled := compileOptionalJoin(t)
	output, err := compiled.Invoke(context.Background(), 7)
	if err != nil || output != 7 {
		t.Fatalf("Invoke() = %d, %v, want 7", output, err)
	}
}

func TestWorkflowResumeRestoresOptionalJoinExpectation(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	compiled := compileOptionalJoin(t, WithStore(storeWithCheckpointer(store)))
	sinkErr := errors.New("sink failed")
	_, err := compiled.Invoke(context.Background(), 7, gopact.WithRunID("optional-join-resume"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeOutputCommitted && event.Summary == "right" {
			return sinkErr
		}
		return nil
	}))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink failure", err)
	}
	expireRecordingLease(t, store, "optional-join-resume")
	output, err := compiled.Invoke(context.Background(), 7, WithResume(ResumeRequest{RunID: "optional-join-resume"}))
	if err != nil || output != 7 {
		t.Fatalf("resumed Invoke() = %d, %v, want 7", output, err)
	}
}

func compileOptionalJoin(t *testing.T, opts ...BuildOption) *compiled[int, int] {
	t.Helper()
	wf := New[int, int]("optional-join", opts...)
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	left := wf.Node("left", func(_ context.Context, input int) (int, error) { return input, nil })
	right := wf.Node("right", func(_ context.Context, input int) (int, error) { return input, nil })
	drop := wf.Node("drop", func(_ context.Context, input int) (int, error) { return input, nil })
	total := wf.Merge("total", func(_ context.Context, in Inputs) (int, error) {
		value, err := in.One(left)
		if err != nil {
			return 0, err
		}
		_, ok, err := in.Lookup(right)
		if err != nil || ok {
			return 0, fmt.Errorf("right lookup = %t, %v, want no contribution", ok, err)
		}
		return value, nil
	})
	plan.Route(func(_ context.Context, input int) (Dispatch, error) {
		return plan.Once(left, input).And(plan.Once(right, input)), nil
	})
	right.Route(func(_ context.Context, input int) (Dispatch, error) { return right.Once(drop, input), nil })
	wf.Entry(plan)
	wf.Edge(plan, left)
	wf.Edge(plan, right)
	wf.Edge(left, total)
	wf.Edge(right, total)
	wf.Edge(right, drop)
	wf.Exit(total)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func TestWorkflowGuardRewritesInputAndOutput(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + " body", nil
	})
	plan.Guard(
		BeforeRun("input", GuardFunc[string, string](
			func(_ context.Context, ctx GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardRewriteInput[string, string]{Input: ctx.Input + " rewritten"}, nil
			},
		)),
		BeforeCommit("output", GuardFunc[string, string](
			func(_ context.Context, ctx GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardRewriteOutput[string, string]{Output: ctx.CandidateOutput + " committed"}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "in")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "in rewritten body committed" {
		t.Fatalf("Invoke() = %q, want rewritten output", got)
	}
}

func TestWorkflowGuardBeforeRunSkipOutputSkipsNodeBody(t *testing.T) {
	wf := New[string, string]("example")
	bodyRuns := 0
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input + " body", nil
	})
	plan.Guard(
		BeforeRun("skip", GuardFunc[string, string](
			func(_ context.Context, _ GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardSkipOutput[string, string]{Output: "fallback"}, nil
			},
		)),
		BeforeCommit("commit", GuardFunc[string, string](
			func(_ context.Context, ctx GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardRewriteOutput[string, string]{Output: ctx.CandidateOutput + " committed"}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "in")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "fallback committed" {
		t.Fatalf("Invoke() = %q, want fallback committed", got)
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs = %d, want 0", bodyRuns)
	}
}

func TestWorkflowGuardBeforeCommitSkipOutputCommitsFallback(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + " body", nil
	})
	plan.Guard(
		BeforeCommit("skip", GuardFunc[string, string](
			func(_ context.Context, _ GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardSkipOutput[string, string]{Output: "fallback"}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "in")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "fallback" {
		t.Fatalf("Invoke() = %q, want fallback", got)
	}
}

func TestWorkflowGuardBeforeRunSkipWithoutOutputSkipsNodeBody(t *testing.T) {
	wf := New[string, string]("example")
	bodyRuns := 0
	var events []string
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input + " body", nil
	})
	plan.Guard(
		BeforeRun("skip", GuardFunc[string, string](
			func(_ context.Context, _ GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardSkip[string, string]{}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(
		context.Background(),
		"in",
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			events = append(events, event.Type)
			return nil
		}),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want no outputs")
	}
	if got := err.Error(); got != "workflow: invoke committed 0 outputs, want 1" {
		t.Fatalf("Invoke() error = %q", got)
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs = %d, want 0", bodyRuns)
	}
	if fmt.Sprint(events) != "[workflow.started node.started node.skipped workflow.completed]" {
		t.Fatalf("events = %v", events)
	}
}

func TestWorkflowGuardBeforeCommitSkipWithoutOutputDoesNotDispatch(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + " body", nil
	})
	reportRuns := 0
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		reportRuns++
		return input, nil
	})
	plan.Guard(
		BeforeCommit("skip", GuardFunc[string, string](
			func(_ context.Context, _ GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardSkip[string, string]{}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in")
	if err == nil {
		t.Fatal("Invoke() error = nil, want no outputs")
	}
	if reportRuns != 0 {
		t.Fatalf("report runs = %d, want 0", reportRuns)
	}
}

func TestWorkflowGuardBeforeCommitRetryRerunsNode(t *testing.T) {
	wf := New[string, string]("example")
	var attempts []string
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		attempts = append(attempts, input)
		return input + " body", nil
	})
	plan.Guard(
		BeforeCommit("retry", GuardFunc[string, string](
			func(_ context.Context, ctx GuardContext[string, string]) (GuardDecision[string, string], error) {
				if ctx.Meta.Attempt == 1 {
					return GuardRetry[string, string]{Input: "retry"}, nil
				}
				return GuardAllow[string, string]{}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "in")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "retry body" {
		t.Fatalf("Invoke() = %q, want retry body", got)
	}
	if fmt.Sprint(attempts) != "[in retry]" {
		t.Fatalf("attempts = %v, want [in retry]", attempts)
	}
}

func TestWorkflowGuardMetaIncludesRunAndWorkflow(t *testing.T) {
	wf := New[string, string]("example")
	var meta GuardMeta
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Guard(
		BeforeRun("meta", GuardFunc[string, string](
			func(_ context.Context, ctx GuardContext[string, string]) (GuardDecision[string, string], error) {
				meta = ctx.Meta
				return GuardAllow[string, string]{}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in", gopact.WithRunID("run-1"))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if meta.RunID != "run-1" ||
		meta.WorkflowName != "example" ||
		meta.NodeName != "plan" ||
		meta.ActivationID == "" ||
		meta.Attempt != 1 {
		t.Fatalf("guard meta = %+v", meta)
	}
}

func TestWorkflowNodeRunInfoCarriesExecutionLineage(t *testing.T) {
	var received RunInfo
	wf := New[string, string]("lineage")
	node := testNode(wf, "work", func(ctx context.Context, input string) (string, error) {
		received = RunInfoFromContext(ctx)
		return input, nil
	})
	wf.Entry(node)
	wf.Exit(node)
	_, err := wf.Invoke(
		context.Background(),
		"input",
		gopact.WithSessionID("session-1"),
		gopact.WithRunID("run-1"),
		gopact.WithRunLineage(gopact.RunLineage{ParentRunID: "parent-1", Depth: 2}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if received.SessionID != "session-1" || received.RunID != "run-1" || received.ParentRunID != "parent-1" || received.Depth != 2 ||
		received.NodeID != "work" || received.ActivationID == "" || received.Attempt != 1 {
		t.Fatalf("RunInfoFromContext() = %+v, want run lineage", received)
	}
}

func TestRunInfoFromContextAcceptsNil(t *testing.T) {
	if got := RunInfoFromContext(nil); got != (RunInfo{}) {
		t.Fatalf("RunInfoFromContext(nil) = %+v, want zero value", got)
	}
}

func TestWorkflowGuardRetryUsesMaxStepsBudget(t *testing.T) {
	wf := New[string, string]("example", WithMaxSteps(2))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + " body", nil
	})
	plan.Guard(
		BeforeCommit("retry", GuardFunc[string, string](
			func(_ context.Context, _ GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardRetry[string, string]{Input: "retry"}, nil
			},
		)),
	)
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in")
	if err == nil {
		t.Fatal("Invoke() error = nil, want retry budget error")
	}
	if got := err.Error(); got != `workflow: exceeded max steps 2` {
		t.Fatalf("Invoke() error = %q", got)
	}
}

func TestWorkflowGuardRetryCreatesNewAttempts(t *testing.T) {
	wf := New[string, string]("guard-retry-attempts", WithMaxSteps(3))
	bodyRuns := 0
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input + " body", nil
	})
	plan.Guard(BeforeCommit("retry-once", GuardFunc[string, string](
		func(_ context.Context, ctx GuardContext[string, string]) (GuardDecision[string, string], error) {
			if ctx.Meta.Attempt == 1 {
				return GuardRetry[string, string]{Input: "retry"}, nil
			}
			return GuardAllow[string, string]{}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)

	var starts []gopact.Event
	var retries []gopact.Event
	var terminals []gopact.Event
	output, err := wf.Invoke(context.Background(), "in",
		gopact.WithRunID("guard-retry-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			switch event.Type {
			case EventNodeStarted:
				starts = append(starts, event)
			case EventNodeRetrying:
				retries = append(retries, event)
				terminals = append(terminals, event)
			case EventNodeCompleted, EventNodeFailed, EventNodeCanceled, EventNodeSkipped:
				terminals = append(terminals, event)
			}
			return nil
		}),
	)
	if err != nil || output != "retry body" {
		t.Fatalf("Invoke() = %q, %v, want retry body", output, err)
	}
	if bodyRuns != 2 || len(starts) != 2 || len(retries) != 1 {
		t.Fatalf("body runs = %d, starts = %d, retries = %d, want 2/2/1", bodyRuns, len(starts), len(retries))
	}
	if starts[0].ActivationID != starts[1].ActivationID || starts[0].AttemptID == starts[1].AttemptID ||
		starts[0].NodeExecutionVersion != 1 || starts[1].NodeExecutionVersion != 2 ||
		starts[0].Origin != "natural" || starts[1].Origin != "guard_retry" {
		t.Fatalf("starts = %+v, want same activation with two ordered attempts", starts)
	}
	if retries[0].AttemptID != starts[0].AttemptID || retries[0].NodeExecutionVersion != 1 {
		t.Fatalf("retry event = %+v, want first attempt terminal", retries[0])
	}
	if len(terminals) != 2 || terminals[0].AttemptID != starts[0].AttemptID ||
		terminals[1].AttemptID != starts[1].AttemptID {
		t.Fatalf("terminal events = %+v, want exactly one per attempt", terminals)
	}
}

func TestWorkflowNodeAttemptEventsCaptureMetadata(t *testing.T) {
	type request struct {
		Text string `json:"text"`
	}
	type response struct {
		Text string `json:"text"`
	}
	wf := New[request, response]("attempt-facts")
	plan := testNode(wf, "plan", func(_ context.Context, input request) (response, error) {
		return response{Text: input.Text + "!"}, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	events := make(map[string]NodeEventPayload)
	_, err := wf.Invoke(context.Background(), request{Text: "hello"},
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type != EventNodeStarted && event.Type != EventNodeCompleted {
				return nil
			}
			var payload NodeEventPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			events[event.Type] = payload
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	started := events[EventNodeStarted]
	completed := events[EventNodeCompleted]
	if started.Status != "running" || started.ActivationPhase != ActivationRunning || started.ContextRevision != 1 {
		t.Fatalf("started facts = %+v, want running metadata", started)
	}
	if completed.Status != "completed" || completed.ActivationPhase != ActivationCompleted || completed.Error != "" {
		t.Fatalf("completed facts = %+v, want completed metadata", completed)
	}
}

func TestWorkflowNodeFailedEventCapturesErrorFact(t *testing.T) {
	wantErr := errors.New("boom")
	wf := New[string, string]("attempt-error-fact")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return "", wantErr
	})
	wf.Entry(plan)
	wf.Exit(plan)

	var failed NodeEventPayload
	_, err := wf.Invoke(context.Background(), "input", gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeFailed {
			return json.Unmarshal(event.Payload, &failed)
		}
		return nil
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, wantErr)
	}
	if failed.Error != "failed" || failed.Status != "failed" {
		t.Fatalf("failed fact = %+v, want classified failure", failed)
	}
}

func TestWorkflowTypedContextCommitsBetweenNodes(t *testing.T) {
	type state struct {
		Count int `json:"count"`
	}
	wf := New[string, int]("typed-context")
	shared := wf.Context(func(string) state { return state{} })
	first := wf.Node("first", func(ctx context.Context, input string) (string, error) {
		current, err := shared.Get(ctx)
		if err != nil {
			return "", err
		}
		current.Count++
		return input, shared.Set(ctx, current)
	})
	second := wf.Node("second", func(ctx context.Context, _ string) (int, error) {
		current, err := shared.Get(ctx)
		return current.Count, err
	})
	wf.Entry(first)
	wf.Edge(first, second)
	wf.Exit(second)

	var starts []NodeEventPayload
	output, err := wf.Invoke(context.Background(), "input", gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type != EventNodeStarted {
			return nil
		}
		var payload NodeEventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		starts = append(starts, payload)
		return nil
	}))
	if err != nil || output != 1 {
		t.Fatalf("Invoke() = %d, %v, want 1", output, err)
	}
	if len(starts) != 2 || starts[0].ContextRevision != 1 || starts[1].ContextRevision != 2 {
		t.Fatalf("started contexts = %+v, want revisions 1 then 2", starts)
	}
}

type typedContextState struct {
	Count int `json:"count"`
}

func TestWorkflowTypedContextRestoresFromCheckpoint(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	firstRuns := 0
	build := func() *Workflow[string, int] {
		wf := New[string, int]("typed-context-resume", WithStore(storeWithCheckpointer(store)))
		shared := wf.Context(func(string) typedContextState { return typedContextState{} })
		first := wf.Node("first", func(ctx context.Context, input string) (string, error) {
			firstRuns++
			current, err := shared.Get(ctx)
			if err != nil {
				return "", err
			}
			current.Count++
			return input, shared.Set(ctx, current)
		})
		second := wf.Node("second", func(ctx context.Context, _ string) (int, error) {
			current, err := shared.Get(ctx)
			return current.Count, err
		})
		wf.Entry(first)
		wf.Edge(first, second)
		wf.Exit(second)
		return wf
	}

	sinkErr := errors.New("sink failed")
	_, err := build().Invoke(context.Background(), "input",
		gopact.WithRunID("typed-context-resume-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeCompleted && event.Summary == "first" {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("first Invoke() error = %v, want %v", err, sinkErr)
	}
	expireRecordingLease(t, store, "typed-context-resume-run")

	output, err := build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "typed-context-resume-run"}))
	if err != nil || output != 1 {
		t.Fatalf("resumed Invoke() = %d, %v, want 1", output, err)
	}
	if firstRuns != 1 {
		t.Fatalf("first node runs = %d, want 1", firstRuns)
	}
}

func TestWorkflowContextCommitsOnSingleRoutedBranch(t *testing.T) {
	wf := New[string, int]("routed-context")
	state := wf.Context(func(string) int { return 0 })
	route := testNode(wf, "route", func(_ context.Context, input string) (string, error) { return input, nil })
	update := testNode(wf, "update", func(ctx context.Context, _ string) (string, error) {
		value, err := state.Get(ctx)
		if err != nil {
			return "", err
		}
		return "updated", state.Set(ctx, value+1)
	})
	audit := testNode(wf, "audit", func(ctx context.Context, _ string) (int, error) {
		return state.Get(ctx)
	})
	route.Route(func(_ context.Context, output string) (Dispatch, error) {
		return route.Once(update, output), nil
	})
	wf.Entry(route)
	wf.Edge(route, update)
	wf.Edge(update, audit)
	wf.Exit(audit)

	got, err := wf.Invoke(context.Background(), "input")
	if err != nil || got != 1 {
		t.Fatalf("Invoke() = %d, %v, want committed routed context 1", got, err)
	}
}

func TestCheckpointPayloadPreservesActivationIDs(t *testing.T) {
	state := runState{
		queue: []activation{
			{id: "act-2", node: "plan", input: "in"},
		},
		nextActSeq: 3,
		scheduled:  map[string]int{"plan": 1},
		completed:  map[string]int{},
		buckets:    map[joinBucketKey]*joinBucket{},
	}
	payload, err := encodeCheckpointPayloadWithMeta[string](state, nil, 1, checkpointPayloadMeta{})
	if err != nil {
		t.Fatalf("encodeCheckpointPayload() error = %v", err)
	}
	decoded, err := decodeCheckpointPayload[string](payload)
	if err != nil {
		t.Fatalf("decodeCheckpointPayload() error = %v", err)
	}
	got := decoded.state()
	if len(got.queue) != 1 || got.queue[0].id != "act-2" || got.nextActSeq != 3 {
		t.Fatalf("decoded state = %+v", got)
	}
}

func TestWorkflowLifecycleHooksRewriteInputAndOutput(t *testing.T) {
	wf := New[string, string]("example")
	wf.BeforeWorkflow(Hook("workflow-input", func(ctx *WorkflowContext[string, string]) error {
		ctx.Input += " workflow"
		return nil
	}))
	wf.AfterWorkflow(Hook("workflow-output", func(ctx *WorkflowContext[string, string]) error {
		ctx.Output += " done"
		return nil
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + " body", nil
	})
	plan.Before(Hook("node-input", func(ctx *NodeContext[string, string]) error {
		ctx.Input += " node"
		return nil
	}))
	plan.After(Hook("node-output", func(ctx *NodeContext[string, string]) error {
		ctx.Output += " after"
		return nil
	}))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var events []string
	got, err := compiled.Invoke(
		context.Background(),
		"in",
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventLifecycleHookStarted || event.Type == EventLifecycleHookCompleted {
				events = append(events, event.Type+":"+event.Summary)
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "in workflow node body after done" {
		t.Fatalf("Invoke() = %q, want lifecycle rewritten output", got)
	}
	wantEvents := []string{
		EventLifecycleHookStarted + ":workflow-input",
		EventLifecycleHookCompleted + ":workflow-input",
		EventLifecycleHookStarted + ":node-input",
		EventLifecycleHookCompleted + ":node-input",
		EventLifecycleHookStarted + ":node-output",
		EventLifecycleHookCompleted + ":node-output",
		EventLifecycleHookStarted + ":workflow-output",
		EventLifecycleHookCompleted + ":workflow-output",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("lifecycle events = %v, want %v", events, wantEvents)
	}
}

func TestWorkflowLifecycleHookFailedEventFailureFailsRun(t *testing.T) {
	hookErr := errors.New("hook failed")
	sinkErr := errors.New("sink failed")
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Before(Hook("before", func(*NodeContext[string, string]) error {
		return hookErr
	}))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(
		context.Background(),
		"in",
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventLifecycleHookFailed {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
}

func TestWorkflowPluginSetupRunsOnceAfterSuccessfulCompile(t *testing.T) {
	plugin := &countingPlugin{name: "audit"}
	wf := New[string, string]("example", WithPlugins(plugin))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	if _, err := wf.compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if _, err := wf.compile(); err != nil {
		t.Fatalf("second Compile() error = %v", err)
	}
	if plugin.setups != 1 {
		t.Fatalf("plugin setups = %d, want 1", plugin.setups)
	}
}

func testRunLogRecord(runID string, sequence int64, eventType string) runlog.Record {
	return runlog.Record{
		SessionID: "session-1",
		RunID:     runID,
		Sequence:  sequence,
		EventType: eventType,
		Source:    "workflow",
		Timestamp: time.Now().UTC(),
	}
}

func TestMemoryRunLogAppendIsIdempotentAndDetectsConflict(t *testing.T) {
	log := runlog.NewMemoryLog()
	rec := testRunLogRecord("run-1", 1, EventWorkflowStarted)
	if err := log.Append(context.Background(), rec); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := log.Append(context.Background(), rec); err != nil {
		t.Fatalf("duplicate Append() error = %v", err)
	}
	conflict := rec
	conflict.EventType = EventWorkflowCompleted
	err := log.Append(context.Background(), conflict)
	if err == nil {
		t.Fatal("conflicting Append() error = nil, want conflict")
	}
	records, err := log.List(context.Background(), runlog.Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].EventType != EventWorkflowStarted {
		t.Fatalf("records = %+v, want one started record", records)
	}
}

func TestRunLogSinkRecordsWorkflowEvents(t *testing.T) {
	log := runlog.NewMemoryLog()
	compiled := testCompiledWorkflow(t)
	_, err := compiled.Invoke(context.Background(), "abc",
		gopact.WithSessionID("session-1"),
		gopact.WithRunID("run-1"),
		gopact.WithEventSink(runlog.NewSink(log)),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	records, err := log.List(context.Background(), runlog.Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) == 0 || records[0].SessionID != "session-1" || records[0].ParentRunID != "" ||
		records[0].DefinitionID == "" || records[0].DefinitionVersion == "" ||
		records[0].RevisionID == "" || records[0].Origin == "" ||
		records[0].Sequence != 1 || records[0].EventType != EventWorkflowStarted {
		t.Fatalf("records = %+v, want workflow events", records)
	}
}

func TestRunLogSnapshotStoreHonorsAfterAndLimit(t *testing.T) {
	log := runlog.NewMemoryLog()
	for i, eventType := range []string{EventWorkflowStarted, EventNodeStarted, EventNodeCompleted} {
		record := testRunLogRecord("run-1", int64(i+1), eventType)
		record.SessionID = "session-1"
		record.ParentRunID = "parent-1"
		record.SourceRunID = "source-1"
		record.SourceEventSeq = 1
		if err := log.Append(context.Background(), record); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	snapshot, err := newTestRunLogSnapshotStore(log, emptyCheckpointHistory{}).Load(context.Background(), SnapshotRequest{RunID: "run-1", After: 1, Limit: 1})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Timeline) != 1 || snapshot.Timeline[0].Sequence != 2 {
		t.Fatalf("timeline = %+v, want only sequence 2", snapshot.Timeline)
	}
	if snapshot.RunMeta.SessionID != "session-1" || snapshot.RunMeta.ParentRunID != "parent-1" ||
		snapshot.RunMeta.SourceRunID != "source-1" {
		t.Fatalf("run meta = %+v, want timeline lineage", snapshot.RunMeta)
	}
}

func TestRegisteredCustomEventIsValidatedAndEmitted(t *testing.T) {
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "events",
		register: func(r *Registry) error {
			return r.RegisterEventType("audit.custom", func(event gopact.Event) error {
				if len(event.Payload) == 0 {
					return errors.New("payload is required")
				}
				return nil
			})
		},
	}))
	plan := testNode(wf, "plan", func(ctx context.Context, input string) (string, error) {
		if err := Emit(ctx, gopact.Event{Type: "audit.custom", Payload: []byte(`{"ok":true}`)}); err != nil {
			return "", err
		}
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var got gopact.Event
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == "audit.custom" {
				got = event
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got.RunID != "run-1" || got.Sequence == 0 || got.Source != "workflow" {
		t.Fatalf("custom event = %+v, want runtime identity", got)
	}
}

func TestNodeMiddlewareWrapsBusinessNodeOnly(t *testing.T) {
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "middleware",
		register: func(r *Registry) error {
			return r.RegisterNodeMiddleware[string, string](
				"node",
				func(ctx *NodeContext[string, string], next NodeNext[string, string]) error {
					ctx.Input += " mw-in"
					if err := next(); err != nil {
						return err
					}
					ctx.Output += " mw-out"
					return nil
				},
			)
		},
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + " body", nil
	})
	plan.Before(Hook("before", func(ctx *NodeContext[string, string]) error {
		ctx.Input += " before"
		return nil
	}))
	plan.After(Hook("after", func(ctx *NodeContext[string, string]) error {
		ctx.Output += " after"
		return nil
	}))
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "in")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "in before mw-in body mw-out after" {
		t.Fatalf("Invoke() = %q, want middleware around business node", got)
	}
}

func TestNodeMiddlewareRejectsDoubleNext(t *testing.T) {
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "middleware",
		register: func(r *Registry) error {
			return r.RegisterNodeMiddleware[string, string](
				"node",
				func(_ *NodeContext[string, string], next NodeNext[string, string]) error {
					if err := next(); err != nil {
						return err
					}
					return next()
				},
			)
		},
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in")
	if err == nil {
		t.Fatal("Invoke() error = nil, want double next error")
	}
}

func TestRouteMiddlewareRewritesDispatchBeforeValidation(t *testing.T) {
	var plan *Node[string, string]
	var report *Node[string, string]
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "middleware",
		register: func(r *Registry) error {
			return r.RegisterRouteMiddleware[string, string]("route", func(ctx *RouteContext[string, string]) error {
				if ctx.NodeName != "plan" {
					return nil
				}
				ctx.Dispatch = plan.Once(report, "changed")
				return nil
			})
		},
	}))
	plan = testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	report = testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input + "!", nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), "in")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "changed!" {
		t.Fatalf("Invoke() = %q, want changed!", got)
	}
}

func TestJoinMiddlewareRewritesAssembledInput(t *testing.T) {
	wf := New[[]string, string]("example", WithPlugins(eventTypePlugin{
		name: "middleware",
		register: func(r *Registry) error {
			return r.RegisterJoinMiddleware[int]("join", func(ctx *JoinContext[int]) error {
				ctx.Input += 10
				return nil
			})
		},
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input []string) ([]string, error) {
		return input, nil
	})
	score := testNode(wf, "score", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	report := testNode(wf, "report", func(_ context.Context, input int) (string, error) {
		return fmt.Sprintf("sum=%d", input), nil
	})
	report.Join(func(_ context.Context, in Inputs) (int, error) {
		values, err := in.All(score)
		if err != nil {
			return 0, err
		}
		var sum int
		for _, value := range values {
			sum += value
		}
		return sum, nil
	})
	plan.Route(func(_ context.Context, output []string) (Dispatch, error) {
		return plan.Each(score, output...), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, score)
	wf.Edge(score, report)
	wf.Exit(report)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := compiled.Invoke(context.Background(), []string{"a", "bb"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "sum=13" {
		t.Fatalf("Invoke() = %q, want sum=13", got)
	}
}

func TestEventSinkWrapperWrapsDeliveryChain(t *testing.T) {
	var seen []string
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "sink-wrapper",
		register: func(r *Registry) error {
			return r.RegisterEventSinkWrapper("audit", func(next gopact.EventSink) gopact.EventSink {
				return gopact.EventSinkFunc(func(ctx context.Context, event gopact.Event) error {
					seen = append(seen, "wrapper:"+event.Type)
					return next.Emit(ctx, event)
				})
			})
		},
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(
		context.Background(),
		"in",
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			seen = append(seen, "sink:"+event.Type)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(seen) < 2 ||
		seen[0] != "wrapper:"+EventWorkflowStarted ||
		seen[1] != "sink:"+EventWorkflowStarted {
		t.Fatalf("seen = %v, want wrapper before sink", seen)
	}
}

func TestEventSinkWrapperErrorFailsRun(t *testing.T) {
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "sink-wrapper",
		register: func(r *Registry) error {
			return r.RegisterEventSinkWrapper("audit", func(gopact.EventSink) gopact.EventSink {
				return gopact.EventSinkFunc(func(context.Context, gopact.Event) error {
					return errors.New("wrapper failed")
				})
			})
		},
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(
		context.Background(),
		"in",
		gopact.WithStrictEventHandler(func(context.Context, gopact.Event) error {
			return nil
		}),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want wrapper failure")
	}
}

func TestRegisteredCustomEventValidationFailureFailsRun(t *testing.T) {
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "events",
		register: func(r *Registry) error {
			return r.RegisterEventType("audit.custom", func(event gopact.Event) error {
				if len(event.Payload) == 0 {
					return errors.New("payload is required")
				}
				return nil
			})
		},
	}))
	plan := testNode(wf, "plan", func(ctx context.Context, input string) (string, error) {
		return input, Emit(ctx, gopact.Event{Type: "audit.custom"})
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc")
	if err == nil {
		t.Fatal("Invoke() error = nil, want event validation error")
	}
}

func TestPluginDuplicateEventTypeIsBuildError(t *testing.T) {
	wf := New[string, string]("example", WithPlugins(eventTypePlugin{
		name: "events",
		register: func(r *Registry) error {
			if err := r.RegisterEventType("audit.custom", func(gopact.Event) error { return nil }); err != nil {
				return err
			}
			return r.RegisterEventType("audit.custom", func(gopact.Event) error { return nil })
		},
	}))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want duplicate event type error")
	}
}

func TestSnapshotForkRunsFromWorkflowInputPatch(t *testing.T) {
	store := NewMemoryCheckpointer()
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input + "!", nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	log := runlog.NewMemoryLog()
	if _, err := compiled.Invoke(context.Background(), "old",
		gopact.WithRunID("source-run"),
		gopact.WithSessionID("source-session"),
		gopact.WithEventSink(runlog.NewSink(log)),
	); err != nil {
		t.Fatalf("source Invoke() error = %v", err)
	}
	snapshot, err := newTestRunLogSnapshotStore(log, store).Load(context.Background(), SnapshotRequest{RunID: "source-run"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Checkpoints) == 0 || !snapshot.Checkpoints[0].Root || snapshot.Checkpoints[0].ReplayStatus != ReplaySafe {
		t.Fatalf("checkpoints = %+v, want safe root", snapshot.Checkpoints)
	}
	got, err := snapshot.Fork(context.Background(), wf, ForkRequest{
		SourceRunID:  "source-run",
		FromEventSeq: 1,
		Patch:        ForkPatch{WorkflowInput: &InputPatch{Value: "new"}},
	}, gopact.WithRunID("fork-run"), gopact.WithEventSink(runlog.NewSink(log)))
	if err != nil {
		t.Fatalf("Fork() error = %v", err)
	}
	if got != "new!" {
		t.Fatalf("Fork() = %q, want new!", got)
	}
	records, err := log.List(context.Background(), runlog.Query{RunID: "fork-run"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 || records[0].SessionID != "source-session" || records[0].SourceRunID != "source-run" || records[0].SourceEventSeq != 1 || records[0].ParentRunID != "" {
		t.Fatalf("fork records = %+v, want independent source association", records)
	}
	forkCheckpoint, err := store.Load(context.Background(), "fork-run")
	if err != nil {
		t.Fatal(err)
	}
	if forkCheckpoint.SessionID != "source-session" || forkCheckpoint.SourceRunID != "source-run" || forkCheckpoint.SourceEventSeq != 1 || forkCheckpoint.SourceRevisionID != "" {
		t.Fatalf("fork checkpoint lineage = %q/%d/%q, want source-run/1 with no revision", forkCheckpoint.SourceRunID, forkCheckpoint.SourceEventSeq, forkCheckpoint.SourceRevisionID)
	}
	matching, err := snapshot.Fork(context.Background(), wf, ForkRequest{
		SourceRunID: "source-run", FromEventSeq: 1,
		Patch: ForkPatch{WorkflowInput: &InputPatch{Value: "matching"}},
	}, gopact.WithRunID("fork-matching"), gopact.WithSessionID("source-session"), gopact.WithEventSink(runlog.NewSink(log)))
	if err != nil || matching != "matching!" {
		t.Fatalf("matching-session Fork() = %q, %v", matching, err)
	}
	_, err = snapshot.Fork(context.Background(), wf, ForkRequest{
		SourceRunID: "source-run", FromEventSeq: 1,
		Patch: ForkPatch{WorkflowInput: &InputPatch{Value: "conflict"}},
	}, gopact.WithRunID("fork-conflict"), gopact.WithSessionID("other-session"), gopact.WithEventSink(runlog.NewSink(log)))
	if !errors.Is(err, gopact.ErrRunConfig) {
		t.Fatalf("conflicting-session Fork() error = %v, want ErrRunConfig", err)
	}
	conflictRecords, listErr := log.List(t.Context(), runlog.Query{RunID: "fork-conflict"})
	if listErr != nil || len(conflictRecords) != 0 {
		t.Fatalf("conflicting fork records = %+v, %v, want none", conflictRecords, listErr)
	}
	if _, loadErr := store.Load(t.Context(), "fork-conflict"); !errors.Is(loadErr, ErrCheckpointNotFound) {
		t.Fatalf("conflicting fork checkpoint error = %v, want ErrCheckpointNotFound", loadErr)
	}
}

func TestSnapshotForkRejectsSourceMismatch(t *testing.T) {
	wf := testWorkflow(t)
	snapshot := Snapshot{
		RunMeta:  RunMeta{RunID: "run-1"},
		Timeline: []runlog.Record{{RunID: "run-1", Sequence: 1}},
	}
	_, err := snapshot.Fork(context.Background(), wf, ForkRequest{
		SourceRunID:  "other",
		FromEventSeq: 1,
		Patch:        ForkPatch{WorkflowInput: &InputPatch{Value: "abc"}},
	})
	if err == nil {
		t.Fatal("Fork() error = nil, want source mismatch")
	}
}

func TestSnapshotForkRejectsEventOutsideSnapshotRange(t *testing.T) {
	wf := testWorkflow(t)
	snapshot := Snapshot{
		RunMeta:  RunMeta{RunID: "run-1"},
		Timeline: []runlog.Record{{RunID: "run-1", Sequence: 3}},
	}
	_, err := snapshot.Fork(context.Background(), wf, ForkRequest{
		SourceRunID:  "run-1",
		FromEventSeq: 2,
		Patch:        ForkPatch{WorkflowInput: &InputPatch{Value: "abc"}},
	})
	if err == nil {
		t.Fatal("Fork() error = nil, want event sequence range error")
	}
}

func TestSnapshotForkRejectsTopologyMismatch(t *testing.T) {
	wf := testWorkflow(t)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{
		RunMeta: RunMeta{RunID: "run-1"}, WorkflowName: compiled.name, TopologyVersion: compiled.topologyVersion,
		Timeline:    []runlog.Record{{RunID: "run-1", Sequence: 1}},
		Checkpoints: []CheckpointView{{EventSeq: 1, Root: true, ReplayStatus: ReplaySafe, SchemaVersion: checkpointSchemaVersion}},
	}
	request := ForkRequest{
		SourceRunID: "run-1", FromEventSeq: 1,
		Patch: ForkPatch{WorkflowInput: &InputPatch{Value: "abc"}},
	}
	snapshot.TopologyVersion = "other"
	if _, err := snapshot.Fork(context.Background(), wf, request); !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("Fork() topology error = %v, want ErrCheckpointMismatch", err)
	}
}

func TestWorkflowGuardRejectsFailClosed(t *testing.T) {
	var events []string
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Guard(BeforeRun("policy", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardReject[string, string]{
				Rejection: gopact.GuardRejection{Reason: "blocked"},
			}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(
		context.Background(),
		"in",
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			events = append(events, event.Type)
			return nil
		})),
	)
	var rejection gopact.GuardRejection
	if !errors.As(err, &rejection) {
		t.Fatalf("Invoke() error = %v, want GuardRejection", err)
	}
	if rejection.GuardName != "policy" || rejection.Phase != string(GuardBeforeRun) {
		t.Fatalf("rejection = %+v, want guard name and phase", rejection)
	}
	wantEvents := []string{
		EventWorkflowStarted,
		EventNodeStarted,
		EventGuardRejected,
		EventNodeFailed,
		EventWorkflowFailed,
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
}

func TestWorkflowGuardRejectedEventFailureFinishesFailedRun(t *testing.T) {
	store := &recordingCheckpointer{}
	sinkErr := errors.New("guard rejected sink")
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Guard(BeforeRun("policy", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardReject[string, string]{
				Rejection: gopact.GuardRejection{Reason: "blocked"},
			}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventGuardRejected {
				return sinkErr
			}
			return nil
		})),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
	if len(store.finished) != 0 {
		t.Fatalf("finished checkpoints = %d, want terminal event confirmation first", len(store.finished))
	}
	if len(store.saved) == 0 || store.saved[len(store.saved)-1].Status != CheckpointRunning {
		t.Fatalf("saved checkpoints = %+v, want running checkpoint with pending terminal", store.saved)
	}
}

func TestWorkflowGuardBeforeRunInterruptResumes(t *testing.T) {
	store := &recordingCheckpointer{}
	guardCalls := 0
	bodyRuns := 0
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input + "!", nil
	})
	plan.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			guardCalls++
			return GuardInterrupt[string, string]{Request: InterruptRequest{
				ID:                  "approval-1",
				Subject:             "plan",
				ResolutionSchemaRef: "schema://approval",
			}}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in", gopact.WithRunID("run-1"))
	var interrupt InterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	if interrupt.Request.ID != "approval-1" {
		t.Fatalf("interrupt = %+v, want approval-1", interrupt)
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs = %d, want 0 before resume", bodyRuns)
	}
	if store.records["run-1"].Status != CheckpointInterrupted {
		t.Fatalf("checkpoint status = %q, want interrupted", store.records["run-1"].Status)
	}
	_, err = compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "run-1"}))
	if err == nil {
		t.Fatal("resume Invoke() error = nil, want missing resolution error")
	}
	if bodyRuns != 0 {
		t.Fatalf("body runs = %d, want 0 without resolution", bodyRuns)
	}

	got, err := compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "run-1",
		Resolutions: []InterruptResolution{{
			InterruptID: "approval-1",
			PayloadRef:  "artifact://approval-ok",
		}},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "in!" {
		t.Fatalf("resume Invoke() = %q, want in!", got)
	}
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1", guardCalls)
	}
	if bodyRuns != 1 {
		t.Fatalf("body runs = %d, want 1 after resume", bodyRuns)
	}
}

func TestWorkflowInvokableChildInterruptResumesSameChildRun(t *testing.T) {
	childRuns := 0
	child := New[string, string]("child")
	work := testNode(child, "work", func(_ context.Context, input string) (string, error) {
		childRuns++
		return input + "!", nil
	})
	work.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{
				ID: "approval-1", Subject: "child work", ResolutionSchemaRef: "schema://approval",
			}}, nil
		},
	)))
	child.Entry(work)
	child.Exit(work)

	parent := New[string, string]("parent")
	childNode := parent.AddInvokable("child", child)
	parent.Entry(childNode)
	parent.Exit(childNode)

	var events []gopact.Event
	sink := gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	})
	_, err := parent.Invoke(context.Background(), "input", gopact.WithRunID("parent-run"), sink)
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}

	got, err := parent.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "parent-run",
		Resolutions: []InterruptResolution{{
			InterruptID: "approval-1", PayloadRef: "artifact://approved",
		}},
	}), sink)
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "input!" || childRuns != 1 {
		t.Fatalf("resume Invoke() = %q, child runs = %d, want input! and one body run", got, childRuns)
	}
	childRunIDs := map[string]struct{}{}
	for _, event := range events {
		if event.ParentRunID == "parent-run" {
			childRunIDs[event.RunID] = struct{}{}
		}
	}
	if len(childRunIDs) != 1 {
		t.Fatalf("child run ids = %v, want one resumed child run", childRunIDs)
	}
}

func TestWorkflowRoutedInvokableChildInterruptResumesSameChildRun(t *testing.T) {
	childRuns := 0
	child := New[string, string]("routed-child")
	work := testNode(child, "work", func(_ context.Context, input string) (string, error) {
		childRuns++
		return input + "!", nil
	})
	work.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1"}}, nil
		},
	)))
	child.Entry(work)
	child.Exit(work)

	parent := New[string, string]("routed-parent")
	route := testNode(parent, "route", func(_ context.Context, input string) (string, error) { return input, nil })
	childNode := parent.AddInvokable("child", child)
	route.Route(func(_ context.Context, output string) (Dispatch, error) {
		return route.Once(childNode, output), nil
	})
	parent.Entry(route)
	parent.Edge(route, childNode)
	parent.Exit(childNode)

	_, err := parent.Invoke(context.Background(), "input", gopact.WithRunID("routed-parent-run"))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	got, err := parent.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID:       "routed-parent-run",
		Resolutions: []InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "input!" || childRuns != 1 {
		t.Fatalf("resume Invoke() = %q, child runs = %d, want input! and one body run", got, childRuns)
	}
}

func TestWorkflowParallelInvokableInterruptsResumeWithoutReplayingCompletedBranch(t *testing.T) {
	var completedRuns, firstRuns, secondRuns int
	completed := New[string, string]("completed-child")
	completedWork := testNode(completed, "work", func(_ context.Context, input string) (string, error) {
		completedRuns++
		return input + "-completed", nil
	})
	completed.Entry(completedWork)
	completed.Exit(completedWork)

	interruptingChild := func(name, interruptID string, runs *int) *Workflow[string, string] {
		child := New[string, string](name)
		work := testNode(child, "work", func(_ context.Context, input string) (string, error) {
			*runs++
			return input + "-" + name, nil
		})
		work.Guard(BeforeRun("approval", GuardFunc[string, string](
			func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
				return GuardInterrupt[string, string]{Request: InterruptRequest{ID: interruptID}}, nil
			},
		)))
		child.Entry(work)
		child.Exit(work)
		return child
	}
	first := interruptingChild("first-child", "approval-first", &firstRuns)
	second := interruptingChild("second-child", "approval-second", &secondRuns)

	parent := New[string, string]("parallel-parent", WithMaxParallelism(3))
	plan := testNode(parent, "plan", func(_ context.Context, input string) (string, error) { return input, nil })
	completedNode := parent.AddInvokable("completed", completed)
	firstNode := parent.AddInvokable("first", first)
	secondNode := parent.AddInvokable("second", second)
	merge := testMerge(parent, "merge", func(_ context.Context, inputs Inputs) (string, error) {
		completedOutput, err := inputs.One(completedNode)
		if err != nil {
			return "", err
		}
		firstOutput, err := inputs.One(firstNode)
		if err != nil {
			return "", err
		}
		secondOutput, err := inputs.One(secondNode)
		if err != nil {
			return "", err
		}
		return completedOutput + "/" + firstOutput + "/" + secondOutput, nil
	})
	plan.Route(func(_ context.Context, output string) (Dispatch, error) {
		return plan.Once(completedNode, output).
			And(plan.Once(firstNode, output)).
			And(plan.Once(secondNode, output)).
			WithSettle(SettleAll()), nil
	})
	parent.Entry(plan)
	parent.Edge(plan, completedNode)
	parent.Edge(plan, firstNode)
	parent.Edge(plan, secondNode)
	parent.Edge(completedNode, merge)
	parent.Edge(firstNode, merge)
	parent.Edge(secondNode, merge)
	parent.Exit(merge)

	_, err := parent.Invoke(context.Background(), "input", gopact.WithRunID("parallel-parent-run"))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	if got, want := interruptRequestIDs(interrupted.Requests), []string{"approval-first", "approval-second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interrupt requests = %v, want %v", got, want)
	}
	if completedRuns != 1 || firstRuns != 0 || secondRuns != 0 {
		t.Fatalf("runs before resume = %d/%d/%d, want 1/0/0", completedRuns, firstRuns, secondRuns)
	}
	_, err = parent.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "parallel-parent-run",
		Resolutions: []InterruptResolution{
			{InterruptID: "approval-first", PayloadRef: "artifact://first-approved"},
		},
	}))
	if err == nil || !strings.Contains(err.Error(), "approval-second") {
		t.Fatalf("partial resume error = %v, want missing approval-second", err)
	}
	if completedRuns != 1 || firstRuns != 0 || secondRuns != 0 {
		t.Fatalf("runs after partial resume = %d/%d/%d, want 1/0/0", completedRuns, firstRuns, secondRuns)
	}

	got, err := parent.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "parallel-parent-run",
		Resolutions: []InterruptResolution{
			{InterruptID: "approval-first", PayloadRef: "artifact://first-approved"},
			{InterruptID: "approval-second", PayloadRef: "artifact://second-approved"},
		},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "input-completed/input-first-child/input-second-child" {
		t.Fatalf("resume Invoke() = %q, want merged child outputs", got)
	}
	if completedRuns != 1 || firstRuns != 1 || secondRuns != 1 {
		t.Fatalf("runs after resume = %d/%d/%d, want 1/1/1", completedRuns, firstRuns, secondRuns)
	}
}

func TestWorkflowInvokableChildPropagatesAllNestedInterrupts(t *testing.T) {
	var firstRuns, secondRuns int
	child := New[string, string]("parallel-child", WithMaxParallelism(2))
	plan := testNode(child, "plan", func(_ context.Context, input string) (string, error) { return input, nil })
	first := testNode(child, "first", func(_ context.Context, input string) (string, error) {
		firstRuns++
		return input + "-first", nil
	})
	second := testNode(child, "second", func(_ context.Context, input string) (string, error) {
		secondRuns++
		return input + "-second", nil
	})
	first.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "nested-first"}}, nil
		},
	)))
	second.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "nested-second"}}, nil
		},
	)))
	merge := testMerge(child, "merge", func(_ context.Context, inputs Inputs) (string, error) {
		firstOutput, err := inputs.One(first)
		if err != nil {
			return "", err
		}
		secondOutput, err := inputs.One(second)
		if err != nil {
			return "", err
		}
		return firstOutput + "/" + secondOutput, nil
	})
	plan.Route(func(_ context.Context, input string) (Dispatch, error) {
		return plan.Once(first, input).And(plan.Once(second, input)).WithSettle(SettleAll()), nil
	})
	child.Entry(plan)
	child.Edge(plan, first)
	child.Edge(plan, second)
	child.Edge(first, merge)
	child.Edge(second, merge)
	child.Exit(merge)

	parent := New[string, string]("parent")
	childNode := parent.AddInvokable("child", child)
	parent.Entry(childNode)
	parent.Exit(childNode)
	_, err := parent.Invoke(context.Background(), "input", gopact.WithRunID("nested-parent"))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	if got, want := interruptRequestIDs(interrupted.Requests), []string{"nested-first", "nested-second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interrupt requests = %v, want %v", got, want)
	}
	got, err := parent.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "nested-parent",
		Resolutions: []InterruptResolution{
			{InterruptID: "nested-first", PayloadRef: "artifact://first"},
			{InterruptID: "nested-second", PayloadRef: "artifact://second"},
		},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "input-first/input-second" || firstRuns != 1 || secondRuns != 1 {
		t.Fatalf("resume Invoke() = %q, runs = %d/%d, want merged output and one run each", got, firstRuns, secondRuns)
	}
}

func interruptRequestIDs(requests []InterruptRequest) []string {
	ids := make([]string, len(requests))
	for index, request := range requests {
		ids[index] = request.ID
	}
	return ids
}

func TestWorkflowGuardBeforeCommitInterruptResumesWithoutRerunningBody(t *testing.T) {
	store := &recordingCheckpointer{}
	guardCalls := 0
	bodyRuns := 0
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input + "!", nil
	})
	plan.Guard(BeforeCommit("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			guardCalls++
			return GuardInterrupt[string, string]{Request: InterruptRequest{
				ID:                  "commit-approval-1",
				Subject:             "plan output",
				ResolutionSchemaRef: "schema://approval",
			}}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in", gopact.WithRunID("run-1"))
	var interrupt InterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	if store.records["run-1"].Status != CheckpointInterrupted {
		t.Fatalf("checkpoint status = %q, want interrupted", store.records["run-1"].Status)
	}
	if bodyRuns != 1 {
		t.Fatalf("body runs = %d, want 1 before resume", bodyRuns)
	}

	got, err := compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "run-1",
		Resolutions: []InterruptResolution{{
			InterruptID: "commit-approval-1",
			PayloadRef:  "artifact://approval-ok",
		}},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if got != "in!" {
		t.Fatalf("resume Invoke() = %q, want in!", got)
	}
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1", guardCalls)
	}
	if bodyRuns != 1 {
		t.Fatalf("body runs = %d, want no rerun after resume", bodyRuns)
	}
}

func TestWorkflowGuardInterruptRequiresID(t *testing.T) {
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(&recordingCheckpointer{})))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardInterrupt[string, string]{Request: InterruptRequest{}}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "in", gopact.WithRunID("run-1"))
	if err == nil {
		t.Fatal("Invoke() error = nil, want interrupt id error")
	}
	if !strings.Contains(err.Error(), "interrupt id is required") {
		t.Fatalf("Invoke() error = %v, want interrupt id error", err)
	}
}

func TestWorkflowCompileRejectsDuplicateGuardNameInPhase(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	allow := GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			return GuardAllow[string, string]{}, nil
		},
	)
	plan.Guard(BeforeRun("policy", allow), BeforeRun("policy", allow))
	wf.Entry(plan)
	wf.Exit(plan)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want duplicate guard error")
	}
}

func TestWorkflowCheckpointerFinishesCompletedRun(t *testing.T) {
	const leaseDuration = 37 * time.Second
	store := &recordingCheckpointer{}
	wf := New[string, int](
		"example",
		WithStore(storeWithCheckpointer(store)),
		WithCheckpointLease(leaseDuration, 10*time.Second),
	)
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc", gopact.WithRunID("run-1"))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(store.created) != 1 || store.created[0].RunID != "run-1" || store.created[0].Status != CheckpointRunning {
		t.Fatalf("created checkpoint = %+v", store.created)
	}
	if store.created[0].LeaseDuration != leaseDuration {
		t.Fatalf("created lease duration = %v, want %v", store.created[0].LeaseDuration, leaseDuration)
	}
	for _, saved := range store.saved {
		if saved.OwnerID != "" && saved.LeaseDuration != leaseDuration {
			t.Fatalf("saved lease duration = %v, want %v", saved.LeaseDuration, leaseDuration)
		}
	}
	if len(store.finished) == 0 || store.finished[len(store.finished)-1].Status != CheckpointCompleted {
		t.Fatalf("finished checkpoint = %+v, want completed", store.finished)
	}
	if store.finished[len(store.finished)-1].LeaseDuration != 0 {
		t.Fatalf("finished lease duration = %v, want cleared", store.finished[len(store.finished)-1].LeaseDuration)
	}
}

func TestWorkflowCompletedEventFailureLeavesPendingEvent(t *testing.T) {
	store := &recordingCheckpointer{}
	bodyRuns := 0
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		bodyRuns++
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventWorkflowCompleted {
				return errors.New("sink failed")
			}
			return nil
		})),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want sink failure")
	}
	if len(store.finished) != 0 {
		t.Fatalf("finished checkpoints = %d, want terminal event confirmation first", len(store.finished))
	}
	if len(store.saved) == 0 || store.saved[len(store.saved)-1].Status != CheckpointRunning {
		t.Fatalf("saved checkpoints = %+v, want running checkpoint with pending terminal", store.saved)
	}
	payload, err := decodeCheckpointPayload[int](store.saved[len(store.saved)-1].Payload)
	if err != nil {
		t.Fatalf("decode checkpoint payload: %v", err)
	}
	if payload.PendingEvent == nil || payload.PendingEvent.Type != EventWorkflowCompleted {
		t.Fatalf("pending event = %+v, want workflow completed", payload.PendingEvent)
	}
	if payload.PendingEvent.Sequence <= payload.EventCursor {
		t.Fatalf(
			"pending sequence = %d, cursor = %d, want pending after cursor",
			payload.PendingEvent.Sequence,
			payload.EventCursor,
		)
	}
	if payload.PendingTerm != CheckpointCompleted {
		t.Fatalf("pending terminal = %q, want %q", payload.PendingTerm, CheckpointCompleted)
	}
	expireRecordingLease(t, store, "run-1")
	output, err := compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "run-1"}))
	if err != nil || output != 3 {
		t.Fatalf("resumed Invoke() = %d, %v, want 3", output, err)
	}
	if bodyRuns != 1 {
		t.Fatalf("body runs = %d, want no terminal replay rerun", bodyRuns)
	}
}

func TestWorkflowStartedEventFailureLeavesPendingEvent(t *testing.T) {
	store := &recordingCheckpointer{}
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventWorkflowStarted {
				return errors.New("sink failed")
			}
			return nil
		})),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want sink failure")
	}
	if len(store.saved) == 0 {
		t.Fatal("saved checkpoints = 0, want checkpoint with pending workflow started")
	}
	payload, err := decodeCheckpointPayload[int](store.saved[len(store.saved)-1].Payload)
	if err != nil {
		t.Fatalf("decode checkpoint payload: %v", err)
	}
	if payload.PendingEvent == nil || payload.PendingEvent.Type != EventWorkflowStarted {
		t.Fatalf("pending event = %+v, want workflow started", payload.PendingEvent)
	}
	if payload.PendingEvent.Sequence <= payload.EventCursor {
		t.Fatalf(
			"pending sequence = %d, cursor = %d, want pending after cursor",
			payload.PendingEvent.Sequence,
			payload.EventCursor,
		)
	}
}

func TestWorkflowCheckpointerFinishesFailedRun(t *testing.T) {
	store := &recordingCheckpointer{}
	wantErr := errors.New("boom")
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, _ string) (int, error) {
		return 0, wantErr
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc", gopact.WithRunID("run-1"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, wantErr)
	}
	if len(store.finished) == 0 || store.finished[len(store.finished)-1].Status != CheckpointFailed {
		t.Fatalf("finished checkpoint = %+v, want failed", store.finished)
	}
}

func TestWorkflowFailedEventFailureLeavesPendingEvent(t *testing.T) {
	store := &recordingCheckpointer{}
	wantErr := errors.New("boom")
	bodyRuns := 0
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, _ string) (int, error) {
		bodyRuns++
		return 0, wantErr
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventWorkflowFailed {
				return errors.New("sink failed")
			}
			return nil
		})),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want failure")
	}
	if len(store.finished) != 0 {
		t.Fatalf("finished checkpoints = %d, want terminal event confirmation first", len(store.finished))
	}
	if len(store.saved) == 0 || store.saved[len(store.saved)-1].Status != CheckpointRunning {
		t.Fatalf("saved checkpoints = %+v, want running checkpoint with pending terminal", store.saved)
	}
	payload, err := decodeCheckpointPayload[int](store.saved[len(store.saved)-1].Payload)
	if err != nil {
		t.Fatalf("decode checkpoint payload: %v", err)
	}
	if payload.PendingEvent == nil || payload.PendingEvent.Type != EventWorkflowFailed {
		t.Fatalf("pending event = %+v, want workflow failed", payload.PendingEvent)
	}
	if payload.PendingEvent.Sequence <= payload.EventCursor {
		t.Fatalf(
			"pending sequence = %d, cursor = %d, want pending after cursor",
			payload.PendingEvent.Sequence,
			payload.EventCursor,
		)
	}
	if payload.PendingTerm != CheckpointFailed {
		t.Fatalf("pending terminal = %q, want %q", payload.PendingTerm, CheckpointFailed)
	}
	expireRecordingLease(t, store, "run-1")
	_, err = compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "run-1"}))
	if err == nil || !strings.Contains(err.Error(), string(CheckpointFailed)) {
		t.Fatalf("resumed Invoke() error = %v, want failed terminal", err)
	}
	if bodyRuns != 1 {
		t.Fatalf("body runs = %d, want no terminal replay rerun", bodyRuns)
	}
}

func TestWorkflowNodeFailedEventFailureFailsRun(t *testing.T) {
	wantErr := errors.New("boom")
	sinkErr := errors.New("node failed sink")
	wf := New[string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, _ string) (int, error) {
		return 0, wantErr
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeFailed {
				return sinkErr
			}
			return nil
		})),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
}

func TestWorkflowNodeFailedEventFailureFinishesFailedRun(t *testing.T) {
	store := &recordingCheckpointer{}
	wantErr := errors.New("boom")
	sinkErr := errors.New("node failed sink")
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, _ string) (int, error) {
		return 0, wantErr
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeFailed {
				return sinkErr
			}
			return nil
		})),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
	if len(store.finished) != 0 {
		t.Fatalf("finished checkpoints = %d, want terminal event confirmation first", len(store.finished))
	}
	if len(store.saved) == 0 || store.saved[len(store.saved)-1].Status != CheckpointRunning {
		t.Fatalf("saved checkpoints = %+v, want running checkpoint with pending terminal", store.saved)
	}
}

func TestWorkflowNodeCompletedEventFailureLeavesPendingEvent(t *testing.T) {
	store := &recordingCheckpointer{}
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "abc",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeCompleted {
				return errors.New("sink failed")
			}
			return nil
		})),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want sink failure")
	}
	if len(store.saved) == 0 {
		t.Fatal("saved checkpoints = 0, want checkpoint with pending node completed")
	}
	payload, err := decodeCheckpointPayload[int](store.saved[len(store.saved)-1].Payload)
	if err != nil {
		t.Fatalf("decode checkpoint payload: %v", err)
	}
	if payload.PendingEvent == nil || payload.PendingEvent.Type != EventNodeCompleted {
		t.Fatalf("pending event = %+v, want node completed", payload.PendingEvent)
	}
	if len(payload.Outputs) != 1 || payload.Outputs[0] != 3 {
		t.Fatalf("outputs = %v, want committed output 3", payload.Outputs)
	}
}

func TestWorkflowCompileRejectsNilStore(t *testing.T) {
	for _, store := range []Store{nil, (*MemoryStore)(nil)} {
		wf := New[string, int]("example", WithStore(store))
		plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
			return len(input), nil
		})
		wf.Entry(plan)
		wf.Exit(plan)

		if _, err := wf.compile(); err == nil {
			t.Fatal("Compile() error = nil, want nil store error")
		}
	}
}

func TestWorkflowCompileAcceptsParallelism(t *testing.T) {
	wf := New[string, int]("example", WithMaxParallelism(2))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	if _, err := wf.compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestWorkflowResumeLoadsCheckpointPayload(t *testing.T) {
	const leaseDuration = 37 * time.Second
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	wf := New[string, string](
		"example",
		WithStore(storeWithCheckpointer(store)),
		WithCheckpointLease(leaseDuration, 10*time.Second),
	)
	planRuns := 0
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		planRuns++
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input + "!", nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	payload, err := encodeCheckpointPayloadWithMeta[string](runState{
		queue:     []activation{{node: "report", input: "saved"}},
		scheduled: map[string]int{"plan": 1, "report": 1},
		completed: map[string]int{"plan": 1},
		buckets:   map[joinBucketKey]*joinBucket{},
	}, nil, 2, compiled.checkpointMeta(checkpointPayloadMeta{}))
	if err != nil {
		t.Fatalf("encodeCheckpointPayload() error = %v", err)
	}
	store.records["run-1"] = workflowCheckpointRecord(compiled, "run-1", 3, CheckpointRunning, payload)

	got, err := compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID:        "run-1",
		CheckpointID: "checkpoint:run-1",
	}))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != "saved!" {
		t.Fatalf("Invoke() = %q, want saved!", got)
	}
	if planRuns != 0 {
		t.Fatalf("plan runs = %d, want 0", planRuns)
	}
	if len(store.saved) == 0 {
		t.Fatal("saved checkpoints = 0, want resume claim before node execution")
	}
	if store.saved[0].LeaseDuration != leaseDuration {
		t.Fatalf("claim lease duration = %v, want %v", store.saved[0].LeaseDuration, leaseDuration)
	}
	claim, err := decodeCheckpointPayload[string](store.saved[0].Payload)
	if err != nil {
		t.Fatalf("decode claim payload: %v", err)
	}
	if claim.OwnerID == "" {
		t.Fatal("claim owner id is empty")
	}
	if !claim.LeaseExpiresAt.After(time.Now()) {
		t.Fatalf("claim lease expires at %v, want future time", claim.LeaseExpiresAt)
	}
	if len(store.finished) == 0 || store.finished[len(store.finished)-1].Status != CheckpointCompleted {
		t.Fatalf("finished checkpoint = %+v, want completed", store.finished)
	}
}

func TestWorkflowResumeRejectsUnexpiredOwnerLease(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	planRuns := 0
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		planRuns++
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	leaseExpiresAt := time.Now().Add(time.Hour)
	payload, err := encodeCheckpointPayloadWithMeta[string](runState{
		queue:     []activation{{node: "plan", input: "saved"}},
		scheduled: map[string]int{"plan": 1},
		completed: map[string]int{},
		buckets:   map[joinBucketKey]*joinBucket{},
	}, nil, 1, compiled.checkpointMeta(checkpointPayloadMeta{
		OwnerID: "active-owner", LeaseExpiresAt: leaseExpiresAt, ClaimSequence: 1,
	}))
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	record := workflowCheckpointRecord(compiled, "run-1", 3, CheckpointRunning, payload)
	record.OwnerID = "active-owner"
	record.LeaseExpiresAt = leaseExpiresAt
	record.ClaimSequence = 1
	store.records["run-1"] = record

	_, err = compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "run-1"}))
	if !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("Invoke() error = %v, want ErrCheckpointConflict", err)
	}
	if planRuns != 0 {
		t.Fatalf("plan runs = %d, want 0", planRuns)
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved checkpoints = %d, want 0", len(store.saved))
	}
}

func TestWorkflowResumeContinuesEventSequenceFromCheckpointCursor(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	payload, err := encodeCheckpointPayloadWithMeta[string](runState{
		queue:     []activation{{node: "plan", input: "saved"}},
		scheduled: map[string]int{"plan": 1},
		completed: map[string]int{},
		buckets:   map[joinBucketKey]*joinBucket{},
	}, nil, 1, compiled.checkpointMeta(checkpointPayloadMeta{
		EventCursor: 5,
	}))
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	store.records["run-1"] = workflowCheckpointRecord(compiled, "run-1", 3, CheckpointRunning, payload)
	var sequences []int64

	_, err = compiled.Invoke(context.Background(), "ignored",
		WithResume(ResumeRequest{RunID: "run-1"}),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			sequences = append(sequences, event.Sequence)
			return nil
		})),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(sequences) == 0 || sequences[0] != 6 {
		t.Fatalf("event sequences = %v, want first sequence 6", sequences)
	}
}

func TestWorkflowResumeReplaysPendingEventBeforeNewEvents(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	wf := New[string, string]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	payload, err := encodeCheckpointPayloadWithMeta[string](runState{
		queue:     []activation{{node: "plan", input: "saved"}},
		scheduled: map[string]int{"plan": 1},
		completed: map[string]int{},
		buckets:   map[joinBucketKey]*joinBucket{},
	}, nil, 1, compiled.checkpointMeta(checkpointPayloadMeta{
		EventCursor: 5,
		PendingEvent: &gopact.Event{
			Sequence: 6,
			Type:     EventWorkflowFailed,
			Summary:  "pending",
		},
	}))
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	store.records["run-1"] = workflowCheckpointRecord(compiled, "run-1", 3, CheckpointRunning, payload)
	var events []gopact.Event

	_, err = compiled.Invoke(context.Background(), "ignored",
		WithResume(ResumeRequest{RunID: "run-1"}),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		})),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("events = %v, want pending, checkpoint loaded, and resumed", events)
	}
	if events[0].Sequence != 6 || events[0].Type != EventWorkflowFailed || events[0].Summary != "pending" {
		t.Fatalf("first event = %+v, want pending event at sequence 6", events[0])
	}
	if events[1].Sequence != 7 || events[1].Type != EventCheckpointLoaded {
		t.Fatalf("second event = %+v, want checkpoint loaded at sequence 7", events[1])
	}
	if events[2].Sequence != 8 || events[2].Type != EventWorkflowResumed {
		t.Fatalf("third event = %+v, want workflow resumed at sequence 8", events[2])
	}
	latest, err := decodeCheckpointPayload[string](store.records["run-1"].Payload)
	if err != nil {
		t.Fatalf("decode latest payload: %v", err)
	}
	if latest.PendingEvent != nil {
		t.Fatalf("pending event = %+v, want cleared after replay", latest.PendingEvent)
	}
}

func TestWorkflowResumeReplaysPendingEventBeforeCompletingTerminalCheckpoint(t *testing.T) {
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{}}
	wf := New[string, int]("example", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	payload, err := encodeCheckpointPayloadWithMeta[int](runState{
		queue:     nil,
		scheduled: map[string]int{"plan": 1},
		completed: map[string]int{"plan": 1},
		buckets:   map[joinBucketKey]*joinBucket{},
	}, []int{3}, 2, compiled.checkpointMeta(checkpointPayloadMeta{
		EventCursor: 3,
		PendingTerm: CheckpointCompleted,
		PendingEvent: &gopact.Event{
			Sequence: 4,
			Type:     EventWorkflowCompleted,
		},
	}))
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	store.records["run-1"] = workflowCheckpointRecord(compiled, "run-1", 3, CheckpointRunning, payload)
	var events []gopact.Event

	got, err := compiled.Invoke(context.Background(), "ignored",
		WithResume(ResumeRequest{RunID: "run-1"}),
		gopact.WithStrictEventSink(gopact.EventSinkFunc(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		})),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 3 {
		t.Fatalf("Invoke() = %d, want 3", got)
	}
	if len(events) != 1 || events[0].Sequence != 4 || events[0].Type != EventWorkflowCompleted {
		t.Fatalf("events = %+v, want only pending completed event", events)
	}
	latest, err := decodeCheckpointPayload[int](store.records["run-1"].Payload)
	if err != nil {
		t.Fatalf("decode latest payload: %v", err)
	}
	if latest.PendingEvent != nil {
		t.Fatalf("pending event = %+v, want cleared after replay", latest.PendingEvent)
	}
}

func TestWorkflowResumeRejectsConflictingOptions(t *testing.T) {
	compiled := testCompiledWorkflow(t)

	_, err := compiled.Invoke(context.Background(), "abc",
		WithResume(ResumeRequest{RunID: "run-1"}),
		WithResume(ResumeRequest{RunID: "run-2"}),
	)
	if err == nil {
		t.Fatal("Invoke() error = nil, want conflicting resume options")
	}
}

func TestWorkflowRejectsUnknownRunExtension(t *testing.T) {
	compiled := testCompiledWorkflow(t)

	_, err := compiled.Invoke(context.Background(), "abc", testRunOptionFunc(func(cfg *gopact.RunConfig) {
		if cfg.Extensions == nil {
			cfg.Extensions = map[string]any{}
		}
		cfg.Extensions["other.runtime"] = true
	}))
	if err == nil {
		t.Fatal("Invoke() error = nil, want unknown run extension")
	}
}

func TestWorkflowCompileRejectsTypeMismatch(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	report := testNode(wf, "report", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want type mismatch")
	}
}

func TestWorkflowCompileRejectsDuplicateNodeName(t *testing.T) {
	wf := New[string, string]("example")
	first := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	second := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(first)
	wf.Exit(second)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want duplicate node name")
	}
}

func TestWorkflowCompileRejectsEmptyNodeName(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want empty node name")
	}
}

func TestWorkflowCompileRejectsDuplicateRoute(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	plan.Route(func(context.Context, string) (Dispatch, error) { return Dispatch{}, nil })
	plan.Route(func(context.Context, string) (Dispatch, error) { return Dispatch{}, nil })
	wf.Entry(plan)
	wf.Exit(plan)

	_, err := wf.compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want duplicate route")
	}
}

func TestWorkflowMutatorPanicsAfterCompile(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	if _, err := wf.compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	defer func() {
		if got := recover(); got != "workflow already compiled" {
			t.Fatalf("panic = %v, want workflow already compiled", got)
		}
	}()
	wf.Node("next", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
}

func TestWorkflowNodeMutatorPanicsAfterCompile(t *testing.T) {
	wf := New[string, string]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		return input, nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	if _, err := wf.compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	defer func() {
		if got := recover(); got != "workflow already compiled" {
			t.Fatalf("panic = %v, want workflow already compiled", got)
		}
	}()
	plan.Route(func(context.Context, string) (Dispatch, error) { return Dispatch{}, nil })
}

func testCompiledWorkflow(t *testing.T) *compiled[string, int] {
	t.Helper()
	wf := testWorkflow(t)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func testWorkflow(t *testing.T) *Workflow[string, int] {
	t.Helper()
	wf := New[string, int]("example")
	plan := testNode(wf, "plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	wf.Entry(plan)
	wf.Exit(plan)
	return wf
}

func workflowCheckpointRecord[I, O any](compiled *compiled[I, O], runID string, version int64, status CheckpointStatus, payload []byte) CheckpointRecord {
	now := time.Now()
	return CheckpointRecord{
		ID: "checkpoint:" + runID, SessionID: "session:" + runID, RunID: runID, WorkflowName: compiled.name,
		TopologyVersion: compiled.topologyVersion, SchemaVersion: checkpointSchemaVersion,
		Version: version, Status: status, Payload: append([]byte(nil), payload...), ReplayStatus: ReplayUnknown,
		CreatedAt: now, UpdatedAt: now,
	}
}

type recordingCheckpointer struct {
	mu       sync.Mutex
	records  map[string]CheckpointRecord
	created  []CheckpointRecord
	saved    []CheckpointRecord
	finished []CheckpointRecord
	renewed  []CheckpointLease
}

type failingWorkflowStore struct {
	err error
}

func (store failingWorkflowStore) Create(context.Context, CheckpointRecord) error {
	return store.err
}

func (store failingWorkflowStore) Load(context.Context, string) (CheckpointRecord, error) {
	return CheckpointRecord{}, store.err
}

func (store failingWorkflowStore) Claim(context.Context, CheckpointRecord, int64) error {
	return store.err
}

func (store failingWorkflowStore) Save(context.Context, CheckpointRecord, int64) error {
	return store.err
}

func (store failingWorkflowStore) Finish(context.Context, CheckpointRecord, int64) error {
	return store.err
}

func (store failingWorkflowStore) RenewLease(context.Context, CheckpointLease) error {
	return store.err
}

func (store failingWorkflowStore) Append(context.Context, runlog.Record) error {
	return store.err
}

func (store failingWorkflowStore) List(context.Context, runlog.Query) ([]runlog.Record, error) {
	return nil, store.err
}

func (store failingWorkflowStore) ListCheckpoints(context.Context, CheckpointHistoryRequest) ([]CheckpointRecord, error) {
	return nil, store.err
}

func (store failingWorkflowStore) AppendFenced(context.Context, runlog.Record, runlog.Fence) error {
	return store.err
}

type countingPlugin struct {
	name   string
	setups int
}

type eventTypePlugin struct {
	name     string
	register func(*Registry) error
}

func (p eventTypePlugin) Name() string {
	return p.name
}

func (p eventTypePlugin) Setup(_ context.Context, r *Registry) error {
	return p.register(r)
}

func (p *countingPlugin) Name() string {
	return p.name
}

func (p *countingPlugin) Setup(context.Context, *Registry) error {
	p.setups++
	return nil
}

type testRunOptionFunc func(*gopact.RunConfig)

func (f testRunOptionFunc) ApplyRunOption(cfg *gopact.RunConfig) {
	f(cfg)
}

func (r *recordingCheckpointer) Create(_ context.Context, rec CheckpointRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = map[string]CheckpointRecord{}
	}
	r.created = append(r.created, rec)
	r.records[rec.RunID] = rec
	return nil
}

func (r *recordingCheckpointer) Load(_ context.Context, runID string) (CheckpointRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[runID]
	if !ok {
		return CheckpointRecord{}, ErrCheckpointNotFound
	}
	return rec, nil
}

func (r *recordingCheckpointer) Claim(_ context.Context, candidate CheckpointRecord, version int64) error {
	if version <= 0 || candidate.Version != version || candidate.Status != CheckpointRunning ||
		candidate.OwnerID == "" || candidate.ClaimSequence <= 0 || candidate.LeaseExpiresAt.IsZero() {
		return ErrInvalidCheckpoint
	}
	if err := validateCheckpointRecord(candidate); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if !candidate.LeaseExpiresAt.After(now) {
		return ErrInvalidCheckpoint
	}
	current, ok := r.records[candidate.RunID]
	if !ok {
		return ErrCheckpointNotFound
	}
	if current.Version != version || (current.Status != CheckpointRunning && current.Status != CheckpointInterrupted) ||
		current.LeaseExpiresAt.After(now) || candidate.ClaimSequence != current.ClaimSequence+1 {
		return ErrCheckpointConflict
	}
	if !sameCheckpointIdentity(current, candidate) {
		return ErrCheckpointMismatch
	}
	candidate.Version = version + 1
	r.saved = append(r.saved, candidate)
	r.records[candidate.RunID] = candidate
	return nil
}

func (r *recordingCheckpointer) Save(_ context.Context, rec CheckpointRecord, version int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = map[string]CheckpointRecord{}
	}
	rec.Version = version + 1
	r.saved = append(r.saved, rec)
	r.records[rec.RunID] = rec
	return nil
}

func (r *recordingCheckpointer) Finish(_ context.Context, rec CheckpointRecord, _ int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = map[string]CheckpointRecord{}
	}
	r.finished = append(r.finished, rec)
	r.records[rec.RunID] = rec
	return nil
}

func (r *recordingCheckpointer) RenewLease(_ context.Context, lease CheckpointLease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renewed = append(r.renewed, lease)
	rec, ok := r.records[lease.RunID]
	if !ok || rec.Status != CheckpointRunning || rec.OwnerID != lease.OwnerID || rec.ClaimSequence != lease.ClaimSequence {
		return ErrCheckpointLeaseLost
	}
	rec.LeaseExpiresAt = lease.ExpiresAt
	r.records[lease.RunID] = rec
	return nil
}
