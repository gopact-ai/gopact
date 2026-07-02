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

func ExampleNewHTTPRegistryHandler() {
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
	agentServer := httptest.NewServer(a2a.NewHTTPHandler(local))
	defer agentServer.Close()
	reviewerEndpoint, err := a2a.NewHTTPAgent(agentServer.URL, a2a.WithHTTPClient(agentServer.Client()))
	if err != nil {
		panic(err)
	}
	registryServer := httptest.NewServer(a2a.NewHTTPRegistryHandler(a2a.NewCompositeDiscoverer(
		a2a.NewStaticDiscoverer(a2a.AgentCard{
			Name:         "planner",
			Capabilities: []string{"planning"},
		}),
		reviewerEndpoint,
	)))
	defer registryServer.Close()

	registry, err := a2a.NewHTTPRegistry(registryServer.URL, a2a.WithHTTPClient(registryServer.Client()))
	if err != nil {
		panic(err)
	}
	cards, err := registry.ListCards(ctx)
	if err != nil {
		panic(err)
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		panic(err)
	}
	if _, err := mesh.Bootstrap(ctx, registry); err != nil {
		panic(err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"code.review"},
		Task:    a2a.Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(len(cards))
	fmt.Println(result.Output)
	// Output:
	// 2
	// reviewed: diff
}
