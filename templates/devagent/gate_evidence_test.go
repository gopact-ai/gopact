package devagent

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRecordReleaseGateCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseGateCheck(recorder, GateResult{
		Status:             GatePassed,
		Mode:               ModeWrite,
		ReportStatus:       gopact.VerificationStatusPassed,
		MaxEntropySeverity: gopact.EntropySeverityLow,
		ReviewStatus:       ReviewApproved,
		Metadata:           map[string]any{"release": "docs"},
	}); err != nil {
		t.Fatalf("RecordReleaseGateCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != VerificationCheckReleaseGate || check.Name != "release gate" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed release gate check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeReleaseGate || check.Evidence[0].Ref != "release-gate:write" {
		t.Fatalf("evidence = %+v, want release gate evidence", check.Evidence)
	}
	if check.Metadata["gate_status"] != string(GatePassed) ||
		check.Metadata["mode"] != string(ModeWrite) ||
		check.Metadata["report_status"] != string(gopact.VerificationStatusPassed) ||
		check.Metadata["max_entropy_severity"] != string(gopact.EntropySeverityLow) ||
		check.Metadata["review_status"] != string(ReviewApproved) ||
		check.Metadata["release"] != "docs" {
		t.Fatalf("metadata = %+v, want release gate metadata", check.Metadata)
	}
}

func TestRecordReleaseGateCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseGateCheck(recorder, GateResult{
		Status:             GatePassed,
		Mode:               ModeWrite,
		ReportStatus:       gopact.VerificationStatusPassed,
		MaxEntropySeverity: gopact.EntropySeverityLow,
		ReviewStatus:       ReviewApproved,
		Reasons:            []string{"all required evidence passed"},
		Metadata: map[string]any{
			"gate_status":          string(GateRejected),
			"mode":                 string(ModePlan),
			"report_status":        string(gopact.VerificationStatusFailed),
			"max_entropy_severity": string(gopact.EntropySeverityCritical),
			"review_status":        string(ReviewRejected),
			"reasons":              []string{"forged reason"},
			"release":              "docs",
		},
	}); err != nil {
		t.Fatalf("RecordReleaseGateCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	if check.Metadata["gate_status"] != string(GatePassed) ||
		check.Metadata["mode"] != string(ModeWrite) ||
		check.Metadata["report_status"] != string(gopact.VerificationStatusPassed) ||
		check.Metadata["max_entropy_severity"] != string(gopact.EntropySeverityLow) ||
		check.Metadata["review_status"] != string(ReviewApproved) {
		t.Fatalf("metadata = %+v, want canonical release gate fields preserved", check.Metadata)
	}
	reasons, ok := check.Metadata["reasons"].([]string)
	if !ok || !reflect.DeepEqual(reasons, []string{"all required evidence passed"}) {
		t.Fatalf("metadata reasons = %#v, want canonical reasons", check.Metadata["reasons"])
	}
	if check.Metadata["release"] != "docs" {
		t.Fatalf("metadata = %+v, want non-conflicting caller metadata preserved", check.Metadata)
	}
}

func TestRecordReleaseGateCheckRecordsRejectedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordReleaseGateCheck(recorder, GateResult{
		Status:       GateRejected,
		Mode:         ModeWrite,
		ReportStatus: gopact.VerificationStatusFailed,
		ReviewStatus: ReviewApproved,
		Reasons:      []string{"verification status failed is not passed", "entropy finding finding-1 severity high exceeds medium"},
	})
	if !errors.Is(err, ErrReleaseGateRejected) {
		t.Fatalf("RecordReleaseGateCheck() error = %v, want ErrReleaseGateRejected", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	reasons, ok := check.Metadata["reasons"].([]string)
	if !ok || !reflect.DeepEqual(reasons, []string{"verification status failed is not passed", "entropy finding finding-1 severity high exceeds medium"}) {
		t.Fatalf("metadata reasons = %#v, want copied reasons", check.Metadata["reasons"])
	}
}

func TestRecordReleaseGateCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseGateCheck(recorder, GateResult{
		Status:  GateSkipped,
		Mode:    ModePlan,
		Reasons: []string{"release gate applies only to write mode"},
	}); err != nil {
		t.Fatalf("RecordReleaseGateCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped release gate check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "release-gate:plan" {
		t.Fatalf("evidence = %+v, want skipped release gate evidence", checks[0].Evidence)
	}
}

func TestRecordReleaseGateCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReleaseGateCheck(nil, GateResult{Status: GatePassed, Mode: ModeWrite}); err == nil {
		t.Fatal("RecordReleaseGateCheck(nil) error = nil, want error")
	}
	if err := RecordReleaseGateCheck(recorder, GateResult{Mode: ModeWrite}); !errors.Is(err, ErrReleaseGateStatusRequired) {
		t.Fatalf("RecordReleaseGateCheck(missing status) error = %v, want ErrReleaseGateStatusRequired", err)
	}
	if err := RecordReleaseGateCheck(recorder, GateResult{Status: GateStatus("maybe"), Mode: ModeWrite}); !errors.Is(err, ErrReleaseGateStatusRequired) {
		t.Fatalf("RecordReleaseGateCheck(invalid status) error = %v, want ErrReleaseGateStatusRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
