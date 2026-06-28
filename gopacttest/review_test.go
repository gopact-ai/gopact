package gopacttest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestRecordReviewCheckRecordsApprovedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	if err := RecordReviewCheck(recorder, ReviewResult{
		ID:        "review-1",
		Name:      "human review",
		Ref:       "lark:approval:123",
		Reviewer:  "alice",
		Source:    "lark",
		Status:    ReviewStatusApproved,
		Summary:   "approved after checking tests",
		CreatedAt: createdAt,
		Metadata:  map[string]any{"channel": "lark", "review_policy_ref": "release-policy-v1"},
	}); err != nil {
		t.Fatalf("RecordReviewCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "review-1" || check.Name != "human review" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed review check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeReview || check.Evidence[0].Ref != "lark:approval:123" {
		t.Fatalf("evidence = %+v, want review evidence", check.Evidence)
	}
	if check.Metadata["reviewer"] != "alice" ||
		check.Metadata["source"] != "lark" ||
		check.Metadata["review_status"] != string(ReviewStatusApproved) ||
		check.Metadata["created_at"] != createdAt.Format(time.RFC3339Nano) ||
		check.Metadata["channel"] != "lark" ||
		check.Metadata["review_policy_ref"] != "release-policy-v1" {
		t.Fatalf("metadata = %+v, want reviewer/source/status/custom metadata", check.Metadata)
	}
	assertReviewStringSliceMetadata(t, check.Metadata, "metadata_keys", []string{"channel", "review_policy_ref"})
	assertReviewStringSliceMetadata(
		t,
		check.Evidence[0].Metadata,
		"metadata_keys",
		[]string{"channel", "review_policy_ref"},
	)
}

func assertReviewStringSliceMetadata(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()
	if got := metadata[key]; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
	}
}

func TestRecordReviewCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	if err := RecordReviewCheck(recorder, ReviewResult{
		ID:        "review-1",
		Ref:       "lark:approval:123",
		Reviewer:  "alice",
		Source:    "lark",
		Status:    ReviewStatusApproved,
		Summary:   "approved after checking tests",
		CreatedAt: createdAt,
		Metadata: map[string]any{
			"ref":           "forged-ref",
			"reviewer":      "mallory",
			"source":        "forged-source",
			"review_status": string(ReviewStatusRejected),
			"summary":       "forged summary",
			"created_at":    "forged-created-at",
			"channel":       "lark",
		},
	}); err != nil {
		t.Fatalf("RecordReviewCheck() error = %v", err)
	}

	metadata := recorder.Checks()[0].Metadata
	if metadata["ref"] != "lark:approval:123" ||
		metadata["reviewer"] != "alice" ||
		metadata["source"] != "lark" ||
		metadata["review_status"] != string(ReviewStatusApproved) ||
		metadata["summary"] != "approved after checking tests" ||
		metadata["created_at"] != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("metadata = %+v, want canonical review fields preserved", metadata)
	}
	if metadata["channel"] != "lark" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
	evidenceMetadata := recorder.Checks()[0].Evidence[0].Metadata
	if evidenceMetadata["reviewer"] != "alice" ||
		evidenceMetadata["review_status"] != string(ReviewStatusApproved) ||
		evidenceMetadata["channel"] != "lark" {
		t.Fatalf("evidence metadata = %+v, want canonical review evidence and supplemental metadata", evidenceMetadata)
	}
}

func TestRecordReviewCheckCopiesGovernanceMetadataToEvidence(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReviewCheck(recorder, ReviewResult{
		ID:       "review-1",
		Reviewer: "model-review",
		Source:   "model",
		Status:   ReviewStatusApproved,
		Metadata: map[string]any{
			"review_prompt_id":      "devagent-review",
			"review_prompt_version": "2026-06-25",
			"review_eval_id":        "release-eval",
			"review_eval_version":   "v1",
			"review_policy_ref":     "release-policy-v1",
		},
	}); err != nil {
		t.Fatalf("RecordReviewCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["review_prompt_id"] != "devagent-review" ||
		evidenceMetadata["review_prompt_version"] != "2026-06-25" ||
		evidenceMetadata["review_eval_id"] != "release-eval" ||
		evidenceMetadata["review_eval_version"] != "v1" ||
		evidenceMetadata["review_policy_ref"] != "release-policy-v1" {
		t.Fatalf("evidence metadata = %+v, want review governance metadata", evidenceMetadata)
	}
}

func TestRecordReviewCheckRecordsRejectedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordReviewCheck(recorder, ReviewResult{
		ID:       "review-1",
		Reviewer: "ci-review",
		Source:   "ci",
		Status:   ReviewStatusRejected,
		Summary:  "coverage dropped",
	})
	if !errors.Is(err, ErrReviewFailed) {
		t.Fatalf("RecordReviewCheck() error = %v, want ErrReviewFailed", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["review_status"] != string(ReviewStatusRejected) || check.Metadata["reviewer"] != "ci-review" {
		t.Fatalf("metadata = %+v, want rejected review metadata", check.Metadata)
	}
}

func TestRecordReviewCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	reviewErr := errors.New("review service unavailable")

	err := RecordReviewCheck(recorder, ReviewResult{
		ID:       "review-1",
		Reviewer: "model-reviewer",
		Source:   "model",
		Err:      reviewErr,
	})
	if !errors.Is(err, ErrReviewFailed) || !errors.Is(err, reviewErr) {
		t.Fatalf("RecordReviewCheck() error = %v, want ErrReviewFailed and review error", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("checks = %+v, want failed review check", checks)
	}
	if checks[0].Metadata["error"] != "review service unavailable" {
		t.Fatalf("metadata = %+v, want error metadata", checks[0].Metadata)
	}
}

func TestRecordReviewCheckRecordsSkippedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReviewCheck(recorder, ReviewResult{
		ID:      "review-1",
		Ref:     "review:skipped",
		Skipped: true,
		Summary: "review not required in plan mode",
	}); err != nil {
		t.Fatalf("RecordReviewCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("checks = %+v, want skipped review check", checks)
	}
	if len(checks[0].Evidence) != 1 || checks[0].Evidence[0].Ref != "review:skipped" {
		t.Fatalf("evidence = %+v, want skipped review evidence", checks[0].Evidence)
	}
}

func TestRecordReviewCheckRejectsInvalidInput(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordReviewCheck(nil, ReviewResult{Reviewer: "alice", Status: ReviewStatusApproved}); err == nil {
		t.Fatal("RecordReviewCheck(nil) error = nil, want error")
	}
	if err := RecordReviewCheck(recorder, ReviewResult{Status: ReviewStatusApproved}); !errors.Is(err, ErrReviewReviewerRequired) {
		t.Fatalf("RecordReviewCheck(missing reviewer) error = %v, want ErrReviewReviewerRequired", err)
	}
	if err := RecordReviewCheck(recorder, ReviewResult{Reviewer: "alice"}); !errors.Is(err, ErrReviewStatusRequired) {
		t.Fatalf("RecordReviewCheck(missing status) error = %v, want ErrReviewStatusRequired", err)
	}
	if err := RecordReviewCheck(recorder, ReviewResult{Reviewer: "alice", Status: ReviewStatus("maybe")}); !errors.Is(err, ErrReviewStatusRequired) {
		t.Fatalf("RecordReviewCheck(invalid status) error = %v, want ErrReviewStatusRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("check count = %d, want 0 after rejected input", len(recorder.Checks()))
	}
}
