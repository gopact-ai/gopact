package gopacttest

import (
	"errors"
	"fmt"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	ErrFileSnapshotFailed       = errors.New("gopacttest: file snapshot failed")
	ErrFileSnapshotPathRequired = errors.New("gopacttest: file snapshot path is required")
	ErrFileSnapshotHashRequired = errors.New("gopacttest: file snapshot hash is required")
)

const (
	// VerificationCheckFileSnapshot is the standard check ID prefix for file snapshots.
	VerificationCheckFileSnapshot = "file-snapshot"

	// VerificationEvidenceTypeFileSnapshot is the evidence type for file snapshot results.
	VerificationEvidenceTypeFileSnapshot = "file_snapshot"
)

// FileSnapshot is an already-observed file state.
type FileSnapshot struct {
	ID            string
	Name          string
	Path          string
	Hash          string
	HashAlgorithm string
	SizeBytes     int64
	Mode          string
	ModifiedAt    time.Time
	Err           error
	Skipped       bool
	Summary       string
	Metadata      map[string]any
}

// RecordFileSnapshotCheck records an already-observed file snapshot as verification evidence.
func RecordFileSnapshotCheck(recorder *gopact.VerificationRecorder, snapshot FileSnapshot) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if snapshot.Path == "" {
		return ErrFileSnapshotPathRequired
	}
	if !snapshot.Skipped && snapshot.Err == nil && snapshot.Hash == "" {
		return ErrFileSnapshotHashRequired
	}

	check := fileSnapshotCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrFileSnapshotFailed, snapshot.Err)
		}
		return ErrFileSnapshotFailed
	}
	return nil
}

func fileSnapshotCheck(snapshot FileSnapshot) gopact.VerificationCheck {
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckFileSnapshot + ":" + snapshot.Path
	}
	name := snapshot.Name
	if name == "" {
		name = "file snapshot"
	}

	status := gopact.VerificationStatusPassed
	if snapshot.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if snapshot.Err != nil {
		status = gopact.VerificationStatusFailed
	}

	summary := snapshot.Summary
	if summary == "" {
		summary = fileSnapshotSummary(status, snapshot)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeFileSnapshot,
				Ref:      snapshot.Path,
				Summary:  fileSnapshotEvidenceSummary(status, snapshot),
				Metadata: fileSnapshotEvidenceMetadata(snapshot),
			},
		},
		Metadata: fileSnapshotCheckMetadata(snapshot),
	}
}

func fileSnapshotSummary(status gopact.VerificationStatus, snapshot FileSnapshot) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "file snapshot check skipped"
	case gopact.VerificationStatusFailed:
		if snapshot.Err != nil {
			return "file snapshot failed: " + snapshot.Err.Error()
		}
		return "file snapshot failed"
	default:
		return "file snapshot captured"
	}
}

func fileSnapshotEvidenceSummary(status gopact.VerificationStatus, snapshot FileSnapshot) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if snapshot.Hash == "" {
		return "snapshot captured"
	}
	algorithm := snapshot.HashAlgorithm
	if algorithm == "" {
		algorithm = "hash"
	}
	return fmt.Sprintf("%s %s", algorithm, snapshot.Hash)
}

func fileSnapshotCheckMetadata(snapshot FileSnapshot) map[string]any {
	metadata := fileSnapshotBaseMetadata(snapshot)
	if keys := sortedSupplementalMetadataKeys(snapshot.Metadata, fileSnapshotReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeSupplementalMetadata(metadata, snapshot.Metadata, fileSnapshotReservedMetadataKey)
	return metadata
}

func fileSnapshotEvidenceMetadata(snapshot FileSnapshot) map[string]any {
	return fileSnapshotCheckMetadata(snapshot)
}

func fileSnapshotBaseMetadata(snapshot FileSnapshot) map[string]any {
	metadata := map[string]any{
		"path": snapshot.Path,
	}
	if snapshot.Hash != "" {
		metadata["hash"] = snapshot.Hash
		algorithm := snapshot.HashAlgorithm
		if algorithm == "" {
			algorithm = "sha256"
		}
		metadata["hash_algorithm"] = algorithm
	}
	if snapshot.Hash != "" || snapshot.SizeBytes != 0 {
		metadata["size_bytes"] = snapshot.SizeBytes
	}
	if snapshot.Mode != "" {
		metadata["mode"] = snapshot.Mode
	}
	if !snapshot.ModifiedAt.IsZero() {
		metadata["modified_at"] = snapshot.ModifiedAt.Format(time.RFC3339Nano)
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func fileSnapshotReservedMetadataKey(key string) bool {
	switch key {
	case "path",
		"hash",
		"hash_algorithm",
		"size_bytes",
		"mode",
		"modified_at",
		"error",
		"metadata_keys",
		"skipped":
		return true
	default:
		return false
	}
}
