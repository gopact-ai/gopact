package gopact_test

import (
	"bytes"
	"context"
	"fmt"
	"iter"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

func ExampleSetup() {
	previous := gopact.Defaults()
	defer func() {
		_ = gopact.Setup(
			gopact.WithLogger(previous.Logger),
			gopact.WithLogLevel(previous.LogLevel),
			gopact.WithDefaultRuntimeIDs(previous.RuntimeIDs),
		)
	}()

	var logs bytes.Buffer
	if err := gopact.Setup(
		gopact.WithLogger(gopact.NewTextLogger(&logs)),
		gopact.WithLogLevel(gopact.LevelWarn),
		gopact.WithDefaultRuntimeIDs(gopact.RuntimeIDs{AgentID: "agent-1"}),
	); err != nil {
		panic(err)
	}

	defaults := gopact.Defaults()
	defaults.Log(context.Background(), gopact.LevelInfo, "ignored")
	defaults.Log(context.Background(), gopact.LevelWarn, "ready", gopact.String("agent_id", defaults.RuntimeIDs.AgentID))

	fmt.Print(logs.String())
	// Output:
	// level=WARN msg="ready" agent_id=agent-1
}

func ExampleNewRunner() {
	runner, err := gopact.NewRunner(
		exampleRunnable{},
		gopact.WithRunnerRuntimeIDs(gopact.RuntimeIDs{AgentID: "agent-1"}),
	)
	if err != nil {
		panic(err)
	}

	for event, err := range runner.Run(
		context.Background(),
		"hello",
		gopact.WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1"}),
	) {
		if err != nil {
			panic(err)
		}
		fmt.Println(event.Type, event.RunID, event.IDs.AgentID)
	}
	// Output:
	// run_started run-1 agent-1
	// run_completed run-1 agent-1
}

func ExampleNewRunRecorder() {
	recorder := gopact.NewRunRecorder()
	events := []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
	}

	for _, event := range events {
		if err := recorder.Record(event); err != nil {
			panic(err)
		}
	}
	export, err := recorder.Export()
	if err != nil {
		panic(err)
	}

	fmt.Println(export.Outcome)
	for event, err := range gopact.ReplayRunExport(export) {
		if err != nil {
			panic(err)
		}
		fmt.Println(event.Type)
	}
	// Output:
	// completed
	// run_started
	// run_completed
}

func ExampleNewVerificationRecorder() {
	recorder := gopact.NewVerificationRecorder()
	if err := recorder.Record(gopact.VerificationCheck{
		ID:     "output-contract",
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{{
			Type: "unit_test",
			Ref:  "memory://checks/output-contract",
		}},
	}); err != nil {
		panic(err)
	}

	report, err := recorder.Report(gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(report.Status, report.PassedCount, report.FailedCount)
	// Output:
	// passed 1 0
}

func ExampleValidateResumePayload() {
	record := gopact.InterruptRecord{
		ID:   "approval-1",
		Type: gopact.InterruptApproval,
		ResumeSchema: gopact.JSONSchema{
			"type":                 "object",
			"required":             []any{"approved"},
			"additionalProperties": false,
			"properties": map[string]any{
				"approved": map[string]any{"type": "boolean"},
			},
		},
	}
	request := gopact.ResumeRequest{
		InterruptID: "approval-1",
		Payload: map[string]any{
			"approved": true,
		},
	}

	fmt.Println(gopact.ValidateResumePayload(record, request) == nil)
	// Output:
	// true
}

func Example_graphRun() {
	ctx := context.Background()
	g := graph.New[exampleState]()

	g.AddNode("plan", func(_ context.Context, state exampleState) (exampleState, error) {
		state.Trace = append(state.Trace, "plan")
		return state, nil
	})
	g.AddEdge(graph.Start, "plan")
	g.AddEdge("plan", graph.End)

	run, err := g.Compile()
	if err != nil {
		panic(err)
	}

	store := checkpoint.NewMemory[exampleState]()
	var result exampleState
	var types []gopact.EventType
	for event, err := range run.Run(
		ctx,
		exampleState{},
		graph.WithRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}),
		graph.WithCheckpointer(store),
	) {
		if err != nil {
			panic(err)
		}
		types = append(types, event.Type)
		if event.Type == gopact.EventNodeCompleted {
			result = event.StepSnapshot.Output.(exampleState)
		}
	}

	fmt.Println(types)
	fmt.Println(result.Trace)
	// Output:
	// [run_started node_started node_completed run_completed]
	// [plan]
}

type exampleState struct {
	Trace []string
}

type exampleRunnable struct{}

func (exampleRunnable) Run(_ context.Context, _ any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
	cfg := gopact.ResolveRunOptions(opts...)
	return func(yield func(gopact.Event, error) bool) {
		if !yield(gopact.Event{Type: gopact.EventRunStarted, IDs: cfg.IDs}, nil) {
			return
		}
		yield(gopact.Event{Type: gopact.EventRunCompleted, IDs: cfg.IDs}, nil)
	}
}
