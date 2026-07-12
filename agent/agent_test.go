package agent

import (
	"context"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestObserveToolOutcome(t *testing.T) {
	t.Run("result", func(t *testing.T) {
		refs := []gopact.ArtifactRef{{URI: "artifact://result"}}
		got, err := ObserveToolOutcome(gopact.ToolResultOutcome{
			CallID: "call-1",
			Name:   "search",
			Result: gopact.ToolResult{Preview: "found", ArtifactRefs: refs},
		})
		if err != nil {
			t.Fatalf("ObserveToolOutcome() error = %v", err)
		}
		refs[0].URI = "mutated"
		if got.ID != "call-1" || got.Kind != ObservationToolResult ||
			got.Source.Kind != ObservationSourceToolOutcome || got.Source.ID != "call-1" ||
			got.Subject.ToolCallID != "call-1" || got.Subject.ToolName != "search" ||
			len(got.Refs) != 1 || got.Refs[0].URI != "artifact://result" {
			t.Fatalf("observation = %+v, want typed tool result", got)
		}
	})

	t.Run("rejection", func(t *testing.T) {
		retry := &gopact.RetryHint{Retryable: true, Message: "change arguments"}
		got, err := ObserveToolOutcome(gopact.ToolRejectedOutcome{
			CallID: "call-2",
			Name:   "write",
			Rejection: gopact.ToolRejection{
				Reason:    "permission",
				Message:   "write is not allowed",
				RetryHint: retry,
			},
		})
		if err != nil {
			t.Fatalf("ObserveToolOutcome() error = %v", err)
		}
		retry.Message = "mutated"
		if got.Kind != ObservationToolRejected || got.RetryHint == nil ||
			got.RetryHint.Message != "change arguments" {
			t.Fatalf("observation = %+v, want typed rejection", got)
		}
	})

	t.Run("feedback error", func(t *testing.T) {
		got, err := ObserveToolOutcome(gopact.ToolErrorOutcome{
			CallID: "call-3",
			Name:   "shell",
			Error: gopact.ToolError{
				Kind:              "invalid_arguments",
				Message:           "command rejected",
				RetryableForModel: true,
				Feedback:          "use a relative path",
			},
		})
		if err != nil {
			t.Fatalf("ObserveToolOutcome() error = %v", err)
		}
		if got.Kind != ObservationToolError || got.Message.Parts[0].Text != "use a relative path" ||
			got.RetryHint == nil || !got.RetryHint.Retryable {
			t.Fatalf("observation = %+v, want model-actionable tool error", got)
		}
	})

	t.Run("system error", func(t *testing.T) {
		_, err := ObserveToolOutcome(gopact.ToolErrorOutcome{
			CallID: "call-4",
			Name:   "shell",
			Error:  gopact.ToolError{Kind: "dependency", Message: "backend unavailable"},
		})
		if err == nil {
			t.Fatal("ObserveToolOutcome() error = nil, want non-feedback error")
		}
	})

	t.Run("interrupt", func(t *testing.T) {
		_, err := ObserveToolOutcome(gopact.ToolInterruptOutcome{
			CallID: "call-5",
			Name:   "deploy",
			Interrupt: gopact.ToolInterrupt{
				InterruptID: "interrupt-1",
			},
		})
		if err == nil {
			t.Fatal("ObserveToolOutcome() error = nil, want interrupt error")
		}
	})
}

func TestObserveGuardRejection(t *testing.T) {
	got, err := ObserveGuardRejection(gopact.GuardRejection{
		ID:        "guard-rejection-1",
		GuardName: "policy",
		Reason:    "restricted",
		Message:   "choose another operation",
	})
	if err != nil {
		t.Fatalf("ObserveGuardRejection() error = %v", err)
	}
	if got.Kind != ObservationGuardRejected || got.Source.Kind != ObservationSourceGuardRejection ||
		got.Subject.GuardName != "policy" || got.Message.Parts[0].Text != "choose another operation" {
		t.Fatalf("observation = %+v, want typed guard rejection", got)
	}
}

func TestObserveRepairRequest(t *testing.T) {
	got, err := ObserveRepairRequest("repair-1", gopact.RepairRequest{
		Reason:  "schema mismatch",
		Message: gopact.UserMessage("return valid JSON"),
		Ref:     "schema://answer",
	})
	if err != nil {
		t.Fatalf("ObserveRepairRequest() error = %v", err)
	}
	if got.ID != "repair-1" || got.Kind != ObservationModelFeedback ||
		got.Source.Kind != ObservationSourceModelFeedback || got.Subject.SubjectRef != "schema://answer" {
		t.Fatalf("observation = %+v, want typed repair feedback", got)
	}
}

func TestToolContractsSeparateDirectAndRunAwareExecution(t *testing.T) {
	var _ Tool = toolSpecOnly{}
	var _ DirectTool = directTestTool{}
}

type toolSpecOnly struct{}

func (toolSpecOnly) Spec() gopact.ToolSpec { return gopact.ToolSpec{Name: "spec"} }

type directTestTool struct{ toolSpecOnly }

func (directTestTool) ExecuteTool(context.Context, gopact.ToolCall) (gopact.ToolOutcome, error) {
	return gopact.ToolResultOutcome{}, nil
}

func testIdentity() Identity {
	return Identity{Name: "planner", Description: "plans work", Version: "v1"}
}
