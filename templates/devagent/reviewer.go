package devagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	ErrReviewerRequired      = errors.New("devagent: reviewer is required")
	ErrInvalidReviewDecision = errors.New("devagent: invalid review decision")
)

// ReviewInput is the already-observed evidence a Dev Agent reviewer may inspect.
type ReviewInput struct {
	Mode          Mode                      `json:"mode"`
	Patch         PatchProposal             `json:"patch,omitempty"`
	Action        ActionResult              `json:"action,omitempty"`
	Report        gopact.VerificationReport `json:"report,omitempty"`
	EntropyAudits []gopact.EntropyAudit     `json:"entropy_audits,omitempty"`
	Gate          GateResult                `json:"gate,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

// Reviewer decides whether already-collected Dev Agent evidence is acceptable.
type Reviewer interface {
	Review(ctx context.Context, input ReviewInput) (ReviewDecision, error)
}

// ReviewerFunc adapts a function into a Reviewer.
type ReviewerFunc func(ctx context.Context, input ReviewInput) (ReviewDecision, error)

// Review calls f with copied input evidence.
func (f ReviewerFunc) Review(ctx context.Context, input ReviewInput) (ReviewDecision, error) {
	if f == nil {
		return ReviewDecision{}, ErrReviewerRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ReviewDecision{}, err
	}
	decision, err := f(ctx, copyReviewInput(input))
	if err != nil {
		return ReviewDecision{}, err
	}
	if err := validateReviewDecision(decision); err != nil {
		return ReviewDecision{}, err
	}
	return copyReviewDecision(decision), nil
}

// StaticReviewer returns the same explicit decision for every review.
type StaticReviewer struct {
	Decision ReviewDecision
}

// Review returns the configured decision after validation.
func (r StaticReviewer) Review(ctx context.Context, _ ReviewInput) (ReviewDecision, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ReviewDecision{}, err
	}
	decision := copyReviewDecision(r.Decision)
	if err := validateReviewDecision(decision); err != nil {
		return ReviewDecision{}, err
	}
	return decision, nil
}

// Review calls reviewer with copied evidence and returns a copied, validated decision.
func Review(ctx context.Context, reviewer Reviewer, input ReviewInput) (ReviewDecision, error) {
	if reviewer == nil {
		return ReviewDecision{}, ErrReviewerRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return ReviewDecision{}, err
	}
	decision, err := reviewer.Review(ctx, copyReviewInput(input))
	if err != nil {
		return ReviewDecision{}, fmt.Errorf("devagent: review: %w", err)
	}
	if err := validateReviewDecision(decision); err != nil {
		return ReviewDecision{}, err
	}
	return copyReviewDecision(decision), nil
}

func validateReviewDecision(decision ReviewDecision) error {
	switch decision.Status {
	case ReviewApproved, ReviewRejected:
	default:
		return fmt.Errorf("%w: status %q", ErrInvalidReviewDecision, decision.Status)
	}
	if strings.TrimSpace(decision.Reviewer) == "" {
		return fmt.Errorf("%w: reviewer is required", ErrInvalidReviewDecision)
	}
	return nil
}

func copyReviewInput(input ReviewInput) ReviewInput {
	out := input
	out.Patch = copyPatchProposal(input.Patch)
	out.Action = copyActionResult(input.Action)
	out.Report = copyVerificationReport(input.Report)
	out.EntropyAudits = copyReviewEntropyAudits(input.EntropyAudits)
	out.Gate = copyGateResult(input.Gate)
	out.Metadata = copyDevAgentMetadata(input.Metadata)
	return out
}

func copyPatchProposal(in PatchProposal) PatchProposal {
	out := in
	out.Files = copyPatchFiles(in.Files)
	out.Metadata = copyDevAgentMetadata(in.Metadata)
	return out
}

func copyPatchFiles(in []PatchFile) []PatchFile {
	if len(in) == 0 {
		return nil
	}
	out := make([]PatchFile, len(in))
	for i, file := range in {
		out[i] = file
		out[i].Metadata = copyDevAgentMetadata(file.Metadata)
	}
	return out
}

func copyActionResult(in ActionResult) ActionResult {
	out := in
	out.Reasons = copyStringSlice(in.Reasons)
	out.Metadata = copyDevAgentMetadata(in.Metadata)
	return out
}

func copyGateResult(in GateResult) GateResult {
	out := in
	out.Reasons = copyStringSlice(in.Reasons)
	out.Metadata = copyDevAgentMetadata(in.Metadata)
	return out
}

func copyReviewDecision(in ReviewDecision) ReviewDecision {
	out := in
	out.Metadata = copyDevAgentMetadata(in.Metadata)
	return out
}

func copyReviewEntropyAudits(in []gopact.EntropyAudit) []gopact.EntropyAudit {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.EntropyAudit, len(in))
	for i, audit := range in {
		out[i] = audit
		out[i].Findings = copyReviewEntropyFindings(audit.Findings)
		out[i].Metadata = copyDevAgentMetadata(audit.Metadata)
	}
	return out
}

func copyReviewEntropyFindings(in []gopact.EntropyFinding) []gopact.EntropyFinding {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.EntropyFinding, len(in))
	for i, finding := range in {
		out[i] = finding
		out[i].Evidence = copyReviewVerificationEvidence(finding.Evidence)
		out[i].Metadata = copyDevAgentMetadata(finding.Metadata)
	}
	return out
}

func copyVerificationReport(in gopact.VerificationReport) gopact.VerificationReport {
	out := in
	out.Checks = copyReviewVerificationChecks(in.Checks)
	out.Metadata = copyDevAgentMetadata(in.Metadata)
	return out
}

func copyReviewVerificationChecks(in []gopact.VerificationCheck) []gopact.VerificationCheck {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.VerificationCheck, len(in))
	for i, check := range in {
		out[i] = check
		out[i].Evidence = copyReviewVerificationEvidence(check.Evidence)
		out[i].Metadata = copyDevAgentMetadata(check.Metadata)
	}
	return out
}

func copyReviewVerificationEvidence(in []gopact.VerificationEvidence) []gopact.VerificationEvidence {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.VerificationEvidence, len(in))
	for i, evidence := range in {
		out[i] = evidence
		out[i].Metadata = copyDevAgentMetadata(evidence.Metadata)
	}
	return out
}

func copyStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
