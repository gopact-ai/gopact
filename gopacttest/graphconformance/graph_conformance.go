// Package graphconformance provides reusable graph workflow contract tests.
package graphconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

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
		checkCheckpointVerifiesEffectArtifactsBeforeResume(ctx),
		checkCheckpointEmitsEffectReplayPlan(ctx),
		checkDAGFanInRunsJoinAfterParents(ctx),
		checkDAGFanInStopsWhenParentFails(ctx),
		checkDAGFanInPreservesEdgeOrder(ctx),
		checkDynamicFanOutResumesIncompleteTargets(ctx),
		checkDynamicFanOutRunsAllTargets(ctx),
		checkDynamicFanOutEmptyCompletes(ctx),
		checkDynamicFanOutStopsOnTargetFailure(ctx),
		checkParallelFanOutRunsTargetsConcurrently(ctx),
		checkParallelFanOutCancelsSiblingsOnFailure(ctx),
		checkParallelFanOutMergeErrorStopsSuccessors(ctx),
		checkParallelFanOutCheckpointingFallsBackToSequential(ctx),
		checkLoopBranchExits(ctx),
		checkLoopStepLimitFails(ctx),
		checkStepExportResumesCompletedBoundary(ctx),
		checkStepExportEmitsEffectReplayPlan(ctx),
		checkInterruptedStepExportResumesWithRequest(ctx),
		checkStepExportVerifiesArtifactsBeforeResume(ctx),
		checkRunnableNodeRunsSubgraph(ctx),
		checkRunnableNodeStreamsNestedEvents(ctx),
		checkNodeEmitsNestedEvents(ctx),
		checkRunnableNodeInheritsRuntimeIDs(ctx),
		checkRunnableNodeCheckpointInheritanceIsolation(ctx),
		checkTopologyExportStable(ctx),
		checkSchemaGuardRejectsInvalidNodeInput(ctx),
		checkSchemaGuardRejectsInvalidNodeOutput(ctx),
		checkSchemaGuardRejectsInvalidResumeState(ctx),
		checkSchemaGuardExportsTopologyContracts(ctx),
		checkNodeMiddlewareRecordsEffects(ctx),
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

func checkCheckpointVerifiesEffectArtifactsBeforeResume(ctx context.Context) GraphConformanceResult {
	const name = "checkpoint-verifies-effect-artifacts-before-resume"
	const threadID = "thread-checkpoint-artifacts"
	wantErr := errors.New("checkpoint artifact integrity mismatch")
	store := &checkpointStore{
		latest: graph.Checkpoint[traceState]{
			ID:       "checkpoint-artifacts",
			ThreadID: threadID,
			Step:     1,
			Node:     "persisted",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"persisted"}},
			Queue:    []string{"after"},
			Effects: []gopact.EffectRecord{
				{
					ID:      "artifact-effect",
					Type:    "artifact_write",
					Applied: true,
					Artifacts: []gopact.ArtifactRef{
						{ID: "checkpoint-effect-artifact", SHA256: "sha-checkpoint"},
					},
				},
			},
		},
		hasLatest: true,
	}
	afterRan := false
	g := graph.New[traceState]()
	g.AddNode("persisted", failNode("checkpointed node reran"))
	g.AddNode("after", func(context.Context, traceState) (traceState, error) {
		afterRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "persisted")
	g.AddEdge("persisted", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	var gotRefs []gopact.ArtifactRef
	events, err := collectRunEvents(run.Run(ctx, traceState{},
		graph.WithThreadID(threadID),
		graph.WithCheckpointLoader[traceState](store),
		graph.WithArtifactVerifier(graph.ArtifactVerifierFunc(func(_ context.Context, refs []gopact.ArtifactRef) error {
			gotRefs = append(gotRefs, refs...)
			return wantErr
		})),
	))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	if afterRan {
		return failedGraphConformance(name, errors.New("successor ran after failed checkpoint artifact verification"))
	}
	if len(gotRefs) != 1 || gotRefs[0].ID != "checkpoint-effect-artifact" {
		return failedGraphConformance(name, fmt.Errorf("verified artifact refs = %+v, want checkpoint effect artifact", gotRefs))
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventRunFailed,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	if events[1].Node != "persisted" || events[1].Step != 1 {
		return failedGraphConformance(name, fmt.Errorf("run failed event = %+v, want checkpoint boundary", events[1]))
	}
	return passedGraphConformance(name)
}

func checkCheckpointEmitsEffectReplayPlan(ctx context.Context) GraphConformanceResult {
	const name = "checkpoint-emits-effect-replay-plan"
	const threadID = "thread-checkpoint-replay"
	store := &checkpointStore{
		latest: graph.Checkpoint[traceState]{
			ID:       "checkpoint-replay",
			ThreadID: threadID,
			Step:     1,
			Node:     "persisted",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"persisted"}},
			Queue:    []string{"after"},
			Effects: []gopact.EffectRecord{
				{
					ID:           "artifact-1",
					Type:         "artifact_write",
					Applied:      true,
					ReplayPolicy: gopact.EffectReplaySkip,
				},
			},
		},
		hasLatest: true,
	}
	g := graph.New[traceState]()
	g.AddNode("persisted", failNode("checkpointed node reran"))
	g.AddNode("after", appendTrace("after"))
	g.AddEdge(graph.Start, "persisted")
	g.AddEdge("persisted", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{},
		graph.WithThreadID(threadID),
		graph.WithCheckpointLoader[traceState](store),
	))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	if len(events) < 2 || events[1].Type != gopact.EventCheckpointLoaded {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want checkpoint_loaded", eventTypes(events)))
	}
	plan, ok := gopact.EventEffectReplayPlan(events[1])
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("checkpoint loaded metadata = %+v, want effect replay plan", events[1].Metadata))
	}
	if plan.StepID == "" || plan.Node != "persisted" || plan.Step != 1 {
		return failedGraphConformance(name, fmt.Errorf("replay plan = %+v, want checkpoint step identity", plan))
	}
	if len(plan.Decisions) != 1 {
		return failedGraphConformance(name, fmt.Errorf("replay decisions = %+v, want one decision", plan.Decisions))
	}
	decision := plan.Decisions[0]
	if decision.Effect.ID != "artifact-1" || decision.Action != gopact.EffectReplayActionSkip ||
		decision.ReplayPolicy != gopact.EffectReplaySkip {
		return failedGraphConformance(name, fmt.Errorf("replay decision = %+v, want skip", decision))
	}
	return passedGraphConformance(name)
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

func checkDAGFanInStopsWhenParentFails(ctx context.Context) GraphConformanceResult {
	const name = "dag-fan-in-stops-when-parent-fails"
	wantErr := errors.New("right failed")
	joinRan := false
	g := graph.New[traceState]()
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("join", func(context.Context, traceState) (traceState, error) {
		joinRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "left")
	g.AddEdge(graph.Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	if joinRan {
		return failedGraphConformance(name, errors.New("join ran after a fan-in parent failed"))
	}
	if len(events) == 0 || events[len(events)-1].Type != gopact.EventRunFailed {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want final run_failed", eventTypes(events)))
	}
	return passedGraphConformance(name)
}

func checkDAGFanInPreservesEdgeOrder(ctx context.Context) GraphConformanceResult {
	const name = "dag-fan-in-preserves-edge-order"
	joinRuns := 0
	g := graph.New[traceState]()
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", appendTrace("right"))
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		joinRuns++
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(graph.Start, "right")
	g.AddEdge(graph.Start, "left")
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
	if joinRuns != 1 {
		return failedGraphConformance(name, fmt.Errorf("join runs = %d, want 1", joinRuns))
	}
	return requireTrace(name, got, []string{"right", "left", "join"})
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

func checkDynamicFanOutStopsOnTargetFailure(ctx context.Context) GraphConformanceResult {
	const name = "dynamic-fan-out-stops-on-target-failure"
	wantErr := errors.New("middle failed")
	rightRan := false
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", appendTrace("left"))
	g.AddNode("middle", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("right", func(context.Context, traceState) (traceState, error) {
		rightRan = true
		return traceState{}, nil
	})
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
	events, err := collectRunEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	if rightRan {
		return failedGraphConformance(name, errors.New("fan-out target ran after sibling target failed"))
	}
	if len(events) < 2 || events[len(events)-1].Type != gopact.EventRunFailed {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want final run_failed", eventTypes(events)))
	}
	return passedGraphConformance(name)
}

func checkParallelFanOutRunsTargetsConcurrently(ctx context.Context) GraphConformanceResult {
	const name = "parallel-fan-out-runs-targets-concurrently"
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started := make(chan string, 2)
	release := make(chan struct{})
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", blockingAppendTrace("left", started, release))
	g.AddNode("right", blockingAppendTrace("right", started, release))
	g.AddNode("join", appendTrace("join"))
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
	type runResult struct {
		events []gopact.Event
		err    error
	}
	resultc := make(chan runResult, 1)
	go func() {
		events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithParallelFanOut(mergeTraceFanOut)))
		resultc <- runResult{events: events, err: err}
	}()

	gotStarted := map[string]bool{}
	for len(gotStarted) < 2 {
		select {
		case node := <-started:
			gotStarted[node] = true
		case <-time.After(500 * time.Millisecond):
			cancel()
			close(release)
			result := <-resultc
			return failedGraphConformance(name, fmt.Errorf("started nodes = %v, want left and right; run error = %v", gotStarted, result.err))
		}
	}
	if !gotStarted["left"] || !gotStarted["right"] {
		cancel()
		close(release)
		result := <-resultc
		return failedGraphConformance(name, fmt.Errorf("started nodes = %v, want left and right; run error = %v", gotStarted, result.err))
	}
	close(release)

	result := <-resultc
	if result.err != nil {
		return failedGraphConformance(name, result.err)
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	}
	if !reflect.DeepEqual(eventTypes(result.events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(result.events), wantTypes))
	}
	output, ok := result.events[8].StepSnapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("join output type = %T, want traceState", result.events[8].StepSnapshot.Output))
	}
	return requireTrace(name, output, []string{"split", "left", "right", "join"})
}

func checkParallelFanOutCancelsSiblingsOnFailure(ctx context.Context) GraphConformanceResult {
	const name = "parallel-fan-out-cancels-siblings-on-failure"
	wantErr := errors.New("left failed")
	rightCanceled := make(chan struct{})
	afterRan := false
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("right", func(ctx context.Context, state traceState) (traceState, error) {
		<-ctx.Done()
		close(rightCanceled)
		return state, ctx.Err()
	})
	g.AddNode("after", func(context.Context, traceState) (traceState, error) {
		afterRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "right"}, nil
	})
	g.AddEdge("left", "after")
	g.AddEdge("right", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithParallelFanOut(mergeTraceFanOut)))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	select {
	case <-rightCanceled:
	case <-time.After(500 * time.Millisecond):
		return failedGraphConformance(name, errors.New("right node did not observe sibling failure cancellation"))
	}
	if afterRan {
		return failedGraphConformance(name, errors.New("successor ran after fan-out target failed"))
	}
	if len(events) == 0 || events[len(events)-1].Type != gopact.EventRunFailed {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want final run_failed", eventTypes(events)))
	}
	return passedGraphConformance(name)
}

func checkParallelFanOutMergeErrorStopsSuccessors(ctx context.Context) GraphConformanceResult {
	const name = "parallel-fan-out-merge-error-stops-successors"
	wantErr := errors.New("merge failed")
	afterRan := false
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", appendTrace("right"))
	g.AddNode("after", func(context.Context, traceState) (traceState, error) {
		afterRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "right"}, nil
	})
	g.AddEdge("left", "after")
	g.AddEdge("right", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithParallelFanOut(func(context.Context, traceState, []graph.FanOutResult[traceState]) (traceState, error) {
		return traceState{}, wantErr
	})))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	if afterRan {
		return failedGraphConformance(name, errors.New("successor ran after fan-out merge failed"))
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventNodeStarted,
		gopact.EventRunFailed,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	return passedGraphConformance(name)
}

func checkParallelFanOutCheckpointingFallsBackToSequential(ctx context.Context) GraphConformanceResult {
	const name = "parallel-fan-out-checkpointing-falls-back-to-sequential"
	store := &checkpointStore{}
	g := graph.New[traceState]()
	g.AddNode("split", appendTrace("split"))
	g.AddNode("left", appendTrace("left"))
	g.AddNode("right", appendTrace("right"))
	g.AddNode("join", appendTrace("join"))
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
	failMerge := func(context.Context, traceState, []graph.FanOutResult[traceState]) (traceState, error) {
		return traceState{}, errors.New("parallel fan-out merge should not run when checkpointing is active")
	}
	got, err := run.Invoke(ctx, traceState{},
		graph.WithParallelFanOut(failMerge),
		graph.WithCheckpointer[traceState](store),
	)
	if err != nil {
		return failedGraphConformance(name, err)
	}
	if result := requireTrace(name, got, []string{"split", "left", "right", "join"}); !result.Passed {
		return result
	}
	if len(store.checkpoints) != 4 {
		return failedGraphConformance(name, fmt.Errorf("checkpoint count = %d, want 4", len(store.checkpoints)))
	}
	for i, wantNode := range []string{"split", "left", "right", "join"} {
		if store.checkpoints[i].Node != wantNode {
			return failedGraphConformance(name, fmt.Errorf("checkpoint[%d].Node = %q, want %q", i, store.checkpoints[i].Node, wantNode))
		}
	}
	return passedGraphConformance(name)
}

func checkLoopBranchExits(ctx context.Context) GraphConformanceResult {
	const name = "loop-branch-exits"
	g := graph.New[traceState]()
	g.AddNode("loop", appendTrace("loop"))
	g.AddEdge(graph.Start, "loop")
	g.AddBranch("loop", func(_ context.Context, state traceState) ([]string, error) {
		if len(state.Trace) < 3 {
			return []string{"loop"}, nil
		}
		return []string{graph.End}, nil
	})

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		return failedGraphConformance(name, err)
	}
	return requireTrace(name, got, []string{"loop", "loop", "loop"})
}

func checkLoopStepLimitFails(ctx context.Context) GraphConformanceResult {
	const name = "loop-step-limit-fails"
	g := graph.New[traceState]()
	g.AddNode("loop", appendTrace("loop"))
	g.AddEdge(graph.Start, "loop")
	g.AddEdge("loop", "loop")

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	got, err := run.Invoke(ctx, traceState{}, graph.WithMaxSteps(2))
	if err == nil || !strings.Contains(err.Error(), "exceeded max steps 2") {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want max steps error", err))
	}
	return requireTrace(name, got, []string{"loop", "loop"})
}

func checkStepExportResumesCompletedBoundary(ctx context.Context) GraphConformanceResult {
	const name = "step-export-resumes-completed-boundary"
	ids := gopact.RuntimeIDs{RunID: "run-step-export", ThreadID: "thread-step-export"}
	g := graph.New[traceState]()
	g.AddNode("first", failNode("completed exported step reran"))
	g.AddNode("next", appendTrace("next"))
	g.AddEdge(graph.Start, "first")
	g.AddEdge("first", "next")
	g.AddEdge("next", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithStepExport(gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-step-export:1",
			Step:   1,
			Node:   "first",
			Phase:  gopact.StepCompleted,
			IDs:    ids,
			Output: traceState{Trace: []string{"first"}},
			Queue:  []string{"next"},
		},
	})))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	output, ok := events[3].StepSnapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("resumed output type = %T, want traceState", events[3].StepSnapshot.Output))
	}
	return requireTrace(name, output, []string{"first", "next"})
}

func checkStepExportEmitsEffectReplayPlan(ctx context.Context) GraphConformanceResult {
	const name = "step-export-emits-effect-replay-plan"
	g := graph.New[traceState]()
	g.AddNode("first", failNode("completed exported step reran"))
	g.AddNode("next", appendTrace("next"))
	g.AddEdge(graph.Start, "first")
	g.AddEdge("first", "next")
	g.AddEdge("next", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithStepExport(gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-step-replay:1",
			Step:   1,
			Node:   "first",
			Phase:  gopact.StepCompleted,
			IDs:    gopact.RuntimeIDs{RunID: "run-step-replay", ThreadID: "thread-step-replay"},
			Output: traceState{Trace: []string{"first"}},
			Queue:  []string{"next"},
			Effects: []gopact.EffectRecord{
				{
					ID:             "tool-1",
					Type:           "tool_call",
					Target:         "tools/search",
					Applied:        true,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "tool-1",
				},
			},
		},
	})))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	if len(events) < 2 || events[1].Type != gopact.EventStepImported {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want step_imported", eventTypes(events)))
	}
	plan, ok := gopact.EventEffectReplayPlan(events[1])
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("step imported metadata = %+v, want effect replay plan", events[1].Metadata))
	}
	if plan.StepID != "run-step-replay:1" || plan.Node != "first" || plan.Step != 1 {
		return failedGraphConformance(name, fmt.Errorf("replay plan = %+v, want imported step identity", plan))
	}
	if len(plan.Decisions) != 1 {
		return failedGraphConformance(name, fmt.Errorf("replay decisions = %+v, want one decision", plan.Decisions))
	}
	decision := plan.Decisions[0]
	if decision.Effect.ID != "tool-1" || decision.Action != gopact.EffectReplayActionReplay ||
		decision.ReplayPolicy != gopact.EffectReplayIdempotent ||
		decision.IdempotencyKey != "tool-1" {
		return failedGraphConformance(name, fmt.Errorf("replay decision = %+v, want idempotent replay", decision))
	}
	return passedGraphConformance(name)
}

func checkInterruptedStepExportResumesWithRequest(ctx context.Context) GraphConformanceResult {
	const name = "interrupted-step-export-resumes-with-request"
	ids := gopact.RuntimeIDs{RunID: "run-step-interrupt", ThreadID: "thread-step-interrupt"}
	g := graph.New[traceState]()
	g.AddNode("ask", failNode("interrupted exported step reran"))
	g.AddNode("answer", appendTrace("answer"))
	g.AddEdge(graph.Start, "ask")
	g.AddEdge("ask", "answer")
	g.AddEdge("answer", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{},
		graph.WithStepExport(gopact.StepExport{
			Version: 1,
			Step: gopact.StepSnapshot{
				ID:     "run-step-interrupt:1",
				Step:   1,
				Node:   "ask",
				Phase:  gopact.StepInterrupted,
				IDs:    ids,
				Output: traceState{Trace: []string{"ask"}},
				Queue:  []string{"answer"},
				Pending: &gopact.InterruptRecord{
					ID:     "interrupt-ask",
					Type:   gopact.InterruptInput,
					Reason: "need input",
				},
			},
		}),
		graph.WithResumeRequest(gopact.ResumeRequest{
			StepID:      "run-step-interrupt:1",
			InterruptID: "interrupt-ask",
			Payload:     "continue",
		}),
	))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventResumeReceived,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	if events[2].Metadata["interrupt_id"] != "interrupt-ask" ||
		events[2].Metadata["step_id"] != "run-step-interrupt:1" {
		return failedGraphConformance(name, fmt.Errorf("resume metadata = %+v, want interrupt and step ids", events[2].Metadata))
	}
	output, ok := events[4].StepSnapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("resumed output type = %T, want traceState", events[4].StepSnapshot.Output))
	}
	return requireTrace(name, output, []string{"ask", "answer"})
}

func checkStepExportVerifiesArtifactsBeforeResume(ctx context.Context) GraphConformanceResult {
	const name = "step-export-verifies-artifacts-before-resume"
	wantErr := errors.New("artifact integrity mismatch")
	afterRan := false
	g := graph.New[traceState]()
	g.AddNode("first", failNode("completed exported step reran"))
	g.AddNode("after", func(context.Context, traceState) (traceState, error) {
		afterRan = true
		return traceState{}, nil
	})
	g.AddEdge(graph.Start, "first")
	g.AddEdge("first", "after")
	g.AddEdge("after", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	var gotRefs []gopact.ArtifactRef
	events, err := collectRunEvents(run.Run(ctx, traceState{},
		graph.WithStepExport(gopact.StepExport{
			Version: 1,
			Step: gopact.StepSnapshot{
				ID:     "run-step-artifacts:1",
				Step:   1,
				Node:   "first",
				Phase:  gopact.StepCompleted,
				IDs:    gopact.RuntimeIDs{RunID: "run-step-artifacts", ThreadID: "thread-step-artifacts"},
				Output: traceState{Trace: []string{"first"}},
				Queue:  []string{"after"},
				Artifacts: []gopact.ArtifactRef{
					{ID: "step-artifact", SHA256: "sha-step"},
				},
				Effects: []gopact.EffectRecord{
					{
						ID:      "artifact-effect",
						Type:    "artifact_write",
						Applied: true,
						Artifacts: []gopact.ArtifactRef{
							{ID: "effect-artifact", SHA256: "sha-effect"},
						},
					},
				},
			},
		}),
		graph.WithArtifactVerifier(graph.ArtifactVerifierFunc(func(_ context.Context, refs []gopact.ArtifactRef) error {
			gotRefs = append(gotRefs, refs...)
			return wantErr
		})),
	))
	if !errors.Is(err, wantErr) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want %v", err, wantErr))
	}
	if afterRan {
		return failedGraphConformance(name, errors.New("successor ran after failed artifact verification"))
	}
	gotRefIDs := map[string]bool{}
	for _, ref := range gotRefs {
		gotRefIDs[ref.ID] = true
	}
	if len(gotRefs) != 2 || !gotRefIDs["step-artifact"] || !gotRefIDs["effect-artifact"] {
		return failedGraphConformance(name, fmt.Errorf("verified artifact refs = %+v, want step and effect artifacts", gotRefs))
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventRunFailed,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	if events[1].Node != "first" || events[1].Step != 1 {
		return failedGraphConformance(name, fmt.Errorf("run failed event = %+v, want imported step boundary", events[1]))
	}
	return passedGraphConformance(name)
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

func checkNodeEmitsNestedEvents(ctx context.Context) GraphConformanceResult {
	const name = "node-emits-nested-events"
	ids := gopact.RuntimeIDs{RunID: "run-graph-node-event", ThreadID: "thread-graph-node-event"}
	g := graph.New[traceState]()
	g.AddNode("delegate", func(ctx context.Context, state traceState) (traceState, error) {
		if !graph.EmitNodeEvent(ctx, gopact.Event{
			Type: gopact.EventA2ATaskCompleted,
			IDs:  ids,
			Metadata: map[string]any{
				"agent_name": "planner",
			},
		}, nil) {
			return state, graph.ErrNodeEventYieldStopped
		}
		state.Trace = append(state.Trace, "delegate")
		return state, nil
	})
	g.AddEdge(graph.Start, "delegate")
	g.AddEdge("delegate", graph.End)

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
		gopact.EventA2ATaskCompleted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	event := events[2]
	if event.Metadata[graph.EventMetadataParentNode] != "delegate" ||
		event.Metadata[graph.EventMetadataParentStep] != 1 {
		return failedGraphConformance(name, fmt.Errorf("nested event metadata = %+v, want parent delegate step 1", event.Metadata))
	}
	if event.Metadata["agent_name"] != "planner" {
		return failedGraphConformance(name, fmt.Errorf("nested event metadata = %+v, want agent_name", event.Metadata))
	}
	output, ok := events[3].StepSnapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("parent output type = %T, want traceState", events[3].StepSnapshot.Output))
	}
	return requireTrace(name, output, []string{"delegate"})
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

func checkTopologyExportStable(_ context.Context) GraphConformanceResult {
	const name = "topology-export-stable"
	subgraph := graph.New[traceState]()
	subgraph.AddNode("child", appendTrace("child"))
	subgraph.AddEdge(graph.Start, "child")
	subgraph.AddEdge("child", graph.End)
	subrun, err := subgraph.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	g := graph.New[traceState]()
	g.AddNode("left", appendTrace("left"))
	g.AddRunnableNode("right", subrun)
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

	topology := run.Topology()
	if topology.MaxSteps != 1024 {
		return failedGraphConformance(name, fmt.Errorf("max steps = %d, want 1024", topology.MaxSteps))
	}
	if !reflect.DeepEqual(topology.Joins, []graph.TopologyJoin{{Node: "join", Predecessors: []string{"left", "right"}}}) {
		return failedGraphConformance(name, fmt.Errorf("joins = %#v", topology.Joins))
	}
	kinds := map[string]graph.TopologyNodeKind{}
	for _, node := range topology.Nodes {
		kinds[node.Name] = node.Kind
	}
	if kinds[graph.Start] != graph.TopologyNodeBoundary ||
		kinds[graph.End] != graph.TopologyNodeBoundary ||
		kinds["left"] != graph.TopologyNodeFunction ||
		kinds["right"] != graph.TopologyNodeRunnable ||
		kinds["join"] != graph.TopologyNodeFunction {
		return failedGraphConformance(name, fmt.Errorf("node kinds = %#v", kinds))
	}
	return passedGraphConformance(name)
}

func checkSchemaGuardRejectsInvalidNodeInput(ctx context.Context) GraphConformanceResult {
	const name = "schema-guard-rejects-invalid-node-input"
	called := false
	g := graph.New[traceState]()
	g.AddNode("guarded", func(context.Context, traceState) (traceState, error) {
		called = true
		return traceState{}, nil
	})
	g.SetNodeInputSchema("guarded", traceMinItemsSchema(1))
	g.AddEdge(graph.Start, "guarded")
	g.AddEdge("guarded", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	_, err = run.Invoke(ctx, traceState{})
	if !errors.Is(err, graph.ErrSchemaGuardFailed) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want ErrSchemaGuardFailed", err))
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want ErrJSONSchemaValidationFailed", err))
	}
	if called {
		return failedGraphConformance(name, errors.New("guarded node ran after invalid input"))
	}
	return passedGraphConformance(name)
}

func checkSchemaGuardRejectsInvalidNodeOutput(ctx context.Context) GraphConformanceResult {
	const name = "schema-guard-rejects-invalid-node-output"
	g := graph.New[traceState]()
	g.AddNode("guarded", appendTrace("guarded"))
	g.SetNodeOutputSchema("guarded", traceMinItemsSchema(2))
	g.AddEdge(graph.Start, "guarded")
	g.AddEdge("guarded", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	_, err = run.Invoke(ctx, traceState{})
	if !errors.Is(err, graph.ErrSchemaGuardFailed) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want ErrSchemaGuardFailed", err))
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want ErrJSONSchemaValidationFailed", err))
	}
	return passedGraphConformance(name)
}

func checkSchemaGuardRejectsInvalidResumeState(ctx context.Context) GraphConformanceResult {
	const name = "schema-guard-rejects-invalid-resume-state"
	called := false
	g := graph.New[traceState]()
	g.SetStateSchema(traceMinItemsSchema(2))
	g.AddNode("one", appendTrace("one"))
	g.AddNode("next", func(_ context.Context, state traceState) (traceState, error) {
		called = true
		state.Trace = append(state.Trace, "next")
		return state, nil
	})
	g.AddEdge(graph.Start, "one")
	g.AddEdge("one", "next")
	g.AddEdge("next", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	_, err = run.Invoke(ctx, traceState{}, graph.WithStepExport(gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "one",
			Phase:  gopact.StepCompleted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1"},
			Output: traceState{Trace: []string{"one"}},
			Queue:  []string{"next"},
		},
	}))
	if !errors.Is(err, graph.ErrSchemaGuardFailed) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want ErrSchemaGuardFailed", err))
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		return failedGraphConformance(name, fmt.Errorf("run error = %v, want ErrJSONSchemaValidationFailed", err))
	}
	if called {
		return failedGraphConformance(name, errors.New("next node ran after invalid resumed state"))
	}
	return passedGraphConformance(name)
}

func checkSchemaGuardExportsTopologyContracts(_ context.Context) GraphConformanceResult {
	const name = "schema-guard-exports-topology-contracts"
	inputSchema := traceMinItemsSchema(0)
	outputSchema := traceMinItemsSchema(1)
	g := graph.New[traceState]()
	g.SetStateSchema(inputSchema)
	g.AddNode("guarded", appendTrace("guarded"))
	g.SetNodeInputSchema("guarded", inputSchema)
	g.SetNodeOutputSchema("guarded", outputSchema)
	g.AddEdge(graph.Start, "guarded")
	g.AddEdge("guarded", graph.End)
	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}

	topology := run.Topology()
	if len(topology.StateSchema) == 0 {
		return failedGraphConformance(name, errors.New("topology state schema is empty"))
	}
	for _, node := range topology.Nodes {
		if node.Name != "guarded" {
			continue
		}
		if len(node.InputSchema) == 0 || len(node.OutputSchema) == 0 {
			return failedGraphConformance(name, fmt.Errorf("node schemas = input:%#v output:%#v", node.InputSchema, node.OutputSchema))
		}
		node.InputSchema["type"] = "mutated"
		again := run.Topology()
		for _, againNode := range again.Nodes {
			if againNode.Name == "guarded" && againNode.InputSchema["type"] != "object" {
				return failedGraphConformance(name, fmt.Errorf("topology schema was mutable: %#v", againNode.InputSchema))
			}
		}
		return passedGraphConformance(name)
	}
	return failedGraphConformance(name, errors.New("guarded node missing from topology"))
}

func checkNodeMiddlewareRecordsEffects(ctx context.Context) GraphConformanceResult {
	const name = "node-middleware-records-effects"
	g := graph.New[traceState]()
	g.AddNode("work", appendTrace("node"))
	g.AddEdge(graph.Start, "work")
	g.AddEdge("work", graph.End)

	run, err := g.Compile()
	if err != nil {
		return failedGraphConformance(name, err)
	}
	events, err := collectRunEvents(run.Run(ctx, traceState{}, graph.WithNodeMiddleware(func(c *gopact.NodeContext) error {
		input, ok := c.Input.(traceState)
		if !ok {
			return fmt.Errorf("middleware input type = %T, want traceState", c.Input)
		}
		input.Trace = append(input.Trace, "before")
		c.Input = input

		if err := c.Next(); err != nil {
			return err
		}

		output, ok := c.Output.(traceState)
		if !ok {
			return fmt.Errorf("middleware output type = %T, want traceState", c.Output)
		}
		output.Trace = append(output.Trace, "after")
		c.Output = output
		c.AddEffect(gopact.EffectRecord{
			ID:      "tool-call-1",
			Type:    "tool_call",
			Target:  "local.echo",
			Applied: true,
		})
		return nil
	})))
	if err != nil {
		return failedGraphConformance(name, err)
	}
	wantTypes := []gopact.EventType{
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	}
	if !reflect.DeepEqual(eventTypes(events), wantTypes) {
		return failedGraphConformance(name, fmt.Errorf("events = %v, want %v", eventTypes(events), wantTypes))
	}
	if events[2].StepSnapshot == nil {
		return failedGraphConformance(name, errors.New("completed event missing step snapshot"))
	}
	output, ok := events[2].StepSnapshot.Output.(traceState)
	if !ok {
		return failedGraphConformance(name, fmt.Errorf("completed output type = %T, want traceState", events[2].StepSnapshot.Output))
	}
	if !reflect.DeepEqual(output.Trace, []string{"before", "node", "after"}) {
		return failedGraphConformance(name, fmt.Errorf("trace = %v, want middleware-wrapped node trace", output.Trace))
	}
	effects := events[2].StepSnapshot.Effects
	if len(effects) != 1 || effects[0].Type != "tool_call" || effects[0].Target != "local.echo" || !effects[0].Applied {
		return failedGraphConformance(name, fmt.Errorf("effects = %+v, want applied local tool call", effects))
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

func blockingAppendTrace(name string, started chan<- string, release <-chan struct{}) graph.NodeFunc[traceState] {
	return func(ctx context.Context, state traceState) (traceState, error) {
		select {
		case started <- name:
		case <-ctx.Done():
			return state, ctx.Err()
		}
		select {
		case <-release:
		case <-ctx.Done():
			return state, ctx.Err()
		}
		state.Trace = append(state.Trace, name)
		return state, nil
	}
}

func mergeTraceFanOut(_ context.Context, base traceState, results []graph.FanOutResult[traceState]) (traceState, error) {
	merged := traceState{Trace: append([]string(nil), base.Trace...)}
	for _, result := range results {
		if len(result.Output.Trace) < len(base.Trace) {
			return traceState{}, fmt.Errorf("fan-out result %q output trace is shorter than base", result.Node)
		}
		merged.Trace = append(merged.Trace, result.Output.Trace[len(base.Trace):]...)
	}
	return merged, nil
}

func traceMinItemsSchema(minItems int) gopact.JSONSchema {
	return gopact.JSONSchema{
		"type":     "object",
		"required": []any{"Trace"},
		"properties": map[string]any{
			"Trace": map[string]any{
				"type":     "array",
				"minItems": minItems,
				"items":    map[string]any{"type": "string"},
			},
		},
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
