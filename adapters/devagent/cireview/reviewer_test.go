package cireview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

func TestReviewerReviewApprovesPassedReport(t *testing.T) {
	reviewer, err := New(
		WithReviewer("github-actions"),
		WithRequiredChecks("unit-tests", "lint"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := devagent.Review(context.Background(), reviewer, devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Patch: devagent.PatchProposal{
			ID:      "patch-1",
			Summary: "add ci reviewer adapter",
		},
		Report: verificationReport(t,
			verificationCheck("unit-tests", gopact.VerificationStatusPassed),
			verificationCheck("lint", gopact.VerificationStatusPassed),
		),
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:     "entropy-1",
				Status: gopact.VerificationStatusPassed,
			},
		},
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewApproved || decision.Reviewer != "github-actions" {
		t.Fatalf("decision = %+v, want approved github-actions decision", decision)
	}
	if decision.Summary != "ci verification passed" {
		t.Fatalf("summary = %q, want ci verification passed", decision.Summary)
	}
	if decision.Metadata["adapter"] != "cireview" ||
		decision.Metadata["report_status"] != "passed" ||
		decision.Metadata["passed_count"] != 2 ||
		decision.Metadata["required_checks"] == nil {
		t.Fatalf("metadata = %+v, want ci review metadata", decision.Metadata)
	}
}

func TestReviewerReviewRejectsFailedReport(t *testing.T) {
	reviewer, err := New(WithReviewer("buildkite"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Report: verificationReport(t,
			verificationCheck("unit-tests", gopact.VerificationStatusFailed),
			verificationCheck("lint", gopact.VerificationStatusPassed),
		),
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected || decision.Reviewer != "buildkite" {
		t.Fatalf("decision = %+v, want rejected buildkite decision", decision)
	}
	if !strings.Contains(decision.Summary, "verification status failed is not passed") ||
		!strings.Contains(decision.Summary, "check unit-tests failed") {
		t.Fatalf("summary = %q, want verification failure reasons", decision.Summary)
	}
	failed, ok := decision.Metadata["failed_checks"].([]string)
	if !ok || len(failed) != 1 || failed[0] != "unit-tests" {
		t.Fatalf("failed_checks = %#v, want unit-tests", decision.Metadata["failed_checks"])
	}
}

func TestReviewerReviewRejectsMissingRequiredCheck(t *testing.T) {
	reviewer, err := New(WithRequiredChecks("unit-tests", "race"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Report: verificationReport(t,
			verificationCheck("unit-tests", gopact.VerificationStatusPassed),
		),
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected || decision.Reviewer != "ci" {
		t.Fatalf("decision = %+v, want default ci rejected decision", decision)
	}
	if !strings.Contains(decision.Summary, "required check race is missing") {
		t.Fatalf("summary = %q, want missing required check reason", decision.Summary)
	}
	missing, ok := decision.Metadata["missing_checks"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "race" {
		t.Fatalf("missing_checks = %#v, want race", decision.Metadata["missing_checks"])
	}
}

func TestReviewerReviewRejectsMissingRequiredCIGate(t *testing.T) {
	reviewer, err := New(WithRequiredCIGates("unit", "race"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Report: verificationReport(t,
			ciGateVerificationCheck("github-actions", ciGateEvidence{
				gate:   "unit",
				status: gopact.VerificationStatusPassed,
			}),
		),
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected {
		t.Fatalf("decision = %+v, want rejected decision", decision)
	}
	if !strings.Contains(decision.Summary, "required CI gate race is missing") {
		t.Fatalf("summary = %q, want missing required CI gate reason", decision.Summary)
	}
	required, ok := decision.Metadata["required_ci_gates"].([]string)
	if !ok || len(required) != 2 || required[0] != "unit" || required[1] != "race" {
		t.Fatalf("required_ci_gates = %#v, want unit race", decision.Metadata["required_ci_gates"])
	}
	missing, ok := decision.Metadata["missing_ci_gates"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "race" {
		t.Fatalf("missing_ci_gates = %#v, want race", decision.Metadata["missing_ci_gates"])
	}
}

func TestReviewerReviewRejectsFailedRequiredCIGate(t *testing.T) {
	reviewer, err := New(WithRequiredCIGates("lint"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Report: verificationReport(t,
			ciGateVerificationCheck("github-actions", ciGateEvidence{
				gate:   "lint",
				status: gopact.VerificationStatusFailed,
			}),
		),
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected {
		t.Fatalf("decision = %+v, want rejected decision", decision)
	}
	if !strings.Contains(decision.Summary, "required CI gate lint status failed is not passed") {
		t.Fatalf("summary = %q, want failed required CI gate reason", decision.Summary)
	}
	failed, ok := decision.Metadata["failed_ci_gates"].([]string)
	if !ok || len(failed) != 1 || failed[0] != "lint" {
		t.Fatalf("failed_ci_gates = %#v, want lint", decision.Metadata["failed_ci_gates"])
	}
}

func TestReviewerReviewRejectsHighEntropyFinding(t *testing.T) {
	reviewer, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{
		Mode:   devagent.ModeWrite,
		Report: verificationReport(t, verificationCheck("unit-tests", gopact.VerificationStatusPassed)),
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:     "entropy-1",
				Status: gopact.VerificationStatusFailed,
				Findings: []gopact.EntropyFinding{
					{
						ID:       "finding-1",
						Category: gopact.EntropySecurity,
						Severity: gopact.EntropySeverityHigh,
						Summary:  "sensitive file changed",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected {
		t.Fatalf("decision = %+v, want rejected decision", decision)
	}
	if !strings.Contains(decision.Summary, "entropy audit entropy-1 status failed") ||
		!strings.Contains(decision.Summary, "entropy finding finding-1 severity high exceeds medium") {
		t.Fatalf("summary = %q, want entropy rejection reasons", decision.Summary)
	}
	if decision.Metadata["max_entropy_severity"] != "high" {
		t.Fatalf("max_entropy_severity = %#v, want high", decision.Metadata["max_entropy_severity"])
	}
}

func TestReviewerReviewRequiresReport(t *testing.T) {
	reviewer, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{Mode: devagent.ModeWrite})
	if !errors.Is(err, ErrReportRequired) {
		t.Fatalf("Review() error = %v, want ErrReportRequired", err)
	}
}

func TestReviewerReviewHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reviewer, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(ctx, devagent.ReviewInput{
		Report: verificationReport(t, verificationCheck("unit-tests", gopact.VerificationStatusPassed)),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Review() error = %v, want context.Canceled", err)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	if reviewer, err := New(WithReviewer("   ")); reviewer != nil || !errors.Is(err, ErrReviewerRequired) {
		t.Fatalf("New(empty reviewer) reviewer=%v err=%v, want ErrReviewerRequired", reviewer, err)
	}
	if reviewer, err := New(WithRequiredChecks("unit-tests", " ")); reviewer != nil || !errors.Is(err, ErrRequiredCheckRequired) {
		t.Fatalf("New(empty required check) reviewer=%v err=%v, want ErrRequiredCheckRequired", reviewer, err)
	}
	if reviewer, err := New(WithRequiredChecks()); reviewer != nil || !errors.Is(err, ErrRequiredCheckRequired) {
		t.Fatalf("New(no required checks) reviewer=%v err=%v, want ErrRequiredCheckRequired", reviewer, err)
	}
	if reviewer, err := New(WithRequiredCIGates("unit", " ")); reviewer != nil || !errors.Is(err, ErrRequiredCIGateRequired) {
		t.Fatalf("New(empty required CI gate) reviewer=%v err=%v, want ErrRequiredCIGateRequired", reviewer, err)
	}
	if reviewer, err := New(WithRequiredCIGates()); reviewer != nil || !errors.Is(err, ErrRequiredCIGateRequired) {
		t.Fatalf("New(no required CI gates) reviewer=%v err=%v, want ErrRequiredCIGateRequired", reviewer, err)
	}
	if reviewer, err := New(WithMaxEntropySeverity(gopact.EntropySeverity("severe"))); reviewer != nil ||
		!errors.Is(err, ErrInvalidEntropySeverity) {
		t.Fatalf("New(invalid entropy severity) reviewer=%v err=%v, want ErrInvalidEntropySeverity", reviewer, err)
	}
}

func verificationReport(t *testing.T, checks ...gopact.VerificationCheck) gopact.VerificationReport {
	t.Helper()

	recorder := gopact.NewVerificationRecorder()
	for _, check := range checks {
		if err := recorder.Record(check); err != nil {
			t.Fatalf("Record(%s) error = %v", check.ID, err)
		}
	}
	report, err := recorder.Report(gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: gopact.RunCompleted,
	})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	return report
}

func verificationCheck(id string, status gopact.VerificationStatus) gopact.VerificationCheck {
	check := gopact.VerificationCheck{
		ID:     id,
		Status: status,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:    "command",
				Ref:     id,
				Summary: "observed ci result",
			},
		},
	}
	if status == gopact.VerificationStatusSkipped {
		check.Evidence = nil
	}
	return check
}

type ciGateEvidence struct {
	gate   string
	status gopact.VerificationStatus
}

func ciGateVerificationCheck(id string, gates ...ciGateEvidence) gopact.VerificationCheck {
	check := gopact.VerificationCheck{
		ID:     id,
		Status: gopact.VerificationStatusPassed,
	}
	for _, gate := range gates {
		check.Evidence = append(check.Evidence, gopact.VerificationEvidence{
			Type:    "ci_gate",
			Ref:     gate.gate,
			Summary: "observed ci gate",
			Metadata: map[string]any{
				"gate":   gate.gate,
				"status": string(gate.status),
			},
		})
	}
	return check
}
