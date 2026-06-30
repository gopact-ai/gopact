package a2a_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/a2a"
)

func ExampleNewMesh() {
	ctx := context.Background()
	var events []gopact.EventType
	mesh, err := a2a.NewMesh(
		a2a.WithMeshRuntimeIDs(gopact.RuntimeIDs{RunID: "run-1"}),
		a2a.WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event.Type)
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}

	_, err = mesh.Register(ctx, a2a.FakeAgent{
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
	})
	if err != nil {
		panic(err)
	}

	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"code.review"},
		Task:    a2a.Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Output)
	fmt.Println(events[0])
	fmt.Println(events[1])
	fmt.Println(events[2])
	// Output:
	// reviewed: diff
	// a2a_agent_registered
	// a2a_task_sent
	// a2a_task_completed
}

func ExampleMesh_Discover() {
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
	server := httptest.NewServer(a2a.NewHTTPHandler(local))
	defer server.Close()

	remote, err := a2a.NewHTTPAgent(server.URL, a2a.WithHTTPClient(server.Client()))
	if err != nil {
		panic(err)
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		panic(err)
	}
	if _, err := mesh.Discover(ctx, remote, a2a.DiscoveryQuery{URL: server.URL}); err != nil {
		panic(err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"code.review"},
		Task:    a2a.Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Output)
	// Output:
	// reviewed: diff
}

func ExampleMesh_Bootstrap() {
	ctx := context.Background()
	local := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "reviewer"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}
	server := httptest.NewServer(a2a.NewHTTPHandler(local))
	defer server.Close()

	mesh, err := a2a.NewMesh()
	if err != nil {
		panic(err)
	}
	_, err = mesh.Bootstrap(ctx, a2a.NewStaticDiscoverer(a2a.AgentCard{
		Name:         "reviewer",
		URL:          server.URL,
		Capabilities: []string{"code.review"},
	}))
	if err != nil {
		panic(err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"code.review"},
		Task:    a2a.Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Output)
	// Output:
	// reviewed: diff
}

func ExampleMesh_Bootstrap_withHTTPAgentOptions() {
	ctx := context.Background()
	local := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "reviewer"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "reviewed with mesh header"}, nil
		},
	}
	next := a2a.NewHTTPHandler(local)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a2a/task/send" && r.Header.Get("X-Mesh") != "yes" {
			http.Error(w, "missing mesh header", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}))
	defer server.Close()

	mesh, err := a2a.NewMesh(a2a.WithMeshHTTPAgentOptions(
		a2a.WithHTTPClient(server.Client()),
		a2a.WithHTTPHeader("X-Mesh", "yes"),
	))
	if err != nil {
		panic(err)
	}
	_, err = mesh.Bootstrap(ctx, a2a.NewStaticDiscoverer(a2a.AgentCard{
		Name:         "reviewer",
		URL:          server.URL,
		Capabilities: []string{"code.review"},
	}))
	if err != nil {
		panic(err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"code.review"},
		Task:    a2a.Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Output)
	// Output:
	// reviewed with mesh header
}

func ExampleMesh_Bootstrap_withJSONRPCAgentOptions() {
	ctx := context.Background()
	local := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "reviewer"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "jsonrpc reviewed with mesh header"}, nil
		},
	}
	next := a2a.NewJSONRPCHandler(local)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("X-Mesh") != "yes" {
			http.Error(w, "missing mesh header", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}))
	defer server.Close()

	mesh, err := a2a.NewMesh(a2a.WithMeshJSONRPCAgentOptions(
		a2a.WithJSONRPCClient(server.Client()),
		a2a.WithJSONRPCHeader("X-Mesh", "yes"),
	))
	if err != nil {
		panic(err)
	}
	_, err = mesh.Bootstrap(ctx, a2a.NewStaticDiscoverer(a2a.AgentCard{
		Name:         "reviewer",
		Capabilities: []string{"code.review"},
		Protocols: []a2a.ProtocolBinding{
			{Name: "a2a-jsonrpc", Transport: "jsonrpc", URL: server.URL},
		},
	}))
	if err != nil {
		panic(err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"code.review"},
		Task:    a2a.Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Output)
	// Output:
	// jsonrpc reviewed with mesh header
}

func ExampleMesh_Call_withOperationTimeout() {
	ctx := context.Background()
	mesh, err := a2a.NewMesh(a2a.WithMeshOperationTimeout(time.Millisecond))
	if err != nil {
		panic(err)
	}
	_, err = mesh.Register(ctx, a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "slow-agent"},
		SendFunc: func(ctx context.Context, _ a2a.Task) (a2a.Result, error) {
			<-ctx.Done()
			return a2a.Result{}, ctx.Err()
		},
	})
	if err != nil {
		panic(err)
	}

	_, err = mesh.Call(ctx, "slow-agent", a2a.Task{ID: "task-1", Input: "wait"})
	fmt.Println(errors.Is(err, context.DeadlineExceeded))
	// Output:
	// true
}
