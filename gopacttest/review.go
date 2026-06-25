package gopacttest

import (
	"errors"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	ErrReviewFailed           = errors.New("gopacttest: review failed")
	ErrReviewReviewerRequired = errors.New("gopacttest: review reviewer is required")
	ErrReviewStatusRequired   = errors.New("gopacttest: review status is required")
)

const (
	// VerificationCheckReview is the standard check ID prefix for review results.
	VerificationCheckReview = "review"

	// VerificationEvidenceTypeReview is the evidence type for reviewer decisions.
	VerificationEvidenceTypeReview = "review"
)

// ReviewStatus describes an already-observed reviewer decision.
type ReviewStatus string

const (
	ReviewStatusApproved ReviewStatus = "approved"
	ReviewStatusRejected ReviewStatus = "rejected"
)

// ReviewResult is an already-observed human, model, CI, or external reviewer decision.
type ReviewResult struct {
	ID        string
	Name      string
	Ref       string
	Reviewer  string
	Source    string
	Status    ReviewStatus
	Summary   string
	CreatedAt time.Time
	Err       error
	Skipped   bool
	Metadata  map[string]any
}

// RecordReviewCheck records an already-observed reviewer decision as verification evidence.
func RecordReviewCheck(recorder *gopact.VerificationRecorder, result ReviewResult) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if !result.Skipped && strings.TrimSpace(result.Reviewer) == "" {
		return ErrReviewReviewerRequired
	}
	if !result.Skipped && result.Err == nil && !result.Status.valid() {
		return ErrReviewStatusRequired
	}

	check := reviewCheck(result)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if result.Err != nil {
			return errors.Join(ErrReviewFailed, result.Err)
		}
		return ErrReviewFailed
	}
	return nil
}

func reviewCheck(result ReviewResult) gopact.VerificationCheck {
	ref := reviewRef(result)
	id := result.ID
	if id == "" {
		id = VerificationCheckReview + ":" + ref
	}
	name := result.Name
	if name == "" {
		name = "review"
	}

	status := gopact.VerificationStatusPassed
	if result.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if result.Err != nil || result.Status == ReviewStatusRejected {
		status = gopact.VerificationStatusFailed
	}

	summary := result.Summary
	if summary == "" {
		summary = reviewSummary(status, result)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeReview,
				Ref:      ref,
				Summary:  reviewEvidenceSummary(status, result),
				Metadata: reviewEvidenceMetadata(result),
			},
		},
		Metadata: reviewCheckMetadata(result),
	}
}

func (s ReviewStatus) valid() bool {
	switch s {
	case ReviewStatusApproved, ReviewStatusRejected:
		return true
	default:
		return false
	}
}

func reviewSummary(status gopact.VerificationStatus, result ReviewResult) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "review check skipped"
	case gopact.VerificationStatusFailed:
		if result.Err != nil {
			return "review failed: " + result.Err.Error()
		}
		return "review rejected"
	default:
		return "review approved"
	}
}

func reviewEvidenceSummary(status gopact.VerificationStatus, result ReviewResult) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Summary != "" {
		return result.Summary
	}
	if result.Status != "" {
		return string(result.Status)
	}
	return "review captured"
}

func reviewCheckMetadata(result ReviewResult) map[string]any {
	metadata := reviewBaseMetadata(result)
	mergeSupplementalMetadata(metadata, result.Metadata, reviewReservedMetadataKey)
	return metadata
}

func reviewEvidenceMetadata(result ReviewResult) map[string]any {
	metadata := reviewBaseMetadata(result)
	mergeSupplementalMetadata(metadata, result.Metadata, reviewReservedMetadataKey)
	return metadata
}

func reviewBaseMetadata(result ReviewResult) map[string]any {
	metadata := map[string]any{
		"ref": reviewRef(result),
	}
	if result.Reviewer != "" {
		metadata["reviewer"] = result.Reviewer
	}
	if result.Source != "" {
		metadata["source"] = result.Source
	}
	if result.Status != "" {
		metadata["review_status"] = string(result.Status)
	}
	if result.Summary != "" {
		metadata["summary"] = result.Summary
	}
	if !result.CreatedAt.IsZero() {
		metadata["created_at"] = result.CreatedAt.Format(time.RFC3339Nano)
	}
	if result.Err != nil {
		metadata["error"] = result.Err.Error()
	}
	if result.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func reviewReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"reviewer",
		"source",
		"review_status",
		"summary",
		"created_at",
		"error",
		"skipped":
		return true
	default:
		return false
	}
}

func reviewRef(result ReviewResult) string {
	if result.Ref != "" {
		return result.Ref
	}
	if result.ID != "" {
		return result.ID
	}
	if result.Source != "" && result.Reviewer != "" {
		return result.Source + ":" + result.Reviewer
	}
	if result.Reviewer != "" {
		return result.Reviewer
	}
	return "review"
}
