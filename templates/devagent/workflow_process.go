package devagent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

// WorkflowInput contains already-observed Dev Agent action boundaries for one workflow.
type WorkflowInput struct {
	IDs       gopact.RuntimeIDs `json:"ids,omitempty"`
	Name      string            `json:"name,omitempty"`
	Actions   []ProcessInput    `json:"actions,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// WorkflowRecords groups a Dev Agent workflow parent task with child process records.
type WorkflowRecords struct {
	Task          gopact.TaskRecord           `json:"task"`
	Tasks         []gopact.TaskRecord         `json:"tasks,omitempty"`
	Inputs        []gopact.InputRecord        `json:"inputs,omitempty"`
	Interventions []gopact.InterventionRecord `json:"interventions,omitempty"`
}

// BuildWorkflowProcessRecords converts observed Dev Agent action records into a parent workflow record.
func BuildWorkflowProcessRecords(input WorkflowInput) (WorkflowRecords, error) {
	if strings.TrimSpace(input.IDs.RunID) == "" {
		return WorkflowRecords{}, fmt.Errorf("%w: workflow run id is required", ErrInvalidActionResult)
	}
	if len(input.Actions) == 0 {
		return WorkflowRecords{}, fmt.Errorf("%w: workflow actions are required", ErrInvalidActionResult)
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	childRecords := make([]ProcessRecords, 0, len(input.Actions))
	failedActions := 0
	for i, action := range input.Actions {
		processInput, err := workflowProcessInput(input, action, createdAt, i+1, len(input.Actions))
		if err != nil {
			return WorkflowRecords{}, fmt.Errorf("%w: workflow action %d: %w", ErrInvalidActionResult, i, err)
		}
		records, err := BuildProcessRecords(processInput)
		if err != nil {
			return WorkflowRecords{}, fmt.Errorf("%w: workflow action %d: %w", ErrInvalidActionResult, i, err)
		}
		if records.Task.Status == gopact.TaskFailed {
			failedActions++
		}
		childRecords = append(childRecords, records)
	}

	workflowStatus := gopact.TaskCompleted
	if failedActions > 0 {
		workflowStatus = gopact.TaskFailed
	}
	workflow := WorkflowRecords{
		Task: gopact.TaskRecord{
			ID:        processID(input.IDs.RunID, "workflow"),
			Name:      workflowName(input),
			Status:    workflowStatus,
			IDs:       input.IDs,
			Input:     workflowProcessInputValue(len(input.Actions)),
			CreatedAt: createdAt,
			Metadata:  workflowProcessMetadata(input, failedActions),
		},
	}
	workflow.Task.Output = workflowProcessOutputValue(workflow.Task.Status, childRecords, failedActions)
	for _, records := range childRecords {
		task := records.Task
		task.ParentID = workflow.Task.ID
		workflow.Tasks = append(workflow.Tasks, task)
		workflow.Inputs = append(workflow.Inputs, records.Inputs...)
		workflow.Interventions = append(workflow.Interventions, records.Interventions...)
	}
	return workflow, nil
}

// RecordWorkflowProcessRecords appends Dev Agent workflow process records to a RunRecorder.
func RecordWorkflowProcessRecords(recorder *gopact.RunRecorder, input WorkflowInput) error {
	if recorder == nil {
		return errors.New("devagent: run recorder is nil")
	}
	records, err := BuildWorkflowProcessRecords(input)
	if err != nil {
		return err
	}
	if err := recorder.RecordTask(records.Task); err != nil {
		return err
	}
	for _, record := range records.Tasks {
		if err := recorder.RecordTask(record); err != nil {
			return err
		}
	}
	for _, record := range records.Inputs {
		if err := recorder.RecordInput(record); err != nil {
			return err
		}
	}
	for _, record := range records.Interventions {
		if err := recorder.RecordIntervention(record); err != nil {
			return err
		}
	}
	return nil
}

func workflowProcessInput(input WorkflowInput, action ProcessInput, createdAt time.Time, index, count int) (ProcessInput, error) {
	if err := validateWorkflowActionRuntimeIDs(input.IDs, action.IDs); err != nil {
		return ProcessInput{}, err
	}
	out := action
	out.IDs = action.IDs.WithDefaults(input.IDs)
	if out.CreatedAt.IsZero() {
		out.CreatedAt = createdAt
	}
	out.Metadata = mergeDevAgentMetadata(input.Metadata, action.Metadata)
	out.Metadata = mergeDevAgentMetadata(out.Metadata, map[string]any{
		"workflow_id":           processID(input.IDs.RunID, "workflow"),
		"workflow_name":         workflowName(input),
		"workflow_action_index": index,
		"workflow_action_count": count,
	})
	return out, nil
}

func validateWorkflowActionRuntimeIDs(workflow, action gopact.RuntimeIDs) error {
	fields := []struct {
		name string
		got  string
		want string
	}{
		{name: "run id", got: action.RunID, want: workflow.RunID},
		{name: "thread id", got: action.ThreadID, want: workflow.ThreadID},
		{name: "user id", got: action.UserID, want: workflow.UserID},
		{name: "session id", got: action.SessionID, want: workflow.SessionID},
		{name: "agent id", got: action.AgentID, want: workflow.AgentID},
		{name: "app id", got: action.AppID, want: workflow.AppID},
		{name: "call id", got: action.CallID, want: workflow.CallID},
		{name: "parent call id", got: action.ParentCallID, want: workflow.ParentCallID},
		{name: "trace id", got: action.TraceID, want: workflow.TraceID},
	}
	for _, field := range fields {
		if field.got == "" || field.want == "" {
			continue
		}
		if field.got != field.want {
			return fmt.Errorf("%s %q does not match workflow %s %q", field.name, field.got, field.name, field.want)
		}
	}
	return nil
}

func workflowName(input WorkflowInput) string {
	if strings.TrimSpace(input.Name) != "" {
		return input.Name
	}
	return "devagent workflow"
}

func workflowProcessInputValue(actionCount int) map[string]any {
	return map[string]any{
		"action_count": actionCount,
	}
}

func workflowProcessOutputValue(status gopact.TaskStatus, records []ProcessRecords, failedActions int) map[string]any {
	inputCount := 0
	interventionCount := 0
	for _, record := range records {
		inputCount += len(record.Inputs)
		interventionCount += len(record.Interventions)
	}
	return map[string]any{
		"status":              string(status),
		"action_count":        len(records),
		"failed_action_count": failedActions,
		"input_count":         inputCount,
		"intervention_count":  interventionCount,
		"actions":             workflowActionSummaries(records),
	}
}

func workflowActionSummaries(records []ProcessRecords) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		summary := map[string]any{
			"task_id":            record.Task.ID,
			"status":             string(record.Task.Status),
			"input_count":        len(record.Inputs),
			"intervention_count": len(record.Interventions),
		}
		if index, ok := record.Task.Metadata["workflow_action_index"].(int); ok {
			summary["index"] = index
		}
		if mode, ok := record.Task.Metadata["mode"].(string); ok && mode != "" {
			summary["mode"] = mode
		}
		if action, ok := record.Task.Metadata["action"].(string); ok && action != "" {
			summary["action"] = action
		}
		if actionStatus, ok := record.Task.Metadata["action_status"].(string); ok && actionStatus != "" {
			summary["action_status"] = actionStatus
		}
		out = append(out, summary)
	}
	return out
}

func workflowProcessMetadata(input WorkflowInput, failedActions int) map[string]any {
	metadata := copyDevAgentMetadata(input.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["source"] = "devagent"
	metadata["workflow"] = "devagent.workflow"
	metadata["action_count"] = len(input.Actions)
	metadata["failed_action_count"] = failedActions
	return metadata
}

func mergeDevAgentMetadata(base, override map[string]any) map[string]any {
	metadata := copyDevAgentMetadata(base)
	if metadata == nil && len(override) > 0 {
		metadata = map[string]any{}
	}
	for key, value := range override {
		metadata[key] = value
	}
	return metadata
}
