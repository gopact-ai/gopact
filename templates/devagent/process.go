package devagent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

// ErrInvalidActionResult reports an incomplete or inconsistent observed Dev Agent action.
var ErrInvalidActionResult = errors.New("devagent: invalid action result")

// ProcessInput contains already-observed Dev Agent decisions to attach to a run export.
type ProcessInput struct {
	IDs       gopact.RuntimeIDs `json:"ids,omitempty"`
	Action    ActionResult      `json:"action"`
	Patch     PatchProposal     `json:"patch,omitempty"`
	Review    ReviewDecision    `json:"review,omitempty"`
	Gate      *GateResult       `json:"gate,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// ProcessRecords groups the process records generated for one Dev Agent decision boundary.
type ProcessRecords struct {
	Task          gopact.TaskRecord           `json:"task"`
	Inputs        []gopact.InputRecord        `json:"inputs,omitempty"`
	Interventions []gopact.InterventionRecord `json:"interventions,omitempty"`
}

// BuildProcessRecords converts observed Dev Agent decisions into portable run process records.
func BuildProcessRecords(input ProcessInput) (ProcessRecords, error) {
	if err := validateProcessInput(input); err != nil {
		return ProcessRecords{}, err
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	records := ProcessRecords{
		Task: gopact.TaskRecord{
			ID:        processID(input.IDs.RunID, string(input.Action.Action)),
			Name:      "devagent " + string(input.Action.Action),
			Status:    taskStatusForAction(input.Action.Status),
			IDs:       input.IDs,
			Input:     actionProcessInput(input.Action),
			Output:    actionProcessOutput(input.Action),
			CreatedAt: createdAt,
			Metadata:  processTaskMetadata(input),
		},
	}
	if hasPatchProcessInput(input.Patch) {
		records.Inputs = append(records.Inputs, gopact.InputRecord{
			ID:        processID(input.IDs.RunID, "patch", input.Patch.ID),
			Kind:      gopact.InputExternal,
			IDs:       input.IDs,
			Source:    "devagent.patch",
			Value:     patchProcessInput(input.Patch),
			CreatedAt: createdAt,
			Metadata:  processBoundaryMetadata(input.Action, input.Metadata),
		})
	}
	if input.Gate != nil {
		records.Inputs = append(records.Inputs, gopact.InputRecord{
			ID:        processID(input.IDs.RunID, "release_gate"),
			Kind:      gopact.InputExternal,
			IDs:       input.IDs,
			Source:    "devagent.release_gate",
			Value:     gateProcessInput(*input.Gate),
			CreatedAt: createdAt,
			Metadata:  processBoundaryMetadata(input.Action, input.Metadata),
		})
	}
	if input.Review.Status != ReviewUnknown {
		records.Interventions = append(records.Interventions, gopact.InterventionRecord{
			ID:        processID(input.IDs.RunID, "review", input.Review.Reviewer),
			Type:      gopact.InterruptApproval,
			Status:    interventionStatusForReview(input.Review.Status),
			IDs:       input.IDs,
			CreatedAt: createdAt,
			Metadata:  reviewInterventionMetadata(input),
		})
	}
	return records, nil
}

// RecordProcessRecords appends Dev Agent process records to a RunRecorder.
func RecordProcessRecords(recorder *gopact.RunRecorder, input ProcessInput) error {
	if recorder == nil {
		return errors.New("devagent: run recorder is nil")
	}
	records, err := BuildProcessRecords(input)
	if err != nil {
		return err
	}
	return ImportProcessRecords(recorder, records)
}

// ImportProcessRecords appends already-observed Dev Agent process records to a RunRecorder.
//
// It validates the record set and stores defensive copies. It does not rebuild
// the action, execute tools, call reviewers, or reinterpret the process.
func ImportProcessRecords(recorder *gopact.RunRecorder, records ProcessRecords) error {
	if recorder == nil {
		return errors.New("devagent: run recorder is nil")
	}
	if err := validateProcessRecordsForImport(records); err != nil {
		return err
	}
	records = copyReleaseProcessRecords(records)
	if err := recorder.RecordTask(records.Task); err != nil {
		return err
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

func validateProcessRecordsForImport(records ProcessRecords) error {
	if err := records.Task.Validate(); err != nil {
		return fmt.Errorf("devagent: process task: %w", err)
	}
	for _, record := range records.Inputs {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("devagent: process input %q: %w", record.ID, err)
		}
		if err := validateWorkflowActionRuntimeIDs(records.Task.IDs, record.IDs); err != nil {
			return fmt.Errorf("devagent: process input %q: %w", record.ID, err)
		}
	}
	for _, record := range records.Interventions {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("devagent: process intervention %q: %w", record.ID, err)
		}
		if err := validateWorkflowActionRuntimeIDs(records.Task.IDs, record.IDs); err != nil {
			return fmt.Errorf("devagent: process intervention %q: %w", record.ID, err)
		}
	}
	return nil
}

func validateProcessInput(input ProcessInput) error {
	if strings.TrimSpace(input.IDs.RunID) == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidActionResult)
	}
	if !input.Action.Status.valid() {
		return fmt.Errorf("%w: status %q", ErrInvalidActionResult, input.Action.Status)
	}
	if !input.Action.Mode.valid() {
		return fmt.Errorf("%w: mode %q", ErrInvalidActionResult, input.Action.Mode)
	}
	if !input.Action.Action.valid() {
		return fmt.Errorf("%w: action %q", ErrInvalidActionResult, input.Action.Action)
	}
	if hasPatchProcessInput(input.Patch) && strings.TrimSpace(input.Patch.ID) == "" {
		return fmt.Errorf("%w: patch id is required", ErrInvalidActionResult)
	}
	if input.Review.Status != ReviewUnknown {
		if err := validateReviewDecision(input.Review); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidActionResult, err)
		}
	}
	return nil
}

func (s ActionStatus) valid() bool {
	switch s {
	case ActionAllowed, ActionRejected:
		return true
	default:
		return false
	}
}

func taskStatusForAction(status ActionStatus) gopact.TaskStatus {
	if status == ActionAllowed {
		return gopact.TaskCompleted
	}
	return gopact.TaskFailed
}

func interventionStatusForReview(status ReviewStatus) gopact.InterventionStatus {
	if status == ReviewApproved {
		return gopact.InterventionResolved
	}
	return gopact.InterventionRejected
}

func hasPatchProcessInput(patch PatchProposal) bool {
	return strings.TrimSpace(patch.ID) != "" ||
		strings.TrimSpace(patch.Summary) != "" ||
		strings.TrimSpace(patch.Diff) != "" ||
		len(patch.Files) > 0
}

func processID(parts ...string) string {
	segments := make([]string, 0, len(parts)+1)
	segments = append(segments, "devagent")
	for _, part := range parts {
		part = safeProcessIDPart(part)
		if part == "" {
			continue
		}
		segments = append(segments, part)
	}
	return strings.Join(segments, ":")
}

func safeProcessIDPart(part string) string {
	part = strings.TrimSpace(part)
	part = strings.ReplaceAll(part, ":", "-")
	return part
}

func actionProcessInput(action ActionResult) map[string]any {
	value := map[string]any{
		"mode":   string(action.Mode),
		"action": string(action.Action),
	}
	if len(action.Reasons) > 0 {
		value["reason_count"] = len(action.Reasons)
	}
	return value
}

func actionProcessOutput(action ActionResult) map[string]any {
	value := map[string]any{
		"status": string(action.Status),
	}
	if len(action.Reasons) > 0 {
		value["reasons"] = copyStringSlice(action.Reasons)
	}
	return value
}

func patchProcessInput(patch PatchProposal) map[string]any {
	value := map[string]any{
		"id":         patch.ID,
		"summary":    patch.Summary,
		"file_count": len(patch.Files),
		"has_diff":   strings.TrimSpace(patch.Diff) != "",
	}
	if len(patch.Files) > 0 {
		files := make([]map[string]any, 0, len(patch.Files))
		for _, file := range patch.Files {
			files = append(files, map[string]any{
				"path":   file.Path,
				"intent": file.Intent,
			})
		}
		value["files"] = files
	}
	return value
}

func gateProcessInput(gate GateResult) map[string]any {
	value := map[string]any{
		"status": string(gate.Status),
	}
	if gate.Mode != "" {
		value["mode"] = string(gate.Mode)
	}
	if gate.ReportStatus != "" {
		value["report_status"] = string(gate.ReportStatus)
	}
	if gate.ReviewStatus != ReviewUnknown {
		value["review_status"] = string(gate.ReviewStatus)
	}
	if gate.MaxEntropySeverity != "" {
		value["max_entropy_severity"] = string(gate.MaxEntropySeverity)
	}
	if len(gate.Reasons) > 0 {
		value["reasons"] = copyStringSlice(gate.Reasons)
	}
	return value
}

func processTaskMetadata(input ProcessInput) map[string]any {
	metadata := processBoundaryMetadata(input.Action, input.Metadata)
	if input.Patch.ID != "" {
		metadata["patch_id"] = input.Patch.ID
	}
	if input.Gate != nil {
		metadata["gate_status"] = string(input.Gate.Status)
	}
	if input.Review.Status != ReviewUnknown {
		metadata["review_status"] = string(input.Review.Status)
	}
	return metadata
}

func processBoundaryMetadata(action ActionResult, base map[string]any) map[string]any {
	metadata := mergeDevAgentMetadata(action.Metadata, base)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["source"] = "devagent"
	metadata["mode"] = string(action.Mode)
	metadata["action"] = string(action.Action)
	metadata["action_status"] = string(action.Status)
	return metadata
}

func reviewInterventionMetadata(input ProcessInput) map[string]any {
	metadata := processBoundaryMetadata(input.Action, input.Metadata)
	metadata["reviewer"] = input.Review.Reviewer
	metadata["review_status"] = string(input.Review.Status)
	if input.Review.Summary != "" {
		metadata["summary"] = input.Review.Summary
	}
	return metadata
}
