package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestAgentCardJSONContractIncludesMeshFields(t *testing.T) {
	card := AgentCard{
		Name: "planner",
		URL:  "https://agents.example/planner",
		Protocols: []ProtocolBinding{{
			Name:      "a2a-http",
			Transport: "http+json",
			URL:       "https://agents.example/planner",
		}},
		Skills: []AgentSkill{{
			Name:         "plan",
			Description:  "plans tasks",
			InputSchema:  gopact.JSONSchema{"type": "object"},
			OutputSchema: gopact.JSONSchema{"type": "object"},
		}},
		InputSchema:  gopact.JSONSchema{"type": "object"},
		OutputSchema: gopact.JSONSchema{"type": "object"},
		Streaming:    true,
		Artifacts:    true,
		Auth: &AuthRequirement{
			Required: true,
			Schemes:  []string{"bearer"},
			Scopes:   []string{"task:send"},
		},
		Owner:   "agents",
		Version: "v0.1.0",
		Health: &HealthHints{
			HealthPath:    "/healthz",
			ReadinessPath: "/readyz",
		},
	}

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal(AgentCard) error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("Unmarshal fields error = %v", err)
	}
	for _, field := range []string{
		"protocols",
		"skills",
		"input_schema",
		"output_schema",
		"streaming",
		"artifacts",
		"auth",
		"owner",
		"version",
		"health",
	} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("AgentCard JSON missing %q: %s", field, raw)
		}
	}

	var roundTrip AgentCard
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("Unmarshal(AgentCard) error = %v", err)
	}
	if roundTrip.Protocols[0].Name != "a2a-http" ||
		roundTrip.Skills[0].Name != "plan" ||
		roundTrip.InputSchema["type"] != "object" ||
		roundTrip.OutputSchema["type"] != "object" ||
		!roundTrip.Streaming ||
		!roundTrip.Artifacts ||
		roundTrip.Auth == nil ||
		!roundTrip.Auth.Required ||
		roundTrip.Auth.Schemes[0] != "bearer" ||
		roundTrip.Owner != "agents" ||
		roundTrip.Version != "v0.1.0" ||
		roundTrip.Health == nil ||
		roundTrip.Health.ReadinessPath != "/readyz" {
		t.Fatalf("AgentCard round-trip = %+v, want mesh fields preserved", roundTrip)
	}
}

func TestRegistryCardReturnsDefensiveCopiesForAgentCardContract(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{
		CardValue: AgentCard{
			Name:      "planner",
			Protocols: []ProtocolBinding{{Name: "a2a-http", URL: "https://agents.example/planner"}},
			Skills: []AgentSkill{{
				Name:        "plan",
				InputSchema: gopact.JSONSchema{"type": "object"},
			}},
			InputSchema: gopact.JSONSchema{
				"type":       "object",
				"properties": map[string]any{"input": map[string]any{"type": "string"}},
			},
			Auth: &AuthRequirement{
				Schemes: []string{"bearer"},
				Scopes:  []string{"task:send"},
			},
		},
	}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	card, err := registry.Card(ctx, "planner")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	card.Protocols[0].URL = "mutated"
	card.Skills[0].InputSchema["type"] = "mutated"
	card.InputSchema["type"] = "mutated"
	card.Auth.Schemes[0] = "mutated"
	card.Auth.Scopes[0] = "mutated"

	card, err = registry.Card(ctx, "planner")
	if err != nil {
		t.Fatalf("Card() after mutation error = %v", err)
	}
	if card.Protocols[0].URL != "https://agents.example/planner" ||
		card.Skills[0].InputSchema["type"] != "object" ||
		card.InputSchema["type"] != "object" ||
		card.Auth.Schemes[0] != "bearer" ||
		card.Auth.Scopes[0] != "task:send" {
		t.Fatalf("Card() = %+v, want defensive copy of mesh fields", card)
	}
}

func TestRegistryListCardsReturnsOrderedDefensiveCopies(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	if err := registry.Register(ctx, FakeAgent{CardValue: AgentCard{
		Name:         "planner",
		Capabilities: []string{"planning"},
		Metadata:     map[string]any{"owner": "agents"},
	}}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	_, err := registry.Discover(ctx, DiscovererFunc(func(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
		return DiscoveryResult{
			Card: AgentCard{
				Name:         "reviewer",
				Capabilities: []string{"code.review"},
				Metadata:     map[string]any{"owner": "review"},
			},
		}, nil
	}), DiscoveryQuery{Name: "reviewer"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	cards, err := registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "planner" || cards[1].Name != "reviewer" {
		t.Fatalf("ListCards() = %+v, want registration/discovery order", cards)
	}
	cards[0].Capabilities[0] = "mutated"
	cards[0].Metadata["owner"] = "mutated"
	cards[1].Capabilities[0] = "mutated"
	cards[1].Metadata["owner"] = "mutated"

	cards, err = registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() after mutation error = %v", err)
	}
	if cards[0].Capabilities[0] != "planning" ||
		cards[0].Metadata["owner"] != "agents" ||
		cards[1].Capabilities[0] != "code.review" ||
		cards[1].Metadata["owner"] != "review" {
		t.Fatalf("ListCards() = %+v, want defensive copies", cards)
	}
}

func TestRegistryImportCardsStoresOrderedDefensiveCopies(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	lister := NewStaticDiscoverer(
		AgentCard{
			Name:         "planner",
			Capabilities: []string{"planning"},
			Metadata:     map[string]any{"owner": "agents"},
		},
		AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"owner": "review"},
		},
	)

	imported, err := registry.ImportCards(ctx, lister)
	if err != nil {
		t.Fatalf("ImportCards() error = %v", err)
	}
	if len(imported) != 2 || imported[0].Name != "planner" || imported[1].Name != "reviewer" {
		t.Fatalf("ImportCards() = %+v, want lister order", imported)
	}
	imported[0].Capabilities[0] = "mutated"
	imported[0].Metadata["owner"] = "mutated"

	cards, err := registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 2 ||
		cards[0].Name != "planner" ||
		cards[0].Capabilities[0] != "planning" ||
		cards[0].Metadata["owner"] != "agents" ||
		cards[1].Name != "reviewer" {
		t.Fatalf("ListCards() = %+v, want imported defensive copies", cards)
	}
}

func TestRegistryImportCardsRejectsMissingNameWithoutPartialImport(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	lister := cardListerFunc(func(ctx context.Context) ([]AgentCard, error) {
		return []AgentCard{
			{Name: "planner"},
			{URL: "http://127.0.0.1:8082"},
		}, nil
	})

	if _, err := registry.ImportCards(ctx, lister); !errors.Is(err, ErrCardNameRequired) {
		t.Fatalf("ImportCards() error = %v, want %v", err, ErrCardNameRequired)
	}
	cards, err := registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("ListCards() = %+v, want no partial import", cards)
	}
}

func TestRegistryBootstrapImportsMultipleSourcesWithEvidence(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	result, err := registry.Bootstrap(ctx, gopact.RuntimeIDs{RunID: "run-1"},
		NewStaticDiscoverer(AgentCard{
			Name:         "planner",
			URL:          "http://127.0.0.1:8081",
			Capabilities: []string{"planning"},
		}),
		NewStaticDiscoverer(AgentCard{
			Name:         "reviewer",
			URL:          "http://127.0.0.1:8082",
			Capabilities: []string{"code.review"},
		}),
	)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(result.Cards) != 2 || result.Cards[0].Name != "planner" || result.Cards[1].Name != "reviewer" {
		t.Fatalf("Bootstrap() cards = %+v, want source order", result.Cards)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventA2AAgentCardFetched ||
		result.Events[0].IDs.RunID != "run-1" ||
		result.Events[0].Metadata["agent_name"] != "planner" ||
		result.Events[0].Metadata["source_index"] != 0 ||
		result.Events[0].Metadata["source_card_index"] != 0 ||
		result.Events[1].Type != gopact.EventA2AAgentCardFetched ||
		result.Events[1].Metadata["agent_name"] != "reviewer" ||
		result.Events[1].Metadata["source_index"] != 1 ||
		result.Events[1].Metadata["source_card_index"] != 0 {
		t.Fatalf("Bootstrap() events = %+v, want fetched evidence", result.Events)
	}

	result.Cards[0].Capabilities[0] = "mutated"
	cards, err := registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 2 || cards[0].Capabilities[0] != "planning" || cards[1].Name != "reviewer" {
		t.Fatalf("ListCards() = %+v, want bootstrapped defensive copies", cards)
	}
}

func TestRegistryBootstrapRejectsInvalidSourceWithoutPartialImport(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	_, err := registry.Bootstrap(ctx, gopact.RuntimeIDs{},
		NewStaticDiscoverer(AgentCard{Name: "planner"}),
		NewStaticDiscoverer(AgentCard{URL: "http://127.0.0.1:8082"}),
	)
	if !errors.Is(err, ErrCardNameRequired) {
		t.Fatalf("Bootstrap() error = %v, want %v", err, ErrCardNameRequired)
	}
	cards, err := registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("ListCards() = %+v, want no partial import", cards)
	}
}

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
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventA2ATaskSent ||
		result.Events[1].Type != gopact.EventA2ATaskCompleted {
		t.Fatalf("Send() events = %+v, want sent/completed evidence", result.Events)
	}
	if result.Events[0].Message == nil || result.Events[0].Message.Text() != "write tests" {
		t.Fatalf("sent event message = %+v, want task input", result.Events[0].Message)
	}
	if result.Events[1].Result == nil || result.Events[1].Result.Content != "planned: write tests" {
		t.Fatalf("completed event result = %+v, want task output", result.Events[1].Result)
	}
}

func TestRegistryRegisterWithEvidenceReturnsAgentRegisteredEvent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{CardValue: AgentCard{
		Name:         "planner",
		Description:  "plans tasks",
		URL:          "http://127.0.0.1:8081",
		Capabilities: []string{"planning"},
		Metadata:     map[string]any{"owner": "agents"},
	}}

	result, err := registry.RegisterWithEvidence(ctx, agent, gopact.RuntimeIDs{RunID: "run-1"})
	if err != nil {
		t.Fatalf("RegisterWithEvidence() error = %v", err)
	}
	if result.Card.Name != "planner" ||
		len(result.Events) != 1 ||
		result.Events[0].Type != gopact.EventA2AAgentRegistered ||
		result.Events[0].IDs.RunID != "run-1" ||
		result.Events[0].Metadata["agent_name"] != "planner" ||
		result.Events[0].Metadata["agent_url"] != "http://127.0.0.1:8081" ||
		result.Events[0].Metadata["capability_count"] != 1 ||
		result.Events[0].Metadata["owner"] != "agents" {
		t.Fatalf("RegisterWithEvidence() = %+v, want registration evidence", result)
	}

	result.Card.Metadata["owner"] = "mutated"
	card, err := registry.Card(ctx, "planner")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Metadata["owner"] != "agents" {
		t.Fatalf("Card() metadata = %+v, want defensive copy", card.Metadata)
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

func TestRegistrySendPolicyDenyDoesNotEmitSentEvidence(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent, err := NewPolicyAgent(
		FakeAgent{CardValue: AgentCard{Name: "reviewer"}},
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "blocked"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyAgent() error = %v", err)
	}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Send(ctx, "reviewer", Task{ID: "task-1", Input: "diff"})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Send() error = %v, want ErrPolicyDenied", err)
	}
	if got := eventTypes(result.Events); containsEventType(got, gopact.EventA2ATaskSent) {
		t.Fatalf("Send() events = %v, want no sent evidence before local policy allow", got)
	} else if !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	}) {
		t.Fatalf("Send() events = %v, want policy evidence then failed event", got)
	}
}

func TestRegistrySendPolicyAllowEmitsPolicyBeforeSentEvidence(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent, err := NewPolicyAgent(
		FakeAgent{
			CardValue: AgentCard{Name: "reviewer"},
			SendFunc: func(ctx context.Context, task Task) (Result, error) {
				return Result{TaskID: task.ID, Output: "reviewed"}, nil
			},
		},
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyAgent() error = %v", err)
	}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Send(ctx, "reviewer", Task{ID: "task-1", Input: "diff"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := eventTypes(result.Events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	}) {
		t.Fatalf("Send() events = %v, want policy evidence before sent/completed", got)
	}
}

func TestRegistryCancelWithEvidenceReturnsCanceledEvent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{CardValue: AgentCard{Name: "planner", URL: "http://127.0.0.1:8081"}}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.CancelWithEvidence(ctx, "planner", "task-1", gopact.RuntimeIDs{RunID: "run-1"})
	if err != nil {
		t.Fatalf("CancelWithEvidence() error = %v", err)
	}
	if result.TaskID != "task-1" ||
		len(result.Events) != 1 ||
		result.Events[0].Type != gopact.EventA2ATaskCanceled ||
		result.Events[0].IDs.RunID != "run-1" ||
		result.Events[0].Metadata["agent_name"] != "planner" ||
		result.Events[0].Metadata["agent_url"] != "http://127.0.0.1:8081" ||
		result.Events[0].Metadata["a2a_task_id"] != "task-1" ||
		result.Events[0].Metadata["a2a_status"] != string(TaskStatusCanceled) {
		t.Fatalf("CancelWithEvidence() = %+v, want canceled evidence", result)
	}
}

func TestRegistryCancelWithEvidenceReturnsFailedEventOnError(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	wantErr := errors.New("cancel failed")
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			return wantErr
		},
	}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.CancelWithEvidence(ctx, "planner", "task-1", gopact.RuntimeIDs{RunID: "run-1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("CancelWithEvidence() error = %v, want %v", err, wantErr)
	}
	if result.TaskID != "task-1" ||
		len(result.Events) != 1 ||
		result.Events[0].Type != gopact.EventA2ATaskFailed ||
		result.Events[0].Err == nil ||
		result.Events[0].Metadata["agent_name"] != "planner" ||
		result.Events[0].Metadata["a2a_task_id"] != "task-1" ||
		result.Events[0].Metadata["a2a_status"] != string(TaskStatusFailed) {
		t.Fatalf("CancelWithEvidence() = %+v, want failed evidence", result)
	}
}

func TestRegistrySendOrdersAgentEventsBetweenSentAndCompleted(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{
		CardValue: AgentCard{Name: "planner"},
		SendFunc: func(context.Context, Task) (Result, error) {
			return Result{
				TaskID: "task-1",
				Output: "planned",
				Events: []gopact.Event{{
					Type: gopact.EventA2AMessageReceived,
					IDs:  gopact.RuntimeIDs{RunID: "child-run"},
				}},
			}, nil
		},
	}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Send(ctx, "planner", Task{ID: "task-1", IDs: gopact.RuntimeIDs{RunID: "run-1"}})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := []gopact.EventType{result.Events[0].Type, result.Events[1].Type, result.Events[2].Type}
	want := []gopact.EventType{
		gopact.EventA2ATaskSent,
		gopact.EventA2AMessageReceived,
		gopact.EventA2ATaskCompleted,
	}
	if len(result.Events) != 3 || !reflect.DeepEqual(got, want) {
		t.Fatalf("Send() event types = %v, want %v", got, want)
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

func TestTaskSentEventMapsEvidence(t *testing.T) {
	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", CallID: "call-1"}
	task := Task{
		ID:    "task-1",
		IDs:   ids,
		Input: "review this diff",
		Auth: &Auth{
			Scheme:        "bearer",
			Principal:     "svc-planner",
			CredentialRef: "secret://a2a/planner",
		},
		Metadata: map[string]any{"priority": "high"},
	}
	card := AgentCard{Name: "planner", URL: "https://agents.example/planner"}

	event := task.SentEvent(card)

	if event.Type != gopact.EventA2ATaskSent {
		t.Fatalf("SentEvent() type = %s, want %s", event.Type, gopact.EventA2ATaskSent)
	}
	if event.IDs.RunID != ids.RunID || event.IDs.ThreadID != ids.ThreadID || event.IDs.CallID != ids.CallID {
		t.Fatalf("SentEvent() ids = %+v, want %+v", event.IDs, ids)
	}
	if event.Message == nil || event.Message.Role != gopact.RoleUser || event.Message.Text() != task.Input {
		t.Fatalf("SentEvent() message = %+v, want user task input", event.Message)
	}
	if event.Metadata["agent_name"] != card.Name ||
		event.Metadata["agent_url"] != card.URL ||
		event.Metadata["a2a_task_id"] != task.ID ||
		event.Metadata["priority"] != "high" {
		t.Fatalf("SentEvent() metadata = %+v, want task evidence metadata", event.Metadata)
	}
	if _, ok := event.Metadata["auth"]; ok {
		t.Fatalf("SentEvent() metadata = %+v, want no auth metadata", event.Metadata)
	}
}

func TestTaskEventRuntimeEventMapsEvidence(t *testing.T) {
	ids := gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", CallID: "call-1"}
	card := AgentCard{Name: "planner", URL: "https://agents.example/planner"}
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	wantErr := errors.New("remote failed")
	tests := []struct {
		name              string
		event             TaskEvent
		wantType          gopact.EventType
		wantMessage       string
		wantResult        string
		wantArtifactCount int
		wantErr           error
	}{
		{
			name: "running status",
			event: TaskEvent{
				TaskID:   "task-1",
				IDs:      ids,
				Status:   TaskStatusRunning,
				Message:  "working",
				Metadata: map[string]any{"progress": 0.5},
			},
			wantType:    gopact.EventA2ATaskStatusUpdated,
			wantMessage: "working",
		},
		{
			name: "message update",
			event: TaskEvent{
				TaskID:   "task-1",
				IDs:      ids,
				Message:  "outline ready",
				Metadata: map[string]any{"phase": "outline"},
			},
			wantType:    gopact.EventA2AMessageReceived,
			wantMessage: "outline ready",
		},
		{
			name: "completed result",
			event: TaskEvent{
				TaskID:    "task-1",
				IDs:       ids,
				Status:    TaskStatusCompleted,
				Artifacts: []gopact.ArtifactRef{artifact},
				Result: &Result{
					TaskID:    "task-1",
					Output:    "planned",
					Artifacts: []gopact.ArtifactRef{artifact},
					Metadata:  map[string]any{"quality": "checked"},
				},
			},
			wantType:          gopact.EventA2ATaskCompleted,
			wantResult:        "planned",
			wantArtifactCount: 1,
		},
		{
			name: "failed task",
			event: TaskEvent{
				TaskID: "task-1",
				IDs:    ids,
				Err:    wantErr,
			},
			wantType: gopact.EventA2ATaskFailed,
			wantErr:  wantErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := tt.event.RuntimeEvent(card)

			if event.Type != tt.wantType {
				t.Fatalf("RuntimeEvent() type = %s, want %s", event.Type, tt.wantType)
			}
			if event.IDs.RunID != ids.RunID || event.IDs.ThreadID != ids.ThreadID || event.IDs.CallID != ids.CallID {
				t.Fatalf("RuntimeEvent() ids = %+v, want %+v", event.IDs, ids)
			}
			if event.Metadata["agent_name"] != card.Name ||
				event.Metadata["agent_url"] != card.URL ||
				event.Metadata["a2a_task_id"] != "task-1" {
				t.Fatalf("RuntimeEvent() metadata = %+v, want agent and task metadata", event.Metadata)
			}
			if tt.event.Status != "" && event.Metadata["a2a_status"] != string(tt.event.Status) {
				t.Fatalf("RuntimeEvent() metadata = %+v, want status %s", event.Metadata, tt.event.Status)
			}
			if tt.wantMessage != "" {
				if event.Message == nil || event.Message.Text() != tt.wantMessage {
					t.Fatalf("RuntimeEvent() message = %+v, want %q", event.Message, tt.wantMessage)
				}
				if event.Metadata["a2a_message"] != tt.wantMessage {
					t.Fatalf("RuntimeEvent() metadata = %+v, want message %q", event.Metadata, tt.wantMessage)
				}
			}
			if tt.wantResult != "" {
				if event.Result == nil || event.Result.Content != tt.wantResult {
					t.Fatalf("RuntimeEvent() result = %+v, want %q", event.Result, tt.wantResult)
				}
				if event.Result.Metadata["quality"] != "checked" {
					t.Fatalf("RuntimeEvent() result metadata = %+v, want copied result metadata", event.Result.Metadata)
				}
			}
			if len(event.Artifacts) != tt.wantArtifactCount {
				t.Fatalf("RuntimeEvent() artifacts = %+v, want %d", event.Artifacts, tt.wantArtifactCount)
			}
			if !errors.Is(event.Err, tt.wantErr) {
				t.Fatalf("RuntimeEvent() error = %v, want %v", event.Err, tt.wantErr)
			}
		})
	}
}

func TestRegistryRouteByCapability(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	researcher := FakeAgent{
		CardValue: AgentCard{
			Name:         "researcher",
			Capabilities: []string{"research.web"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "researched: " + task.Input}, nil
		},
	}
	reviewer := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewed: " + task.Input}, nil
		},
	}

	if err := registry.Register(ctx, researcher); err != nil {
		t.Fatalf("Register(researcher) error = %v", err)
	}
	if err := registry.Register(ctx, reviewer); err != nil {
		t.Fatalf("Register(reviewer) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task: Task{
			ID:    "task-1",
			Input: "diff",
		},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "reviewed: diff" {
		t.Fatalf("Route() result = %+v, want reviewer output", result)
	}

	_, err = registry.Route(ctx, RouteQuery{
		Require: []string{"security.audit"},
		Task:    Task{ID: "task-2"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Route() missing capability error = %v, want ErrAgentNotFound", err)
	}
}

func TestRegistryRouteByCapabilityUsesRegistrationOrder(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	first := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-a",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "first"}, nil
		},
	}
	second := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-b",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "second"}, nil
		},
	}

	if err := registry.Register(ctx, first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	for i := 0; i < 20; i++ {
		result, err := registry.Route(ctx, RouteQuery{
			Require: []string{"code.review"},
			Task:    Task{ID: "task-1"},
		})
		if err != nil {
			t.Fatalf("Route(%d) error = %v", i, err)
		}
		if result.Output != "first" {
			t.Fatalf("Route(%d) output = %q, want first", i, result.Output)
		}
	}
}

func TestRegistryRouteFallbackTriesNextMatchingAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	wantErr := errors.New("first failed")
	first := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-a",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "partial"}, wantErr
		},
	}
	second := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-b",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "second"}, nil
		},
	}
	if err := registry.Register(ctx, first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Require:  []string{"code.review"},
		Fallback: true,
		Task:     Task{ID: "task-1", Input: "diff"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "second" {
		t.Fatalf("Route() output = %q, want second", result.Output)
	}
	gotTypes := make([]gopact.EventType, 0, len(result.Events))
	gotAgents := make([]any, 0, len(result.Events))
	for _, event := range result.Events {
		gotTypes = append(gotTypes, event.Type)
		gotAgents = append(gotAgents, event.Metadata["agent_name"])
	}
	wantTypes := []gopact.EventType{
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskFailed,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("Route() event types = %v, want %v", gotTypes, wantTypes)
	}
	if gotAgents[0] != "reviewer-a" ||
		gotAgents[1] != "reviewer-a" ||
		gotAgents[2] != "reviewer-b" ||
		gotAgents[3] != "reviewer-b" {
		t.Fatalf("Route() event agents = %v, want fallback evidence", gotAgents)
	}
}

func TestRegistryRouteFallbackStopsOnPolicyDeny(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	first, err := NewPolicyAgent(
		FakeAgent{
			CardValue: AgentCard{
				Name:         "reviewer-a",
				Capabilities: []string{"code.review"},
			},
		},
		gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "blocked"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyAgent() error = %v", err)
	}
	var secondCalled bool
	second := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-b",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			secondCalled = true
			return Result{TaskID: task.ID, Output: "second"}, nil
		},
	}
	if err := registry.Register(ctx, first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Require:  []string{"code.review"},
		Fallback: true,
		Task:     Task{ID: "task-1", Input: "diff"},
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Route() error = %v, want ErrPolicyDenied", err)
	}
	if secondCalled {
		t.Fatalf("Route() called fallback agent after local policy deny")
	}
	if got := eventTypes(result.Events); !reflect.DeepEqual(got, []gopact.EventType{
		gopact.EventPolicyRequested,
		gopact.EventPolicyDecided,
		gopact.EventA2ATaskFailed,
	}) {
		t.Fatalf("Route() events = %v, want policy evidence then failed event", got)
	}
}

func TestRegistryRouteWithoutFallbackReturnsFirstMatchingError(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	wantErr := errors.New("first failed")
	first := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-a",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID}, wantErr
		},
	}
	second := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-b",
			Capabilities: []string{"code.review"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "second"}, nil
		},
	}
	if err := registry.Register(ctx, first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task:    Task{ID: "task-1"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Route() error = %v, want first error", err)
	}
	if result.Output == "second" {
		t.Fatalf("Route() result = %+v, want no fallback by default", result)
	}
}

func TestRegistryRouteByMetadata(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	researcher := FakeAgent{
		CardValue: AgentCard{
			Name:     "researcher",
			Metadata: map[string]any{"domain": "research", "tier": "gold"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "researcher"}, nil
		},
	}
	reviewer := FakeAgent{
		CardValue: AgentCard{
			Name:     "reviewer",
			Metadata: map[string]any{"domain": "code", "tier": "gold"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewer"}, nil
		},
	}

	if err := registry.Register(ctx, researcher); err != nil {
		t.Fatalf("Register(researcher) error = %v", err)
	}
	if err := registry.Register(ctx, reviewer); err != nil {
		t.Fatalf("Register(reviewer) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Metadata: map[string]any{"domain": "code"},
		Task:     Task{ID: "task-1"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "reviewer" {
		t.Fatalf("Route() output = %q, want reviewer", result.Output)
	}

	_, err = registry.Route(ctx, RouteQuery{
		Metadata: map[string]any{"domain": "security"},
		Task:     Task{ID: "task-2"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Route() missing metadata error = %v, want ErrAgentNotFound", err)
	}
}

func TestRegistryRouteByTags(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	researcher := FakeAgent{
		CardValue: AgentCard{Name: "researcher", Tags: []string{"research", "local"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "researcher"}, nil
		},
	}
	reviewer := FakeAgent{
		CardValue: AgentCard{Name: "reviewer", Tags: []string{"code", "local"}},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "reviewer"}, nil
		},
	}

	if err := registry.Register(ctx, researcher); err != nil {
		t.Fatalf("Register(researcher) error = %v", err)
	}
	if err := registry.Register(ctx, reviewer); err != nil {
		t.Fatalf("Register(reviewer) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Tags: []string{"code", "local"},
		Task: Task{ID: "task-1"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "reviewer" {
		t.Fatalf("Route() output = %q, want reviewer", result.Output)
	}

	_, err = registry.Route(ctx, RouteQuery{
		Tags: []string{"security"},
		Task: Task{ID: "task-2"},
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Route() missing tag error = %v, want ErrAgentNotFound", err)
	}
}

func TestRegistryRouteRequiresCapabilitiesAndMetadata(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	first := FakeAgent{
		CardValue: AgentCard{
			Name:         "general-reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "general"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "general"}, nil
		},
	}
	second := FakeAgent{
		CardValue: AgentCard{
			Name:         "security-reviewer",
			Capabilities: []string{"code.review"},
			Metadata:     map[string]any{"domain": "security"},
		},
		SendFunc: func(ctx context.Context, task Task) (Result, error) {
			return Result{TaskID: task.ID, Output: "security"}, nil
		},
	}

	if err := registry.Register(ctx, first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	result, err := registry.Route(ctx, RouteQuery{
		Require:  []string{"code.review"},
		Metadata: map[string]any{"domain": "security"},
		Task:     Task{ID: "task-1"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != "security" {
		t.Fatalf("Route() output = %q, want security", result.Output)
	}
}

func TestRegistryRouteStreamByCapability(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	researcher := FakeAgent{
		CardValue: AgentCard{
			Name:         "researcher",
			Capabilities: []string{"research.web"},
		},
	}
	reviewer := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer",
			Capabilities: []string{"code.review"},
		},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				if !yield(TaskEvent{TaskID: task.ID, IDs: task.IDs, Status: TaskStatusRunning, Message: "reviewing"}, nil) {
					return
				}
				yield(TaskEvent{
					TaskID: task.ID,
					IDs:    task.IDs,
					Status: TaskStatusCompleted,
					Result: &Result{TaskID: task.ID, Output: "reviewed"},
				}, nil)
			}
		},
	}

	if err := registry.Register(ctx, researcher); err != nil {
		t.Fatalf("Register(researcher) error = %v", err)
	}
	if err := registry.Register(ctx, reviewer); err != nil {
		t.Fatalf("Register(reviewer) error = %v", err)
	}

	events, err := collectTaskEvents(registry.RouteStream(ctx, RouteQuery{
		Require: []string{"code.review"},
		Task: Task{
			ID:    "task-1",
			IDs:   gopact.RuntimeIDs{RunID: "run-1"},
			Input: "diff",
		},
	}))
	if err != nil {
		t.Fatalf("RouteStream() error = %v", err)
	}
	if len(events) != 2 ||
		events[0].Status != TaskStatusRunning ||
		events[0].Message != "reviewing" ||
		events[1].Status != TaskStatusCompleted ||
		events[1].Result.Output != "reviewed" {
		t.Fatalf("RouteStream() events = %+v, want routed running/completed", events)
	}
	if events[0].Metadata["agent_name"] != "reviewer" ||
		events[1].Metadata["agent_name"] != "reviewer" {
		t.Fatalf("RouteStream() event metadata = %+v / %+v, want selected agent evidence", events[0].Metadata, events[1].Metadata)
	}

	_, err = collectTaskEvents(registry.RouteStream(ctx, RouteQuery{
		Require: []string{"security.audit"},
		Task:    Task{ID: "task-2"},
	}))
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("RouteStream() missing capability error = %v, want ErrAgentNotFound", err)
	}
}

func TestRegistryRouteStreamFallbackTriesNextMatchingAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	first := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-a",
			Capabilities: []string{"code.review"},
		},
	}
	second := FakeAgent{
		CardValue: AgentCard{
			Name:         "reviewer-b",
			Capabilities: []string{"code.review"},
		},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				yield(TaskEvent{
					TaskID:  task.ID,
					IDs:     task.IDs,
					Message: "streamed by fallback",
				}, nil)
			}
		},
	}
	if err := registry.Register(ctx, first); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	events, err := collectTaskEvents(registry.RouteStream(ctx, RouteQuery{
		Require:  []string{"code.review"},
		Fallback: true,
		Task:     Task{ID: "task-1", IDs: gopact.RuntimeIDs{RunID: "run-1"}},
	}))
	if err != nil {
		t.Fatalf("RouteStream() error = %v", err)
	}
	if len(events) != 1 || events[0].Message != "streamed by fallback" {
		t.Fatalf("RouteStream() events = %+v, want fallback stream", events)
	}
}

func TestRegistryRouteStreamByMetadata(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	researcher := FakeAgent{
		CardValue: AgentCard{
			Name:     "researcher",
			Metadata: map[string]any{"domain": "research"},
		},
	}
	reviewer := FakeAgent{
		CardValue: AgentCard{
			Name:     "reviewer",
			Metadata: map[string]any{"domain": "code"},
		},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				yield(TaskEvent{
					TaskID:  task.ID,
					IDs:     task.IDs,
					Message: "streamed by metadata",
				}, nil)
			}
		},
	}

	if err := registry.Register(ctx, researcher); err != nil {
		t.Fatalf("Register(researcher) error = %v", err)
	}
	if err := registry.Register(ctx, reviewer); err != nil {
		t.Fatalf("Register(reviewer) error = %v", err)
	}

	events, err := collectTaskEvents(registry.RouteStream(ctx, RouteQuery{
		Metadata: map[string]any{"domain": "code"},
		Task: Task{
			ID:  "task-1",
			IDs: gopact.RuntimeIDs{RunID: "run-1"},
		},
	}))
	if err != nil {
		t.Fatalf("RouteStream() error = %v", err)
	}
	if len(events) != 1 || events[0].Message != "streamed by metadata" {
		t.Fatalf("RouteStream() events = %+v, want metadata-routed event", events)
	}
}

func TestRegistryStreamAnnotatesSelectedAgentMetadata(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	agent := FakeAgent{
		CardValue: AgentCard{
			Name: "reviewer",
			URL:  "https://agents.example/reviewer",
		},
		StreamFunc: func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
			return func(yield func(TaskEvent, error) bool) {
				yield(TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Status:   TaskStatusRunning,
					Message:  "reviewing",
					Metadata: map[string]any{"phase": "review"},
				}, nil)
			}
		},
	}
	if err := registry.Register(ctx, agent); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	events, err := collectTaskEvents(registry.Stream(ctx, "reviewer", Task{ID: "task-1"}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) != 1 ||
		events[0].Metadata["agent_name"] != "reviewer" ||
		events[0].Metadata["agent_url"] != "https://agents.example/reviewer" ||
		events[0].Metadata["phase"] != "review" {
		t.Fatalf("Stream() event metadata = %+v, want selected agent evidence and original metadata", events)
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

func TestRegistryDiscoverAcceptsMetadataOnlyQuery(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	discoverer := DiscovererFunc(func(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
		if query.Name != "" || query.URL != "" || query.Metadata["domain"] != "code" {
			t.Fatalf("Discover() query = %+v, want metadata-only query", query)
		}
		return DiscoveryResult{
			Card: AgentCard{
				Name:     "reviewer",
				Metadata: map[string]any{"domain": "code"},
			},
		}, nil
	})

	result, err := registry.Discover(ctx, discoverer, DiscoveryQuery{
		Metadata: map[string]any{"domain": "code"},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Card.Name != "reviewer" {
		t.Fatalf("Discover() = %+v, want reviewer card", result.Card)
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

type cardListerFunc func(ctx context.Context) ([]AgentCard, error)

func (f cardListerFunc) ListCards(ctx context.Context) ([]AgentCard, error) {
	return f(ctx)
}
