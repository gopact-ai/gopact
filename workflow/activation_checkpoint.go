package workflow

import (
	"errors"
	"sort"
)

type checkpointActivationState struct {
	Activation           checkpointActivation
	Phase                activationPhase
	Attempt              int
	NodeExecutionVersion int64
	Origin               string
	ContextFact          NodeValue
	ContextRevision      int64
	EffectiveInput       checkpointValue
	HasInput             bool
	Result               checkpointValue
	HasResult            bool
	Skipped              bool
	Cause                string
}

func (state runState) checkpointActivations() []checkpointActivationState {
	ids := make([]string, 0, len(state.activations))
	for id := range state.activations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	records := make([]checkpointActivationState, 0, len(ids))
	for _, id := range ids {
		records = append(records, state.activations[id].checkpoint())
	}
	return records
}

func (record *activationRecord) checkpoint() checkpointActivationState {
	cause := ""
	if record.cause != nil {
		cause = record.cause.Error()
	}
	var input, result checkpointValue
	if record.result.hasInput {
		input = newCheckpointValue(record.result.input)
	}
	if record.hasResult {
		result = newCheckpointValue(record.result.output)
	}
	return checkpointActivationState{
		Activation: record.activation.checkpoint(), Phase: record.phase, Attempt: record.attempt,
		NodeExecutionVersion: record.nodeExecutionVersion, Origin: record.origin,
		ContextFact: record.contextFact, ContextRevision: record.contextRevision,
		EffectiveInput: input, HasInput: record.result.hasInput, Result: result, HasResult: record.hasResult,
		Skipped: record.result.skipped, Cause: cause,
	}
}

func (state *runState) restoreActivations(records []checkpointActivationState) {
	if state.nodeVersions == nil {
		state.nodeVersions = map[string]int64{}
	}
	for _, encoded := range records {
		record := encoded.runtime()
		state.activations[record.activation.id] = record
		state.nodeVersions[record.activation.node] = max(
			state.nodeVersions[record.activation.node],
			record.nodeExecutionVersion,
		)
	}
}

func (encoded checkpointActivationState) runtime() *activationRecord {
	var cause error
	if encoded.Cause != "" {
		cause = errors.New(encoded.Cause)
	}
	origin := encoded.Origin
	if origin == "" {
		origin = "natural"
	}
	return &activationRecord{
		activation: encoded.Activation.runtime(), phase: encoded.Phase, attempt: encoded.Attempt,
		nodeExecutionVersion: encoded.NodeExecutionVersion, origin: origin,
		contextFact: encoded.ContextFact, contextRevision: encoded.ContextRevision,
		result: nodeRunResult{
			input: encoded.EffectiveInput.runtime(), hasInput: encoded.HasInput,
			output: encoded.Result.runtime(), skipped: encoded.Skipped,
		},
		hasResult: encoded.HasResult, cause: cause,
	}
}
