// Package cireview adapts already-observed CI verification evidence into Dev Agent review decisions.
package cireview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

var (
	// ErrReviewerRequired is returned when a reviewer identity is required but missing.
	ErrReviewerRequired = errors.New("cireview: reviewer is required")
	// ErrReportRequired is returned when CI review has no verification report.
	ErrReportRequired = errors.New("cireview: verification report is required")
	// ErrRequiredCheckRequired is returned when an empty required check is configured.
	ErrRequiredCheckRequired = errors.New("cireview: required check is required")
	// ErrRequiredCIGateRequired is returned when an empty required CI gate is configured.
	ErrRequiredCIGateRequired = errors.New("cireview: required CI gate is required")
	// ErrInvalidEntropySeverity is returned when an entropy threshold is not valid.
	ErrInvalidEntropySeverity = errors.New("cireview: invalid entropy severity")
)

type config struct {
	reviewer           string
	requiredChecks     []string
	requiredCIGates    []string
	maxEntropySeverity gopact.EntropySeverity
}

const ciGateEvidenceType = "ci_gate"

// Option configures a CI-backed reviewer.
type Option func(*config) error

// WithReviewer sets the reviewer identity attached to CI review decisions.
func WithReviewer(reviewer string) Option {
	return func(cfg *config) error {
		reviewer = strings.TrimSpace(reviewer)
		if reviewer == "" {
			return ErrReviewerRequired
		}
		cfg.reviewer = reviewer
		return nil
	}
}

// WithRequiredChecks requires the named verification checks to be present and passed.
func WithRequiredChecks(ids ...string) Option {
	return func(cfg *config) error {
		if len(ids) == 0 {
			return ErrRequiredCheckRequired
		}
		checks := make([]string, 0, len(ids))
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				return ErrRequiredCheckRequired
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			checks = append(checks, id)
		}
		cfg.requiredChecks = checks
		return nil
	}
}

// WithRequiredCIGates requires named ci_gate verification evidence to be present and passed.
func WithRequiredCIGates(gates ...string) Option {
	return func(cfg *config) error {
		if len(gates) == 0 {
			return ErrRequiredCIGateRequired
		}
		required := make([]string, 0, len(gates))
		seen := make(map[string]struct{}, len(gates))
		for _, gate := range gates {
			gate = strings.TrimSpace(gate)
			if gate == "" {
				return ErrRequiredCIGateRequired
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

// WithMaxEntropySeverity sets the highest entropy finding severity allowed by CI review.
func WithMaxEntropySeverity(severity gopact.EntropySeverity) Option {
	return func(cfg *config) error {
		if !validEntropySeverity(severity) {
			return fmt.Errorf("%w: %q", ErrInvalidEntropySeverity, severity)
		}
		cfg.maxEntropySeverity = severity
		return nil
	}
}

// Reviewer turns verification reports and entropy audits into explicit Dev Agent review decisions.
type Reviewer struct {
	cfg config
}

var _ devagent.Reviewer = (*Reviewer)(nil)

// New creates a reviewer that evaluates already-observed CI evidence.
func New(opts ...Option) (*Reviewer, error) {
	cfg := config{
		reviewer:           "ci",
		maxEntropySeverity: gopact.EntropySeverityMedium,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	return &Reviewer{cfg: cfg}, nil
}

// Review approves only when the verification report passes and configured CI constraints hold.
func (r *Reviewer) Review(ctx context.Context, input devagent.ReviewInput) (devagent.ReviewDecision, error) {
	if r == nil {
		r = &Reviewer{cfg: config{reviewer: "ci", maxEntropySeverity: gopact.EntropySeverityMedium}}
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return devagent.ReviewDecision{}, err
	}
	if reportMissing(input.Report) {
		return devagent.ReviewDecision{}, ErrReportRequired
	}
	if err := input.Report.Validate(); err != nil {
		return devagent.ReviewDecision{}, fmt.Errorf("cireview: verification report: %w", err)
	}

	reasons, metadata, err := r.evaluate(input)
	if err != nil {
		return devagent.ReviewDecision{}, err
	}
	status := devagent.ReviewApproved
	summary := "ci verification passed"
	if len(reasons) > 0 {
		status = devagent.ReviewRejected
		summary = strings.Join(reasons, "; ")
	}
	return devagent.ReviewDecision{
		Status:   status,
		Reviewer: r.cfg.reviewer,
		Summary:  summary,
		Metadata: metadata,
	}, nil
}

func (r *Reviewer) evaluate(input devagent.ReviewInput) ([]string, map[string]any, error) {
	metadata := map[string]any{
		"adapter":           "cireview",
		"report_status":     string(input.Report.Status),
		"passed_count":      input.Report.PassedCount,
		"failed_count":      input.Report.FailedCount,
		"skipped_count":     input.Report.SkippedCount,
		"required_checks":   append([]string(nil), r.cfg.requiredChecks...),
		"required_ci_gates": append([]string(nil), r.cfg.requiredCIGates...),
	}
	if input.Mode != "" {
		metadata["mode"] = string(input.Mode)
	}
	if input.Patch.ID != "" {
		metadata["patch_id"] = input.Patch.ID
	}

	var reasons []string
	if input.Report.Status != gopact.VerificationStatusPassed {
		reasons = append(reasons, fmt.Sprintf("verification status %s is not passed", input.Report.Status))
	}

	missingChecks, failedChecks, skippedChecks := checkReasons(input.Report.Checks, r.cfg.requiredChecks)
	if len(missingChecks) > 0 {
		for _, id := range missingChecks {
			reasons = append(reasons, fmt.Sprintf("required check %s is missing", id))
		}
		metadata["missing_checks"] = missingChecks
	}
	if len(failedChecks) > 0 {
		for _, id := range failedChecks {
			reasons = append(reasons, fmt.Sprintf("check %s failed", id))
		}
		metadata["failed_checks"] = failedChecks
	}
	if len(skippedChecks) > 0 {
		for _, id := range skippedChecks {
			reasons = append(reasons, fmt.Sprintf("check %s skipped", id))
		}
		metadata["skipped_checks"] = skippedChecks
	}

	gateStatuses := ciGateStatuses(input.Report.Checks)
	missingGates, failedGates, skippedGates := ciGateReasons(gateStatuses, r.cfg.requiredCIGates)
	if len(missingGates) > 0 {
		for _, gate := range missingGates {
			reasons = append(reasons, fmt.Sprintf("required CI gate %s is missing", gate))
		}
		metadata["missing_ci_gates"] = missingGates
	}
	if len(failedGates) > 0 {
		for _, gate := range failedGates {
			reasons = append(reasons, fmt.Sprintf(
				"required CI gate %s status %s is not passed",
				gate,
				gateStatuses[gate],
			))
		}
		metadata["failed_ci_gates"] = failedGates
	}
	if len(skippedGates) > 0 {
		for _, gate := range skippedGates {
			reasons = append(reasons, fmt.Sprintf("required CI gate %s status skipped is not passed", gate))
		}
		metadata["skipped_ci_gates"] = skippedGates
	}

	maxSeverity, entropyReasons, err := entropyReasons(input.EntropyAudits, r.cfg.maxEntropySeverity)
	if err != nil {
		return nil, nil, err
	}
	if maxSeverity != "" {
		metadata["max_entropy_severity"] = string(maxSeverity)
	}
	reasons = append(reasons, entropyReasons...)
	return reasons, metadata, nil
}

func checkReasons(checks []gopact.VerificationCheck, required []string) ([]string, []string, []string) {
	byID := make(map[string]gopact.VerificationCheck, len(checks))
	var failed []string
	var skipped []string
	for _, check := range checks {
		byID[check.ID] = check
		switch check.Status {
		case gopact.VerificationStatusFailed:
			failed = append(failed, check.ID)
		case gopact.VerificationStatusSkipped:
			skipped = append(skipped, check.ID)
		}
	}

	var missing []string
	for _, id := range required {
		check, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if check.Status == gopact.VerificationStatusFailed && !containsString(failed, id) {
			failed = append(failed, id)
		}
		if check.Status == gopact.VerificationStatusSkipped && !containsString(skipped, id) {
			skipped = append(skipped, id)
		}
	}
	return missing, failed, skipped
}

func ciGateReasons(gates map[string]gopact.VerificationStatus, required []string) ([]string, []string, []string) {
	var missing []string
	var failed []string
	var skipped []string
	for _, gate := range required {
		status, ok := gates[gate]
		if !ok {
			missing = append(missing, gate)
			continue
		}
		switch status {
		case gopact.VerificationStatusPassed:
		case gopact.VerificationStatusSkipped:
			skipped = append(skipped, gate)
		default:
			failed = append(failed, gate)
		}
	}
	return missing, failed, skipped
}

func ciGateStatuses(checks []gopact.VerificationCheck) map[string]gopact.VerificationStatus {
	gates := make(map[string]gopact.VerificationStatus)
	for _, check := range checks {
		if check.Status != gopact.VerificationStatusPassed {
			continue
		}
		for _, evidence := range check.Evidence {
			if evidence.Type != ciGateEvidenceType {
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

func entropyReasons(audits []gopact.EntropyAudit, maxAllowed gopact.EntropySeverity) (gopact.EntropySeverity, []string, error) {
	var maxSeen gopact.EntropySeverity
	var reasons []string
	for i, audit := range audits {
		if err := audit.Validate(); err != nil {
			return "", nil, fmt.Errorf("cireview: entropy audit %d: %w", i, err)
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

func reportMissing(report gopact.VerificationReport) bool {
	return report.Version == 0 && report.Status == "" && len(report.Checks) == 0
}

func validEntropySeverity(severity gopact.EntropySeverity) bool {
	switch severity {
	case gopact.EntropySeverityLow, gopact.EntropySeverityMedium, gopact.EntropySeverityHigh, gopact.EntropySeverityCritical:
		return true
	default:
		return false
	}
}

func compareEntropySeverity(left gopact.EntropySeverity, right gopact.EntropySeverity) int {
	leftRank := entropySeverityRank(left)
	rightRank := entropySeverityRank(right)
	return leftRank - rightRank
}

func entropySeverityRank(severity gopact.EntropySeverity) int {
	switch severity {
	case gopact.EntropySeverityLow:
		return 1
	case gopact.EntropySeverityMedium:
		return 2
	case gopact.EntropySeverityHigh:
		return 3
	case gopact.EntropySeverityCritical:
		return 4
	default:
		return 0
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
