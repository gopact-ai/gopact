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
		checkWorkflowProcessChildTaskIDs(harness.Records),
		checkWorkflowProcessParentChildLinks(harness.Records),
		checkWorkflowProcessParentTaskIO(harness.Records),
		checkWorkflowProcessChildTaskIO(harness.Records),
		checkWorkflowProcessSummary(harness.Records),
		checkWorkflowProcessFailureSummary(harness.Records),
		checkWorkflowProcessRequiredActions(harness.Records, harness.RequiredActions),
		checkWorkflowProcessInputBoundaries(harness.Records, harness.RequiredInputSources),
		checkWorkflowProcessInterventionBoundaries(harness.Records),
		checkWorkflowProcessReleaseBoundaries(harness.Records),
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

func checkWorkflowProcessChildTaskIDs(records WorkflowRecords) WorkflowProcessConformanceResult {
	seen := make(map[string]int, len(records.Tasks))
	for i, task := range records.Tasks {
		id := strings.TrimSpace(task.ID)
		if id == "" {
			return failedWorkflowProcessConformance(
				"child-task-ids",
				fmt.Errorf("child task %d id is required", i),
			)
		}
		if first, ok := seen[id]; ok {
			return failedWorkflowProcessConformance(
				"child-task-ids",
				fmt.Errorf("child task %d id %q duplicates child task %d", i, id, first),
			)
		}
		seen[id] = i
	}
	return passedWorkflowProcessConformance("child-task-ids")
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

func checkWorkflowProcessParentTaskIO(records WorkflowRecords) WorkflowProcessConformanceResult {
	input, ok := records.Task.Input.(map[string]any)
	if !ok {
		return failedWorkflowProcessConformance(
			"workflow-task-io",
			fmt.Errorf("workflow input = %T, want map", records.Task.Input),
		)
	}
	if got, ok := workflowProcessIntMetadata(input, "action_count"); !ok || got != len(records.Tasks) {
		return failedWorkflowProcessConformance(
			"workflow-task-io",
			fmt.Errorf("workflow input action_count = %v, want %d", input["action_count"], len(records.Tasks)),
		)
	}
	return passedWorkflowProcessConformance("workflow-task-io")
}

func checkWorkflowProcessChildTaskIO(records WorkflowRecords) WorkflowProcessConformanceResult {
	for i, task := range records.Tasks {
		input, ok := task.Input.(map[string]any)
		if !ok {
			return failedWorkflowProcessConformance(
				"child-task-io",
				fmt.Errorf("child task %d input = %T, want map", i, task.Input),
			)
		}
		output, ok := task.Output.(map[string]any)
		if !ok {
			return failedWorkflowProcessConformance(
				"child-task-io",
				fmt.Errorf("child task %d output = %T, want map", i, task.Output),
			)
		}
		for _, field := range []string{"mode", "action"} {
			got := workflowProcessStringMetadata(input, field)
			want := workflowProcessStringMetadata(task.Metadata, field)
			if want != "" && got != want {
				return failedWorkflowProcessConformance(
					"child-task-io",
					fmt.Errorf("child task %d input %s = %q, want %q", i, field, got, want),
				)
			}
		}
		if wantReasonCount, ok := workflowProcessOutputReasonCount(output); ok {
			if got, ok := workflowProcessIntMetadata(input, "reason_count"); !ok || got != wantReasonCount {
				return failedWorkflowProcessConformance(
					"child-task-io",
					fmt.Errorf("child task %d input reason_count = %v, want %d", i, input["reason_count"], wantReasonCount),
				)
			}
		}
		if got, want := workflowProcessStringMetadata(output, "status"), workflowProcessStringMetadata(task.Metadata, "action_status"); want != "" && got != want {
			return failedWorkflowProcessConformance(
				"child-task-io",
				fmt.Errorf("child task %d output status = %q, want %q", i, got, want),
			)
		}
	}
	return passedWorkflowProcessConformance("child-task-io")
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
		if got, want := workflowProcessStringMetadata(summary, "mode"), workflowProcessStringMetadata(task.Metadata, "mode"); want != "" && got != want {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d mode = %q, want %q", i, got, want),
			)
		}
		if got, want := workflowProcessStringMetadata(summary, "action"), workflowProcessStringMetadata(task.Metadata, "action"); want != "" && got != want {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d action = %q, want %q", i, got, want),
			)
		}
		if got, want := workflowProcessStringMetadata(summary, "action_status"), workflowProcessStringMetadata(task.Metadata, "action_status"); want != "" && got != want {
			return failedWorkflowProcessConformance(
				"workflow-summary",
				fmt.Errorf("workflow action summary %d action_status = %q, want %q", i, got, want),
			)
		}
		if wantReasonCount, ok := workflowProcessOutputReasonCount(task.Output); ok {
			if got, ok := workflowProcessIntMetadata(summary, "reason_count"); !ok || got != wantReasonCount {
				return failedWorkflowProcessConformance(
					"workflow-summary",
					fmt.Errorf("workflow action summary %d reason_count = %v, want %d", i, summary["reason_count"], wantReasonCount),
				)
			}
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
		if err := validateWorkflowProcessReleaseSummaryBoundary(records, task, summary, i+1); err != nil {
			return failedWorkflowProcessConformance("workflow-summary", err)
		}
		if err := validateWorkflowProcessResumeSummaryBoundary(records, summary, i+1); err != nil {
			return failedWorkflowProcessConformance("workflow-summary", err)
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
	interruptedActions := workflowProcessInterruptedActionCount(records.Tasks)
	wantStatus := gopact.TaskCompleted
	switch {
	case failedActions > 0:
		wantStatus = gopact.TaskFailed
	case interruptedActions > 0:
		wantStatus = gopact.TaskInterrupted
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
		if got, ok := workflowProcessIntMetadata(source.data, "interrupted_action_count"); !ok || got != interruptedActions {
			return failedWorkflowProcessConformance(
				"failure-summary",
				fmt.Errorf("workflow %s interrupted_action_count = %v, want %d", source.name, source.data["interrupted_action_count"], interruptedActions),
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
		if err := validateWorkflowProcessBoundaryMetadata(records, input.Metadata, "input", i); err != nil {
			return failedWorkflowProcessConformance("input-boundaries", err)
		}
		if input.Source == "devagent.patch" && workflowProcessPatchInputLeaksRawDiff(input.Value) {
			return failedWorkflowProcessConformance("input-boundaries", fmt.Errorf("patch input %q leaked raw diff", input.ID))
		}
		if err := validateWorkflowProcessResumeInput(records, input, i); err != nil {
			return failedWorkflowProcessConformance("input-boundaries", err)
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
		if err := validateWorkflowProcessBoundaryMetadata(records, intervention.Metadata, "intervention", i); err != nil {
			return failedWorkflowProcessConformance("intervention-boundaries", err)
		}
		if err := validateWorkflowProcessInterventionResume(records, intervention, i); err != nil {
			return failedWorkflowProcessConformance("intervention-boundaries", err)
		}
	}
	return passedWorkflowProcessConformance("intervention-boundaries")
}

func checkWorkflowProcessReleaseBoundaries(records WorkflowRecords) WorkflowProcessConformanceResult {
	for i, task := range records.Tasks {
		if ActionKind(workflowProcessStringMetadata(task.Metadata, "action")) != ActionRelease {
			continue
		}
		actionIndex, ok := workflowProcessIntMetadata(task.Metadata, "workflow_action_index")
		if !ok {
			return failedWorkflowProcessConformance(
				"release-boundaries",
				fmt.Errorf("release action %d workflow_action_index = %v, want integer", i, task.Metadata["workflow_action_index"]),
			)
		}
		gateInput, ok := workflowProcessReleaseGateInput(records.Inputs, actionIndex)
		if !ok {
			return failedWorkflowProcessConformance(
				"release-boundaries",
				fmt.Errorf("release action %d release gate input is required", i),
			)
		}
		gateValue, err := validateWorkflowProcessReleaseGateInput(task, gateInput)
		if err != nil {
			return failedWorkflowProcessConformance("release-boundaries", fmt.Errorf("release action %d: %w", i, err))
		}

		actionStatus := ActionStatus(workflowProcessStringMetadata(task.Metadata, "action_status"))
		if actionStatus != ActionAllowed && !workflowProcessReleaseRequiresReview(task, gateValue) {
			continue
		}
		review, ok := workflowProcessReviewIntervention(records.Interventions, actionIndex)
		if !ok {
			status := "resolved"
			if actionStatus != ActionAllowed {
				status = "recorded"
			}
			return failedWorkflowProcessConformance(
				"release-boundaries",
				fmt.Errorf("release action %d %s review intervention is required", i, status),
			)
		}
		if err := validateWorkflowProcessReleaseReview(task, gateValue, review); err != nil {
			return failedWorkflowProcessConformance("release-boundaries", fmt.Errorf("release action %d: %w", i, err))
		}
	}
	return passedWorkflowProcessConformance("release-boundaries")
}

func validateWorkflowProcessReleaseSummaryBoundary(
	records WorkflowRecords,
	task gopact.TaskRecord,
	summary map[string]any,
	actionIndex int,
) error {
	if ActionKind(workflowProcessStringMetadata(task.Metadata, "action")) != ActionRelease {
		return nil
	}
	if gateInput, ok := workflowProcessReleaseGateInput(records.Inputs, actionIndex); ok {
		if got := workflowProcessStringMetadata(summary, "release_gate_input_id"); got != gateInput.ID {
			return fmt.Errorf("release action summary %d release_gate_input_id = %q, want %q", actionIndex, got, gateInput.ID)
		}
	}
	if review, ok := workflowProcessReviewIntervention(records.Interventions, actionIndex); ok {
		if got := workflowProcessStringMetadata(summary, "review_intervention_id"); got != review.ID {
			return fmt.Errorf("release action summary %d review_intervention_id = %q, want %q", actionIndex, got, review.ID)
		}
	}
	return nil
}

func validateWorkflowProcessResumeSummaryBoundary(
	records WorkflowRecords,
	summary map[string]any,
	actionIndex int,
) error {
	if resumeInput, ok := workflowProcessResumeInputForAction(records.Inputs, actionIndex); ok {
		if got := workflowProcessStringMetadata(summary, "resume_input_id"); got != resumeInput.ID {
			return fmt.Errorf("workflow action summary %d resume_input_id = %q, want %q", actionIndex, got, resumeInput.ID)
		}
	}
	if review, ok := workflowProcessReviewIntervention(records.Interventions, actionIndex); ok {
		if got := workflowProcessStringMetadata(summary, "review_intervention_id"); got != review.ID {
			return fmt.Errorf("workflow action summary %d review_intervention_id = %q, want %q", actionIndex, got, review.ID)
		}
	}
	return nil
}

func workflowProcessReleaseGateInput(records []gopact.InputRecord, actionIndex int) (gopact.InputRecord, bool) {
	for _, record := range records {
		if record.Source != "devagent.release_gate" {
			continue
		}
		if got, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok && got == actionIndex {
			return record, true
		}
	}
	return gopact.InputRecord{}, false
}

func workflowProcessReviewIntervention(records []gopact.InterventionRecord, actionIndex int) (gopact.InterventionRecord, bool) {
	for _, record := range records {
		if record.Type != gopact.InterruptApproval {
			continue
		}
		if got, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok && got == actionIndex {
			return record, true
		}
	}
	return gopact.InterventionRecord{}, false
}

func workflowProcessResumeInputForAction(records []gopact.InputRecord, actionIndex int) (gopact.InputRecord, bool) {
	for _, record := range records {
		if record.Kind != gopact.InputResume || record.Source != "devagent.review_resume" {
			continue
		}
		if got, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok && got == actionIndex {
			return record, true
		}
	}
	return gopact.InputRecord{}, false
}

func workflowProcessReleaseRequiresReview(task gopact.TaskRecord, gateValue map[string]any) bool {
	return ActionStatus(workflowProcessStringMetadata(task.Metadata, "action_status")) == ActionInterrupted ||
		GateStatus(workflowProcessStringMetadata(gateValue, "status")) == GatePending ||
		workflowProcessStringMetadata(task.Metadata, "review_status") != "" ||
		workflowProcessStringMetadata(gateValue, "review_status") != ""
}

func validateWorkflowProcessReleaseGateInput(
	task gopact.TaskRecord,
	record gopact.InputRecord,
) (map[string]any, error) {
	value, ok := record.Value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("release gate input value = %T, want map", record.Value)
	}
	status := GateStatus(workflowProcessStringMetadata(value, "status"))
	if !status.valid() {
		return nil, fmt.Errorf("release gate status = %q, want valid status", status)
	}
	if got, want := string(status), workflowProcessStringMetadata(task.Metadata, "gate_status"); want != "" && got != want {
		return nil, fmt.Errorf("release gate status = %q, want task gate_status %q", got, want)
	}
	actionStatus := ActionStatus(workflowProcessStringMetadata(task.Metadata, "action_status"))
	if actionStatus == ActionAllowed && status != GatePassed {
		return nil, fmt.Errorf("allowed release gate status = %q, want %q", status, GatePassed)
	}
	if actionStatus == ActionInterrupted && status != GatePending {
		return nil, fmt.Errorf("interrupted release gate status = %q, want %q", status, GatePending)
	}
	return value, nil
}

func validateWorkflowProcessReleaseReview(
	task gopact.TaskRecord,
	gateValue map[string]any,
	record gopact.InterventionRecord,
) error {
	if ActionStatus(workflowProcessStringMetadata(task.Metadata, "action_status")) == ActionInterrupted ||
		GateStatus(workflowProcessStringMetadata(gateValue, "status")) == GatePending {
		if record.Status != gopact.InterventionRequested {
			return fmt.Errorf("pending review intervention status = %q, want %q", record.Status, gopact.InterventionRequested)
		}
		if record.Request == nil {
			return errors.New("pending review intervention request is required")
		}
		if record.Request.Type != gopact.InterruptApproval {
			return fmt.Errorf("pending review request type = %q, want %q", record.Request.Type, gopact.InterruptApproval)
		}
		if got := workflowProcessStringMetadata(record.Metadata, "interrupt_id"); got != "" && got != record.Request.ID {
			return fmt.Errorf("pending review interrupt_id = %q, want request id %q", got, record.Request.ID)
		}
		if got := workflowProcessStringMetadata(record.Metadata, "interrupt_type"); got != "" && got != string(record.Request.Type) {
			return fmt.Errorf("pending review interrupt_type = %q, want request type %q", got, record.Request.Type)
		}
		return nil
	}

	reviewStatus := workflowProcessStringMetadata(record.Metadata, "review_status")
	status := ReviewStatus(reviewStatus)
	switch status {
	case ReviewApproved:
		if record.Status != gopact.InterventionResolved {
			return fmt.Errorf("approved review intervention status = %q, want %q", record.Status, gopact.InterventionResolved)
		}
	case ReviewRejected:
		if record.Status != gopact.InterventionRejected {
			return fmt.Errorf("rejected review intervention status = %q, want %q", record.Status, gopact.InterventionRejected)
		}
	default:
		return fmt.Errorf("review intervention review_status = %q, want valid status", reviewStatus)
	}
	if reviewer := workflowProcessStringMetadata(record.Metadata, "reviewer"); reviewer == "" {
		return errors.New("review intervention reviewer is required")
	}
	for _, expected := range []struct {
		name  string
		value string
	}{
		{name: "task review_status", value: workflowProcessStringMetadata(task.Metadata, "review_status")},
		{name: "gate review_status", value: workflowProcessStringMetadata(gateValue, "review_status")},
	} {
		if expected.value != "" && reviewStatus != expected.value {
			return fmt.Errorf("review intervention review_status = %q, want %s %q", reviewStatus, expected.name, expected.value)
		}
	}
	return nil
}

func validateWorkflowProcessResumeInput(records WorkflowRecords, record gopact.InputRecord, index int) error {
	if record.Kind != gopact.InputResume && record.Source != "devagent.review_resume" {
		return nil
	}
	if record.Kind != gopact.InputResume {
		return fmt.Errorf("resume input %d kind = %q, want %q", index, record.Kind, gopact.InputResume)
	}
	if record.Source != "devagent.review_resume" {
		return fmt.Errorf("resume input %d source = %q, want devagent.review_resume", index, record.Source)
	}
	if record.Resume == nil {
		return fmt.Errorf("resume input %d resume request is required", index)
	}
	if err := validateWorkflowActionRuntimeIDs(record.IDs, record.Resume.IDs); err != nil {
		return fmt.Errorf("resume input %d resume ids: %w", index, err)
	}
	if err := validateWorkflowProcessResumeMetadata(
		record.Metadata,
		*record.Resume,
		fmt.Sprintf("resume input %d", index),
	); err != nil {
		return err
	}
	actionIndex, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index")
	if !ok {
		return fmt.Errorf("resume input %d workflow_action_index = %v, want integer", index, record.Metadata["workflow_action_index"])
	}
	review, ok := workflowProcessReviewIntervention(records.Interventions, actionIndex)
	if !ok {
		return fmt.Errorf("resume input %d review intervention is required", index)
	}
	if review.Resume == nil {
		return fmt.Errorf("resume input %d review intervention resume request is required", index)
	}
	if err := validateWorkflowActionRuntimeIDs(review.IDs, review.Resume.IDs); err != nil {
		return fmt.Errorf("resume input %d review resume ids: %w", index, err)
	}
	if review.Resume.InterruptID != record.Resume.InterruptID {
		return fmt.Errorf(
			"resume input %d review resume interrupt id = %q, want %q",
			index,
			review.Resume.InterruptID,
			record.Resume.InterruptID,
		)
	}
	if err := validateWorkflowProcessResumeMetadata(
		review.Metadata,
		*review.Resume,
		fmt.Sprintf("resume input %d review intervention", index),
	); err != nil {
		return err
	}
	return nil
}

func validateWorkflowProcessInterventionResume(records WorkflowRecords, record gopact.InterventionRecord, index int) error {
	if record.Resume == nil {
		return nil
	}
	if err := validateWorkflowProcessResumeMetadata(
		record.Metadata,
		*record.Resume,
		fmt.Sprintf("intervention %d", index),
	); err != nil {
		return err
	}
	actionIndex, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index")
	if !ok {
		return fmt.Errorf("intervention %d workflow_action_index = %v, want integer", index, record.Metadata["workflow_action_index"])
	}
	input, ok := workflowProcessResumeInput(records.Inputs, actionIndex, record.Resume.InterruptID)
	if !ok {
		return fmt.Errorf("intervention %d resume input is required", index)
	}
	if input.Resume == nil {
		return fmt.Errorf("intervention %d resume input request is required", index)
	}
	if input.Resume.InterruptID != record.Resume.InterruptID {
		return fmt.Errorf(
			"intervention %d resume input interrupt id = %q, want %q",
			index,
			input.Resume.InterruptID,
			record.Resume.InterruptID,
		)
	}
	if err := validateWorkflowActionRuntimeIDs(input.IDs, input.Resume.IDs); err != nil {
		return fmt.Errorf("intervention %d resume input ids: %w", index, err)
	}
	return nil
}

func validateWorkflowProcessResumeMetadata(metadata map[string]any, resume gopact.ResumeRequest, label string) error {
	for _, field := range []struct {
		name string
		want string
	}{
		{name: "resume_interrupt_id", want: resume.InterruptID},
		{name: "resume_checkpoint_id", want: resume.CheckpointID},
		{name: "resume_step_id", want: resume.StepID},
		{name: "resume_payload_codec", want: resume.PayloadCodec},
	} {
		got := workflowProcessStringMetadata(metadata, field.name)
		if got != field.want && (got != "" || field.want != "") {
			return fmt.Errorf("%s %s = %q, want %q", label, field.name, got, field.want)
		}
	}
	return nil
}

func workflowProcessResumeInput(records []gopact.InputRecord, actionIndex int, interruptID string) (gopact.InputRecord, bool) {
	for _, record := range records {
		if record.Kind != gopact.InputResume || record.Source != "devagent.review_resume" {
			continue
		}
		gotIndex, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index")
		if !ok || gotIndex != actionIndex {
			continue
		}
		if record.Resume != nil && record.Resume.InterruptID == interruptID {
			return record, true
		}
	}
	return gopact.InputRecord{}, false
}

func validateWorkflowProcessBoundaryMetadata(records WorkflowRecords, metadata map[string]any, label string, index int) error {
	if got := workflowProcessStringMetadata(metadata, "workflow_id"); got != records.Task.ID {
		return fmt.Errorf("%s %d workflow_id = %q, want %q", label, index, got, records.Task.ID)
	}
	if got, ok := workflowProcessIntMetadata(metadata, "workflow_action_count"); !ok || got != len(records.Tasks) {
		return fmt.Errorf("%s %d workflow_action_count = %v, want %d", label, index, metadata["workflow_action_count"], len(records.Tasks))
	}
	actionIndex, ok := workflowProcessIntMetadata(metadata, "workflow_action_index")
	if !ok {
		return fmt.Errorf("%s %d workflow_action_index = %v, want integer", label, index, metadata["workflow_action_index"])
	}
	if actionIndex < 1 || actionIndex > len(records.Tasks) {
		return fmt.Errorf("%s %d workflow_action_index = %d, want 1..%d", label, index, actionIndex, len(records.Tasks))
	}
	task := records.Tasks[actionIndex-1]
	if got := workflowProcessStringMetadata(metadata, "workflow_task_id"); got != task.ID {
		return fmt.Errorf("%s %d workflow_task_id = %q, want %q", label, index, got, task.ID)
	}
	for _, field := range []string{"mode", "action", "action_status"} {
		got := workflowProcessStringMetadata(metadata, field)
		want := workflowProcessStringMetadata(task.Metadata, field)
		if want != "" && got != want {
			return fmt.Errorf("%s %d %s = %q, want %q", label, index, field, got, want)
		}
	}
	return nil
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

func workflowProcessInterruptedActionCount(tasks []gopact.TaskRecord) int {
	interrupted := 0
	for _, task := range tasks {
		if task.Status == gopact.TaskInterrupted {
			interrupted++
		}
	}
	return interrupted
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
