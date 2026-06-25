// Package devagent contains optional template primitives for repository development agents.
package devagent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	ErrInvalidMode         = errors.New("devagent: invalid mode")
	ErrReleaseGateRejected = errors.New("devagent: release gate rejected")
)

// Mode controls how a Dev Agent may act.
type Mode string

const (
	ModeAnalyze Mode = "analyze"
	ModePlan    Mode = "plan"
	ModeWrite   Mode = "write"
)

// ReviewStatus describes an already-observed human or reviewer-plugin decision.
type ReviewStatus string

const (
	ReviewUnknown  ReviewStatus = ""
	ReviewApproved ReviewStatus = "approved"
	ReviewRejected ReviewStatus = "rejected"
)

// GateStatus is the release gate outcome.
type GateStatus string

const (
	GatePassed   GateStatus = "passed"
	GateRejected GateStatus = "rejected"
	GateSkipped  GateStatus = "skipped"
)

// ReviewDecision records the reviewer state consumed by the release gate.
type ReviewDecision struct {
	Status   ReviewStatus   `json:"status"`
	Reviewer string         `json:"reviewer,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GateInput is the already-observed evidence consumed by the Dev Agent release gate.
type GateInput struct {
	Mode          Mode                      `json:"mode"`
	Report        gopact.VerificationReport `json:"report"`
	EntropyAudits []gopact.EntropyAudit     `json:"entropy_audits,omitempty"`
	Review        ReviewDecision            `json:"review,omitempty"`
}

// GateResult is a structured, exportable release gate decision.
type GateResult struct {
	Status             GateStatus                `json:"status"`
	Mode               Mode                      `json:"mode"`
	ReportStatus       gopact.VerificationStatus `json:"report_status,omitempty"`
	MaxEntropySeverity gopact.EntropySeverity    `json:"max_entropy_severity,omitempty"`
	ReviewStatus       ReviewStatus              `json:"review_status,omitempty"`
	Reasons            []string                  `json:"reasons,omitempty"`
	Metadata           map[string]any            `json:"metadata,omitempty"`
}

type gateConfig struct {
	requireReview      bool
	maxEntropySeverity gopact.EntropySeverity
	requiredEvidence   []string
	requiredCheckIDs   []string
	requiredCIGates    []string
}

// GateOption configures release gate evaluation.
type GateOption func(*gateConfig) error

// RequireReview controls whether write mode requires an approved reviewer decision.
func RequireReview(required bool) GateOption {
	return func(cfg *gateConfig) error {
		cfg.requireReview = required
		return nil
	}
}

// WithMaxEntropySeverity sets the highest entropy severity allowed through the gate.
func WithMaxEntropySeverity(severity gopact.EntropySeverity) GateOption {
	return func(cfg *gateConfig) error {
		if !validEntropySeverity(severity) {
			return fmt.Errorf("devagent: max entropy severity %q is invalid", severity)
		}
		cfg.maxEntropySeverity = severity
		return nil
	}
}

// RequireEvidenceTypes requires a passed verification report to contain each evidence type.
func RequireEvidenceTypes(types ...string) GateOption {
	return func(cfg *gateConfig) error {
		required := make([]string, 0, len(types))
		seen := make(map[string]struct{}, len(types))
		for _, evidenceType := range types {
			evidenceType = strings.TrimSpace(evidenceType)
			if evidenceType == "" {
				return errors.New("devagent: required evidence type is required")
			}
			if _, ok := seen[evidenceType]; ok {
				continue
			}
			seen[evidenceType] = struct{}{}
			required = append(required, evidenceType)
		}
		cfg.requiredEvidence = required
		return nil
	}
}

// RequireCIGates requires a verification report to contain each named CI gate with passed status.
func RequireCIGates(gates ...string) GateOption {
	return func(cfg *gateConfig) error {
		required := make([]string, 0, len(gates))
		seen := make(map[string]struct{}, len(gates))
		for _, gate := range gates {
			gate = strings.TrimSpace(gate)
			if gate == "" {
				return errors.New("devagent: required CI gate is required")
			}
			if _, ok := seen[gate]; ok {
				continue
			}
			seen[gate] = struct{}{}
			required = append(required, gate)
		}
		cfg.requiredCIGates = required
		return nil
	}
}

// RequireCheckIDs requires a verification report to contain each named check with passed status.
func RequireCheckIDs(ids ...string) GateOption {
	return func(cfg *gateConfig) error {
		required := make([]string, 0, len(ids))
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				return errors.New("devagent: required check id is required")
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			required = append(required, id)
		}
		cfg.requiredCheckIDs = required
		return nil
	}
}

// EvaluateReleaseGate evaluates already-collected verification, entropy, and review evidence.
func EvaluateReleaseGate(input GateInput, opts ...GateOption) (GateResult, error) {
	if !input.Mode.valid() {
		return GateResult{}, fmt.Errorf("%w: %q", ErrInvalidMode, input.Mode)
	}
	cfg := gateConfig{
		requireReview:      true,
		maxEntropySeverity: gopact.EntropySeverityMedium,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return GateResult{}, err
		}
	}

	result := GateResult{
		Mode:         input.Mode,
		ReportStatus: input.Report.Status,
		ReviewStatus: input.Review.Status,
	}
	if input.Mode != ModeWrite {
		result.Status = GateSkipped
		result.Reasons = []string{"release gate applies only to write mode"}
		return result, nil
	}

	var reasons []string
	if err := input.Report.Validate(); err != nil {
		return result, fmt.Errorf("devagent: verification report: %w", err)
	}
	if input.Report.Status != gopact.VerificationStatusPassed {
		reasons = append(reasons, fmt.Sprintf("verification status %s is not passed", input.Report.Status))
	}
	reasons = append(reasons, requiredCheckReasons(input.Report, cfg.requiredCheckIDs)...)
	reasons = append(reasons, requiredEvidenceReasons(input.Report, cfg.requiredEvidence)...)
	reasons = append(reasons, requiredCIGateReasons(input.Report, cfg.requiredCIGates)...)
	reasons = append(reasons, reviewGateReasons(input.Review, cfg.requireReview)...)

	maxSeverity, entropyReasons, err := entropyGateReasons(input.EntropyAudits, cfg.maxEntropySeverity)
	if err != nil {
		return result, err
	}
	result.MaxEntropySeverity = maxSeverity
	reasons = append(reasons, entropyReasons...)

	if len(reasons) > 0 {
		result.Status = GateRejected
		result.Reasons = reasons
		return result, fmt.Errorf("%w: %s", ErrReleaseGateRejected, strings.Join(reasons, "; "))
	}
	result.Status = GatePassed
	return result, nil
}

func requiredCheckReasons(report gopact.VerificationReport, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	byID := verificationChecksByID(report)
	reasons := make([]string, 0, len(required))
	for _, id := range required {
		check, ok := byID[id]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("required check %s is missing", id))
			continue
		}
		switch check.Status {
		case gopact.VerificationStatusPassed:
		case gopact.VerificationStatusFailed:
			reasons = append(reasons, fmt.Sprintf("required check %s failed", id))
		case gopact.VerificationStatusSkipped:
			reasons = append(reasons, fmt.Sprintf("required check %s skipped", id))
		default:
			reasons = append(reasons, fmt.Sprintf("required check %s status %s is not passed", id, check.Status))
		}
	}
	return reasons
}

func verificationChecksByID(report gopact.VerificationReport) map[string]gopact.VerificationCheck {
	byID := make(map[string]gopact.VerificationCheck, len(report.Checks))
	for _, check := range report.Checks {
		byID[check.ID] = check
	}
	return byID
}

func requiredEvidenceReasons(report gopact.VerificationReport, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	seen := verificationEvidenceTypes(report)
	reasons := make([]string, 0, len(required))
	for _, evidenceType := range required {
		if _, ok := seen[evidenceType]; !ok {
			reasons = append(reasons, fmt.Sprintf("verification evidence type %s is required", evidenceType))
		}
	}
	return reasons
}

func verificationEvidenceTypes(report gopact.VerificationReport) map[string]struct{} {
	seen := make(map[string]struct{})
	for _, check := range report.Checks {
		if check.Status != gopact.VerificationStatusPassed {
			continue
		}
		for _, evidence := range check.Evidence {
			if evidence.Type == "" {
				continue
			}
			seen[evidence.Type] = struct{}{}
		}
	}
	return seen
}

func requiredCIGateReasons(report gopact.VerificationReport, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	gates := verificationCIGates(report)
	reasons := make([]string, 0, len(required))
	for _, gate := range required {
		status, ok := gates[gate]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("required CI gate %s is missing", gate))
			continue
		}
		if status != gopact.VerificationStatusPassed {
			reasons = append(reasons, fmt.Sprintf("required CI gate %s status %s is not passed", gate, status))
		}
	}
	return reasons
}

func verificationCIGates(report gopact.VerificationReport) map[string]gopact.VerificationStatus {
	gates := make(map[string]gopact.VerificationStatus)
	for _, check := range report.Checks {
		if check.Status != gopact.VerificationStatusPassed {
			continue
		}
		for _, evidence := range check.Evidence {
			if evidence.Type != "ci_gate" {
				continue
			}
			gate, ok := evidence.Metadata["gate"].(string)
			if !ok {
				continue
			}
			gate = strings.TrimSpace(gate)
			if gate == "" {
				continue
			}
			rawStatus, ok := evidence.Metadata["status"].(string)
			if !ok || rawStatus == "" {
				continue
			}
			gates[gate] = gopact.VerificationStatus(rawStatus)
		}
	}
	return gates
}

func (m Mode) valid() bool {
	switch m {
	case ModeAnalyze, ModePlan, ModeWrite:
		return true
	default:
		return false
	}
}

func reviewGateReasons(review ReviewDecision, required bool) []string {
	if !required {
		return nil
	}
	switch review.Status {
	case ReviewApproved:
		return nil
	case ReviewRejected:
		if review.Summary != "" {
			return []string{"review rejected: " + review.Summary}
		}
		return []string{"review rejected"}
	default:
		return []string{"review approval is required"}
	}
}

func entropyGateReasons(audits []gopact.EntropyAudit, maxAllowed gopact.EntropySeverity) (gopact.EntropySeverity, []string, error) {
	var maxSeen gopact.EntropySeverity
	var reasons []string
	for i, audit := range audits {
		if err := audit.Validate(); err != nil {
			return maxSeen, nil, fmt.Errorf("devagent: entropy audit %d: %w", i, err)
		}
		if audit.Status == gopact.VerificationStatusFailed {
			reasons = append(reasons, fmt.Sprintf("entropy audit %s status failed", audit.ID))
		}
		for _, finding := range audit.Findings {
			if compareEntropySeverity(finding.Severity, maxSeen) > 0 {
				maxSeen = finding.Severity
			}
			if compareEntropySeverity(finding.Severity, maxAllowed) > 0 {
				reasons = append(reasons, fmt.Sprintf(
					"entropy finding %s severity %s exceeds %s",
					finding.ID,
					finding.Severity,
					maxAllowed,
				))
			}
		}
	}
	return maxSeen, reasons, nil
}

func validEntropySeverity(severity gopact.EntropySeverity) bool {
	_, ok := entropySeverityRank(severity)
	return ok
}

func compareEntropySeverity(left gopact.EntropySeverity, right gopact.EntropySeverity) int {
	leftRank, _ := entropySeverityRank(left)
	rightRank, _ := entropySeverityRank(right)
	return leftRank - rightRank
}

func entropySeverityRank(severity gopact.EntropySeverity) (int, bool) {
	switch severity {
	case "":
		return 0, true
	case gopact.EntropySeverityLow:
		return 1, true
	case gopact.EntropySeverityMedium:
		return 2, true
	case gopact.EntropySeverityHigh:
		return 3, true
	case gopact.EntropySeverityCritical:
		return 4, true
	default:
		return 0, false
	}
}
