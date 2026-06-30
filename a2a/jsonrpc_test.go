package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestJSONRPCAgentRoundTripsDiscoverSendAndCancel(t *testing.T) {
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
				Metadata:  map[string]any{"route": "jsonrpc"},
			}, nil
		},
		CancelFunc: func(ctx context.Context, taskID string) error {
			canceledTaskID = taskID
			return nil
		},
	}
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()

	remote, err := NewJSONRPCAgent(server.URL,
		WithJSONRPCClient(server.Client()),
		WithJSONRPCAgentCard(agent.Card()),
		WithJSONRPCHeader("A2A-Version", "1.0"),
	)
	if err != nil {
		t.Fatalf("NewJSONRPCAgent() error = %v", err)
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
	if sentTask.Metadata["priority"] != "high" {
		t.Fatalf("sent task metadata = %+v, want priority", sentTask.Metadata)
	}
	if result.TaskID != "task-1" ||
		result.Output != "planned: write tests" ||
		len(result.Artifacts) != 1 ||
		result.Artifacts[0].ID != artifact.ID ||
		result.Metadata["route"] != "jsonrpc" {
		t.Fatalf("Send() = %+v, want remote result", result)
	}

	if err := remote.Cancel(ctx, "task-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceledTaskID != "task-1" {
		t.Fatalf("canceled task id = %q, want task-1", canceledTaskID)
	}
}

func TestJSONRPCAgentListCardsReturnsWellKnownAgentCard(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{CardValue: AgentCard{Name: "planner", Description: "plans tasks"}}
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()
	remote, err := NewJSONRPCAgent(server.URL, WithJSONRPCClient(server.Client()))
	if err != nil {
		t.Fatalf("NewJSONRPCAgent() error = %v", err)
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

func TestJSONRPCAgentDiscoverMatchesNameAndMetadata(t *testing.T) {
	ctx := context.Background()
	agent := FakeAgent{
		CardValue: AgentCard{
			Name:     "reviewer",
			Metadata: map[string]any{"domain": "code"},
		},
	}
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()

	remote, err := NewJSONRPCAgent(server.URL, WithJSONRPCClient(server.Client()))
	if err != nil {
		t.Fatalf("NewJSONRPCAgent() error = %v", err)
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

func TestJSONRPCAgentStreamsTaskEventsOverSSE(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				if !yield(TaskEvent{
					TaskID:  task.ID,
					IDs:     task.IDs,
					Status:  TaskStatusRunning,
					Message: "outline ready",
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
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()
	remote, err := NewJSONRPCAgent(server.URL, WithJSONRPCClient(server.Client()), WithJSONRPCAgentCard(agent.Card()))
	if err != nil {
		t.Fatalf("NewJSONRPCAgent() error = %v", err)
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
		events[0].Status != TaskStatusRunning ||
		events[0].Message != "outline ready" ||
		len(events[1].Artifacts) != 1 ||
		events[2].Status != TaskStatusCompleted ||
		events[2].Result.Output != "planned" {
		t.Fatalf("Stream() events = %+v, want running/artifact/completed", events)
	}
	for _, event := range events {
		if event.TaskID != "task-1" || event.IDs.RunID != "run-1" || event.IDs.CallID != "call-1" {
			t.Fatalf("stream event identity = task %q ids %+v, want task identity", event.TaskID, event.IDs)
		}
	}
}

func TestJSONRPCHandlerAcceptsOfficialSendMessageShape(t *testing.T) {
	ctx := context.Background()
	var sentTask Task
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			sentTask = task
			return Result{TaskID: task.ID, Output: "planned: " + task.Input}, nil
		},
	}
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()

	body := []byte(`{
		"jsonrpc": "2.0",
		"id": "req-1",
		"method": "SendMessage",
		"params": {
			"message": {
				"messageId": "msg-1",
				"role": "ROLE_USER",
				"parts": [{"text": "write tests"}]
			}
		}
	}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var out testJSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if out.JSONRPC != "2.0" || out.ID != "req-1" || out.Error != nil {
		t.Fatalf("response = %+v, want JSON-RPC success", out)
	}
	if sentTask.ID != "msg-1" || sentTask.Input != "write tests" {
		t.Fatalf("sent task = %+v, want official message mapped to task", sentTask)
	}
	if out.Result.Task.ID != "msg-1" ||
		out.Result.Task.Status.State != "TASK_STATE_COMPLETED" ||
		out.Result.Task.Artifacts[0].Parts[0].Text != "planned: write tests" {
		t.Fatalf("result task = %+v, want completed task with output artifact", out.Result.Task)
	}
}

func TestJSONRPCHandlerAcceptsOfficialCancelTaskShape(t *testing.T) {
	ctx := context.Background()
	var canceledTaskID string
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			canceledTaskID = taskID
			return nil
		},
	}
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()

	body := []byte(`{
		"jsonrpc": "2.0",
		"id": "req-1",
		"method": "CancelTask",
		"params": {"id": "task-1"}
	}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var out testJSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if out.Error != nil {
		t.Fatalf("response error = %+v, want success", out.Error)
	}
	if canceledTaskID != "task-1" {
		t.Fatalf("canceled task id = %q, want task-1", canceledTaskID)
	}
	if out.Result.Task.ID != "task-1" || out.Result.Task.Status.State != "TASK_STATE_CANCELED" {
		t.Fatalf("cancel result = %+v, want canceled task", out.Result.Task)
	}
}

func TestJSONRPCAgentReturnsRemoteErrors(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("remote failed")
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{}, wantErr
		},
	}
	server := httptest.NewServer(NewJSONRPCHandler(agent))
	defer server.Close()
	remote, err := NewJSONRPCAgent(server.URL, WithJSONRPCClient(server.Client()), WithJSONRPCAgentCard(agent.Card()))
	if err != nil {
		t.Fatalf("NewJSONRPCAgent() error = %v", err)
	}

	_, err = remote.Send(ctx, Task{ID: "task-1", Input: "write tests"})
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("Send() error = %v, want remote failure", err)
	}
}

type testJSONRPCResponse struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      string                 `json:"id"`
	Result  testJSONRPCResult      `json:"result"`
	Error   map[string]interface{} `json:"error"`
}

type testJSONRPCResult struct {
	Task testJSONRPCTask `json:"task"`
}

type testJSONRPCTask struct {
	ID        string                `json:"id"`
	Status    testJSONRPCTaskStatus `json:"status"`
	Artifacts []testJSONRPCArtifact `json:"artifacts"`
}

type testJSONRPCTaskStatus struct {
	State string `json:"state"`
}

type testJSONRPCArtifact struct {
	Parts []testJSONRPCPart `json:"parts"`
}

type testJSONRPCPart struct {
	Text string `json:"text"`
}
