package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestHTTPAgentRoundTripsCardSendAndCancel(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	var sentTask Task
	var sentAuth Auth
	var canceledTaskID string
	agent := FakeAgent{
		CardValue: AgentCard{
			Name:         "planner",
			Description:  "plans tasks",
			Capabilities: []string{"planning"},
			Metadata:     map[string]any{"owner": "agents"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			sentTask = task
			var ok bool
			sentAuth, ok = AuthFromContext(ctx)
			if !ok {
				t.Fatal("handler context missing A2A auth")
			}
			return Result{
				TaskID:    task.ID,
				Output:    "planned: " + task.Input,
				Artifacts: []gopact.ArtifactRef{artifact},
				Metadata:  map[string]any{"route": "http"},
				Events: []gopact.Event{{
					Type: gopact.EventA2ATaskCompleted,
					IDs:  task.IDs,
				}},
			}, nil
		},
		CancelFunc: func(_ context.Context, taskID string) error {
			canceledTaskID = taskID
			return nil
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()

	remote, err := NewHTTPAgent(server.URL,
		WithHTTPClient(server.Client()),
		WithHTTPAgentCard(agent.Card()),
		WithHTTPHeader("X-Gopact-Test", "yes"),
	)
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	discovered, err := remote.Discover(ctx, DiscoveryQuery{Name: "planner"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if discovered.Card.Name != "planner" ||
		discovered.Card.Description != "plans tasks" ||
		discovered.Card.Metadata["owner"] != "agents" {
		t.Fatalf("Discover() = %+v, want remote card", discovered.Card)
	}

	callCtx := ContextWithAuth(ctx, Auth{
		Scheme:        "bearer",
		Principal:     "svc-planner",
		CredentialRef: "secret://a2a/planner",
	})
	result, err := remote.Send(callCtx, Task{
		ID:       "task-1",
		IDs:      gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", CallID: "call-1", UserID: "user-1"},
		Input:    "write tests",
		Metadata: map[string]any{"priority": "high"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if sentTask.ID != "task-1" ||
		sentTask.Input != "write tests" ||
		sentTask.IDs.RunID != "run-1" ||
		sentTask.Auth == nil ||
		sentTask.Auth.Principal != "svc-planner" ||
		sentAuth.Principal != "svc-planner" {
		t.Fatalf("sent task = %+v auth=%+v, want task with auth context", sentTask, sentAuth)
	}
	if result.TaskID != "task-1" ||
		result.Output != "planned: write tests" ||
		len(result.Artifacts) != 1 ||
		result.Artifacts[0].ID != artifact.ID ||
		result.Metadata["route"] != "http" ||
		len(result.Events) != 1 ||
		result.Events[0].Type != gopact.EventA2ATaskCompleted {
		t.Fatalf("Send() = %+v, want remote result", result)
	}

	if err := remote.Cancel(ctx, "task-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceledTaskID != "task-1" {
		t.Fatalf("canceled task id = %q, want task-1", canceledTaskID)
	}
}

func TestHTTPAgentDiscoversWellKnownAgentCard(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/.well-known/agent-card.json" {
			http.NotFound(w, r)
			return
		}
		writeHTTPJSON(w, http.StatusOK, AgentCard{Name: "planner", Description: "plans tasks"})
	}))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	discovered, err := remote.Discover(ctx, DiscoveryQuery{Name: "planner"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if discovered.Card.Name != "planner" || discovered.Card.Description != "plans tasks" {
		t.Fatalf("Discover() = %+v, want well-known card", discovered.Card)
	}
}

func TestHTTPAgentReadinessCheckFiltersNotReadyDiscovery(t *testing.T) {
	ctx := context.Background()
	readyHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-card.json":
			writeHTTPJSON(w, http.StatusOK, AgentCard{
				Name:   "planner",
				Health: &HealthHints{ReadinessPath: "/custom-ready"},
			})
		case "/custom-ready":
			readyHits++
			writeHTTPJSON(w, http.StatusServiceUnavailable, httpStatusResponse{Status: "not_ready"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()), WithHTTPReadinessCheck())
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	_, err = remote.Discover(ctx, DiscoveryQuery{Name: "planner"})
	if !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("Discover() error = %v, want ErrHTTPStatus", err)
	}
	if readyHits != 1 {
		t.Fatalf("readiness hits = %d, want 1", readyHits)
	}
}

func TestHTTPAgentListCardsReturnsWellKnownAgentCard(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/.well-known/agent-card.json" {
			http.NotFound(w, r)
			return
		}
		writeHTTPJSON(w, http.StatusOK, AgentCard{Name: "planner", Description: "plans tasks"})
	}))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	cards, err := remote.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 1 ||
		cards[0].Name != "planner" ||
		cards[0].Description != "plans tasks" ||
		cards[0].URL != server.URL {
		t.Fatalf("ListCards() = %+v, want well-known card with endpoint URL", cards)
	}
}

func TestNewHTTPCardListersBootstrapMultipleEndpoints(t *testing.T) {
	ctx := context.Background()
	newServer := func(card AgentCard) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/.well-known/agent-card.json" {
				http.NotFound(w, r)
				return
			}
			if r.Header.Get("X-Cluster") != "dev" {
				http.Error(w, "missing cluster header", http.StatusUnauthorized)
				return
			}
			writeHTTPJSON(w, http.StatusOK, card)
		}))
	}
	planner := newServer(AgentCard{Name: "planner", Capabilities: []string{"planning"}})
	defer planner.Close()
	reviewer := newServer(AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}})
	defer reviewer.Close()

	listers, err := NewHTTPCardListers([]string{planner.URL, reviewer.URL}, WithHTTPHeader("X-Cluster", "dev"))
	if err != nil {
		t.Fatalf("NewHTTPCardListers() error = %v", err)
	}
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	bootstrap, err := mesh.Bootstrap(ctx, listers...)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Cards) != 2 ||
		bootstrap.Cards[0].Name != "planner" ||
		bootstrap.Cards[1].Name != "reviewer" {
		t.Fatalf("Bootstrap() cards = %+v, want planner then reviewer", bootstrap.Cards)
	}
}

func TestHTTPRegistryBootstrapsMultipleAgentCards(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/registry/agents.json" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Cluster") != "dev" {
			http.Error(w, "missing cluster header", http.StatusUnauthorized)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"agents": []AgentCard{
				{Name: "planner", Capabilities: []string{"planning"}, Metadata: map[string]any{"domain": "work"}},
				{Name: "reviewer", Capabilities: []string{"code.review"}, Tags: []string{"code", "local"}, Metadata: map[string]any{"domain": "code"}},
			},
		})
	}))
	defer server.Close()
	registry, err := NewHTTPRegistry(server.URL+"/registry/agents.json",
		WithHTTPClient(server.Client()),
		WithHTTPHeader("X-Cluster", "dev"),
	)
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}

	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	bootstrap, err := mesh.Bootstrap(ctx, registry)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Cards) != 2 ||
		bootstrap.Cards[0].Name != "planner" ||
		bootstrap.Cards[1].Name != "reviewer" {
		t.Fatalf("Bootstrap() cards = %+v, want registry order", bootstrap.Cards)
	}

	result, err := registry.Discover(ctx, DiscoveryQuery{Metadata: map[string]any{"domain": "code"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" || result.Metadata["source"] != "http_registry" {
		t.Fatalf("Discover() = %+v, want reviewer from http registry", result)
	}

	result, err = registry.Discover(ctx, DiscoveryQuery{Tags: []string{"code", "local"}})
	if err != nil {
		t.Fatalf("Discover(tags) error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover(tags) = %+v, want reviewer from http registry", result)
	}
}

func TestHTTPRegistryHandlerServesCardsForBootstrap(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(NewHTTPRegistryHandler(NewStaticDiscoverer(
		AgentCard{Name: "planner", Capabilities: []string{"planning"}},
		AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
	)))
	defer server.Close()
	registry, err := NewHTTPRegistry(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}

	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	bootstrap, err := mesh.Bootstrap(ctx, registry)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Cards) != 2 ||
		bootstrap.Cards[0].Name != "planner" ||
		bootstrap.Cards[1].Name != "reviewer" {
		t.Fatalf("Bootstrap() cards = %+v, want handler registry order", bootstrap.Cards)
	}
}

func TestHTTPAgentDiscoverMatchesNameAndMetadata(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{
		CardValue: AgentCard{
			Name:     "reviewer",
			Metadata: map[string]any{"domain": "code"},
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()

	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	result, err := remote.Discover(ctx, DiscoveryQuery{
		Metadata: map[string]any{"domain": "code"},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover() = %+v, want reviewer card", result.Card)
	}

	_, err = remote.Discover(ctx, DiscoveryQuery{
		Name:     "reviewer",
		Metadata: map[string]any{"domain": "research"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Discover() mismatched metadata error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestHTTPAgentDiscoverMatchesCapabilities(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review", "git.diff"},
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()

	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	result, err := remote.Discover(ctx, DiscoveryQuery{
		Require: []string{"code.review", "git.diff"},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover() = %+v, want reviewer card", result.Card)
	}

	_, err = remote.Discover(ctx, DiscoveryQuery{
		Name:    "reviewer",
		Require: []string{"web.search"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Discover() mismatched capability error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestHTTPHandlerServesWellKnownAgentCard(t *testing.T) {
	agent := FakeAgent{CardValue: AgentCard{Name: "planner", Description: "plans tasks"}}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("GET well-known card error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET well-known card status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll card error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("Unmarshal card fields error = %v", err)
	}
	if _, ok := fields["name"]; !ok {
		t.Fatalf("card JSON fields = %+v, want lower-case name", fields)
	}
	if _, ok := fields["Name"]; ok {
		t.Fatalf("card JSON fields = %+v, want no Go field name", fields)
	}
	var card AgentCard
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatalf("Unmarshal card error = %v", err)
	}
	if card.Name != "planner" || card.Description != "plans tasks" {
		t.Fatalf("card = %+v, want handler card", card)
	}
}

func TestHTTPHandlerOptionExposesHostOwnedAgentCard(t *testing.T) {
	card := AgentCard{
		Name:         "reviewer",
		URL:          "https://agents.example/reviewer",
		Capabilities: []string{"code.review"},
		Protocols: []ProtocolBinding{
			{Name: "a2a", Transport: "http", URL: "https://agents.example/reviewer"},
		},
		Health:   &HealthHints{HealthPath: "/healthz", ReadinessPath: "/readyz"},
		Metadata: map[string]any{"domain": "code"},
	}
	server := httptest.NewServer(NewHTTPHandler(
		FakeAgent{CardValue: AgentCard{Name: "local-reviewer"}},
		WithHTTPHandlerAgentCard(card),
	))
	defer server.Close()
	card.Metadata["domain"] = "mutated"
	card.Protocols[0].URL = "https://mutated.example/reviewer"

	resp, err := server.Client().Get(server.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("GET well-known card error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET well-known card status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("Decode card error = %v", err)
	}
	if got.Name != "reviewer" ||
		got.URL != "https://agents.example/reviewer" ||
		got.Metadata["domain"] != "code" ||
		len(got.Protocols) != 1 ||
		got.Protocols[0].URL != "https://agents.example/reviewer" ||
		got.Health == nil ||
		got.Health.HealthPath != "/healthz" ||
		got.Health.ReadinessPath != "/readyz" {
		t.Fatalf("card = %+v, want host-owned card metadata", got)
	}
}

func TestHTTPHandlerSendUsesLowercaseWireJSON(t *testing.T) {
	var sentTask Task
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			sentTask = task
			return Result{
				TaskID:   task.ID,
				Output:   "planned: " + task.Input,
				Metadata: map[string]any{"route": "http"},
			}, nil
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()

	resp, err := server.Client().Post(
		server.URL+"/a2a/task/send",
		"application/json",
		strings.NewReader(`{"id":"task-1","input":"write tests","metadata":{"priority":"high"}}`),
	)
	if err != nil {
		t.Fatalf("POST send error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST send status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if sentTask.ID != "task-1" || sentTask.Input != "write tests" || sentTask.Metadata["priority"] != "high" {
		t.Fatalf("sent task = %+v, want lowercase JSON decoded", sentTask)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll send response error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("Unmarshal response fields error = %v", err)
	}
	if fields["task_id"] != "task-1" || fields["output"] != "planned: write tests" {
		t.Fatalf("response fields = %+v, want lowercase result JSON", fields)
	}
	if _, ok := fields["TaskID"]; ok {
		t.Fatalf("response fields = %+v, want no Go field name", fields)
	}
}

func TestHTTPHandlerServesHealthAndReadiness(t *testing.T) {
	server := httptest.NewServer(NewHTTPHandler(FakeAgent{CardValue: AgentCard{Name: "planner"}}))
	defer server.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestHTTPHandlerReadinessFailsWithoutAgent(t *testing.T) {
	server := httptest.NewServer(NewHTTPHandler(nil))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET readyz error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET readyz status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHTTPHandlerHealthRejectsWrongMethod(t *testing.T) {
	server := httptest.NewServer(NewHTTPHandler(FakeAgent{CardValue: AgentCard{Name: "planner"}}))
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/healthz", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST healthz error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST healthz status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestRegistryDiscoversHTTPAgentForNameCall(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "planned: " + task.Input}, nil
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}
	registry := NewRegistry()

	if _, err := registry.Discover(ctx, remote, DiscoveryQuery{URL: server.URL}); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	result, err := registry.Send(ctx, "planner", Task{ID: "task-1", Input: "write tests"})
	if err != nil {
		t.Fatalf("Send() after discovery error = %v", err)
	}
	if result.Output != "planned: write tests" {
		t.Fatalf("Send() after discovery = %+v, want remote output", result)
	}
	if len(result.Events) == 0 || result.Events[0].Metadata["agent_name"] != "planner" {
		t.Fatalf("Send() events = %+v, want discovered agent evidence", result.Events)
	}
}

func TestRegistryDiscoversHTTPAgentForNameStream(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				if !yield(TaskEvent{TaskID: task.ID, IDs: task.IDs, Status: TaskStatusRunning}, nil) {
					return
				}
				yield(TaskEvent{
					TaskID: task.ID,
					IDs:    task.IDs,
					Status: TaskStatusCompleted,
					Result: &Result{TaskID: task.ID, Output: "planned"},
				}, nil)
			}
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}
	registry := NewRegistry()
	if _, err := registry.Discover(ctx, remote, DiscoveryQuery{URL: server.URL}); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	events, err := collectTaskEvents(registry.Stream(ctx, "planner", Task{ID: "task-1"}))
	if err != nil {
		t.Fatalf("Stream() after discovery error = %v", err)
	}
	if len(events) != 2 ||
		events[0].Status != TaskStatusRunning ||
		events[1].Status != TaskStatusCompleted ||
		events[1].Result.Output != "planned" {
		t.Fatalf("Stream() after discovery events = %+v, want running/completed", events)
	}
}

func TestRegistryDiscoversHTTPAgentForCapabilityRoute(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}
	registry := NewRegistry()
	if _, err := registry.Discover(ctx, remote, DiscoveryQuery{URL: server.URL}); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() after discovery error = %v", err)
	}
	if result.Output != "reviewed: diff" {
		t.Fatalf("Route() after discovery = %+v, want remote output", result)
	}
}

func TestHTTPAgentStreamsTaskEvents(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		StreamFunc: func(_ context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				if !yield(TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Message:  "outline ready",
					Metadata: map[string]any{"phase": "outline"},
				}, nil) {
					return
				}
				if !yield(TaskEvent{
					TaskID:    task.ID,
					IDs:       task.IDs,
					Artifacts: []gopact.ArtifactRef{artifact},
					Metadata:  map[string]any{"phase": "draft"},
				}, nil) {
					return
				}
				yield(TaskEvent{
					TaskID: task.ID,
					IDs:    task.IDs,
					Status: TaskStatusCompleted,
					Result: &Result{
						TaskID:    task.ID,
						Output:    "planned",
						Artifacts: []gopact.ArtifactRef{artifact},
					},
				}, nil)
			}
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()), WithHTTPAgentCard(agent.Card()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	events, err := collectTaskEvents(remote.Stream(ctx, Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"},
		Input: "write tests",
	}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) != 3 ||
		events[0].Message != "outline ready" ||
		len(events[1].Artifacts) != 1 ||
		events[2].Status != TaskStatusCompleted ||
		events[2].Result.Output != "planned" {
		t.Fatalf("Stream() events = %+v, want message/artifact/completed", events)
	}
	for _, event := range events {
		if event.TaskID != "task-1" || event.IDs.RunID != "run-1" || event.IDs.CallID != "call-1" {
			t.Fatalf("stream event identity = task %q ids %+v, want task identity", event.TaskID, event.IDs)
		}
	}
}

func TestHTTPAgentReturnsRemoteErrors(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("remote failed")
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		SendFunc: func(_ context.Context, _ Task) (Result, error) {
			return Result{}, wantErr
		},
	}
	server := httptest.NewServer(NewHTTPHandler(agent))
	defer server.Close()
	remote, err := NewHTTPAgent(server.URL, WithHTTPClient(server.Client()), WithHTTPAgentCard(agent.Card()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	_, err = remote.Send(ctx, Task{ID: "task-1", Input: "write tests"})
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("Send() error = %v, want remote failure", err)
	}
}

func TestHTTPHandlerRejectsInvalidRequests(t *testing.T) {
	server := httptest.NewServer(NewHTTPHandler(FakeAgent{CardValue: AgentCard{Name: "planner"}}))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/a2a/task/send")
	if err != nil {
		t.Fatalf("GET send error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET send status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}

	resp, err = server.Client().Post(server.URL+"/a2a/task/send", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatalf("POST invalid json error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST invalid json status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	resp, err = server.Client().Post(
		server.URL+"/a2a/task/send",
		"application/json",
		strings.NewReader(`{"id":"task-1"} {"id":"task-2"}`),
	)
	if err != nil {
		t.Fatalf("POST trailing json error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST trailing json status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
