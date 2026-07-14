package workflow

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

const (
	checkpointProcessModeEnv = "GOPACT_CHECKPOINT_PROCESS_MODE"
	checkpointProcessFileEnv = "GOPACT_CHECKPOINT_PROCESS_FILE"
)

type checkpointProcessInput struct {
	Value string
}

type checkpointProcessOutput struct {
	Value   string
	Dynamic any
}

type checkpointProcessDynamic struct {
	Label string
}

type checkpointProcessContext struct {
	Visits int
}

type checkpointProcessSnapshot struct {
	Checkpoints []CheckpointRecord
	Events      []runlog.Record
}

func TestWorkflowResumeInFreshProcess(t *testing.T) {
	path := t.TempDir() + "/checkpoint.json"
	runCheckpointProcess(t, "writer", path)
	runCheckpointProcess(t, "reader", path)
}

func TestWorkflowCheckpointProcessHelper(t *testing.T) {
	mode := os.Getenv(checkpointProcessModeEnv)
	if mode == "" {
		t.Skip("checkpoint process helper")
	}
	path := os.Getenv(checkpointProcessFileEnv)
	store := NewMemoryStore()
	if mode == "reader" || mode == "iterator-reader" {
		restoreCheckpointProcessSnapshot(t, store, path)
	}
	if mode == "renamed-writer" {
		gob.RegisterName("workflow.checkpointProcessRenamed.v1", checkpointProcessRenamed{})
		payload, err := encodeCheckpointPayloadWithMeta[checkpointProcessRenamed](runState{
			queue: []activation{{id: "act-1", node: "node", input: checkpointProcessRenamed{Value: "old"}}},
		}, nil, 1, checkpointPayloadMeta{})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if mode == "renamed-reader" {
		wf := New[checkpointProcessRenamed, checkpointProcessRenamed]("renamed")
		node := wf.Node("node", func(_ context.Context, input checkpointProcessRenamed) (checkpointProcessRenamed, error) {
			return input, nil
		})
		wf.Entry(node)
		wf.Exit(node)
		if _, err := wf.compile(); err != nil {
			t.Fatal(err)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeCheckpointPayload[checkpointProcessRenamed](payload); err == nil || !strings.Contains(err.Error(), "name not registered") {
			t.Fatalf("decode error = %v, want stable unknown type error", err)
		}
		return
	}
	if mode == "registration-conflict" {
		name := checkpointTypeIdentity(reflect.TypeOf(checkpointConflictVictim{}))
		gob.RegisterName(name, checkpointConflictOccupier{})
		wf := New[checkpointConflictVictim, checkpointConflictVictim]("conflict")
		node := wf.Node("node", func(_ context.Context, input checkpointConflictVictim) (checkpointConflictVictim, error) {
			return input, nil
		})
		wf.Entry(node)
		wf.Exit(node)
		_, err := wf.compile()
		if err == nil || !strings.Contains(err.Error(), "register checkpoint type") || !strings.Contains(err.Error(), "duplicate types") {
			t.Fatalf("Compile() error = %v, want duplicate registration error", err)
		}
		return
	}
	if mode == "pointer-value-conflict" {
		wf := New[int, int]("pointer-value-conflict", WithCheckpointTypes(checkpointPointerConflict{}, (*checkpointPointerConflict)(nil)))
		node := wf.Node("node", func(_ context.Context, input int) (int, error) { return input, nil })
		wf.Entry(node)
		wf.Exit(node)
		_, err := wf.compile()
		if err == nil || !strings.Contains(err.Error(), "register checkpoint type") || !strings.Contains(err.Error(), "duplicate names") {
			t.Fatalf("Compile() error = %v, want pointer/value conflict", err)
		}
		return
	}
	if mode == "iterator-writer" || mode == "iterator-reader" {
		runCheckpointIteratorProcess(t, mode, store, path)
		return
	}
	wf := checkpointProcessWorkflow(store)
	switch mode {
	case "writer":
		writeCheckpointProcess(t, wf, store, path)
	case "reader":
		readCheckpointProcess(t, wf)
	default:
		t.Fatalf("unknown checkpoint process mode %q", mode)
	}
}

func writeCheckpointProcess(t *testing.T, wf *Workflow[checkpointProcessInput, checkpointProcessOutput], store *MemoryStore, path string) {
	t.Helper()
	_, err := wf.Invoke(t.Context(), checkpointProcessInput{Value: "fresh"}, gopact.WithRunID("fresh-process-run"))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	writeCheckpointProcessSnapshot(t, store, path, "fresh-process-run")
}

func readCheckpointProcess(t *testing.T, wf *Workflow[checkpointProcessInput, checkpointProcessOutput]) {
	t.Helper()
	output, err := wf.Invoke(t.Context(), checkpointProcessInput{}, WithResume(ResumeRequest{
		RunID: "fresh-process-run",
		Resolutions: []InterruptResolution{{
			InterruptID: "approval-1",
			PayloadRef:  "artifact://approved",
		}},
	}))
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	dynamic, ok := output.Dynamic.(checkpointProcessDynamic)
	if output.Value != "fresh!" || !ok || dynamic.Label != "dynamic" {
		t.Fatalf("Resume() output = %+v, want fresh!", output)
	}
}

func checkpointProcessWorkflow(store Store) *Workflow[checkpointProcessInput, checkpointProcessOutput] {
	wf := New[checkpointProcessInput, checkpointProcessOutput](
		"fresh-process",
		WithStore(store),
		WithCheckpointTypes(checkpointProcessDynamic{}, reflect.TypeOf(checkpointProcessDynamic{})),
	)
	state := wf.Context(func(checkpointProcessInput) checkpointProcessContext {
		return checkpointProcessContext{Visits: 1}
	})
	prepare := wf.Node("prepare", func(ctx context.Context, input checkpointProcessInput) (checkpointProcessOutput, error) {
		current, err := state.Get(ctx)
		if err != nil {
			return checkpointProcessOutput{}, err
		}
		current.Visits++
		if err := state.Set(ctx, current); err != nil {
			return checkpointProcessOutput{}, err
		}
		return checkpointProcessOutput{Value: input.Value + "!", Dynamic: checkpointProcessDynamic{Label: "dynamic"}}, nil
	})
	approve := wf.Node("approve", func(ctx context.Context, input checkpointProcessOutput) (checkpointProcessOutput, error) {
		current, err := state.Get(ctx)
		if err != nil {
			return checkpointProcessOutput{}, err
		}
		if current.Visits != 2 {
			return checkpointProcessOutput{}, errors.New("workflow context was not restored")
		}
		return input, nil
	})
	approve.Guard(BeforeRun("approval", GuardFunc[checkpointProcessOutput, checkpointProcessOutput](
		func(context.Context, GuardContext[checkpointProcessOutput, checkpointProcessOutput]) (GuardDecision[checkpointProcessOutput, checkpointProcessOutput], error) {
			return GuardInterrupt[checkpointProcessOutput, checkpointProcessOutput]{
				Request: InterruptRequest{ID: "approval-1", Subject: "approval"},
			}, nil
		},
	)))
	prepare.Route(func(context.Context, checkpointProcessOutput) (Dispatch, error) { return prepare.To(approve), nil })
	wf.Entry(prepare)
	wf.Edge(prepare, approve)
	wf.Exit(approve)
	return wf
}

type checkpointProcessRenamed struct {
	Value string
}

func TestWorkflowCheckpointTypeRenameReturnsError(t *testing.T) {
	path := t.TempDir() + "/renamed.gob"
	runCheckpointProcess(t, "renamed-writer", path)
	runCheckpointProcess(t, "renamed-reader", path)
}

type checkpointConcurrentType struct {
	Value int
}

func TestWorkflowCheckpointTypeRegistrationIsConcurrentAndIdempotent(t *testing.T) {
	const workers = 16
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			wf := New[checkpointConcurrentType, checkpointConcurrentType]("concurrent", WithCheckpointTypes(checkpointConcurrentType{}))
			node := wf.Node("node", func(_ context.Context, input checkpointConcurrentType) (checkpointConcurrentType, error) {
				return input, nil
			})
			wf.Entry(node)
			wf.Exit(node)
			_, err := wf.compile()
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkflowCheckpointTypeInterfaceTopologyUsesEscapeHatch(t *testing.T) {
	wf := New[any, any]("interface", WithCheckpointTypes(checkpointProcessDynamic{}))
	node := wf.Node("node", func(_ context.Context, input any) (any, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	output, err := wf.Invoke(t.Context(), checkpointProcessDynamic{Label: "dynamic"})
	if err != nil {
		t.Fatal(err)
	}
	if dynamic, ok := output.(checkpointProcessDynamic); !ok || dynamic.Label != "dynamic" {
		t.Fatalf("Invoke() output = %#v", output)
	}
}

func TestWorkflowCheckpointTypeValidation(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil", value: nil, want: "checkpoint type is nil"},
		{name: "interface", value: reflect.TypeOf((*any)(nil)).Elem(), want: "must be concrete"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wf := New[int, int]("invalid", WithCheckpointTypes(test.value))
			node := wf.Node("node", func(_ context.Context, input int) (int, error) { return input, nil })
			wf.Entry(node)
			wf.Exit(node)
			_, err := wf.compile()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

type checkpointConflictVictim struct{}
type checkpointConflictOccupier struct{}
type checkpointPointerConflict struct{}

func TestWorkflowCheckpointTypeRegistrationConflictReturnsError(t *testing.T) {
	runCheckpointProcess(t, "registration-conflict", t.TempDir()+"/unused")
}

func TestWorkflowCheckpointTypePointerAndValueConflictReturnsError(t *testing.T) {
	runCheckpointProcess(t, "pointer-value-conflict", t.TempDir()+"/unused")
}

type checkpointIteratorCursor struct {
	Index int
}

func TestWorkflowCheckpointTypeIteratorResumeInFreshProcess(t *testing.T) {
	path := t.TempDir() + "/iterator.json"
	runCheckpointProcess(t, "iterator-writer", path)
	runCheckpointProcess(t, "iterator-reader", path)
}

func runCheckpointIteratorProcess(t *testing.T, mode string, store *MemoryStore, path string) {
	t.Helper()
	cursor := checkpointIteratorCursor{}
	wf := New[string, int]("fresh-iterator", WithStore(store), WithMaxParallelism(1), WithCheckpointTypes(checkpointIteratorCursor{}))
	plan := wf.Node("plan", func(_ context.Context, input string) (string, error) { return input, nil })
	branch := wf.Node("branch", func(_ context.Context, input int) (int, error) { return input, nil })
	sequence := func(start int) iter.Seq2[int, error] {
		return func(yield func(int, error) bool) {
			for index := start; index < 3; index++ {
				cursor.Index = index + 1
				if !yield(index+1, nil) {
					return
				}
			}
		}
	}
	plan.Route(func(context.Context, string) (Dispatch, error) {
		return plan.EachIter(branch, func(context.Context) iter.Seq2[int, error] { return sequence(0) }, WithIterReplay(
			func() checkpointIteratorCursor { return cursor },
			func(_ context.Context, saved checkpointIteratorCursor) iter.Seq2[int, error] {
				cursor = saved
				return sequence(saved.Index)
			},
		)), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, branch)
	wf.Exit(branch)
	if mode == "iterator-writer" {
		failIteratorWriter(t, wf, store, path)
		return
	}
	resumeIteratorReader(t, wf)
}

func failIteratorWriter(t *testing.T, wf *Workflow[string, int], store *MemoryStore, path string) {
	t.Helper()
	sinkErr := errors.New("stop after first iterator item")
	_, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("fresh-iterator-run"), gopact.WithStrictEventHandler(
		func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeCompleted && event.Summary == "branch" {
				return sinkErr
			}
			return nil
		},
	))
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want sink error", err)
	}
	store.MemoryCheckpointer.mu.Lock()
	record := store.MemoryCheckpointer.records["fresh-iterator-run"]
	record.LeaseExpiresAt = record.UpdatedAt.Add(-1)
	store.MemoryCheckpointer.records[record.RunID] = record
	store.MemoryCheckpointer.history[record.RunID][len(store.MemoryCheckpointer.history[record.RunID])-1] = record
	store.MemoryCheckpointer.mu.Unlock()
	writeCheckpointProcessSnapshot(t, store, path, "fresh-iterator-run")
}

func resumeIteratorReader(t *testing.T, wf *Workflow[string, int]) {
	t.Helper()
	var outputs []int
	for output, err := range wf.InvokeStream(t.Context(), "ignored", WithResume(ResumeRequest{RunID: "fresh-iterator-run"})) {
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, output)
	}
	if !reflect.DeepEqual(outputs, []int{1, 2, 3}) {
		t.Fatalf("resumed outputs = %v, want [1 2 3]", outputs)
	}
}

func TestWorkflowCheckpointTypeInterruptProgressRoundTrip(t *testing.T) {
	progress := &checkpointInterruptProgress{Events: []gopact.Event{{Type: EventGuardInterrupted}, {Type: EventWorkflowInterrupted}}, Next: 1}
	payload, err := encodeCheckpointPayloadWithMeta[string](runState{}, nil, 1, checkpointPayloadMeta{InterruptProgress: progress})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCheckpointPayload[string](payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.InterruptProgress == nil || decoded.InterruptProgress.Next != 1 || len(decoded.InterruptProgress.Events) != 2 ||
		decoded.InterruptProgress.Events[0].Type != EventGuardInterrupted || decoded.InterruptProgress.Events[1].Type != EventWorkflowInterrupted {
		t.Fatalf("interrupt progress = %+v", decoded.InterruptProgress)
	}
}

func runCheckpointProcess(t *testing.T, mode, path string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestWorkflowCheckpointProcessHelper$")
	cmd.Env = append(os.Environ(), checkpointProcessModeEnv+"="+mode, checkpointProcessFileEnv+"="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s process failed: %v\n%s", mode, err, output)
	}
}

func writeCheckpointProcessSnapshot(t *testing.T, store *MemoryStore, path, runID string) {
	t.Helper()
	checkpoints, err := store.ListCheckpoints(t.Context(), CheckpointHistoryRequest{RunID: runID, Limit: 1024})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), runlog.Query{RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(checkpointProcessSnapshot{Checkpoints: checkpoints, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func restoreCheckpointProcessSnapshot(t *testing.T, store *MemoryStore, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot checkpointProcessSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, checkpoint := range snapshot.Checkpoints {
		store.MemoryCheckpointer.restore(checkpoint)
	}
	for _, event := range snapshot.Events {
		if err := store.MemoryLog.Append(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
}
