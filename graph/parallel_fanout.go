package graph

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

// FanOutResult is one completed node result passed to a parallel fan-out merge function.
type FanOutResult[S any] struct {
	Node    string
	Step    int
	Input   S
	Output  S
	Effects []gopact.EffectRecord
}

// FanOutMergeFunc merges parallel fan-out node outputs into the next graph state.
type FanOutMergeFunc[S any] func(ctx context.Context, base S, results []FanOutResult[S]) (S, error)

// WithParallelFanOut enables concurrent execution for ready fan-out targets.
//
// The merge function is required because each parallel target receives the same
// base state. Results are passed to merge in stable graph target order.
func WithParallelFanOut[S any](merge FanOutMergeFunc[S]) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.fanOutMerge = merge
	}
}

type parallelFanOutResult[S any] struct {
	node      string
	step      int
	input     S
	output    S
	err       error
	effects   []gopact.EffectRecord
	startedAt time.Time
	endedAt   time.Time
	nested    []capturedGraphEvent
}

type capturedGraphEvent struct {
	event gopact.Event
	err   error
}

func (r *Runnable[S]) executeParallelFanOut(
	ctx context.Context,
	base S,
	nodes []string,
	completed map[string]struct{},
	step int,
	maxSteps int,
	cfg invokeConfig,
	resuming bool,
	merge FanOutMergeFunc[S],
	yield func(gopact.Event, error) bool,
) (S, []string, map[string]struct{}, int, error) {
	if step+len(nodes) > maxSteps {
		err := fmt.Errorf("graph: exceeded max steps %d", maxSteps)
		emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", step, nil, err), err)
		return base, nil, completed, step, err
	}

	startEvent := gopact.EventNodeStarted
	if resuming {
		startEvent = gopact.EventNodeResumed
	}
	startedAt := make([]time.Time, len(nodes))
	for i, node := range nodes {
		startedAt[i] = time.Now()
		nextStep := step + i + 1
		snapshot := stepSnapshot(nextStep, node, cfg.ids, gopact.StepRunning, base, nil, "", startedAt[i], time.Time{})
		if !emit(yield, graphEvent(startEvent, cfg.ids, node, nextStep, &snapshot, nil), nil) {
			return base, nil, completed, step, errNestedEventYieldStopped
		}
	}

	fanOutCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]parallelFanOutResult[S], len(nodes))
	var cancelOnce sync.Once
	var wg sync.WaitGroup
	wg.Add(len(nodes))
	for i, node := range nodes {
		go func(i int, node string) {
			defer wg.Done()
			result := r.runParallelFanOutNode(fanOutCtx, base, node, step+i+1, startedAt[i], cfg)
			if result.err != nil {
				cancelOnce.Do(cancel)
			}
			results[i] = result
		}(i, node)
	}
	wg.Wait()

	if failed, ok := firstParallelFanOutError(results); ok {
		for _, nested := range failed.nested {
			if !emit(yield, nested.event, nested.err) {
				return base, nil, completed, step, errNestedEventYieldStopped
			}
		}
		failedSnapshot := stepSnapshot(failed.step, failed.node, cfg.ids, gopact.StepFailed, failed.input, failed.output, failed.err.Error(), failed.startedAt, failed.endedAt)
		attachEffects(&failedSnapshot, failed.effects)
		if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, failed.node, failed.step, &failedSnapshot, failed.err), nil) {
			return base, nil, completed, step, failed.err
		}
		wrapped := fmt.Errorf("graph: parallel fan-out node %q: %w", failed.node, failed.err)
		emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, failed.node, failed.step, nil, wrapped), wrapped)
		return base, nil, completed, step, wrapped
	}

	nextCompleted := copyCompleted(completed)
	for _, result := range results {
		nextCompleted[result.node] = struct{}{}
	}

	var candidates []string
	for _, result := range results {
		next, err := r.candidateNodes(ctx, result.node, result.output)
		if err != nil {
			wrapped := fmt.Errorf("graph: branch from node %q: %w", result.node, err)
			failedSnapshot := stepSnapshot(result.step, result.node, cfg.ids, gopact.StepFailed, result.input, result.output, wrapped.Error(), result.startedAt, time.Now())
			attachEffects(&failedSnapshot, result.effects)
			if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, result.node, result.step, &failedSnapshot, wrapped), nil) {
				return base, nil, completed, step, wrapped
			}
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, result.node, result.step, nil, wrapped), wrapped)
			return base, nil, completed, step, wrapped
		}
		candidates = append(candidates, next...)
	}
	nextQueue := r.readyNodes(candidates, nextCompleted)

	publicResults := make([]FanOutResult[S], 0, len(results))
	for _, result := range results {
		publicResults = append(publicResults, FanOutResult[S]{
			Node:    result.node,
			Step:    result.step,
			Input:   result.input,
			Output:  result.output,
			Effects: copyEffects(result.effects),
		})
	}
	merged, err := merge(ctx, base, publicResults)
	if err != nil {
		wrapped := fmt.Errorf("graph: parallel fan-out merge: %w", err)
		emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", step+len(nodes), nil, wrapped), wrapped)
		return base, nil, completed, step, wrapped
	}
	if err := validateSchemaGuard(ctx, cfg.schemaValidator, r.stateSchema, merged, "parallel fan-out merged state"); err != nil {
		emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", step+len(nodes), nil, err), err)
		return base, nil, completed, step, err
	}

	for _, result := range results {
		for _, nested := range result.nested {
			if !emit(yield, nested.event, nested.err) {
				return base, nil, completed, step, errNestedEventYieldStopped
			}
		}
		completedSnapshot := stepSnapshot(result.step, result.node, cfg.ids, gopact.StepCompleted, result.input, result.output, "", result.startedAt, result.endedAt)
		completedSnapshot.Queue = append([]string(nil), nextQueue...)
		completedSnapshot.Metadata = completedNodesMetadata(nextCompleted)
		attachEffects(&completedSnapshot, result.effects)
		if !emit(yield, graphEvent(gopact.EventNodeCompleted, cfg.ids, result.node, result.step, &completedSnapshot, nil), nil) {
			return base, nil, completed, step, nil
		}
	}
	return merged, nextQueue, nextCompleted, step + len(nodes), nil
}

func (r *Runnable[S]) runParallelFanOutNode(
	ctx context.Context,
	base S,
	nodeName string,
	step int,
	startedAt time.Time,
	cfg invokeConfig,
) parallelFanOutResult[S] {
	result := parallelFanOutResult[S]{
		node:      nodeName,
		step:      step,
		input:     base,
		output:    base,
		startedAt: startedAt,
	}
	node, ok := r.nodes[nodeName]
	if !ok {
		result.err = fmt.Errorf("graph: missing node %q", nodeName)
		result.endedAt = time.Now()
		return result
	}
	if err := r.validateNodeInput(ctx, cfg.schemaValidator, nodeName, base); err != nil {
		result.err = err
		result.endedAt = time.Now()
		return result
	}

	nodeCtxContext := ctx
	if !cfg.ids.IsZero() {
		nodeCtxContext = gopact.ContextWithRuntimeIDs(ctx, cfg.ids)
	}
	nodeCtxContext = contextWithNestedEventSink(nodeCtxContext, nodeName, step, func(event gopact.Event, err error) bool {
		result.nested = append(result.nested, capturedGraphEvent{event: event, err: err})
		return true
	})
	nodeCtx := gopact.NewNodeContext(nodeCtxContext, gopact.NodeContextOptions{
		IDs:   cfg.ids,
		Node:  nodeName,
		Step:  step,
		Input: base,
	})
	output, err := invokeNode(ctx, node, base, nodeCtx, cfg.nodeMiddlewares)
	result.output = output
	result.effects = copyEffects(nodeCtx.Effects)
	result.endedAt = time.Now()
	if err != nil {
		result.err = err
		if errors.Is(err, errNestedEventYieldStopped) {
			result.err = nil
		}
		return result
	}
	if err := r.validateNodeOutput(ctx, cfg.schemaValidator, nodeName, output); err != nil {
		result.err = err
		return result
	}
	return result
}

func firstParallelFanOutError[S any](results []parallelFanOutResult[S]) (parallelFanOutResult[S], bool) {
	var canceled *parallelFanOutResult[S]
	for i := range results {
		if results[i].err == nil {
			continue
		}
		if errors.Is(results[i].err, context.Canceled) {
			if canceled == nil {
				canceled = &results[i]
			}
			continue
		}
		return results[i], true
	}
	if canceled != nil {
		return *canceled, true
	}
	var zero parallelFanOutResult[S]
	return zero, false
}

func queueCanRunInParallel(nodes []string) bool {
	for _, node := range nodes {
		if node == End {
			return false
		}
	}
	return true
}

func copyCompleted(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for node := range in {
		out[node] = struct{}{}
	}
	return out
}
