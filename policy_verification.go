package gopact

import (
	"errors"
	"fmt"
)

var ErrPolicyDecisionNotAllowed = errors.New("gopact: policy decision not allowed")

const (
	// VerificationCheckPolicyDecision is the standard check ID prefix for policy decisions.
	VerificationCheckPolicyDecision = "policy-decision"

	// VerificationEvidenceTypePolicyDecision is the evidence type for policy decisions.
	VerificationEvidenceTypePolicyDecision = "policy_decision"
)

// RecordPolicyDecisionCheck records an already-observed policy decision as verification evidence.
func RecordPolicyDecisionCheck(recorder *VerificationRecorder, request PolicyRequest, decision PolicyDecision) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if err := validatePolicyDecisionEvidenceInput(request, decision); err != nil {
		return err
	}

	check := policyDecisionCheck(request, decision)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		return ErrPolicyDecisionNotAllowed
	}
	return nil
}

func validatePolicyDecisionEvidenceInput(request PolicyRequest, decision PolicyDecision) error {
	if request.IDs.RunID == "" && request.IDs.CallID == "" {
		return errors.New("gopact: policy decision evidence run id or call id is required")
	}
	if !validPolicyBoundary(request.Boundary) {
		return fmt.Errorf("gopact: policy boundary %q is invalid", request.Boundary)
	}
	if !validPolicyRequestAction(request.Action) {
		return fmt.Errorf("gopact: policy request action %q is invalid", request.Action)
	}
	if !validPolicyAction(decision.Action) {
		return fmt.Errorf("gopact: policy decision action %q is invalid", decision.Action)
	}
	return nil
}

func policyDecisionCheck(request PolicyRequest, decision PolicyDecision) VerificationCheck {
	status := VerificationStatusFailed
	if decision.Allowed() {
		status = VerificationStatusPassed
	}
	return VerificationCheck{
		ID:       VerificationCheckPolicyDecision + ":" + policyDecisionRef(request),
		Name:     "policy decision",
		Status:   status,
		Summary:  policyDecisionSummary(request, decision),
		Evidence: policyDecisionEvidence(request, decision),
		Metadata: policyDecisionMetadata(request, decision),
	}
}

func policyDecisionSummary(request PolicyRequest, decision PolicyDecision) string {
	if decision.Reason != "" {
		return fmt.Sprintf("policy %s for %s %s: %s", decision.Action, request.Boundary, request.Action, decision.Reason)
	}
	return fmt.Sprintf("policy %s for %s %s", decision.Action, request.Boundary, request.Action)
}

func policyDecisionEvidence(request PolicyRequest, decision PolicyDecision) []VerificationEvidence {
	return []VerificationEvidence{
		{
			Type:     VerificationEvidenceTypePolicyDecision,
			Ref:      policyDecisionRef(request),
			Summary:  policyDecisionEvidenceSummary(request, decision),
			Metadata: policyDecisionEvidenceMetadata(request, decision),
		},
	}
}

func policyDecisionEvidenceSummary(request PolicyRequest, decision PolicyDecision) string {
	return fmt.Sprintf("%s %s %s", decision.Action, request.Boundary, request.Action)
}

func policyDecisionMetadata(request PolicyRequest, decision PolicyDecision) map[string]any {
	return policyDecisionBaseMetadata(request, decision)
}

func policyDecisionEvidenceMetadata(request PolicyRequest, decision PolicyDecision) map[string]any {
	return policyDecisionBaseMetadata(request, decision)
}

func policyDecisionBaseMetadata(request PolicyRequest, decision PolicyDecision) map[string]any {
	metadata := map[string]any{
		"ref":                   policyDecisionRef(request),
		"decision_action":       string(decision.Action),
		"policy_boundary":       string(request.Boundary),
		"policy_request_action": string(request.Action),
	}
	addPolicyDecisionRuntimeIDMetadata(metadata, request.IDs)
	if decision.Reason != "" {
		metadata["reason"] = decision.Reason
	}
	if len(decision.Metadata) > 0 {
		metadata["policy_metadata"] = copyAnyMap(decision.Metadata)
	}
	if len(request.Metadata) > 0 {
		metadata["request_metadata"] = copyAnyMap(request.Metadata)
	}
	return metadata
}

func policyDecisionRef(request PolicyRequest) string {
	if request.IDs.CallID != "" {
		return request.IDs.CallID
	}
	if request.IDs.RunID != "" {
		return request.IDs.RunID + ":" + string(request.Boundary) + ":" + string(request.Action)
	}
	return string(request.Boundary) + ":" + string(request.Action)
}

func addPolicyDecisionRuntimeIDMetadata(metadata map[string]any, ids RuntimeIDs) {
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
	if ids.TraceID != "" {
		metadata["trace_id"] = ids.TraceID
	}
}

func validPolicyAction(action PolicyAction) bool {
	switch action {
	case PolicyAllow, PolicyDeny, PolicyReview:
		return true
	default:
		return false
	}
}

func validPolicyBoundary(boundary PolicyBoundary) bool {
	switch boundary {
	case PolicyBoundaryNode,
		PolicyBoundaryModel,
		PolicyBoundaryTool,
		PolicyBoundaryEvent,
		PolicyBoundaryMemory,
		PolicyBoundarySandbox,
		PolicyBoundaryArtifact,
		PolicyBoundaryA2A,
		PolicyBoundaryChannel,
		PolicyBoundaryMCP,
		PolicyBoundarySkill,
		PolicyBoundaryExporter,
		PolicyBoundaryTurn,
		PolicyBoundarySecret:
		return true
	default:
		return false
	}
}

func validPolicyRequestAction(action PolicyRequestAction) bool {
	switch action {
	case PolicyActionRun,
		PolicyActionGenerate,
		PolicyActionInvoke,
		PolicyActionEmit,
		PolicyActionSend,
		PolicyActionStream,
		PolicyActionReceive,
		PolicyActionConnect,
		PolicyActionCancel,
		PolicyActionActivate,
		PolicyActionExport,
		PolicyActionCreate,
		PolicyActionExec,
		PolicyActionRead,
		PolicyActionWrite,
		PolicyActionPut,
		PolicyActionGet,
		PolicyActionSearch,
		PolicyActionDelete,
		PolicyActionList,
		PolicyActionResume,
		PolicyActionResolve,
		PolicyActionInspect:
		return true
	default:
		return false
	}
}
