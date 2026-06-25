package graph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

type traceState struct {
	Trace []string
}

func TestGraphRunExecutesNodesInEdgeOrder(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("plan", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "plan")
		return state, nil
	})
	g.AddNode("act", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "act")
		return state, nil
	})
	g.AddEdge(Start, "plan")
	g.AddEdge("plan", "act")
	g.AddEdge("act", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"plan", "act"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphRunPersistsCheckpointAfterEachNode(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()

	g.AddNode("one", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
		return state, nil
	})
	g.AddNode("two", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = run.Invoke(ctx, traceState{}, WithThreadID("thread-1"), WithCheckpointer(store))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	if len(store.checkpoints) != 2 {
		t.Fatalf("checkpoint count = %d, want 2", len(store.checkpoints))
	}
	if store.checkpoints[0].ThreadID != "thread-1" || store.checkpoints[0].Node != "one" {
		t.Fatalf("first checkpoint = %+v", store.checkpoints[0])
	}
	if store.checkpoints[1].Step != 2 || store.checkpoints[1].Node != "two" {
		t.Fatalf("second checkpoint = %+v", store.checkpoints[1])
	}
	if store.checkpoints[0].Phase != gopact.StepCompleted {
		t.Fatalf("first checkpoint phase = %q, want completed", store.checkpoints[0].Phase)
	}
	if !reflect.DeepEqual(store.checkpoints[0].Queue, []string{"two"}) {
		t.Fatalf("first checkpoint queue = %v, want [two]", store.checkpoints[0].Queue)
	}
	if store.checkpoints[0].IDs.ThreadID != "thread-1" {
		t.Fatalf("first checkpoint ids = %+v, want thread-1", store.checkpoints[0].IDs)
	}
}

func TestGraphRunPersistsCheckpointConfigVersion(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()

	g.AddNode("one", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = run.Invoke(ctx, traceState{}, WithThreadID("thread-1"), WithConfigVersion("config:v1"), WithCheckpointer(store))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	if len(store.checkpoints) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(store.checkpoints))
	}
	if store.checkpoints[0].ConfigVersion != "config:v1" {
		t.Fatalf("checkpoint config version = %q, want config:v1", store.checkpoints[0].ConfigVersion)
	}
}

func TestGraphRunLoadsLatestCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			IDs:      gopact.RuntimeIDs{RunID: "previous-run", ThreadID: "thread-1"},
			Step:     1,
			Node:     "one",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"one"}},
			Queue:    []string{"two"},
			Effects: []gopact.EffectRecord{
				{
					ID:           "artifact-1",
					Type:         "artifact_export",
					Applied:      true,
					ReplayPolicy: gopact.EffectReplaySkip,
				},
			},
			Metadata: map[string]any{
				"checkpoint_config_drift": map[string]any{
					"stored_version":  "config:v1",
					"current_version": "config:v2",
				},
			},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("checkpointed node should not run after restore")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(gopact.RuntimeIDs{RunID: "new-run", ThreadID: "thread-1"}), WithCheckpointLoader(store)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventCheckpointLoaded,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[1].Metadata["checkpoint_id"] != "checkpoint-1" {
		t.Fatalf("checkpoint loaded metadata = %+v, want checkpoint id", events[1].Metadata)
	}
	if events[1].Metadata["checkpoint_config_drift"] == nil {
		t.Fatalf("checkpoint loaded metadata = %+v, want config drift", events[1].Metadata)
	}
	requireReplayPlan(t, events[1], []gopact.EffectReplayAction{gopact.EffectReplayActionSkip})
	if events[2].Node != "two" || events[2].Step != 2 {
		t.Fatalf("resumed node event = %+v, want node two step 2", events[2])
	}
	output, ok := events[3].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("output type = %T, want traceState", events[3].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"one", "two"}) {
		t.Fatalf("resumed trace = %v, want [one two]", output.Trace)
	}
}

func TestGraphRunVerifiesCheckpointArtifactsBeforeResume(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			Step:     1,
			Node:     "one",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"one"}},
			Queue:    []string{"two"},
			Effects: []gopact.EffectRecord{
				{
					ID:      "artifact-effect",
					Type:    "artifact_write",
					Applied: true,
					Artifacts: []gopact.ArtifactRef{
						{ID: "effect-artifact", SHA256: "sha-1"},
					},
				},
			},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("checkpointed node should not run after restore")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	var gotRefs []gopact.ArtifactRef
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithThreadID("thread-1"),
		WithCheckpointLoader(store),
		WithArtifactVerifier(artifactVerifierFunc(func(ctx context.Context, refs []gopact.ArtifactRef) error {
			gotRefs = append(gotRefs, refs...)
			return nil
		})),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventCheckpointLoaded,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if gotArtifactIDs(gotRefs)[0] != "effect-artifact" {
		t.Fatalf("verified artifact refs = %+v, want effect-artifact", gotRefs)
	}
}

func TestGraphRunRejectsCheckpointWhenArtifactIntegrityFails(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("integrity mismatch")
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			Step:     1,
			Node:     "one",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"one"}},
			Queue:    []string{"two"},
			Effects: []gopact.EffectRecord{
				{
					ID:      "artifact-effect",
					Type:    "artifact_write",
					Applied: true,
					Artifacts: []gopact.ArtifactRef{
						{ID: "effect-artifact", SHA256: "sha-1"},
					},
				},
			},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("checkpointed node should not run after restore")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("node should not run after failed artifact verification")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithThreadID("thread-1"),
		WithCheckpointLoader(store),
		WithArtifactVerifier(artifactVerifierFunc(func(ctx context.Context, refs []gopact.ArtifactRef) error {
			return wantErr
		})),
	))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventRunFailed,
	)
	if events[1].Node != "one" || events[1].Step != 1 {
		t.Fatalf("run failed event = %+v, want checkpoint boundary", events[1])
	}
}

func TestGraphRunWithCheckpointStoreLoadsAndPersists(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			Step:     1,
			Node:     "one",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"one"}},
			Queue:    []string{"two"},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("checkpointed node should not run after restore")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithThreadID("thread-1"), WithCheckpointStore(store)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventCheckpointLoaded,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if len(store.checkpoints) != 1 {
		t.Fatalf("checkpoint writes = %d, want 1", len(store.checkpoints))
	}
	if store.checkpoints[0].Node != "two" {
		t.Fatalf("written checkpoint node = %q, want two", store.checkpoints[0].Node)
	}
}

func TestGraphRunResumesInterruptedCheckpointWithResumeRequest(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			IDs:      gopact.RuntimeIDs{RunID: "previous-run", ThreadID: "thread-1"},
			Step:     1,
			Node:     "ask",
			Phase:    gopact.StepInterrupted,
			State:    traceState{Trace: []string{"ask"}},
			Queue:    []string{"answer"},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-1",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
			},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("interrupted node should not rerun after checkpoint restore")
		return state, nil
	})
	g.AddNode("answer", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "answer")
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", "answer")
	g.AddEdge("answer", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithRuntimeIDs(gopact.RuntimeIDs{RunID: "new-run", ThreadID: "thread-1"}),
		WithCheckpointLoader(store),
		WithResumeRequest(gopact.ResumeRequest{
			CheckpointID: "checkpoint-1",
			InterruptID:  "interrupt-1",
			Payload:      "continue",
		}),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventCheckpointLoaded,
		gopact.EventResumeReceived,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[2].Metadata["interrupt_id"] != "interrupt-1" || events[2].Metadata["checkpoint_id"] != "checkpoint-1" {
		t.Fatalf("resume metadata = %+v, want checkpoint and interrupt ids", events[2].Metadata)
	}
	if events[3].Node != "answer" || events[3].Step != 2 {
		t.Fatalf("resumed node event = %+v, want node answer step 2", events[3])
	}
	output, ok := events[4].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("output type = %T, want traceState", events[4].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"ask", "answer"}) {
		t.Fatalf("resumed trace = %v, want [ask answer]", output.Trace)
	}
}

func TestGraphRunRejectsInterruptedCheckpointResumeWhenPayloadSchemaMismatches(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			IDs:      gopact.RuntimeIDs{RunID: "previous-run", ThreadID: "thread-1"},
			Step:     1,
			Node:     "ask",
			Phase:    gopact.StepInterrupted,
			State:    traceState{Trace: []string{"ask"}},
			Queue:    []string{"answer"},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-1",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
				ResumeSchema: gopact.JSONSchema{
					"type":     "object",
					"required": []any{"answer"},
					"properties": map[string]any{
						"answer": map[string]any{"type": "string"},
					},
				},
			},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("interrupted node should not rerun after checkpoint restore")
		return state, nil
	})
	g.AddNode("answer", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("answer node should not run after invalid resume payload")
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", "answer")
	g.AddEdge("answer", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithRuntimeIDs(gopact.RuntimeIDs{RunID: "new-run", ThreadID: "thread-1"}),
		WithCheckpointLoader(store),
		WithResumeRequest(gopact.ResumeRequest{
			CheckpointID: "checkpoint-1",
			InterruptID:  "interrupt-1",
			Payload: map[string]any{
				"answer": 42,
			},
		}),
	))
	if !errors.Is(err, gopact.ErrResumePayloadInvalid) {
		t.Fatalf("Run() error = %v, want ErrResumePayloadInvalid", err)
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestGraphRunDoesNotEmitResumeReceivedForCompletedCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-1",
			ThreadID: "thread-1",
			Step:     1,
			Node:     "one",
			Phase:    gopact.StepCompleted,
			State:    traceState{Trace: []string{"one"}},
			Queue:    []string{"two"},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("checkpointed node should not run after restore")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithThreadID("thread-1"),
		WithCheckpointLoader(store),
		WithResumeRequest(gopact.ResumeRequest{InterruptID: "interrupt-ignored"}),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventCheckpointLoaded,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
}

func TestGraphRunReturnsCheckpointLoadError(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{loadErr: errors.New("checkpoint store down")}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "one")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithThreadID("thread-1"), WithCheckpointLoader(store)))
	if !errors.Is(err, store.loadErr) {
		t.Fatalf("Run() error = %v, want %v", err, store.loadErr)
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestGraphCompileRejectsMissingNode(t *testing.T) {
	g := New[traceState]()
	g.AddEdge(Start, "missing")

	_, err := g.Compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want missing node error")
	}
}

func TestGraphRunEmitsStepEvents(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("plan", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "plan")
		return state, nil
	})
	g.AddEdge(Start, "plan")
	g.AddEdge("plan", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)

	completed := events[2]
	if completed.StepSnapshot == nil {
		t.Fatal("node_completed event missing step snapshot")
	}
	if completed.StepSnapshot.ID != "run-1:1" || completed.StepSnapshot.Node != "plan" || completed.StepSnapshot.Step != 1 || completed.StepSnapshot.Phase != gopact.StepCompleted {
		t.Fatalf("step snapshot = %+v", completed.StepSnapshot)
	}
	output, ok := completed.StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("step output type = %T, want traceState", completed.StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"plan"}) {
		t.Fatalf("step output trace = %v, want [plan]", output.Trace)
	}
}

func TestGraphRunEmitsFailureEvents(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("node failed")
	g := New[traceState]()
	g.AddNode("fail", func(ctx context.Context, state traceState) (traceState, error) {
		return state, wantErr
	})
	g.AddEdge(Start, "fail")
	g.AddEdge("fail", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeFailed,
		gopact.EventRunFailed,
	)
	if events[2].StepSnapshot == nil || events[2].StepSnapshot.Phase != gopact.StepFailed {
		t.Fatalf("failed step snapshot = %+v", events[2].StepSnapshot)
	}
}

func TestGraphRunAppliesNodeMiddlewareAroundNode(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("work", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "node")
		return state, nil
	})
	g.AddEdge(Start, "work")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	middleware := func(c *gopact.NodeContext) error {
		state, ok := c.Input.(traceState)
		if !ok {
			t.Fatalf("middleware input type = %T, want traceState", c.Input)
		}
		state.Trace = append(state.Trace, "before")
		c.Input = state

		if err := c.Next(); err != nil {
			return err
		}

		output, ok := c.Output.(traceState)
		if !ok {
			t.Fatalf("middleware output type = %T, want traceState", c.Output)
		}
		output.Trace = append(output.Trace, "after")
		c.Output = output
		return nil
	}

	got, err := run.Invoke(ctx, traceState{}, WithNodeMiddleware(middleware))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"before", "node", "after"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphRunCopiesNodeMiddlewareEffectsToStepSnapshot(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("work", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "work")
		return state, nil
	})
	g.AddEdge(Start, "work")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithNodeMiddleware(func(c *gopact.NodeContext) error {
		if err := c.Next(); err != nil {
			return err
		}
		c.AddEffect(gopact.EffectRecord{
			ID:      "call-1",
			Type:    "tool_call",
			Target:  "local.echo",
			Applied: true,
		})
		return nil
	})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	completed := events[2]
	if completed.StepSnapshot == nil {
		t.Fatal("completed event missing step snapshot")
	}
	if len(completed.StepSnapshot.Effects) != 1 {
		t.Fatalf("effect count = %d, want 1", len(completed.StepSnapshot.Effects))
	}
	effect := completed.StepSnapshot.Effects[0]
	if effect.Type != "tool_call" || effect.Target != "local.echo" || !effect.Applied {
		t.Fatalf("effect = %+v, want applied tool_call", effect)
	}
}

func TestGraphRunNodeMiddlewareCanShortCircuit(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("work", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("node should not run when middleware short-circuits")
		return state, nil
	})
	g.AddEdge(Start, "work")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	shortCircuit := func(c *gopact.NodeContext) error {
		c.Output = traceState{Trace: []string{"short-circuit"}}
		return nil
	}

	got, err := run.Invoke(ctx, traceState{}, WithNodeMiddleware(shortCircuit))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"short-circuit"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphRunNodeMiddlewareErrorProducesNodeFailedEvent(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("middleware failed")
	g := New[traceState]()
	g.AddNode("work", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("node should not run after middleware error")
		return state, nil
	})
	g.AddEdge(Start, "work")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithNodeMiddleware(func(_ *gopact.NodeContext) error {
		return wantErr
	})))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeFailed,
		gopact.EventRunFailed,
	)
}

func TestGraphRunNodeMiddlewareRejectsWrongOutputType(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("work", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "work")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = run.Invoke(ctx, traceState{}, WithNodeMiddleware(func(c *gopact.NodeContext) error {
		c.Output = "wrong"
		return nil
	}))
	if err == nil {
		t.Fatal("Invoke() error = nil, want output type mismatch")
	}
	if !strings.Contains(err.Error(), "output type mismatch") {
		t.Fatalf("Invoke() error = %v, want output type mismatch", err)
	}
}

func TestGraphRunResumesFromCompletedStepExport(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("completed node should not run after resume")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "one",
			Phase:  gopact.StepCompleted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"one"}},
			Effects: []gopact.EffectRecord{
				{
					ID:             "tool-1",
					Type:           "tool_call",
					Applied:        true,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "call-1",
				},
			},
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithStepExport(export)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[1].StepSnapshot == nil || events[1].StepSnapshot.ID != "run-1:1" {
		t.Fatalf("step imported event = %+v, want source snapshot", events[1])
	}
	requireReplayPlan(t, events[1], []gopact.EffectReplayAction{gopact.EffectReplayActionReplay})
	if events[2].Node != "two" || events[2].Step != 2 {
		t.Fatalf("resumed node event = %+v, want node two step 2", events[2])
	}
	output, ok := events[3].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("output type = %T, want traceState", events[3].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"one", "two"}) {
		t.Fatalf("resumed trace = %v, want [one two]", output.Trace)
	}
}

func TestGraphRunVerifiesStepExportArtifactsBeforeResume(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("completed node should not run after resume")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "one",
			Phase:  gopact.StepCompleted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"one"}},
			Queue:  []string{"two"},
			Artifacts: []gopact.ArtifactRef{
				{ID: "step-artifact", SHA256: "sha-1"},
			},
			Effects: []gopact.EffectRecord{
				{
					ID:      "artifact-effect",
					Type:    "artifact_write",
					Applied: true,
					Artifacts: []gopact.ArtifactRef{
						{ID: "effect-artifact", SHA256: "sha-2"},
					},
				},
			},
		},
	}

	var gotRefs []gopact.ArtifactRef
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithStepExport(export),
		WithArtifactVerifier(artifactVerifierFunc(func(ctx context.Context, refs []gopact.ArtifactRef) error {
			gotRefs = append(gotRefs, refs...)
			return nil
		})),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	expectedIDs := []string{"step-artifact", "effect-artifact"}
	if !reflect.DeepEqual(gotArtifactIDs(gotRefs), expectedIDs) {
		t.Fatalf("verified artifact refs = %+v, want ids %v", gotRefs, expectedIDs)
	}
}

func TestGraphRunRejectsStepExportWhenArtifactIntegrityFails(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("integrity mismatch")
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("completed node should not run after resume")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("node should not run after failed artifact verification")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")
	g.AddEdge("two", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "one",
			Phase:  gopact.StepCompleted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"one"}},
			Queue:  []string{"two"},
			Artifacts: []gopact.ArtifactRef{
				{ID: "step-artifact", SHA256: "sha-1"},
			},
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithStepExport(export),
		WithArtifactVerifier(artifactVerifierFunc(func(ctx context.Context, refs []gopact.ArtifactRef) error {
			return wantErr
		})),
	))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventRunFailed,
	)
	if events[1].Node != "one" || events[1].Step != 1 {
		t.Fatalf("run failed event = %+v, want imported step boundary", events[1])
	}
}

func TestGraphRunEmitsInterruptedEvents(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "ask")
		return state, gopact.Interrupt(gopact.InterruptRecord{
			ID:     "interrupt-1",
			Type:   gopact.InterruptInput,
			Reason: "need user input",
		})
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Run() error = %v, want ErrInterrupted", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventInterrupted,
		gopact.EventRunInterrupted,
	)

	interrupted := events[2]
	if interrupted.StepSnapshot == nil {
		t.Fatal("interrupted event missing step snapshot")
	}
	if interrupted.StepSnapshot.Phase != gopact.StepInterrupted {
		t.Fatalf("step phase = %q, want interrupted", interrupted.StepSnapshot.Phase)
	}
	if interrupted.StepSnapshot.Pending == nil || interrupted.StepSnapshot.Pending.ID != "interrupt-1" {
		t.Fatalf("pending interrupt = %+v", interrupted.StepSnapshot.Pending)
	}
	output, ok := interrupted.StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("step output type = %T, want traceState", interrupted.StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"ask"}) {
		t.Fatalf("interrupted output trace = %v, want [ask]", output.Trace)
	}
}

func TestGraphRunPersistsInterruptedCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "ask")
		return state, gopact.Interrupt(gopact.InterruptRecord{
			ID:     "interrupt-1",
			Type:   gopact.InterruptInput,
			Reason: "need user input",
		})
	})
	g.AddNode("answer", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", "answer")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}), WithCheckpointer(store)))
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Run() error = %v, want ErrInterrupted", err)
	}
	if len(store.checkpoints) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(store.checkpoints))
	}
	checkpoint := store.checkpoints[0]
	if checkpoint.Phase != gopact.StepInterrupted {
		t.Fatalf("checkpoint phase = %q, want interrupted", checkpoint.Phase)
	}
	if checkpoint.Pending == nil || checkpoint.Pending.ID != "interrupt-1" {
		t.Fatalf("checkpoint pending = %+v, want interrupt-1", checkpoint.Pending)
	}
	if !reflect.DeepEqual(checkpoint.Queue, []string{"answer"}) {
		t.Fatalf("checkpoint queue = %v, want [answer]", checkpoint.Queue)
	}
	if !reflect.DeepEqual(checkpoint.State.Trace, []string{"ask"}) {
		t.Fatalf("checkpoint state trace = %v, want [ask]", checkpoint.State.Trace)
	}
}

func TestGraphRunResumesFromInterruptedStepExportWithResumeRequest(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("interrupted node should not rerun after resume")
		return state, nil
	})
	g.AddNode("answer", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "answer")
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", "answer")
	g.AddEdge("answer", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "ask",
			Phase:  gopact.StepInterrupted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"ask"}},
			Queue:  []string{"answer"},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-1",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
			},
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithStepExport(export), WithResumeRequest(gopact.ResumeRequest{
		InterruptID: "interrupt-1",
		Payload:     "continue",
	})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventResumeReceived,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[2].Metadata["interrupt_id"] != "interrupt-1" {
		t.Fatalf("resume event metadata = %+v, want interrupt id", events[2].Metadata)
	}
	if events[3].Node != "answer" || events[3].Step != 2 {
		t.Fatalf("resumed node event = %+v, want node answer step 2", events[3])
	}
	output, ok := events[4].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("output type = %T, want traceState", events[4].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"ask", "answer"}) {
		t.Fatalf("resumed trace = %v, want [ask answer]", output.Trace)
	}
}

func TestGraphRunRejectsInterruptedStepResumeWhenPayloadSchemaMismatches(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("interrupted node should not rerun after resume")
		return state, nil
	})
	g.AddNode("answer", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("answer node should not run after invalid resume payload")
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", "answer")
	g.AddEdge("answer", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "ask",
			Phase:  gopact.StepInterrupted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"ask"}},
			Queue:  []string{"answer"},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-1",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
				ResumeSchema: gopact.JSONSchema{
					"type":     "object",
					"required": []any{"answer"},
					"properties": map[string]any{
						"answer": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithStepExport(export),
		WithResumeRequest(gopact.ResumeRequest{
			InterruptID: "interrupt-1",
			Payload: map[string]any{
				"answer": 42,
			},
		}),
	))
	if !errors.Is(err, gopact.ErrResumePayloadInvalid) {
		t.Fatalf("Run() error = %v, want ErrResumePayloadInvalid", err)
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestGraphRunResumesInterruptedStepWithInjectedSchemaValidator(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("interrupted node should not rerun after resume")
		return state, nil
	})
	g.AddNode("answer", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "answer")
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", "answer")
	g.AddEdge("answer", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "ask",
			Phase:  gopact.StepInterrupted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"ask"}},
			Queue:  []string{"answer"},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-1",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
				ResumeSchema: gopact.JSONSchema{
					"$ref": "#/$defs/resume",
				},
			},
		},
	}
	called := false
	validator := gopact.JSONSchemaValidatorFunc(func(ctx context.Context, schema gopact.JSONSchema, value any) error {
		called = true
		if schema["$ref"] != "#/$defs/resume" {
			t.Fatalf("schema = %+v, want resume ref schema", schema)
		}
		return nil
	})

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{},
		WithStepExport(export),
		WithResumeRequest(gopact.ResumeRequest{
			InterruptID: "interrupt-1",
			Payload:     map[string]any{"answer": 42},
		}),
		WithJSONSchemaValidator(validator),
	))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !called {
		t.Fatal("validator called = false, want true")
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventResumeReceived,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
}

func TestGraphRunImportInterruptedStepWithoutResumeStaysInterrupted(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("ask", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("interrupted node should not run while waiting for resume")
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge("ask", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "ask",
			Phase:  gopact.StepInterrupted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Output: traceState{Trace: []string{"ask"}},
			Queue:  []string{End},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-1",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
			},
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithStepExport(export)))
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Run() error = %v, want ErrInterrupted", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunInterrupted,
	)
	if events[0].StepSnapshot == nil || events[0].StepSnapshot.Pending == nil {
		t.Fatalf("run interrupted event = %+v, want pending snapshot", events[0])
	}
}

func TestGraphRunRejectsResumeWithWrongStateType(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "two")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "two",
			Phase:  gopact.StepCompleted,
			Output: "wrong",
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithStepExport(export)))
	if err == nil {
		t.Fatal("Run() error = nil, want state type mismatch")
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestGraphRunEmitsCanceledEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	g := New[traceState]()
	g.AddNode("never", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("node should not run after context cancellation")
		return state, nil
	})
	g.AddEdge(Start, "never")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventRunCanceled,
	)
}

func TestGraphRunEmitsCanceledSnapshotWhenNodeReturnsContextCanceled(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("stop", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "stop")
		return state, context.Canceled
	})
	g.AddEdge(Start, "stop")
	g.AddEdge("stop", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1"})))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventRunCanceled,
	)
	if events[2].StepSnapshot == nil {
		t.Fatal("run_canceled event missing step snapshot")
	}
	if events[2].StepSnapshot.Phase != gopact.StepCanceled {
		t.Fatalf("step phase = %q, want canceled", events[2].StepSnapshot.Phase)
	}
	output, ok := events[2].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("step output type = %T, want traceState", events[2].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"stop"}) {
		t.Fatalf("canceled output trace = %v, want [stop]", output.Trace)
	}
}

func TestGraphRunPersistsCanceledCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()
	g.AddNode("stop", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "stop")
		return state, context.Canceled
	})
	g.AddNode("after", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "stop")
	g.AddEdge("stop", "after")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}), WithCheckpointer(store)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if len(store.checkpoints) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(store.checkpoints))
	}
	checkpoint := store.checkpoints[0]
	if checkpoint.Phase != gopact.StepCanceled {
		t.Fatalf("checkpoint phase = %q, want canceled", checkpoint.Phase)
	}
	if !reflect.DeepEqual(checkpoint.Queue, []string{"after"}) {
		t.Fatalf("checkpoint queue = %v, want [after]", checkpoint.Queue)
	}
	if !reflect.DeepEqual(checkpoint.State.Trace, []string{"stop"}) {
		t.Fatalf("checkpoint state trace = %v, want [stop]", checkpoint.State.Trace)
	}
}

func TestGraphRunResumesFromCanceledStepExport(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("stop", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("canceled node should not rerun after resume")
		return state, nil
	})
	g.AddNode("after", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "after")
		return state, nil
	})
	g.AddEdge(Start, "stop")
	g.AddEdge("stop", "after")
	g.AddEdge("after", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "stop",
			Phase:  gopact.StepCanceled,
			IDs:    gopact.RuntimeIDs{RunID: "run-1"},
			Output: traceState{Trace: []string{"stop"}},
			Queue:  []string{"after"},
		},
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithStepExport(export)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[2].Node != "after" || events[2].Step != 2 {
		t.Fatalf("resumed node event = %+v, want node after step 2", events[2])
	}
	output, ok := events[3].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("output type = %T, want traceState", events[3].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"stop", "after"}) {
		t.Fatalf("resumed trace = %v, want [stop after]", output.Trace)
	}
}

func TestGraphRunReturnsCheckpointError(t *testing.T) {
	ctx := context.Background()
	store := failingCheckpointer[traceState]{err: errors.New("store down")}
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
		return state, nil
	})
	g.AddEdge(Start, "one")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithCheckpointer(store)))
	if !errors.Is(err, store.err) {
		t.Fatalf("Run() error = %v, want %v", err, store.err)
	}

	if events[len(events)-1].Type != gopact.EventRunFailed {
		t.Fatalf("last event = %s, want run_failed", events[len(events)-1].Type)
	}
}

func TestGraphRunReportsNilRunnable(t *testing.T) {
	var run *Runnable[traceState]

	events, err := gopacttest.CollectEvents(run.Run(context.Background(), traceState{}))
	if err == nil {
		t.Fatal("Run() error = nil, want nil runnable error")
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestGraphRunRejectsMismatchedCheckpointerType(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "one")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithCheckpointer(&recordingCheckpointer[int]{})))
	if err == nil {
		t.Fatal("Run() error = nil, want checkpointer type mismatch")
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestGraphRunnableAdapterWorksWithRootRunner(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
		return state, nil
	})
	g.AddEdge(Start, "one")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	runner, err := gopact.NewRunner(run.AsRunnable())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(runner.Run(ctx, traceState{}, gopact.WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if events[0].IDs.RunID != "run-1" {
		t.Fatalf("first event run id = %q, want run-1", events[0].IDs.RunID)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
}

func TestGraphRunnableAdapterPassesRootResumeOptions(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		t.Fatal("completed node should not run after root resume")
		return state, nil
	})
	g.AddNode("two", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "two")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "two")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	runner, err := gopact.NewRunner(run.AsRunnable())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "one",
			Phase:  gopact.StepCompleted,
			IDs:    gopact.RuntimeIDs{RunID: "run-1"},
			Output: traceState{Trace: []string{"one"}},
		},
	}

	events, err := gopacttest.CollectEvents(runner.Run(ctx, traceState{}, gopact.WithStepExport(export)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventStepImported,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
}

func TestGraphRunnableAdapterRejectsWrongInputType(t *testing.T) {
	g := New[traceState]()
	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "one")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	runner, err := gopact.NewRunner(run.AsRunnable())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(runner.Run(context.Background(), "wrong"))
	if err == nil {
		t.Fatal("Run() error = nil, want input type mismatch")
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func requireReplayPlan(t *testing.T, event gopact.Event, actions []gopact.EffectReplayAction) {
	t.Helper()
	plan, ok := gopact.EventEffectReplayPlan(event)
	if !ok {
		t.Fatalf("%s metadata = %+v, want effect replay plan", event.Type, event.Metadata)
	}
	if len(plan.Decisions) != len(actions) {
		t.Fatalf("%s replay decision count = %d, want %d", event.Type, len(plan.Decisions), len(actions))
	}
	for i, action := range actions {
		if plan.Decisions[i].Action != action {
			t.Fatalf("%s replay decision[%d] action = %q, want %q", event.Type, i, plan.Decisions[i].Action, action)
		}
	}
}

type artifactVerifierFunc func(context.Context, []gopact.ArtifactRef) error

func (f artifactVerifierFunc) VerifyRefs(ctx context.Context, refs []gopact.ArtifactRef) error {
	return f(ctx, refs)
}

func gotArtifactIDs(refs []gopact.ArtifactRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	return ids
}

type recordingCheckpointer[S any] struct {
	checkpoints []Checkpoint[S]
}

func (r *recordingCheckpointer[S]) Put(ctx context.Context, checkpoint Checkpoint[S]) error {
	r.checkpoints = append(r.checkpoints, checkpoint)
	return nil
}

type recordingCheckpointStore[S any] struct {
	recordingCheckpointer[S]
	latest    Checkpoint[S]
	hasLatest bool
	loadErr   error
}

func (r *recordingCheckpointStore[S]) Latest(ctx context.Context, threadID string) (Checkpoint[S], bool, error) {
	if r.loadErr != nil {
		var zero Checkpoint[S]
		return zero, false, r.loadErr
	}
	if !r.hasLatest || r.latest.ThreadID != threadID {
		var zero Checkpoint[S]
		return zero, false, nil
	}
	return r.latest, true, nil
}

type failingCheckpointer[S any] struct {
	err error
}

func (f failingCheckpointer[S]) Put(ctx context.Context, checkpoint Checkpoint[S]) error {
	return f.err
}
