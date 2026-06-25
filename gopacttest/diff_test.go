package gopacttest

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRecordDiffCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordDiffCheck(recorder, DiffSnapshot{
		ID:         "patch-1",
		Name:       "observed patch diff",
		Ref:        "patch-1.diff",
		Diff:       "diff --git a/README.md b/README.md\n",
		Insertions: 12,
		Deletions:  3,
		Metadata:   map[string]any{"mode": "write"},
	}); err != nil {
		t.Fatalf("RecordDiffCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "patch-1" || check.Name != "observed patch diff" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed diff check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeDiff || check.Evidence[0].Ref != "patch-1.diff" {
		t.Fatalf("evidence = %+v, want diff evidence", check.Evidence)
	}
	if check.Metadata["diff"] != "diff --git a/README.md b/README.md\n" ||
		check.Metadata["insertions"] != 12 ||
		check.Metadata["deletions"] != 3 ||
		check.Metadata["mode"] != "write" {
		t.Fatalf("metadata = %+v, want diff stats and custom metadata", check.Metadata)
	}
	files, ok := check.Metadata["files"].([]string)
		t.Fatalf("metadata files = %#v, want copied files", check.Metadata["files"])
	}
}

func TestRecordDiffCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	err := RecordDiffCheck(recorder, DiffSnapshot{
		ID:         "patch-1",
		Ref:        "patch-1.diff",
		Diff:       "diff --git a/README.md b/README.md\n",
		Files:      []string{"README.md"},
		Insertions: 12,
		Deletions:  3,
		Metadata: map[string]any{
			"ref":        "forged-ref",
			"diff":       "forged diff",
			"files":      []string{"forged.go"},
			"file_count": 99,
			"insertions": 99,
			"deletions":  99,
			"mode":       "write",
		},
	})
	if err != nil {
		t.Fatalf("RecordDiffCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "patch-1.diff" ||
		metadata["diff"] != "diff --git a/README.md b/README.md\n" ||
		metadata["file_count"] != 1 ||
		metadata["insertions"] != 12 ||
		metadata["deletions"] != 3 {
		t.Fatalf("metadata = %+v, want canonical diff fields preserved", metadata)
	}
	files, ok := metadata["files"].([]string)
	if !ok || !reflect.DeepEqual(files, []string{"README.md"}) {
		t.Fatalf("metadata files = %#v, want canonical files", metadata["files"])
	}
	if metadata["mode"] != "write" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
}

func TestRecordDiffCheckRecordsZeroDiffStats(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordDiffCheck(recorder, DiffSnapshot{
		ID:         "patch-1",
		Diff:       "diff --git a/README.md b/README.md\n",
		Insertions: 1,
	}); err != nil {
		t.Fatalf("RecordDiffCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	if checks[0].Metadata["insertions"] != 1 || checks[0].Metadata["deletions"] != 0 {
		t.Fatalf("metadata = %+v, want explicit zero deletion count", checks[0].Metadata)
	}
}

func TestRecordDiffCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	diffErr := errors.New("working tree unavailable")

	err := RecordDiffCheck(recorder, DiffSnapshot{
		ID:  "patch-1",
		Ref: "patch-1.diff",
		Err: diffErr,
	})
	if !errors.Is(err, ErrDiffFailed) || !errors.Is(err, diffErr) {
		t.Fatalf("RecordDiffCheck() error = %v, want ErrDiffFailed and diff error", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["error"] != "working tree unavailable" {
		t.Fatalf("metadata = %+v, want error metadata", check.Metadata)
	}
}

func TestRecordDiffCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordDiffCheck(recorder, DiffSnapshot{
		ID:      "patch-1",
		Ref:     "patch-1.diff",
		Skipped: true,
		Summary: "write mode not enabled",
	}); err != nil {
		t.Fatalf("RecordDiffCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped diff check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "patch-1.diff" {
		t.Fatalf("evidence = %+v, want skipped diff evidence", checks[0].Evidence)
	}
}

func TestRecordDiffCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordDiffCheck(nil, DiffSnapshot{ID: "patch-1", Diff: "diff --git a/a b/a\n"}); err == nil {
		t.Fatal("RecordDiffCheck(nil) error = nil, want error")
	}
	if err := RecordDiffCheck(recorder, DiffSnapshot{ID: "patch-1"}); !errors.Is(err, ErrDiffRequired) {
		t.Fatalf("RecordDiffCheck(missing diff) error = %v, want ErrDiffRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
