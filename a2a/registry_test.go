package a2a

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestRegistryRegisterCardAndSend(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "planned: " + task.Input}, nil
		},
	}

	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	card, err := registry.Card(ctx, "planner")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Name != "planner" {
		t.Fatalf("Card() = %+v", card)
	}

	result, err := registry.Send(ctx, "planner", Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "run-1"},
		Input: "write tests",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.TaskID != "task-1" || result.Output != "planned: write tests" {
		t.Fatalf("Send() = %+v", result)
	}
}

func TestRegistryCancelTask(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	var canceledTaskID string
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner", Description: "plans tasks"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			canceledTaskID = taskID
			return nil
		},
	}

	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Cancel(ctx, "planner", "task-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceledTaskID != "task-1" {
		t.Fatalf("canceled task ID = %q, want task-1", canceledTaskID)
	}
}

func TestRegistryStreamTaskStatus(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				if !yield(TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Status:   TaskStatusRunning,
					Message:  "working",
					Metadata: map[string]any{"progress": 0.5},
				}, nil) {
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

	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	events, err := collectTaskEvents(registry.Stream(ctx, "planner", Task{
		ID:    "task-1",
		IDs:   gopact.RuntimeIDs{RunID: "run-1"},
		Input: "write tests",
	}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) != 2 ||
		events[0].Status != TaskStatusRunning ||
		events[1].Status != TaskStatusCompleted {
		t.Fatalf("Stream() events = %+v, want running/completed", events)
	}
	if events[0].Metadata["progress"] != 0.5 || events[1].Result.Output != "planned" {
		t.Fatalf("Stream() events = %+v, want status metadata and result", events)
	}
}

func TestRegistryDiscoverCachesAgentCardAndEmitsEvent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	discoverer := DiscovererFunc(func(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
		if query.Name != "planner" || query.URL != "https://agents.example/planner" {
			t.Fatalf("Discover() query = %+v, want planner query", query)
		}
		return DiscoveryResult{
			Card: AgentCard{
				Name:         "planner",
				Description:  "plans tasks",
				URL:          query.URL,
				Capabilities: []string{"planning"},
				Metadata:     map[string]any{"owner": "agents"},
			},
			Metadata: map[string]any{"source": "catalog"},
		}, nil
	})

	result, err := registry.Discover(ctx, discoverer, DiscoveryQuery{
		Name: "planner",
		URL:  "https://agents.example/planner",
		IDs:  gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "planner" || result.Card.Description != "plans tasks" {
		t.Fatalf("Discover() card = %+v, want planner card", result.Card)
	}
	if len(result.Events) != 1 || result.Events[0].Type != gopact.EventA2AAgentCardFetched {
		t.Fatalf("Discover() events = %+v, want card fetched event", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_agent_card_fetched.golden.json", result.Events)
	if result.Events[0].Metadata["agent_name"] != "planner" ||
		result.Events[0].Metadata["agent_url"] != "https://agents.example/planner" ||
		result.Events[0].Metadata["source"] != "catalog" {
		t.Fatalf("Discover() event metadata = %+v, want discovery metadata", result.Events[0].Metadata)
	}

	cached, err := registry.Card(ctx, "planner")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if cached.Name != "planner" || cached.Description != "plans tasks" {
		t.Fatalf("Card() = %+v, want cached discovered card", cached)
	}

	result.Card.Metadata["owner"] = "mutated"
	cachedAgain, err := registry.Card(ctx, "planner")
	if err != nil {
		t.Fatalf("Card() after mutation error = %v", err)
	}
	if cachedAgain.Metadata["owner"] != "agents" {
		t.Fatalf("cached card metadata = %+v, want defensive copy", cachedAgain.Metadata)
	}
}

func TestRegistryRejectsInvalidAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	if err := registry.Register(ctx, nil); err == nil {
		t.Fatal("Register() error = nil, want nil agent error")
	}
	if err := registry.Register(ctx, FakeAgent{}); err == nil {
		t.Fatal("Register() error = nil, want missing name error")
	}
}

func TestRegistryRejectsDuplicateAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{CardValue: AgentCard{Name: "planner"}}

	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	err := registry.Register(ctx, agent)
	if !errors.Is(err, ErrAgentExists) {
		t.Fatalf("Register() error = %v, want %v", err, ErrAgentExists)
	}
}

func TestRegistrySendRejectsMissingAgent(t *testing.T) {
	_, err := NewRegistry().Send(context.Background(), "missing", Task{})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Send() error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestRegistryCancelRejectsMissingAgent(t *testing.T) {
	err := NewRegistry().Cancel(context.Background(), "missing", "task-1")
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Cancel() error = %v, want %v", err, ErrAgentNotFound)
	}
}

func collectTaskEvents(seq iter.Seq2[TaskEvent, error]) ([]TaskEvent, error) {
	var events []TaskEvent
	for event, err := range seq {
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}
