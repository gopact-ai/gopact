package workflow

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
)

func testNode[WI, WO, NI, NO any](wf *Workflow[WI, WO], name string, fn func(context.Context, NI) (NO, error)) *Node[NI, NO] {
	return wf.Node(name, fn)
}

func testInvokable[WI, WO, NI, NO any](wf *Workflow[WI, WO], name string, inv gopact.Invokable[NI, NO]) *Node[NI, NO] {
	return wf.AddInvokable(name, inv)
}

func testMerge[WI, WO, NO any](wf *Workflow[WI, WO], name string, fn func(context.Context, Inputs) (NO, error)) *Node[Inputs, NO] {
	return wf.Merge(name, fn)
}

func ExampleNew() {
	wf := New[string, string]("review")
	plan := wf.Node("plan", func(_ context.Context, input string) (int, error) {
		return len(input), nil
	})
	report := wf.Node("report", func(_ context.Context, input int) (string, error) {
		return fmt.Sprintf("len=%d", input), nil
	})
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	output, err := wf.Invoke(context.Background(), "abc")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(output)

	// Output:
	// len=3
}
