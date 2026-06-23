package graph

import (
	"context"
	"reflect"
	"testing"
)

type traceState struct {
	Trace []string
}

func TestGraphRunExecutesNodesInEdgeOrder(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("plan", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "plan")
		return state, nil
	})
	g.AddNode("act", func(ctx context.Context, state traceState) (traceState, error) {
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

	g.AddNode("one", func(ctx context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
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
}

func TestGraphCompileRejectsMissingNode(t *testing.T) {
	g := New[traceState]()
	g.AddEdge(Start, "missing")

	_, err := g.Compile()
	if err == nil {
		t.Fatal("Compile() error = nil, want missing node error")
	}
}

type recordingCheckpointer[S any] struct {
	checkpoints []Checkpoint[S]
}

func (r *recordingCheckpointer[S]) Put(ctx context.Context, checkpoint Checkpoint[S]) error {
	r.checkpoints = append(r.checkpoints, checkpoint)
	return nil
}
