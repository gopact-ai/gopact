package devagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

// ErrWorkflowProcessConformanceFailed reports a failed workflow process conformance case.
var ErrWorkflowProcessConformanceFailed = errors.New("devagent: workflow process conformance failed")

// WorkflowProcessConformanceHarness describes one observed workflow process under test.
type WorkflowProcessConformanceHarness struct {
	Records              WorkflowRecords
	RequiredActions      []ActionKind
	RequiredInputSources []string
}

// WorkflowProcessConformanceResult is the observed result for one workflow process contract case.
type WorkflowProcessConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckWorkflowProcessConformance runs reusable Dev Agent workflow process contract cases.
func CheckWorkflowProcessConformance(ctx context.Context, harness WorkflowProcessConformanceHarness) []WorkflowProcessConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []WorkflowProcessConformanceResult{failedWorkflowProcessConformance("context", err)}
	}

	return []WorkflowProcessConformanceResult{
		checkWorkflowProcessRecordsValid(harness.Records),
		checkWorkflowProcessParentChildLinks(harness.Records),
		checkWorkflowProcessSummary(harness.Records),
		checkWorkflowProcessFailureSummary(harness.Records),
		checkWorkflowProcessRequiredActions(harness.Records, harness.RequiredActions),
		checkWorkflowProcessInputBoundaries(harness.Records, harness.RequiredInputSources),
		checkWorkflowProcessInterventionBoundaries(harness.Records),
	}
}

// RequireWorkflowProcessConformance fails the test unless records satisfy the workflow process contract.
func RequireWorkflowProcessConformance(t testing.TB, harness WorkflowProcessConformanceHarness) {
	t.Helper()

	for _, result := range CheckWorkflowProcessConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("workflow process conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkWorkflowProcessRecordsValid(records WorkflowRecords) WorkflowProcessConformanceResult {
	if err := records.Task.Validate(); err != nil {
		return failedWorkflowProcessConformance("valid-records", fmt.Errorf("workflow task: %w", err))
	}
	if strings.TrimSpace(records.Task.IDs.RunID) == "" {
		return failedWorkflowProcessConformance("valid-records", errors.New("workflow task run id is required"))
	}
	if len(records.Tasks) == 0 {
		return failedWorkflowProcessConformance("valid-records", errors.New("workflow child tasks are required"))
	}
	for i, task := range records.Tasks {
		if err := task.Validate(); err != nil {
			return failedWorkflowProcessConformance("valid-records", fmt.Errorf("child task %d: %w", i, err))
		}
	}
	for i, input := range records.Inputs {
		if err := input.Validate(); err != nil {
			return failedWorkflowProcessConformance("valid-records", fmt.Errorf("input %d: %w", i, err))
		}
	}
	for i, intervention := range records.Interventions {
		if err := intervention.Validate(); err != nil {
			return failedWorkflowProcessConformance("valid-records", fmt.Errorf("intervention %d: %w", i, err))
		}
	}
	return passedWorkflowProcessConformance("valid-records")
}

func checkWorkflowProcessParentChildLinks(records WorkflowRecords) WorkflowProcessConformanceResult {
	for i, task := range records.Tasks {
		if task.ParentID != records.Task.ID {
			return failedWorkflowProcessConformance(
				"parent-child-links",
				fmt.Errorf("child task %d parent id = %q, want %q", i, task.ParentID, records.Task.ID),
			)
		}
		if err := validateWorkflowActionRuntimeIDs(records.Task.IDs, task.IDs); err != nil {
			return failedWorkflowProcessConformance("parent-child-links", fmt.Errorf("child task %d ids: %w", i, err))
		}
		if task.IDs.RunID != records.Task.IDs.RunID {
			return failedWorkflowProcessConformance(
				"parent-child-links",
				fmt.Errorf("child task %d run id = %q, want %q", i, task.IDs.RunID, records.Task.IDs.RunID),
			)
		}
		if got := workflowProcessStringMetadata(task.Metadata, "workflow_id"); got != records.Task.ID {
			return failedWorkflowProcessConformance(
				"parent-child-links",
				fmt.Errorf("child task %d workflow_id = %q, want %q", i, got, records.Task.ID),
			)
		}
		if got, ok := workflowProcessIntMetadata(task.Metadata, "workflow_action_index"); !ok || got != i+1 {
			return failedWorkflowProcessConformance(
				"parent-child-links",
				fmt.Errorf("child task %d workflow_action_index = %v, want %d", i, task.Metadata["workflow_action_index"], i+1),
			)
		}
		if got, ok := workflowProcessIntMetadata(task.Metadata, "workflow_action_count"); !ok || got != len(records.Tasks) {
			return failedWorkflowProcessConformance(
				"parent-child-links",
				fmt.Errorf("child task %d workflow_action_count = %v, want %d", i, task.Metadata["workflow_action_count"], len(records.Tasks)),
			)
		}
	}
	return passedWorkflowProcessConformance("parent-child-links")
}

func checkWorkflowProcessSummary(records WorkflowRecords) WorkflowProcessConformanceResult {
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		return failedWorkflowProcessConformance("workflow-summary", fmt.Errorf("workflow output = %T, want map", records.Task.Output))
	}
	if got, ok := workflowProcessIntMetadata(records.Task.Metadata, "action_count"); !ok || got != len(records.Tasks) {
		return failedWorkflowProcessConformance(
			"workflow-summary",
			fmt.Errorf("workflow metadata action_count = %v, want %d", records.Task.Metadata["action_count"], len(records.Tasks)),
		)
	}
	if got, ok := workflowProcessIntMetadata(output, "action_count"); !ok || got != len(records.Tasks) {
		return failedWorkflowProcessConformance(
			"workflow-summary",
			fmt.Errorf("workflow output action_count = %v, want %d", output["action_count"], len(records.Tasks)),
		)
	}
	if got, ok := workflowProcessIntMetadata(output, "input_count"); !ok || got != len(records.Inputs) {
		return failedWorkflowProcessConformance(
			"workflow-summary",
			fmt.Errorf("workflow output input_count = %v, want %d", output["input_count"], len(records.Inputs)),
		)
	}
	if got, ok := workflowProcessIntMetadata(output, "intervention_count"); !ok || got != len(records.Interventions) {
		return failedWorkflowProcessConformance(
			"workflow-summary",
			fmt.Errorf("workflow output intervention_count = %v, want %d", output["intervention_count"], len(records.Interventions)),
		)
	}
	if got := workflowProcessStringMetadata(output, "status"); got != string(records.Task.Status) {
		return failedWorkflowProcessConformance(
			"workflow-summary",
			fmt.Errorf("workflow output status = %q, want %q", got, records.Task.Status),
		)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		return failedWorkflowProcessConformance("workflow-summary", err)
	}
	if len(summaries) != len(records.Tasks) {
		return failedWorkflowProcessConformance(
			"workflow-summary",
			fmt.Errorf("workflow action summaries = %d, want %d", len(summaries), len(records.Tasks)),
		)
	}
	inputCounts := workflowProcessInputCountsByActionIndex(records.Inputs)
	interventionCounts := workflowProcessInterventionCountsByActionIndex(records.Interventions)
	for i, task := range records.Tasks {
		summary := summaries[i]
		if got, ok := workflowProcessIntMetadata(summary, "index"); !ok || got != i+1 {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d index = %v, want %d", i, summary["index"], i+1),
			)
		}
		if got := workflowProcessStringMetadata(summary, "task_id"); got != task.ID {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d task_id = %q, want %q", i, got, task.ID),
			)
		}
		if got := workflowProcessStringMetadata(summary, "status"); got != string(task.Status) {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d status = %q, want %q", i, got, task.Status),
			)
		}
		if got, ok := workflowProcessIntMetadata(summary, "input_count"); !ok || got != inputCounts[i+1] {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d input_count = %v, want %d", i, summary["input_count"], inputCounts[i+1]),
			)
		}
		if got, ok := workflowProcessIntMetadata(summary, "intervention_count"); !ok || got != interventionCounts[i+1] {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d intervention_count = %v, want %d", i, summary["intervention_count"], interventionCounts[i+1]),
			)
		}
	}
	return passedWorkflowProcessConformance("workflow-summary")
}

func checkWorkflowProcessFailureSummary(records WorkflowRecords) WorkflowProcessConformanceResult {
	output, ok := records.Task.Output.(map[string]any)
	if !ok {
		return failedWorkflowProcessConformance("failure-summary", fmt.Errorf("workflow output = %T, want map", records.Task.Output))
	}
	failedActions := workflowProcessFailedActionCount(records.Tasks)
	wantStatus := gopact.TaskCompleted
	if failedActions > 0 {
		wantStatus = gopact.TaskFailed
	}
	if records.Task.Status != wantStatus {
		return failedWorkflowProcessConformance(
			"failure-summary",
			fmt.Errorf("workflow task status = %q, want %q", records.Task.Status, wantStatus),
		)
	}
	for _, source := range []struct {
		name string
		data map[string]any
	}{
		{name: "metadata", data: records.Task.Metadata},
		{name: "output", data: output},
	} {
		if got, ok := workflowProcessIntMetadata(source.data, "failed_action_count"); !ok || got != failedActions {
			return failedWorkflowProcessConformance(
				"failure-summary",
				fmt.Errorf("workflow %s failed_action_count = %v, want %d", source.name, source.data["failed_action_count"], failedActions),
			)
		}
	}
	return passedWorkflowProcessConformance("failure-summary")
}

func checkWorkflowProcessRequiredActions(records WorkflowRecords, required []ActionKind) WorkflowProcessConformanceResult {
	actions := workflowProcessActionKinds(records.Tasks)
	next := 0
	for _, requiredAction := range required {
		if !requiredAction.valid() {
			return failedWorkflowProcessConformance("required-actions", fmt.Errorf("required action %q is invalid", requiredAction))
		}
		for next < len(actions) && actions[next] != requiredAction {
			next++
		}
		if next == len(actions) {
			return failedWorkflowProcessConformance("required-actions", fmt.Errorf("missing required action %q", requiredAction))
		}
		next++
	}
	return passedWorkflowProcessConformance("required-actions")
}

func checkWorkflowProcessInputBoundaries(records WorkflowRecords, requiredSources []string) WorkflowProcessConformanceResult {
	sources := make([]string, 0, len(records.Inputs))
	for i, input := range records.Inputs {
		if err := validateWorkflowActionRuntimeIDs(records.Task.IDs, input.IDs); err != nil {
			return failedWorkflowProcessConformance("input-boundaries", fmt.Errorf("input %d ids: %w", i, err))
		}
		if input.IDs.RunID != records.Task.IDs.RunID {
			return failedWorkflowProcessConformance(
				"input-boundaries",
				fmt.Errorf("input %d run id = %q, want %q", i, input.IDs.RunID, records.Task.IDs.RunID),
			)
		}
		if got := workflowProcessStringMetadata(input.Metadata, "workflow_id"); got != records.Task.ID {
			return failedWorkflowProcessConformance(
				"input-boundaries",
				fmt.Errorf("input %d workflow_id = %q, want %q", i, got, records.Task.ID),
			)
		}
		if got, ok := workflowProcessIntMetadata(input.Metadata, "workflow_action_count"); !ok || got != len(records.Tasks) {
			return failedWorkflowProcessConformance(
				"input-boundaries",
				fmt.Errorf("input %d workflow_action_count = %v, want %d", i, input.Metadata["workflow_action_count"], len(records.Tasks)),
			)
		}
		if input.Source == "devagent.patch" && workflowProcessPatchInputLeaksRawDiff(input.Value) {
			return failedWorkflowProcessConformance("input-boundaries", fmt.Errorf("patch input %q leaked raw diff", input.ID))
		}
		sources = append(sources, input.Source)
	}
	if err := workflowProcessRequiredStrings("input source", sources, requiredSources); err != nil {
		return failedWorkflowProcessConformance("input-boundaries", err)
	}
	return passedWorkflowProcessConformance("input-boundaries")
}

func checkWorkflowProcessInterventionBoundaries(records WorkflowRecords) WorkflowProcessConformanceResult {
	for i, intervention := range records.Interventions {
		if err := validateWorkflowActionRuntimeIDs(records.Task.IDs, intervention.IDs); err != nil {
			return failedWorkflowProcessConformance("intervention-boundaries", fmt.Errorf("intervention %d ids: %w", i, err))
		}
		if intervention.IDs.RunID != records.Task.IDs.RunID {
			return failedWorkflowProcessConformance(
				"intervention-boundaries",
				fmt.Errorf("intervention %d run id = %q, want %q", i, intervention.IDs.RunID, records.Task.IDs.RunID),
			)
		}
		if got := workflowProcessStringMetadata(intervention.Metadata, "workflow_id"); got != records.Task.ID {
			return failedWorkflowProcessConformance(
				"intervention-boundaries",
				fmt.Errorf("intervention %d workflow_id = %q, want %q", i, got, records.Task.ID),
			)
		}
		if got, ok := workflowProcessIntMetadata(intervention.Metadata, "workflow_action_count"); !ok || got != len(records.Tasks) {
			return failedWorkflowProcessConformance(
				"intervention-boundaries",
				fmt.Errorf("intervention %d workflow_action_count = %v, want %d", i, intervention.Metadata["workflow_action_count"], len(records.Tasks)),
			)
		}
	}
	return passedWorkflowProcessConformance("intervention-boundaries")
}

func workflowProcessActionSummaries(output map[string]any) ([]map[string]any, error) {
	switch summaries := output["actions"].(type) {
	case []map[string]any:
		return summaries, nil
	case []any:
		out := make([]map[string]any, 0, len(summaries))
		for i, summary := range summaries {
			item, ok := summary.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("workflow action summary %d = %T, want map", i, summary)
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("workflow output actions = %T, want slice", output["actions"])
	}
}

func workflowProcessActionKinds(tasks []gopact.TaskRecord) []ActionKind {
	out := make([]ActionKind, 0, len(tasks))
	for _, task := range tasks {
		action := ActionKind(workflowProcessStringMetadata(task.Metadata, "action"))
		if action != "" {
			out = append(out, action)
		}
	}
	return out
}

func workflowProcessFailedActionCount(tasks []gopact.TaskRecord) int {
	failed := 0
	for _, task := range tasks {
		if task.Status == gopact.TaskFailed {
			failed++
		}
	}
	return failed
}

func workflowProcessInputCountsByActionIndex(records []gopact.InputRecord) map[int]int {
	counts := map[int]int{}
	for _, record := range records {
		if index, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok {
			counts[index]++
		}
	}
	return counts
}

func workflowProcessInterventionCountsByActionIndex(records []gopact.InterventionRecord) map[int]int {
	counts := map[int]int{}
	for _, record := range records {
		if index, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok {
			counts[index]++
		}
	}
	return counts
}

func workflowProcessRequiredStrings(label string, observed []string, required []string) error {
	next := 0
	for _, want := range required {
		if strings.TrimSpace(want) == "" {
			return fmt.Errorf("required %s is empty", label)
		}
		for next < len(observed) && observed[next] != want {
			next++
		}
		if next == len(observed) {
			return fmt.Errorf("missing required %s %q", label, want)
		}
		next++
	}
	return nil
}

func workflowProcessPatchInputLeaksRawDiff(value any) bool {
	input, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, hasDiff := input["diff"]
	return hasDiff
}

func workflowProcessStringMetadata(metadata map[string]any, key string) string {
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return value
}

func workflowProcessIntMetadata(metadata map[string]any, key string) (int, bool) {
	switch value := metadata[key].(type) {
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case uint:
		return int(value), true
	case uint8:
		return int(value), true
	case uint16:
		return int(value), true
	case uint32:
		return int(value), true
	case uint64:
		return int(value), true
	case float64:
		if value == float64(int(value)) {
			return int(value), true
		}
		return 0, false
	case float32:
		if value == float32(int(value)) {
			return int(value), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func passedWorkflowProcessConformance(name string) WorkflowProcessConformanceResult {
	return WorkflowProcessConformanceResult{Case: name, Passed: true}
}

func failedWorkflowProcessConformance(name string, err error) WorkflowProcessConformanceResult {
	return WorkflowProcessConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrWorkflowProcessConformanceFailed, err),
	}
}
