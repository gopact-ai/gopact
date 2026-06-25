package a2a

import (
	"context"
	"errors"
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
		result.Metadata["route"] != "http" {
		t.Fatalf("Send() = %+v, want remote result", result)
	}

	if err := remote.Cancel(ctx, "task-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceledTaskID != "task-1" {
		t.Fatalf("canceled task id = %q, want task-1", canceledTaskID)
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
}
