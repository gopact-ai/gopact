package workflow

import (
	"errors"
	"sort"
)

type checkpointSourceSet struct {
	ID           string
	PolicyMode   string
	PolicyQuorum int
	Expected     int
	Branches     []string
	Outcomes     []checkpointBranchOutcome
	Selected     []string
	Decided      bool
	Settled      bool
	Failed       bool
	OpenSources  int
	Cause        string
}

type checkpointBranchOutcome struct {
	Activation checkpointActivation
	Output     checkpointValue
	Skipped    bool
	Cause      string
	Phase      branchPhase
	Completion int64
	Committed  bool
}

type checkpointValue struct {
	Value     any
	Inputs    map[string][]any
	HasInputs bool
}

func (item activation) checkpoint() checkpointActivation {
	value := newCheckpointValue(item.input)
	return checkpointActivation{
		ID: item.id, Node: item.node, Input: value.Value, SourceSet: item.sourceSet, BranchIndex: item.branchIndex,
		Correlation: item.correlation, JoinInput: value.Inputs, HasJoinInput: value.HasInputs,
	}
}

func (item checkpointActivation) runtime() activation {
	value := checkpointValue{Value: item.Input, Inputs: item.JoinInput, HasInputs: item.HasJoinInput}
	return activation{
		id: item.ID, node: item.Node, input: value.runtime(), sourceSet: item.SourceSet,
		branchIndex: item.BranchIndex, correlation: item.Correlation,
	}
}

func newCheckpointValue(value any) checkpointValue {
	inputs, ok := value.(Inputs)
	if !ok {
		return checkpointValue{Value: value}
	}
	return checkpointValue{Inputs: copyContributions(inputs.contributions), HasInputs: true}
}

func (value checkpointValue) runtime() any {
	if value.HasInputs {
		return Inputs{contributions: copyContributions(value.Inputs)}
	}
	return value.Value
}

func (state runState) checkpointSourceSets() []checkpointSourceSet {
	ids := make([]string, 0, len(state.sourceSets))
	for id := range state.sourceSets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sets := make([]checkpointSourceSet, 0, len(ids))
	for _, id := range ids {
		sets = append(sets, state.sourceSets[id].checkpoint())
	}
	return sets
}

func (set *sourceSet) checkpoint() checkpointSourceSet {
	ids := make([]string, 0, len(set.outcomes))
	for id := range set.outcomes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	outcomes := make([]checkpointBranchOutcome, 0, len(ids))
	for _, id := range ids {
		outcomes = append(outcomes, set.outcomes[id].checkpoint())
	}
	cause := ""
	if set.cause != nil {
		cause = set.cause.Error()
	}
	return checkpointSourceSet{
		ID: set.id, PolicyMode: set.policy.mode, PolicyQuorum: set.policy.quorum, Expected: set.expected,
		Branches: append([]string(nil), set.branches...), Outcomes: outcomes, Selected: append([]string(nil), set.selected...),
		Decided: set.decided, Settled: set.settled, Failed: set.failed, OpenSources: set.openSources, Cause: cause,
	}
}

func (outcome branchOutcome) checkpoint() checkpointBranchOutcome {
	cause := ""
	if outcome.cause != nil {
		cause = outcome.cause.Error()
	}
	return checkpointBranchOutcome{
		Activation: outcome.activation.checkpoint(), Output: newCheckpointValue(outcome.result.output), Skipped: outcome.result.skipped,
		Cause: cause, Phase: outcome.phase, Completion: outcome.completion, Committed: outcome.committed,
	}
}

func (state *runState) restoreSourceSets(items []checkpointSourceSet) {
	for _, item := range items {
		set := item.runtime()
		state.sourceSets[set.id] = set
		state.restoreBranchActivations(set)
	}
	if state.nextSetSeq <= 0 {
		state.nextSetSeq = 1
	}
}

func (state *runState) restoreBranchActivations(set *sourceSet) {
	for id, outcome := range set.outcomes {
		if state.activations[id] != nil {
			continue
		}
		state.activations[id] = &activationRecord{
			activation: outcome.activation, phase: activationPhaseForBranch(outcome.phase), attempt: 1,
			result: outcome.result, hasResult: outcome.cause == nil, cause: outcome.cause,
		}
	}
}

func activationPhaseForBranch(phase branchPhase) activationPhase {
	switch phase {
	case branchCompleted, branchSuperseded:
		return activationCompleted
	case branchFailed:
		return activationFailed
	case branchSkipped:
		return activationSkipped
	case branchCanceled:
		return activationCanceled
	default:
		return activationReady
	}
}

func (set checkpointSourceSet) runtime() *sourceSet {
	var cause error
	if set.Cause != "" {
		cause = errors.New(set.Cause)
	}
	restored := &sourceSet{
		id: set.ID, policy: SettlePolicy{mode: set.PolicyMode, quorum: set.PolicyQuorum}, expected: set.Expected,
		branches: append([]string(nil), set.Branches...), outcomes: map[string]branchOutcome{},
		selected: append([]string(nil), set.Selected...), decided: set.Decided || len(set.Selected) > 0,
		settled: set.Settled, failed: set.Failed, openSources: set.OpenSources, cause: cause,
	}
	for _, encoded := range set.Outcomes {
		outcome := encoded.runtime()
		restored.outcomes[outcome.activation.id] = outcome
	}
	return restored
}

func (item checkpointBranchOutcome) runtime() branchOutcome {
	var cause error
	if item.Cause != "" {
		cause = errors.New(item.Cause)
	}
	return branchOutcome{
		activation: item.Activation.runtime(), result: nodeRunResult{output: item.Output.runtime(), skipped: item.Skipped},
		cause: cause, phase: item.Phase, completion: item.Completion, committed: item.Committed,
	}
}
