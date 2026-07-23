// Package gopacttest provides reusable protocol conformance helpers for gopact implementations.
// Application task-quality evaluation remains outside the runtime and this package.
package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/agent"
	"github.com/gopact-ai/gopact/workflow"
)

const (
	defaultConformanceConcurrentCalls = 4
	maxConformanceConcurrentCalls     = 32
	minimumRunEventCount              = 2
	conformanceTimeout                = 10 * time.Second
	conformanceSessionID              = "conformance-session"
)

// AgentConformanceCase configures reusable public Agent contract checks.
// Validate is a deterministic test assertion for Request, not a task-quality evaluator.
type AgentConformanceCase struct {
	Agent           agent.Agent
	Request         agent.Request
	Validate        func(agent.Response) error
	ConcurrentCalls int
}

// RequireAgentConformance verifies the minimal ADK Agent contract shared by
// direct and Workflow-backed implementations.
func RequireAgentConformance(t *testing.T, testCase AgentConformanceCase) {
	t.Helper()
	requireAgentConformance(t, testCase, false)
}

// RequireWorkflowAgentConformance verifies the minimal Agent contract plus
// Workflow lifecycle, lineage, and run-extension semantics.
func RequireWorkflowAgentConformance(t *testing.T, testCase AgentConformanceCase) {
	t.Helper()
	requireAgentConformance(t, testCase, true)
}

func requireAgentConformance(t *testing.T, testCase AgentConformanceCase, withWorkflow bool) {
	t.Helper()
	if isNilConformanceValue(testCase.Agent) {
		t.Fatal("agent is nil")
	}
	if testCase.Validate == nil {
		t.Fatal("response validator is nil")
	}
	identity := testCase.Agent.Identity()
	if identity.Name == "" || identity.Description == "" || identity.Version == "" {
		t.Fatalf("Identity() = %+v, want non-empty immutable identity", identity)
	}
	if next := testCase.Agent.Identity(); next != identity {
		t.Fatalf("Identity() changed from %+v to %+v", identity, next)
	}

	request := cloneAgentRequest(testCase.Request)
	requestBefore := cloneAgentRequest(request)
	var events []gopact.Event
	var eventsMu sync.Mutex
	options := []gopact.RunOption{
		gopact.WithSessionID(conformanceSessionID),
		gopact.WithRunID("conformance-lifecycle"),
	}
	if withWorkflow {
		options = append(options, gopact.WithEventHandler(func(_ context.Context, event gopact.Event) error {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			events = append(events, event)
			return nil
		}))
	}
	response, err := testCase.Agent.Invoke(context.Background(), request, options...)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if err := testCase.Validate(response); err != nil {
		t.Fatalf("response validation: %v", err)
	}
	if !reflect.DeepEqual(request, requestBefore) {
		t.Fatalf("Invoke() mutated request: before=%+v after=%+v", requestBefore, request)
	}
	if withWorkflow {
		eventsMu.Lock()
		eventSnapshot := append([]gopact.Event(nil), events...)
		eventsMu.Unlock()
		requireAgentEvents(t, identity, "conformance-lifecycle", eventSnapshot)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testCase.Agent.Invoke(canceled, cloneAgentRequest(testCase.Request),
		gopact.WithRunID("conformance-canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Invoke() error = %v, want context.Canceled", err)
	}

	if withWorkflow {
		if _, err := testCase.Agent.Invoke(
			context.Background(),
			cloneAgentRequest(testCase.Request),
			conformanceUnknownRunOption{},
		); err == nil {
			t.Fatal("Invoke() accepted an unknown run extension")
		}
	}

	concurrentCalls := testCase.ConcurrentCalls
	if concurrentCalls == 0 {
		concurrentCalls = defaultConformanceConcurrentCalls
	}
	if concurrentCalls < 0 || concurrentCalls > maxConformanceConcurrentCalls {
		t.Fatalf("ConcurrentCalls = %d, want 0..%d", concurrentCalls, maxConformanceConcurrentCalls)
	}
	type invocationResult struct {
		response agent.Response
		err      error
	}
	results := make(chan invocationResult, concurrentCalls)
	ctx, stop := context.WithTimeout(context.Background(), conformanceTimeout)
	defer stop()
	for index := range concurrentCalls {
		go func() {
			response, err := testCase.Agent.Invoke(
				ctx,
				cloneAgentRequest(testCase.Request),
				gopact.WithRunID(fmt.Sprintf("conformance-concurrent-%d", index)),
			)
			results <- invocationResult{response: response, err: err}
		}()
	}
	for range concurrentCalls {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent Invoke() error = %v", result.err)
		}
		if err := testCase.Validate(result.response); err != nil {
			t.Fatalf("concurrent response validation: %v", err)
		}
	}
	if next := testCase.Agent.Identity(); next != identity {
		t.Fatalf("Identity() changed after concurrent Invoke: %+v", next)
	}
}

func requireAgentEvents(t *testing.T, identity agent.Identity, runID string, events []gopact.Event) {
	t.Helper()
	if len(events) < minimumRunEventCount {
		t.Fatalf("events = %+v, want started and terminal", events)
	}
	type runFacts struct {
		sessionID    string
		parentRunID  string
		source       string
		definitionID string
		sequence     int64
		terminal     bool
	}
	runs := make(map[string]runFacts)
	var rootEvents []gopact.Event
	for index, event := range events {
		if event.SessionID != conformanceSessionID || event.RunID == "" || event.Sequence <= 0 ||
			event.Source == "" || event.Timestamp.IsZero() || event.Timestamp.Location() != time.UTC {
			t.Fatalf("event[%d] = %+v, want complete event identity", index, event)
		}
		facts := runs[event.RunID]
		if facts.sequence == 0 {
			facts.sessionID = event.SessionID
			facts.parentRunID = event.ParentRunID
			facts.source = event.Source
			facts.definitionID = event.DefinitionID
		}
		if facts.terminal || event.SessionID != facts.sessionID ||
			event.ParentRunID != facts.parentRunID ||
			(facts.definitionID == "" && event.Source != facts.source) ||
			(facts.definitionID != "" && event.DefinitionID != facts.definitionID) ||
			event.Sequence != facts.sequence+1 {
			t.Fatalf("event[%d] = %+v, want stable per-run lineage and sequence", index, event)
		}
		facts.sequence = event.Sequence
		facts.terminal = isAgentTerminalEvent(event.Type)
		runs[event.RunID] = facts

		if event.RunID == runID {
			requireRootAgentEvent(t, identity, runID, index, event)
			rootEvents = append(rootEvents, event)
			continue
		}
		if event.ParentRunID == "" {
			t.Fatalf("event[%d] = %+v, want nested lineage under root %q", index, event, runID)
		}
	}
	if len(rootEvents) < minimumRunEventCount || !isAgentStartEvent(rootEvents[0].Type) ||
		!isAgentCompletedEvent(rootEvents[len(rootEvents)-1].Type) ||
		events[len(events)-1].RunID != runID || !isAgentCompletedEvent(events[len(events)-1].Type) {
		t.Fatalf("events = %+v, want one completed terminal", events)
	}
}

func requireRootAgentEvent(t *testing.T, identity agent.Identity, runID string, index int, event gopact.Event) {
	t.Helper()
	hasWorkflowEnvelope := event.DefinitionID == identity.Name && event.Source != ""
	if event.SessionID != conformanceSessionID || event.ParentRunID != "" || !hasWorkflowEnvelope {
		t.Fatalf("event[%d] = %+v, want accurate root envelope", index, event)
	}
}

func isAgentTerminalEvent(eventType string) bool {
	switch eventType {
	case workflow.EventWorkflowInterrupted, workflow.EventWorkflowCompleted, workflow.EventWorkflowFailed,
		workflow.EventWorkflowCanceled, workflow.EventWorkflowTerminated:
		return true
	default:
		return false
	}
}

func isAgentStartEvent(eventType string) bool {
	return eventType == workflow.EventWorkflowStarted
}

func isAgentCompletedEvent(eventType string) bool {
	return eventType == workflow.EventWorkflowCompleted
}

type conformanceUnknownRunOption struct{}

func (conformanceUnknownRunOption) ApplyRunOption(config *gopact.RunConfig) {
	if config.Extensions == nil {
		config.Extensions = make(map[string]any)
	}
	config.Extensions["gopacttest.unknown"] = true
}

func cloneAgentRequest(request agent.Request) agent.Request {
	request.Messages = cloneConformanceMessages(request.Messages)
	request.Artifacts = append([]gopact.ArtifactRef(nil), request.Artifacts...)
	if request.Metadata != nil {
		metadata := make(map[string]string, len(request.Metadata))
		for key, value := range request.Metadata {
			metadata[key] = value
		}
		request.Metadata = metadata
	}
	return request
}

func cloneConformanceMessages(messages []gopact.Message) []gopact.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]gopact.Message, len(messages))
	for index, message := range messages {
		cloned[index] = cloneConformanceMessage(message)
	}
	return cloned
}

func cloneConformanceMessage(message gopact.Message) gopact.Message {
	return message.Clone()
}

func isNilConformanceValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
