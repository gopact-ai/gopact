package devagent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var ErrActionRejected = errors.New("devagent: action rejected")

// ActionKind describes one Dev Agent workflow action.
type ActionKind string

const (
	ActionAnalyze      ActionKind = "analyze"
	ActionProposePatch ActionKind = "propose_patch"
	ActionApplyPatch   ActionKind = "apply_patch"
	ActionRelease      ActionKind = "release"
)

// ActionStatus is the result of evaluating a Dev Agent action.
type ActionStatus string

const (
	ActionAllowed  ActionStatus = "allowed"
	ActionRejected ActionStatus = "rejected"
)

// PatchFile describes one file touched by a proposed patch.
type PatchFile struct {
	Path     string         `json:"path,omitempty"`
	Intent   string         `json:"intent,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PatchProposal is a generated patch suggestion, not proof that the patch was applied.
type PatchProposal struct {
	ID       string         `json:"id,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Diff     string         `json:"diff,omitempty"`
	Files    []PatchFile    `json:"files,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ActionInput contains already-observed evidence for a Dev Agent action decision.
type ActionInput struct {
	Mode                  Mode                   `json:"mode"`
	Action                ActionKind             `json:"action"`
	Patch                 PatchProposal          `json:"patch,omitempty"`
	ObservedDiff          string                 `json:"observed_diff,omitempty"`
	ObservedCheckpointRef string                 `json:"observed_checkpoint_ref,omitempty"`
	PolicyDecision        *gopact.PolicyDecision `json:"policy_decision,omitempty"`
	Events                []gopact.Event         `json:"events,omitempty"`
	Gate                  *GateResult            `json:"gate,omitempty"`
	Metadata              map[string]any         `json:"metadata,omitempty"`
}

// ActionResult is a structured, exportable action decision.
type ActionResult struct {
	Status   ActionStatus   `json:"status"`
	Mode     Mode           `json:"mode"`
	Action   ActionKind     `json:"action"`
	Reasons  []string       `json:"reasons,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// EvaluateAction checks whether a Dev Agent action is allowed in the current mode.
func EvaluateAction(input ActionInput) (ActionResult, error) {
	result := ActionResult{
		Mode:     input.Mode,
		Action:   input.Action,
		Metadata: copyDevAgentMetadata(input.Metadata),
	}
	if !input.Mode.valid() {
		return result, fmt.Errorf("%w: %q", ErrInvalidMode, input.Mode)
	}
	if !input.Action.valid() {
		return result, fmt.Errorf("devagent: invalid action %q", input.Action)
	}

	var reasons []string
	if !actionAllowedInMode(input.Mode, input.Action) {
		reasons = append(reasons, fmt.Sprintf("mode %s cannot %s", input.Mode, input.Action.reasonVerb()))
	}
	switch input.Action {
	case ActionProposePatch:
		reasons = append(reasons, patchProposalReasons(input.Patch)...)
	case ActionApplyPatch:
		reasons = append(reasons, patchProposalReasons(input.Patch)...)
		reasons = append(reasons, applyPatchEvidenceReasons(input)...)
	case ActionRelease:
		reasons = append(reasons, releaseEvidenceReasons(input.Gate)...)
	}
	if len(reasons) > 0 {
		result.Status = ActionRejected
		result.Reasons = reasons
		return result, fmt.Errorf("%w: %s", ErrActionRejected, strings.Join(reasons, "; "))
	}
	result.Status = ActionAllowed
	return result, nil
}

func (a ActionKind) valid() bool {
	switch a {
	case ActionAnalyze, ActionProposePatch, ActionApplyPatch, ActionRelease:
		return true
	default:
		return false
	}
}

func (a ActionKind) reasonVerb() string {
	switch a {
	case ActionProposePatch:
		return "propose patch"
	case ActionApplyPatch:
		return "apply patch"
	default:
		return string(a)
	}
}

func actionAllowedInMode(mode Mode, action ActionKind) bool {
	switch mode {
	case ModeAnalyze:
		return action == ActionAnalyze
	case ModePlan:
		return action == ActionAnalyze || action == ActionProposePatch
	case ModeWrite:
		return true
	default:
		return false
	}
}

func patchProposalReasons(patch PatchProposal) []string {
	var reasons []string
	if patch.ID == "" {
		reasons = append(reasons, "patch id is required")
	}
	if strings.TrimSpace(patch.Summary) == "" {
		reasons = append(reasons, "patch summary is required")
	}
	if strings.TrimSpace(patch.Diff) == "" && len(patch.Files) == 0 {
		reasons = append(reasons, "patch diff or files are required")
	}
	for i, file := range patch.Files {
		if strings.TrimSpace(file.Path) == "" {
			reasons = append(reasons, fmt.Sprintf("patch file %d path is required", i))
		}
	}
	return reasons
}

func applyPatchEvidenceReasons(input ActionInput) []string {
	var reasons []string
	if input.PolicyDecision == nil || !input.PolicyDecision.Allowed() {
		reasons = append(reasons, "policy allow decision is required")
	}
	if !hasSandboxEvent(input.Events) {
		reasons = append(reasons, "sandbox event is required")
	}
	if strings.TrimSpace(input.ObservedDiff) == "" {
		reasons = append(reasons, "observed diff is required")
	}
	if strings.TrimSpace(input.ObservedCheckpointRef) == "" {
		reasons = append(reasons, "observed checkpoint is required")
	}
	return reasons
}

func releaseEvidenceReasons(gate *GateResult) []string {
	if gate == nil {
		return []string{"release gate result is required"}
	}
	if gate.Status != GatePassed {
		return []string{fmt.Sprintf("release gate status %s is not passed", gate.Status)}
	}
	return nil
}

func hasSandboxEvent(events []gopact.Event) bool {
	for _, event := range events {
		switch event.Type {
		case gopact.EventSandboxCreated,
			gopact.EventSandboxExecStarted,
			gopact.EventSandboxExecCompleted,
			gopact.EventSandboxExecFailed,
			gopact.EventSandboxFileRead,
			gopact.EventSandboxFileWritten,
			gopact.EventSandboxClosed:
			return true
		}
	}
	return false
}
