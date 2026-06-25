package gopact

import (
	"errors"
	"fmt"
	"time"
)

// StepPhase describes the lifecycle phase of a workflow step.
type StepPhase string

const (
	StepPending     StepPhase = "pending"
	StepRunning     StepPhase = "running"
	StepCompleted   StepPhase = "completed"
	StepFailed      StepPhase = "failed"
	StepCanceled    StepPhase = "canceled"
	StepInterrupted StepPhase = "interrupted"
)

// EffectReplayPolicy describes whether an external effect may be replayed or skipped.
type EffectReplayPolicy string

const (
	EffectReplayUnspecified EffectReplayPolicy = ""
	EffectReplayRecordOnly  EffectReplayPolicy = "record_only"
	EffectReplayIdempotent  EffectReplayPolicy = "idempotent"
	EffectReplaySkip        EffectReplayPolicy = "skip"
)

// SandboxEffect describes a sandbox operation attached to a step effect.
type SandboxEffect struct {
	SessionID string         `json:"session_id,omitempty"`
	Operation string         `json:"operation,omitempty"`
	Path      string         `json:"path,omitempty"`
	Command   []string       `json:"command,omitempty"`
	ExitCode  int            `json:"exit_code,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// EffectRecord describes an external effect attached to a step.
type EffectRecord struct {
	ID             string             `json:"id,omitempty"`
	Type           string             `json:"type,omitempty"`
	Target         string             `json:"target,omitempty"`
	Applied        bool               `json:"applied,omitempty"`
	ReplayPolicy   EffectReplayPolicy `json:"replay_policy,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	DependsOn      []string           `json:"depends_on,omitempty"`
	Artifacts      []ArtifactRef      `json:"artifacts,omitempty"`
	Sandbox        *SandboxEffect     `json:"sandbox,omitempty"`
	Metadata       map[string]any     `json:"metadata,omitempty"`
}

// StepSnapshot is the step-level export/import boundary for resumable workflows.
type StepSnapshot struct {
	ID          string           `json:"id"`
	Step        int              `json:"step"`
	Node        string           `json:"node"`
	Phase       StepPhase        `json:"phase"`
	IDs         RuntimeIDs       `json:"ids,omitempty"`
	Input       any              `json:"input,omitempty"`
	Output      any              `json:"output,omitempty"`
	Queue       []string         `json:"queue,omitempty"`
	Pending     *InterruptRecord `json:"pending,omitempty"`
	Error       string           `json:"error,omitempty"`
	Effects     []EffectRecord   `json:"effects,omitempty"`
	Artifacts   []ArtifactRef    `json:"artifacts,omitempty"`
	StartedAt   time.Time        `json:"started_at,omitempty"`
	CompletedAt time.Time        `json:"completed_at,omitempty"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

// Validate checks whether the step snapshot has the minimum resumable identity.
func (s StepSnapshot) Validate() error {
	if s.ID == "" {
		return errors.New("gopact: step snapshot id is required")
	}
	if s.Step < 0 {
		return fmt.Errorf("gopact: step must be non-negative, got %d", s.Step)
	}
	if s.Node == "" {
		return errors.New("gopact: step snapshot node is required")
	}
	if s.Phase == "" {
		return errors.New("gopact: step snapshot phase is required")
	}
	if !s.Phase.valid() {
		return fmt.Errorf("gopact: step snapshot phase %q is invalid", s.Phase)
	}
	if s.Phase == StepInterrupted {
		if s.Pending == nil {
			return errors.New("gopact: interrupted step pending record is required")
		}
		if err := s.Pending.Validate(); err != nil {
			return fmt.Errorf("gopact: interrupted step pending record: %w", err)
		}
	}
	if err := validateEffectRecords(s.Effects); err != nil {
		return fmt.Errorf("gopact: step effects: %w", err)
	}
	return nil
}

// StepExport is a versioned representation of a single resumable step.
type StepExport struct {
	Version  int            `json:"version"`
	Step     StepSnapshot   `json:"step"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks whether the step export can be imported safely.
func (e StepExport) Validate() error {
	if e.Version <= 0 {
		return errors.New("gopact: step export version is required")
	}
	if err := e.Step.Validate(); err != nil {
		return err
	}
	return nil
}

func (p StepPhase) valid() bool {
	switch p {
	case StepPending, StepRunning, StepCompleted, StepFailed, StepCanceled, StepInterrupted:
		return true
	default:
		return false
	}
}

func (p EffectReplayPolicy) valid() bool {
	switch p {
	case EffectReplayUnspecified, EffectReplayRecordOnly, EffectReplayIdempotent, EffectReplaySkip:
		return true
	default:
		return false
	}
}

func validateEffectRecords(effects []EffectRecord) error {
	ids := make(map[string]struct{}, len(effects))
	for _, effect := range effects {
		if !effect.ReplayPolicy.valid() {
			return fmt.Errorf("effect %q replay policy %q is invalid", effect.ID, effect.ReplayPolicy)
		}
		if effect.ReplayPolicy == EffectReplayIdempotent && effect.IdempotencyKey == "" {
			return fmt.Errorf("effect %q idempotency key is required", effect.ID)
		}
		if effect.ID == "" {
			continue
		}
		if _, ok := ids[effect.ID]; ok {
			return fmt.Errorf("effect id %q is duplicated", effect.ID)
		}
		ids[effect.ID] = struct{}{}
	}
	for _, effect := range effects {
		for _, dep := range effect.DependsOn {
			if dep == "" {
				return fmt.Errorf("effect %q dependency id is required", effect.ID)
			}
		}
	}
	return nil
}
