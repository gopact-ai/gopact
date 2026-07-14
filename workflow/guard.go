package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

type guardMetaContextKey struct{}
type resumedInterruptContextKey struct{}
type resumedInterruptsContextKey struct{}
type nodeResumedInterruptsContextKey struct{}

type guardMetaContext struct {
	runID        string
	workflowName string
	activationID string
	attempt      int
}

type resumedInterrupt struct {
	id                 string
	payloadRef         string
	guardName          string
	phase              GuardPhase
	nodeName           string
	activationID       string
	childRunID         string
	childCheckpointID  string
	candidateOutput    any
	hasCandidateOutput bool
}

type nodeRunResult struct {
	input               any
	hasInput            bool
	output              any
	skipped             bool
	retry               bool
	retryInput          any
	contextValue        any
	contextChanged      bool
	contextBaseRevision int64
}

type guardAttemptResult[I, O any] struct {
	input      I
	retryInput I
	output     O
	skipped    bool
	retry      bool
}

type guardApplyResult[I, O any] struct {
	input      I
	retryInput I
	output     O
	skipBody   bool
	skipped    bool
	retry      bool
}

// GuardPhase identifies where a guard runs around a node boundary.
type GuardPhase string

// Guard phases.
const (
	GuardBeforeRun    GuardPhase = "before_run"
	GuardBeforeCommit GuardPhase = "before_commit"
)

// GuardMeta carries workflow-owned guard identity.
type GuardMeta struct {
	RunID        string
	WorkflowName string
	NodeName     string
	ActivationID string
	Attempt      int
}

// GuardContext is the input to a guard evaluator.
type GuardContext[I, O any] struct {
	Phase           GuardPhase
	Input           I
	CandidateOutput O
	Meta            GuardMeta
}

// GuardEvaluator evaluates one guard boundary.
type GuardEvaluator[I, O any] interface {
	EvaluateGuard(context.Context, GuardContext[I, O]) (GuardDecision[I, O], error)
}

// GuardFunc adapts a function into a GuardEvaluator.
type GuardFunc[I, O any] func(context.Context, GuardContext[I, O]) (GuardDecision[I, O], error)

// EvaluateGuard implements GuardEvaluator.
func (f GuardFunc[I, O]) EvaluateGuard(ctx context.Context, guardCtx GuardContext[I, O]) (GuardDecision[I, O], error) {
	return f(ctx, guardCtx)
}

// GuardDecision is the closed set of workflow-owned guard decisions understood
// by the scheduler and checkpoint replay.
type GuardDecision[I, O any] interface {
	isGuardDecision()
}

// GuardAllow allows guard evaluation to continue.
type GuardAllow[I, O any] struct{}

// GuardRewriteInput replaces the node input before running.
type GuardRewriteInput[I, O any] struct{ Input I }

// GuardRewriteOutput replaces the candidate output before commit.
type GuardRewriteOutput[I, O any] struct{ Output O }

// GuardRetry retries the node with a new input.
type GuardRetry[I, O any] struct{ Input I }

// GuardSkip skips the current activation without output.
type GuardSkip[I, O any] struct{}

// GuardSkipOutput skips the node body with fallback output.
type GuardSkipOutput[I, O any] struct{ Output O }

// GuardFail fails the current activation.
type GuardFail[I, O any] struct{ Err error }

// GuardInterrupt interrupts the current activation.
type GuardInterrupt[I, O any] struct{ Request InterruptRequest }

// GuardReject rejects the current activation with a guard fact.
type GuardReject[I, O any] struct{ Rejection gopact.GuardRejection }

func (GuardAllow[I, O]) isGuardDecision()         {}
func (GuardRewriteInput[I, O]) isGuardDecision()  {}
func (GuardRewriteOutput[I, O]) isGuardDecision() {}
func (GuardRetry[I, O]) isGuardDecision()         {}
func (GuardSkip[I, O]) isGuardDecision()          {}
func (GuardSkipOutput[I, O]) isGuardDecision()    {}
func (GuardFail[I, O]) isGuardDecision()          {}
func (GuardInterrupt[I, O]) isGuardDecision()     {}
func (GuardReject[I, O]) isGuardDecision()        {}

// InterruptRequest describes a workflow-owned guard interrupt.
type InterruptRequest struct {
	ID                  string
	Subject             string
	ResolutionSchemaRef string
}

// InterruptError reports a workflow-owned guard interrupt.
type InterruptError struct {
	Request      InterruptRequest
	Requests     []InterruptRequest
	RunID        string
	CheckpointID string
	GuardName    string
	Phase        GuardPhase

	candidateOutput    any
	hasCandidateOutput bool
}

func (e InterruptError) Error() string {
	if e.Request.ID == "" {
		return "workflow: guard interrupted"
	}
	return "workflow: guard interrupted: " + e.Request.ID
}

// GuardBinding binds a named guard to one phase.
type GuardBinding[I, O any] struct {
	phase     GuardPhase
	name      string
	evaluator GuardEvaluator[I, O]
}

// BeforeRun creates a before-run guard binding.
func BeforeRun[I, O any](name string, evaluator GuardEvaluator[I, O]) GuardBinding[I, O] {
	return GuardBinding[I, O]{phase: GuardBeforeRun, name: name, evaluator: evaluator}
}

// BeforeCommit creates a before-commit guard binding.
func BeforeCommit[I, O any](name string, evaluator GuardEvaluator[I, O]) GuardBinding[I, O] {
	return GuardBinding[I, O]{phase: GuardBeforeCommit, name: name, evaluator: evaluator}
}

// GuardInvokable adapts an invokable into a guard evaluator.
func GuardInvokable[I, O any](inv gopact.Invokable[GuardContext[I, O], GuardDecision[I, O]]) GuardEvaluator[I, O] {
	return GuardFunc[I, O](func(ctx context.Context, guardCtx GuardContext[I, O]) (GuardDecision[I, O], error) {
		if inv == nil {
			return nil, errors.New("workflow: guard invokable is nil")
		}
		return inv.Invoke(ctx, guardCtx)
	})
}

// Guard binds guards to this node.
func (n *Node[I, O]) Guard(bindings ...GuardBinding[I, O]) {
	if n == nil {
		return
	}
	n.assertMutable()
	n.guards = append(n.guards, bindings...)
}

func (n *Node[I, O]) validateGuards() error {
	seen := map[GuardPhase]map[string]struct{}{}
	for _, binding := range n.guards {
		if binding.name == "" {
			return fmt.Errorf("workflow: guard on node %q has empty name", n.name)
		}
		if binding.evaluator == nil {
			return fmt.Errorf("workflow: guard %q on node %q has nil evaluator", binding.name, n.name)
		}
		byName := seen[binding.phase]
		if byName == nil {
			byName = map[string]struct{}{}
			seen[binding.phase] = byName
		}
		if _, ok := byName[binding.name]; ok {
			return fmt.Errorf("workflow: duplicate guard %q on node %q phase %q", binding.name, n.name, binding.phase)
		}
		byName[binding.name] = struct{}{}
	}
	return nil
}

func (n *Node[I, O]) validateLifecycle() error {
	if err := validateLifecycleHooks("node "+n.name, "before", n.before); err != nil {
		return err
	}
	return validateLifecycleHooks("node "+n.name, "after", n.after)
}

func (n *Node[I, O]) validateBindings() error {
	if n.name == "" {
		return errors.New("workflow: node name is required")
	}
	if n.joinTwice {
		return fmt.Errorf("workflow: node %q has multiple join bindings", n.name)
	}
	if n.routeTwice {
		return fmt.Errorf("workflow: node %q has multiple route bindings", n.name)
	}
	if n.merge && n.join != nil {
		return fmt.Errorf("workflow: merge node %q cannot bind join", n.name)
	}
	if n.run == nil {
		return fmt.Errorf("workflow: node %q run function is nil", n.name)
	}
	return nil
}

func (n *Node[I, O]) freeze() {
	if n != nil {
		n.frozen = true
	}
}

func (n *Node[I, O]) runGuardedResult(ctx context.Context, input I, middlewares []erasedNodeMiddleware, opts ...gopact.RunOption) (nodeRunResult, error) {
	attempt := 1
	if meta, ok := ctx.Value(guardMetaContextKey{}).(guardMetaContext); ok && meta.attempt > 0 {
		attempt = meta.attempt
	}
	result, err := n.runAttempt(ctx, input, attempt, middlewares, opts...)
	if err != nil {
		return nodeRunResult{input: result.input, hasInput: true, output: result.output}, err
	}
	if result.retry && result.skipped {
		return nodeRunResult{}, errors.New("workflow: skipped guard cannot retry")
	}
	return nodeRunResult{
		input: result.input, hasInput: true, output: result.output, skipped: result.skipped,
		retry: result.retry, retryInput: result.retryInput,
	}, nil
}

func (n *Node[I, O]) runAttempt(ctx context.Context, input I, attempt int, middlewares []erasedNodeMiddleware, opts ...gopact.RunOption) (guardAttemptResult[I, O], error) {
	var zero O
	resumed, ok := ctx.Value(resumedInterruptContextKey{}).(resumedInterrupt)
	if ok && resumed.phase == GuardBeforeCommit && resumed.hasCandidateOutput {
		output, ok := resumed.candidateOutput.(O)
		if !ok {
			return guardAttemptResult[I, O]{input: input}, fmt.Errorf(
				"workflow: resumed interrupt candidate output %T is not assignable to %s",
				resumed.candidateOutput,
				typeOf[O](),
			)
		}
		result, err := n.applyGuards(ctx, GuardBeforeCommit, input, output, attempt)
		if err != nil {
			return guardAttemptResult[I, O]{input: result.input, output: result.output}, err
		}
		return guardAttemptResult[I, O]{
			input:      result.input,
			retryInput: result.retryInput,
			output:     result.output,
			skipped:    result.skipped,
			retry:      result.retry,
		}, nil
	}
	result, err := n.applyGuards(ctx, GuardBeforeRun, input, zero, attempt)
	if err != nil {
		return guardAttemptResult[I, O]{input: result.input}, err
	}
	input = result.input
	output := result.output
	if result.skipped {
		return guardAttemptResult[I, O]{input: input, skipped: true}, nil
	}
	if !result.skipBody {
		beforeCtx := NodeContext[I, O]{ctx: ctx, Input: input}
		if err := runLifecycleHooks(n.before, &beforeCtx); err != nil {
			return guardAttemptResult[I, O]{input: input}, fmt.Errorf("workflow: before node %q: %w", n.name, err)
		}
		input = beforeCtx.Input
		output, input, err = n.runBody(ctx, input, middlewares, opts...)
		if err != nil {
			return guardAttemptResult[I, O]{input: input}, err
		}
		afterCtx := NodeContext[I, O]{ctx: ctx, Input: input, Output: output}
		if err := runLifecycleHooks(n.after, &afterCtx); err != nil {
			return guardAttemptResult[I, O]{input: input, output: output}, fmt.Errorf("workflow: after node %q: %w", n.name, err)
		}
		output = afterCtx.Output
	}
	result, err = n.applyGuards(ctx, GuardBeforeCommit, input, output, attempt)
	if err != nil {
		return guardAttemptResult[I, O]{input: result.input, output: result.output}, err
	}
	return guardAttemptResult[I, O]{
		input:      result.input,
		retryInput: result.retryInput,
		output:     result.output,
		skipped:    result.skipped,
		retry:      result.retry,
	}, nil
}

func (n *Node[I, O]) runBody(ctx context.Context, input I, middlewares []erasedNodeMiddleware, opts ...gopact.RunOption) (O, I, error) {
	if n.invokable {
		factory, ok := ctx.Value(childOptionsFactoryContextKey{}).(childOptionsFactory)
		if !ok {
			var zero O
			return zero, input, errors.New("workflow: child invocation options are unavailable")
		}
		opts = factory()
	} else {
		opts = nil
	}
	if len(middlewares) == 0 {
		output, err := n.run(ctx, input, opts...)
		return output, input, err
	}
	bodyInput := input
	base := nodeInvoker(func(nextCtx context.Context, nextInput any, nextOpts ...gopact.RunOption) (any, error) {
		typedInput, ok := nextInput.(I)
		if !ok {
			return nil, fmt.Errorf("input type mismatch: got %T, want %s", nextInput, typeOf[I]())
		}
		bodyInput = typedInput
		return n.run(nextCtx, typedInput, nextOpts...)
	})
	invoker := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		middleware := middlewares[i]
		next := invoker
		invoker = func(nextCtx context.Context, nextInput any, nextOpts ...gopact.RunOption) (any, error) {
			output, matched, err := middleware.run(nextCtx, n, nextInput, nextOpts, next)
			if err != nil || matched {
				return output, err
			}
			return next(nextCtx, nextInput, nextOpts...)
		}
	}
	output, err := invoker(ctx, input, opts...)
	if err != nil {
		var zero O
		return zero, bodyInput, err
	}
	typedOutput, ok := output.(O)
	if !ok {
		var zero O
		return zero, bodyInput, fmt.Errorf("output type mismatch: got %T, want %s", output, typeOf[O]())
	}
	return typedOutput, bodyInput, nil
}

func (n *Node[I, O]) applyGuards(ctx context.Context, phase GuardPhase, input I, output O, attempt int) (guardApplyResult[I, O], error) {
	result := guardApplyResult[I, O]{
		input:  input,
		output: output,
	}
	for _, binding := range n.guards {
		if !binding.shouldRun(ctx, phase) {
			continue
		}
		decision, err := binding.evaluator.EvaluateGuard(ctx, GuardContext[I, O]{
			Phase:           phase,
			Input:           result.input,
			CandidateOutput: result.output,
			Meta:            n.guardMeta(ctx, attempt),
		})
		if err != nil {
			return result, err
		}
		done, err := binding.apply(phase, decision, &result)
		if err != nil {
			return result, err
		}
		if done {
			return result, nil
		}
	}
	return result, nil
}

func (binding GuardBinding[I, O]) shouldRun(ctx context.Context, phase GuardPhase) bool {
	if binding.phase != phase {
		return false
	}
	resumed, ok := ctx.Value(resumedInterruptContextKey{}).(resumedInterrupt)
	return !ok || resumed.id == "" || resumed.guardName != binding.name || resumed.phase != phase
}

func (binding GuardBinding[I, O]) apply(phase GuardPhase, decision GuardDecision[I, O], result *guardApplyResult[I, O]) (bool, error) {
	switch d := decision.(type) {
	case nil, GuardAllow[I, O]:
		return false, nil
	case GuardRewriteInput[I, O]:
		return false, binding.rewriteInput(phase, d, result)
	case GuardRewriteOutput[I, O]:
		return false, binding.rewriteOutput(phase, d, result)
	case GuardSkipOutput[I, O]:
		return binding.skipOutput(d, result), nil
	case GuardSkip[I, O]:
		return binding.skip(result), nil
	case GuardRetry[I, O]:
		return binding.retry(phase, d, result)
	case GuardFail[I, O]:
		return true, binding.fail(d)
	case GuardReject[I, O]:
		return true, binding.reject(phase, d)
	case GuardInterrupt[I, O]:
		return true, binding.interrupt(phase, d, result.output)
	default:
		return true, fmt.Errorf("workflow: unknown guard decision %T", d)
	}
}

func (binding GuardBinding[I, O]) rewriteInput(phase GuardPhase, decision GuardRewriteInput[I, O], result *guardApplyResult[I, O]) error {
	if phase != GuardBeforeRun {
		return fmt.Errorf("workflow: guard %q rewrote input outside before_run", binding.name)
	}
	result.input = decision.Input
	return nil
}

func (binding GuardBinding[I, O]) rewriteOutput(phase GuardPhase, decision GuardRewriteOutput[I, O], result *guardApplyResult[I, O]) error {
	if phase != GuardBeforeCommit {
		return fmt.Errorf("workflow: guard %q rewrote output outside before_commit", binding.name)
	}
	result.output = decision.Output
	return nil
}

func (GuardBinding[I, O]) skipOutput(decision GuardSkipOutput[I, O], result *guardApplyResult[I, O]) bool {
	result.output = decision.Output
	result.skipBody = true
	return true
}

func (GuardBinding[I, O]) skip(result *guardApplyResult[I, O]) bool {
	result.skipBody = true
	result.skipped = true
	return true
}

func (binding GuardBinding[I, O]) retry(phase GuardPhase, decision GuardRetry[I, O], result *guardApplyResult[I, O]) (bool, error) {
	if phase != GuardBeforeCommit {
		return true, fmt.Errorf("workflow: guard %q retried outside before_commit", binding.name)
	}
	result.retryInput = decision.Input
	result.retry = true
	return true, nil
}

func (binding GuardBinding[I, O]) fail(decision GuardFail[I, O]) error {
	if decision.Err == nil {
		return fmt.Errorf("workflow: guard %q failed", binding.name)
	}
	return decision.Err
}

func (binding GuardBinding[I, O]) reject(phase GuardPhase, decision GuardReject[I, O]) error {
	rejection := decision.Rejection
	if rejection.GuardName == "" {
		rejection.GuardName = binding.name
	}
	if rejection.Phase == "" {
		rejection.Phase = string(phase)
	}
	return rejection
}

func (binding GuardBinding[I, O]) interrupt(phase GuardPhase, decision GuardInterrupt[I, O], output O) error {
	if decision.Request.ID == "" {
		return fmt.Errorf("workflow: guard %q interrupt id is required", binding.name)
	}
	return InterruptError{
		Request: decision.Request, GuardName: binding.name, Phase: phase,
		candidateOutput: output, hasCandidateOutput: phase == GuardBeforeCommit,
	}
}

func (n *Node[I, O]) guardMeta(ctx context.Context, attempt int) GuardMeta {
	meta, _ := ctx.Value(guardMetaContextKey{}).(guardMetaContext)
	return GuardMeta{
		RunID:        meta.runID,
		WorkflowName: meta.workflowName,
		NodeName:     n.name,
		ActivationID: meta.activationID,
		Attempt:      attempt,
	}
}
