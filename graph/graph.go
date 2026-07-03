package graph

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sort"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// Start 是 graph 的虚拟入口节点。
	Start = "__start__"
	// End 是 graph 的虚拟终止节点。
	End = "__end__"

	// EventMetadataParentNode marks the immediate parent graph node for a nested event.
	EventMetadataParentNode = "graph_parent_node"
	// EventMetadataParentStep marks the immediate parent graph step for a nested event.
	EventMetadataParentStep = "graph_parent_step"
)

const metadataCompletedNodes = "graph_completed_nodes"

// ErrNodeEventYieldStopped can be returned by a node after EmitNodeEvent reports
// that the graph event consumer stopped accepting events.
var ErrNodeEventYieldStopped = errors.New("graph: nested event yield stopped")

var errNestedEventYieldStopped = ErrNodeEventYieldStopped

// NodeFunc 表示 graph 中一步状态转换。
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)

// BranchFunc selects dynamic successor nodes from the completed node state.
type BranchFunc[S any] func(ctx context.Context, state S) ([]string, error)

// Checkpoint 是节点完成后的可持久化状态快照。
type Checkpoint[S any] struct {
	ID            string
	IDs           gopact.RuntimeIDs
	ThreadID      string
	Step          int
	Node          string
	Phase         gopact.StepPhase
	State         S
	Queue         []string
	Pending       *gopact.InterruptRecord
	Effects       []gopact.EffectRecord
	ConfigVersion string
	CreatedAt     time.Time
	Metadata      map[string]any
}

// Checkpointer 持久化 graph 状态快照。
type Checkpointer[S any] interface {
	Put(ctx context.Context, checkpoint Checkpoint[S]) error
}

// CheckpointLoader loads the latest resumable graph checkpoint for a thread.
type CheckpointLoader[S any] interface {
	Latest(ctx context.Context, threadID string) (Checkpoint[S], bool, error)
}

// CheckpointStore can both persist checkpoints and load the latest one.
type CheckpointStore[S any] interface {
	Checkpointer[S]
	CheckpointLoader[S]
}

// ArtifactVerifier verifies artifact refs before a resumable boundary is trusted.
type ArtifactVerifier interface {
	VerifyRefs(ctx context.Context, refs []gopact.ArtifactRef) error
}

// ArtifactVerifierFunc adapts a function into an ArtifactVerifier.
type ArtifactVerifierFunc func(context.Context, []gopact.ArtifactRef) error

// VerifyRefs calls f.
func (f ArtifactVerifierFunc) VerifyRefs(ctx context.Context, refs []gopact.ArtifactRef) error {
	return f(ctx, refs)
}

// Graph 是一个小型的类型化执行 graph。
type Graph[S any] struct {
	nodes         map[string]NodeFunc[S]
	runnableNodes map[string]*Runnable[S]
	edges         map[string][]string
	branches      map[string][]BranchFunc[S]
}

// New 创建一个空 graph。
func New[S any]() *Graph[S] {
	return &Graph[S]{
		nodes:         make(map[string]NodeFunc[S]),
		runnableNodes: make(map[string]*Runnable[S]),
		edges:         make(map[string][]string),
		branches:      make(map[string][]BranchFunc[S]),
	}
}

// AddNode 注册一个状态转换节点。
func (g *Graph[S]) AddNode(name string, node NodeFunc[S]) {
	g.nodes[name] = node
	delete(g.runnableNodes, name)
}

// AddRunnableNode registers a same-state runnable as a graph node.
func (g *Graph[S]) AddRunnableNode(name string, runnable *Runnable[S], opts ...InvokeOption) {
	invokeOpts := append([]InvokeOption(nil), opts...)
	g.AddNode(name, func(ctx context.Context, state S) (S, error) {
		if runnable == nil {
			return state, errors.New("graph: runnable node is nil")
		}
		callOpts := make([]InvokeOption, 0, len(invokeOpts)+1)
		if ids, ok := gopact.RuntimeIDsFromContext(ctx); ok && !ids.IsZero() {
			callOpts = append(callOpts, WithRuntimeIDs(ids))
		}
		callOpts = append(callOpts, invokeOpts...)
		if sink, ok := nestedEventSinkFromContext(ctx); ok {
			return runNestedRunnableNode(ctx, state, runnable, callOpts, sink)
		}
		return runnable.Invoke(ctx, state, callOpts...)
	})
	g.runnableNodes[name] = runnable
}

// AddEdge 连接两个节点。Graph 边界使用 Start 和 End。
func (g *Graph[S]) AddEdge(from, to string) {
	g.edges[from] = append(g.edges[from], to)
}

// AddBranch registers dynamic successors for a node.
func (g *Graph[S]) AddBranch(from string, branch BranchFunc[S]) {
	g.branches[from] = append(g.branches[from], branch)
}

// Compile 校验 graph 结构，并返回不可变 runnable。
func (g *Graph[S]) Compile() (*Runnable[S], error) {
	if g == nil {
		return nil, errors.New("graph: nil graph")
	}
	for from, tos := range g.edges {
		if from != Start {
			if _, ok := g.nodes[from]; !ok {
				return nil, fmt.Errorf("graph: missing source node %q", from)
			}
		}
		for _, to := range tos {
			if to == End {
				continue
			}
			if _, ok := g.nodes[to]; !ok {
				return nil, fmt.Errorf("graph: missing target node %q", to)
			}
		}
	}
	for name, runnable := range g.runnableNodes {
		if runnable == nil {
			return nil, fmt.Errorf("graph: runnable node %q is nil", name)
		}
	}
	for from, branches := range g.branches {
		if from == Start || from == End {
			return nil, fmt.Errorf("graph: branch source %q is not supported", from)
		}
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("graph: missing branch source node %q", from)
		}
		for i, branch := range branches {
			if branch == nil {
				return nil, fmt.Errorf("graph: branch %q[%d] is nil", from, i)
			}
		}
	}

	nodes := make(map[string]NodeFunc[S], len(g.nodes))
	for name, node := range g.nodes {
		if node == nil {
			return nil, fmt.Errorf("graph: node %q is nil", name)
		}
		nodes[name] = node
	}
	edges := make(map[string][]string, len(g.edges))
	for from, tos := range g.edges {
		edges[from] = append([]string(nil), tos...)
	}
	joins := joinPredecessors(edges)
	branches := make(map[string][]BranchFunc[S], len(g.branches))
	for from, branchList := range g.branches {
		branches[from] = append([]BranchFunc[S](nil), branchList...)
	}
	return &Runnable[S]{
		nodes:     nodes,
		nodeKinds: topologyNodeKinds(g.nodes, g.runnableNodes),
		edges:     edges,
		branches:  branches,
		joins:     joins,
		maxSteps:  1024,
	}, nil
}

// Runnable 是编译后的 graph。
type Runnable[S any] struct {
	nodes     map[string]NodeFunc[S]
	nodeKinds map[string]TopologyNodeKind
	edges     map[string][]string
	branches  map[string][]BranchFunc[S]
	joins     map[string][]string
	maxSteps  int
}

type nestedEventSinkContextKey struct{}

type nestedEventSink struct {
	parentNode string
	parentStep int
	yield      func(gopact.Event, error) bool
}

func nestedEventSinkFromContext(ctx context.Context) (nestedEventSink, bool) {
	if ctx == nil {
		return nestedEventSink{}, false
	}
	sink, ok := ctx.Value(nestedEventSinkContextKey{}).(nestedEventSink)
	if !ok || sink.yield == nil {
		return nestedEventSink{}, false
	}
	return sink, true
}

func contextWithNestedEventSink(ctx context.Context, parentNode string, parentStep int, yield func(gopact.Event, error) bool) context.Context {
	if yield == nil {
		return ctx
	}
	return context.WithValue(ctx, nestedEventSinkContextKey{}, nestedEventSink{
		parentNode: parentNode,
		parentStep: parentStep,
		yield:      yield,
	})
}

// EmitNodeEvent publishes a child event from inside a graph node.
//
// The event is annotated with graph parent metadata when the node is running
// through Runnable.Run. Calls outside a graph run are no-ops and return true.
func EmitNodeEvent(ctx context.Context, event gopact.Event, err error) bool {
	sink, ok := nestedEventSinkFromContext(ctx)
	if !ok {
		return true
	}
	return sink.yield(annotateNestedEvent(event, sink), err)
}

func runNestedRunnableNode[S any](ctx context.Context, state S, runnable *Runnable[S], opts []InvokeOption, sink nestedEventSink) (S, error) {
	next := state
	for event, err := range runnable.Run(ctx, state, opts...) {
		if output, ok := nestedEventOutput[S](event); ok {
			next = output
		}
		event = annotateNestedEvent(event, sink)
		if !sink.yield(event, err) {
			return next, errNestedEventYieldStopped
		}
		if err != nil {
			return next, err
		}
	}
	return next, nil
}

func nestedEventOutput[S any](event gopact.Event) (S, bool) {
	var zero S
	if event.StepSnapshot == nil {
		return zero, false
	}
	switch event.StepSnapshot.Phase {
	case gopact.StepCompleted, gopact.StepInterrupted, gopact.StepCanceled, gopact.StepFailed:
	default:
		return zero, false
	}
	output, ok := event.StepSnapshot.Output.(S)
	if !ok {
		return zero, false
	}
	return output, true
}

func annotateNestedEvent(event gopact.Event, sink nestedEventSink) gopact.Event {
	event.Metadata = copyMap(event.Metadata)
	if event.Metadata == nil {
		event.Metadata = make(map[string]any, 2)
	}
	event.Metadata[EventMetadataParentNode] = sink.parentNode
	event.Metadata[EventMetadataParentStep] = sink.parentStep
	return event
}

func joinPredecessors(edges map[string][]string) map[string][]string {
	preds := map[string]map[string]struct{}{}
	for from, tos := range edges {
		if from == Start {
			continue
		}
		for _, to := range tos {
			if to == End || to == from {
				continue
			}
			if preds[to] == nil {
				preds[to] = map[string]struct{}{}
			}
			preds[to][from] = struct{}{}
		}
	}
	joins := map[string][]string{}
	for to, froms := range preds {
		if len(froms) < 2 {
			continue
		}
		joins[to] = make([]string, 0, len(froms))
		for from := range froms {
			joins[to] = append(joins[to], from)
		}
		sort.Strings(joins[to])
	}
	return joins
}

type invokeConfig struct {
	ids              gopact.RuntimeIDs
	configVersion    string
	checkpointer     any
	checkpointLoader any
	nodeMiddlewares  []gopact.NodeHandler
	stepExport       *gopact.StepExport
	resumeRequest    *gopact.ResumeRequest
	artifactVerifier ArtifactVerifier
	schemaValidator  gopact.JSONSchemaValidator
	maxSteps         int
	maxStepsSet      bool
}

// InvokeOption 配置一次 graph 调用。
type InvokeOption func(*invokeConfig)

// WithThreadID 设置 checkpointer 使用的对话或 workflow thread 标识。
func WithThreadID(threadID string) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.ids.ThreadID = threadID
	}
}

// WithRuntimeIDs 设置本次 graph run 的运行时身份。
func WithRuntimeIDs(ids gopact.RuntimeIDs) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.ids = ids.WithDefaults(cfg.ids)
	}
}

// WithConfigVersion marks checkpoints written by this graph run with a runtime config version.
func WithConfigVersion(version string) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.configVersion = version
	}
}

// WithCheckpointer 在每个节点完成后持久化 checkpoint。
func WithCheckpointer[S any](checkpointer Checkpointer[S]) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.checkpointer = checkpointer
	}
}

// WithCheckpointLoader resumes from the latest checkpoint for the configured thread.
func WithCheckpointLoader[S any](loader CheckpointLoader[S]) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.checkpointLoader = loader
	}
}

// WithCheckpointStore writes checkpoints and resumes from the latest checkpoint for a thread.
func WithCheckpointStore[S any](store CheckpointStore[S]) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.checkpointer = store
		cfg.checkpointLoader = store
	}
}

// WithNodeMiddleware wraps every graph node execution with gopact node middleware.
func WithNodeMiddleware(middlewares ...gopact.NodeHandler) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.nodeMiddlewares = append(cfg.nodeMiddlewares, middlewares...)
	}
}

// WithStepExport resumes execution from a completed exported step boundary.
func WithStepExport(export gopact.StepExport) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.stepExport = &export
	}
}

// WithResumeRequest resumes an interrupted step export with an external payload.
func WithResumeRequest(request gopact.ResumeRequest) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.resumeRequest = &request
	}
}

// WithJSONSchemaValidator sets the validator used for schema-guarded resume
// boundaries in this graph invocation.
func WithJSONSchemaValidator(validator gopact.JSONSchemaValidator) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.schemaValidator = validator
	}
}

// WithMaxSteps overrides the runnable step limit for a single graph invocation.
func WithMaxSteps(maxSteps int) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.maxSteps = maxSteps
		cfg.maxStepsSet = true
	}
}

// WithArtifactVerifier verifies exported artifact refs before step import or checkpoint resume continues.
func WithArtifactVerifier(verifier ArtifactVerifier) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.artifactVerifier = verifier
	}
}

// Invoke 从 Start 运行 graph，直到到达 End 或没有后续节点。
func (r *Runnable[S]) Invoke(ctx context.Context, initial S, opts ...InvokeOption) (S, error) {
	return r.execute(ctx, initial, nil, opts...)
}

// Run 从 Start 运行 graph，并按 step 边界流式发出事件。
func (r *Runnable[S]) Run(ctx context.Context, initial S, opts ...InvokeOption) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		_, _ = r.execute(ctx, initial, yield, opts...)
	}
}

// AsRunnable adapts a typed graph runnable to the root gopact.Runner facade.
func (r *Runnable[S]) AsRunnable(opts ...InvokeOption) gopact.Runnable {
	return runnableAdapter[S]{
		runnable: r,
		opts:     append([]InvokeOption(nil), opts...),
	}
}

type runnableAdapter[S any] struct {
	runnable *Runnable[S]
	opts     []InvokeOption
}

func (a runnableAdapter[S]) Run(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		state, ok := input.(S)
		if !ok {
			err := fmt.Errorf("graph: input type mismatch: got %T", input)
			yield(graphEvent(gopact.EventRunFailed, gopact.RuntimeIDs{}, "", 0, nil, err), err)
			return
		}
		runCfg := gopact.ResolveRunOptions(opts...)
		invokeOpts := append([]InvokeOption(nil), a.opts...)
		if !runCfg.IDs.IsZero() {
			invokeOpts = append(invokeOpts, WithRuntimeIDs(runCfg.IDs))
		}
		if runCfg.StepExport != nil {
			invokeOpts = append(invokeOpts, WithStepExport(*runCfg.StepExport))
		}
		if runCfg.ResumeRequest != nil {
			invokeOpts = append(invokeOpts, WithResumeRequest(*runCfg.ResumeRequest))
		}
		if runCfg.JSONSchemaValidator != nil {
			invokeOpts = append(invokeOpts, WithJSONSchemaValidator(runCfg.JSONSchemaValidator))
		}
		for event, err := range a.runnable.Run(ctx, state, invokeOpts...) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (r *Runnable[S]) execute(ctx context.Context, initial S, yield func(gopact.Event, error) bool, opts ...InvokeOption) (S, error) {
	if r == nil {
		err := errors.New("graph: nil runnable")
		emit(yield, graphEvent(gopact.EventRunFailed, gopact.RuntimeIDs{}, "", 0, nil, err), err)
		return initial, err
	}

	cfg := invokeConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if ids, ok := gopact.RuntimeIDsFromContext(ctx); ok && !ids.IsZero() {
		cfg.ids = cfg.ids.WithDefaults(ids)
	}
	maxSteps := r.maxSteps
	if cfg.maxStepsSet {
		if cfg.maxSteps <= 0 {
			err := fmt.Errorf("graph: max steps must be positive, got %d", cfg.maxSteps)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", 0, nil, err), err)
			return initial, err
		}
		maxSteps = cfg.maxSteps
	}

	var checkpointer Checkpointer[S]
	if cfg.checkpointer != nil {
		cp, ok := cfg.checkpointer.(Checkpointer[S])
		if !ok {
			err := errors.New("graph: checkpointer state type mismatch")
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", 0, nil, err), err)
			return initial, err
		}
		checkpointer = cp
	}
	var checkpointLoader CheckpointLoader[S]
	if cfg.checkpointLoader != nil {
		loader, ok := cfg.checkpointLoader.(CheckpointLoader[S])
		if !ok {
			err := errors.New("graph: checkpoint loader state type mismatch")
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", 0, nil, err), err)
			return initial, err
		}
		checkpointLoader = loader
	}

	state := initial
	queue := append([]string(nil), r.edges[Start]...)
	completed := map[string]struct{}{}
	step := 0
	var importedStep *gopact.StepSnapshot
	var loadedCheckpoint *gopact.StepSnapshot
	var resumeRequest *gopact.ResumeRequest
	resumingNode := false
	if cfg.stepExport != nil {
		resumedState, resumedQueue, resumedStep, resumedIDs, resumedCompleted, err := r.resumeFromStepExport(ctx, *cfg.stepExport, cfg.resumeRequest, cfg.schemaValidator)
		if err != nil {
			var interruptErr *gopact.InterruptError
			if errors.As(err, &interruptErr) && cfg.stepExport.Step.Phase == gopact.StepInterrupted {
				snapshot := cfg.stepExport.Step
				ids := cfg.ids.WithDefaults(snapshot.IDs)
				emit(yield, graphEvent(gopact.EventRunInterrupted, ids, snapshot.Node, snapshot.Step, &snapshot, interruptErr), interruptErr)
				return state, interruptErr
			}
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", step, nil, err), err)
			return state, err
		}
		state = resumedState
		queue = resumedQueue
		completed = resumedCompleted
		step = resumedStep
		cfg.ids = cfg.ids.WithDefaults(resumedIDs)
		step := cfg.stepExport.Step
		importedStep = &step
		resumingNode = true
		if cfg.resumeRequest != nil && cfg.stepExport.Step.Phase == gopact.StepInterrupted {
			resume := *cfg.resumeRequest
			resumeRequest = &resume
		}
	} else if checkpointLoader != nil {
		if cfg.ids.ThreadID == "" {
			err := errors.New("graph: checkpoint loader requires thread id")
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", 0, nil, err), err)
			return state, err
		}
		checkpoint, ok, err := checkpointLoader.Latest(ctx, cfg.ids.ThreadID)
		if err != nil {
			wrapped := fmt.Errorf("graph: load checkpoint for thread %q: %w", cfg.ids.ThreadID, err)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", 0, nil, wrapped), wrapped)
			return state, wrapped
		}
		if ok {
			resumedState, resumedQueue, resumedStep, resumedIDs, resumedCompleted, err := r.resumeFromCheckpoint(ctx, checkpoint, cfg.resumeRequest, cfg.schemaValidator)
			if err != nil {
				var interruptErr *gopact.InterruptError
				if errors.As(err, &interruptErr) && checkpointPhase(checkpoint) == gopact.StepInterrupted {
					snapshot := checkpointStepSnapshot(checkpoint, cfg.ids.WithDefaults(checkpointIDs(checkpoint)))
					emit(yield, graphEvent(gopact.EventRunInterrupted, snapshot.IDs, snapshot.Node, snapshot.Step, &snapshot, interruptErr), interruptErr)
					return state, interruptErr
				}
				emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", step, nil, err), err)
				return state, err
			}
			state = resumedState
			queue = resumedQueue
			completed = resumedCompleted
			step = resumedStep
			cfg.ids = cfg.ids.WithDefaults(resumedIDs)
			snapshot := checkpointStepSnapshot(checkpoint, cfg.ids)
			loadedCheckpoint = &snapshot
			resumingNode = true
			if cfg.resumeRequest != nil && checkpointPhase(checkpoint) == gopact.StepInterrupted {
				resume := *cfg.resumeRequest
				resumeRequest = &resume
			}
		}
	}
	if !emit(yield, graphEvent(gopact.EventRunStarted, cfg.ids, "", step, nil, nil), nil) {
		return state, nil
	}
	if importedStep != nil {
		if err := verifySnapshotArtifacts(ctx, cfg.artifactVerifier, *importedStep); err != nil {
			wrapped := fmt.Errorf("graph: verify imported step artifacts: %w", err)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, importedStep.Node, importedStep.Step, nil, wrapped), wrapped)
			return state, wrapped
		}
		event := graphEvent(gopact.EventStepImported, cfg.ids, importedStep.Node, importedStep.Step, importedStep, nil)
		if err := attachEffectReplayPlan(&event, *importedStep); err != nil {
			wrapped := fmt.Errorf("graph: plan imported step effects: %w", err)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, importedStep.Node, importedStep.Step, nil, wrapped), wrapped)
			return state, wrapped
		}
		if !emit(yield, event, nil) {
			return state, nil
		}
		if resumeRequest != nil {
			resumeEvent := graphEvent(gopact.EventResumeReceived, cfg.ids, importedStep.Node, importedStep.Step, importedStep, nil)
			resumeEvent.Metadata = map[string]any{
				"interrupt_id": resumeRequest.InterruptID,
			}
			if resumeRequest.StepID != "" {
				resumeEvent.Metadata["step_id"] = resumeRequest.StepID
			}
			if !emit(yield, resumeEvent, nil) {
				return state, nil
			}
		}
	}
	if loadedCheckpoint != nil {
		if err := verifySnapshotArtifacts(ctx, cfg.artifactVerifier, *loadedCheckpoint); err != nil {
			wrapped := fmt.Errorf("graph: verify checkpoint artifacts: %w", err)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, loadedCheckpoint.Node, loadedCheckpoint.Step, nil, wrapped), wrapped)
			return state, wrapped
		}
		event := graphEvent(gopact.EventCheckpointLoaded, cfg.ids, loadedCheckpoint.Node, loadedCheckpoint.Step, loadedCheckpoint, nil)
		event.Metadata = copyMap(loadedCheckpoint.Metadata)
		if checkpointID, _ := loadedCheckpoint.Metadata["checkpoint_id"].(string); checkpointID != "" {
			if event.Metadata == nil {
				event.Metadata = make(map[string]any)
			}
			event.Metadata["checkpoint_id"] = checkpointID
		}
		if err := attachEffectReplayPlan(&event, *loadedCheckpoint); err != nil {
			wrapped := fmt.Errorf("graph: plan checkpoint effects: %w", err)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, loadedCheckpoint.Node, loadedCheckpoint.Step, nil, wrapped), wrapped)
			return state, wrapped
		}
		if !emit(yield, event, nil) {
			return state, nil
		}
		if resumeRequest != nil {
			resumeEvent := graphEvent(gopact.EventResumeReceived, cfg.ids, loadedCheckpoint.Node, loadedCheckpoint.Step, loadedCheckpoint, nil)
			resumeEvent.Metadata = map[string]any{
				"interrupt_id": resumeRequest.InterruptID,
			}
			if checkpointID, _ := loadedCheckpoint.Metadata["checkpoint_id"].(string); checkpointID != "" {
				resumeEvent.Metadata["checkpoint_id"] = checkpointID
			}
			if resumeRequest.StepID != "" {
				resumeEvent.Metadata["step_id"] = resumeRequest.StepID
			}
			if !emit(yield, resumeEvent, nil) {
				return state, nil
			}
		}
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			emit(yield, graphEvent(gopact.EventRunCanceled, cfg.ids, "", step, nil, err), err)
			return state, err
		}
		if step >= maxSteps {
			err := fmt.Errorf("graph: exceeded max steps %d", maxSteps)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, "", step, nil, err), err)
			return state, err
		}

		name := queue[0]
		queue = queue[1:]
		if name == End {
			continue
		}

		node, ok := r.nodes[name]
		if !ok {
			err := fmt.Errorf("graph: missing node %q", name)
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, err), err)
			return state, err
		}

		nextStep := step + 1
		startedAt := time.Now()
		startSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepRunning, state, nil, "", startedAt, time.Time{})
		startEvent := gopact.EventNodeStarted
		if resumingNode {
			startEvent = gopact.EventNodeResumed
			resumingNode = false
		}
		if !emit(yield, graphEvent(startEvent, cfg.ids, name, nextStep, &startSnapshot, nil), nil) {
			return state, nil
		}

		nodeCtxContext := ctx
		if !cfg.ids.IsZero() {
			nodeCtxContext = gopact.ContextWithRuntimeIDs(ctx, cfg.ids)
		}
		nodeCtxContext = contextWithNestedEventSink(nodeCtxContext, name, nextStep, yield)
		nodeCtx := gopact.NewNodeContext(nodeCtxContext, gopact.NodeContextOptions{
			IDs:   cfg.ids,
			Node:  name,
			Step:  nextStep,
			Input: state,
		})
		next, err := invokeNode(ctx, node, state, nodeCtx, cfg.nodeMiddlewares)
		if err != nil {
			if errors.Is(err, errNestedEventYieldStopped) {
				return state, nil
			}
			var interruptErr *gopact.InterruptError
			if errors.As(err, &interruptErr) {
				interrupted, ok := nodeCtx.Output.(S)
				if !ok {
					wrapped := fmt.Errorf("graph: node %q interrupted with output type mismatch: got %T", name, nodeCtx.Output)
					failedSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepFailed, nodeCtx.Input, nodeCtx.Output, wrapped.Error(), startedAt, time.Now())
					attachEffects(&failedSnapshot, nodeCtx.Effects)
					if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, name, nextStep, &failedSnapshot, wrapped), nil) {
						return state, wrapped
					}
					emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, wrapped), wrapped)
					return state, wrapped
				}
				if err := interruptErr.Record.Validate(); err != nil {
					wrapped := fmt.Errorf("graph: node %q interrupt record: %w", name, err)
					failedSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepFailed, nodeCtx.Input, nodeCtx.Output, err.Error(), startedAt, time.Now())
					attachEffects(&failedSnapshot, nodeCtx.Effects)
					if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, name, nextStep, &failedSnapshot, err), nil) {
						return state, wrapped
					}
					emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, err), wrapped)
					return state, wrapped
				}
				state = interrupted
				interruptedSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepInterrupted, nodeCtx.Input, nodeCtx.Output, "", startedAt, time.Now())
				attachEffects(&interruptedSnapshot, nodeCtx.Effects)
				interruptedSnapshot.Metadata = completedNodesMetadata(completed)
				record := interruptErr.Record
				interruptedSnapshot.Pending = &record
				interruptedSnapshot.Queue = append([]string(nil), queue...)
				interruptedSnapshot.Queue = append(interruptedSnapshot.Queue, r.edges[name]...)
				if checkpointer != nil {
					checkpoint := checkpointFromSnapshot(interruptedSnapshot, state)
					checkpoint.ConfigVersion = cfg.configVersion
					err := checkpointer.Put(ctx, checkpoint)
					if err != nil {
						wrapped := fmt.Errorf("graph: checkpoint interrupted node %q: %w", name, err)
						emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, nextStep, nil, wrapped), wrapped)
						return state, wrapped
					}
				}
				if !emit(yield, graphEvent(gopact.EventInterrupted, cfg.ids, name, nextStep, &interruptedSnapshot, nil), nil) {
					return state, interruptErr
				}
				emit(yield, graphEvent(gopact.EventRunInterrupted, cfg.ids, name, nextStep, &interruptedSnapshot, interruptErr), interruptErr)
				return state, interruptErr
			}
			if errors.Is(err, context.Canceled) {
				canceled := state
				if nodeCtx.Output != nil {
					output, ok := nodeCtx.Output.(S)
					if !ok {
						wrapped := fmt.Errorf("graph: node %q canceled with output type mismatch: got %T", name, nodeCtx.Output)
						failedSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepFailed, nodeCtx.Input, nodeCtx.Output, wrapped.Error(), startedAt, time.Now())
						attachEffects(&failedSnapshot, nodeCtx.Effects)
						if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, name, nextStep, &failedSnapshot, wrapped), nil) {
							return state, wrapped
						}
						emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, wrapped), wrapped)
						return state, wrapped
					}
					canceled = output
				} else {
					nodeCtx.Output = state
				}
				state = canceled
				canceledSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepCanceled, nodeCtx.Input, nodeCtx.Output, err.Error(), startedAt, time.Now())
				attachEffects(&canceledSnapshot, nodeCtx.Effects)
				canceledSnapshot.Metadata = completedNodesMetadata(completed)
				canceledSnapshot.Queue = append([]string(nil), queue...)
				canceledSnapshot.Queue = append(canceledSnapshot.Queue, r.edges[name]...)
				if checkpointer != nil {
					checkpoint := checkpointFromSnapshot(canceledSnapshot, state)
					checkpoint.ConfigVersion = cfg.configVersion
					err := checkpointer.Put(ctx, checkpoint)
					if err != nil {
						wrapped := fmt.Errorf("graph: checkpoint canceled node %q: %w", name, err)
						emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, nextStep, nil, wrapped), wrapped)
						return state, wrapped
					}
				}
				emit(yield, graphEvent(gopact.EventRunCanceled, cfg.ids, name, nextStep, &canceledSnapshot, err), err)
				return state, err
			}
			wrapped := fmt.Errorf("graph: node %q: %w", name, err)
			failedSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepFailed, nodeCtx.Input, nodeCtx.Output, err.Error(), startedAt, time.Now())
			attachEffects(&failedSnapshot, nodeCtx.Effects)
			if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, name, nextStep, &failedSnapshot, err), nil) {
				return state, wrapped
			}
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, err), wrapped)
			return state, wrapped
		}
		state = next
		completed[name] = struct{}{}
		successors, err := r.nextNodes(ctx, name, state, completed)
		if err != nil {
			wrapped := fmt.Errorf("graph: branch from node %q: %w", name, err)
			failedSnapshot := stepSnapshot(nextStep, name, cfg.ids, gopact.StepFailed, nodeCtx.Input, nodeCtx.Output, wrapped.Error(), startedAt, time.Now())
			attachEffects(&failedSnapshot, nodeCtx.Effects)
			if !emit(yield, graphEvent(gopact.EventNodeFailed, cfg.ids, name, nextStep, &failedSnapshot, wrapped), nil) {
				return state, wrapped
			}
			emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, wrapped), wrapped)
			return state, wrapped
		}

		step++
		nextQueue := append([]string(nil), queue...)
		nextQueue = append(nextQueue, successors...)
		completedSnapshot := stepSnapshot(step, name, cfg.ids, gopact.StepCompleted, nodeCtx.Input, nodeCtx.Output, "", startedAt, time.Now())
		completedSnapshot.Queue = append([]string(nil), nextQueue...)
		completedSnapshot.Metadata = completedNodesMetadata(completed)
		attachEffects(&completedSnapshot, nodeCtx.Effects)
		if !emit(yield, graphEvent(gopact.EventNodeCompleted, cfg.ids, name, step, &completedSnapshot, nil), nil) {
			return state, nil
		}

		if checkpointer != nil {
			err := checkpointer.Put(ctx, Checkpoint[S]{
				ID:            completedSnapshot.ID,
				IDs:           cfg.ids,
				ThreadID:      cfg.ids.ThreadID,
				Step:          step,
				Node:          name,
				Phase:         gopact.StepCompleted,
				State:         state,
				Queue:         nextQueue,
				Effects:       copyEffects(completedSnapshot.Effects),
				ConfigVersion: cfg.configVersion,
				CreatedAt:     time.Now(),
				Metadata:      copyMap(completedSnapshot.Metadata),
			})
			if err != nil {
				wrapped := fmt.Errorf("graph: checkpoint node %q: %w", name, err)
				emit(yield, graphEvent(gopact.EventRunFailed, cfg.ids, name, step, nil, err), wrapped)
				return state, wrapped
			}
		}

		queue = append(queue, successors...)
	}

	emit(yield, graphEvent(gopact.EventRunCompleted, cfg.ids, "", step, nil, nil), nil)
	return state, nil
}

func (r *Runnable[S]) nextNodes(ctx context.Context, name string, state S, completed map[string]struct{}) ([]string, error) {
	next := append([]string(nil), r.edges[name]...)
	for _, branch := range r.branches[name] {
		targets, err := branch(ctx, state)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if target == "" {
				return nil, errors.New("branch returned empty target")
			}
			if target != End {
				if _, ok := r.nodes[target]; !ok {
					return nil, fmt.Errorf("branch returned missing target %q", target)
				}
			}
			next = append(next, target)
		}
	}
	return r.readyNodes(next, completed), nil
}

func (r *Runnable[S]) readyNodes(targets []string, completed map[string]struct{}) []string {
	if len(targets) == 0 {
		return nil
	}
	ready := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		prereqs := r.joins[target]
		if len(prereqs) == 0 || target == End {
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			ready = append(ready, target)
			continue
		}
		allDone := true
		for _, prereq := range prereqs {
			if _, ok := completed[prereq]; !ok {
				allDone = false
				break
			}
		}
		if allDone {
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			ready = append(ready, target)
		}
	}
	return ready
}

func (r *Runnable[S]) resumeFromStepExport(ctx context.Context, export gopact.StepExport, resume *gopact.ResumeRequest, validator gopact.JSONSchemaValidator) (S, []string, int, gopact.RuntimeIDs, map[string]struct{}, error) {
	var zero S
	if err := export.Validate(); err != nil {
		return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume step export: %w", err)
	}
	if export.Step.Phase != gopact.StepCompleted && export.Step.Phase != gopact.StepInterrupted && export.Step.Phase != gopact.StepCanceled {
		return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume step phase %q is not supported", export.Step.Phase)
	}
	if export.Step.Phase == gopact.StepInterrupted {
		if resume == nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, &gopact.InterruptError{Record: *export.Step.Pending}
		}
		if err := resume.Validate(); err != nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume request: %w", err)
		}
		if resume.InterruptID != export.Step.Pending.ID {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume interrupt id %q does not match pending interrupt %q", resume.InterruptID, export.Step.Pending.ID)
		}
		if resume.StepID != "" && resume.StepID != export.Step.ID {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume step id %q does not match export step %q", resume.StepID, export.Step.ID)
		}
		if err := gopact.ValidateResumePayloadWithValidator(ctx, validator, *export.Step.Pending, *resume); err != nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume payload: %w", err)
		}
	}
	if export.Step.Node != Start && export.Step.Node != End {
		if _, ok := r.nodes[export.Step.Node]; !ok {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume node %q is missing", export.Step.Node)
		}
	}
	state, ok := export.Step.Output.(S)
	if !ok {
		return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume state type mismatch: got %T", export.Step.Output)
	}
	queue := append([]string(nil), export.Step.Queue...)
	if len(queue) == 0 {
		queue = append(queue, r.edges[export.Step.Node]...)
	}
	completed := completedNodesFromMetadata(export.Step.Metadata)
	if export.Step.Phase == gopact.StepCompleted ||
		export.Step.Phase == gopact.StepCanceled ||
		(export.Step.Phase == gopact.StepInterrupted && resume != nil) {
		completed[export.Step.Node] = struct{}{}
	}
	return state, queue, export.Step.Step, export.Step.IDs, completed, nil
}

func (r *Runnable[S]) resumeFromCheckpoint(ctx context.Context, checkpoint Checkpoint[S], resume *gopact.ResumeRequest, validator gopact.JSONSchemaValidator) (S, []string, int, gopact.RuntimeIDs, map[string]struct{}, error) {
	var zero S
	if checkpoint.Step < 0 {
		return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: checkpoint step must be non-negative, got %d", checkpoint.Step)
	}
	if checkpoint.Node == "" {
		return zero, nil, 0, gopact.RuntimeIDs{}, nil, errors.New("graph: checkpoint node is required")
	}

	phase := checkpointPhase(checkpoint)
	if phase != gopact.StepCompleted && phase != gopact.StepInterrupted && phase != gopact.StepCanceled {
		return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: checkpoint phase %q is not supported", phase)
	}
	if phase == gopact.StepInterrupted {
		if checkpoint.Pending == nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, errors.New("graph: interrupted checkpoint pending record is required")
		}
		if err := checkpoint.Pending.Validate(); err != nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: interrupted checkpoint pending record: %w", err)
		}
		if resume == nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, &gopact.InterruptError{Record: *checkpoint.Pending}
		}
		if err := resume.Validate(); err != nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume request: %w", err)
		}
		if resume.CheckpointID != "" && checkpoint.ID != "" && resume.CheckpointID != checkpoint.ID {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume checkpoint id %q does not match checkpoint %q", resume.CheckpointID, checkpoint.ID)
		}
		if resume.InterruptID != checkpoint.Pending.ID {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume interrupt id %q does not match pending interrupt %q", resume.InterruptID, checkpoint.Pending.ID)
		}
		ids := checkpointIDs(checkpoint)
		if resume.StepID != "" && resume.StepID != stepID(ids, checkpoint.Step) && resume.StepID != checkpoint.ID {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume step id %q does not match checkpoint step %q", resume.StepID, stepID(ids, checkpoint.Step))
		}
		if err := gopact.ValidateResumePayloadWithValidator(ctx, validator, *checkpoint.Pending, *resume); err != nil {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: resume payload: %w", err)
		}
	}
	if checkpoint.Node != Start && checkpoint.Node != End {
		if _, ok := r.nodes[checkpoint.Node]; !ok {
			return zero, nil, 0, gopact.RuntimeIDs{}, nil, fmt.Errorf("graph: checkpoint node %q is missing", checkpoint.Node)
		}
	}
	queue := append([]string(nil), checkpoint.Queue...)
	if len(queue) == 0 {
		queue = append(queue, r.edges[checkpoint.Node]...)
	}
	completed := completedNodesFromMetadata(checkpoint.Metadata)
	if phase == gopact.StepCompleted ||
		phase == gopact.StepCanceled ||
		(phase == gopact.StepInterrupted && resume != nil) {
		completed[checkpoint.Node] = struct{}{}
	}
	return checkpoint.State, queue, checkpoint.Step, checkpointIDs(checkpoint), completed, nil
}

func checkpointPhase[S any](checkpoint Checkpoint[S]) gopact.StepPhase {
	if checkpoint.Phase == "" {
		return gopact.StepCompleted
	}
	return checkpoint.Phase
}

func checkpointIDs[S any](checkpoint Checkpoint[S]) gopact.RuntimeIDs {
	ids := checkpoint.IDs
	if ids.ThreadID == "" {
		ids.ThreadID = checkpoint.ThreadID
	}
	return ids
}

func checkpointStepSnapshot[S any](checkpoint Checkpoint[S], ids gopact.RuntimeIDs) gopact.StepSnapshot {
	metadata := copyMap(checkpoint.Metadata)
	if checkpoint.ID != "" {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["checkpoint_id"] = checkpoint.ID
	}
	if checkpoint.ConfigVersion != "" {
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["config_version"] = checkpoint.ConfigVersion
	}

	var pending *gopact.InterruptRecord
	if checkpoint.Pending != nil {
		record := *checkpoint.Pending
		pending = &record
	}

	return gopact.StepSnapshot{
		ID:          stepID(ids, checkpoint.Step),
		Step:        checkpoint.Step,
		Node:        checkpoint.Node,
		Phase:       checkpointPhase(checkpoint),
		IDs:         ids,
		Output:      checkpoint.State,
		Queue:       append([]string(nil), checkpoint.Queue...),
		Pending:     pending,
		Effects:     copyEffects(checkpoint.Effects),
		CompletedAt: checkpoint.CreatedAt,
		Metadata:    metadata,
	}
}

func checkpointFromSnapshot[S any](snapshot gopact.StepSnapshot, state S) Checkpoint[S] {
	var pending *gopact.InterruptRecord
	if snapshot.Pending != nil {
		record := *snapshot.Pending
		pending = &record
	}
	return Checkpoint[S]{
		ID:        snapshot.ID,
		IDs:       snapshot.IDs,
		ThreadID:  snapshot.IDs.ThreadID,
		Step:      snapshot.Step,
		Node:      snapshot.Node,
		Phase:     snapshot.Phase,
		State:     state,
		Queue:     append([]string(nil), snapshot.Queue...),
		Pending:   pending,
		Effects:   copyEffects(snapshot.Effects),
		CreatedAt: time.Now(),
		Metadata:  copyMap(snapshot.Metadata),
	}
}

func copyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func completedNodesMetadata(completed map[string]struct{}) map[string]any {
	if len(completed) == 0 {
		return nil
	}
	nodes := make([]string, 0, len(completed))
	for node := range completed {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	return map[string]any{metadataCompletedNodes: nodes}
}

func completedNodesFromMetadata(metadata map[string]any) map[string]struct{} {
	completed := map[string]struct{}{}
	switch nodes := metadata[metadataCompletedNodes].(type) {
	case []string:
		for _, node := range nodes {
			if node != "" {
				completed[node] = struct{}{}
			}
		}
	case []any:
		for _, value := range nodes {
			node, ok := value.(string)
			if ok && node != "" {
				completed[node] = struct{}{}
			}
		}
	}
	return completed
}

func invokeNode[S any](_ context.Context, node NodeFunc[S], current S, nodeCtx *gopact.NodeContext, middlewares []gopact.NodeHandler) (S, error) {
	final := func(c *gopact.NodeContext) error {
		input, ok := c.Input.(S)
		if !ok {
			return fmt.Errorf("graph: node input type mismatch: got %T", c.Input)
		}

		output, err := node(c.Context, input)
		c.Output = output
		if err != nil {
			return err
		}
		return nil
	}

	handler := gopact.ComposeNodeHandler(final, middlewares...)
	if err := handler(nodeCtx); err != nil {
		return current, err
	}

	output, ok := nodeCtx.Output.(S)
	if !ok {
		return current, fmt.Errorf("graph: node output type mismatch: got %T", nodeCtx.Output)
	}
	return output, nil
}

func emit(yield func(gopact.Event, error) bool, event gopact.Event, err error) bool {
	if yield == nil {
		return true
	}
	return yield(event, err)
}

func graphEvent(eventType gopact.EventType, ids gopact.RuntimeIDs, node string, step int, snapshot *gopact.StepSnapshot, err error) gopact.Event {
	event := gopact.Event{
		Type:         eventType,
		IDs:          ids,
		RunID:        ids.RunID,
		ThreadID:     ids.ThreadID,
		Node:         node,
		Step:         step,
		StepSnapshot: snapshot,
		CreatedAt:    time.Now(),
		Err:          err,
	}
	return event
}

func attachEffectReplayPlan(event *gopact.Event, snapshot gopact.StepSnapshot) error {
	if event == nil || len(snapshot.Effects) == 0 {
		return nil
	}
	plan, err := gopact.PlanEffectReplay(snapshot)
	if err != nil {
		return err
	}
	if len(plan.Decisions) == 0 {
		return nil
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	event.Metadata[gopact.EventMetadataEffectReplayPlan] = plan
	return nil
}

func verifySnapshotArtifacts(ctx context.Context, verifier ArtifactVerifier, snapshot gopact.StepSnapshot) error {
	if verifier == nil {
		return nil
	}
	refs := snapshotArtifactRefs(snapshot)
	if len(refs) == 0 {
		return nil
	}
	return verifier.VerifyRefs(ctx, refs)
}

func snapshotArtifactRefs(snapshot gopact.StepSnapshot) []gopact.ArtifactRef {
	refs := copyArtifactRefs(snapshot.Artifacts)
	for _, effect := range snapshot.Effects {
		refs = append(refs, copyArtifactRefs(effect.Artifacts)...)
	}
	return refs
}

func stepSnapshot(step int, node string, ids gopact.RuntimeIDs, phase gopact.StepPhase, input any, output any, errText string, startedAt time.Time, completedAt time.Time) gopact.StepSnapshot {
	return gopact.StepSnapshot{
		ID:          stepID(ids, step),
		Step:        step,
		Node:        node,
		Phase:       phase,
		IDs:         ids,
		Input:       input,
		Output:      output,
		Error:       errText,
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}
}

func attachEffects(snapshot *gopact.StepSnapshot, effects []gopact.EffectRecord) {
	if snapshot == nil || len(effects) == 0 {
		return
	}
	snapshot.Effects = append(snapshot.Effects, copyEffects(effects)...)
}

func copyEffects(effects []gopact.EffectRecord) []gopact.EffectRecord {
	if len(effects) == 0 {
		return nil
	}
	out := make([]gopact.EffectRecord, len(effects))
	for i, effect := range effects {
		out[i] = effect
		out[i].DependsOn = append([]string(nil), effect.DependsOn...)
		out[i].Artifacts = copyArtifactRefs(effect.Artifacts)
		if effect.Sandbox != nil {
			sandbox := *effect.Sandbox
			sandbox.Command = append([]string(nil), effect.Sandbox.Command...)
			sandbox.Metadata = copyMap(effect.Sandbox.Metadata)
			out[i].Sandbox = &sandbox
		}
		out[i].Metadata = copyMap(effect.Metadata)
	}
	return out
}

func copyArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Metadata = copyMap(ref.Metadata)
	}
	return out
}

func stepID(ids gopact.RuntimeIDs, step int) string {
	if ids.RunID == "" {
		return fmt.Sprintf("step:%d", step)
	}
	return fmt.Sprintf("%s:%d", ids.RunID, step)
}
