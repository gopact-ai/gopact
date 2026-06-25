package devagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestReviewInvokesReviewerWithCopiedEvidence(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	now := time.Now()
	input := ReviewInput{
		Mode: ModeWrite,
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "update docs",
			Diff:    "diff --git a/README.md b/README.md\n",
			Files: []PatchFile{
				{
					Path:   "README.md",
					Intent: "document reviewer slot",
					Metadata: map[string]any{
						"kind": "docs",
					},
				},
			},
			Metadata: map[string]any{
				"patch": "metadata",
			},
		},
		Action: ActionResult{
			Status: ActionAllowed,
			Mode:   ModeWrite,
			Action: ActionRelease,
			Metadata: map[string]any{
				"action": "metadata",
			},
		},
		Report: report,
		EntropyAudits: []gopact.EntropyAudit{
			{
				ID:        "entropy-1",
				Status:    gopact.VerificationStatusPassed,
				CreatedAt: now,
				Findings: []gopact.EntropyFinding{
					{
						ID:        "finding-1",
						Category:  gopact.EntropyStaleDocs,
						Severity:  gopact.EntropySeverityLow,
						Summary:   "source file has docs",
						CreatedAt: now,
						Evidence: []gopact.VerificationEvidence{
							{
								Type: "diff",
								Ref:  "README.md",
								Metadata: map[string]any{
									"evidence": "metadata",
								},
							},
						},
						Metadata: map[string]any{
							"finding": "metadata",
						},
					},
				},
				Metadata: map[string]any{
					"audit": "metadata",
				},
			},
		},
		Gate: GateResult{
			Status: GatePassed,
			Metadata: map[string]any{
				"gate": "metadata",
			},
		},
		Metadata: map[string]any{
			"request": "metadata",
		},
	}

	decision := ReviewDecision{
		Status:   ReviewApproved,
		Reviewer: "reviewer-1",
		Summary:  "safe docs change",
		Metadata: map[string]any{
			"score": "ok",
		},
	}
	result, err := Review(context.Background(), ReviewerFunc(func(ctx context.Context, got ReviewInput) (ReviewDecision, error) {
		if ctx == nil {
			t.Fatal("review ctx is nil")
		}
		if got.Mode != ModeWrite || got.Patch.ID != "patch-1" || got.Report.IDs.RunID != "run-1" {
			t.Fatalf("review input = %+v, want write patch/report evidence", got)
		}
		got.Patch.Files[0].Path = "mutated.md"
		got.Patch.Files[0].Metadata["kind"] = "mutated"
		got.Patch.Metadata["patch"] = "mutated"
		got.Action.Metadata["action"] = "mutated"
		got.EntropyAudits[0].ID = "mutated"
		got.EntropyAudits[0].Findings[0].Evidence[0].Metadata["evidence"] = "mutated"
		got.EntropyAudits[0].Findings[0].Metadata["finding"] = "mutated"
		got.EntropyAudits[0].Metadata["audit"] = "mutated"
		got.Gate.Metadata["gate"] = "mutated"
		got.Metadata["request"] = "mutated"
		return decision, nil
	}), input)
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if result.Status != ReviewApproved || result.Reviewer != "reviewer-1" {
		t.Fatalf("Review() = %+v, want approved decision", result)
	}

	result.Metadata["score"] = "mutated"
	if decision.Metadata["score"] != "ok" {
		t.Fatalf("returned decision shares metadata with reviewer decision")
	}
	if input.Patch.Files[0].Path != "README.md" ||
		input.Patch.Files[0].Metadata["kind"] != "docs" ||
		input.Patch.Metadata["patch"] != "metadata" ||
		input.Action.Metadata["action"] != "metadata" ||
		input.EntropyAudits[0].ID != "entropy-1" ||
		input.EntropyAudits[0].Findings[0].Evidence[0].Metadata["evidence"] != "metadata" ||
		input.EntropyAudits[0].Findings[0].Metadata["finding"] != "metadata" ||
		input.EntropyAudits[0].Metadata["audit"] != "metadata" ||
		input.Gate.Metadata["gate"] != "metadata" ||
		input.Metadata["request"] != "metadata" {
		t.Fatalf("Review() allowed reviewer to mutate input: %+v", input)
	}
}

func TestReviewRejectsMissingReviewer(t *testing.T) {
	_, err := Review(context.Background(), nil, ReviewInput{})
	if !errors.Is(err, ErrReviewerRequired) {
		t.Fatalf("Review(nil) error = %v, want ErrReviewerRequired", err)
	}

	var fn ReviewerFunc
	_, err = fn.Review(context.Background(), ReviewInput{})
	if !errors.Is(err, ErrReviewerRequired) {
		t.Fatalf("ReviewerFunc(nil).Review() error = %v, want ErrReviewerRequired", err)
	}
}

func TestReviewRejectsInvalidDecision(t *testing.T) {
	tests := []struct {
		name     string
		decision ReviewDecision
	}{
		{
			name:     "unknown status",
			decision: ReviewDecision{Reviewer: "reviewer-1"},
		},
		{
			name:     "missing reviewer",
			decision: ReviewDecision{Status: ReviewApproved},
		},
		{
			name:     "unsupported status",
			decision: ReviewDecision{Status: ReviewStatus("maybe"), Reviewer: "reviewer-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Review(context.Background(), StaticReviewer{Decision: tt.decision}, ReviewInput{})
			if !errors.Is(err, ErrInvalidReviewDecision) {
				t.Fatalf("Review() error = %v, want ErrInvalidReviewDecision", err)
			}
		})
	}
}

func TestStaticReviewerReturnsCopiedDecision(t *testing.T) {
	reviewer := StaticReviewer{
		Decision: ReviewDecision{
			Status:   ReviewRejected,
			Reviewer: "human",
			Summary:  "needs more tests",
			Metadata: map[string]any{
				"source": "fixture",
			},
		},
	}
	first, err := reviewer.Review(context.Background(), ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	first.Metadata["source"] = "mutated"

	second, err := reviewer.Review(context.Background(), ReviewInput{})
	if err != nil {
		t.Fatalf("Review() second error = %v", err)
	}
	if second.Metadata["source"] != "fixture" {
		t.Fatalf("StaticReviewer returned shared decision metadata: %+v", second.Metadata)
	}
}
