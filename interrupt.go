package gopact

import (
	"errors"
	"fmt"
	"time"
)

const (
	// MetadataResumeRequest carries the ResumeRequest that authorized a resumed boundary.
	MetadataResumeRequest = "resume_request"
	// MetadataResumePayload carries the ResumeRequest payload for policy and audit code.
	MetadataResumePayload = "resume_payload"
)

// ErrInterrupted marks a run that paused at an interrupt boundary.
var ErrInterrupted = errors.New("gopact: interrupted")

// InterruptType identifies why a run paused.
type InterruptType string

const (
	InterruptApproval     InterruptType = "approval"
	InterruptInput        InterruptType = "input"
	InterruptSelection    InterruptType = "selection"
	InterruptExternalWait InterruptType = "external_wait"
)

// InterruptRecord describes a resumable pause point.
type InterruptRecord struct {
	ID           string         `json:"id"`
	Type         InterruptType  `json:"type"`
	Reason       string         `json:"reason,omitempty"`
	Prompt       Message        `json:"prompt,omitempty"`
	RequiredBy   string         `json:"required_by,omitempty"`
	ResumeSchema JSONSchema     `json:"resume_schema,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Validate checks whether the interrupt record can be used for resume matching.
func (r InterruptRecord) Validate() error {
	if r.ID == "" {
		return errors.New("gopact: interrupt id is required")
	}
	if r.Type == "" {
		return errors.New("gopact: interrupt type is required")
	}
	if !r.Type.valid() {
		return fmt.Errorf("gopact: interrupt type %q is invalid", r.Type)
	}
	return nil
}

// ResumeRequest carries the external payload that resumes an interrupted step.
type ResumeRequest struct {
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	StepID       string         `json:"step_id,omitempty"`
	InterruptID  string         `json:"interrupt_id"`
	IDs          RuntimeIDs     `json:"ids,omitempty"`
	Payload      any            `json:"payload,omitempty"`
	PayloadCodec string         `json:"payload_codec,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Validate checks whether the resume request can be matched to an interrupt.
func (r ResumeRequest) Validate() error {
	if r.InterruptID == "" {
		return errors.New("gopact: resume interrupt id is required")
	}
	return nil
}

// InterruptError carries a pause request through ordinary Go error flow.
type InterruptError struct {
	Record InterruptRecord
}

// Interrupt creates an interrupt error for a node to return.
func Interrupt(record InterruptRecord) *InterruptError {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now()
	}
	return &InterruptError{Record: record}
}

// Error describes the interrupt.
func (e *InterruptError) Error() string {
	if e == nil || e.Record.Reason == "" {
		return ErrInterrupted.Error()
	}
	return fmt.Sprintf("%s: %s", ErrInterrupted, e.Record.Reason)
}

// Unwrap makes errors.Is(err, ErrInterrupted) work.
func (e *InterruptError) Unwrap() error {
	return ErrInterrupted
}

func (t InterruptType) valid() bool {
	switch t {
	case InterruptApproval, InterruptInput, InterruptSelection, InterruptExternalWait:
		return true
	default:
		return false
	}
}
