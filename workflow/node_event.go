package workflow

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// NodeValue is a complete node fact value encoded for history consumers.
type NodeValue struct {
	Type string          `json:"type"`
	JSON json.RawMessage `json:"json,omitempty"`
	Gob  []byte          `json:"gob,omitempty"`
}

// NodeEventPayload identifies the activation behind a workflow node event.
type NodeEventPayload struct {
	NodeName             string          `json:"node_name"`
	ActivationID         string          `json:"activation_id"`
	Attempt              int             `json:"attempt"`
	NodeExecutionVersion int64           `json:"node_execution_version,omitempty"`
	ActivationPhase      ActivationPhase `json:"activation_phase"`
	SourceSetID          string          `json:"source_set_id,omitempty"`
	BranchIndex          int             `json:"branch_index,omitempty"`
	BranchPhase          BranchPhase     `json:"branch_phase,omitempty"`
	Correlation          CorrelationKey  `json:"correlation"`
	Input                NodeValue       `json:"input"`
	EffectiveInput       *NodeValue      `json:"effective_input,omitempty"`
	WorkflowContext      NodeValue       `json:"workflow_context"`
	ContextRevision      int64           `json:"context_revision"`
	Output               *NodeValue      `json:"output,omitempty"`
	Error                string          `json:"error,omitempty"`
}

func (state *runState) nodeEvent(id, eventType string, branch BranchPhase, runResult ...nodeRunResult) (gopact.Event, error) {
	record, err := state.activationRecord(id)
	if err != nil {
		return gopact.Event{}, err
	}
	result := record.result
	if len(runResult) > 0 {
		result = runResult[0]
	}
	return record.nodeEvent(eventType, branch, result)
}

func (record *activationRecord) nodeEvent(eventType string, branch BranchPhase, result nodeRunResult) (gopact.Event, error) {
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
	input, err := snapshotNodeValue(record.activation.input)
	if err != nil {
		return gopact.Event{}, err
	}
	facts := NodeEventPayload{
		NodeName: record.activation.node, ActivationID: record.activation.id, Attempt: record.attempt,
		NodeExecutionVersion: record.nodeExecutionVersion,
		ActivationPhase:      record.phase, SourceSetID: record.activation.sourceSet,
		BranchIndex: record.activation.branchIndex, BranchPhase: branch,
		Correlation: record.activation.correlation, Input: input,
		WorkflowContext: cloneNodeValue(record.contextFact), ContextRevision: record.contextRevision,
	}
	if record.cause != nil {
		facts.Error = record.cause.Error()
	}
	if result.hasInput && eventType != EventNodeStarted {
		effectiveInput, err := snapshotNodeValue(result.input)
		if err != nil {
			return gopact.Event{}, err
		}
		facts.EffectiveInput = &effectiveInput
	}
	if record.hasResult && nodeEventIncludesOutput(eventType) {
		output, err := snapshotNodeValue(result.output)
		if err != nil {
			return gopact.Event{}, err
		}
		facts.Output = &output
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

func cloneNodeValue(value NodeValue) NodeValue {
	value.JSON = append(json.RawMessage(nil), value.JSON...)
	value.Gob = append([]byte(nil), value.Gob...)
	return value
}

func nodeEventIncludesOutput(eventType string) bool {
	return eventType == EventNodeCompleted || eventType == EventNodeOutputCommitted || eventType == EventNodeSuperseded
}

func snapshotNodeValue(value any) (NodeValue, error) {
	typeName := fmt.Sprintf("%T", value)
	jsonValue := value
	if inputs, ok := value.(Inputs); ok {
		jsonValue = copyContributions(inputs.contributions)
	}
	if data, err := json.Marshal(jsonValue); err == nil {
		return NodeValue{Type: typeName, JSON: data}, nil
	}
	checkpoint := newCheckpointValue(value)
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(checkpoint); err != nil {
		return NodeValue{}, fmt.Errorf("workflow: encode node fact %s: %w", typeName, err)
	}
	return NodeValue{Type: typeName, Gob: buffer.Bytes()}, nil
}
