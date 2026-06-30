package a2aconformance

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/a2a"
)

// ErrAgentMeshConformanceFailed marks a failed A2A Agent Mesh conformance case.
var ErrAgentMeshConformanceFailed = errors.New("gopacttest: a2a agent mesh conformance failed")

// AgentMeshConformanceHarness describes one discoverable A2A agent under test.
type AgentMeshConformanceHarness struct {
	Agent            a2a.Agent
	Query            a2a.DiscoveryQuery
	ExpectedCard     a2a.AgentCard
	Task             a2a.Task
	RequireStreaming bool
}

// AgentMeshConformanceResult is the observed result for one Agent Mesh contract case.
type AgentMeshConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckAgentMeshConformance runs reusable discovery-to-routing contract cases.
func CheckAgentMeshConformance(ctx context.Context, harness AgentMeshConformanceHarness) []AgentMeshConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	task := harness.Task
	if task.ID == "" {
		task.ID = "gopact-a2a-mesh-conformance-task"
	}
	if task.Input == "" {
		task.Input = "gopact a2a mesh conformance"
	}
	query := meshDiscoveryQuery(harness.Query, harness.ExpectedCard)

	results := []AgentMeshConformanceResult{
		checkMeshAgentPresent(harness.Agent),
		checkMeshAgentImplementsDiscoverer(harness.Agent),
		checkMeshRegistryDiscoversAgent(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshRegistryRoutesByCard(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshRegistryCachesDefensiveCard(ctx, harness.Agent, query, harness.ExpectedCard),
		checkMeshFacadeCachesCards(ctx, harness.Agent, query, harness.ExpectedCard),
		checkMeshFacadeBootstrapsCards(ctx, harness.ExpectedCard),
		checkMeshFacadeBootstrapHTTPAgentOptions(ctx, task),
		checkMeshFacadeBootstrapJSONRPCAgentOptions(ctx, task),
		checkMeshFacadeCallsByName(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadeOperationTimeout(ctx, task),
		checkMeshFacadePropagatesRuntimeIDs(ctx, task),
		checkMeshFacadeAuthenticatesCall(ctx, task),
		checkMeshFacadeDiscoversAndRoutes(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadePublishesEvidence(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadeRoutesWithFallback(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadePolicyDenyFailClosed(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadePolicyReviewInterrupts(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadeCancelsWithEvidence(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadePropagatesCancelRuntimeIDs(ctx, task),
		checkMeshFacadeAuthenticatesCancel(ctx, task),
		checkMeshFacadeCancelPolicyDenyFailClosed(ctx, harness.Agent, query, harness.ExpectedCard, task),
		checkMeshFacadeCancelPolicyReviewInterrupts(ctx, harness.Agent, query, harness.ExpectedCard, task),
	}
	if harness.RequireStreaming {
		results = append(results, checkMeshRegistryRoutesStream(ctx, harness.Agent, query, harness.ExpectedCard, task))
		results = append(results, checkMeshFacadeStreamsByName(ctx, harness.Agent, query, harness.ExpectedCard, task))
		results = append(results, checkMeshFacadePropagatesStreamRuntimeIDs(ctx, task))
		results = append(results, checkMeshFacadeAuthenticatesStream(ctx, task))
		results = append(results, checkMeshFacadeRoutesStream(ctx, harness.Agent, query, harness.ExpectedCard, task))
		results = append(results, checkMeshFacadeRouteStreamPublishesEvidence(ctx, harness.Agent, query, harness.ExpectedCard, task))
		results = append(results, checkMeshFacadeRouteStreamWithFallback(ctx, harness.Agent, query, harness.ExpectedCard, task))
		results = append(results, checkMeshFacadeRouteStreamPolicyDenyFailClosed(ctx, harness.Agent, query, harness.ExpectedCard, task))
		results = append(results, checkMeshFacadeRouteStreamPolicyReviewInterrupts(ctx, harness.Agent, query, harness.ExpectedCard, task))
	}
	return results
}

// RequireAgentMeshConformance fails the test unless agent satisfies the Agent Mesh contract.
func RequireAgentMeshConformance(t testing.TB, harness AgentMeshConformanceHarness) {
	t.Helper()

	for _, result := range CheckAgentMeshConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("a2a agent mesh conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkMeshAgentPresent(agent a2a.Agent) AgentMeshConformanceResult {
	if agent == nil {
		return failedAgentMeshConformance("has-agent", errors.New("agent is nil"))
	}
	return passedAgentMeshConformance("has-agent")
}

func checkMeshAgentImplementsDiscoverer(agent a2a.Agent) AgentMeshConformanceResult {
	if agent == nil {
		return failedAgentMeshConformance("implements-discoverer", errors.New("agent is nil"))
	}
	if _, ok := agent.(a2a.Discoverer); !ok {
		return failedAgentMeshConformance("implements-discoverer", errors.New("agent does not implement Discoverer"))
	}
	return passedAgentMeshConformance("implements-discoverer")
}

func checkMeshRegistryDiscoversAgent(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	registry, err := discoverMeshAgent(ctx, agent, query, expected)
	if err != nil {
		return failedAgentMeshConformance("registry-discover-registers-agent", err)
	}
	if _, err := registry.Send(ctx, expected.Name, task); err != nil {
		return failedAgentMeshConformance("registry-discover-registers-agent", fmt.Errorf("send discovered agent: %w", err))
	}
	return passedAgentMeshConformance("registry-discover-registers-agent")
}

func checkMeshRegistryRoutesByCard(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	registry, err := discoverMeshAgent(ctx, agent, query, expected)
	if err != nil {
		return failedAgentMeshConformance("registry-routes-by-card", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("registry-routes-by-card", err)
	}
	result, err := registry.Route(ctx, route)
	if err != nil {
		return failedAgentMeshConformance("registry-routes-by-card", err)
	}
	if result.TaskID == "" {
		return failedAgentMeshConformance("registry-routes-by-card", errors.New("route result task id is empty"))
	}
	if len(result.Events) == 0 {
		return failedAgentMeshConformance("registry-routes-by-card", errors.New("route result events are empty"))
	}
	return passedAgentMeshConformance("registry-routes-by-card")
}

func checkMeshRegistryCachesDefensiveCard(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard) AgentMeshConformanceResult {
	registry, err := discoverMeshAgent(ctx, agent, query, expected)
	if err != nil {
		return failedAgentMeshConformance("registry-caches-defensive-card", err)
	}
	first, err := registry.Card(ctx, expected.Name)
	if err != nil {
		return failedAgentMeshConformance("registry-caches-defensive-card", err)
	}
	mutateCard(&first)
	second, err := registry.Card(ctx, expected.Name)
	if err != nil {
		return failedAgentMeshConformance("registry-caches-defensive-card", err)
	}
	if cardHasMutation(second) {
		return failedAgentMeshConformance("registry-caches-defensive-card", errors.New("registry returned shared card data"))
	}
	return passedAgentMeshConformance("registry-caches-defensive-card")
}

func checkMeshFacadeCachesCards(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-caches-cards", errors.New("agent does not implement Discoverer"))
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		return failedAgentMeshConformance("mesh-caches-cards", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-caches-cards", err)
	}
	first, err := mesh.Card(ctx, expected.Name)
	if err != nil {
		return failedAgentMeshConformance("mesh-caches-cards", err)
	}
	if err := checkExpectedCard(first, expected); err != nil {
		return failedAgentMeshConformance("mesh-caches-cards", err)
	}
	mutateCard(&first)
	second, err := mesh.Card(ctx, expected.Name)
	if err != nil {
		return failedAgentMeshConformance("mesh-caches-cards", err)
	}
	if cardHasMutation(second) {
		return failedAgentMeshConformance("mesh-caches-cards", errors.New("mesh returned shared card data"))
	}
	cards, err := mesh.ListCards(ctx)
	if err != nil {
		return failedAgentMeshConformance("mesh-caches-cards", err)
	}
	for _, card := range cards {
		if checkExpectedCard(card, expected) == nil {
			return passedAgentMeshConformance("mesh-caches-cards")
		}
	}
	return failedAgentMeshConformance("mesh-caches-cards", errors.New("mesh list cards missing expected card"))
}

func checkMeshFacadeBootstrapsCards(ctx context.Context, expected a2a.AgentCard) AgentMeshConformanceResult {
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		return failedAgentMeshConformance("mesh-bootstraps-cards", err)
	}
	result, err := mesh.Bootstrap(ctx, a2a.NewStaticDiscoverer(expected))
	if err != nil {
		return failedAgentMeshConformance("mesh-bootstraps-cards", err)
	}
	var found bool
	for _, card := range result.Cards {
		if checkExpectedCard(card, expected) == nil {
			found = true
			break
		}
	}
	if !found {
		return failedAgentMeshConformance("mesh-bootstraps-cards", errors.New("bootstrap result missing expected card"))
	}
	if !hasEventType(result.Events, gopact.EventA2AAgentCardFetched) || !hasEventType(events, gopact.EventA2AAgentCardFetched) {
		return failedAgentMeshConformance("mesh-bootstraps-cards", errors.New("bootstrap missing fetched evidence"))
	}
	card, err := mesh.Card(ctx, expected.Name)
	if err != nil {
		return failedAgentMeshConformance("mesh-bootstraps-cards", err)
	}
	if err := checkExpectedCard(card, expected); err != nil {
		return failedAgentMeshConformance("mesh-bootstraps-cards", err)
	}
	return passedAgentMeshConformance("mesh-bootstraps-cards")
}

func checkMeshFacadeBootstrapHTTPAgentOptions(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	const caseName = "mesh-bootstrap-http-agent-options"
	const headerKey = "X-Gopact-A2A-Conformance"
	const headerValue = "yes"

	card := a2a.AgentCard{
		Name:         "gopact-a2a-bootstrap-http-option-probe",
		Capabilities: []string{"bootstrap.option.probe"},
	}
	handler := requireHTTPHeader(a2a.NewHTTPHandler(a2a.FakeAgent{
		CardValue: card,
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "header accepted"}, nil
		},
	}), headerKey, headerValue)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	mesh, err := a2a.NewMesh(a2a.WithMeshHTTPAgentOptions(
		a2a.WithHTTPHeader(headerKey, headerValue),
	))
	if err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	if _, err := mesh.Bootstrap(ctx, a2a.NewStaticDiscoverer(card)); err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"bootstrap.option.probe"},
		Task:    task,
	})
	if err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	if result.Output != "header accepted" {
		return failedAgentMeshConformance(caseName, fmt.Errorf("route output = %q, want header accepted", result.Output))
	}
	return passedAgentMeshConformance(caseName)
}

func checkMeshFacadeBootstrapJSONRPCAgentOptions(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	const caseName = "mesh-bootstrap-jsonrpc-agent-options"
	const headerKey = "X-Gopact-A2A-Conformance"
	const headerValue = "yes"

	card := a2a.AgentCard{
		Name:         "gopact-a2a-bootstrap-jsonrpc-option-probe",
		Capabilities: []string{"bootstrap.option.probe"},
	}
	handler := requireHTTPHeader(a2a.NewJSONRPCHandler(a2a.FakeAgent{
		CardValue: card,
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "jsonrpc header accepted"}, nil
		},
	}), headerKey, headerValue)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.Protocols = []a2a.ProtocolBinding{
		{Name: "a2a-jsonrpc", Transport: "jsonrpc", URL: server.URL},
	}

	mesh, err := a2a.NewMesh(a2a.WithMeshJSONRPCAgentOptions(
		a2a.WithJSONRPCHeader(headerKey, headerValue),
	))
	if err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	if _, err := mesh.Bootstrap(ctx, a2a.NewStaticDiscoverer(card)); err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"bootstrap.option.probe"},
		Task:    task,
	})
	if err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	if result.Output != "jsonrpc header accepted" {
		return failedAgentMeshConformance(caseName, fmt.Errorf("route output = %q, want jsonrpc header accepted", result.Output))
	}
	return passedAgentMeshConformance(caseName)
}

func checkMeshFacadeAuthenticatesCall(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	card := a2a.AgentCard{Name: "gopact-a2a-auth-call-probe", Capabilities: []string{"auth.probe"}}
	mesh, err := a2a.NewMesh(a2a.WithMeshAuthenticator(a2a.AuthenticatorFunc(func(_ context.Context, req a2a.AuthRequest) (a2a.Auth, error) {
		if req.AgentName != card.Name || req.Action != gopact.PolicyActionSend {
			return a2a.Auth{}, fmt.Errorf("auth request = %+v, want call probe send", req)
		}
		return a2a.Auth{Scheme: "bearer", Principal: "mesh-conformance"}, nil
	})))
	if err != nil {
		return failedAgentMeshConformance("mesh-authenticates-call", err)
	}
	if _, err := mesh.Register(ctx, a2a.FakeAgent{
		CardValue: card,
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			auth, ok := a2a.AuthFromContext(ctx)
			if !ok || task.Auth == nil || task.Auth.Principal != "mesh-conformance" || auth.Principal != "mesh-conformance" {
				return a2a.Result{}, errors.New("call missing injected auth")
			}
			return a2a.Result{TaskID: task.ID, Output: "authenticated"}, nil
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-authenticates-call", err)
	}
	if _, err := mesh.Call(ctx, card.Name, task); err != nil {
		return failedAgentMeshConformance("mesh-authenticates-call", err)
	}
	return passedAgentMeshConformance("mesh-authenticates-call")
}

func checkMeshFacadeOperationTimeout(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	const caseName = "mesh-operation-timeout"

	card := a2a.AgentCard{Name: "gopact-a2a-timeout-probe", Capabilities: []string{"timeout.probe"}}
	mesh, err := a2a.NewMesh(a2a.WithMeshOperationTimeout(time.Millisecond))
	if err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	if _, err := mesh.Register(ctx, a2a.FakeAgent{
		CardValue: card,
		SendFunc: func(ctx context.Context, _ a2a.Task) (a2a.Result, error) {
			<-ctx.Done()
			return a2a.Result{}, ctx.Err()
		},
	}); err != nil {
		return failedAgentMeshConformance(caseName, err)
	}
	result, err := mesh.Call(ctx, card.Name, task)
	if !errors.Is(err, context.DeadlineExceeded) {
		return failedAgentMeshConformance(caseName, fmt.Errorf("call error = %v, want deadline exceeded", err))
	}
	if !hasEventType(result.Events, gopact.EventA2ATaskSent) || !hasEventType(result.Events, gopact.EventA2ATaskFailed) {
		return failedAgentMeshConformance(caseName, errors.New("timeout result missing sent/failed evidence"))
	}
	return passedAgentMeshConformance(caseName)
}

func checkMeshFacadeAuthenticatesCancel(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	card := a2a.AgentCard{Name: "gopact-a2a-auth-cancel-probe", Capabilities: []string{"auth.probe"}}
	mesh, err := a2a.NewMesh(a2a.WithMeshAuthenticator(a2a.AuthenticatorFunc(func(_ context.Context, req a2a.AuthRequest) (a2a.Auth, error) {
		if req.AgentName != card.Name || req.Action != gopact.PolicyActionCancel || req.TaskID != task.ID {
			return a2a.Auth{}, fmt.Errorf("auth request = %+v, want cancel probe", req)
		}
		return a2a.Auth{Scheme: "bearer", Principal: "mesh-conformance"}, nil
	})))
	if err != nil {
		return failedAgentMeshConformance("mesh-authenticates-cancel", err)
	}
	if _, err := mesh.Register(ctx, a2a.FakeAgent{
		CardValue: card,
		CancelFunc: func(ctx context.Context, taskID string) error {
			auth, ok := a2a.AuthFromContext(ctx)
			if !ok || auth.Principal != "mesh-conformance" || taskID != task.ID {
				return errors.New("cancel missing injected auth")
			}
			return nil
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-authenticates-cancel", err)
	}
	if _, err := mesh.Cancel(ctx, card.Name, task.ID); err != nil {
		return failedAgentMeshConformance("mesh-authenticates-cancel", err)
	}
	return passedAgentMeshConformance("mesh-authenticates-cancel")
}

func checkMeshFacadeCallsByName(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-calls-by-name", errors.New("agent does not implement Discoverer"))
	}
	if expected.Name == "" {
		return failedAgentMeshConformance("mesh-calls-by-name", errors.New("expected card name is empty"))
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		return failedAgentMeshConformance("mesh-calls-by-name", err)
	}
	discovery, err := mesh.Discover(ctx, discoverer, query)
	if err != nil {
		return failedAgentMeshConformance("mesh-calls-by-name", err)
	}
	if err := checkExpectedCard(discovery.Card, expected); err != nil {
		return failedAgentMeshConformance("mesh-calls-by-name", err)
	}
	result, err := mesh.Call(ctx, expected.Name, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-calls-by-name", err)
	}
	if result.TaskID == "" {
		return failedAgentMeshConformance("mesh-calls-by-name", errors.New("call result task id is empty"))
	}
	if len(result.Events) == 0 {
		return failedAgentMeshConformance("mesh-calls-by-name", errors.New("call result events are empty"))
	}
	return passedAgentMeshConformance("mesh-calls-by-name")
}

func checkMeshFacadePropagatesRuntimeIDs(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	card := a2a.AgentCard{Name: "gopact-a2a-runtime-id-probe", Capabilities: []string{"identity.probe"}}
	taskIDs := gopact.RuntimeIDs{RunID: "task-run", CallID: "task-call"}
	ctx, meshIDs, ctxIDs := meshRuntimeIDProbeContext(ctx)
	want := taskIDs.WithDefaults(ctxIDs).WithDefaults(meshIDs)

	events := []gopact.Event{}
	var gotTaskIDs gopact.RuntimeIDs
	var gotContextIDs gopact.RuntimeIDs
	mesh, err := a2a.NewMesh(
		a2a.WithMeshRuntimeIDs(meshIDs),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", err)
	}
	if _, err := mesh.Register(context.Background(), a2a.FakeAgent{
		CardValue: card,
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			gotTaskIDs = task.IDs
			gotContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return a2a.Result{TaskID: task.ID, Output: "ok"}, nil
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", err)
	}
	events = nil

	task.IDs = taskIDs
	result, err := mesh.Call(ctx, card.Name, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", err)
	}
	if gotTaskIDs != want {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", fmt.Errorf("agent task ids = %+v, want %+v", gotTaskIDs, want))
	}
	if gotContextIDs != want {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", fmt.Errorf("agent context ids = %+v, want %+v", gotContextIDs, want))
	}
	if err := checkMeshRuntimeEventIDs(result.Events, want); err != nil {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", fmt.Errorf("result events: %w", err))
	}
	if err := checkMeshRuntimeEventIDs(events, want); err != nil {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", fmt.Errorf("published events: %w", err))
	}
	if !hasEventType(result.Events, gopact.EventA2ATaskSent) || !hasEventType(result.Events, gopact.EventA2ATaskCompleted) {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", errors.New("call result missing sent/completed evidence"))
	}
	if !hasEventType(events, gopact.EventA2ATaskSent) || !hasEventType(events, gopact.EventA2ATaskCompleted) {
		return failedAgentMeshConformance("mesh-propagates-runtime-ids", errors.New("published events missing sent/completed evidence"))
	}
	return passedAgentMeshConformance("mesh-propagates-runtime-ids")
}

func checkMeshFacadePropagatesStreamRuntimeIDs(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	card := a2a.AgentCard{Name: "gopact-a2a-stream-runtime-id-probe", Capabilities: []string{"identity.probe"}, Streaming: true}
	taskIDs := gopact.RuntimeIDs{RunID: "task-run", CallID: "task-call"}
	ctx, meshIDs, ctxIDs := meshRuntimeIDProbeContext(ctx)
	want := taskIDs.WithDefaults(ctxIDs).WithDefaults(meshIDs)

	events := []gopact.Event{}
	var gotTaskIDs gopact.RuntimeIDs
	var gotContextIDs gopact.RuntimeIDs
	mesh, err := a2a.NewMesh(
		a2a.WithMeshRuntimeIDs(meshIDs),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", err)
	}
	if _, err := mesh.Register(context.Background(), a2a.FakeAgent{
		CardValue: card,
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			return func(yield func(a2a.TaskEvent, error) bool) {
				gotTaskIDs = task.IDs
				gotContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
				yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusCompleted}, nil)
			}
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", err)
	}
	events = nil

	task.IDs = taskIDs
	var gotStreamEvent a2a.TaskEvent
	var sawStream bool
	for event, err := range mesh.Stream(ctx, card.Name, task) {
		if err != nil {
			return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", err)
		}
		gotStreamEvent = event
		sawStream = true
		break
	}
	if !sawStream {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", errors.New("stream ended without events"))
	}
	if gotTaskIDs != want {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", fmt.Errorf("agent task ids = %+v, want %+v", gotTaskIDs, want))
	}
	if gotContextIDs != want {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", fmt.Errorf("agent context ids = %+v, want %+v", gotContextIDs, want))
	}
	if gotStreamEvent.IDs != want {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", fmt.Errorf("stream event ids = %+v, want %+v", gotStreamEvent.IDs, want))
	}
	if err := checkMeshRuntimeEventIDs(events, want); err != nil {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", fmt.Errorf("published events: %w", err))
	}
	if !hasEventType(events, gopact.EventA2ATaskSent) || !hasEventType(events, gopact.EventA2ATaskCompleted) {
		return failedAgentMeshConformance("mesh-propagates-stream-runtime-ids", errors.New("published events missing sent/completed evidence"))
	}
	return passedAgentMeshConformance("mesh-propagates-stream-runtime-ids")
}

func checkMeshFacadePropagatesCancelRuntimeIDs(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	card := a2a.AgentCard{Name: "gopact-a2a-cancel-runtime-id-probe", Capabilities: []string{"identity.probe"}}
	ctx, meshIDs, ctxIDs := meshRuntimeIDProbeContext(ctx)
	want := ctxIDs.WithDefaults(meshIDs)

	events := []gopact.Event{}
	var gotContextIDs gopact.RuntimeIDs
	mesh, err := a2a.NewMesh(
		a2a.WithMeshRuntimeIDs(meshIDs),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", err)
	}
	if _, err := mesh.Register(context.Background(), a2a.FakeAgent{
		CardValue: card,
		CancelFunc: func(ctx context.Context, taskID string) error {
			if taskID != task.ID {
				return fmt.Errorf("task id = %q, want %q", taskID, task.ID)
			}
			gotContextIDs, _ = gopact.RuntimeIDsFromContext(ctx)
			return nil
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", err)
	}
	events = nil

	result, err := mesh.Cancel(ctx, card.Name, task.ID)
	if err != nil {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", err)
	}
	if gotContextIDs != want {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", fmt.Errorf("agent context ids = %+v, want %+v", gotContextIDs, want))
	}
	if err := checkMeshRuntimeEventIDs(result.Events, want); err != nil {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", fmt.Errorf("result events: %w", err))
	}
	if err := checkMeshRuntimeEventIDs(events, want); err != nil {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", fmt.Errorf("published events: %w", err))
	}
	if !hasEventType(result.Events, gopact.EventA2ATaskCanceled) || !hasEventType(events, gopact.EventA2ATaskCanceled) {
		return failedAgentMeshConformance("mesh-propagates-cancel-runtime-ids", errors.New("cancel missing canceled evidence"))
	}
	return passedAgentMeshConformance("mesh-propagates-cancel-runtime-ids")
}

func checkMeshRegistryRoutesStream(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	registry, err := discoverMeshAgent(ctx, agent, query, expected)
	if err != nil {
		return failedAgentMeshConformance("registry-routes-stream", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("registry-routes-stream", err)
	}
	stream := registry.RouteStream(ctx, route)
	for event, err := range stream {
		if err != nil {
			return failedAgentMeshConformance("registry-routes-stream", err)
		}
		if event.Metadata["agent_name"] != expected.Name {
			return failedAgentMeshConformance("registry-routes-stream", fmt.Errorf("stream metadata agent_name = %v, want %q", event.Metadata["agent_name"], expected.Name))
		}
		return passedAgentMeshConformance("registry-routes-stream")
	}
	return failedAgentMeshConformance("registry-routes-stream", errors.New("route stream ended without events"))
}

func checkMeshFacadeAuthenticatesStream(ctx context.Context, task a2a.Task) AgentMeshConformanceResult {
	card := a2a.AgentCard{Name: "gopact-a2a-auth-stream-probe", Capabilities: []string{"auth.probe"}, Streaming: true}
	mesh, err := a2a.NewMesh(a2a.WithMeshAuthenticator(a2a.AuthenticatorFunc(func(_ context.Context, req a2a.AuthRequest) (a2a.Auth, error) {
		if req.AgentName != card.Name || req.Action != gopact.PolicyActionStream {
			return a2a.Auth{}, fmt.Errorf("auth request = %+v, want stream probe", req)
		}
		return a2a.Auth{Scheme: "bearer", Principal: "mesh-conformance"}, nil
	})))
	if err != nil {
		return failedAgentMeshConformance("mesh-authenticates-stream", err)
	}
	if _, err := mesh.Register(ctx, a2a.FakeAgent{
		CardValue: card,
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			return func(yield func(a2a.TaskEvent, error) bool) {
				auth, ok := a2a.AuthFromContext(ctx)
				if !ok || task.Auth == nil || task.Auth.Principal != "mesh-conformance" || auth.Principal != "mesh-conformance" {
					yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusFailed}, errors.New("stream missing injected auth"))
					return
				}
				yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusCompleted}, nil)
			}
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-authenticates-stream", err)
	}
	var sawStream bool
	for _, err := range mesh.Stream(ctx, card.Name, task) {
		if err != nil {
			return failedAgentMeshConformance("mesh-authenticates-stream", err)
		}
		sawStream = true
		break
	}
	if !sawStream {
		return failedAgentMeshConformance("mesh-authenticates-stream", errors.New("auth stream ended without events"))
	}
	return passedAgentMeshConformance("mesh-authenticates-stream")
}

func checkMeshFacadeStreamsByName(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-streams-by-name", errors.New("agent does not implement Discoverer"))
	}
	if expected.Name == "" {
		return failedAgentMeshConformance("mesh-streams-by-name", errors.New("expected card name is empty"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		return failedAgentMeshConformance("mesh-streams-by-name", err)
	}
	discovery, err := mesh.Discover(ctx, discoverer, query)
	if err != nil {
		return failedAgentMeshConformance("mesh-streams-by-name", err)
	}
	if err := checkExpectedCard(discovery.Card, expected); err != nil {
		return failedAgentMeshConformance("mesh-streams-by-name", err)
	}
	var sawStream bool
	for event, err := range mesh.Stream(ctx, expected.Name, task) {
		if err != nil {
			return failedAgentMeshConformance("mesh-streams-by-name", err)
		}
		if event.Metadata["agent_name"] != expected.Name {
			return failedAgentMeshConformance("mesh-streams-by-name", fmt.Errorf("stream metadata agent_name = %v, want %q", event.Metadata["agent_name"], expected.Name))
		}
		sawStream = true
		break
	}
	if !sawStream {
		return failedAgentMeshConformance("mesh-streams-by-name", errors.New("stream ended without events"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-streams-by-name", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-streams-by-name")
}

func checkMeshFacadeDiscoversAndRoutes(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-discovers-and-routes", errors.New("agent does not implement Discoverer"))
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		return failedAgentMeshConformance("mesh-discovers-and-routes", err)
	}
	discovery, err := mesh.Discover(ctx, discoverer, query)
	if err != nil {
		return failedAgentMeshConformance("mesh-discovers-and-routes", err)
	}
	if err := checkExpectedCard(discovery.Card, expected); err != nil {
		return failedAgentMeshConformance("mesh-discovers-and-routes", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-discovers-and-routes", err)
	}
	result, err := mesh.Route(ctx, route)
	if err != nil {
		return failedAgentMeshConformance("mesh-discovers-and-routes", err)
	}
	if result.TaskID == "" {
		return failedAgentMeshConformance("mesh-discovers-and-routes", errors.New("route result task id is empty"))
	}
	if len(result.Events) == 0 {
		return failedAgentMeshConformance("mesh-discovers-and-routes", errors.New("route result events are empty"))
	}
	return passedAgentMeshConformance("mesh-discovers-and-routes")
}

func checkMeshFacadePublishesEvidence(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-publishes-evidence", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		return failedAgentMeshConformance("mesh-publishes-evidence", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-publishes-evidence", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-publishes-evidence", err)
	}
	if _, err := mesh.Route(ctx, route); err != nil {
		return failedAgentMeshConformance("mesh-publishes-evidence", err)
	}
	for _, typ := range []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-publishes-evidence", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-publishes-evidence")
}

func checkMeshFacadeRoutesWithFallback(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-route-fallback", errors.New("agent does not implement Discoverer"))
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		return failedAgentMeshConformance("mesh-route-fallback", err)
	}
	first := a2a.AgentCard{
		Name:         "gopact-a2a-mesh-fallback-primary",
		Capabilities: append([]string(nil), expected.Capabilities...),
		Metadata:     copyAnyMap(expected.Metadata),
	}
	if _, err := mesh.Register(ctx, a2a.FakeAgent{
		CardValue: first,
		SendFunc: func(context.Context, a2a.Task) (a2a.Result, error) {
			return a2a.Result{TaskID: task.ID}, errors.New("fallback primary failed")
		},
	}); err != nil {
		return failedAgentMeshConformance("mesh-route-fallback", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-route-fallback", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-fallback", err)
	}
	route.Fallback = true
	result, err := mesh.Route(ctx, route)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-fallback", err)
	}
	if result.TaskID == "" {
		return failedAgentMeshConformance("mesh-route-fallback", errors.New("route result task id is empty"))
	}
	if !hasEventType(result.Events, gopact.EventA2ATaskFailed) || !hasEventType(result.Events, gopact.EventA2ATaskCompleted) {
		return failedAgentMeshConformance("mesh-route-fallback", errors.New("fallback route missing failed and completed evidence"))
	}
	return passedAgentMeshConformance("mesh-route-fallback")
}

func checkMeshFacadePolicyDenyFailClosed(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-policy-deny-fail-closed", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(
		a2a.WithMeshPolicy(gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "blocked by conformance"}, nil
		})),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-policy-deny-fail-closed", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-policy-deny-fail-closed", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-policy-deny-fail-closed", err)
	}
	_, err = mesh.Route(ctx, route)
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		return failedAgentMeshConformance("mesh-policy-deny-fail-closed", fmt.Errorf("route error = %v, want ErrPolicyDenied", err))
	}
	if hasEventType(events, gopact.EventA2ATaskSent) {
		return failedAgentMeshConformance("mesh-policy-deny-fail-closed", errors.New("published task_sent before local policy allow"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-policy-deny-fail-closed", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-policy-deny-fail-closed")
}

func checkMeshFacadePolicyReviewInterrupts(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-policy-review-interrupts", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(
		a2a.WithMeshPolicy(gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionSend {
				return gopact.PolicyDecision{}, fmt.Errorf("policy action = %s, want send", req.Action)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review by conformance"}, nil
		})),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-policy-review-interrupts", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-policy-review-interrupts", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-policy-review-interrupts", err)
	}
	_, err = mesh.Route(ctx, route)
	if !errors.Is(err, gopact.ErrInterrupted) {
		return failedAgentMeshConformance("mesh-policy-review-interrupts", fmt.Errorf("route error = %v, want ErrInterrupted", err))
	}
	if hasEventType(events, gopact.EventA2ATaskSent) {
		return failedAgentMeshConformance("mesh-policy-review-interrupts", errors.New("published task_sent before policy review approval"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-policy-review-interrupts", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-policy-review-interrupts")
}

func checkMeshFacadeCancelsWithEvidence(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-cancels-with-evidence", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		return failedAgentMeshConformance("mesh-cancels-with-evidence", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-cancels-with-evidence", err)
	}
	result, err := mesh.Cancel(ctx, expected.Name, task.ID)
	if err != nil {
		return failedAgentMeshConformance("mesh-cancels-with-evidence", err)
	}
	if result.TaskID == "" || !hasEventType(result.Events, gopact.EventA2ATaskCanceled) {
		return failedAgentMeshConformance("mesh-cancels-with-evidence", errors.New("cancel result missing canceled evidence"))
	}
	if !hasEventType(events, gopact.EventA2ATaskCanceled) {
		return failedAgentMeshConformance("mesh-cancels-with-evidence", errors.New("sink missing canceled evidence"))
	}
	return passedAgentMeshConformance("mesh-cancels-with-evidence")
}

func checkMeshFacadeCancelPolicyDenyFailClosed(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(
		a2a.WithMeshPolicy(gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionCancel {
				return gopact.PolicyDecision{}, fmt.Errorf("policy action = %s, want cancel", req.Action)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "blocked by conformance"}, nil
		})),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", err)
	}
	result, err := mesh.Cancel(ctx, expected.Name, task.ID)
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", fmt.Errorf("cancel error = %v, want ErrPolicyDenied", err))
	}
	if result.TaskID != task.ID || !hasEventType(result.Events, gopact.EventA2ATaskFailed) {
		return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", errors.New("cancel result missing failed evidence"))
	}
	if hasEventType(result.Events, gopact.EventA2ATaskCanceled) || hasEventType(events, gopact.EventA2ATaskCanceled) {
		return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", errors.New("published canceled evidence after local policy deny"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-cancel-policy-deny-fail-closed")
}

func checkMeshFacadeCancelPolicyReviewInterrupts(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(
		a2a.WithMeshPolicy(gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionCancel {
				return gopact.PolicyDecision{}, fmt.Errorf("policy action = %s, want cancel", req.Action)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review by conformance"}, nil
		})),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", err)
	}
	result, err := mesh.Cancel(ctx, expected.Name, task.ID)
	if !errors.Is(err, gopact.ErrInterrupted) {
		return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", fmt.Errorf("cancel error = %v, want ErrInterrupted", err))
	}
	if result.TaskID != task.ID || !hasEventType(result.Events, gopact.EventA2ATaskFailed) {
		return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", errors.New("cancel result missing failed evidence"))
	}
	if hasEventType(result.Events, gopact.EventA2ATaskCanceled) || hasEventType(events, gopact.EventA2ATaskCanceled) {
		return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", errors.New("published canceled evidence before policy review approval"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-cancel-policy-review-interrupts", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-cancel-policy-review-interrupts")
}

func checkMeshFacadeRoutesStream(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-routes-stream", errors.New("agent does not implement Discoverer"))
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		return failedAgentMeshConformance("mesh-routes-stream", err)
	}
	discovery, err := mesh.Discover(ctx, discoverer, query)
	if err != nil {
		return failedAgentMeshConformance("mesh-routes-stream", err)
	}
	if err := checkExpectedCard(discovery.Card, expected); err != nil {
		return failedAgentMeshConformance("mesh-routes-stream", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-routes-stream", err)
	}
	for event, err := range mesh.RouteStream(ctx, route) {
		if err != nil {
			return failedAgentMeshConformance("mesh-routes-stream", err)
		}
		if event.Metadata["agent_name"] != expected.Name {
			return failedAgentMeshConformance("mesh-routes-stream", fmt.Errorf("stream metadata agent_name = %v, want %q", event.Metadata["agent_name"], expected.Name))
		}
		return passedAgentMeshConformance("mesh-routes-stream")
	}
	return failedAgentMeshConformance("mesh-routes-stream", errors.New("route stream ended without events"))
}

func checkMeshFacadeRouteStreamPublishesEvidence(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", err)
	}
	var sawStream bool
	for _, err := range mesh.RouteStream(ctx, route) {
		if err != nil {
			return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", err)
		}
		sawStream = true
		break
	}
	if !sawStream {
		return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", errors.New("route stream ended without events"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-route-stream-publishes-evidence", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-route-stream-publishes-evidence")
}

func checkMeshFacadeRouteStreamWithFallback(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-route-stream-fallback", errors.New("agent does not implement Discoverer"))
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-fallback", err)
	}
	first := a2a.AgentCard{
		Name:         "gopact-a2a-mesh-stream-fallback-primary",
		Capabilities: append([]string(nil), expected.Capabilities...),
		Metadata:     copyAnyMap(expected.Metadata),
	}
	if _, err := mesh.Register(ctx, a2a.FakeAgent{CardValue: first}); err != nil {
		return failedAgentMeshConformance("mesh-route-stream-fallback", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-route-stream-fallback", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-fallback", err)
	}
	route.Fallback = true
	for event, err := range mesh.RouteStream(ctx, route) {
		if err != nil {
			return failedAgentMeshConformance("mesh-route-stream-fallback", err)
		}
		if event.Metadata["agent_name"] != expected.Name {
			return failedAgentMeshConformance("mesh-route-stream-fallback", fmt.Errorf("stream metadata agent_name = %v, want %q", event.Metadata["agent_name"], expected.Name))
		}
		return passedAgentMeshConformance("mesh-route-stream-fallback")
	}
	return failedAgentMeshConformance("mesh-route-stream-fallback", errors.New("route stream fallback ended without events"))
}

func checkMeshFacadeRouteStreamPolicyDenyFailClosed(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(
		a2a.WithMeshPolicy(gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionStream {
				return gopact.PolicyDecision{}, fmt.Errorf("policy action = %s, want stream", req.Action)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "blocked by conformance"}, nil
		})),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", err)
	}
	var sawPolicyDeny bool
	for _, err := range mesh.RouteStream(ctx, route) {
		if err == nil {
			return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", errors.New("route stream yielded remote event after local policy deny"))
		}
		if !errors.Is(err, gopact.ErrPolicyDenied) {
			return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", fmt.Errorf("route stream error = %v, want ErrPolicyDenied", err))
		}
		sawPolicyDeny = true
		break
	}
	if !sawPolicyDeny {
		return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", errors.New("route stream ended without policy denial"))
	}
	if hasEventType(events, gopact.EventA2ATaskSent) {
		return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", errors.New("published task_sent before local policy allow"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-route-stream-policy-deny-fail-closed")
}

func checkMeshFacadeRouteStreamPolicyReviewInterrupts(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard, task a2a.Task) AgentMeshConformanceResult {
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", errors.New("agent does not implement Discoverer"))
	}
	events := []gopact.Event{}
	mesh, err := a2a.NewMesh(
		a2a.WithMeshPolicy(gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Action != gopact.PolicyActionStream {
				return gopact.PolicyDecision{}, fmt.Errorf("policy action = %s, want stream", req.Action)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review by conformance"}, nil
		})),
		a2a.WithMeshEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", err)
	}
	if _, err := mesh.Discover(ctx, discoverer, query); err != nil {
		return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", err)
	}
	route, err := routeQueryForCard(expected, task)
	if err != nil {
		return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", err)
	}
	var sawInterrupt bool
	for _, err := range mesh.RouteStream(ctx, route) {
		if err == nil {
			return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", errors.New("route stream yielded remote event after policy review"))
		}
		if !errors.Is(err, gopact.ErrInterrupted) {
			return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", fmt.Errorf("route stream error = %v, want ErrInterrupted", err))
		}
		sawInterrupt = true
		break
	}
	if !sawInterrupt {
		return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", errors.New("route stream ended without policy review interrupt"))
	}
	if hasEventType(events, gopact.EventA2ATaskSent) {
		return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", errors.New("published task_sent before policy review approval"))
	}
	for _, typ := range []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	} {
		if !hasEventType(events, typ) {
			return failedAgentMeshConformance("mesh-route-stream-policy-review-interrupts", fmt.Errorf("missing event type %s", typ))
		}
	}
	return passedAgentMeshConformance("mesh-route-stream-policy-review-interrupts")
}

func discoverMeshAgent(ctx context.Context, agent a2a.Agent, query a2a.DiscoveryQuery, expected a2a.AgentCard) (*a2a.Registry, error) {
	if agent == nil {
		return nil, errors.New("agent is nil")
	}
	discoverer, ok := agent.(a2a.Discoverer)
	if !ok {
		return nil, errors.New("agent does not implement Discoverer")
	}
	registry := a2a.NewRegistry()
	result, err := registry.Discover(ctx, discoverer, query)
	if err != nil {
		return nil, err
	}
	if err := checkExpectedCard(result.Card, expected); err != nil {
		return nil, err
	}
	return registry, nil
}

func meshDiscoveryQuery(query a2a.DiscoveryQuery, expected a2a.AgentCard) a2a.DiscoveryQuery {
	query = copyDiscoveryQuery(query)
	if query.Name == "" && query.URL == "" && len(query.Require) == 0 && len(query.Metadata) == 0 {
		query.Name = expected.Name
	}
	return query
}

func routeQueryForCard(card a2a.AgentCard, task a2a.Task) (a2a.RouteQuery, error) {
	route := a2a.RouteQuery{Task: task}
	if len(card.Capabilities) > 0 {
		route.Require = append([]string(nil), card.Capabilities...)
	}
	if len(card.Metadata) > 0 {
		route.Metadata = copyAnyMap(card.Metadata)
	}
	if len(route.Require) == 0 && len(route.Metadata) == 0 {
		return a2a.RouteQuery{}, errors.New("expected card must include routeable capabilities or metadata")
	}
	return route, nil
}

func meshRuntimeIDProbeContext(ctx context.Context) (context.Context, gopact.RuntimeIDs, gopact.RuntimeIDs) {
	meshIDs := gopact.RuntimeIDs{
		UserID:       "mesh-user",
		SessionID:    "mesh-session",
		ThreadID:     "mesh-thread",
		RunID:        "mesh-run",
		AgentID:      "mesh-agent",
		AppID:        "mesh-app",
		CallID:       "mesh-call",
		ParentCallID: "mesh-parent-call",
		TraceID:      "mesh-trace",
	}
	ctxIDs := gopact.RuntimeIDs{
		UserID:       "ctx-user",
		SessionID:    "ctx-session",
		ThreadID:     "ctx-thread",
		RunID:        "ctx-run",
		AppID:        "ctx-app",
		ParentCallID: "ctx-parent-call",
		TraceID:      "ctx-trace",
	}
	return gopact.ContextWithRuntimeIDs(ctx, ctxIDs), meshIDs, ctxIDs
}

func checkMeshRuntimeEventIDs(events []gopact.Event, want gopact.RuntimeIDs) error {
	if len(events) == 0 {
		return errors.New("events are empty")
	}
	for _, event := range events {
		if event.IDs != want {
			return fmt.Errorf("event %s ids = %+v, want %+v", event.Type, event.IDs, want)
		}
	}
	return nil
}

func hasEventType(events []gopact.Event, typ gopact.EventType) bool {
	for _, event := range events {
		if event.Type == typ {
			return true
		}
	}
	return false
}

func requireHTTPHeader(next http.Handler, key string, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Header.Get(key) != value {
			http.Error(w, "missing conformance header", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func passedAgentMeshConformance(name string) AgentMeshConformanceResult {
	return AgentMeshConformanceResult{Case: name, Passed: true}
}

func failedAgentMeshConformance(name string, err error) AgentMeshConformanceResult {
	return AgentMeshConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrAgentMeshConformanceFailed, err),
	}
}
