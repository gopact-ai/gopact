package gopacttest

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	ErrDiffFailed   = errors.New("gopacttest: diff failed")
	ErrDiffRequired = errors.New("gopacttest: diff is required")
)

const (
	// VerificationCheckDiff is the standard check ID prefix for observed diffs.
	VerificationCheckDiff = "diff"

	// VerificationEvidenceTypeDiff is the evidence type for observed diff results.
	VerificationEvidenceTypeDiff = "diff"
)

// DiffSnapshot is an already-observed patch or working tree diff.
type DiffSnapshot struct {
	ID         string
	Name       string
	Ref        string
	Diff       string
	Files      []string
	Insertions int
	Deletions  int
	Err        error
	Skipped    bool
	Summary    string
	Metadata   map[string]any
}

// RecordDiffCheck records an already-observed diff as verification evidence.
func RecordDiffCheck(recorder *gopact.VerificationRecorder, snapshot DiffSnapshot) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if !snapshot.Skipped && snapshot.Err == nil && strings.TrimSpace(snapshot.Diff) == "" {
		return ErrDiffRequired
	}

	check := diffCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrDiffFailed, snapshot.Err)
		}
		return ErrDiffFailed
	}
	return nil
}

func diffCheck(snapshot DiffSnapshot) gopact.VerificationCheck {
	ref := diffRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckDiff + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "diff"
	}

	status := gopact.VerificationStatusPassed
	if snapshot.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if snapshot.Err != nil {
		status = gopact.VerificationStatusFailed
	}

	summary := snapshot.Summary
	if summary == "" {
		summary = diffSummary(status, snapshot)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeDiff,
				Ref:      ref,
				Summary:  diffEvidenceSummary(status, snapshot),
				Metadata: diffEvidenceMetadata(snapshot),
			},
		},
		Metadata: diffCheckMetadata(snapshot),
	}
}

func diffSummary(status gopact.VerificationStatus, snapshot DiffSnapshot) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "diff check skipped"
	case gopact.VerificationStatusFailed:
		if snapshot.Err != nil {
			return "diff failed: " + snapshot.Err.Error()
		}
		return "diff failed"
	default:
		return "diff captured"
	}
}

func diffEvidenceSummary(status gopact.VerificationStatus, snapshot DiffSnapshot) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if len(snapshot.Files) > 0 || snapshot.Insertions > 0 || snapshot.Deletions > 0 {
		return fmt.Sprintf("%d files, +%d -%d", len(snapshot.Files), snapshot.Insertions, snapshot.Deletions)
	}
	return "diff captured"
}

func diffCheckMetadata(snapshot DiffSnapshot) map[string]any {
	metadata := diffBaseMetadata(snapshot)
	mergeSupplementalMetadata(metadata, snapshot.Metadata, diffReservedMetadataKey)
	return metadata
}

func diffEvidenceMetadata(snapshot DiffSnapshot) map[string]any {
	return diffBaseMetadata(snapshot)
}

func diffBaseMetadata(snapshot DiffSnapshot) map[string]any {
	metadata := map[string]any{
		"ref": diffRef(snapshot),
	}
	if snapshot.Diff != "" {
		metadata["diff"] = snapshot.Diff
	}
	if len(snapshot.Files) > 0 {
		metadata["files"] = append([]string(nil), snapshot.Files...)
		metadata["file_count"] = len(snapshot.Files)
	}
	if snapshot.Insertions != 0 || snapshot.Deletions != 0 || snapshot.Diff != "" {
		metadata["insertions"] = snapshot.Insertions
		metadata["deletions"] = snapshot.Deletions
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func diffReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"diff",
		"files",
		"file_count",
		"insertions",
		"deletions",
		"error",
		"skipped":
		return true
	default:
		return false
	}
}

func diffRef(snapshot DiffSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	if len(snapshot.Files) > 0 {
		return strings.Join(snapshot.Files, ",")
	}
	return "observed-diff"
}
