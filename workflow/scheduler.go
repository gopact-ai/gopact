package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

type batchActivation struct {
	index      int
	step       int
	activation activation
	node       runtimeNode
	result     nodeRunResult
	err        error
	completion int64
}

func (execution *workflowExecution[I, O]) advanceBatch() error {
	batch, err := execution.nextBatch()
	if err != nil {
		return err
	}
	if err := execution.emitBatchStarts(batch); err != nil {
		return preservedExecutionError{cause: err}
	}
	results := execution.executeBatch(batch)
	if err := execution.eventError(); err != nil {
		return preservedExecutionError{cause: err}
	}
	interrupts := batchInterrupts(results)
	if len(interrupts) > 0 {
		return execution.handleBatchInterrupts(results, interrupts)
	}
	if failed, ok := failedStandaloneActivation(results); ok {
		return execution.handleNodeError(failed.activation, failed.result, failed.err)
	}
	var sourceSets []string
	for _, item := range results {
		sourceSetID, err := execution.commitBatchItem(item)
		if err != nil {
			return err
		}
		if sourceSetID != "" {
			sourceSets = appendStringOnce(sourceSets, sourceSetID)
		}
		execution.step++
	}
	return execution.settleSourceSets(sourceSets)
}

func batchInterrupts(results []batchActivation) []batchActivation {
	interrupts := make([]batchActivation, 0, len(results))
	for _, item := range results {
		var interrupt InterruptError
		if errors.As(item.err, &interrupt) {
			interrupts = append(interrupts, item)
		}
	}
	return interrupts
}

func (execution *workflowExecution[I, O]) handleBatchInterrupts(results, interrupts []batchActivation) error {
	interrupted := make(map[string]struct{}, len(interrupts))
	for _, item := range interrupts {
		interrupted[item.activation.id] = struct{}{}
	}
	if err := execution.reorderInterruptedBatch(results, interrupted); err != nil {
		return err
	}
	var sourceSets []string
	for _, item := range results {
		if _, ok := interrupted[item.activation.id]; ok {
			continue
		}
		if item.activation.sourceSet == "" && item.err != nil {
			return execution.handleNodeError(item.activation, item.result, item.err)
		}
		sourceSetID, err := execution.commitBatchItem(item)
		if err != nil {
			return err
		}
		if sourceSetID != "" {
			sourceSets = appendStringOnce(sourceSets, sourceSetID)
		}
		execution.step++
	}
	if err := execution.settleSourceSets(sourceSets); err != nil {
		return err
	}
	return execution.interruptBatch(interrupts)
}

func (execution *workflowExecution[I, O]) reorderInterruptedBatch(results []batchActivation, interrupted map[string]struct{}) error {
	if len(execution.state.queue) < len(results) {
		return errors.New("workflow: scheduler batch exceeds ready queue")
	}
	next := make([]activation, 0, len(execution.state.queue))
	for _, item := range results {
		if _, ok := interrupted[item.activation.id]; !ok {
			next = append(next, item.activation)
		}
	}
	for _, item := range results {
		if _, ok := interrupted[item.activation.id]; ok {
			next = append(next, item.activation)
		}
	}
	next = append(next, execution.state.queue[len(results):]...)
	execution.state.queue = next
	return nil
}

func (execution *workflowExecution[I, O]) commitBatchItem(item batchActivation) (string, error) {
	if item.err == nil && item.result.retry {
		return "", execution.commitRetry(item.activation, item.result)
	}
	if item.err == nil && item.result.contextChanged {
		if item.activation.sourceSet != "" && !execution.state.singleBranchSourceSet(item.activation.sourceSet) {
			return "", errors.New("workflow: fan-out branch cannot update workflow context; use a merge node")
		}
		if err := execution.state.commitContext(item.result); err != nil {
			return "", err
		}
	}
	if item.activation.sourceSet != "" {
		return item.activation.sourceSet, execution.commitBranch(item)
	}
	return "", execution.commitActivation(item.activation, item.result)
}

func (state runState) singleBranchSourceSet(id string) bool {
	set := state.sourceSets[id]
	return set != nil && set.expected == 1 && len(set.branches) == 1 && set.openSources == 0
}

func (execution *workflowExecution[I, O]) nextBatch() ([]batchActivation, error) {
	remaining := execution.compiled.maxSteps - execution.step + 1
	if remaining <= 0 {
		return nil, fmt.Errorf("workflow: exceeded max steps %d", execution.compiled.maxSteps)
	}
	size := min(execution.compiled.maxParallelism, len(execution.state.queue), remaining)
	batch := make([]batchActivation, size)
	for index, current := range execution.state.queue[:size] {
		batch[index] = batchActivation{index: index, step: execution.step + index, activation: current, node: execution.compiled.nodes[current.node]}
	}
	return batch, nil
}

func (execution *workflowExecution[I, O]) emitBatchStarts(batch []batchActivation) error {
	for _, item := range batch {
		if err := execution.state.startActivation(item.activation.id); err != nil {
			return err
		}
		event, err := execution.state.nodeEvent(item.activation.id, EventNodeStarted, "")
		if err != nil {
			return err
		}
		if err := execution.commitRunningEvent(event, execution.step); err != nil {
			return err
		}
	}
	return nil
}

func (execution *workflowExecution[I, O]) executeBatch(batch []batchActivation) []batchActivation {
	coordinator := newBatchCoordinator(execution.ctx, execution.state, batch)
	defer coordinator.close()
	results := make(chan batchActivation, len(batch))
	var completion atomic.Int64
	completion.Store(execution.state.nextCompletion)
	var workers sync.WaitGroup
	for _, item := range batch {
		item := item
		workers.Go(func() {
			item.result, item.err = execution.runActivation(coordinator.context(item), item)
			item.completion = completion.Add(1)
			results <- item
		})
	}
	ordered := make([]batchActivation, len(batch))
	for range batch {
		item := <-results
		coordinator.observe(item)
		ordered[item.index] = item
	}
	workers.Wait()
	close(results)
	execution.state.nextCompletion = completion.Load()
	return ordered
}

func (execution *workflowExecution[I, O]) runActivation(ctx context.Context, item batchActivation) (result nodeRunResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("workflow: node %q panic: %v", item.activation.node, recovered)
		}
	}()
	nodeCtx, txn := execution.nodeContext(ctx, item.activation, item.step)
	result, err = item.node.runAny(nodeCtx, item.activation.input, execution.compiled.nodeMiddlewares)
	if err == nil && txn != nil && txn.changed {
		result.contextValue = txn.value
		result.contextChanged = true
		result.contextBaseRevision = txn.revision
	}
	return result, err
}

func failedStandaloneActivation(results []batchActivation) (batchActivation, bool) {
	var canceled *batchActivation
	for index := range results {
		if results[index].err == nil || results[index].activation.sourceSet != "" {
			continue
		}
		if captureCanceled(&canceled, &results[index]) {
			continue
		}
		return results[index], true
	}
	if canceled != nil {
		return *canceled, true
	}
	return batchActivation{}, false
}

func captureCanceled(current **batchActivation, candidate *batchActivation) bool {
	if !errors.Is(candidate.err, context.Canceled) {
		return false
	}
	if *current == nil {
		*current = candidate
	}
	return true
}

type batchGroup struct {
	ctx       context.Context
	cancel    context.CancelCauseFunc
	policy    SettlePolicy
	required  int
	successes int
	sourceSet string
}

type batchCoordinator struct {
	groups map[string]*batchGroup
}

func newBatchCoordinator(ctx context.Context, state runState, batch []batchActivation) *batchCoordinator {
	coordinator := &batchCoordinator{groups: map[string]*batchGroup{}}
	for _, item := range batch {
		coordinator.addGroup(ctx, state, item.activation.sourceSet)
	}
	return coordinator
}

func (coordinator *batchCoordinator) addGroup(ctx context.Context, state runState, sourceSetID string) {
	if coordinator.groups[sourceSetID] != nil {
		return
	}
	groupCtx, cancel := context.WithCancelCause(ctx)
	group := &batchGroup{ctx: groupCtx, cancel: cancel, sourceSet: sourceSetID}
	if set := state.sourceSets[sourceSetID]; set != nil {
		group.policy = set.policy
		group.required, _ = set.policy.threshold()
		group.successes = set.successCount()
	}
	coordinator.groups[sourceSetID] = group
}

func (coordinator *batchCoordinator) context(item batchActivation) context.Context {
	return coordinator.groups[item.activation.sourceSet].ctx
}

func (coordinator *batchCoordinator) observe(item batchActivation) {
	group := coordinator.groups[item.activation.sourceSet]
	if item.err != nil {
		coordinator.observeError(group, item.err)
		return
	}
	if item.result.skipped || item.result.retry || group.sourceSet == "" {
		return
	}
	group.successes++
	if group.policy.normalized().mode != "all" && group.successes >= group.required {
		group.cancel(errSettleSatisfied)
	}
}

func (coordinator *batchCoordinator) observeError(group *batchGroup, err error) {
	var interrupt InterruptError
	if errors.As(err, &interrupt) {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if group.sourceSet == "" {
		coordinator.cancelAll(err)
		return
	}
	if group.policy.normalized().mode == "all" {
		group.cancel(err)
	}
}

func (coordinator *batchCoordinator) cancelAll(cause error) {
	for _, group := range coordinator.groups {
		group.cancel(cause)
	}
}

func (coordinator *batchCoordinator) close() {
	coordinator.cancelAll(nil)
}
