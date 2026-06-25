package devagent

import (
	"errors"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestEvaluateActionRejectsPatchProposalInAnalyzeMode(t *testing.T) {
	result, err := EvaluateAction(ActionInput{
		Mode:   ModeAnalyze,
		Action: ActionProposePatch,
		Patch:  validPatchProposal(),
	})
	if !errors.Is(err, ErrActionRejected) {
		t.Fatalf("EvaluateAction() error = %v, want ErrActionRejected", err)
	}
	if result.Status != ActionRejected {
		t.Fatalf("result.Status = %q, want %q", result.Status, ActionRejected)
	}
	if !containsActionReason(result.Reasons, "mode analyze cannot propose patch") {
		t.Fatalf("result.Reasons = %v, want analyze rejection", result.Reasons)
	}
}

func TestEvaluateActionAllowsPatchProposalInPlanModeButRejectsApply(t *testing.T) {
	result, err := EvaluateAction(ActionInput{
		Mode:   ModePlan,
		Action: ActionProposePatch,
		Patch:  validPatchProposal(),
	})
	if err != nil {
		t.Fatalf("EvaluateAction(propose) error = %v", err)
	}
	if result.Status != ActionAllowed {
		t.Fatalf("result.Status = %q, want %q", result.Status, ActionAllowed)
	}

	result, err = EvaluateAction(ActionInput{
		Mode:         ModePlan,
		Action:       ActionApplyPatch,
		Patch:        validPatchProposal(),
		ObservedDiff: "diff --git a/README.md b/README.md\n",
		PolicyDecision: &gopact.PolicyDecision{
			Action: gopact.PolicyAllow,
		},
		Events: []gopact.Event{{Type: gopact.EventSandboxFileWritten}},
	})
	if !errors.Is(err, ErrActionRejected) {
		t.Fatalf("EvaluateAction(apply) error = %v, want ErrActionRejected", err)
	}
	if !containsActionReason(result.Reasons, "mode plan cannot apply patch") {
		t.Fatalf("result.Reasons = %v, want plan apply rejection", result.Reasons)
	}
}

func TestEvaluateActionRejectsWriteApplyWithoutRequiredEvidence(t *testing.T) {
	result, err := EvaluateAction(ActionInput{
		Mode:   ModeWrite,
		Action: ActionApplyPatch,
		Patch:  validPatchProposal(),
	})
	if !errors.Is(err, ErrActionRejected) {
		t.Fatalf("EvaluateAction() error = %v, want ErrActionRejected", err)
	}
	if result.Status != ActionRejected {
		t.Fatalf("result.Status = %q, want %q", result.Status, ActionRejected)
	}
	expectedReasons := []string{
		"policy allow decision is required",
		"sandbox event is required",
		"observed diff is required",
		"observed checkpoint is required",
	}
	for _, reason := range expectedReasons {
		if !containsActionReason(result.Reasons, reason) {
			t.Fatalf("result.Reasons = %v, want %q", result.Reasons, reason)
		}
	}
}

func TestEvaluateActionAllowsWriteApplyWithPolicySandboxAndDiff(t *testing.T) {
	result, err := EvaluateAction(ActionInput{
		Mode:                  ModeWrite,
		Action:                ActionApplyPatch,
		Patch:                 validPatchProposal(),
		ObservedDiff:          "diff --git a/README.md b/README.md\n",
		ObservedCheckpointRef: "checkpoint:thread-1:2",
		PolicyDecision: &gopact.PolicyDecision{
			Action: gopact.PolicyAllow,
		},
		Events: []gopact.Event{
			{Type: gopact.EventPolicyRequested},
			{Type: gopact.EventPolicyDecided},
			{Type: gopact.EventSandboxFileWritten},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateAction() error = %v", err)
	}
	if result.Status != ActionAllowed {
		t.Fatalf("result.Status = %q, want %q", result.Status, ActionAllowed)
	}
	if result.Action != ActionApplyPatch || result.Mode != ModeWrite {
		t.Fatalf("result = %+v, want write apply", result)
	}
}

func TestEvaluateActionCopiesInputMetadataIntoResult(t *testing.T) {
	metadata := map[string]any{
		"prompt_id":  "devagent-action-v1",
		"policy_ref": "policy:write-apply",
	}
	result, err := EvaluateAction(ActionInput{
		Mode:     ModeAnalyze,
		Action:   ActionAnalyze,
		Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("EvaluateAction() error = %v", err)
	}
	if result.Metadata["prompt_id"] != "devagent-action-v1" ||
		result.Metadata["policy_ref"] != "policy:write-apply" {
		t.Fatalf("result metadata = %+v, want copied action metadata", result.Metadata)
	}

	result.Metadata["prompt_id"] = "mutated"
	if metadata["prompt_id"] != "devagent-action-v1" {
		t.Fatalf("EvaluateAction() returned shared metadata map")
	}
}

func TestEvaluateActionReleaseRequiresPassedReleaseGate(t *testing.T) {
	result, err := EvaluateAction(ActionInput{
		Mode:   ModeWrite,
		Action: ActionRelease,
		Gate:   &GateResult{Status: GateRejected, Reasons: []string{"verification failed"}},
	})
	if !errors.Is(err, ErrActionRejected) {
		t.Fatalf("EvaluateAction(rejected gate) error = %v, want ErrActionRejected", err)
	}
	if !containsActionReason(result.Reasons, "release gate status rejected is not passed") {
		t.Fatalf("result.Reasons = %v, want release gate rejection", result.Reasons)
	}

	result, err = EvaluateAction(ActionInput{
		Mode:   ModeWrite,
		Action: ActionRelease,
		Gate:   &GateResult{Status: GatePassed},
	})
	if err != nil {
		t.Fatalf("EvaluateAction(passed gate) error = %v", err)
	}
	if result.Status != ActionAllowed {
		t.Fatalf("result.Status = %q, want %q", result.Status, ActionAllowed)
	}
}

func validPatchProposal() PatchProposal {
	return PatchProposal{
		ID:      "patch-1",
		Summary: "update docs",
		Diff:    "diff --git a/README.md b/README.md\n",
		Files: []PatchFile{
			{Path: "README.md", Intent: "document current SDK state"},
		},
	}
}

func containsActionReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, want) {
			return true
		}
	}
	return false
}
