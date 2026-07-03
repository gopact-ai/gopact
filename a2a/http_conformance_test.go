package a2a_test

import (
	"context"
	"iter"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gopact-ai/gopact/a2a"
	"github.com/gopact-ai/gopact/gopacttest/a2aconformance"
)

func TestHTTPAgentSatisfiesAgentMeshConformance(t *testing.T) {
	local := a2a.FakeAgent{
		CardValue: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "code"},
			Streaming:    true,
		},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if err := ctx.Err(); err != nil {
				return a2a.Result{}, err
			}
			return a2a.Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			return func(yield func(a2a.TaskEvent, error) bool) {
				if err := ctx.Err(); err != nil {
					yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusFailed, Err: err}, err)
					return
				}
				yield(a2a.TaskEvent{
					TaskID:  task.ID,
					Status:  a2a.TaskStatusCompleted,
					Message: "review complete",
					Result:  &a2a.Result{TaskID: task.ID, Output: "reviewed: " + task.Input},
				}, nil)
			}
		},
	}
	card := local.Card()
	card.URL = "https://agents.example/reviewer"
	card.Protocols = []a2a.ProtocolBinding{{Name: "a2a", Transport: "http", URL: "https://agents.example/reviewer"}}
	card.Health = &a2a.HealthHints{HealthPath: "/healthz", ReadinessPath: "/readyz"}
	server := httptest.NewServer(a2a.NewHTTPHandler(local, a2a.WithHTTPHandlerAgentCard(card)))
	defer server.Close()

	remote, err := a2a.NewHTTPAgent(server.URL, a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}

	a2aconformance.RequireAgentMeshConformance(t, a2aconformance.AgentMeshConformanceHarness{
		Agent:            remote,
		Query:            a2a.DiscoveryQuery{URL: server.URL},
		ExpectedCard:     card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})
}

func TestHTTPRegistrySatisfiesCardRegistrarConformance(t *testing.T) {
	store := a2a.NewRegistry()
	server := httptest.NewServer(a2a.NewHTTPRegistryHandler(store))
	defer server.Close()

	registry, err := a2a.NewHTTPRegistry(server.URL, a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}

	a2aconformance.RequireCardRegistrarConformance(t, a2aconformance.CardRegistrarConformanceHarness{
		Registrar: registry,
		Card: a2a.AgentCard{
			Name:         "reviewer",
			URL:          "http://127.0.0.1:8080",
			Capabilities: []string{"code.review"},
			Tags:         []string{"code"},
			Metadata:     map[string]any{"region": "local"},
		},
		TTL: time.Minute,
	})
}
