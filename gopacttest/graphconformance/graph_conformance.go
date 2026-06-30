// Package graphconformance provides reusable graph workflow contract tests.
package graphconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

var ErrGraphConformanceFailed = errors.New("gopacttest: graph conformance failed")

type GraphConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

func CheckGraphConformance(ctx context.Context) []GraphConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	return []GraphConformanceResult{
		checkBranchRoutesSelectedTarget(ctx),
		checkBranchRoutesMultipleTargets(ctx),
		checkBranchCanEndWithNoTargets(ctx),
		checkBranchRejectsMissingTarget(ctx),
		checkBranchResumeUsesCheckpointQueue(ctx),
		checkDAGFanInRunsJoinAfterParents(ctx),
		checkDynamicFanOutResumesIncompleteTargets(ctx),
		checkDynamicFanOutRunsAllTargets(ctx),
		checkDynamicFanOutEmptyCompletes(ctx),
		checkRunnableNodeRunsSubgraph(ctx),
		checkRunnableNodeStreamsNestedEvents(ctx),
		checkRunnableNodeInheritsRuntimeIDs(ctx),
		checkRunnableNodeCheckpointInheritanceIsolation(ctx),
		checkFailedNodeStopsSuccessors(ctx),
		checkCanceledNodeStopsSuccessors(ctx),
	}
}

func RequireGraphConformance(t testing.TB) {
	t.Helper()

	for _, result := range CheckGraphConformance(context.Background()) {
		if !result.Passed {
			t.Fatalf("graph conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkBranchRoutesSelectedTarget(ctx context.Context) GraphConformanceResult {
	const name = "branch-routes-selected-target"
	g := graph.New[traceState]()
	g.AddNode("decide", appendTrace("decide"))
	g.AddNode("high", appendTrace("high"))
	g.AddNode("low", appendTrace("low"))
	g.AddEdge(graph.Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return []string{"high"}, nil
	})
	g.AddEdge("high", graph.End)
	g.AddEdge("low", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"decide", "high"})
}

func checkBranchRoutesMultipleTargets(ctx context.Context) GraphConformanceResult {
	const name = "branch-routes-multiple-targets"
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", appendTrace("right"))
	g.AddEdge(graph.Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "right"}, nil
	})
	g.AddEdge("left", graph.End)
	g.AddEdge("right", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"split", "left", "right"})
}

func checkBranchCanEndWithNoTargets(ctx context.Context) GraphConformanceResult {
	const name = "branch-can-end-with-no-targets"
	g := graph.New[traceState]()
	g.AddNode("decide", appendTrace("decide"))
	g.AddNode("unused", appendTrace("unused"))
	g.AddEdge(graph.Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return nil, nil
	})

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"decide"})
}

func checkBranchRejectsMissingTarget(ctx context.Context) GraphConformanceResult {
	const name = "branch-rejects-missing-target"
	g := graph.New[traceState]()
	g.AddNode("decide", appendTrace("decide"))
	g.AddEdge(graph.Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return []string{"missing"}, nil
	})

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}))
	if err == nil || !strings.Contains(err.Error(), `missing target "missing"`) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want missing target error", err))
	}
	if len(events) < 2 || events[len(events)-1].Type != gopact.EventRunFailed {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want final run_failed", eventTypes(events)))
	}
	return passedGraphConformance(name)
}

func checkBranchResumeUsesCheckpointQueue(ctx context.Context) GraphConformanceResult {
	const name = "branch-resume-uses-checkpoint-queue"
	store := &checkpointStore{
		latest: graph.Checkpoint[traceState]{
			ID:       "checkpoint-branch",
			ThreadID: "thread-branch",
			Step:     1,
			Node:     "decide",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"decide"}},
			Queue:    []string{"next"},
		},
		hasLatest: true,
	}
	g := graph.New[traceState]()
	g.AddNode("decide", failNode("checkpointed branch source reran"))
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return nil, errors.New("checkpointed branch decision reran")
	})
	g.AddNode("next", appendTrace("next"))
	g.AddEdge(graph.Start, "decide")
	g.AddEdge("next", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{},
		graph.WithThreadID("thread-branch"),
		graph.WithCheckpointLoader[traceState](store),
	)
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"decide", "next"})
}

func checkDAGFanInRunsJoinAfterParents(ctx context.Context) GraphConformanceResult {
	const name = "dag-fan-in-runs-join-after-parents"
	g := graph.New[traceState]()
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", appendTrace("right"))
	g.AddNode("join", appendTrace("join"))
	g.AddEdge(graph.Start, "left")
	g.AddEdge(graph.Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"left", "right", "join"})
}

func checkDynamicFanOutResumesIncompleteTargets(ctx context.Context) GraphConformanceResult {
	const name = "dynamic-fan-out-resumes-incomplete-targets"
	wantErr := errors.New("stop at right")
	store := &checkpointStore{}
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("join", func(context.Context, traceState) (traceState, error) {
		return traceState{}, errors.New("join ran before right completed")
	})
	g.AddEdge(graph.Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "right"}, nil
	})
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	if _, err := run.Invoke(ctx, traceState{}, graph.WithCheckpointer[traceState](store)); !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("initial run error = %v, want %v", err, wantErr))
	}
	if len(store.checkpoints) < 2 || !reflect.DeepEqual(store.checkpoints[1].Queue, []string{"right"}) {
		return failedGraphConformance(name, fmt.Errorf("checkpoint queue = %+v, want [right]", store.checkpoints))
	}

	resume := &checkpointStore{latest: store.checkpoints[1], hasLatest: true}
	resume.latest.ThreadID = "thread-fan-out"
	g = graph.New[traceState]()
	g.AddNode("split", failNode("checkpointed split reran"))
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return nil, errors.New("checkpointed fan-out branch reran")
	})
	g.AddNode("left", failNode("completed fan-out target reran"))
	g.AddNode("right", appendTrace("right"))
	g.AddNode("join", appendTrace("join"))
	g.AddEdge(graph.Start, "split")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", graph.End)

	resumed, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := resumed.Invoke(ctx, traceState{},
		graph.WithThreadID("thread-fan-out"),
		graph.WithCheckpointLoader[traceState](resume),
	)
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"split", "left", "right", "join"})
}

func checkDynamicFanOutRunsAllTargets(ctx context.Context) GraphConformanceResult {
	const name = "dynamic-fan-out-runs-all-targets"
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", appendTrace("left"))
	g.AddNode("middle", appendTrace("middle"))
	g.AddNode("right", appendTrace("right"))
	g.AddEdge(graph.Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "middle", "right"}, nil
	})
	g.AddEdge("left", graph.End)
	g.AddEdge("middle", graph.End)
	g.AddEdge("right", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"split", "left", "middle", "right"})
}

func checkDynamicFanOutEmptyCompletes(ctx context.Context) GraphConformanceResult {
	const name = "dynamic-fan-out-empty-completes"
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("unused", appendTrace("unused"))
	g.AddEdge(graph.Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return nil, nil
	})

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"split"})
}

func checkRunnableNodeRunsSubgraph(ctx context.Context) GraphConformanceResult {
	const name = "runnable-node-runs-subgraph"
	sub := graph.New[traceState]()
	sub.AddNode("sub-one", appendTrace("sub-one"))
	sub.AddNode("sub-two", appendTrace("sub-two"))
	sub.AddEdge(graph.Start, "sub-one")
	sub.AddEdge("sub-one", "sub-two")
	sub.AddEdge("sub-two", graph.End)
	subrun, err := sub.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	g := graph.New[traceState]()
	g.AddNode("before", appendTrace("before"))
	g.AddRunnableNode("subgraph", subrun)
	g.AddNode("after", appendTrace("after"))
	g.AddEdge(graph.Start, "before")
	g.AddEdge("before", "subgraph")
	g.AddEdge("subgraph", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"before", "sub-one", "sub-two", "after"})
}

func checkRunnableNodeStreamsNestedEvents(ctx context.Context) GraphConformanceResult {
	const name = "runnable-node-streams-nested-events"
	ids := gopact.RuntimeIDs{RunID: "run-graph-conformance", ThreadID: "thread-graph-conformance"}
	sub := graph.New[traceState]()
	sub.AddNode("child", appendTrace("child"))
	sub.AddEdge(graph.Start, "child")
	sub.AddEdge("child", graph.End)
	subrun, err := sub.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	g := graph.New[traceState]()
	g.AddRunnableNode("subgraph", subrun)
	g.AddEdge(graph.Start, "subgraph")
	g.AddEdge("subgraph", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithRuntimeIDs(ids)))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	for _, event := range events[2:6] {
		if event.IDs != ids {
			return failedGraphConformance(name, fmt.Errorf("nested event ids = %+v, want %+v", event.IDs, ids))
		}
		if event.Metadata[graph.EventMetadataParentNode] != "subgraph" ||
			event.Metadata[graph.EventMetadataParentStep] != 1 {
			return failedGraphConformance(name, fmt.Errorf("nested event metadata = %+v, want parent subgraph step 1", event.Metadata))
		}
	}
	output, ok := events[6].StepSnapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("parent output type = %T, want traceState", events[6].StepSnapshot.Output))
	}
	return requireTrace(name, output, []string{"child"})
}

func checkRunnableNodeInheritsRuntimeIDs(ctx context.Context) GraphConformanceResult {
	const name = "runnable-node-inherits-runtime-ids"
	ids := gopact.RuntimeIDs{RunID: "run-subgraph", ThreadID: "thread-subgraph", TraceID: "trace-subgraph"}
	var got gopact.RuntimeIDs
	var ok bool
	sub := graph.New[traceState]()
	sub.AddNode("child", func(ctx context.Context, state traceState) (traceState, error) {
		got, ok = gopact.RuntimeIDsFromContext(ctx)
		state.Trace = append(state.Trace, "child")
		return state, nil
	})
	sub.AddEdge(graph.Start, "child")
	sub.AddEdge("child", graph.End)
	subrun, err := sub.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	g := graph.New[traceState]()
	g.AddRunnableNode("subgraph", subrun)
	g.AddEdge(graph.Start, "subgraph")
	g.AddEdge("subgraph", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	gotState, err := run.Invoke(ctx, traceState{}, graph.WithRuntimeIDs(ids))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	if !ok || got != ids {
		return failedGraphConformance(name, fmt.Errorf("runtime ids = %+v/%v, want %+v", got, ok, ids))
	}
	return requireTrace(name, gotState, []string{"child"})
}

func checkRunnableNodeCheckpointInheritanceIsolation(ctx context.Context) GraphConformanceResult {
	const name = "runnable-node-checkpoint-inheritance-isolation"
	const threadID = "thread-subgraph-checkpoint"
	parentStore := &checkpointStore{}
	childStore := &checkpointStore{}

	sub := graph.New[traceState]()
	sub.AddNode("child-one", appendTrace("child-one"))
	sub.AddNode("child-two", appendTrace("child-two"))
	sub.AddEdge(graph.Start, "child-one")
	sub.AddEdge("child-one", "child-two")
	sub.AddEdge("child-two", graph.End)
	subrun, err := sub.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	g := graph.New[traceState]()
	g.AddRunnableNode("subgraph", subrun, graph.WithCheckpointer[traceState](childStore))
	g.AddEdge(graph.Start, "subgraph")
	g.AddEdge("subgraph", graph.End)
	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	got, err := run.Invoke(ctx, traceState{},
		graph.WithThreadID(threadID),
		graph.WithCheckpointer[traceState](parentStore),
	)
	if err != nil {
		return failedGraphConformance(name, err)
	}
	if result := requireTrace(name, got, []string{"child-one", "child-two"}); !result.Passed {
		return result
	}
	if len(parentStore.checkpoints) != 1 || parentStore.checkpoints[0].Node != "subgraph" {
		return failedGraphConformance(name, fmt.Errorf("parent checkpoints = %+v, want only subgraph", parentStore.checkpoints))
	}
	if len(childStore.checkpoints) != 2 {
		return failedGraphConformance(name, fmt.Errorf("child checkpoint count = %d, want 2", len(childStore.checkpoints)))
	}
	if childStore.checkpoints[0].ThreadID != threadID || childStore.checkpoints[1].ThreadID != threadID {
		return failedGraphConformance(name, fmt.Errorf("child checkpoint thread ids = %q/%q, want %q", childStore.checkpoints[0].ThreadID, childStore.checkpoints[1].ThreadID, threadID))
	}
	if childStore.checkpoints[0].Node != "child-one" || childStore.checkpoints[1].Node != "child-two" {
		return failedGraphConformance(name, fmt.Errorf("child checkpoint nodes = %q/%q, want child-one/child-two", childStore.checkpoints[0].Node, childStore.checkpoints[1].Node))
	}
	return passedGraphConformance(name)
}

func checkFailedNodeStopsSuccessors(ctx context.Context) GraphConformanceResult {
	const name = "failed-node-stops-successors"
	wantErr := errors.New("node failed")
	afterRan := false
	g := graph.New[traceState]()
	g.AddNode("fail", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("after", func(context.Context, traceState) (traceState, error) {
		afterRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "fail")
	g.AddEdge("fail", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	if afterRan {
		return failedGraphConformance(name, errors.New("successor ran after failed node"))
	}
	if len(events) < 2 || events[len(events)-1].Type != gopact.EventRunFailed {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want final run_failed", eventTypes(events)))
	}
	return passedGraphConformance(name)
}

func checkCanceledNodeStopsSuccessors(ctx context.Context) GraphConformanceResult {
	const name = "canceled-node-stops-successors"
	afterRan := false
	g := graph.New[traceState]()
	g.AddNode("cancel", func(context.Context, traceState) (traceState, error) {
		return traceState{Trace: []string{"cancel"}}, context.Canceled
	})
	g.AddNode("after", func(context.Context, traceState) (traceState, error) {
		afterRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "cancel")
	g.AddEdge("cancel", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, context.Canceled) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want context canceled", err))
	}
	if afterRan {
		return failedGraphConformance(name, errors.New("successor ran after canceled node"))
	}
	if len(events) < 2 || events[len(events)-1].Type != gopact.EventRunCanceled {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want final run_canceled", eventTypes(events)))
	}
	snapshot := events[len(events)-1].StepSnapshot
	if snapshot == nil || snapshot.Phase != gopact.StepCanceled {
		return failedGraphConformance(name, fmt.Errorf("canceled snapshot = %+v, want step_canceled", snapshot))
	}
	got, ok := snapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("canceled snapshot output type = %T, want traceState", snapshot.Output))
	}
	return requireTrace(name, got, []string{"cancel"})
}

type traceState struct {
	Trace []string
}

type checkpointStore struct {
	checkpoints []graph.Checkpoint[traceState]
	latest      graph.Checkpoint[traceState]
	hasLatest   bool
}

func (s *checkpointStore) Put(ctx context.Context, checkpoint graph.Checkpoint[traceState]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.checkpoints = append(s.checkpoints, checkpoint)
	s.latest = checkpoint
	s.hasLatest = true
	return nil
}

func (s *checkpointStore) Latest(ctx context.Context, threadID string) (graph.Checkpoint[traceState], bool, error) {
	if err := ctx.Err(); err != nil {
		return graph.Checkpoint[traceState]{}, false, err
	}
	if !s.hasLatest || s.latest.ThreadID != threadID {
		return graph.Checkpoint[traceState]{}, false, nil
	}
	return s.latest, true, nil
}

func appendTrace(name string) graph.NodeFunc[traceState] {
	return func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, name)
		return state, nil
	}
}

func failNode(message string) graph.NodeFunc[traceState] {
	return func(context.Context, traceState) (traceState, error) {
		return traceState{}, errors.New(message)
	}
}

func requireTrace(name string, got traceState, want []string) GraphConformanceResult {
	if !reflect.DeepEqual(got.Trace, want) {
		return failedGraphConformance(name, fmt.Errorf("trace = %v, want %v", got.Trace, want))
	}
	return passedGraphConformance(name)
}

func collectRunEvents(seq func(func(gopact.Event, error) bool)) ([]gopact.Event, error) {
	var events []gopact.Event
	var firstErr error
	for event, err := range seq {
		events = append(events, event)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return events, firstErr
}

func eventTypes(events []gopact.Event) []gopact.EventType {
	out := make([]gopact.EventType, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func passedGraphConformance(name string) GraphConformanceResult {
	return GraphConformanceResult{Case: name, Passed: true}
}

func failedGraphConformance(name string, err error) GraphConformanceResult {
	return GraphConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrGraphConformanceFailed, err),
	}
}
