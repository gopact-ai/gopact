package objectstore

import (
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRecordIndexConsistencyCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	report := IndexConsistencyReport{
		ThreadID:         "thread-1",
		IndexThreadID:    "thread-1",
		IndexExists:      true,
		IndexedRecordIDs: []string{"checkpoint-1"},
		ValidRecordIDs:   []string{"checkpoint-1"},
		Consistent:       true,
	}
	metadata := map[string]any{"source": "scheduled-check"}

	if err := RecordIndexConsistencyCheck(recorder, IndexConsistencySnapshot{
		Report:   report,
		Metadata: metadata,
	}); err != nil {
		t.Fatalf("RecordIndexConsistencyCheck() error = %v", err)
	}
	report.IndexedRecordIDs[0] = "mutated"
	metadata["source"] = "mutated"

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "checkpoint-objectstore-index:thread-1" ||
		check.Name != "checkpoint objectstore index" ||
		check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed objectstore index check", check)
	}
	if len(check.Evidence) != 1 ||
		check.Evidence[0].Type != VerificationEvidenceTypeIndexConsistency ||
		check.Evidence[0].Ref != "thread-1" {
		t.Fatalf("evidence = %+v, want index consistency evidence", check.Evidence)
	}
	if check.Metadata["thread_id"] != "thread-1" ||
		check.Metadata["index_thread_id"] != "thread-1" ||
		check.Metadata["index_exists"] != true ||
		check.Metadata["consistent"] != true ||
		check.Metadata["indexed_record_count"] != 1 ||
		check.Metadata["valid_record_count"] != 1 ||
		check.Metadata["source"] != "scheduled-check" {
		t.Fatalf("metadata = %+v, want index report metadata", check.Metadata)
	}
	indexedIDs, ok := check.Metadata["indexed_record_ids"].([]string)
	if !ok || len(indexedIDs) != 1 || indexedIDs[0] != "checkpoint-1" {
		t.Fatalf("indexed_record_ids = %#v, want copied checkpoint-1", check.Metadata["indexed_record_ids"])
	}
}

func TestRecordIndexConsistencyCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	report := IndexConsistencyReport{
		ThreadID:             "thread-1",
		IndexThreadID:        "thread-1",
		IndexExists:          true,
		IndexedRecordIDs:     []string{"checkpoint-1", "checkpoint-1", "missing", "wrong-thread"},
		ValidRecordIDs:       []string{"checkpoint-1"},
		DuplicateRecordIDs:   []string{"checkpoint-1"},
		MissingRecordIDs:     []string{"missing"},
		WrongThreadRecordIDs: []string{"wrong-thread"},
		Consistent:           false,
	}

	err := RecordIndexConsistencyCheck(recorder, IndexConsistencySnapshot{Report: report})
	if !errors.Is(err, ErrIndexConsistencyCheckFailed) {
		t.Fatalf("RecordIndexConsistencyCheck() error = %v, want ErrIndexConsistencyCheckFailed", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["duplicate_record_count"] != 1 ||
		check.Metadata["missing_record_count"] != 1 ||
		check.Metadata["wrong_thread_record_count"] != 1 {
		t.Fatalf("metadata = %+v, want issue counts", check.Metadata)
	}
}

func TestRecordIndexConsistencyCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	report := IndexConsistencyReport{
		ThreadID:             "thread-1",
		IndexThreadID:        "thread-1",
		IndexExists:          true,
		IndexedRecordIDs:     []string{"checkpoint-1"},
		ValidRecordIDs:       []string{"checkpoint-1"},
		DuplicateRecordIDs:   []string{"duplicate-1"},
		MissingRecordIDs:     []string{"missing-1"},
		WrongThreadRecordIDs: []string{"wrong-thread-1"},
		Consistent:           false,
		Repaired:             true,
		ThreadIDMismatch:     true,
	}

	err := RecordIndexConsistencyCheck(recorder, IndexConsistencySnapshot{
		Ref:    "index:thread-1",
		Report: report,
		Metadata: map[string]any{
			"ref":                       "forged-ref",
			"thread_id":                 "forged-thread",
			"index_thread_id":           "forged-index-thread",
			"index_exists":              false,
			"consistent":                true,
			"repaired":                  false,
			"thread_id_mismatch":        false,
			"indexed_record_ids":        []string{"forged-indexed"},
			"indexed_record_count":      99,
			"valid_record_ids":          []string{"forged-valid"},
			"valid_record_count":        99,
			"duplicate_record_ids":      []string{"forged-duplicate"},
			"duplicate_record_count":    99,
			"missing_record_ids":        []string{"forged-missing"},
			"missing_record_count":      99,
			"wrong_thread_record_ids":   []string{"forged-wrong-thread"},
			"wrong_thread_record_count": 99,
			"source":                    "scheduled-check",
		},
	})
	if !errors.Is(err, ErrIndexConsistencyCheckFailed) {
		t.Fatalf("RecordIndexConsistencyCheck() error = %v, want ErrIndexConsistencyCheckFailed", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "index:thread-1" ||
		metadata["thread_id"] != "thread-1" ||
		metadata["index_thread_id"] != "thread-1" ||
		metadata["index_exists"] != true ||
		metadata["consistent"] != false ||
		metadata["repaired"] != true ||
		metadata["thread_id_mismatch"] != true ||
		metadata["indexed_record_count"] != 1 ||
		metadata["valid_record_count"] != 1 ||
		metadata["duplicate_record_count"] != 1 ||
		metadata["missing_record_count"] != 1 ||
		metadata["wrong_thread_record_count"] != 1 {
		t.Fatalf("metadata = %+v, want canonical objectstore index fields preserved", metadata)
	}
	assertStringSliceMetadata(t, metadata, "indexed_record_ids", []string{"checkpoint-1"})
	assertStringSliceMetadata(t, metadata, "valid_record_ids", []string{"checkpoint-1"})
	assertStringSliceMetadata(t, metadata, "duplicate_record_ids", []string{"duplicate-1"})
	assertStringSliceMetadata(t, metadata, "missing_record_ids", []string{"missing-1"})
	assertStringSliceMetadata(t, metadata, "wrong_thread_record_ids", []string{"wrong-thread-1"})
	if metadata["source"] != "scheduled-check" {
		t.Fatalf("metadata = %+v, want non-conflicting caller metadata preserved", metadata)
	}
}

func TestRecordIndexConsistencyCheckRecordsStorageError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	storageErr := errors.New("object store unavailable")

	err := RecordIndexConsistencyCheck(recorder, IndexConsistencySnapshot{
		Ref: "thread-1",
		Err: storageErr,
	})
	if !errors.Is(err, ErrIndexConsistencyCheckFailed) || !errors.Is(err, storageErr) {
		t.Fatalf("RecordIndexConsistencyCheck() error = %v, want check failure and storage error", err)
	}
	if checks := recorder.Checks(); len(checks) != 1 || checks[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}
}

func TestRecordIndexConsistencyCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordIndexConsistencyCheck(recorder, IndexConsistencySnapshot{
		Ref:     "maintenance-window",
		Skipped: true,
		Summary: "index check skipped during maintenance",
	}); err != nil {
		t.Fatalf("RecordIndexConsistencyCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped check", checks)
	}
}

func TestRecordIndexConsistencyCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordIndexConsistencyCheck(nil, IndexConsistencySnapshot{Report: IndexConsistencyReport{ThreadID: "thread-1"}}); err == nil {
		t.Fatal("RecordIndexConsistencyCheck(nil) error = nil, want error")
	}
	if err := RecordIndexConsistencyCheck(recorder, IndexConsistencySnapshot{}); !errors.Is(err, ErrIndexConsistencyReportRequired) {
		t.Fatalf("RecordIndexConsistencyCheck(empty) error = %v, want ErrIndexConsistencyReportRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}

func assertStringSliceMetadata(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()

	got, ok := metadata[key].([]string)
	if !ok {
		t.Fatalf("metadata[%q] = %#v, want []string", key, metadata[key])
	}
	if len(got) != len(want) {
		t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
		}
	}
}
