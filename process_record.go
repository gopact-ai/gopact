package gopact

import (
	"errors"
	"fmt"
	"time"
)

// TaskStatus describes the lifecycle status of a user or system task.
type TaskStatus string

const (
	TaskPending     TaskStatus = "pending"
	TaskRunning     TaskStatus = "running"
	TaskCompleted   TaskStatus = "completed"
	TaskFailed      TaskStatus = "failed"
	TaskCanceled    TaskStatus = "canceled"
	TaskInterrupted TaskStatus = "interrupted"
)

// InputKind describes the origin or role of an input record.
type InputKind string

const (
	InputUser     InputKind = "user"
	InputSystem   InputKind = "system"
	InputResume   InputKind = "resume"
	InputExternal InputKind = "external"
)

// InterventionStatus describes the lifecycle status of a HITL intervention.
type InterventionStatus string

const (
	InterventionRequested InterventionStatus = "requested"
	InterventionResolved  InterventionStatus = "resolved"
	InterventionRejected  InterventionStatus = "rejected"
	InterventionCanceled  InterventionStatus = "canceled"
)

// TaskRecord records the task a run is trying to accomplish.
type TaskRecord struct {
	ID          string         `json:"id"`
	ParentID    string         `json:"parent_id,omitempty"`
	Name        string         `json:"name,omitempty"`
	Status      TaskStatus     `json:"status"`
	IDs         RuntimeIDs     `json:"ids,omitempty"`
	Input       any            `json:"input,omitempty"`
	Output      any            `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
	Artifacts   []ArtifactRef  `json:"artifacts,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	StartedAt   time.Time      `json:"started_at,omitempty"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// InputRecord records an input observed by a run or turn loop.
type InputRecord struct {
	ID        string         `json:"id"`
	Kind      InputKind      `json:"kind"`
	IDs       RuntimeIDs     `json:"ids,omitempty"`
	Source    string         `json:"source,omitempty"`
	Value     any            `json:"value,omitempty"`
	Resume    *ResumeRequest `json:"resume,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// InterventionRecord records a human or external intervention boundary.
type InterventionRecord struct {
	ID         string             `json:"id"`
	Type       InterruptType      `json:"type"`
	Status     InterventionStatus `json:"status"`
	IDs        RuntimeIDs         `json:"ids,omitempty"`
	Request    *InterruptRecord   `json:"request,omitempty"`
	Resume     *ResumeRequest     `json:"resume,omitempty"`
	CreatedAt  time.Time          `json:"created_at,omitempty"`
	ResolvedAt time.Time          `json:"resolved_at,omitempty"`
	Metadata   map[string]any     `json:"metadata,omitempty"`
}

// Validate checks whether the task record has stable identity and status.
func (r TaskRecord) Validate() error {
	if r.ID == "" {
		return errors.New("gopact: task record id is required")
	}
	if !r.Status.valid() {
		return fmt.Errorf("gopact: task record status %q is invalid", r.Status)
	}
	return nil
}

// Validate checks whether the input record has stable identity and kind.
func (r InputRecord) Validate() error {
	if r.ID == "" {
		return errors.New("gopact: input record id is required")
	}
	if !r.Kind.valid() {
		return fmt.Errorf("gopact: input record kind %q is invalid", r.Kind)
	}
	if r.Resume != nil {
		if err := r.Resume.Validate(); err != nil {
			return fmt.Errorf("gopact: input record resume request: %w", err)
		}
	}
	return nil
}

// Validate checks whether the intervention record has stable identity and status.
func (r InterventionRecord) Validate() error {
	if r.ID == "" {
		return errors.New("gopact: intervention record id is required")
	}
	if !r.Type.valid() {
		return fmt.Errorf("gopact: intervention record type %q is invalid", r.Type)
	}
	if !r.Status.valid() {
		return fmt.Errorf("gopact: intervention record status %q is invalid", r.Status)
	}
	if r.Request != nil {
		if err := r.Request.Validate(); err != nil {
			return fmt.Errorf("gopact: intervention record request: %w", err)
		}
	}
	if r.Resume != nil {
		if err := r.Resume.Validate(); err != nil {
			return fmt.Errorf("gopact: intervention record resume request: %w", err)
		}
	}
	return nil
}

func (s TaskStatus) valid() bool {
	switch s {
	case TaskPending, TaskRunning, TaskCompleted, TaskFailed, TaskCanceled, TaskInterrupted:
		return true
	default:
		return false
	}
}

func (k InputKind) valid() bool {
	switch k {
	case InputUser, InputSystem, InputResume, InputExternal:
		return true
	default:
		return false
	}
}

func (s InterventionStatus) valid() bool {
	switch s {
	case InterventionRequested, InterventionResolved, InterventionRejected, InterventionCanceled:
		return true
	default:
		return false
	}
}

func copyTaskRecords(in []TaskRecord) []TaskRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]TaskRecord, len(in))
	for i, record := range in {
		out[i] = copyTaskRecord(record)
	}
	return out
}

func copyTaskRecord(in TaskRecord) TaskRecord {
	out := in
	out.Artifacts = copyArtifactRefs(in.Artifacts)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyInputRecords(in []InputRecord) []InputRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]InputRecord, len(in))
	for i, record := range in {
		out[i] = copyInputRecord(record)
	}
	return out
}

func copyInputRecord(in InputRecord) InputRecord {
	out := in
	if in.Resume != nil {
		resume := copyResumeRequest(*in.Resume)
		out.Resume = &resume
	}
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyInterventionRecords(in []InterventionRecord) []InterventionRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]InterventionRecord, len(in))
	for i, record := range in {
		out[i] = copyInterventionRecord(record)
	}
	return out
}

func copyInterventionRecord(in InterventionRecord) InterventionRecord {
	out := in
	if in.Request != nil {
		request := copyInterruptRecord(*in.Request)
		out.Request = &request
	}
	if in.Resume != nil {
		resume := copyResumeRequest(*in.Resume)
		out.Resume = &resume
	}
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyInterruptRecord(in InterruptRecord) InterruptRecord {
	out := in
	out.ResumeSchema = copyJSONSchema(in.ResumeSchema)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyResumeRequest(in ResumeRequest) ResumeRequest {
	out := in
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyJSONSchema(in JSONSchema) JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(JSONSchema, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
