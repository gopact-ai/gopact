// Package agent defines the Agent domain protocol built on Workflow execution.
package agent

import "github.com/gopact-ai/gopact"

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

// Response is the final provider-neutral output shared by public ADK Agents.
type Response struct {
	Message   gopact.Message
	Artifacts []gopact.ArtifactRef
	Metadata  map[string]string
}

// Chunk is one user-visible streaming Agent output item.
type Chunk struct {
	Text  string
	Parts []gopact.MessagePart
}

// Agent is the minimal public ADK Agent protocol.
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
