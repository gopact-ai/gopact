package a2a_test

import (
	"context"
	"iter"
	"net/http/httptest"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
	"github.com/gopact-ai/gopact/gopacttest/a2aconformance"
)

func TestJSONRPCAgentSatisfiesAgentMeshConformance(t *testing.T) {
	local := a2a.FakeAgent{
		CardValue: a2a.AgentCard{
			Name:         "reviewer",
			URL:          "https://agents.example/reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "code"},
			Protocols: []a2a.ProtocolBinding{
				{Name: "a2a-jsonrpc", Transport: "jsonrpc", URL: "https://agents.example/reviewer"},
			},
			Streaming: true,
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
	server := httptest.NewServer(a2a.NewJSONRPCHandler(local))
	defer server.Close()

	remote, err := a2a.NewJSONRPCAgent(
		server.URL,
		a2a.WithJSONRPCClient(server.Client()),
		a2a.WithJSONRPCAgentCard(local.Card()),
	)
	if err != nil {
		t.Fatalf("NewJSONRPCAgent() error = %v", err)
	}

	a2aconformance.RequireAgentMeshConformance(t, a2aconformance.AgentMeshConformanceHarness{
		Agent:            remote,
		Query:            a2a.DiscoveryQuery{URL: server.URL},
		ExpectedCard:     local.Card(),
		Task:             a2a.Task{ID: "task-1", Input: "review this diff"},
		RequireStreaming: true,
	})
}
