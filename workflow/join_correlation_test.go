package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowJoinKeepsLoopEpochsInSeparateCorrelations(t *testing.T) {
	wf := New[int, int]("join-loop")
	plan := wf.Node("plan", func(_ context.Context, input int) (int, error) { return input, nil })
	left := wf.Node("left", func(_ context.Context, input int) (int, error) { return input, nil })
	right := wf.Node("right", func(_ context.Context, input int) (int, error) { return input * 10, nil })
	merge := wf.Merge("merge", func(_ context.Context, in Inputs) (int, error) {
		leftValue, err := in.One(left)
		if err != nil {
			return 0, err
		}
		rightValue, err := in.One(right)
		if err != nil {
			return 0, err
		}
		return leftValue + rightValue, nil
	})
	result := wf.Node("result", func(_ context.Context, input int) (int, error) { return input, nil })
	plan.Route(func(_ context.Context, input int) (Dispatch, error) {
		return plan.Once(left, input).And(plan.Once(right, input)), nil
	})
	merge.Route(func(_ context.Context, input int) (Dispatch, error) {
		if input < 20 {
			return merge.Once(plan, 2), nil
		}
		return merge.Once(result, input), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, left)
	wf.Edge(plan, right)
	wf.Edge(left, merge)
	wf.Edge(right, merge)
	wf.Edge(merge, plan)
	wf.Edge(merge, result)
	wf.Exit(result)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var correlations []CorrelationKey
	output, err := compiled.Invoke(context.Background(), 1, gopact.WithRunID("join-loop"), gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type != EventNodeCompleted || event.Summary != "merge" {
			return nil
		}
		var payload NodeEventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		correlations = append(correlations, payload.Correlation)
		return nil
	}))
	if err != nil || output != 22 {
		t.Fatalf("Invoke() = %d, %v, want 22", output, err)
	}
	if len(correlations) != 2 {
		t.Fatalf("merge correlations = %+v, want two epochs", correlations)
	}
	if correlations[0].ID == "" || correlations[0].ID != correlations[1].ID {
		t.Fatalf("merge correlation ids = %+v, want same non-empty id", correlations)
	}
	if correlations[0].Epoch != 1 || correlations[1].Epoch != 2 {
		t.Fatalf("merge correlation epochs = %+v, want 1 then 2", correlations)
	}
}

func TestWorkflowBackEdgeIntoMergeStartsNewCorrelationEpoch(t *testing.T) {
	wf := New[int, int]("merge-back-edge")
	start := wf.Node("start", func(_ context.Context, input int) (int, error) { return input, nil })
	step := wf.Node("step", func(_ context.Context, input int) (int, error) { return input + 1, nil })
	merge := wf.Merge("merge", func(_ context.Context, inputs Inputs) (int, error) {
		if value, ok, err := inputs.Lookup(start); err != nil {
			return 0, err
		} else if ok {
			return value, nil
		}
		return inputs.One(step)
	})
	finish := wf.Node("finish", func(_ context.Context, input int) (int, error) { return input, nil })
	merge.Route(func(_ context.Context, value int) (Dispatch, error) {
		if value < 2 {
			return merge.Once(step, value), nil
		}
		return merge.Once(finish, value), nil
	})
	wf.Entry(start)
	wf.Edge(start, merge)
	wf.Edge(merge, step)
	wf.Edge(step, merge)
	wf.Edge(merge, finish)
	wf.Exit(finish)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatal(err)
	}

	var nodes []string
	got, err := compiled.Invoke(context.Background(), 1, gopact.WithEventHandler(func(_ context.Context, event gopact.Event) error {
		if event.Type == EventNodeStarted {
			var payload NodeEventPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			nodes = append(nodes, fmt.Sprintf("%s@%d", event.NodeID, payload.Correlation.Epoch))
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("Invoke() error = %v, nodes = %v, back edges = %v", err, nodes, compiled.backEdges)
	}
	if got != 2 {
		t.Fatalf("Invoke() = %d, want second merge epoch output 2", got)
	}
}
