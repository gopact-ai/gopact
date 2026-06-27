package gopact

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrPromptInjectionDetectorRequired = errors.New("gopact: prompt injection detector is required")
	ErrPromptInjectionPolicyRequired   = errors.New("gopact: prompt injection policy is required")
)

// PromptInjectionSeverity describes the detector-assigned risk level.
type PromptInjectionSeverity string

const (
	// PromptInjectionSeverity values are intentionally coarse for policy routing.
	PromptInjectionSeverityLow      PromptInjectionSeverity = "low"
	PromptInjectionSeverityMedium   PromptInjectionSeverity = "medium"
	PromptInjectionSeverityHigh     PromptInjectionSeverity = "high"
	PromptInjectionSeverityCritical PromptInjectionSeverity = "critical"
)

// PromptInjectionSource identifies the model-visible source that triggered a finding.
type PromptInjectionSource string

const (
	// PromptInjectionSource values identify model-visible input classes.
	PromptInjectionSourceUnknown     PromptInjectionSource = "unknown"
	PromptInjectionSourceSystem      PromptInjectionSource = "system"
	PromptInjectionSourceUserMessage PromptInjectionSource = "user_message"
	PromptInjectionSourceToolResult  PromptInjectionSource = "tool_result"
	PromptInjectionSourceMemory      PromptInjectionSource = "memory"
	PromptInjectionSourceMCP         PromptInjectionSource = "mcp"
	PromptInjectionSourceA2A         PromptInjectionSource = "a2a"
	PromptInjectionSourceSkill       PromptInjectionSource = "skill"
	PromptInjectionSourceArtifact    PromptInjectionSource = "artifact"
)

// PromptInjectionFinding is a payload-free detector finding.
type PromptInjectionFinding struct {
	RuleID   string                  `json:"rule_id,omitempty"`
	Severity PromptInjectionSeverity `json:"severity,omitempty"`
	Source   PromptInjectionSource   `json:"source,omitempty"`
	Field    string                  `json:"field,omitempty"`
}

// PromptInjectionReport is the detector output passed to policy.
type PromptInjectionReport struct {
	Findings []PromptInjectionFinding `json:"findings,omitempty"`
}

// PromptInjectionPolicyInput is the stable policy input for prompt-injection inspection.
type PromptInjectionPolicyInput struct {
	Report PromptInjectionReport `json:"report"`
}

// PromptInjectionDetector inspects a model request before provider invocation.
type PromptInjectionDetector interface {
	DetectPromptInjection(ctx context.Context, request ModelRequest) (PromptInjectionReport, error)
}

// PromptInjectionDetectorFunc adapts a function into a PromptInjectionDetector.
type PromptInjectionDetectorFunc func(ctx context.Context, request ModelRequest) (PromptInjectionReport, error)

// DetectPromptInjection calls f.
func (f PromptInjectionDetectorFunc) DetectPromptInjection(
	ctx context.Context,
	request ModelRequest,
) (PromptInjectionReport, error) {
	if f == nil {
		return PromptInjectionReport{}, ErrPromptInjectionDetectorRequired
	}
	return f(ctx, request)
}

// PromptInjectionGuardMiddleware inspects model input and gates findings through policy.
func PromptInjectionGuardMiddleware(detector PromptInjectionDetector, policy Policy) ModelHandler {
	return func(c *ModelContext) error {
		if c == nil {
			c = NewModelContext(context.TODO(), ModelContextOptions{})
		}
		if detector == nil {
			return ErrPromptInjectionDetectorRequired
		}
		report, err := detector.DetectPromptInjection(c.Context, copyModelRequest(c.Request))
		if err != nil {
			return fmt.Errorf("gopact: detect prompt injection: %w", err)
		}
		report = copyPromptInjectionReport(report)
		if len(report.Findings) == 0 {
			return c.Next()
		}
		if policy == nil {
			return ErrPromptInjectionPolicyRequired
		}
		req := PolicyRequest{
			IDs:      c.Request.IDs,
			Boundary: PolicyBoundaryModel,
			Action:   PolicyActionInspect,
			Input:    PromptInjectionPolicyInput{Report: report},
			Metadata: copyAnyMap(c.Metadata),
		}
		c.AddEvent(policyRequestedEvent(req))
		decision, err := policy.Decide(c.Context, copyPolicyRequest(req))
		if err != nil {
			return fmt.Errorf("gopact: prompt injection policy: %w", err)
		}
		c.AddEvent(policyDecidedEvent(req, decision))
		if decision.Action == PolicyReview {
			return Interrupt(policyApprovalRecord(req, decision))
		}
		if !decision.Allowed() {
			return &PolicyDeniedError{Decision: decision, Request: req}
		}
		return c.Next()
	}
}

func copyPromptInjectionReport(in PromptInjectionReport) PromptInjectionReport {
	return PromptInjectionReport{
		Findings: append([]PromptInjectionFinding(nil), in.Findings...),
	}
}

func copyPromptInjectionPolicyInput(in PromptInjectionPolicyInput) PromptInjectionPolicyInput {
	return PromptInjectionPolicyInput{
		Report: copyPromptInjectionReport(in.Report),
	}
}
