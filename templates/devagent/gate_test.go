package devagent

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestEvaluateReleaseGatePassesApprovedWriteRun(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status:   ReviewApproved,
			Reviewer: "reviewer-1",
			Summary:  "safe docs/test change",
		},
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:        "entropy-1",
				Status:    gopact.VerificationStatusPassed,
				IDs:       report.IDs,
				CreatedAt: time.Now(),
			},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	if result.Status != GatePassed {
		t.Fatalf("result.Status = %q, want %q", result.Status, GatePassed)
	}
	if result.Mode != ModeWrite || result.ReportStatus != gopact.VerificationStatusPassed || result.ReviewStatus != ReviewApproved {
		t.Fatalf("result = %+v, want write/passed/approved", result)
	}
	if len(result.Reasons) != 0 {
		t.Fatalf("result.Reasons = %v, want none", result.Reasons)
	}
}

func TestEvaluateReleaseGateRejectsFailedVerification(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusFailed)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if result.Status != GateRejected {
		t.Fatalf("result.Status = %q, want %q", result.Status, GateRejected)
	}
	if !containsReason(result.Reasons, "verification status failed") {
		t.Fatalf("result.Reasons = %v, want verification rejection", result.Reasons)
	}
}

func TestEvaluateReleaseGateRejectsHighEntropyFinding(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:        "entropy-1",
				Status:    gopact.VerificationStatusFailed,
				IDs:       report.IDs,
				CreatedAt: time.Now(),
				Findings: []gopact.EntropyFinding{
					{
						ID:        "finding-1",
						Category:  gopact.EntropySecurity,
						Severity:  gopact.EntropySeverityHigh,
						Summary:   "unsafe write scope",
						CreatedAt: time.Now(),
					},
				},
			},
		},
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if result.MaxEntropySeverity != gopact.EntropySeverityHigh {
		t.Fatalf("result.MaxEntropySeverity = %q, want high", result.MaxEntropySeverity)
	}
	if !containsReason(result.Reasons, "entropy finding finding-1 severity high exceeds medium") {
		t.Fatalf("result.Reasons = %v, want entropy rejection", result.Reasons)
	}
}

func TestEvaluateReleaseGateSkipsNonWriteModes(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusFailed)
	for _, mode := range []Mode{ModeAnalyze, ModePlan} {
		t.Run(string(mode), func(t *testing.T) {
			result, err := EvaluateReleaseGate(GateInput{
				Mode:   mode,
				Report: report,
			})
			if err != nil {
				t.Fatalf("EvaluateReleaseGate() error = %v", err)
			}
			if result.Status != GateSkipped {
				t.Fatalf("result.Status = %q, want %q", result.Status, GateSkipped)
			}
			if !reflect.DeepEqual(result.Reasons, []string{"release gate applies only to write mode"}) {
				t.Fatalf("result.Reasons = %v, want skip reason", result.Reasons)
			}
		})
	}
}

func TestEvaluateReleaseGateRejectsMissingReviewWhenRequired(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if !containsReason(result.Reasons, "review approval is required") {
		t.Fatalf("result.Reasons = %v, want review rejection", result.Reasons)
	}

	result, err = EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
	}, RequireReview(false))
	if err != nil {
		t.Fatalf("EvaluateReleaseGate(require review false) error = %v", err)
	}
	if result.Status != GatePassed {
		t.Fatalf("result.Status = %q, want %q", result.Status, GatePassed)
	}
}

func TestEvaluateReleaseGateRejectsMissingRequiredEvidenceTypes(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed, "command")
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireEvidenceTypes("command", "diff", "checkpoint"))
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if result.Status != GateRejected {
		t.Fatalf("result.Status = %q, want %q", result.Status, GateRejected)
	}
	if containsReason(result.Reasons, "verification evidence type command is required") {
		t.Fatalf("result.Reasons = %v, did not expect command rejection", result.Reasons)
	}
	for _, want := range []string{
		"verification evidence type diff is required",
		"verification evidence type checkpoint is required",
	} {
		if !containsReason(result.Reasons, want) {
			t.Fatalf("result.Reasons = %v, want %q", result.Reasons, want)
		}
	}
}

func TestEvaluateReleaseGatePassesRequiredEvidenceTypes(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed, "command", "diff", "checkpoint")
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireEvidenceTypes("checkpoint", "diff", "command", "diff"))
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	if result.Status != GatePassed {
		t.Fatalf("result.Status = %q, want %q", result.Status, GatePassed)
	}
}

func TestRequireEvidenceTypesRejectsEmptyType(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	_, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireEvidenceTypes("command", " "))
	if err == nil || !strings.Contains(err.Error(), "required evidence type is required") {
		t.Fatalf("EvaluateReleaseGate() error = %v, want required evidence type error", err)
	}
}

func TestEvaluateReleaseGateRejectsMissingRequiredCheckIDs(t *testing.T) {
	report := verificationReportWithChecks(t,
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
	)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireCheckIDs("unit-tests", "diff-check", "checkpoint-check"))
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if result.Status != GateRejected {
		t.Fatalf("result.Status = %q, want %q", result.Status, GateRejected)
	}
	if containsReason(result.Reasons, "required check unit-tests") {
		t.Fatalf("result.Reasons = %v, did not expect unit-tests rejection", result.Reasons)
	}
	for _, want := range []string{
		"required check diff-check is missing",
		"required check checkpoint-check is missing",
	} {
		if !containsReason(result.Reasons, want) {
			t.Fatalf("result.Reasons = %v, want %q", result.Reasons, want)
		}
	}
}

func TestEvaluateReleaseGateRejectsRequiredCheckIDsThatDidNotPass(t *testing.T) {
	report := verificationReportWithChecks(t,
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusFailed, "diff"),
		verificationCheck("docs-check", gopact.VerificationStatusSkipped, ""),
	)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireCheckIDs("unit-tests", "diff-check", "docs-check"))
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	for _, want := range []string{
		"required check diff-check failed",
		"required check docs-check skipped",
	} {
		if !containsReason(result.Reasons, want) {
			t.Fatalf("result.Reasons = %v, want %q", result.Reasons, want)
		}
	}
}

func TestEvaluateReleaseGatePassesRequiredCheckIDs(t *testing.T) {
	report := verificationReportWithChecks(t,
		verificationCheck("unit-tests", gopact.VerificationStatusPassed, "command"),
		verificationCheck("diff-check", gopact.VerificationStatusPassed, "diff"),
		verificationCheck("checkpoint-check", gopact.VerificationStatusPassed, "checkpoint"),
	)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireCheckIDs(" checkpoint-check ", "diff-check", "unit-tests", "diff-check"))
	if err != nil {
		t.Fatalf("EvaluateReleaseGate() error = %v", err)
	}
	if result.Status != GatePassed {
		t.Fatalf("result.Status = %q, want %q", result.Status, GatePassed)
	}
}

func TestEvaluateReleaseGateRequiresPassedCIGates(t *testing.T) {
	report := verificationReportWithChecks(t,
		ciGateCheck("core-ci", map[string]gopact.VerificationStatus{
			"unit": gopact.VerificationStatusPassed,
			"race": gopact.VerificationStatusPassed,
			"lint": gopact.VerificationStatusSkipped,
		}),
		ciGateWithoutStatusCheck("ci-without-status", "coverage"),
	)
	result, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireCIGates(" unit ", "race", "lint", "coverage", "security", "unit"))
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("EvaluateReleaseGate() error = %v, want ErrReleaseGateRejected", err)
	}
	if result.Status != GateRejected {
		t.Fatalf("result.Status = %q, want %q", result.Status, GateRejected)
	}
	for _, want := range []string{
		"required CI gate lint status skipped is not passed",
		"required CI gate coverage is missing",
		"required CI gate security is missing",
	} {
		if !containsReason(result.Reasons, want) {
			t.Fatalf("result.Reasons = %v, want %q", result.Reasons, want)
		}
	}
	if containsReason(result.Reasons, "required CI gate unit") ||
		containsReason(result.Reasons, "required CI gate race") {
		t.Fatalf("result.Reasons = %v, did not expect passed CI gate rejection", result.Reasons)
	}
}

func TestRequireCIGatesRejectsEmptyGate(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	_, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireCIGates("unit", " "))
	if err == nil || !strings.Contains(err.Error(), "required CI gate is required") {
		t.Fatalf("EvaluateReleaseGate() error = %v, want required CI gate error", err)
	}
}

func TestRequireCheckIDsRejectsEmptyID(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	_, err := EvaluateReleaseGate(GateInput{
		Mode:   ModeWrite,
		Report: report,
		Review: ReviewDecision{
			Status: ReviewApproved,
		},
	}, RequireCheckIDs("unit-tests", " "))
	if err == nil || !strings.Contains(err.Error(), "required check id is required") {
		t.Fatalf("EvaluateReleaseGate() error = %v, want required check id error", err)
	}
}

func verificationReport(t *testing.T, status gopact.VerificationStatus, evidenceTypes ...string) gopact.VerificationReport {
	t.Helper()

	if len(evidenceTypes) == 0 {
		evidenceTypes = []string{"command"}
	}
	evidence := make([]gopact.VerificationEvidence, 0, len(evidenceTypes))
	for _, evidenceType := range evidenceTypes {
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    evidenceType,
			Ref:     evidenceType + ":observed",
			Summary: "observed " + evidenceType,
		})
	}
	check := gopact.VerificationCheck{
		ID:       "unit-tests",
		Status:   status,
		Evidence: evidence,
	}
	if status == gopact.VerificationStatusSkipped {
		check.Evidence = nil
	}
	return verificationReportWithChecks(t, check)
}

func verificationReportWithChecks(t *testing.T, checks ...gopact.VerificationCheck) gopact.VerificationReport {
	t.Helper()

	recorder := gopact.NewVerificationRecorder()
	for _, check := range checks {
		if err := recorder.Record(check); err != nil {
			t.Fatalf("Record(check) error = %v", err)
		}
	}
	report, err := recorder.Report(gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
	})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	return report
}

func verificationCheck(id string, status gopact.VerificationStatus, evidenceType string) gopact.VerificationCheck {
	check := gopact.VerificationCheck{
		ID:     id,
		Status: status,
	}
	if status == gopact.VerificationStatusSkipped {
		return check
	}
	check.Evidence = []gopact.VerificationEvidence{
		{
			Type:    evidenceType,
			Ref:     evidenceType + ":observed",
			Summary: "observed " + evidenceType,
		},
	}
	return check
}

func ciGateCheck(id string, gates map[string]gopact.VerificationStatus) gopact.VerificationCheck {
	evidence := make([]gopact.VerificationEvidence, 0, len(gates))
	names := make([]string, 0, len(gates))
	for gate := range gates {
		names = append(names, gate)
	}
	sort.Strings(names)
	for _, gate := range names {
		status := gates[gate]
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    "ci_gate",
			Ref:     "ci-gate:" + gate,
			Summary: gate + " gate " + string(status),
			Metadata: map[string]any{
				"gate":   gate,
				"status": string(status),
			},
		})
	}
	return gopact.VerificationCheck{
		ID:       id,
		Status:   gopact.VerificationStatusPassed,
		Evidence: evidence,
	}
}

func ciGateWithoutStatusCheck(id, gate string) gopact.VerificationCheck {
	return gopact.VerificationCheck{
		ID:     id,
		Status: gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:    "ci_gate",
				Ref:     "ci-gate:" + gate,
				Summary: gate + " gate observed without explicit status",
				Metadata: map[string]any{
					"gate": gate,
				},
			},
		},
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, want) {
			return true
		}
	}
	return false
}
