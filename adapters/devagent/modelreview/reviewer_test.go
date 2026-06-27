package modelreview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

func TestReviewerReviewReturnsDecisionFromModelJSON(t *testing.T) {
	model := &chatModelStub{
		message: gopact.Message{
			Role:    gopact.RoleAssistant,
			Content: `{"status":"approved","reviewer":"model-review","summary":"docs and tests look safe","metadata":{"confidence":"high"}}`,
		},
	}
	reviewer, err := New(model)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := devagent.Review(context.Background(), reviewer, devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Patch: devagent.PatchProposal{
			ID:      "patch-1",
			Summary: "add model reviewer adapter",
		},
		Report: gopact.VerificationReport{
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			Status: gopact.VerificationStatusPassed,
		},
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewApproved || decision.Reviewer != "model-review" {
		t.Fatalf("decision = %+v, want approved model review", decision)
	}
	if decision.Summary != "docs and tests look safe" || decision.Metadata["confidence"] != "high" {
		t.Fatalf("decision = %+v, want summary and metadata from model", decision)
	}
	if len(model.requests) != 1 {
		t.Fatalf("requests = %d, want one model request", len(model.requests))
	}
	request := model.requests[0]
	if request.IDs.RunID != "run-1" || request.Capabilities[0] != gopact.CapabilityJSONSchema {
		t.Fatalf("request = %+v, want runtime ids and json schema capability", request)
	}
	if len(request.Messages) != 2 ||
		request.Messages[0].Role != gopact.RoleSystem ||
		request.Messages[1].Role != gopact.RoleUser ||
		!strings.Contains(request.Messages[1].Text(), "add model reviewer adapter") {
		t.Fatalf("request messages = %+v, want system prompt and serialized evidence", request.Messages)
	}
}

func TestReviewerReviewUsesFallbackReviewerWhenModelOmitsReviewer(t *testing.T) {
	model := &chatModelStub{
		message: gopact.Message{
			Role:    gopact.RoleAssistant,
			Content: `{"status":"rejected","summary":"missing verification evidence"}`,
		},
	}
	reviewer, err := New(model, WithReviewer("ai-reviewer"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected || decision.Reviewer != "ai-reviewer" {
		t.Fatalf("decision = %+v, want fallback reviewer on rejected decision", decision)
	}
}

func TestReviewerReviewAnnotatesRequiredPromptAndEvalGovernance(t *testing.T) {
	model := &chatModelStub{
		message: gopact.Message{
			Role:    gopact.RoleAssistant,
			Content: `{"status":"approved","summary":"governed review"}`,
		},
	}
	reviewer, err := New(
		model,
		WithGovernance(Governance{
			PromptID:      "devagent-review",
			PromptVersion: "2026-06-25",
			EvalID:        "release-eval",
			EvalVersion:   "v1",
			PolicyRef:     "release-policy-v1",
			Metadata: map[string]any{
				"dataset": "devagent-review-golden",
			},
		}),
		WithRequiredGovernanceFields(
			GovernanceFieldPromptID,
			GovernanceFieldPromptVersion,
			GovernanceFieldEvalID,
			GovernanceFieldEvalVersion,
			GovernanceFieldPolicyRef,
		),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if len(model.requests) != 1 {
		t.Fatalf("requests = %d, want one model request", len(model.requests))
	}
	request := model.requests[0]
	if request.Metadata["review_prompt_id"] != "devagent-review" ||
		request.Metadata["review_prompt_version"] != "2026-06-25" ||
		request.Metadata["review_eval_id"] != "release-eval" ||
		request.Metadata["review_eval_version"] != "v1" ||
		request.Metadata["review_policy_ref"] != "release-policy-v1" ||
		request.Metadata["dataset"] != "devagent-review-golden" {
		t.Fatalf("request metadata = %+v, want governance metadata", request.Metadata)
	}
	if decision.Metadata["review_prompt_id"] != "devagent-review" ||
		decision.Metadata["review_prompt_version"] != "2026-06-25" ||
		decision.Metadata["review_eval_id"] != "release-eval" ||
		decision.Metadata["review_eval_version"] != "v1" ||
		decision.Metadata["review_policy_ref"] != "release-policy-v1" ||
		decision.Metadata["dataset"] != "devagent-review-golden" {
		t.Fatalf("decision metadata = %+v, want copied governance metadata", decision.Metadata)
	}

	decision.Metadata["dataset"] = "mutated"
	if model.requests[0].Metadata["dataset"] != "devagent-review-golden" {
		t.Fatalf("request metadata was aliased by decision mutation: %+v", model.requests[0].Metadata)
	}
}

func TestReviewerReviewPreservesCanonicalMetadata(t *testing.T) {
	model := &chatModelStub{
		message: gopact.Message{
			Role: gopact.RoleAssistant,
			Content: strings.Join([]string{
				`{"status":"approved","summary":"governed review",`,
				`"metadata":{"adapter":"spoofed-model","review_prompt_id":"spoofed-model"}}`,
			}, ""),
		},
	}
	reviewer, err := New(
		model,
		WithGovernance(Governance{
			PromptID:      "devagent-review",
			PromptVersion: "2026-06-27",
			EvalID:        "release-eval",
			Metadata: map[string]any{
				"adapter":          "spoofed-governance",
				"purpose":          "spoofed-purpose",
				"review_prompt_id": "spoofed-governance",
				"dataset":          "review-golden",
			},
		}),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if len(model.requests) != 1 {
		t.Fatalf("requests = %d, want one model request", len(model.requests))
	}
	request := model.requests[0]
	if request.Metadata["adapter"] != "modelreview" ||
		request.Metadata["purpose"] != "devagent_review" ||
		request.Metadata["review_prompt_id"] != "devagent-review" ||
		request.Metadata["dataset"] != "review-golden" {
		t.Fatalf("request metadata = %+v, want canonical adapter/purpose and governed prompt metadata", request.Metadata)
	}
	if decision.Metadata["adapter"] != "modelreview" ||
		decision.Metadata["review_prompt_id"] != "devagent-review" ||
		decision.Metadata["dataset"] != "review-golden" {
		t.Fatalf("decision metadata = %+v, want canonical adapter and governed prompt metadata", decision.Metadata)
	}
}

func TestNewRejectsMissingRequiredGovernanceField(t *testing.T) {
	_, err := New(
		&chatModelStub{},
		WithGovernance(Governance{
			PromptID: "devagent-review",
		}),
		WithRequiredGovernanceFields(GovernanceFieldPromptID, GovernanceFieldEvalID),
	)
	if !errors.Is(err, ErrGovernanceFieldRequired) || !strings.Contains(err.Error(), "review_eval_id") {
		t.Fatalf("New(missing required governance field) error = %v, want review_eval_id", err)
	}
}

func TestReviewerReviewParsesJSONInsideMarkdownFence(t *testing.T) {
	model := &chatModelStub{
		message: gopact.Message{
			Role: gopact.RoleAssistant,
			Content: strings.Join([]string{
				"```json",
				`{"status":"approved","reviewer":"model-review","summary":"safe"}`,
				"```",
			}, "\n"),
		},
	}
	reviewer, err := New(model)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if decision.Status != devagent.ReviewApproved || decision.Summary != "safe" {
		t.Fatalf("decision = %+v, want parsed fenced json", decision)
	}
}

func TestReviewerReviewRejectsInvalidModelDecision(t *testing.T) {
	model := &chatModelStub{
		message: gopact.Message{
			Role:    gopact.RoleAssistant,
			Content: `{"status":"maybe","reviewer":"model-review"}`,
		},
	}
	reviewer, err := New(model)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{})
	if !errors.Is(err, ErrInvalidModelDecision) {
		t.Fatalf("Review() error = %v, want ErrInvalidModelDecision", err)
	}
}

func TestReviewerReviewPropagatesModelError(t *testing.T) {
	modelErr := errors.New("model failed")
	reviewer, err := New(&chatModelStub{err: modelErr})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{})
	if !errors.Is(err, modelErr) {
		t.Fatalf("Review() error = %v, want wrapped model error", err)
	}
}

func TestReviewerReviewHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reviewer, err := New(&chatModelStub{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(ctx, devagent.ReviewInput{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Review() error = %v, want context.Canceled", err)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	if reviewer, err := New(nil); reviewer != nil || !errors.Is(err, ErrModelRequired) {
		t.Fatalf("New(nil) reviewer=%v err=%v, want ErrModelRequired", reviewer, err)
	}
	if reviewer, err := New(&chatModelStub{}, WithReviewer(" ")); reviewer != nil || !errors.Is(err, ErrReviewerRequired) {
		t.Fatalf("New(empty reviewer) reviewer=%v err=%v, want ErrReviewerRequired", reviewer, err)
	}
	if reviewer, err := New(&chatModelStub{}, WithPromptBuilder(nil)); reviewer != nil || !errors.Is(err, ErrPromptBuilderRequired) {
		t.Fatalf("New(nil prompt builder) reviewer=%v err=%v, want ErrPromptBuilderRequired", reviewer, err)
	}
	if reviewer, err := New(&chatModelStub{}, WithParser(nil)); reviewer != nil || !errors.Is(err, ErrParserRequired) {
		t.Fatalf("New(nil parser) reviewer=%v err=%v, want ErrParserRequired", reviewer, err)
	}
	if reviewer, err := New(&chatModelStub{}, WithRequiredGovernanceFields()); reviewer != nil ||
		!errors.Is(err, ErrGovernanceFieldRequired) {
		t.Fatalf("New(no governance fields) reviewer=%v err=%v, want ErrGovernanceFieldRequired", reviewer, err)
	}
	if reviewer, err := New(&chatModelStub{}, WithRequiredGovernanceFields(GovernanceField("unknown"))); reviewer != nil ||
		!errors.Is(err, ErrInvalidGovernanceField) {
		t.Fatalf("New(invalid governance field) reviewer=%v err=%v, want ErrInvalidGovernanceField", reviewer, err)
	}
}

type chatModelStub struct {
	message  gopact.Message
	err      error
	requests []gopact.ModelRequest
}

func (s *chatModelStub) Generate(ctx context.Context, request gopact.ModelRequest) (gopact.Message, error) {
	if err := ctx.Err(); err != nil {
		return gopact.Message{}, err
	}
	s.requests = append(s.requests, request)
	if s.err != nil {
		return gopact.Message{}, s.err
	}
	return s.message, nil
}
