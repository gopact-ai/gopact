// Package a2aconformance provides reusable A2A agent contract tests.
package a2aconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact/a2a"
)

// ErrAgentConformanceFailed marks a failed A2A agent conformance case.
var ErrAgentConformanceFailed = errors.New("gopacttest: a2a agent conformance failed")

// AgentConformanceHarness describes one A2A agent implementation under test.
type AgentConformanceHarness struct {
	Agent            a2a.Agent
	Task             a2a.Task
	RequireStreaming bool
}

// AgentConformanceResult is the observed result for one A2A contract case.
type AgentConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckAgentConformance runs reusable A2A agent contract cases for adapters.
func CheckAgentConformance(ctx context.Context, harness AgentConformanceHarness) []AgentConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	task := harness.Task
	if task.ID == "" {
		task.ID = "gopact-a2a-conformance-task"
	}
	if task.Input == "" {
		task.Input = "gopact a2a conformance"
	}

	results := []AgentConformanceResult{
		checkAgentCardName(harness.Agent),
		checkAgentSendCanceledContext(harness.Agent, copyTask(task)),
		checkAgentSendsTask(ctx, harness.Agent, copyTask(task)),
		checkAgentDoesNotMutateTask(ctx, harness.Agent, copyTask(task)),
		checkAgentCancelCanceledContext(harness.Agent, task.ID),
	}
	if harness.RequireStreaming {
		results = append(results,
			checkAgentImplementsStreaming(harness.Agent),
			checkAgentStreamsEvents(ctx, harness.Agent, copyTask(task)),
			checkAgentStreamCanceledContext(harness.Agent, copyTask(task)),
			checkAgentStreamDoesNotMutateTask(ctx, harness.Agent, copyTask(task)),
		)
	}
	return results
}

// RequireAgentConformance fails the test unless agent satisfies the A2A contract.
func RequireAgentConformance(t testing.TB, harness AgentConformanceHarness) {
	t.Helper()

	for _, result := range CheckAgentConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("a2a agent conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkAgentCardName(agent a2a.Agent) AgentConformanceResult {
	if agent == nil {
		return failedAgentConformance("has-card-name", errors.New("agent is nil"))
	}
	if agent.Card().Name == "" {
		return failedAgentConformance("has-card-name", errors.New("agent card name is empty"))
	}
	return passedAgentConformance("has-card-name")
}

func checkAgentSendCanceledContext(agent a2a.Agent, task a2a.Task) AgentConformanceResult {
	if agent == nil {
		return failedAgentConformance("send-respects-canceled-context", errors.New("agent is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := agent.Send(ctx, task)
	if !errors.Is(err, context.Canceled) {
		return failedAgentConformance("send-respects-canceled-context", fmt.Errorf("send canceled context error = %v, want context canceled", err))
	}
	return passedAgentConformance("send-respects-canceled-context")
}

func checkAgentSendsTask(ctx context.Context, agent a2a.Agent, task a2a.Task) AgentConformanceResult {
	if agent == nil {
		return failedAgentConformance("send-returns-task-id", errors.New("agent is nil"))
	}
	result, err := agent.Send(ctx, task)
	if err != nil {
		return failedAgentConformance("send-returns-task-id", err)
	}
	if result.TaskID == "" {
		return failedAgentConformance("send-returns-task-id", errors.New("send result task id is empty"))
	}
	return passedAgentConformance("send-returns-task-id")
}

func checkAgentDoesNotMutateTask(ctx context.Context, agent a2a.Agent, task a2a.Task) AgentConformanceResult {
	if agent == nil {
		return failedAgentConformance("send-does-not-mutate-task", errors.New("agent is nil"))
	}
	before := copyTask(task)
	if _, err := agent.Send(ctx, task); err != nil {
		return failedAgentConformance("send-does-not-mutate-task", err)
	}
	if !reflect.DeepEqual(task, before) {
		return failedAgentConformance("send-does-not-mutate-task", errors.New("agent mutated input task"))
	}
	return passedAgentConformance("send-does-not-mutate-task")
}

func checkAgentCancelCanceledContext(agent a2a.Agent, taskID string) AgentConformanceResult {
	if agent == nil {
		return failedAgentConformance("cancel-respects-canceled-context", errors.New("agent is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := agent.Cancel(ctx, taskID)
	if !errors.Is(err, context.Canceled) {
		return failedAgentConformance("cancel-respects-canceled-context", fmt.Errorf("cancel canceled context error = %v, want context canceled", err))
	}
	return passedAgentConformance("cancel-respects-canceled-context")
}

func checkAgentImplementsStreaming(agent a2a.Agent) AgentConformanceResult {
	if agent == nil {
		return failedAgentConformance("implements-streaming", errors.New("agent is nil"))
	}
	if _, ok := agent.(a2a.StreamingAgent); !ok {
		return failedAgentConformance("implements-streaming", errors.New("agent does not implement StreamingAgent"))
	}
	return passedAgentConformance("implements-streaming")
}

func checkAgentStreamsEvents(ctx context.Context, agent a2a.Agent, task a2a.Task) AgentConformanceResult {
	streamer, ok := agent.(a2a.StreamingAgent)
	if !ok {
		return failedAgentConformance("streams-events", errors.New("agent does not implement StreamingAgent"))
	}
	stream := streamer.Stream(ctx, task)
	if stream == nil {
		return failedAgentConformance("streams-events", errors.New("agent stream is nil"))
	}
	for event, err := range stream {
		if err != nil {
			return failedAgentConformance("streams-events", err)
		}
		if event.TaskID == "" && event.Status == "" && event.Message == "" && event.Result == nil && len(event.Artifacts) == 0 {
			return failedAgentConformance("streams-events", errors.New("stream event is empty"))
		}
		return passedAgentConformance("streams-events")
	}
	return failedAgentConformance("streams-events", errors.New("agent stream ended without events"))
}

func checkAgentStreamCanceledContext(agent a2a.Agent, task a2a.Task) AgentConformanceResult {
	streamer, ok := agent.(a2a.StreamingAgent)
	if !ok {
		return failedAgentConformance("stream-respects-canceled-context", errors.New("agent does not implement StreamingAgent"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := streamer.Stream(ctx, task)
	if stream == nil {
		return failedAgentConformance("stream-respects-canceled-context", errors.New("agent stream is nil"))
	}
	for _, err := range stream {
		if errors.Is(err, context.Canceled) {
			return passedAgentConformance("stream-respects-canceled-context")
		}
		if err != nil {
			return failedAgentConformance("stream-respects-canceled-context", fmt.Errorf("stream canceled context error = %v, want context canceled", err))
		}
		return failedAgentConformance("stream-respects-canceled-context", errors.New("stream yielded an event instead of context canceled"))
	}
	return failedAgentConformance("stream-respects-canceled-context", errors.New("stream ended without context canceled"))
}

func checkAgentStreamDoesNotMutateTask(ctx context.Context, agent a2a.Agent, task a2a.Task) AgentConformanceResult {
	streamer, ok := agent.(a2a.StreamingAgent)
	if !ok {
		return failedAgentConformance("stream-does-not-mutate-task", errors.New("agent does not implement StreamingAgent"))
	}
	before := copyTask(task)
	stream := streamer.Stream(ctx, task)
	if stream == nil {
		return failedAgentConformance("stream-does-not-mutate-task", errors.New("agent stream is nil"))
	}
	for _, err := range stream {
		if err != nil {
			return failedAgentConformance("stream-does-not-mutate-task", err)
		}
		break
	}
	if !reflect.DeepEqual(task, before) {
		return failedAgentConformance("stream-does-not-mutate-task", errors.New("agent mutated input task"))
	}
	return passedAgentConformance("stream-does-not-mutate-task")
}

func passedAgentConformance(name string) AgentConformanceResult {
	return AgentConformanceResult{Case: name, Passed: true}
}

func failedAgentConformance(name string, err error) AgentConformanceResult {
	return AgentConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrAgentConformanceFailed, err),
	}
}

func copyTask(task a2a.Task) a2a.Task {
	out := task
	if task.Auth != nil {
		auth := *task.Auth
		auth.Metadata = copyAnyMap(task.Auth.Metadata)
		out.Auth = &auth
	}
	out.Metadata = copyAnyMap(task.Metadata)
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
