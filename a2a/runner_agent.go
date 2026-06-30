package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/gopact-ai/gopact"
)

// ErrRunnableRequired is returned when a local runnable-backed A2A agent has no runnable.
var ErrRunnableRequired = errors.New("a2a: runnable is required")

// RunnableInputMapper converts an A2A task into the input passed to a local runnable.
type RunnableInputMapper func(ctx context.Context, task Task) (any, error)

// RunnableResultMapper converts local runtime events into an A2A task result.
type RunnableResultMapper func(ctx context.Context, task Task, events []gopact.Event) (Result, error)

// RunnableAgentOption configures a local runnable exposed as an A2A agent.
type RunnableAgentOption func(*RunnableAgent) error

// RunnableAgent exposes a local gopact runnable behind the A2A Agent contract.
type RunnableAgent struct {
	card         AgentCard
	runnable     gopact.Runnable
	inputMapper  RunnableInputMapper
	resultMapper RunnableResultMapper
}

var (
	_ Agent          = (*RunnableAgent)(nil)
	_ StreamingAgent = (*RunnableAgent)(nil)
)

// NewRunnableAgent creates an A2A agent adapter for a local gopact runnable.
func NewRunnableAgent(card AgentCard, runnable gopact.Runnable, opts ...RunnableAgentOption) (*RunnableAgent, error) {
	if card.Name == "" {
		return nil, ErrCardNameRequired
	}
	if runnable == nil {
		return nil, ErrRunnableRequired
	}
	agent := &RunnableAgent{
		card:         copyAgentCard(card),
		runnable:     runnable,
		inputMapper:  defaultRunnableInputMapper,
		resultMapper: defaultRunnableResultMapper,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(agent); err != nil {
			return nil, err
		}
	}
	if agent.inputMapper == nil {
		return nil, errors.New("a2a: runnable input mapper is required")
	}
	if agent.resultMapper == nil {
		return nil, errors.New("a2a: runnable result mapper is required")
	}
	return agent, nil
}

// WithRunnableInputMapper sets the task-to-runnable input mapper.
func WithRunnableInputMapper(mapper RunnableInputMapper) RunnableAgentOption {
	return func(agent *RunnableAgent) error {
		if mapper == nil {
			return errors.New("a2a: runnable input mapper is required")
		}
		agent.inputMapper = mapper
		return nil
	}
}

// WithRunnableResultMapper sets the runtime-events-to-result mapper.
func WithRunnableResultMapper(mapper RunnableResultMapper) RunnableAgentOption {
	return func(agent *RunnableAgent) error {
		if mapper == nil {
			return errors.New("a2a: runnable result mapper is required")
		}
		agent.resultMapper = mapper
		return nil
	}
}

// Card returns this local agent's A2A card.
func (a *RunnableAgent) Card() AgentCard {
	if a == nil {
		return AgentCard{}
	}
	return copyAgentCard(a.card)
}

// Send runs the local runnable once and aggregates its runtime events into an A2A result.
func (a *RunnableAgent) Send(ctx context.Context, task Task) (Result, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if a == nil || a.runnable == nil {
		return Result{}, ErrRunnableRequired
	}
	ctx, task = contextWithTaskRuntimeIDs(ctx, task)
	input, err := a.inputMapper(ctx, copyTask(task))
	if err != nil {
		return Result{TaskID: taskID(task)}, err
	}
	events, runErr := collectRunnableEvents(a.runnable.Run(ctx, input, gopact.WithRuntimeIDs(task.IDs)), task.IDs)
	result, resultErr := a.resultMapper(ctx, copyTask(task), events)
	result = a.finalizeResult(task, result, len(events))
	if runErr != nil {
		wrapped := fmt.Errorf("a2a: run runnable agent %q: %w", a.card.Name, runErr)
		if resultErr != nil {
			return result, errors.Join(wrapped, resultErr)
		}
		return result, wrapped
	}
	return result, resultErr
}

// Stream runs the local runnable and projects selected runtime events into A2A task events.
func (a *RunnableAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if err := ctx.Err(); err != nil {
			yield(failedTaskEvent(task, err), err)
			return
		}
		if a == nil || a.runnable == nil {
			yield(failedTaskEvent(task, ErrRunnableRequired), ErrRunnableRequired)
			return
		}
		ctx, task = contextWithTaskRuntimeIDs(ctx, task)
		input, err := a.inputMapper(ctx, copyTask(task))
		if err != nil {
			yield(failedTaskEvent(task, err), err)
			return
		}

		var events []gopact.Event
		for event, streamErr := range a.runnable.Run(ctx, input, gopact.WithRuntimeIDs(task.IDs)) {
			event = event.WithRuntimeDefaults(task.IDs)
			if streamErr != nil {
				event.Err = streamErr
			}
			events = append(events, event)

			for _, taskEvent := range runtimeTaskEvents(task, event) {
				if !yield(taskEvent, nil) {
					return
				}
			}

			if streamErr != nil || event.Err != nil || event.Type == gopact.EventRunFailed {
				err := streamErr
				if err == nil {
					err = event.Err
				}
				if err == nil {
					err = errors.New("a2a: runnable agent failed")
				}
				yield(failedTaskEvent(task, err), err)
				return
			}
			if event.Type == gopact.EventRunCompleted {
				result, err := a.resultMapper(ctx, copyTask(task), events)
				result = a.finalizeResult(task, result, len(events))
				yield(TaskEvent{
					TaskID: taskID(task),
					IDs:    task.IDs,
					Status: TaskStatusCompleted,
					Result: &result,
				}, err)
				return
			}
		}

		result, err := a.resultMapper(ctx, copyTask(task), events)
		result = a.finalizeResult(task, result, len(events))
		yield(TaskEvent{
			TaskID: taskID(task),
			IDs:    task.IDs,
			Status: TaskStatusCompleted,
			Result: &result,
		}, err)
	}
}

// Cancel validates the request; local runnable cancellation is driven by caller-owned contexts.
func (a *RunnableAgent) Cancel(ctx context.Context, taskID string) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if taskID == "" {
		return ErrTaskIDRequired
	}
	if a == nil || a.runnable == nil {
		return ErrRunnableRequired
	}
	return nil
}

func contextWithTaskRuntimeIDs(ctx context.Context, task Task) (context.Context, Task) {
	ids := task.IDs
	if contextIDs, ok := gopact.RuntimeIDsFromContext(ctx); ok && !contextIDs.IsZero() {
		ids = ids.WithDefaults(contextIDs)
	}
	if ids.IsZero() {
		return ctx, task
	}
	task.IDs = ids
	return gopact.ContextWithRuntimeIDs(ctx, ids), task
}

func defaultRunnableInputMapper(_ context.Context, task Task) (any, error) {
	return gopact.Message{Role: gopact.RoleUser, Content: task.Input}, nil
}

func defaultRunnableResultMapper(_ context.Context, task Task, events []gopact.Event) (Result, error) {
	var result Result
	result.TaskID = taskID(task)
	for _, event := range events {
		if event.Message != nil && event.Message.Role == gopact.RoleAssistant {
			if text := event.Message.Text(); text != "" {
				result.Output = text
			}
		}
		if event.Result != nil && event.Result.Content != "" && result.Output == "" {
			result.Output = event.Result.Content
		}
		result.Artifacts = append(result.Artifacts, copyArtifactRefs(event.Artifacts)...)
		if event.Result != nil {
			result.Artifacts = append(result.Artifacts, copyArtifactRefs(event.Result.Artifacts)...)
		}
	}
	result.Artifacts = dedupeArtifactRefs(result.Artifacts)
	return result, nil
}

func (a *RunnableAgent) finalizeResult(task Task, result Result, eventCount int) Result {
	if result.TaskID == "" {
		result.TaskID = taskID(task)
	}
	result.Artifacts = dedupeArtifactRefs(result.Artifacts)
	if result.Metadata == nil {
		result.Metadata = make(map[string]any)
	}
	result.Metadata["agent_name"] = a.card.Name
	result.Metadata["child_event_count"] = eventCount
	return result
}

func collectRunnableEvents(seq iter.Seq2[gopact.Event, error], ids gopact.RuntimeIDs) ([]gopact.Event, error) {
	var events []gopact.Event
	for event, err := range seq {
		event = event.WithRuntimeDefaults(ids)
		if err != nil {
			event.Err = err
		}
		events = append(events, event)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}

func runtimeTaskEvents(task Task, event gopact.Event) []TaskEvent {
	var events []TaskEvent
	ids := event.RuntimeIDs().WithDefaults(task.IDs)
	if event.Message != nil && event.Message.Role == gopact.RoleAssistant {
		if text := event.Message.Text(); text != "" {
			events = append(events, TaskEvent{
				TaskID:   taskID(task),
				IDs:      ids,
				Message:  text,
				Metadata: runnableEventMetadata(event),
			})
		}
	}
	artifacts := copyArtifactRefs(event.Artifacts)
	if event.Result != nil {
		artifacts = append(artifacts, copyArtifactRefs(event.Result.Artifacts)...)
	}
	artifacts = dedupeArtifactRefs(artifacts)
	if len(artifacts) > 0 {
		events = append(events, TaskEvent{
			TaskID:    taskID(task),
			IDs:       ids,
			Artifacts: artifacts,
			Metadata:  runnableEventMetadata(event),
		})
	}
	return events
}

func runnableEventMetadata(event gopact.Event) map[string]any {
	metadata := copyAnyMap(event.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["source_event"] = string(event.Type)
	if event.Node != "" {
		metadata["node"] = event.Node
	}
	if event.Step != 0 {
		metadata["step"] = event.Step
	}
	return metadata
}

func failedTaskEvent(task Task, err error) TaskEvent {
	return TaskEvent{
		TaskID: taskID(task),
		IDs:    task.IDs,
		Status: TaskStatusFailed,
		Err:    err,
	}
}

func taskID(task Task) string {
	if task.ID != "" {
		return task.ID
	}
	if task.IDs.CallID != "" {
		return task.IDs.CallID
	}
	return task.IDs.RunID
}

func copyTask(task Task) Task {
	if task.Auth != nil {
		auth := copyAuth(*task.Auth)
		task.Auth = &auth
	}
	task.Metadata = copyAnyMap(task.Metadata)
	return task
}

func copyArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	copy(out, in)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}

func dedupeArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, ref := range in {
		key := ref.ID
		if key == "" {
			key = ref.URI
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, ref)
	}
	return out
}
