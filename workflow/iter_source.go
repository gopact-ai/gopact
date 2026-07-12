package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"sort"

	"github.com/gopact-ai/gopact"
)

// Iterator route event types.
const (
	EventIterItemPulled = "route.iter_item_pulled"
	EventIterClosed     = "route.iter_closed"
	EventIterFailed     = "route.iter_failed"
)

// IterEventPayload identifies one durable iterator transition.
type IterEventPayload struct {
	IteratorID       string         `json:"iterator_id"`
	SourceActivation string         `json:"source_activation_id"`
	SourceSetID      string         `json:"source_set_id"`
	Target           string         `json:"target"`
	ItemIndex        int            `json:"item_index"`
	Correlation      CorrelationKey `json:"correlation"`
}

type iterSource struct {
	id            string
	sourceID      string
	target        string
	sourceSet     string
	deliveryIndex int
	pulled        int
	cursor        any
	hasCursor     bool
	replay        bool
	open          bool
	cause         error
}

type liveIterator struct {
	next       func() (any, error, bool)
	stop       func()
	checkpoint func() any
}

type iterFactory func() iter.Seq2[any, error]

func (c *compiled[I, O]) scheduleIter(ctx context.Context, state *runState, req deliveryRequest) error {
	if c.isJoinTarget(req.item.target) {
		return fmt.Errorf("workflow: route from %q to join target %q cannot use each iter", req.source.node, req.item.target)
	}
	if req.sourceSet == "" {
		return errors.New("workflow: each iter is missing source set")
	}
	if err := validateIterReplay(req.item); err != nil {
		return err
	}
	sequence, err := callIterFactory("factory", func() iter.Seq2[any, error] { return req.item.iter(ctx) })
	if err != nil {
		return err
	}
	if state.nextIterSeq <= 0 {
		state.nextIterSeq = 1
	}
	id := fmt.Sprintf("iter-%d", state.nextIterSeq)
	state.nextIterSeq++
	if state.iterSources == nil {
		state.iterSources = map[string]*iterSource{}
	}
	source := &iterSource{
		id: id, sourceID: req.source.id, target: req.item.target, sourceSet: req.sourceSet,
		deliveryIndex: req.deliveryIndex, replay: req.item.iterRestore != nil, open: true,
	}
	state.iterSources[id] = source
	state.bindLiveIterator(source, sequence, req.item.iterCheckpoint)
	return nil
}

func validateIterReplay(item delivery) error {
	if item.iterErr != nil {
		return item.iterErr
	}
	if item.iterCheckpoint == nil && item.iterRestore == nil {
		return nil
	}
	if item.iterCheckpoint == nil || item.iterRestore == nil {
		return errors.New("workflow: iterator checkpoint and restore must be configured together")
	}
	return nil
}

func (state *runState) bindLiveIterator(source *iterSource, sequence iter.Seq2[any, error], checkpoint func() any) {
	next, stop := iter.Pull2(sequence)
	if state.liveIters == nil {
		state.liveIters = map[string]*liveIterator{}
	}
	state.liveIters[source.id] = &liveIterator{next: next, stop: stop, checkpoint: checkpoint}
}

func (state *runState) hasWork() bool {
	return len(state.queue) > 0 || state.hasOpenIterSource()
}

func (state *runState) hasOpenIterSource() bool {
	for _, source := range state.iterSources {
		if source.open {
			return true
		}
	}
	return false
}

func (state *runState) nextOpenIterSource() *iterSource {
	ids := make([]string, 0, len(state.iterSources))
	for id, source := range state.iterSources {
		if source.open {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	return state.iterSources[ids[0]]
}

func (execution *workflowExecution[I, O]) fillIterQueue() error {
	for len(execution.state.queue) < execution.compiled.maxParallelism && execution.state.hasOpenIterSource() {
		source := execution.state.nextOpenIterSource()
		if err := execution.pullIterSource(source); err != nil {
			return err
		}
	}
	return nil
}

func (execution *workflowExecution[I, O]) pullIterSource(source *iterSource) error {
	live := execution.state.liveIters[source.id]
	if live == nil {
		return fmt.Errorf("workflow: live iterator %q is missing", source.id)
	}
	value, ok, cause := live.pull()
	if !ok {
		return execution.closeIterSource(source)
	}
	if cause != nil {
		return execution.failIterSource(source, cause)
	}
	activation, set, err := execution.iterSourceState(source)
	if err != nil {
		return err
	}
	if err := validateActivationPayload(activation.node, source.target, value, execution.compiled.nodes[source.target]); err != nil {
		return execution.failIterSource(source, err)
	}
	branchIndex := set.expected
	set.addExpectedBranch()
	execution.compiled.enqueue(&execution.state, enqueueRequest{
		target: source.target, input: value, sourceSet: source.sourceSet, branchIndex: branchIndex,
		correlation: execution.compiled.nextCorrelation(activation, source.target),
	})
	source.pulled++
	if err := source.captureCursor(live.checkpoint); err != nil {
		return execution.failIterSource(source, err)
	}
	event, err := source.event(EventIterItemPulled, activation.correlation)
	if err != nil {
		return err
	}
	return preserveExecutionError(execution.commitRunningEvent(event, execution.step))
}

func (execution *workflowExecution[I, O]) iterSourceState(source *iterSource) (activation, *sourceSet, error) {
	record := execution.state.activations[source.sourceID]
	if record == nil {
		return activation{}, nil, fmt.Errorf("workflow: iterator source activation %q is missing", source.sourceID)
	}
	set := execution.state.sourceSets[source.sourceSet]
	if set == nil {
		return activation{}, nil, fmt.Errorf("workflow: iterator source set %q is missing", source.sourceSet)
	}
	return record.activation, set, nil
}

func (source *iterSource) captureCursor(checkpoint func() any) (err error) {
	if checkpoint == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("workflow: iterator checkpoint panic: %v", recovered)
		}
	}()
	source.cursor = checkpoint()
	source.hasCursor = true
	return nil
}

func (execution *workflowExecution[I, O]) closeIterSource(source *iterSource) error {
	activation, set, err := execution.iterSourceState(source)
	if err != nil {
		return err
	}
	execution.stopIterSource(source)
	if err := set.closeSource(); err != nil {
		return err
	}
	event, err := source.event(EventIterClosed, activation.correlation)
	if err != nil {
		return err
	}
	if err := execution.commitRunningEvent(event, execution.step); err != nil {
		return preserveExecutionError(err)
	}
	return execution.settleSourceSet(set.id)
}

func (execution *workflowExecution[I, O]) failIterSource(source *iterSource, cause error) error {
	activation, set, err := execution.iterSourceState(source)
	if err != nil {
		return err
	}
	source.cause = cause
	set.cause = cause
	set.failed = true
	execution.closeIterSources(set.id)
	event, err := source.event(EventIterFailed, activation.correlation)
	if err != nil {
		return err
	}
	if err := execution.commitRunningEvent(event, execution.step); err != nil {
		return preserveExecutionError(err)
	}
	return execution.failSourceSet(set)
}

func (source *iterSource) event(eventType string, correlation CorrelationKey) (gopact.Event, error) {
	payload, err := json.Marshal(IterEventPayload{
		IteratorID: source.id, SourceActivation: source.sourceID, SourceSetID: source.sourceSet,
		Target: source.target, ItemIndex: source.pulled, Correlation: correlation,
	})
	if err != nil {
		return gopact.Event{}, err
	}
	return gopact.Event{Type: eventType, Source: "workflow.route", Summary: source.target, Payload: payload}, nil
}

func (execution *workflowExecution[I, O]) stopIterSource(source *iterSource) {
	if !source.open {
		return
	}
	source.open = false
	live := execution.state.liveIters[source.id]
	if live != nil {
		stopLiveIterator(live)
		delete(execution.state.liveIters, source.id)
	}
}

func (execution *workflowExecution[I, O]) stopLiveIterators() {
	for id, live := range execution.state.liveIters {
		stopLiveIterator(live)
		delete(execution.state.liveIters, id)
	}
}

func stopLiveIterator(live *liveIterator) {
	if live == nil || live.stop == nil {
		return
	}
	defer func() { _ = recover() }()
	live.stop()
}

func (live *liveIterator) pull() (value any, ok bool, cause error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			cause = fmt.Errorf("workflow: iterator panic: %v", recovered)
			ok = true
		}
	}()
	value, cause, ok = live.next()
	return value, ok, cause
}

func (execution *workflowExecution[I, O]) closeIterSources(sourceSetID string) {
	for _, source := range execution.state.iterSources {
		if source.sourceSet == sourceSetID {
			execution.stopIterSource(source)
		}
	}
	if set := execution.state.sourceSets[sourceSetID]; set != nil {
		set.closeAllSources()
	}
}

func (execution *workflowExecution[I, O]) bindIterSources() error {
	ids := make([]string, 0, len(execution.state.iterSources))
	for id, source := range execution.state.iterSources {
		if source.open {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := execution.bindIterSource(execution.state.iterSources[id]); err != nil {
			return err
		}
	}
	return nil
}

func (execution *workflowExecution[I, O]) bindIterSource(source *iterSource) error {
	record := execution.state.activations[source.sourceID]
	if record == nil || !record.hasResult {
		return fmt.Errorf("workflow: iterator source activation %q has no reusable output", source.sourceID)
	}
	node := execution.compiled.nodes[record.activation.node]
	dispatch, err := node.routeAny(execution.ctx, record.result.output)
	if err != nil {
		return err
	}
	dispatch, err = execution.compiled.applyRouteMiddlewares(execution.ctx, node, record.activation.node, record.result.output, dispatch)
	if err != nil {
		return err
	}
	if err := dispatch.validateSource(record.activation.node); err != nil {
		return err
	}
	if source.deliveryIndex >= len(dispatch.deliveries) {
		return fmt.Errorf("workflow: iterator delivery %d is missing after restore", source.deliveryIndex)
	}
	item := dispatch.deliveries[source.deliveryIndex]
	if item.iter == nil || item.target != source.target {
		return fmt.Errorf("workflow: iterator delivery %d changed after restore", source.deliveryIndex)
	}
	if err := validateIterReplay(item); err != nil {
		return err
	}
	if source.replay != (item.iterRestore != nil) {
		return errors.New("workflow: iterator replay capability changed after restore")
	}
	sequence, err := source.restoredSequence(execution.ctx, item)
	if err != nil {
		return err
	}
	execution.state.bindLiveIterator(source, sequence, item.iterCheckpoint)
	if !source.replay && source.pulled > 0 {
		return skipIterator(execution.state.liveIters[source.id], source.pulled)
	}
	return nil
}

func (source *iterSource) restoredSequence(ctx context.Context, item delivery) (sequence iter.Seq2[any, error], err error) {
	if source.hasCursor {
		if item.iterRestore == nil {
			return nil, errors.New("workflow: iterator restore function is missing")
		}
		return callIterFactory("restore", func() iter.Seq2[any, error] { return item.iterRestore(ctx, source.cursor) })
	}
	return callIterFactory("factory", func() iter.Seq2[any, error] { return item.iter(ctx) })
}

func callIterFactory(boundary string, factory iterFactory) (sequence iter.Seq2[any, error], err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("workflow: iterator %s panic: %v", boundary, recovered)
		}
	}()
	sequence = factory()
	if sequence == nil {
		return nil, fmt.Errorf("workflow: iterator %s returned nil sequence", boundary)
	}
	return sequence, nil
}

func skipIterator(live *liveIterator, count int) error {
	for range count {
		_, ok, cause := live.pull()
		if !ok {
			return errors.New("workflow: iterator ended before restored cursor")
		}
		if cause != nil {
			return fmt.Errorf("workflow: iterator failed before restored cursor: %w", cause)
		}
	}
	return nil
}
