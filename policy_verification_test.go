package gopact

import (
	"errors"
	"testing"
)

func TestRecordPolicyDecisionCheckRecordsAllowAsPassedCheck(t *testing.T) {
	recorder := NewVerificationRecorder()
	request := PolicyRequest{
		IDs: RuntimeIDs{
			RunID:    "run-1",
			ThreadID: "thread-1",
			CallID:   "call-1",
			UserID:   "user-1",
		},
		Boundary: PolicyBoundaryTool,
		Action:   PolicyActionInvoke,
		Metadata: map[string]any{"scope": "write"},
	}
	decision := PolicyDecision{
		Action: PolicyAllow,
		Reason: "safe write",
		Metadata: map[string]any{
			"reviewer": "policy",
		},
	}

	if err := RecordPolicyDecisionCheck(recorder, request, decision); err != nil {
		t.Fatalf("RecordPolicyDecisionCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "policy-decision:call-1" || check.Name != "policy decision" || check.Status != VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed policy decision check", check)
	}
	if len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypePolicyDecision ||
		check.Evidence[0].Ref != "call-1" {
		t.Fatalf("evidence = %+v, want policy decision evidence", check.Evidence)
	}
	if check.Metadata["decision_action"] != string(PolicyAllow) ||
		check.Metadata["policy_boundary"] != string(PolicyBoundaryTool) ||
		check.Metadata["policy_request_action"] != string(PolicyActionInvoke) ||
		check.Metadata["reason"] != "safe write" ||
		check.Metadata["run_id"] != "run-1" ||
		check.Metadata["thread_id"] != "thread-1" ||
		check.Metadata["call_id"] != "call-1" ||
		check.Metadata["user_id"] != "user-1" {
		t.Fatalf("metadata = %+v, want policy decision metadata", check.Metadata)
	}
	policyMetadata, ok := check.Metadata["policy_metadata"].(map[string]any)
	if !ok || policyMetadata["reviewer"] != "policy" {
		t.Fatalf("policy metadata = %#v, want copied policy metadata", check.Metadata["policy_metadata"])
	}
	requestMetadata, ok := check.Metadata["request_metadata"].(map[string]any)
	if !ok || requestMetadata["scope"] != "write" {
		t.Fatalf("request metadata = %#v, want copied request metadata", check.Metadata["request_metadata"])
	}
}

func TestRecordPolicyDecisionCheckAcceptsTurnResumePolicy(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordPolicyDecisionCheck(recorder, PolicyRequest{
		IDs:      RuntimeIDs{RunID: "run-1"},
		Boundary: PolicyBoundaryTurn,
		Action:   PolicyActionResume,
	}, PolicyDecision{Action: PolicyAllow})
	if err != nil {
		t.Fatalf("RecordPolicyDecisionCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(checks))
	}
	if checks[0].Metadata["policy_boundary"] != string(PolicyBoundaryTurn) ||
		checks[0].Metadata["policy_request_action"] != string(PolicyActionResume) {
		t.Fatalf("metadata = %+v, want turn/resume policy metadata", checks[0].Metadata)
	}
}

func TestRecordPolicyDecisionCheckAcceptsA2ACancelPolicy(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordPolicyDecisionCheck(recorder, PolicyRequest{
		IDs:      RuntimeIDs{RunID: "run-1"},
		Boundary: PolicyBoundaryA2A,
		Action:   PolicyActionCancel,
	}, PolicyDecision{Action: PolicyAllow})
	if err != nil {
		t.Fatalf("RecordPolicyDecisionCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(checks))
	}
	if checks[0].Metadata["policy_boundary"] != string(PolicyBoundaryA2A) ||
		checks[0].Metadata["policy_request_action"] != string(PolicyActionCancel) {
		t.Fatalf("metadata = %+v, want a2a/cancel policy metadata", checks[0].Metadata)
	}
}

func TestRecordPolicyDecisionCheckRecordsDenyBeforeReturningError(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordPolicyDecisionCheck(recorder, PolicyRequest{
		IDs:      RuntimeIDs{RunID: "run-1"},
		Boundary: PolicyBoundaryModel,
		Action:   PolicyActionGenerate,
	}, PolicyDecision{
		Action: PolicyDeny,
		Reason: "blocked",
	})
	if !errors.Is(err, ErrPolicyDecisionNotAllowed) {
		t.Fatalf("RecordPolicyDecisionCheck() error = %v, want ErrPolicyDecisionNotAllowed", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed policy decision check", checks)
	}
	if checks[0].Metadata["decision_action"] != string(PolicyDeny) {
		t.Fatalf("metadata = %+v, want deny action", checks[0].Metadata)
	}
}

func TestRecordPolicyDecisionCheckRecordsReviewBeforeReturningError(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordPolicyDecisionCheck(recorder, PolicyRequest{
		IDs:      RuntimeIDs{RunID: "run-1"},
		Boundary: PolicyBoundaryTool,
		Action:   PolicyActionInvoke,
	}, PolicyDecision{
		Action: PolicyReview,
		Reason: "needs approval",
	})
	if !errors.Is(err, ErrPolicyDecisionNotAllowed) {
		t.Fatalf("RecordPolicyDecisionCheck() error = %v, want ErrPolicyDecisionNotAllowed", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed policy decision check", checks)
	}
	if checks[0].Metadata["decision_action"] != string(PolicyReview) {
		t.Fatalf("metadata = %+v, want review action", checks[0].Metadata)
	}
}

func TestRecordPolicyDecisionCheckRejectsInvalidInput(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordPolicyDecisionCheck(nil, PolicyRequest{
		IDs:      RuntimeIDs{RunID: "run-1"},
		Boundary: PolicyBoundaryTool,
		Action:   PolicyActionInvoke,
	}, PolicyDecision{Action: PolicyAllow}); err == nil {
		t.Fatal("RecordPolicyDecisionCheck(nil) error = nil, want error")
	}
	if err := RecordPolicyDecisionCheck(recorder, PolicyRequest{
		Boundary: PolicyBoundaryTool,
		Action:   PolicyActionInvoke,
	}, PolicyDecision{Action: PolicyAllow}); err == nil {
		t.Fatal("RecordPolicyDecisionCheck(missing run id/call id) error = nil, want error")
	}
	if err := RecordPolicyDecisionCheck(recorder, PolicyRequest{
		IDs:      RuntimeIDs{RunID: "run-1"},
		Boundary: PolicyBoundaryTool,
		Action:   PolicyActionInvoke,
	}, PolicyDecision{}); err == nil {
		t.Fatal("RecordPolicyDecisionCheck(empty action) error = nil, want error")
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("checks = %+v, want none after invalid policy decision", recorder.Checks())
	}
}
