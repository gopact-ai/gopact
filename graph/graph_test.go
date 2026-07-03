package graph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

type traceState struct {
	Trace []string
}

func traceNode(label string) NodeFunc[traceState] {
	return func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, label)
		return state, nil
	}
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

func TestGraphRunInheritsRuntimeIDsFromContext(t *testing.T) {
	want := gopact.RuntimeIDs{
		RunID:    "ctx-run",
		ThreadID: "thread-1",
		TraceID:  "trace-1",
	}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), want)
	var got gopact.RuntimeIDs
	var ok bool
	g := New[traceState]()
	g.AddNode("step", func(ctx context.Context, state traceState) (traceState, error) {
		got, ok = gopact.RuntimeIDsFromContext(ctx)
		state.Trace = append(state.Trace, "step")
		return state, nil
	})
	g.AddEdge(Start, "step")
	g.AddEdge("step", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !ok {
		t.Fatal("RuntimeIDsFromContext() ok = false, want true")
	}
	if got != want {
		t.Fatalf("RuntimeIDsFromContext() = %+v, want %+v", got, want)
	}
	if len(events) == 0 || events[0].IDs != want {
		t.Fatalf("first event IDs = %+v, want %+v", events[0].IDs, want)
	}
}

func TestGraphRunnableNodeExecutesSubgraph(t *testing.T) {
	ctx := context.Background()
	subgraph := New[traceState]()
	subgraph.AddNode("sub-one", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "sub-one")
		return state, nil
	})
	subgraph.AddNode("sub-two", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "sub-two")
		return state, nil
	})
	subgraph.AddEdge(Start, "sub-one")
	subgraph.AddEdge("sub-one", "sub-two")
	subgraph.AddEdge("sub-two", End)
	subrun, err := subgraph.Compile()
	if err != nil {
		t.Fatalf("Compile(subgraph) error = %v", err)
	}

	g := New[traceState]()
	g.AddNode("before", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "before")
		return state, nil
	})
	g.AddRunnableNode("subgraph", subrun)
	g.AddNode("after", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "after")
		return state, nil
	})
	g.AddEdge(Start, "before")
	g.AddEdge("before", "subgraph")
	g.AddEdge("subgraph", "after")
	g.AddEdge("after", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"before", "sub-one", "sub-two", "after"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphRunnableNodeStreamsNestedEvents(t *testing.T) {
	ctx := context.Background()
	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	subgraph := New[traceState]()
	subgraph.AddNode("sub-one", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "sub-one")
		return state, nil
	})
	subgraph.AddNode("sub-two", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "sub-two")
		return state, nil
	})
	subgraph.AddEdge(Start, "sub-one")
	subgraph.AddEdge("sub-one", "sub-two")
	subgraph.AddEdge("sub-two", End)
	subrun, err := subgraph.Compile()
	if err != nil {
		t.Fatalf("Compile(subgraph) error = %v", err)
	}

	g := New[traceState]()
	g.AddNode("before", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "before")
		return state, nil
	})
	g.AddRunnableNode("subgraph", subrun)
	g.AddNode("after", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "after")
		return state, nil
	})
	g.AddEdge(Start, "before")
	g.AddEdge("before", "subgraph")
	g.AddEdge("subgraph", "after")
	g.AddEdge("after", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(ids)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[3].Node != "subgraph" {
		t.Fatalf("parent subgraph start node = %q, want subgraph", events[3].Node)
	}
	for _, index := range []int{4, 5, 6, 7, 8, 9} {
		event := events[index]
		if event.IDs != ids {
			t.Fatalf("nested event ids = %+v, want %+v", event.IDs, ids)
		}
		if event.Metadata["graph_parent_node"] != "subgraph" || event.Metadata["graph_parent_step"] != 2 {
			t.Fatalf("nested event metadata = %+v, want parent subgraph step 2", event.Metadata)
		}
	}
	output, ok := events[12].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("after output type = %T, want traceState", events[12].StepSnapshot.Output)
	}
	expected := []string{"before", "sub-one", "sub-two", "after"}
	if !reflect.DeepEqual(output.Trace, expected) {
		t.Fatalf("final trace = %v, want %v", output.Trace, expected)
	}
}

func TestGraphNodeCanEmitNestedEvents(t *testing.T) {
	ctx := context.Background()
	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	g := New[traceState]()
	g.AddNode("delegate", func(ctx context.Context, state traceState) (traceState, error) {
		if !EmitNodeEvent(ctx, gopact.Event{
			Type: gopact.EventA2ATaskCompleted,
			IDs:  ids,
			Metadata: map[string]any{
				"agent_name": "planner",
			},
		}, nil) {
			return state, ErrNodeEventYieldStopped
		}
		state.Trace = append(state.Trace, "delegate")
		return state, nil
	})
	g.AddEdge(Start, "delegate")
	g.AddEdge("delegate", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithRuntimeIDs(ids)))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventA2ATaskCompleted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if events[2].Metadata[EventMetadataParentNode] != "delegate" {
		t.Fatalf("nested event parent node = %v, want delegate", events[2].Metadata[EventMetadataParentNode])
	}
	if events[2].Metadata[EventMetadataParentStep] != 1 {
		t.Fatalf("nested event parent step = %v, want 1", events[2].Metadata[EventMetadataParentStep])
	}
	if events[2].Metadata["agent_name"] != "planner" {
		t.Fatalf("nested event metadata = %+v, want agent_name", events[2].Metadata)
	}
}

func TestGraphRunnableNodeInheritsRuntimeIDs(t *testing.T) {
	ctx := context.Background()
	want := gopact.RuntimeIDs{
		RunID:    "run-1",
		ThreadID: "thread-1",
		TraceID:  "trace-1",
	}
	var got gopact.RuntimeIDs
	var ok bool

	subgraph := New[traceState]()
	subgraph.AddNode("child", func(ctx context.Context, state traceState) (traceState, error) {
		got, ok = gopact.RuntimeIDsFromContext(ctx)
		state.Trace = append(state.Trace, "child")
		return state, nil
	})
	subgraph.AddEdge(Start, "child")
	subgraph.AddEdge("child", End)
	subrun, err := subgraph.Compile()
	if err != nil {
		t.Fatalf("Compile(subgraph) error = %v", err)
	}

	g := New[traceState]()
	g.AddRunnableNode("subgraph", subrun)
	g.AddEdge(Start, "subgraph")
	g.AddEdge("subgraph", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = run.Invoke(ctx, traceState{}, WithRuntimeIDs(want))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !ok {
		t.Fatal("RuntimeIDsFromContext() ok = false, want true")
	}
	if got != want {
		t.Fatalf("RuntimeIDsFromContext() = %+v, want %+v", got, want)
	}
}

func TestGraphTopologyExportIncludesNodesEdgesBranchesAndJoins(t *testing.T) {
	subgraph := New[traceState]()
	subgraph.AddNode("child", traceNode("child"))
	subgraph.AddEdge(Start, "child")
	subgraph.AddEdge("child", End)
	subrun, err := subgraph.Compile()
	if err != nil {
		t.Fatalf("Compile(subgraph) error = %v", err)
	}

	g := New[traceState]()
	g.AddNode("plan", traceNode("plan"))
	g.AddRunnableNode("review", subrun)
	g.AddNode("work", traceNode("work"))
	g.AddNode("join", traceNode("join"))
	g.AddEdge(Start, "plan")
	g.AddBranch("plan", func(context.Context, traceState) ([]string, error) {
		return []string{"review", "work"}, nil
	})
	g.AddEdge("review", "join")
	g.AddEdge("work", "join")
	g.AddEdge("join", End)

	topology := g.Topology()

	expectedNodes := []TopologyNode{
		{Name: Start, Kind: TopologyNodeBoundary},
		{Name: "join", Kind: TopologyNodeFunction},
		{Name: "plan", Kind: TopologyNodeFunction},
		{Name: "review", Kind: TopologyNodeRunnable},
		{Name: "work", Kind: TopologyNodeFunction},
		{Name: End, Kind: TopologyNodeBoundary},
	}
	if !reflect.DeepEqual(topology.Nodes, expectedNodes) {
		t.Fatalf("topology nodes = %#v, want %#v", topology.Nodes, expectedNodes)
	}

	expectedEdges := []TopologyEdge{
		{From: Start, To: "plan", Index: 0},
		{From: "join", To: End, Index: 0},
		{From: "review", To: "join", Index: 0},
		{From: "work", To: "join", Index: 0},
	}
	if !reflect.DeepEqual(topology.Edges, expectedEdges) {
		t.Fatalf("topology edges = %#v, want %#v", topology.Edges, expectedEdges)
	}

	expectedBranches := []TopologyBranch{{From: "plan", Count: 1}}
	if !reflect.DeepEqual(topology.Branches, expectedBranches) {
		t.Fatalf("topology branches = %#v, want %#v", topology.Branches, expectedBranches)
	}

	expectedJoins := []TopologyJoin{{Node: "join", Predecessors: []string{"review", "work"}}}
	if !reflect.DeepEqual(topology.Joins, expectedJoins) {
		t.Fatalf("topology joins = %#v, want %#v", topology.Joins, expectedJoins)
	}
}

func TestRunnableTopologyExportIsStableAndDefensive(t *testing.T) {
	subgraph := New[traceState]()
	subgraph.AddNode("child", traceNode("child"))
	subgraph.AddEdge(Start, "child")
	subgraph.AddEdge("child", End)
	subrun, err := subgraph.Compile()
	if err != nil {
		t.Fatalf("Compile(subgraph) error = %v", err)
	}

	g := New[traceState]()
	g.AddNode("left", traceNode("left"))
	g.AddRunnableNode("right", subrun)
	g.AddNode("join", traceNode("join"))
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)
	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	topology := run.Topology()
	if topology.MaxSteps != 1024 {
		t.Fatalf("topology max steps = %d, want 1024", topology.MaxSteps)
	}
	if !reflect.DeepEqual(topology.Joins, []TopologyJoin{{Node: "join", Predecessors: []string{"left", "right"}}}) {
		t.Fatalf("topology joins = %#v", topology.Joins)
	}
	var rightKind TopologyNodeKind
	for _, node := range topology.Nodes {
		if node.Name == "right" {
			rightKind = node.Kind
		}
	}
	if rightKind != TopologyNodeRunnable {
		t.Fatalf("right node kind = %q, want %q", rightKind, TopologyNodeRunnable)
	}

	topology.Nodes[0].Name = "mutated"
	topology.Edges[0].From = "mutated"
	topology.Joins[0].Predecessors[0] = "mutated"

	again := run.Topology()
	if again.Nodes[0].Name == "mutated" ||
		again.Edges[0].From == "mutated" ||
		again.Joins[0].Predecessors[0] == "mutated" {
		t.Fatalf("Topology() returned mutable internal state: %#v", again)
	}
}

func TestGraphSchemaGuardRejectsInvalidNodeInput(t *testing.T) {
	ctx := context.Background()
	called := false
	g := New[traceState]()
	g.AddNode("guarded", func(_ context.Context, state traceState) (traceState, error) {
		called = true
		state.Trace = append(state.Trace, "guarded")
		return state, nil
	})
	g.SetNodeInputSchema("guarded", traceMinItemsSchema(1))
	g.AddEdge(Start, "guarded")
	g.AddEdge("guarded", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, ErrSchemaGuardFailed) {
		t.Fatalf("Run() error = %v, want ErrSchemaGuardFailed", err)
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		t.Fatalf("Run() error = %v, want ErrJSONSchemaValidationFailed", err)
	}
	if called {
		t.Fatal("guarded node was called after invalid input")
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeFailed,
		gopact.EventRunFailed,
	)
	if events[2].StepSnapshot == nil || events[2].StepSnapshot.Phase != gopact.StepFailed {
		t.Fatalf("failed event snapshot = %+v, want failed step snapshot", events[2].StepSnapshot)
	}
}

func TestGraphSchemaGuardRejectsInvalidNodeOutput(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("guarded", traceNode("guarded"))
	g.SetNodeOutputSchema("guarded", traceMinItemsSchema(2))
	g.AddEdge(Start, "guarded")
	g.AddEdge("guarded", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, ErrSchemaGuardFailed) {
		t.Fatalf("Run() error = %v, want ErrSchemaGuardFailed", err)
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		t.Fatalf("Run() error = %v, want ErrJSONSchemaValidationFailed", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeFailed,
		gopact.EventRunFailed,
	)
	output, ok := events[2].StepSnapshot.Output.(traceState)
	if !ok {
		t.Fatalf("failed output type = %T, want traceState", events[2].StepSnapshot.Output)
	}
	if !reflect.DeepEqual(output.Trace, []string{"guarded"}) {
		t.Fatalf("failed output trace = %v, want [guarded]", output.Trace)
	}
}

func TestGraphStateSchemaGuardRejectsInvalidStepExportResume(t *testing.T) {
	ctx := context.Background()
	called := false
	g := New[traceState]()
	g.SetStateSchema(traceMinItemsSchema(2))
	g.AddNode("one", traceNode("one"))
	g.AddNode("next", func(_ context.Context, state traceState) (traceState, error) {
		called = true
		state.Trace = append(state.Trace, "next")
		return state, nil
	})
	g.AddEdge(Start, "one")
	g.AddEdge("one", "next")
	g.AddEdge("next", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithStepExport(gopact.StepExport{
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
	})))
	if !errors.Is(err, ErrSchemaGuardFailed) {
		t.Fatalf("Run() error = %v, want ErrSchemaGuardFailed", err)
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		t.Fatalf("Run() error = %v, want ErrJSONSchemaValidationFailed", err)
	}
	if called {
		t.Fatal("next node was called after invalid resumed state")
	}
	gopacttest.RequireEventTypes(t, events, gopact.EventRunFailed)
	if events[0].Node != "one" || events[0].Step != 1 {
		t.Fatalf("run failed event = %+v, want resumed node one step 1", events[0])
	}
}

func TestGraphSchemaGuardsUseInjectedValidatorAndExportTopology(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaValidatorTestKey{}, "ctx-1")
	var calls int
	validator := gopact.JSONSchemaValidatorFunc(func(ctx context.Context, schema gopact.JSONSchema, value any) error {
		calls++
		if got := ctx.Value(schemaValidatorTestKey{}); got != "ctx-1" {
			return errors.New("validator context marker missing")
		}
		marker, _ := schema["x-test"].(string)
		if marker == "" {
			return nil
		}
		if marker != "node-output" {
			return fmt.Errorf("schema marker = %v, want node-output", marker)
		}
		return gopact.ErrJSONSchemaValidationFailed
	})
	outputSchema := traceMinItemsSchema(1)
	outputSchema["x-test"] = "node-output"
	outputSchema["required"] = []string{"Trace"}

	g := New[traceState]()
	g.SetStateSchema(traceMinItemsSchema(0))
	g.AddNode("guarded", traceNode("guarded"))
	g.SetNodeOutputSchema("guarded", outputSchema)
	g.AddEdge(Start, "guarded")
	g.AddEdge("guarded", End)

	topology := g.Topology()
	var exportedSchema gopact.JSONSchema
	for _, node := range topology.Nodes {
		if node.Name == "guarded" {
			exportedSchema = node.OutputSchema
		}
	}
	if exportedSchema["x-test"] != "node-output" {
		t.Fatalf("topology output schema = %#v, want node-output marker", exportedSchema)
	}
	exportedSchema["x-test"] = "mutated"
	exportedRequired, ok := exportedSchema["required"].([]string)
	if !ok {
		t.Fatalf("topology required type = %T, want []string", exportedSchema["required"])
	}
	exportedRequired[0] = "mutated"
	if again := g.Topology(); again.Nodes[1].OutputSchema["x-test"] != "node-output" {
		t.Fatalf("Topology() returned mutable schema state: %#v", again.Nodes[1].OutputSchema)
	} else if again.Nodes[1].OutputSchema["required"].([]string)[0] != "Trace" {
		t.Fatalf("Topology() returned mutable required schema slice: %#v", again.Nodes[1].OutputSchema["required"])
	}

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = run.Invoke(ctx, traceState{}, WithJSONSchemaValidator(validator))
	if !errors.Is(err, ErrSchemaGuardFailed) {
		t.Fatalf("Invoke() error = %v, want ErrSchemaGuardFailed", err)
	}
	if !errors.Is(err, gopact.ErrJSONSchemaValidationFailed) {
		t.Fatalf("Invoke() error = %v, want ErrJSONSchemaValidationFailed", err)
	}
	if calls != 3 {
		t.Fatalf("validator calls = %d, want 3", calls)
	}
}

type schemaValidatorTestKey struct{}

func TestGraphCompileRejectsSchemaGuardForMissingNode(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Graph[traceState])
	}{
		{
			name: "input schema",
			apply: func(g *Graph[traceState]) {
				g.SetNodeInputSchema("missing", traceMinItemsSchema(1))
			},
		},
		{
			name: "output schema",
			apply: func(g *Graph[traceState]) {
				g.SetNodeOutputSchema("missing", traceMinItemsSchema(1))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New[traceState]()
			g.AddNode("present", traceNode("present"))
			g.AddEdge(Start, "present")
			g.AddEdge("present", End)
			tt.apply(g)

			if _, err := g.Compile(); err == nil {
				t.Fatal("Compile() error = nil, want missing schema node error")
			}
		})
	}
}

func TestGraphRunnableNodeRuntimeIDsCanOverrideParentDefaults(t *testing.T) {
	ctx := context.Background()
	parent := gopact.RuntimeIDs{
		RunID:    "parent-run",
		ThreadID: "thread-1",
		TraceID:  "trace-1",
	}
	override := gopact.RuntimeIDs{
		RunID:   "child-run",
		AgentID: "child-agent",
	}
	want := gopact.RuntimeIDs{
		RunID:    "child-run",
		ThreadID: "thread-1",
		TraceID:  "trace-1",
		AgentID:  "child-agent",
	}
	var got gopact.RuntimeIDs
	var ok bool

	subgraph := New[traceState]()
	subgraph.AddNode("child", func(ctx context.Context, state traceState) (traceState, error) {
		got, ok = gopact.RuntimeIDsFromContext(ctx)
		state.Trace = append(state.Trace, "child")
		return state, nil
	})
	subgraph.AddEdge(Start, "child")
	subgraph.AddEdge("child", End)
	subrun, err := subgraph.Compile()
	if err != nil {
		t.Fatalf("Compile(subgraph) error = %v", err)
	}

	g := New[traceState]()
	g.AddRunnableNode("subgraph", subrun, WithRuntimeIDs(override))
	g.AddEdge(Start, "subgraph")
	g.AddEdge("subgraph", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = run.Invoke(ctx, traceState{}, WithRuntimeIDs(parent))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !ok {
		t.Fatal("RuntimeIDsFromContext() ok = false, want true")
	}
	if got != want {
		t.Fatalf("RuntimeIDsFromContext() = %+v, want %+v", got, want)
	}
}

func TestGraphCompileRejectsNilRunnableNode(t *testing.T) {
	g := New[traceState]()
	g.AddRunnableNode("subgraph", nil)
	g.AddEdge(Start, "subgraph")

	_, err := g.Compile()
	if err == nil || !strings.Contains(err.Error(), `runnable node "subgraph" is nil`) {
		t.Fatalf("Compile() error = %v, want nil runnable node error", err)
	}
}

func TestGraphAddNodeReplacesRunnableNode(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddRunnableNode("step", nil)
	g.AddNode("step", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "replacement")
		return state, nil
	})
	g.AddEdge(Start, "step")
	g.AddEdge("step", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"replacement"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphBranchRoutesToSelectedNode(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("decide", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "decide")
		return state, nil
	})
	g.AddNode("high", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "high")
		return state, nil
	})
	g.AddNode("low", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "low")
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddBranch("decide", func(_ context.Context, state traceState) ([]string, error) {
		if len(state.Trace) == 1 && state.Trace[0] == "decide" {
			return []string{"high"}, nil
		}
		return []string{"low"}, nil
	})
	g.AddEdge("high", End)
	g.AddEdge("low", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"decide", "high"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphBranchRoutesMultipleTargetsInOrder(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("split", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "split")
		return state, nil
	})
	g.AddNode("left", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "left")
		return state, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, nil
	})
	g.AddEdge(Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "right"}, nil
	})
	g.AddEdge("left", End)
	g.AddEdge("right", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"split", "left", "right"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphBranchDeduplicatesSuccessors(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()

	g.AddNode("split", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "split")
		return state, nil
	})
	g.AddNode("next", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "next")
		return state, nil
	})
	g.AddEdge(Start, "split")
	g.AddEdge("split", "next")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"next", "next"}, nil
	})
	g.AddEdge("next", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{}, WithCheckpointer(store))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"split", "next"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
	if len(store.checkpoints) == 0 {
		t.Fatal("checkpoint count = 0, want at least 1")
	}
	if !reflect.DeepEqual(store.checkpoints[0].Queue, []string{"next"}) {
		t.Fatalf("checkpoint queue = %v, want [next]", store.checkpoints[0].Queue)
	}
}

func TestGraphBranchCanEndWithNoTargets(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("decide", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "decide")
		return state, nil
	})
	g.AddNode("unused", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "unused")
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return nil, nil
	})

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"decide"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphDAGFanInRunsJoinOnceAfterAllParents(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("left", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "left")
		return state, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, nil
	})
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"left", "right", "join"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphDAGFanInStopsWhenParentFails(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("right failed")
	joinCalled := false
	g := New[traceState]()

	g.AddNode("left", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "left")
		return state, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, wantErr
	})
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		joinCalled = true
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if joinCalled {
		t.Fatal("join ran after a parent failed")
	}
	if len(events) == 0 {
		t.Fatal("events is empty, want run_failed")
	}
	if events[len(events)-1].Type != gopact.EventRunFailed {
		t.Fatalf("last event = %+v, want run_failed", events[len(events)-1])
	}
}

func TestGraphDAGFanInCheckpointResumeKeepsJoinPending(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("stop before right")
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()

	g.AddNode("left", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "left")
		return state, nil
	})
	g.AddNode("right", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("join", func(context.Context, traceState) (traceState, error) {
		t.Fatal("join should wait for right")
		return traceState{}, nil
	})
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = run.Invoke(ctx, traceState{}, WithCheckpointer(store))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, wantErr)
	}
	if len(store.checkpoints) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(store.checkpoints))
	}
	if !reflect.DeepEqual(store.checkpoints[0].Queue, []string{"right"}) {
		t.Fatalf("checkpoint queue = %v, want [right]", store.checkpoints[0].Queue)
	}

	resumeStore := &recordingCheckpointStore[traceState]{
		latest:    store.checkpoints[0],
		hasLatest: true,
	}
	resumeStore.latest.ThreadID = "thread-fan-in"
	g = New[traceState]()
	g.AddNode("left", func(context.Context, traceState) (traceState, error) {
		t.Fatal("checkpointed left should not rerun")
		return traceState{}, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, nil
	})
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	resumed, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile(resume) error = %v", err)
	}
	got, err := resumed.Invoke(ctx, traceState{}, WithThreadID("thread-fan-in"), WithCheckpointLoader(resumeStore))
	if err != nil {
		t.Fatalf("Invoke(resume) error = %v", err)
	}
	expected := []string{"left", "right", "join"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("resumed trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphDynamicFanOutCheckpointResumeRunsOnlyIncompleteTargets(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("stop at right")
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()

	g.AddNode("split", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "split")
		return state, nil
	})
	g.AddNode("left", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "left")
		return state, nil
	})
	g.AddNode("right", func(context.Context, traceState) (traceState, error) {
		return traceState{}, wantErr
	})
	g.AddNode("join", func(context.Context, traceState) (traceState, error) {
		t.Fatal("join should wait for right")
		return traceState{}, nil
	})
	g.AddEdge(Start, "split")
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		return []string{"left", "right"}, nil
	})
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = run.Invoke(ctx, traceState{}, WithCheckpointer(store))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, wantErr)
	}
	if len(store.checkpoints) != 2 {
		t.Fatalf("checkpoint count = %d, want split and left checkpoints", len(store.checkpoints))
	}
	if !reflect.DeepEqual(store.checkpoints[1].Queue, []string{"right"}) {
		t.Fatalf("left checkpoint queue = %v, want [right]", store.checkpoints[1].Queue)
	}

	resumeStore := &recordingCheckpointStore[traceState]{
		latest:    store.checkpoints[1],
		hasLatest: true,
	}
	resumeStore.latest.ThreadID = "thread-fan-out"
	g = New[traceState]()
	g.AddNode("split", func(context.Context, traceState) (traceState, error) {
		t.Fatal("checkpointed split should not rerun")
		return traceState{}, nil
	})
	g.AddBranch("split", func(context.Context, traceState) ([]string, error) {
		t.Fatal("checkpointed fan-out decision should not rerun")
		return nil, nil
	})
	g.AddNode("left", func(context.Context, traceState) (traceState, error) {
		t.Fatal("completed fan-out target should not rerun")
		return traceState{}, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, nil
	})
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(Start, "split")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	resumed, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile(resume) error = %v", err)
	}
	got, err := resumed.Invoke(ctx, traceState{}, WithThreadID("thread-fan-out"), WithCheckpointLoader(resumeStore))
	if err != nil {
		t.Fatalf("Invoke(resume) error = %v", err)
	}
	expected := []string{"split", "left", "right", "join"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("resumed trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphBranchPersistsSelectedQueueInCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointer[traceState]{}
	g := New[traceState]()

	g.AddNode("decide", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "decide")
		return state, nil
	})
	g.AddNode("next", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "next")
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return []string{"next"}, nil
	})
	g.AddEdge("next", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = run.Invoke(ctx, traceState{}, WithCheckpointer(store))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	if len(store.checkpoints) < 1 {
		t.Fatal("checkpoint count = 0, want at least 1")
	}
	if !reflect.DeepEqual(store.checkpoints[0].Queue, []string{"next"}) {
		t.Fatalf("branch checkpoint queue = %v, want [next]", store.checkpoints[0].Queue)
	}
}

func TestGraphBranchResumeUsesCheckpointQueue(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
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
	g := New[traceState]()
	g.AddNode("decide", func(context.Context, traceState) (traceState, error) {
		t.Fatal("checkpointed branch source should not rerun")
		return traceState{}, nil
	})
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		t.Fatal("checkpointed branch decision should not rerun")
		return nil, nil
	})
	g.AddNode("next", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "next")
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddEdge("next", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{}, WithThreadID("thread-branch"), WithCheckpointLoader(store))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"decide", "next"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("trace = %v, want %v", got.Trace, expected)
	}
}

func TestGraphBranchCompletedEventIncludesSelectedQueue(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("decide", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "decide")
		return state, nil
	})
	g.AddNode("next", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "next")
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return []string{"next"}, nil
	})
	g.AddEdge("next", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventNodeStarted,
		gopact.EventNodeCompleted,
		gopact.EventRunCompleted,
	)
	if !reflect.DeepEqual(events[2].StepSnapshot.Queue, []string{"next"}) {
		t.Fatalf("completed branch queue = %v, want [next]", events[2].StepSnapshot.Queue)
	}
}

func TestGraphBranchRejectsMissingTargetAtRuntime(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()

	g.AddNode("decide", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "decide")
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddBranch("decide", func(context.Context, traceState) ([]string, error) {
		return []string{"missing"}, nil
	})

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}))
	if err == nil || !strings.Contains(err.Error(), `missing target "missing"`) {
		t.Fatalf("Run() error = %v, want missing target error", err)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventRunStarted,
		gopact.EventNodeStarted,
		gopact.EventNodeFailed,
		gopact.EventRunFailed,
	)
	if events[2].StepSnapshot == nil || events[2].StepSnapshot.Phase != gopact.StepFailed {
		t.Fatalf("failed branch snapshot = %+v", events[2].StepSnapshot)
	}
}

func TestGraphCompileRejectsNilBranch(t *testing.T) {
	g := New[traceState]()
	g.AddNode("decide", func(_ context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "decide")
	g.AddBranch("decide", nil)

	_, err := g.Compile()
	if err == nil || !strings.Contains(err.Error(), `branch "decide"[0] is nil`) {
		t.Fatalf("Compile() error = %v, want nil branch error", err)
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
	if !reflect.DeepEqual(store.checkpoints[0].Metadata[metadataCompletedNodes], []string{"one"}) {
		t.Fatalf("first checkpoint completed nodes = %v, want [one]", store.checkpoints[0].Metadata[metadataCompletedNodes])
	}
	if !reflect.DeepEqual(store.checkpoints[1].Metadata[metadataCompletedNodes], []string{"one", "two"}) {
		t.Fatalf("second checkpoint completed nodes = %v, want [one two]", store.checkpoints[1].Metadata[metadataCompletedNodes])
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

func TestGraphRunInterruptedCheckpointResumeCompletesFanInSource(t *testing.T) {
	ctx := context.Background()
	store := &recordingCheckpointStore[traceState]{
		latest: Checkpoint[traceState]{
			ID:       "checkpoint-left",
			ThreadID: "thread-fan-in",
			IDs:      gopact.RuntimeIDs{RunID: "previous-run", ThreadID: "thread-fan-in"},
			Step:     1,
			Node:     "left",
			Phase:    gopact.StepInterrupted,
			State:    traceState{Trace: []string{"left"}},
			Queue:    []string{"right"},
			Pending: &gopact.InterruptRecord{
				ID:     "interrupt-left",
				Type:   gopact.InterruptInput,
				Reason: "need user input",
			},
		},
		hasLatest: true,
	}
	g := New[traceState]()
	g.AddNode("left", func(context.Context, traceState) (traceState, error) {
		t.Fatal("interrupted left should not rerun after checkpoint restore")
		return traceState{}, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, nil
	})
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{},
		WithThreadID("thread-fan-in"),
		WithCheckpointLoader(store),
		WithResumeRequest(gopact.ResumeRequest{
			CheckpointID: "checkpoint-left",
			InterruptID:  "interrupt-left",
			Payload:      "continue",
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"left", "right", "join"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("resumed trace = %v, want %v", got.Trace, expected)
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

func TestGraphRunAllowsPerInvokeMaxSteps(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("loop", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "loop")
		return state, nil
	})
	g.AddEdge(Start, "loop")
	g.AddEdge("loop", "loop")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := run.Invoke(ctx, traceState{}, WithMaxSteps(2))
	if err == nil || !strings.Contains(err.Error(), "exceeded max steps 2") {
		t.Fatalf("Invoke() error = %v, want max steps error", err)
	}
	if len(got.Trace) != 2 {
		t.Fatalf("trace length = %d, want 2", len(got.Trace))
	}
}

func TestGraphRunRejectsInvalidMaxSteps(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("one", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "one")
		return state, nil
	})
	g.AddEdge(Start, "one")

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(run.Run(ctx, traceState{}, WithMaxSteps(0)))
	if err == nil || !strings.Contains(err.Error(), "max steps must be positive") {
		t.Fatalf("Run() error = %v, want invalid max steps error", err)
	}
	if len(events) != 1 || events[0].Type != gopact.EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
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
	g.AddNode("sibling", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "ask")
	g.AddEdge(Start, "sibling")
	g.AddEdge("ask", "answer")
	g.AddEdge("sibling", End)

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
	if !reflect.DeepEqual(checkpoint.Queue, []string{"sibling", "answer"}) {
		t.Fatalf("checkpoint queue = %v, want [sibling answer]", checkpoint.Queue)
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
	g.AddNode("sibling", func(ctx context.Context, state traceState) (traceState, error) {
		return state, nil
	})
	g.AddEdge(Start, "stop")
	g.AddEdge(Start, "sibling")
	g.AddEdge("stop", "after")
	g.AddEdge("sibling", End)

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
	if !reflect.DeepEqual(checkpoint.Queue, []string{"sibling", "after"}) {
		t.Fatalf("checkpoint queue = %v, want [sibling after]", checkpoint.Queue)
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

func TestGraphRunCanceledStepExportResumeCompletesFanInSource(t *testing.T) {
	ctx := context.Background()
	g := New[traceState]()
	g.AddNode("left", func(context.Context, traceState) (traceState, error) {
		t.Fatal("canceled left should not rerun after step import")
		return traceState{}, nil
	})
	g.AddNode("right", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "right")
		return state, nil
	})
	g.AddNode("join", func(_ context.Context, state traceState) (traceState, error) {
		state.Trace = append(state.Trace, "join")
		return state, nil
	})
	g.AddEdge(Start, "left")
	g.AddEdge(Start, "right")
	g.AddEdge("left", "join")
	g.AddEdge("right", "join")
	g.AddEdge("join", End)

	run, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	export := gopact.StepExport{
		Version: 1,
		Step: gopact.StepSnapshot{
			ID:     "run-1:1",
			Step:   1,
			Node:   "left",
			Phase:  gopact.StepCanceled,
			IDs:    gopact.RuntimeIDs{RunID: "run-1"},
			Output: traceState{Trace: []string{"left"}},
			Queue:  []string{"right"},
		},
	}

	got, err := run.Invoke(ctx, traceState{}, WithStepExport(export))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	expected := []string{"left", "right", "join"}
	if !reflect.DeepEqual(got.Trace, expected) {
		t.Fatalf("resumed trace = %v, want %v", got.Trace, expected)
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
