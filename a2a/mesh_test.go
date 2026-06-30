package a2a

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestMeshRegisterAndCallPublishesEvidence(t *testing.T) {
	ctx := context.Background()
	ids := gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"}
	events := []gopact.Event{}
	mesh, err := NewMesh(
		WithMeshRuntimeIDs(ids),
		WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	agent := FakeAgent{
		CardValue: AgentCard{Name: "reviewer", URL: "https://agents.example/reviewer"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}
	registration, err := mesh.Register(ctx, agent)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(registration.Events) != 1 || registration.Events[0].Type != gopact.EventA2AAgentRegistered {
		t.Fatalf("Register() events = %+v, want registration evidence", registration.Events)
	}

	result, err := mesh.Call(ctx, "reviewer", Task{ID: "task-1", Input: "diff"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Output != "reviewed: diff" {
		t.Fatalf("Call() result = %+v, want remote output", result)
	}
	gotTypes := eventTypes(events)
	wantTypes := []gopact.EventType{
		gopact.EventA2AAgentRegistered,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("published events = %v, want %v", gotTypes, wantTypes)
	}
	for _, event := range events {
		if event.IDs.RunID != ids.RunID || event.IDs.CallID != ids.CallID {
			t.Fatalf("published event ids = %+v, want defaults %+v", event.IDs, ids)
		}
	}
	if events[1].Metadata["agent_name"] != "reviewer" ||
		events[2].Metadata["agent_url"] != "https://agents.example/reviewer" {
		t.Fatalf("published metadata = %+v / %+v, want selected agent evidence", events[1].Metadata, events[2].Metadata)
	}
}

func TestMeshCallPropagatesContextRuntimeIDs(t *testing.T) {
	want := gopact.RuntimeIDs{
		RunID:   "task-run",
		AgentID: "mesh-agent",
		CallID:  "call-1",
		TraceID: "trace-1",
	}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{
		RunID:   "ctx-run",
		TraceID: "trace-1",
	})
	var gotTaskIDs, gotContextIDs gopact.RuntimeIDs
	var events []gopact.Event
	mesh, err := NewMesh(
		WithMeshRuntimeIDs(gopact.RuntimeIDs{RunID: "mesh-run", AgentID: "mesh-agent"}),
		WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Register(context.Background(), FakeAgent{
		CardValue: AgentCard{Name: "reviewer"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			gotTaskIDs = task.IDs
			gotContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return Result{TaskID: task.ID, Output: "reviewed"}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	events = nil

	result, err := mesh.Call(ctx, "reviewer", Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "task-run", CallID: "call-1"},
		Input: "diff",
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if result.Output != "reviewed" {
		t.Fatalf("Call() result = %+v, want reviewed", result)
	}
	if gotTaskIDs != want {
		t.Fatalf("agent task IDs = %+v, want %+v", gotTaskIDs, want)
	}
	if gotContextIDs != want {
		t.Fatalf("agent context IDs = %+v, want %+v", gotContextIDs, want)
	}
	if len(events) != 2 || events[0].IDs != want || events[1].IDs != want {
		t.Fatalf("published events = %+v, want two events with %+v", events, want)
	}
}

func TestMeshCallUsesOperationTimeout(t *testing.T) {
	ctx := context.Background()
	mesh, err := NewMesh(WithMeshOperationTimeout(5 * time.Millisecond))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Register(ctx, FakeAgent{
		CardValue: AgentCard{Name: "slow-agent"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := mesh.Call(ctx, "slow-agent", Task{ID: "task-1", Input: "wait"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want deadline exceeded", err)
	}
	if got := eventTypes(result.Events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskFailed,
	}) {
		t.Fatalf("Call() events = %v, want sent and failed timeout evidence", got)
	}
}

func TestMeshDiscoverRegistersCallablePolicyWrappedAgent(t *testing.T) {
	ctx := context.Background()
	policyCalls := 0
	mesh, err := NewMesh(
		WithMeshPolicy(gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			policyCalls++
			if req.Boundary != gopact.PolicyBoundaryA2A || req.Action != gopact.PolicyActionSend {
				t.Fatalf("policy request = %+v, want a2a send", req)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok || input.AgentName != "researcher" || input.Task == nil || input.Task.Input != "topic" {
				t.Fatalf("policy input = %+v, want discovered researcher task", req.Input)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
		})),
	)
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	agent := meshDiscoverableAgent{
		card: AgentCard{
			Name:         "researcher",
			Capabilities: []string{"research.web"},
			Metadata:     map[string]any{"domain": "research"},
		},
	}
	if _, err := mesh.Discover(ctx, agent, DiscoveryQuery{Name: "researcher"}); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	result, err := mesh.Call(ctx, "researcher", Task{ID: "task-1", Input: "topic"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Output != "researched: topic" {
		t.Fatalf("Call() result = %+v, want discovered agent output", result)
	}
	if policyCalls != 1 {
		t.Fatalf("policy calls = %d, want 1", policyCalls)
	}
}

func TestMeshBootstrapAndRoute(t *testing.T) {
	ctx := context.Background()
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	bootstrap, err := mesh.Bootstrap(ctx,
		NewStaticDiscoverer(AgentCard{Name: "planner", Capabilities: []string{"planning"}}),
		NewStaticDiscoverer(AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}}),
	)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Cards) != 2 || bootstrap.Cards[0].Name != "planner" || bootstrap.Cards[1].Name != "reviewer" {
		t.Fatalf("Bootstrap() cards = %+v, want source order", bootstrap.Cards)
	}
	if _, err := mesh.Register(ctx, FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewed"}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := mesh.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "reviewed" {
		t.Fatalf("Route() result = %+v, want reviewer output", result)
	}
	cards, err := mesh.Cards(ctx)
	if err != nil {
		t.Fatalf("Cards() error = %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "planner" || cards[1].Name != "reviewer" {
		t.Fatalf("Cards() = %+v, want bootstrapped order", cards)
	}
}

func TestMeshRouteStreamPropagatesContextRuntimeIDs(t *testing.T) {
	want := gopact.RuntimeIDs{
		RunID:   "ctx-run",
		AgentID: "mesh-agent",
		CallID:  "call-1",
		TraceID: "trace-1",
	}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{
		RunID:   "ctx-run",
		TraceID: "trace-1",
	})
	var gotTaskIDs, gotContextIDs gopact.RuntimeIDs
	mesh, err := NewMesh(WithMeshRuntimeIDs(gopact.RuntimeIDs{AgentID: "mesh-agent"}))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Register(context.Background(), FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			gotTaskIDs = task.IDs
			gotContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return func(yield func(TaskEvent, error) bool) {
				yield(TaskEvent{TaskID: task.ID, Status: TaskStatusCompleted, Result: &Result{TaskID: task.ID, Output: "reviewed"}}, nil)
			}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	events, err := collectTaskEvents(mesh.RouteStream(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", IDs: gopact.RuntimeIDs{CallID: "call-1"}, Input: "diff"},
	}))
	if err != nil {
		t.Fatalf("RouteStream() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("RouteStream() events = %+v, want one completed event", events)
	}
	if gotTaskIDs != want {
		t.Fatalf("agent task IDs = %+v, want %+v", gotTaskIDs, want)
	}
	if gotContextIDs != want {
		t.Fatalf("agent context IDs = %+v, want %+v", gotContextIDs, want)
	}
	if events[0].IDs != want {
		t.Fatalf("stream event IDs = %+v, want %+v", events[0].IDs, want)
	}
}

func TestMeshBootstrapRegistersHTTPCardForRouting(t *testing.T) {
	ctx := context.Background()
	events := []gopact.Event{}
	server := httptest.NewServer(NewHTTPHandler(FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			return Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}))
	defer server.Close()
	mesh, err := NewMesh(WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	bootstrap, err := mesh.Bootstrap(ctx, NewStaticDiscoverer(AgentCard{
		Name:         "reviewer",
		URL:          server.URL,
		Capabilities: []string{"code.review"},
	}))
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	result, err := mesh.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	if result.Output != "reviewed: diff" {
		t.Fatalf("Route() result = %+v, want HTTP bootstrap route", result)
	}
	if got := eventTypes(bootstrap.Events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
	}) {
		t.Fatalf("Bootstrap() event types = %v, want fetched and registered evidence", got)
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	}) {
		t.Fatalf("published events = %v, want bootstrap and route evidence", got)
	}
}

func TestMeshBootstrapAppliesHTTPAgentOptions(t *testing.T) {
	ctx := context.Background()
	handler := httpHeaderHandler(NewHTTPHandler(FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewed"}, nil
		},
	}), "X-Mesh-Test", "yes")
	server := httptest.NewServer(handler)
	defer server.Close()
	mesh, err := NewMesh(WithMeshHTTPAgentOptions(WithHTTPHeader("X-Mesh-Test", "yes")))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	if _, err := mesh.Bootstrap(ctx, NewStaticDiscoverer(AgentCard{
		Name:         "reviewer",
		URL:          server.URL,
		Capabilities: []string{"code.review"},
	})); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	result, err := mesh.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "reviewed" {
		t.Fatalf("Route() result = %+v, want reviewed", result)
	}
}

func TestMeshBootstrapRegistersJSONRPCCardForRouting(t *testing.T) {
	ctx := context.Background()
	events := []gopact.Event{}
	server := httptest.NewServer(NewJSONRPCHandler(FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			return Result{TaskID: task.ID, Output: "jsonrpc reviewed: " + task.Input}, nil
		},
	}))
	defer server.Close()
	mesh, err := NewMesh(WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	bootstrap, err := mesh.Bootstrap(ctx, NewStaticDiscoverer(AgentCard{
		Name:         "reviewer",
		Capabilities: []string{"code.review"},
		Protocols: []ProtocolBinding{
			{Name: "a2a-jsonrpc", Transport: "jsonrpc", URL: server.URL},
		},
	}))
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	result, err := mesh.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	if result.Output != "jsonrpc reviewed: diff" {
		t.Fatalf("Route() result = %+v, want JSON-RPC bootstrap route", result)
	}
	if got := eventTypes(bootstrap.Events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
	}) {
		t.Fatalf("Bootstrap() event types = %v, want fetched and registered evidence", got)
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	}) {
		t.Fatalf("published events = %v, want bootstrap and route evidence", got)
	}
}

func TestMeshBootstrapAppliesJSONRPCAgentOptions(t *testing.T) {
	ctx := context.Background()
	handler := httpHeaderHandler(NewJSONRPCHandler(FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewed"}, nil
		},
	}), "X-Mesh-Test", "yes")
	server := httptest.NewServer(handler)
	defer server.Close()
	mesh, err := NewMesh(WithMeshJSONRPCAgentOptions(WithJSONRPCHeader("X-Mesh-Test", "yes")))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	if _, err := mesh.Bootstrap(ctx, NewStaticDiscoverer(AgentCard{
		Name:         "reviewer",
		Capabilities: []string{"code.review"},
		Protocols: []ProtocolBinding{
			{Name: "a2a-jsonrpc", Transport: "jsonrpc", URL: server.URL},
		},
	})); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	result, err := mesh.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "reviewed" {
		t.Fatalf("Route() result = %+v, want reviewed", result)
	}
}

func httpHeaderHandler(next http.Handler, key string, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != httpPathCard && r.Header.Get(key) != value {
			http.Error(w, "missing mesh header", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestMeshDiscoversHTTPAgentAndRoutesByCapability(t *testing.T) {
	ctx := context.Background()
	events := []gopact.Event{}
	local := FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			return Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}
	server := httptest.NewServer(NewHTTPHandler(local))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}
	mesh, err := NewMesh(WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	if _, err := mesh.Discover(ctx, remote, DiscoveryQuery{URL: server.URL, Require: []string{"code.review"}}); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	result, err := mesh.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}

	if result.Output != "reviewed: diff" {
		t.Fatalf("Route() result = %+v, want HTTP agent output", result)
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	}) {
		t.Fatalf("published events = %v, want HTTP discovery, registration, and call evidence", got)
	}
}

func TestMeshStreamPolicyDenyDoesNotPublishSentEvidence(t *testing.T) {
	ctx := context.Background()
	events := []gopact.Event{}
	mesh, err := NewMesh(
		WithMeshPolicy(gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionStream {
				t.Fatalf("policy action = %s, want stream", req.Action)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "stream blocked"}, nil
		})),
		WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Register(ctx, FakeAgent{
		CardValue: AgentCard{Name: "reviewer"},
		StreamFunc: func(context.Context, Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				yield(TaskEvent{Status: TaskStatusRunning}, nil)
			}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	events = nil

	streamed, err := collectTaskEvents(mesh.Stream(ctx, "reviewer", Task{ID: "task-1", Input: "diff"}))
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Stream() error = %v, want ErrPolicyDenied", err)
	}
	if len(streamed) != 0 {
		t.Fatalf("Stream() events = %+v, want no remote stream events after policy denial", streamed)
	}
	if got := eventTypes(events); containsEventType(got, gopact.EventA2ATaskSent) {
		t.Fatalf("published events = %v, want no sent event before local policy allow", got)
	} else if !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	}) {
		t.Fatalf("published events = %v, want policy evidence then failed event", got)
	}
}

func TestMeshCancelPublishesCanceledEvidence(t *testing.T) {
	wantIDs := gopact.RuntimeIDs{RunID: "ctx-run", AgentID: "mesh-agent", TraceID: "trace-1"}
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{RunID: "ctx-run", TraceID: "trace-1"})
	events := []gopact.Event{}
	var cancelContextIDs gopact.RuntimeIDs
	mesh, err := NewMesh(
		WithMeshRuntimeIDs(gopact.RuntimeIDs{AgentID: "mesh-agent"}),
		WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Register(context.Background(), FakeAgent{
		CardValue: AgentCard{Name: "reviewer"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			cancelContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	events = nil

	result, err := mesh.Cancel(ctx, "reviewer", "task-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if result.TaskID != "task-1" || len(result.Events) != 1 || result.Events[0].Type != gopact.EventA2ATaskCanceled {
		t.Fatalf("Cancel() = %+v, want canceled evidence", result)
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, []gopact.EventType{gopact.EventA2ATaskCanceled}) {
		t.Fatalf("published events = %v, want canceled evidence", got)
	}
	if cancelContextIDs != wantIDs {
		t.Fatalf("cancel context IDs = %+v, want %+v", cancelContextIDs, wantIDs)
	}
	if result.Events[0].IDs != wantIDs || events[0].IDs != wantIDs {
		t.Fatalf("cancel event IDs = %+v / %+v, want %+v", result.Events[0].IDs, events[0].IDs, wantIDs)
	}
}

type meshDiscoverableAgent struct {
	card AgentCard
}

func (a meshDiscoverableAgent) Card() AgentCard {
	return a.card
}

func (a meshDiscoverableAgent) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	if query.Name != "" && query.Name != a.card.Name {
		return DiscoveryResult{}, ErrAgentNotFound
	}
	return DiscoveryResult{Card: a.card}, nil
}

func (a meshDiscoverableAgent) Send(ctx context.Context, task Task) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if task.Input == "" {
		return Result{}, errors.New("input is required")
	}
	return Result{TaskID: task.ID, Output: "researched: " + task.Input}, nil
}

func (a meshDiscoverableAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(TaskEvent{TaskID: task.ID, Status: TaskStatusFailed, Err: err}, err)
			return
		}
		yield(TaskEvent{TaskID: task.ID, Status: TaskStatusCompleted, Result: &Result{TaskID: task.ID, Output: "researched"}}, nil)
	}
}

func (a meshDiscoverableAgent) Cancel(ctx context.Context, taskID string) error {
	return ctx.Err()
}

func eventTypes(events []gopact.Event) []gopact.EventType {
	types := make([]gopact.EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func containsEventType(types []gopact.EventType, want gopact.EventType) bool {
	for _, eventType := range types {
		if eventType == want {
			return true
		}
	}
	return false
}
