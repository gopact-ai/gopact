package a2aconformance

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
)

func TestRequireAgentMeshConformanceAcceptsDiscoverableStreamingAgent(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			URL:          "https://agents.example/reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "code"},
			Streaming:    true,
		},
	}

	RequireAgentMeshConformance(t, AgentMeshConformanceHarness{
		Agent:            agent,
		Query:            a2a.DiscoveryQuery{Name: "reviewer"},
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})
}

func TestCheckAgentMeshConformanceUsesMeshFacade(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-discovers-and-routes")
}

func TestCheckAgentMeshConformanceRoutesByTags(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name: "reviewer",
			Tags: []string{"code", "local"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		Query:        a2a.DiscoveryQuery{Tags: []string{"code", "local"}},
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "registry-routes-by-card")
	requireConformanceCasePassed(t, results, "mesh-discovers-and-routes")
}

func TestCheckAgentMeshConformanceUsesMeshCallFacade(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-calls-by-name")
}

func TestCheckAgentMeshConformanceRequiresMeshRuntimeIDs(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-propagates-runtime-ids")
}

func TestCheckAgentMeshConformanceRequiresMeshStreamRuntimeIDs(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-propagates-stream-runtime-ids")
}

func TestCheckAgentMeshConformanceRequiresMeshCancelRuntimeIDs(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-propagates-cancel-runtime-ids")
}

func TestCheckAgentMeshConformanceRequiresMeshCardCache(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "code"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-caches-cards")
}

func TestCheckAgentMeshConformanceRequiresMeshBootstrap(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-bootstraps-cards")
}

func TestCheckAgentMeshConformanceRequiresMeshBootstrapMultipleSources(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-bootstrap-multiple-sources")
}

func TestCheckAgentMeshConformanceRequiresBootstrapHTTPAgentOptions(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-bootstrap-http-agent-options")
}

func TestCheckAgentMeshConformanceRequiresBootstrapJSONRPCAgentOptions(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-bootstrap-jsonrpc-agent-options")
}

func TestCheckAgentMeshConformanceRequiresMeshSyncPruneReadiness(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-sync-prunes-unready-http-agents")
}

func TestCheckAgentMeshConformanceRequiresOperationTimeout(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-operation-timeout")
}

func TestCheckAgentMeshConformanceRequiresMeshCallAuth(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-authenticates-call")
}

func TestCheckAgentMeshConformanceRequiresMeshStreamAuth(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-authenticates-stream")
}

func TestCheckAgentMeshConformanceRequiresMeshCancelAuth(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-authenticates-cancel")
}

func TestCheckAgentMeshConformanceUsesMeshStreamFacade(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-routes-stream")
}

func TestCheckAgentMeshConformanceUsesMeshNamedStreamFacade(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-streams-by-name")
}

func TestCheckAgentMeshConformanceAcceptsProgressBeforeCompletedStream(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
		streamProgressFirst: true,
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-streams-by-name")
	requireConformanceCasePassed(t, results, "mesh-route-stream-publishes-evidence")
}

func TestCheckAgentMeshConformanceRequiresMeshEvidence(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-publishes-evidence")
}

func TestCheckAgentMeshConformanceRequiresHeartbeatEvidence(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-heartbeat-publishes-evidence")
}

func TestCheckAgentMeshConformanceRequiresRouteStreamEvidence(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-route-stream-publishes-evidence")
}

func TestCheckAgentMeshConformanceRequiresMeshRouteFallback(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-route-fallback")
}

func TestCheckAgentMeshConformanceRequiresMeshRouteStreamFallback(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-route-stream-fallback")
}

func TestCheckAgentMeshConformanceRequiresPolicyFailClosed(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-policy-deny-fail-closed")
}

func TestCheckAgentMeshConformanceRequiresPolicyReviewInterrupt(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-policy-review-interrupts")
}

func TestCheckAgentMeshConformanceRequiresRouteStreamPolicyFailClosed(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-route-stream-policy-deny-fail-closed")
}

func TestCheckAgentMeshConformanceRequiresRouteStreamPolicyReviewInterrupt(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Streaming:    true,
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:            agent,
		ExpectedCard:     agent.card,
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})

	requireConformanceCasePassed(t, results, "mesh-route-stream-policy-review-interrupts")
}

func TestCheckAgentMeshConformanceRequiresCancelEvidence(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-cancels-with-evidence")
}

func TestCheckAgentMeshConformanceRequiresCancelPolicyDenyFailClosed(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-cancel-policy-deny-fail-closed")
}

func TestCheckAgentMeshConformanceRequiresCancelPolicyReviewInterrupt(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	requireConformanceCasePassed(t, results, "mesh-cancel-policy-review-interrupts")
}

func TestCheckAgentMeshConformanceReportsFailures(t *testing.T) {
	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent: a2a.FakeAgent{
			CardValue: a2a.AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		},
		ExpectedCard: a2a.AgentCard{Name: "reviewer", Capabilities: []string{"code.review"}},
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
			if !errors.Is(result.Err, ErrAgentMeshConformanceFailed) {
				t.Fatalf("case %q error = %v, want ErrAgentMeshConformanceFailed", result.Case, result.Err)
			}
		}
	}
	for _, want := range []string{
		"implements-discoverer",
		"registry-discover-registers-agent",
		"registry-routes-by-card",
	} {
		if !failures[want] {
			t.Fatalf("failures = %v, want case %q to fail", failures, want)
		}
	}
}

func TestCheckAgentMeshConformanceRequiresRouteableCard(t *testing.T) {
	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        meshAgent{card: a2a.AgentCard{Name: "reviewer"}},
		ExpectedCard: a2a.AgentCard{Name: "reviewer"},
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
		}
	}
	if !failures["registry-routes-by-card"] {
		t.Fatalf("failures = %v, want registry-routes-by-card to fail", failures)
	}
}

func TestCheckAgentMeshConformanceUsesHarnessTaskForDiscoveredSend(t *testing.T) {
	agent := meshAgent{
		card: a2a.AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
		requireInput: true,
	}

	results := CheckAgentMeshConformance(context.Background(), AgentMeshConformanceHarness{
		Agent:        agent,
		ExpectedCard: agent.card,
		Task:         a2a.Task{ID: "task-1", Input: "review this diff"},
	})

	failures := map[string]bool{}
	for _, result := range results {
		if !result.Passed {
			failures[result.Case] = true
		}
	}
	if failures["registry-discover-registers-agent"] {
		t.Fatalf("failures = %v, want registry-discover-registers-agent to pass with harness task input", failures)
	}
}

func requireConformanceCasePassed(t *testing.T, results []AgentMeshConformanceResult, name string) {
	t.Helper()
	for _, result := range results {
		if result.Case != name {
			continue
		}
		if !result.Passed {
			t.Fatalf("case %q failed: %v", name, result.Err)
		}
		return
	}
	t.Fatalf("results = %+v, want case %q", results, name)
}

type meshAgent struct {
	card                a2a.AgentCard
	requireInput        bool
	streamProgressFirst bool
}

func (a meshAgent) Card() a2a.AgentCard {
	return a.card
}

func (a meshAgent) Discover(ctx context.Context, query a2a.DiscoveryQuery) (a2a.DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return a2a.DiscoveryResult{}, err
	}
	if query.Name != "" && query.Name != a.card.Name {
		return a2a.DiscoveryResult{}, a2a.ErrAgentNotFound
	}
	return a2a.DiscoveryResult{Card: a.card, Metadata: map[string]any{"source": "mesh-test"}}, nil
}

func (a meshAgent) Send(ctx context.Context, task a2a.Task) (a2a.Result, error) {
	if err := ctx.Err(); err != nil {
		return a2a.Result{}, err
	}
	if a.requireInput && task.Input == "" {
		return a2a.Result{}, errors.New("input is required")
	}
	return a2a.Result{TaskID: task.ID, Output: "reviewed"}, nil
}

func (a meshAgent) Stream(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
	return func(yield func(a2a.TaskEvent, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusFailed, Err: err}, err)
			return
		}
		if a.streamProgressFirst && !yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusRunning, Message: "review started"}, nil) {
			return
		}
		yield(a2a.TaskEvent{TaskID: task.ID, Status: a2a.TaskStatusCompleted, Result: &a2a.Result{TaskID: task.ID, Output: "reviewed"}}, nil)
	}
}

func (a meshAgent) Cancel(ctx context.Context, _ string) error {
	return ctx.Err()
}
