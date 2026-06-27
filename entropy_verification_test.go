package gopact

import (
	"errors"
	"testing"
	"time"
)

func TestRecordEntropyAuditCheckRecordsPartialAuditAsPassedCheck(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC)
	audit := EntropyAudit{
		ID:     "entropy-1",
		Status: VerificationStatusPartial,
		IDs: RuntimeIDs{
			RunID:    "run-1",
			ThreadID: "thread-1",
			UserID:   "user-1",
		},
		CreatedAt: createdAt,
		Findings: []EntropyFinding{
			{
				ID:       "finding-1",
				Category: EntropyStaleDocs,
				Severity: EntropySeverityLow,
				Summary:  "source changed without docs",
			},
		},
		Metadata: map[string]any{"source": "devagent"},
	}

	if err := RecordEntropyAuditCheck(recorder, audit); err != nil {
		t.Fatalf("RecordEntropyAuditCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "entropy-audit:entropy-1" || check.Name != "entropy audit" || check.Status != VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed entropy audit check", check)
	}
	if len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypeEntropyAudit ||
		check.Evidence[0].Ref != "entropy-1" {
		t.Fatalf("evidence = %+v, want entropy audit evidence", check.Evidence)
	}
	if check.Metadata["audit_status"] != string(VerificationStatusPartial) ||
		check.Metadata["finding_count"] != 1 ||
		check.Metadata["max_entropy_severity"] != string(EntropySeverityLow) ||
		check.Metadata["run_id"] != "run-1" ||
		check.Metadata["thread_id"] != "thread-1" ||
		check.Metadata["user_id"] != "user-1" ||
		check.Metadata["source"] != "devagent" {
		t.Fatalf("metadata = %+v, want entropy audit metadata", check.Metadata)
	}
	findings, ok := check.Metadata["findings"].([]map[string]any)
	if !ok || len(findings) != 1 || findings[0]["id"] != "finding-1" {
		t.Fatalf("metadata findings = %#v, want copied finding summary", check.Metadata["findings"])
	}
}

func TestRecordEntropyAuditCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC)
	audit := EntropyAudit{
		ID:     "entropy-1",
		Status: VerificationStatusPartial,
		IDs: RuntimeIDs{
			RunID:    "run-1",
			ThreadID: "thread-1",
			UserID:   "user-1",
		},
		CreatedAt: createdAt,
		Findings: []EntropyFinding{
			{
				ID:       "finding-1",
				Category: EntropyStaleDocs,
				Severity: EntropySeverityLow,
				Summary:  "source changed without docs",
			},
		},
		Metadata: map[string]any{
			"ref":                  "forged-ref",
			"audit_id":             "forged-audit",
			"audit_status":         string(VerificationStatusFailed),
			"finding_count":        999,
			"run_id":               "forged-run",
			"thread_id":            "forged-thread",
			"user_id":              "forged-user",
			"created_at":           "forged-created-at",
			"max_entropy_severity": string(EntropySeverityCritical),
			"findings":             []map[string]any{{"id": "forged-finding"}},
			"source":               "devagent",
		},
	}

	if err := RecordEntropyAuditCheck(recorder, audit); err != nil {
		t.Fatalf("RecordEntropyAuditCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	metadata := check.Metadata
	if metadata["ref"] != "entropy-1" ||
		metadata["audit_id"] != "entropy-1" ||
		metadata["audit_status"] != string(VerificationStatusPartial) ||
		metadata["finding_count"] != 1 ||
		metadata["run_id"] != "run-1" ||
		metadata["thread_id"] != "thread-1" ||
		metadata["user_id"] != "user-1" ||
		metadata["created_at"] != createdAt.Format(time.RFC3339Nano) ||
		metadata["max_entropy_severity"] != string(EntropySeverityLow) {
		t.Fatalf("metadata = %+v, want canonical entropy audit fields preserved", metadata)
	}
	findings, ok := metadata["findings"].([]map[string]any)
	if !ok || len(findings) != 1 || findings[0]["id"] != "finding-1" {
		t.Fatalf("metadata findings = %#v, want canonical findings", metadata["findings"])
	}
	if metadata["source"] != "devagent" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}

	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["ref"] != "entropy-1" ||
		evidenceMetadata["audit_id"] != "entropy-1" ||
		evidenceMetadata["audit_status"] != string(VerificationStatusPartial) ||
		evidenceMetadata["finding_count"] != 1 ||
		evidenceMetadata["run_id"] != "run-1" ||
		evidenceMetadata["thread_id"] != "thread-1" ||
		evidenceMetadata["user_id"] != "user-1" ||
		evidenceMetadata["created_at"] != createdAt.Format(time.RFC3339Nano) ||
		evidenceMetadata["max_entropy_severity"] != string(EntropySeverityLow) {
		t.Fatalf("evidence metadata = %+v, want canonical entropy audit fields preserved", evidenceMetadata)
	}
	evidenceFindings, ok := evidenceMetadata["findings"].([]map[string]any)
	if !ok || len(evidenceFindings) != 1 || evidenceFindings[0]["id"] != "finding-1" {
		t.Fatalf("evidence metadata findings = %#v, want canonical findings", evidenceMetadata["findings"])
	}
	if evidenceMetadata["source"] != "devagent" {
		t.Fatalf("evidence metadata = %+v, want supplemental metadata preserved", evidenceMetadata)
	}
}

func TestRecordEntropyAuditCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordEntropyAuditCheck(recorder, EntropyAudit{
		ID:     "entropy-1",
		Status: VerificationStatusFailed,
		Findings: []EntropyFinding{
			{
				ID:       "finding-1",
				Category: EntropySecurity,
				Severity: EntropySeverityHigh,
				Summary:  "sensitive file changed",
			},
		},
	})
	if !errors.Is(err, ErrEntropyAuditFailed) {
		t.Fatalf("RecordEntropyAuditCheck() error = %v, want ErrEntropyAuditFailed", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed entropy audit check", checks)
	}
	if checks[0].Metadata["max_entropy_severity"] != string(EntropySeverityHigh) {
		t.Fatalf("metadata = %+v, want high max entropy severity", checks[0].Metadata)
	}
}

func TestRecordEntropyAuditCheckRecordsSkippedCheck(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordEntropyAuditCheck(recorder, EntropyAudit{
		ID:     "entropy-1",
		Status: VerificationStatusSkipped,
	}); err != nil {
		t.Fatalf("RecordEntropyAuditCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped entropy audit check", checks)
	}
	if len(checks[0].Evidence) != 0 {
		t.Fatalf("evidence = %+v, want none for skipped check", checks[0].Evidence)
	}
}

func TestRecordEntropyAuditCheckRejectsInvalidInput(t *testing.T) {
	recorder := NewVerificationRecorder()
	if err := RecordEntropyAuditCheck(nil, EntropyAudit{ID: "entropy-1", Status: VerificationStatusPassed}); err == nil {
		t.Fatal("RecordEntropyAuditCheck(nil) error = nil, want error")
	}
	if err := RecordEntropyAuditCheck(recorder, EntropyAudit{Status: VerificationStatusPassed}); err == nil {
		t.Fatal("RecordEntropyAuditCheck(missing id) error = nil, want validation error")
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("checks = %+v, want none after invalid audit", recorder.Checks())
	}
}
