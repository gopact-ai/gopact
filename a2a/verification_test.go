package a2a

import (
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRecordTaskEventCheckCapturesCompletedEvent(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordTaskEventCheck(recorder, TaskEventSnapshot{
		Agent: AgentCard{Name: "reviewer", URL: "https://agents.example/reviewer"},
		Task: Task{
			ID:       "task-1",
			IDs:      gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", CallID: "call-1"},
			Input:    "private prompt text",
			Metadata: map[string]any{"priority": "high"},
		},
		Event: TaskEvent{
			Status:   TaskStatusCompleted,
			Message:  "done",
			Metadata: map[string]any{"phase": "review"},
			Result: &Result{
				TaskID:    "task-1",
				Output:    "private result text",
				Artifacts: []gopact.ArtifactRef{{ID: "artifact-1", URI: "memory://artifact-1"}},
				Metadata:  map[string]any{"quality": "checked"},
			},
		},
		Metadata: map[string]any{
			"suite":  "mesh",
			"status": "mutated",
		},
	})
	if err != nil {
		t.Fatalf("RecordTaskEventCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one check", checks)
	}
	check := checks[0]
	if check.ID != "a2a-task:task-1" ||
		check.Status != gopact.VerificationStatusPassed ||
		len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypeTaskEvent {
		t.Fatalf("check = %+v, want passed a2a task evidence", check)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["agent_name"] != "reviewer" ||
		metadata["agent_url"] != "https://agents.example/reviewer" ||
		metadata["status"] != string(TaskStatusCompleted) ||
		metadata["task_id"] != "task-1" ||
		metadata["run_id"] != "run-1" ||
		metadata["thread_id"] != "thread-1" ||
		metadata["call_id"] != "call-1" ||
		metadata["input_bytes"] != len("private prompt text") ||
		metadata["output_bytes"] != len("private result text") ||
		metadata["result_artifact_count"] != 1 ||
		metadata["suite"] != "mesh" {
		t.Fatalf("metadata = %+v, want canonical a2a task shape", metadata)
	}
	if metadata["status"] == "mutated" {
		t.Fatalf("metadata = %+v, supplemental status should not override canonical status", metadata)
	}
	if _, ok := metadata["input"]; ok {
		t.Fatalf("metadata = %+v, want no raw task input", metadata)
	}
	if _, ok := metadata["output"]; ok {
		t.Fatalf("metadata = %+v, want no raw task output", metadata)
	}
}

func TestRecordTaskEventCheckRecordsFailedBeforeReturning(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	wantErr := errors.New("remote failed")

	err := RecordTaskEventCheck(recorder, TaskEventSnapshot{
		Task: Task{ID: "task-1"},
		Event: TaskEvent{
			TaskID: "task-1",
			Status: TaskStatusFailed,
			Err:    wantErr,
		},
	})
	if !errors.Is(err, ErrTaskEventFailed) || !errors.Is(err, wantErr) {
		t.Fatalf("RecordTaskEventCheck() error = %v, want ErrTaskEventFailed + cause", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed check recorded", checks)
	}
	if checks[0].Evidence[0].Metadata["error"] != wantErr.Error() {
		t.Fatalf("metadata = %+v, want error", checks[0].Evidence[0].Metadata)
	}
}

func TestRecordTaskEventCheckRejectsMissingReference(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordTaskEventCheck(recorder, TaskEventSnapshot{})
	if !errors.Is(err, ErrTaskEventRequired) {
		t.Fatalf("RecordTaskEventCheck() error = %v, want ErrTaskEventRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("checks = %+v, want no check recorded", recorder.Checks())
	}
}
