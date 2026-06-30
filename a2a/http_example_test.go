package a2a_test

import (
	"context"
	"fmt"
	"net/http/httptest"

	"github.com/gopact-ai/gopact/a2a"
)

func ExampleNewHTTPHandler() {
	ctx := context.Background()
	local := a2a.FakeAgent{
		CardValue: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}
	server := httptest.NewServer(a2a.NewHTTPHandler(local, a2a.WithHTTPHandlerAgentCard(a2a.AgentCard{
		Name:         "reviewer",
		Capabilities: []string{"code.review"},
		Protocols: []a2a.ProtocolBinding{
			{Name: "a2a", Transport: "http"},
		},
		Health: &a2a.HealthHints{HealthPath: "/healthz", ReadinessPath: "/readyz"},
	})))
	defer server.Close()

	remote, err := a2a.NewHTTPAgent(server.URL, a2a.WithHTTPClient(server.Client()))
	if err != nil {
		panic(err)
	}
	discovered, err := remote.Discover(ctx, a2a.DiscoveryQuery{Require: []string{"code.review"}})
	if err != nil {
		panic(err)
	}
	result, err := remote.Send(ctx, a2a.Task{ID: "task-1", Input: "diff"})
	if err != nil {
		panic(err)
	}

	fmt.Println(discovered.Card.Name)
	fmt.Println(result.Output)
	// Output:
	// reviewer
	// reviewed: diff
}
