package gopacttest

import (
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordFileSnapshotCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	modifiedAt := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)

	if err := RecordFileSnapshotCheck(recorder, FileSnapshot{
		ID:            "readme-snapshot",
		Name:          "README snapshot",
		Path:          "README.md",
		Hash:          "abc123",
		HashAlgorithm: "sha256",
		SizeBytes:     42,
		ModifiedAt:    modifiedAt,
		Metadata:      map[string]any{"purpose": "release gate"},
	}); err != nil {
		t.Fatalf("RecordFileSnapshotCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "readme-snapshot" || check.Name != "README snapshot" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed README snapshot check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeFileSnapshot || check.Evidence[0].Ref != "README.md" {
		t.Fatalf("evidence = %+v, want file snapshot evidence", check.Evidence)
	}
	if check.Metadata["hash"] != "abc123" || check.Metadata["hash_algorithm"] != "sha256" || check.Metadata["size_bytes"] != int64(42) {
		t.Fatalf("metadata = %+v, want hash and size metadata", check.Metadata)
	}
	if check.Metadata["modified_at"] != modifiedAt.Format(time.RFC3339Nano) || check.Metadata["purpose"] != "release gate" {
		t.Fatalf("metadata = %+v, want modified_at and custom metadata", check.Metadata)
	}
}

func TestRecordFileSnapshotCheckRecordsZeroSizeSnapshot(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordFileSnapshotCheck(recorder, FileSnapshot{
		Path: "empty.txt",
		Hash: "e3b0c44298fc1c149afbf4c8996fb924",
	}); err != nil {
		t.Fatalf("RecordFileSnapshotCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	if checks[0].Metadata["size_bytes"] != int64(0) {
		t.Fatalf("metadata = %+v, want zero size metadata", checks[0].Metadata)
	}
}

func TestRecordFileSnapshotCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	modifiedAt := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)

	if err := RecordFileSnapshotCheck(recorder, FileSnapshot{
		Path:          "README.md",
		Hash:          "abc123",
		HashAlgorithm: "sha256",
		SizeBytes:     42,
		Mode:          "0644",
		ModifiedAt:    modifiedAt,
		Metadata: map[string]any{
			"path":           "forged.md",
			"hash":           "forged",
			"hash_algorithm": "md5",
			"size_bytes":     int64(99),
			"mode":           "0777",
			"modified_at":    "forged-time",
			"skipped":        true,
			"purpose":        "release gate",
		},
	}); err != nil {
		t.Fatalf("RecordFileSnapshotCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["path"] != "README.md" ||
		metadata["hash"] != "abc123" ||
		metadata["hash_algorithm"] != "sha256" ||
		metadata["size_bytes"] != int64(42) ||
		metadata["mode"] != "0644" ||
		metadata["modified_at"] != modifiedAt.Format(time.RFC3339Nano) {
		t.Fatalf("metadata = %+v, want canonical file snapshot fields preserved", metadata)
	}
	if _, ok := metadata["skipped"]; ok {
		t.Fatalf("metadata = %+v, did not expect forged skipped metadata", metadata)
	}
	if metadata["purpose"] != "release gate" {
		t.Fatalf("metadata = %+v, want non-conflicting caller metadata preserved", metadata)
	}
}

func TestRecordFileSnapshotCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	snapshotErr := errors.New("permission denied")

	err := RecordFileSnapshotCheck(recorder, FileSnapshot{
		ID:   "secret-snapshot",
		Path: "secret.txt",
		Err:  snapshotErr,
	})
	if !errors.Is(err, ErrFileSnapshotFailed) || !errors.Is(err, snapshotErr) {
		t.Fatalf("RecordFileSnapshotCheck() error = %v, want ErrFileSnapshotFailed and snapshot error", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Ref != "secret.txt" {
		t.Fatalf("evidence = %+v, want failed file snapshot evidence", check.Evidence)
	}
	if check.Metadata["error"] != "permission denied" {
		t.Fatalf("metadata = %+v, want error metadata", check.Metadata)
	}
}

func TestRecordFileSnapshotCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordFileSnapshotCheck(recorder, FileSnapshot{
		ID:      "generated-snapshot",
		Path:    "generated.pb.go",
		Skipped: true,
		Summary: "generated file ignored",
	}); err != nil {
		t.Fatalf("RecordFileSnapshotCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped file snapshot check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "generated.pb.go" {
		t.Fatalf("evidence = %+v, want skipped file snapshot evidence", checks[0].Evidence)
	}
}

func TestRecordFileSnapshotCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordFileSnapshotCheck(nil, FileSnapshot{Path: "README.md", Hash: "abc123"}); err == nil {
		t.Fatal("RecordFileSnapshotCheck(nil) error = nil, want error")
	}
	if err := RecordFileSnapshotCheck(recorder, FileSnapshot{Hash: "abc123"}); !errors.Is(err, ErrFileSnapshotPathRequired) {
		t.Fatalf("RecordFileSnapshotCheck(empty path) error = %v, want ErrFileSnapshotPathRequired", err)
	}
	if err := RecordFileSnapshotCheck(recorder, FileSnapshot{Path: "README.md"}); !errors.Is(err, ErrFileSnapshotHashRequired) {
		t.Fatalf("RecordFileSnapshotCheck(missing hash) error = %v, want ErrFileSnapshotHashRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
