package gopact

import (
	"errors"
	"fmt"
	"time"
)

// EntropyCategory classifies an audit finding about process or artifact drift.
type EntropyCategory string

const (
	EntropyDiff       EntropyCategory = "diff"
	EntropyDependency EntropyCategory = "dependency"
	EntropyStaleDocs  EntropyCategory = "stale_docs"
	EntropyResidue    EntropyCategory = "residue"
	EntropyProcess    EntropyCategory = "process"
	EntropySecurity   EntropyCategory = "security"
)

// EntropySeverity describes the impact of an entropy finding.
type EntropySeverity string

const (
	EntropySeverityLow      EntropySeverity = "low"
	EntropySeverityMedium   EntropySeverity = "medium"
	EntropySeverityHigh     EntropySeverity = "high"
	EntropySeverityCritical EntropySeverity = "critical"
)

// EntropyAudit records already-observed entropy findings for a run.
type EntropyAudit struct {
	ID        string             `json:"id"`
	Status    VerificationStatus `json:"status"`
	IDs       RuntimeIDs         `json:"ids,omitempty"`
	Findings  []EntropyFinding   `json:"findings,omitempty"`
	CreatedAt time.Time          `json:"created_at,omitempty"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
}

// EntropyFinding records one drift, residue, or audit-quality concern.
type EntropyFinding struct {
	ID        string                 `json:"id"`
	Category  EntropyCategory        `json:"category"`
	Severity  EntropySeverity        `json:"severity"`
	Summary   string                 `json:"summary,omitempty"`
	Evidence  []VerificationEvidence `json:"evidence,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	Metadata  map[string]any         `json:"metadata,omitempty"`
}

// Validate checks whether the entropy audit is structurally usable.
func (a EntropyAudit) Validate() error {
	if a.ID == "" {
		return errors.New("gopact: entropy audit id is required")
	}
	if !a.Status.validReportStatus() {
		return fmt.Errorf("gopact: entropy audit status %q is invalid", a.Status)
	}
	for i, finding := range a.Findings {
		if err := finding.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid entropy finding %d: %w", i, err)
		}
	}
	return nil
}

// Validate checks whether the entropy finding has stable identity, category, and severity.
func (f EntropyFinding) Validate() error {
	if f.ID == "" {
		return errors.New("gopact: entropy finding id is required")
	}
	if !f.Category.valid() {
		return fmt.Errorf("gopact: entropy finding category %q is invalid", f.Category)
	}
	if !f.Severity.valid() {
		return fmt.Errorf("gopact: entropy finding severity %q is invalid", f.Severity)
	}
	if f.Summary == "" && len(f.Evidence) == 0 {
		return errors.New("gopact: entropy finding summary or evidence is required")
	}
	for i, evidence := range f.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid entropy finding evidence %d: %w", i, err)
		}
	}
	return nil
}

func (c EntropyCategory) valid() bool {
	switch c {
	case EntropyDiff, EntropyDependency, EntropyStaleDocs, EntropyResidue, EntropyProcess, EntropySecurity:
		return true
	default:
		return false
	}
}

func (s EntropySeverity) valid() bool {
	switch s {
	case EntropySeverityLow, EntropySeverityMedium, EntropySeverityHigh, EntropySeverityCritical:
		return true
	default:
		return false
	}
}

func copyEntropyAudits(in []EntropyAudit) []EntropyAudit {
	if len(in) == 0 {
		return nil
	}
	out := make([]EntropyAudit, len(in))
	for i, audit := range in {
		out[i] = copyEntropyAudit(audit)
	}
	return out
}

func copyEntropyAudit(in EntropyAudit) EntropyAudit {
	out := in
	out.Findings = copyEntropyFindings(in.Findings)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyEntropyFindings(in []EntropyFinding) []EntropyFinding {
	if len(in) == 0 {
		return nil
	}
	out := make([]EntropyFinding, len(in))
	for i, finding := range in {
		out[i] = copyEntropyFinding(finding)
	}
	return out
}

func copyEntropyFinding(in EntropyFinding) EntropyFinding {
	out := in
	out.Evidence = copyVerificationEvidence(in.Evidence)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}
