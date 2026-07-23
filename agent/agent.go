// Package agent defines the minimal Agent domain protocol and its Workflow-backed facade.
package agent

import (
	"maps"
	"slices"

	"github.com/gopact-ai/gopact"
)

// Identity is immutable Agent identity used by directories, events, and checkpoints.
type Identity struct {
	Name        string
	Description string
	Version     string
}

// Request is the provider-neutral input shared by public ADK Agents.
type Request struct {
	Messages  []gopact.Message
	Artifacts []gopact.ArtifactRef
	Metadata  map[string]string
}

// Clone returns an independent copy of the request-owned mutable values.
func (request Request) Clone() Request {
	request.Messages = cloneMessages(request.Messages)
	request.Artifacts = slices.Clone(request.Artifacts)
	request.Metadata = maps.Clone(request.Metadata)
	return request
}

// Response is the final provider-neutral output shared by public ADK Agents.
type Response struct {
	Message   gopact.Message
	Artifacts []gopact.ArtifactRef
	Metadata  map[string]string
}

// Clone returns an independent copy of the response-owned mutable values.
func (response Response) Clone() Response {
	response.Message = response.Message.Clone()
	response.Artifacts = slices.Clone(response.Artifacts)
	response.Metadata = maps.Clone(response.Metadata)
	return response
}

// Chunk is one user-visible streaming Agent output item.
type Chunk struct {
	Text  string
	Parts []gopact.MessagePart
}

// Agent is the minimal public ADK Agent protocol. Direct implementations do not
// acquire Workflow checkpoint, recovery, control, or history semantics; use a
// WorkflowAgent when those runtime guarantees are required.
type Agent interface {
	gopact.Invokable[Request, Response]
	Identity() Identity
}

// StreamingAgent adds user-visible output streaming to an Agent.
type StreamingAgent interface {
	Agent
	gopact.StreamingInvokable[Request, Chunk]
}

// ObservationKind identifies model-facing feedback semantics.
type ObservationKind string

// Observation kinds.
const (
	ObservationToolResult    ObservationKind = "tool_result"
	ObservationToolRejected  ObservationKind = "tool_rejected"
	ObservationToolError     ObservationKind = "tool_error"
	ObservationGuardRejected ObservationKind = "guard_rejected"
	ObservationModelFeedback ObservationKind = "model_feedback"
)

// ObservationSourceKind identifies the fact that produced an observation.
type ObservationSourceKind string

// Observation source kinds.
const (
	ObservationSourceToolOutcome    ObservationSourceKind = "tool_outcome"
	ObservationSourceGuardRejection ObservationSourceKind = "guard_rejection"
	ObservationSourceModelFeedback  ObservationSourceKind = "model_feedback"
)

// ObservationSource identifies the source fact within an agent run.
type ObservationSource struct {
	Kind ObservationSourceKind
	ID   string
}

// ObservationSubject identifies what the feedback concerns.
type ObservationSubject struct {
	ToolCallID string
	ToolName   string
	GuardName  string
	SubjectRef string
}

// Observation is one fact fed back into an agent loop.
type Observation struct {
	ID        string
	Kind      ObservationKind
	Source    ObservationSource
	Subject   ObservationSubject
	Message   gopact.Message
	Summary   string
	Refs      []gopact.ArtifactRef
	RetryHint *gopact.RetryHint
}
