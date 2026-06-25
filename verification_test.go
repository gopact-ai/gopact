package gopact

import "testing"

func TestBuildVerificationReportSummarizesChecks(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: RunCompleted,
		Steps: []StepSnapshot{
			{ID: "step-1", Step: 1, Node: "verify", Phase: StepCompleted},
		},
	}
	checks := []VerificationCheck{
		{
			ID:      "unit-tests",
			Name:    "unit tests",
			Status:  VerificationStatusPassed,
			Summary: "go test passed",
			Evidence: []VerificationEvidence{
				{Type: "command", Ref: "go test ./... -count=1", Summary: "exit 0"},
			},
		},
		{
			ID:      "review",
			Name:    "code review",
			Status:  VerificationStatusFailed,
			Summary: "review found a blocker",
			Evidence: []VerificationEvidence{
				{Type: "review", Ref: "review-1", Summary: "missing policy coverage"},
			},
		},
	}

	report, err := BuildVerificationReport(export, checks)
	if err != nil {
		t.Fatalf("BuildVerificationReport() error = %v", err)
	}
	if report.Version != VerificationReportVersion {
		t.Fatalf("Version = %d, want %d", report.Version, VerificationReportVersion)
	}
	if report.IDs.RunID != "run-1" || report.IDs.ThreadID != "thread-1" || report.Outcome != RunCompleted {
		t.Fatalf("report run identity = %+v/%q, want run export identity", report.IDs, report.Outcome)
	}
	if report.Status != VerificationStatusFailed {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	if report.PassedCount != 1 || report.FailedCount != 1 || report.SkippedCount != 0 {
		t.Fatalf("counts = passed:%d failed:%d skipped:%d, want 1/1/0", report.PassedCount, report.FailedCount, report.SkippedCount)
	}
	if len(report.Checks) != 2 || report.Checks[0].Evidence[0].Ref != "go test ./... -count=1" {
		t.Fatalf("checks = %+v, want copied verification checks", report.Checks)
	}

	checks[0].Evidence[0].Ref = "mutated"
	if report.Checks[0].Evidence[0].Ref != "go test ./... -count=1" {
		t.Fatal("BuildVerificationReport() returned mutable backing evidence")
	}
}

func TestBuildVerificationReportMarksPartialWhenChecksAreSkipped(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunCompleted,
	}

	report, err := BuildVerificationReport(export, []VerificationCheck{
		{
			ID:       "unit-tests",
			Status:   VerificationStatusPassed,
			Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
		},
		{
			ID:      "manual-review",
			Status:  VerificationStatusSkipped,
			Summary: "not requested",
		},
	})
	if err != nil {
		t.Fatalf("BuildVerificationReport() error = %v", err)
	}
	if report.Status != VerificationStatusPartial {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
	if report.PassedCount != 1 || report.SkippedCount != 1 {
		t.Fatalf("counts = passed:%d skipped:%d, want 1/1", report.PassedCount, report.SkippedCount)
	}
}

func TestBuildVerificationReportRejectsInvalidInput(t *testing.T) {
	validExport := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunCompleted,
	}
	validCheck := VerificationCheck{
		ID:       "unit-tests",
		Status:   VerificationStatusPassed,
		Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
	}

	tests := []struct {
		name   string
		export RunExport
		checks []VerificationCheck
	}{
		{
			name:   "invalid export",
			export: RunExport{},
			checks: []VerificationCheck{validCheck},
		},
		{
			name:   "empty checks",
			export: validExport,
		},
		{
			name:   "missing check id",
			export: validExport,
			checks: []VerificationCheck{{
				Status:   VerificationStatusPassed,
				Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
			}},
		},
		{
			name:   "invalid status",
			export: validExport,
			checks: []VerificationCheck{{
				ID:     "unit-tests",
				Status: VerificationStatus("unknown"),
			}},
		},
		{
			name:   "passed check without evidence",
			export: validExport,
			checks: []VerificationCheck{{
				ID:     "unit-tests",
				Status: VerificationStatusPassed,
			}},
		},
		{
			name:   "failed check without evidence",
			export: validExport,
			checks: []VerificationCheck{{
				ID:     "review",
				Status: VerificationStatusFailed,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildVerificationReport(tt.export, tt.checks); err == nil {
				t.Fatal("BuildVerificationReport() error = nil, want validation error")
			}
		})
	}
}

func TestVerificationRecorderRecordsChecksAndBuildsReport(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: RunCompleted,
	}
	recorder := NewVerificationRecorder()

	if err := recorder.Record(VerificationCheck{
		ID:       "unit-tests",
		Status:   VerificationStatusPassed,
		Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
	}); err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}
	if err := recorder.Record(VerificationCheck{
		ID:      "manual-review",
		Status:  VerificationStatusSkipped,
		Summary: "not requested",
	}); err != nil {
		t.Fatalf("Record(second) error = %v", err)
	}

	report, err := recorder.Report(export)
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if report.Status != VerificationStatusPartial || report.PassedCount != 1 || report.SkippedCount != 1 {
		t.Fatalf("report status/counts = %q %d/%d, want partial 1/1", report.Status, report.PassedCount, report.SkippedCount)
	}
	if report.IDs.RunID != "run-1" || report.IDs.ThreadID != "thread-1" {
		t.Fatalf("report ids = %+v, want run/thread ids", report.IDs)
	}
}

func TestVerificationRecorderReturnsCopiedChecks(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := recorder.Record(VerificationCheck{
		ID:       "unit-tests",
		Status:   VerificationStatusPassed,
		Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
		Metadata: map[string]any{"suite": "unit"},
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	checks := recorder.Checks()
	checks[0].Evidence[0].Ref = "mutated"
	checks[0].Metadata["suite"] = "mutated"

	checksAgain := recorder.Checks()
	if checksAgain[0].Evidence[0].Ref != "go test ./..." || checksAgain[0].Metadata["suite"] != "unit" {
		t.Fatalf("Checks() returned mutable backing data: %+v", checksAgain[0])
	}
}

func TestVerificationRecorderRejectsInvalidCheck(t *testing.T) {
	recorder := NewVerificationRecorder()

	if err := recorder.Record(VerificationCheck{ID: "unit-tests", Status: VerificationStatusPassed}); err == nil {
		t.Fatal("Record() error = nil, want validation error")
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected record", len(recorder.Checks()))
	}
}

func TestVerificationRecorderReportRejectsNilRecorder(t *testing.T) {
	var recorder *VerificationRecorder
	export := RunExport{Version: RunExportVersion, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted}

	if _, err := recorder.Report(export); err == nil {
		t.Fatal("Report() error = nil, want nil recorder error")
	}
}
