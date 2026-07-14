package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// NodeEventPayload identifies the activation behind a workflow node event.
// It does not contain business values or raw error messages.
type NodeEventPayload struct {
	NodeName             string          `json:"node_name"`
	ActivationID         string          `json:"activation_id"`
	Attempt              int             `json:"attempt"`
	NodeExecutionVersion int64           `json:"node_execution_version"`
	ActivationPhase      ActivationPhase `json:"activation_phase"`
	SourceSetID          string          `json:"source_set_id,omitempty"`
	BranchIndex          int             `json:"branch_index"`
	BranchPhase          BranchPhase     `json:"branch_phase,omitempty"`
	Correlation          CorrelationKey  `json:"correlation"`
	ContextRevision      int64           `json:"context_revision"`
	Status               string          `json:"status"`
	// Error is empty or a non-sensitive failure classification.
	Error string `json:"error"`
}

func (state *runState) nodeEvent(id, eventType string, branch BranchPhase) (gopact.Event, error) {
	record, err := state.activationRecord(id)
	if err != nil {
		return gopact.Event{}, err
	}
	return record.nodeEvent(eventType, branch)
}

func (record *activationRecord) nodeEvent(eventType string, branch BranchPhase) (gopact.Event, error) {
	if branch == "" && record.activation.sourceSet != "" {
		branch = BranchRunning
	}
	attemptID := ""
	if record.attempt > 0 {
		attemptID = fmt.Sprintf("%s/attempt-%d", record.activation.id, record.attempt)
	}
	origin := record.origin
	if origin == "" {
		origin = "natural"
	}
	facts := NodeEventPayload{
		NodeName: record.activation.node, ActivationID: record.activation.id, Attempt: record.attempt,
		NodeExecutionVersion: record.nodeExecutionVersion,
		ActivationPhase:      record.phase, SourceSetID: record.activation.sourceSet,
		BranchIndex: record.activation.branchIndex, BranchPhase: branch,
		Correlation: record.activation.correlation, ContextRevision: record.contextRevision,
		Status: nodeEventStatus(eventType, record.phase), Error: nodeErrorClass(record.cause),
	}
	payload, err := json.Marshal(facts)
	if err != nil {
		return gopact.Event{}, err
	}
	return gopact.Event{
		Type: eventType, Source: "workflow.node", Origin: origin, Summary: record.activation.node,
		NodeID: record.activation.node, ActivationID: record.activation.id, AttemptID: attemptID,
		NodeExecutionVersion: record.nodeExecutionVersion, Payload: payload,
	}, nil
}

func nodeEventStatus(eventType string, phase activationPhase) string {
	switch eventType {
	case EventNodeRetrying:
		return "retrying"
	case EventNodeSuperseded:
		return "superseded"
	default:
		return string(phase)
	}
}

func nodeErrorClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case err != nil:
		return "failed"
	default:
		return ""
	}
}
