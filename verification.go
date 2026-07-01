package gopact

import (
	"errors"
	"fmt"
	"time"
)

const (
	// VerificationReportVersion is the current verification report schema version.
	VerificationReportVersion = 1

	// EventMetadataVerificationReport carries the VerificationReport emitted by a verifier node.
	EventMetadataVerificationReport = "verification_report"
)

// VerificationStatus is the outcome of a verification check or report.
type VerificationStatus string

const (
	VerificationStatusUnknown VerificationStatus = ""
	VerificationStatusPassed  VerificationStatus = "passed"
	VerificationStatusFailed  VerificationStatus = "failed"
	VerificationStatusSkipped VerificationStatus = "skipped"
	VerificationStatusPartial VerificationStatus = "partial"
)

// VerificationEvidence points at evidence used by a verification check.
type VerificationEvidence struct {
	Type     string         `json:"type,omitempty"`
	Ref      string         `json:"ref,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// VerificationCheck records one verification claim and the evidence supporting it.
type VerificationCheck struct {
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Status   VerificationStatus     `json:"status"`
	Summary  string                 `json:"summary,omitempty"`
	Evidence []VerificationEvidence `json:"evidence,omitempty"`
	Metadata map[string]any         `json:"metadata,omitempty"`
}

// VerificationReport summarizes verification evidence for one run export.
type VerificationReport struct {
	Version      int                 `json:"version"`
	IDs          RuntimeIDs          `json:"ids"`
	Outcome      RunOutcome          `json:"outcome"`
	Status       VerificationStatus  `json:"status"`
	Checks       []VerificationCheck `json:"checks,omitempty"`
	PassedCount  int                 `json:"passed_count,omitempty"`
	FailedCount  int                 `json:"failed_count,omitempty"`
	SkippedCount int                 `json:"skipped_count,omitempty"`
	CreatedAt    time.Time           `json:"created_at,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
}

// VerificationRecorder collects verification checks before building a report.
type VerificationRecorder struct {
	checks []VerificationCheck
}

// NewVerificationRecorder creates an empty verification recorder.
func NewVerificationRecorder() *VerificationRecorder {
	return &VerificationRecorder{}
}

// BuildVerificationReport creates a validated report from a run export and already-collected checks.
func BuildVerificationReport(export RunExport, checks []VerificationCheck) (VerificationReport, error) {
	if err := export.Validate(); err != nil {
		return VerificationReport{}, fmt.Errorf("gopact: build verification report: %w", err)
	}
	report := VerificationReport{
		Version:   VerificationReportVersion,
		IDs:       export.IDs,
		Outcome:   export.Outcome,
		Checks:    copyVerificationChecks(checks),
		CreatedAt: now(),
	}
	for _, check := range report.Checks {
		switch check.Status {
		case VerificationStatusPassed:
			report.PassedCount++
		case VerificationStatusFailed:
			report.FailedCount++
		case VerificationStatusSkipped:
			report.SkippedCount++
		}
	}
	report.Status = verificationReportStatus(report.PassedCount, report.FailedCount, report.SkippedCount)
	if err := report.Validate(); err != nil {
		return VerificationReport{}, err
	}
	return report, nil
}

// EmbedVerificationReport returns a copy of export with report embedded.
func EmbedVerificationReport(export RunExport, report VerificationReport) (RunExport, error) {
	if err := export.Validate(); err != nil {
		return RunExport{}, fmt.Errorf("gopact: embed verification report: %w", err)
	}
	if err := report.Validate(); err != nil {
		return RunExport{}, fmt.Errorf("gopact: embed verification report: %w", err)
	}
	if err := validateRunRecorderRuntimeIDs(export.IDs, report.IDs); err != nil {
		return RunExport{}, fmt.Errorf("gopact: embed verification report: %w", err)
	}
	if report.Outcome != export.Outcome {
		return RunExport{}, fmt.Errorf("gopact: embed verification report outcome %q does not match run export outcome %q", report.Outcome, export.Outcome)
	}

	bundled := export
	bundled.VerificationReports = append(copyVerificationReports(export.VerificationReports), copyVerificationReport(report))
	if err := bundled.Validate(); err != nil {
		return RunExport{}, fmt.Errorf("gopact: embed verification report: %w", err)
	}
	return bundled, nil
}

// Record appends one already-completed verification check.
func (r *VerificationRecorder) Record(check VerificationCheck) error {
	if r == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if err := check.Validate(); err != nil {
		return err
	}
	r.checks = append(r.checks, copyVerificationCheck(check))
	return nil
}

// Checks returns a copy of recorded verification checks.
func (r *VerificationRecorder) Checks() []VerificationCheck {
	if r == nil {
		return nil
	}
	return copyVerificationChecks(r.checks)
}

// Report builds a verification report for the supplied run export.
func (r *VerificationRecorder) Report(export RunExport) (VerificationReport, error) {
	if r == nil {
		return VerificationReport{}, errors.New("gopact: verification recorder is nil")
	}
	return BuildVerificationReport(export, r.checks)
}

// Validate checks report integrity without running any verification commands.
func (r VerificationReport) Validate() error {
	if r.Version <= 0 {
		return errors.New("gopact: verification report version is required")
	}
	if r.IDs.RunID == "" {
		return errors.New("gopact: verification report run id is required")
	}
	if !r.Outcome.valid() {
		return fmt.Errorf("gopact: verification report outcome %q is invalid", r.Outcome)
	}
	if !r.Status.validReportStatus() {
		return fmt.Errorf("gopact: verification report status %q is invalid", r.Status)
	}
	if len(r.Checks) == 0 {
		return errors.New("gopact: verification report checks are required")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("gopact: verification report created time is required")
	}

	var passed, failed, skipped int
	for i, check := range r.Checks {
		if err := check.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid verification check %d: %w", i, err)
		}
		switch check.Status {
		case VerificationStatusPassed:
			passed++
		case VerificationStatusFailed:
			failed++
		case VerificationStatusSkipped:
			skipped++
		}
	}
	if r.PassedCount != passed || r.FailedCount != failed || r.SkippedCount != skipped {
		return errors.New("gopact: verification report counts do not match checks")
	}
	expectedStatus := verificationReportStatus(passed, failed, skipped)
	if r.Status != expectedStatus {
		return fmt.Errorf("gopact: verification report status %q does not match checks status %q", r.Status, expectedStatus)
	}
	return nil
}

// Validate checks one verification check.
func (c VerificationCheck) Validate() error {
	if c.ID == "" {
		return errors.New("gopact: verification check id is required")
	}
	if !c.Status.validCheckStatus() {
		return fmt.Errorf("gopact: verification check status %q is invalid", c.Status)
	}
	if (c.Status == VerificationStatusPassed || c.Status == VerificationStatusFailed) && len(c.Evidence) == 0 {
		return errors.New("gopact: verification check evidence is required")
	}
	for i, evidence := range c.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid verification evidence %d: %w", i, err)
		}
	}
	return nil
}

// Validate checks one evidence reference.
func (e VerificationEvidence) Validate() error {
	if e.Type == "" {
		return errors.New("gopact: verification evidence type is required")
	}
	if e.Ref == "" {
		return errors.New("gopact: verification evidence ref is required")
	}
	return nil
}

func (s VerificationStatus) validCheckStatus() bool {
	switch s {
	case VerificationStatusPassed, VerificationStatusFailed, VerificationStatusSkipped:
		return true
	default:
		return false
	}
}

func (s VerificationStatus) validReportStatus() bool {
	switch s {
	case VerificationStatusPassed, VerificationStatusFailed, VerificationStatusSkipped, VerificationStatusPartial:
		return true
	default:
		return false
	}
}

func verificationReportStatus(passed, failed, skipped int) VerificationStatus {
	if failed > 0 {
		return VerificationStatusFailed
	}
	if passed > 0 && skipped > 0 {
		return VerificationStatusPartial
	}
	if passed > 0 {
		return VerificationStatusPassed
	}
	if skipped > 0 {
		return VerificationStatusSkipped
	}
	return VerificationStatusUnknown
}

func copyVerificationChecks(in []VerificationCheck) []VerificationCheck {
	if len(in) == 0 {
		return nil
	}
	out := make([]VerificationCheck, len(in))
	for i, check := range in {
		out[i] = copyVerificationCheck(check)
	}
	return out
}

func copyVerificationReports(in []VerificationReport) []VerificationReport {
	if len(in) == 0 {
		return nil
	}
	out := make([]VerificationReport, len(in))
	for i, report := range in {
		out[i] = copyVerificationReport(report)
	}
	return out
}

func copyVerificationReport(in VerificationReport) VerificationReport {
	out := in
	out.Checks = copyVerificationChecks(in.Checks)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyVerificationCheck(in VerificationCheck) VerificationCheck {
	out := in
	out.Evidence = copyVerificationEvidence(in.Evidence)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyVerificationEvidence(in []VerificationEvidence) []VerificationEvidence {
	if len(in) == 0 {
		return nil
	}
	out := make([]VerificationEvidence, len(in))
	for i, evidence := range in {
		out[i] = evidence
		out[i].Metadata = copyAnyMap(evidence.Metadata)
	}
	return out
}
