package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

var errSettleSatisfied = errors.New("workflow: source set satisfied")

// BranchPhase describes a fan-out branch's settle state.
type BranchPhase string

const (
	BranchRunning    BranchPhase = "running"
	BranchCompleted  BranchPhase = "completed"
	BranchFailed     BranchPhase = "failed"
	BranchSkipped    BranchPhase = "skipped"
	BranchCanceled   BranchPhase = "canceled"
	BranchSuperseded BranchPhase = "superseded"
)

type branchPhase = BranchPhase

const (
	branchCompleted  = BranchCompleted
	branchFailed     = BranchFailed
	branchSkipped    = BranchSkipped
	branchCanceled   = BranchCanceled
	branchSuperseded = BranchSuperseded
)

type branchOutcome struct {
	activation activation
	result     nodeRunResult
	cause      error
	phase      branchPhase
	completion int64
	committed  bool
}

type sourceSet struct {
	id          string
	policy      SettlePolicy
	expected    int
	branches    []string
	outcomes    map[string]branchOutcome
	selected    []string
	decided     bool
	settled     bool
	failed      bool
	openSources int
	cause       error
}

type settleDecision struct {
	selected []string
	failed   bool
	ready    bool
}

func (c *compiled[I, O]) prepareSourceSet(state *runState, source activation, dispatch Dispatch) (*sourceSet, error) {
	count, iterCount := c.activationDeliveryCount(dispatch)
	policy := dispatch.settle.normalized()
	if count == 0 && iterCount == 0 {
		return nil, nil
	}
	if iterCount == 0 {
		if _, err := policy.required(count); err != nil {
			return nil, err
		}
	}
	set := state.newSourceSet(source, policy, count)
	set.openSources = iterCount
	return set, nil
}

func (c *compiled[I, O]) activationDeliveryCount(dispatch Dispatch) (int, int) {
	count := 0
	iterCount := 0
	for _, item := range dispatch.deliveries {
		if item.iter != nil {
			iterCount++
			continue
		}
		if c.materializesActivation(item) {
			count++
		}
	}
	return count, iterCount
}

func (c *compiled[I, O]) materializesActivation(item delivery) bool {
	return !c.isJoinTarget(item.target)
}

func (state *runState) newSourceSet(source activation, policy SettlePolicy, expected int) *sourceSet {
	if state.sourceSets == nil {
		state.sourceSets = map[string]*sourceSet{}
	}
	id := fmt.Sprintf("set-%d-%s", state.nextSetSeq, source.id)
	state.nextSetSeq++
	set := &sourceSet{id: id, policy: policy, expected: expected, outcomes: map[string]branchOutcome{}}
	state.sourceSets[id] = set
	return set
}

func (set *sourceSet) addExpectedBranch() {
	set.expected++
}

func (set *sourceSet) closeSource() error {
	if set.openSources <= 0 {
		return errors.New("workflow: source set has no open iterator")
	}
	set.openSources--
	return nil
}

func (set *sourceSet) closeAllSources() {
	set.openSources = 0
}

func (set *sourceSet) successCount() int {
	count := 0
	for _, outcome := range set.outcomes {
		if outcome.phase == branchCompleted {
			count++
		}
	}
	return count
}

func (execution *workflowExecution[I, O]) commitBranch(item batchActivation) error {
	set := execution.state.sourceSets[item.activation.sourceSet]
	if set == nil {
		return fmt.Errorf("workflow: source set %q is missing", item.activation.sourceSet)
	}
	if err := execution.state.removeReady(item.activation.id); err != nil {
		return err
	}
	execution.state.completed[item.activation.node]++
	if err := execution.state.finishActivation(item.activation.id, item.result, item.err); err != nil {
		return err
	}
	outcome := branchOutcome{
		activation: item.activation,
		result:     item.result,
		cause:      item.err,
		phase:      branchOutcomePhase(item),
		completion: item.completion,
	}
	set.outcomes[item.activation.id] = outcome
	event, err := execution.state.nodeEvent(item.activation.id, outcome.eventType(), BranchPhase(outcome.phase))
	if err != nil {
		return err
	}
	err = execution.commitRunningEvent(event, execution.step+1)
	return preserveExecutionError(err)
}

func branchOutcomePhase(item batchActivation) branchPhase {
	if item.err != nil {
		if errors.Is(item.err, context.Canceled) || errors.Is(item.err, context.DeadlineExceeded) {
			return branchCanceled
		}
		return branchFailed
	}
	if item.result.skipped {
		return branchSkipped
	}
	return branchCompleted
}

func (outcome branchOutcome) eventType() string {
	switch outcome.phase {
	case branchCompleted:
		return EventNodeCompleted
	case branchSkipped:
		return EventNodeSkipped
	case branchCanceled:
		return EventNodeCanceled
	default:
		return EventNodeFailed
	}
}

func (execution *workflowExecution[I, O]) settleSourceSets(ids []string) error {
	for _, id := range ids {
		if err := execution.settleSourceSet(id); err != nil {
			return err
		}
	}
	return nil
}

func (execution *workflowExecution[I, O]) reconcileSourceSets() error {
	ids := make([]string, 0, len(execution.state.sourceSets))
	for id, set := range execution.state.sourceSets {
		if !set.settled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return execution.settleSourceSets(ids)
}

func (execution *workflowExecution[I, O]) settleSourceSet(id string) error {
	set := execution.state.sourceSets[id]
	if set == nil || set.settled {
		return nil
	}
	if set.failed {
		return execution.failSourceSet(set)
	}
	if set.decided {
		return execution.releaseSourceSet(set, set.selected)
	}
	decision, err := set.decide()
	if err != nil {
		return err
	}
	if !decision.ready {
		return nil
	}
	if decision.failed {
		return execution.failSourceSet(set)
	}
	return execution.releaseSourceSet(set, decision.selected)
}

func (set *sourceSet) decide() (settleDecision, error) {
	successes := set.successes()
	if set.openSources > 0 {
		return set.decideOpen(successes)
	}
	required, err := set.policy.required(set.expected)
	if err != nil {
		return settleDecision{}, err
	}
	terminal := len(set.outcomes)
	if set.policy.normalized().mode == "all" {
		return set.decideAll(successes, terminal), nil
	}
	if len(successes) >= required {
		return settleDecision{selected: successes[:required], ready: true}, nil
	}
	possible := len(successes) + set.expected - terminal
	if possible < required {
		return settleDecision{failed: true, ready: true}, nil
	}
	return settleDecision{}, nil
}

func (set *sourceSet) decideOpen(successes []string) (settleDecision, error) {
	if set.policy.normalized().mode == "all" {
		failed := set.hasFailure()
		return settleDecision{failed: failed, ready: failed}, nil
	}
	required, err := set.policy.threshold()
	if err != nil {
		return settleDecision{}, err
	}
	if len(successes) >= required {
		return settleDecision{selected: successes[:required], ready: true}, nil
	}
	return settleDecision{}, nil
}

func (set *sourceSet) decideAll(successes []string, terminal int) settleDecision {
	if set.hasFailure() {
		return settleDecision{failed: true, ready: true}
	}
	if terminal < set.expected {
		return settleDecision{}
	}
	return settleDecision{selected: successes, ready: true}
}

func (set *sourceSet) hasFailure() bool {
	for _, outcome := range set.outcomes {
		if outcome.phase == branchFailed || outcome.phase == branchCanceled {
			return true
		}
	}
	return false
}

func (set *sourceSet) successes() []string {
	if set.policy.normalized().mode == "all" {
		return set.successesInBranchOrder()
	}
	values := make([]branchOutcome, 0, len(set.outcomes))
	for _, outcome := range set.outcomes {
		if outcome.phase == branchCompleted {
			values = append(values, outcome)
		}
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].completion == values[right].completion {
			return values[left].activation.branchIndex < values[right].activation.branchIndex
		}
		return values[left].completion < values[right].completion
	})
	selected := make([]string, 0, len(values))
	for _, outcome := range values {
		selected = append(selected, outcome.activation.id)
	}
	return selected
}

func (set *sourceSet) successesInBranchOrder() []string {
	selected := make([]string, 0, len(set.branches))
	for _, id := range set.branches {
		if set.outcomes[id].phase == branchCompleted {
			selected = append(selected, id)
		}
	}
	return selected
}

func (execution *workflowExecution[I, O]) releaseSourceSet(set *sourceSet, selected []string) error {
	execution.closeIterSources(set.id)
	if !set.decided {
		set.decided = true
		set.selected = append([]string(nil), selected...)
	}
	selectedSet := make(map[string]struct{}, len(set.selected))
	for _, id := range set.selected {
		selectedSet[id] = struct{}{}
	}
	if err := execution.cancelQueuedBranches(set, selectedSet); err != nil {
		return err
	}
	if err := execution.supersedeBranches(set, selectedSet); err != nil {
		return err
	}
	if err := execution.closeUnselectedBranchExpectations(set, selectedSet); err != nil {
		return err
	}
	for _, id := range set.selected {
		if err := execution.releaseBranch(set, id); err != nil {
			return err
		}
	}
	if err := execution.compiled.materializeReadyJoins(execution.ctx, &execution.state); err != nil {
		return err
	}
	set.settled = true
	return nil
}

func (execution *workflowExecution[I, O]) cancelQueuedBranches(set *sourceSet, selected map[string]struct{}) error {
	for {
		outcome, ok, err := execution.state.cancelNextQueuedBranch(set, selected)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		event, err := execution.state.nodeEvent(outcome.activation.id, outcome.eventType(), BranchPhase(outcome.phase))
		if err != nil {
			return err
		}
		if err := execution.commitRunningEvent(event, execution.step); err != nil {
			return preserveExecutionError(err)
		}
	}
}

func (state *runState) cancelNextQueuedBranch(set *sourceSet, selected map[string]struct{}) (branchOutcome, bool, error) {
	for index, item := range state.queue {
		_, keep := selected[item.id]
		if item.sourceSet != set.id || keep {
			continue
		}
		copy(state.queue[index:], state.queue[index+1:])
		state.queue[len(state.queue)-1] = activation{}
		state.queue = state.queue[:len(state.queue)-1]
		state.nextCompletion++
		outcome := branchOutcome{activation: item, phase: branchCanceled, cause: context.Canceled, completion: state.nextCompletion}
		if err := state.cancelActivation(item.id, context.Canceled); err != nil {
			return branchOutcome{}, false, err
		}
		set.outcomes[item.id] = outcome
		state.completed[item.node]++
		return outcome, true, nil
	}
	return branchOutcome{}, false, nil
}

func (execution *workflowExecution[I, O]) supersedeBranches(set *sourceSet, selected map[string]struct{}) error {
	for _, id := range set.branches {
		outcome := set.outcomes[id]
		if !outcome.completedButUnselected(selected) {
			continue
		}
		outcome.phase = branchSuperseded
		set.outcomes[id] = outcome
		event, err := execution.state.nodeEvent(id, EventNodeSuperseded, BranchSuperseded)
		if err != nil {
			return err
		}
		if err := execution.commitRunningEvent(event, execution.step); err != nil {
			return preserveExecutionError(err)
		}
	}
	return nil
}

func (outcome branchOutcome) completedButUnselected(selected map[string]struct{}) bool {
	if outcome.phase != branchCompleted {
		return false
	}
	_, ok := selected[outcome.activation.id]
	return !ok
}

func (execution *workflowExecution[I, O]) closeUnselectedBranchExpectations(set *sourceSet, selected map[string]struct{}) error {
	for _, id := range set.branches {
		if _, ok := selected[id]; ok {
			continue
		}
		outcome, ok := set.outcomes[id]
		if !ok {
			return fmt.Errorf("workflow: branch outcome %q is missing", id)
		}
		if err := execution.compiled.closeJoinExpectations(&execution.state, outcome.activation, Dispatch{}); err != nil {
			return err
		}
	}
	return nil
}

func (execution *workflowExecution[I, O]) releaseBranch(set *sourceSet, id string) error {
	outcome := set.outcomes[id]
	if outcome.committed {
		return nil
	}
	node := execution.compiled.nodes[outcome.activation.node]
	if err := execution.route(node, outcome.activation, outcome.result.output, false); err != nil {
		return err
	}
	if err := execution.collectOutput(outcome.activation.node, outcome.result.output); err != nil {
		return err
	}
	outcome.committed = true
	set.outcomes[id] = outcome
	event, err := execution.state.nodeEvent(id, EventNodeOutputCommitted, BranchCompleted)
	if err != nil {
		return err
	}
	err = execution.commitRunningEvent(event, execution.step)
	if err != nil {
		return preserveExecutionError(err)
	}
	return execution.flushOutputs()
}

func (execution *workflowExecution[I, O]) failSourceSet(set *sourceSet) error {
	set.failed = true
	execution.closeIterSources(set.id)
	if err := execution.cancelQueuedBranches(set, nil); err != nil {
		return err
	}
	if err := execution.supersedeBranches(set, nil); err != nil {
		return err
	}
	if err := execution.closeUnselectedBranchExpectations(set, nil); err != nil {
		return err
	}
	cause := set.firstFailure()
	if cause == nil {
		cause = errors.New("no branch satisfied settle policy")
	}
	return fmt.Errorf("workflow: source set %q failed: %w", set.id, cause)
}

func (set *sourceSet) firstFailure() error {
	if set.cause != nil {
		return set.cause
	}
	var canceled error
	for _, id := range set.branches {
		cause := set.outcomes[id].cause
		if cause == nil {
			continue
		}
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			canceled = firstNonNil(canceled, cause)
			continue
		}
		return cause
	}
	return canceled
}

func firstNonNil(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
