package a2a

import (
	"errors"
	"fmt"
	"sort"

	"github.com/gopact-ai/gopact"
)

var (
	ErrTaskEventFailed   = errors.New("a2a: task event failed")
	ErrTaskEventRequired = errors.New("a2a: task event evidence is required")
)

const (
	// VerificationCheckTaskEvent is the standard check ID prefix for observed A2A task events.
	VerificationCheckTaskEvent = "a2a-task"

	// VerificationEvidenceTypeTaskEvent is the evidence type for observed A2A task events.
	VerificationEvidenceTypeTaskEvent = "a2a_task"
)

// TaskEventSnapshot is an already-observed A2A task event summary. It records
// routing, status, result shape, and error metadata, not task input or output text.
type TaskEventSnapshot struct {
	ID       string
	Name     string
	Ref      string
	Agent    AgentCard
	Task     Task
	Event    TaskEvent
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordTaskEventCheck records an already-observed A2A task event as verification evidence.
func RecordTaskEventCheck(recorder *gopact.VerificationRecorder, snapshot TaskEventSnapshot) error {
	if recorder == nil {
		return errors.New("a2a: verification recorder is nil")
	}
	if !snapshot.Skipped && !taskEventHasRef(snapshot) {
		return ErrTaskEventRequired
	}

	check := taskEventCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if err := taskEventErr(snapshot); err != nil {
			return errors.Join(ErrTaskEventFailed, err)
		}
		return ErrTaskEventFailed
	}
	return nil
}

func taskEventCheck(snapshot TaskEventSnapshot) gopact.VerificationCheck {
	ref := taskEventRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckTaskEvent + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "a2a task event"
	}
	status := taskEventCheckStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = taskEventSummary(status, snapshot)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeTaskEvent,
				Ref:      ref,
				Summary:  taskEventEvidenceSummary(status, snapshot),
				Metadata: taskEventEvidenceMetadata(snapshot),
			},
		},
		Metadata: taskEventCheckMetadata(snapshot),
	}
}

func taskEventCheckStatus(snapshot TaskEventSnapshot) gopact.VerificationStatus {
	if snapshot.Skipped {
		return gopact.VerificationStatusSkipped
	}
	event := snapshotEvent(snapshot)
	if snapshot.Err != nil || event.Err != nil ||
		event.Status == TaskStatusFailed || event.Status == TaskStatusCanceled {
		return gopact.VerificationStatusFailed
	}
	return gopact.VerificationStatusPassed
}

func taskEventSummary(status gopact.VerificationStatus, snapshot TaskEventSnapshot) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "a2a task event skipped"
	case gopact.VerificationStatusFailed:
		if err := taskEventErr(snapshot); err != nil {
			return "a2a task event failed: " + err.Error()
		}
		return "a2a task event failed"
	default:
		event := snapshotEvent(snapshot)
		if event.Status != "" {
			return "a2a task event captured: " + string(event.Status)
		}
		return "a2a task event captured"
	}
}

func taskEventEvidenceSummary(status gopact.VerificationStatus, snapshot TaskEventSnapshot) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if err := taskEventErr(snapshot); err != nil {
		return err.Error()
	}
	event := snapshotEvent(snapshot)
	if snapshot.Agent.Name != "" && event.Status != "" {
		return fmt.Sprintf("%s %s", snapshot.Agent.Name, event.Status)
	}
	if event.Status != "" {
		return string(event.Status)
	}
	return "task event captured"
}

func taskEventCheckMetadata(snapshot TaskEventSnapshot) map[string]any {
	metadata := taskEventBaseMetadata(snapshot)
	mergeTaskEventSupplementalMetadata(metadata, snapshot.Metadata)
	return metadata
}

func taskEventEvidenceMetadata(snapshot TaskEventSnapshot) map[string]any {
	return taskEventCheckMetadata(snapshot)
}

func taskEventBaseMetadata(snapshot TaskEventSnapshot) map[string]any {
	event := snapshotEvent(snapshot)
	metadata := map[string]any{
		"ref":               taskEventRef(snapshot),
		"input_bytes":       len(snapshot.Task.Input),
		"message_bytes":     len(event.Message),
		"artifact_count":    len(event.Artifacts),
		"event_metadata_ct": len(event.Metadata),
	}
	addTaskEventRuntimeIDMetadata(metadata, event.IDs.WithDefaults(snapshot.Task.IDs))
	if event.TaskID != "" {
		metadata["task_id"] = event.TaskID
	}
	if snapshot.Agent.Name != "" {
		metadata["agent_name"] = snapshot.Agent.Name
	}
	if snapshot.Agent.URL != "" {
		metadata["agent_url"] = snapshot.Agent.URL
	}
	if event.Status != "" {
		metadata["status"] = string(event.Status)
	}
	if keys := sortedTaskEventMetadataKeys(snapshot.Task.Metadata); len(keys) > 0 {
		metadata["task_metadata_keys"] = keys
	}
	if keys := sortedTaskEventMetadataKeys(event.Metadata); len(keys) > 0 {
		metadata["event_metadata_keys"] = keys
	}
	if event.Result != nil {
		metadata["result_present"] = true
		metadata["output_bytes"] = len(event.Result.Output)
		metadata["result_artifact_count"] = len(event.Result.Artifacts)
		if keys := sortedTaskEventMetadataKeys(event.Result.Metadata); len(keys) > 0 {
			metadata["result_metadata_keys"] = keys
		}
	}
	if err := taskEventErr(snapshot); err != nil {
		metadata["error"] = err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func snapshotEvent(snapshot TaskEventSnapshot) TaskEvent {
	event := snapshot.Event.WithDefaults(snapshot.Task)
	if event.Err == nil {
		event.Err = snapshot.Err
	}
	return event
}

func taskEventErr(snapshot TaskEventSnapshot) error {
	if snapshot.Err != nil {
		return snapshot.Err
	}
	return snapshot.Event.Err
}

func taskEventHasRef(snapshot TaskEventSnapshot) bool {
	if snapshot.Ref != "" || snapshot.ID != "" {
		return true
	}
	if snapshot.Event.TaskID != "" || snapshot.Task.ID != "" || snapshot.Agent.Name != "" {
		return true
	}
	ids := snapshot.Event.IDs.WithDefaults(snapshot.Task.IDs)
	return ids.CallID != "" || ids.RunID != ""
}

func taskEventRef(snapshot TaskEventSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	event := snapshotEvent(snapshot)
	if event.TaskID != "" {
		return event.TaskID
	}
	if event.IDs.CallID != "" {
		return event.IDs.CallID
	}
	if snapshot.Agent.Name != "" {
		return snapshot.Agent.Name
	}
	if event.IDs.RunID != "" {
		return event.IDs.RunID + ":a2a"
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckTaskEvent
}

func mergeTaskEventSupplementalMetadata(metadata, supplemental map[string]any) {
	for key, value := range supplemental {
		if taskEventReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func taskEventReservedMetadataKey(key string) bool {
	switch key {
	case "user_id",
		"session_id",
		"thread_id",
		"run_id",
		"agent_id",
		"app_id",
		"call_id",
		"parent_call_id",
		"trace_id",
		"ref",
		"input_bytes",
		"message_bytes",
		"artifact_count",
		"event_metadata_ct",
		"task_id",
		"agent_name",
		"agent_url",
		"status",
		"task_metadata_keys",
		"event_metadata_keys",
		"result_present",
		"output_bytes",
		"result_artifact_count",
		"result_metadata_keys",
		"error",
		"skipped":
		return true
	default:
		return false
	}
}

func addTaskEventRuntimeIDMetadata(metadata map[string]any, ids gopact.RuntimeIDs) {
	if ids.UserID != "" {
		metadata["user_id"] = ids.UserID
	}
	if ids.SessionID != "" {
		metadata["session_id"] = ids.SessionID
	}
	if ids.ThreadID != "" {
		metadata["thread_id"] = ids.ThreadID
	}
	if ids.RunID != "" {
		metadata["run_id"] = ids.RunID
	}
	if ids.AgentID != "" {
		metadata["agent_id"] = ids.AgentID
	}
	if ids.AppID != "" {
		metadata["app_id"] = ids.AppID
	}
	if ids.CallID != "" {
		metadata["call_id"] = ids.CallID
	}
	if ids.ParentCallID != "" {
		metadata["parent_call_id"] = ids.ParentCallID
	}
	if ids.TraceID != "" {
		metadata["trace_id"] = ids.TraceID
	}
}

func sortedTaskEventMetadataKeys(in map[string]any) []string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
