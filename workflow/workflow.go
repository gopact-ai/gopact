// Package workflow provides typed workflow orchestration primitives.
package workflow

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

var (
	errNilWorkflow = errors.New("workflow: nil workflow")
	errNilCompiled = errors.New("workflow: nil compiled workflow")

	// ErrCheckpointExists reports a duplicate checkpoint create.
	ErrCheckpointExists = errors.New("workflow: checkpoint already exists")
	// ErrCheckpointNotFound reports a missing checkpoint.
	ErrCheckpointNotFound = errors.New("workflow: checkpoint not found")
	// ErrCheckpointConflict reports an optimistic checkpoint version conflict.
	ErrCheckpointConflict = errors.New("workflow: checkpoint version conflict")
	// ErrInvalidCheckpoint reports malformed checkpoint metadata or payload.
	ErrInvalidCheckpoint = errors.New("workflow: invalid checkpoint")
	// ErrCheckpointMismatch reports incompatible workflow or topology identity.
	ErrCheckpointMismatch = errors.New("workflow: checkpoint identity mismatch")
	// ErrCheckpointLeaseLost reports that another owner may execute the Run.
	ErrCheckpointLeaseLost = errors.New("workflow: checkpoint lease lost")
	// ErrHistoryLimitExceeded reports a high-level history read that exceeded its configured bound.
	ErrHistoryLimitExceeded = errors.New("workflow: history limit exceeded")
)

const (
	resumeExtensionKey                = "gopact.workflow.resume"
	controlStartExtensionKey          = "gopact.workflow.control_start"
	sourceAssociationExtensionKey     = "gopact.workflow.source_association"
	defaultCheckpointLeaseDuration    = time.Minute
	defaultCheckpointLeaseRenewEvery  = 20 * time.Second
	defaultMaxSteps                   = 1024
	defaultMaxParallelism             = 1
	initialActivationSequence         = 2
	checkpointSchemaVersion           = 2
	initialCorrelationEpoch           = 1
	maxWorkflowEventPayloadBytes      = 64 << 10
	maxWorkflowCheckpointPayloadBytes = 4 << 20
	journalReconcilePageSize          = 256
	maxGeneratedIDBytes               = 191
)

type resumeOptionConflict struct{}
type workflowRunOptionFunc func(*gopact.RunConfig)
type eventEmitterContextKey struct{}
type eventEmitter func(context.Context, gopact.Event) error
type childOptionsFactoryContextKey struct{}
type childOptionsFactory func() []gopact.RunOption
type runInfoContextKey struct{}

func (f workflowRunOptionFunc) ApplyRunOption(cfg *gopact.RunConfig) {
	f(cfg)
}

// Emit emits a workflow-scoped custom event on the current run.
func Emit(ctx context.Context, event gopact.Event) error {
	if event.DefinitionID != "" || event.DefinitionVersion != "" || event.SessionID != "" ||
		event.RunID != "" || event.NodeID != "" || event.ActivationID != "" || event.AttemptID != "" ||
		event.RevisionID != "" || event.ParentRunID != "" ||
		event.NodeExecutionVersion != 0 || event.ExecutionEpoch != 0 || event.SourceRevisionID != "" ||
		event.Sequence != 0 || !event.Timestamp.IsZero() ||
		event.Source != "" || event.Origin != "" {
		return errors.New("workflow: emitted event must not set runtime identity")
	}
	emit, ok := ctx.Value(eventEmitterContextKey{}).(eventEmitter)
	if !ok {
		return errors.New("workflow: event emitter is not available")
	}
	return emit(ctx, event)
}

func validateCustomEvent(types map[string]EventTypeValidator, event gopact.Event) error {
	if event.Type == "" {
		return errors.New("workflow: event type is required")
	}
	if len(event.Payload) > maxWorkflowEventPayloadBytes {
		return errors.New("workflow: event payload is too large")
	}
	if len(event.Payload) > 0 && !json.Valid(event.Payload) {
		return errors.New("workflow: event payload is invalid JSON")
	}
	validator := types[event.Type]
	if validator == nil {
		return nil
	}
	if err := validator(event); err != nil {
		return fmt.Errorf("workflow: validate event %q: %w", event.Type, err)
	}
	return nil
}

// Workflow event types.
const (
	EventWorkflowStarted        = "workflow.started"
	EventWorkflowResumed        = "workflow.resumed"
	EventWorkflowRetryStarted   = "workflow.retry_started"
	EventWorkflowJumpStarted    = "workflow.jump_started"
	EventWorkflowCompleted      = "workflow.completed"
	EventWorkflowFailed         = "workflow.failed"
	EventWorkflowCanceled       = "workflow.canceled"
	EventWorkflowTerminated     = "workflow.terminated"
	EventWorkflowInterrupted    = "workflow.interrupted"
	EventNodeStarted            = "node.started"
	EventNodeRetrying           = "node.retrying"
	EventNodeCompleted          = "node.completed"
	EventNodeCanceled           = "node.canceled"
	EventNodeSuperseded         = "node.superseded"
	EventNodeOutputCommitted    = "node.output_committed"
	EventNodeSkipped            = "node.skipped"
	EventNodeFailed             = "node.failed"
	EventGuardRejected          = "guard.rejected"
	EventGuardInterrupted       = "guard.interrupted"
	EventCheckpointLoaded       = "checkpoint.loaded"
	EventWorkflowCustomEvent    = "workflow.event"
	EventLifecycleHookStarted   = "lifecycle.hook_started"
	EventLifecycleHookCompleted = "lifecycle.hook_completed"
	EventLifecycleHookFailed    = "lifecycle.hook_failed"
)

// Workflow is a typed workflow builder.
type Workflow[I, O any] struct {
	compileMu                 sync.Mutex
	name                      string
	nodes                     map[string]runtimeNode
	edges                     map[string][]string
	predecessors              map[string][]string
	entry                     string
	exits                     map[string]struct{}
	duplicateNodes            map[string]struct{}
	store                     Store
	maxSteps                  int
	maxParallelism            int
	checkpointLeaseDuration   time.Duration
	checkpointLeaseRenewEvery time.Duration
	idGenerators              map[gopact.IDKind]gopact.IDGenerator
	beforeWorkflow            []LifecycleHook[WorkflowContext[I, O]]
	afterWorkflow             []LifecycleHook[WorkflowContext[I, O]]
	plugins                   []Plugin
	topologyVersion           string
	topologySet               bool
	contextKey                *workflowContextKey
	contextType               reflect.Type
	contextInit               func(any) (any, error)
	contextSet                bool
	contextTwice              bool
	checkpointTypes           []reflect.Type
	checkpointTypeErr         error
	compiled                  *compiled[I, O]
}

// RunInfo identifies the current Workflow execution without exposing scheduler state.
type RunInfo struct {
	SessionID    string
	RunID        string
	ParentRunID  string
	Depth        int
	NodeID       string
	ActivationID string
	Attempt      int
}

// IDKind identifies an automatically generated workflow identity.
type IDKind = gopact.IDKind

// IDGenerator creates globally unique IDs.
type IDGenerator = gopact.IDGenerator

// Automatically generated workflow identity kinds.
const (
	IDKindSession = gopact.IDKindSession
	IDKindRun     = gopact.IDKindRun
	IDKindOwner   = gopact.IDKindOwner
)

// RunInfoFromContext returns the current Workflow execution identity.
func RunInfoFromContext(ctx context.Context) RunInfo {
	if ctx == nil {
		return RunInfo{}
	}
	info, _ := ctx.Value(runInfoContextKey{}).(RunInfo)
	return info
}

// compiled is the immutable internal execution plan.
type compiled[I, O any] struct {
	name                      string
	nodes                     map[string]runtimeNode
	edges                     map[string][]string
	predecessors              map[string][]string
	entry                     string
	exits                     map[string]struct{}
	store                     Store
	maxSteps                  int
	maxParallelism            int
	checkpointLeaseDuration   time.Duration
	checkpointLeaseRenewEvery time.Duration
	idGenerators              map[gopact.IDKind]gopact.IDGenerator
	beforeWorkflow            []LifecycleHook[WorkflowContext[I, O]]
	afterWorkflow             []LifecycleHook[WorkflowContext[I, O]]
	eventTypes                map[string]EventTypeValidator
	nodeMiddlewares           []erasedNodeMiddleware
	routeMiddlewares          []erasedRouteMiddleware
	joinMiddlewares           []erasedJoinMiddleware
	eventSinkWrappers         []EventSinkWrapper
	topologyVersion           string
	backEdges                 map[topologyEdge]struct{}
	contextKey                *workflowContextKey
	contextType               reflect.Type
	contextInit               func(any) (any, error)
	activeMu                  sync.Mutex
	activeRuns                map[string]context.CancelCauseFunc
}

// Node is a typed workflow topology vertex.
type Node[I, O any] struct {
	name       string
	run        func(context.Context, I, ...gopact.RunOption) (O, error)
	join       func(context.Context, Inputs) (I, error)
	route      func(context.Context, O) (Dispatch, error)
	guards     []GuardBinding[I, O]
	before     []LifecycleHook[NodeContext[I, O]]
	after      []LifecycleHook[NodeContext[I, O]]
	joinTwice  bool
	routeTwice bool
	frozen     bool
	merge      bool
	invokable  bool
}

// Inputs is a read-only join contribution view.
type Inputs struct {
	contributions map[string][]any
}

// CheckpointStatus describes durable workflow run state.
type CheckpointStatus string

// ReplayStatus describes whether a checkpoint may be used as a fork boundary.
type ReplayStatus string

// Checkpoint statuses.
const (
	CheckpointRunning     CheckpointStatus = "running"
	CheckpointInterrupted CheckpointStatus = "interrupted"
	CheckpointCompleted   CheckpointStatus = "completed"
	CheckpointFailed      CheckpointStatus = "failed"
	CheckpointCanceled    CheckpointStatus = "canceled"
	CheckpointTerminated  CheckpointStatus = "terminated"
)

// Workflow checkpoint replay classifications.
const (
	ReplayUnknown ReplayStatus = "unknown"
	ReplaySafe    ReplayStatus = "safe"
	ReplayUnsafe  ReplayStatus = "unsafe"
)

// CheckpointRecord is a workflow-owned durable runtime snapshot.
// LeaseDuration is a transient TTL request and must not be persisted.
type CheckpointRecord struct {
	ID                string
	SessionID         string
	RunID             string
	SourceRunID       string
	SourceEventSeq    int64
	SourceRevisionID  string
	WorkflowName      string
	TopologyVersion   string
	SchemaVersion     int
	Version           int64
	Status            CheckpointStatus
	Payload           []byte
	ConfirmedSequence int64
	PendingSequence   int64
	ReplayStatus      ReplayStatus
	OwnerID           string
	LeaseExpiresAt    time.Time
	LeaseDuration     time.Duration `json:"-"`
	ClaimSequence     int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CheckpointLease identifies one Run ownership claim to renew.
// Duration is the preferred TTL; ExpiresAt remains the compatibility fallback.
type CheckpointLease struct {
	RunID         string
	OwnerID       string
	ClaimSequence int64
	ExpiresAt     time.Time
	Duration      time.Duration
}

// Checkpointer persists workflow checkpoint records.
// When LeaseDuration or Duration is positive, implementations derive expiry
// from their clock while holding the ownership lock. ExpiresAt remains the
// fallback for callers that do not yet supply a duration.
// Claim must atomically replace only an expired running or interrupted head.
// RenewLease must atomically extend only the current unexpired running claim
// when both OwnerID and ClaimSequence still match, and return
// ErrCheckpointLeaseLost otherwise. A stale renewal may be a no-op but must
// never shorten the current expiry.
// Save and Finish must fence writes against the current head's non-empty,
// unexpired ownership claim. A concurrent RenewLease must never be overwritten
// by an older snapshot, and Save must not trust a caller-supplied ExpiresAt to
// extend the same claim. Clearing OwnerID with the current ClaimSequence is an
// explicit release, interrupt, or terminal transition. An owner/claim mismatch
// or expired current lease must return ErrCheckpointLeaseLost before reporting
// a version conflict; a same-claim CAS conflict returns ErrCheckpointConflict.
type Checkpointer interface {
	Create(ctx context.Context, rec CheckpointRecord) error
	Load(ctx context.Context, runID string) (CheckpointRecord, error)
	Claim(ctx context.Context, candidate CheckpointRecord, version int64) error
	Save(ctx context.Context, rec CheckpointRecord, version int64) error
	Finish(ctx context.Context, rec CheckpointRecord, version int64) error
	RenewLease(ctx context.Context, lease CheckpointLease) error
}

// CheckpointHistoryRequest bounds an ordered workflow checkpoint history query.
type CheckpointHistoryRequest struct {
	RunID        string
	AfterVersion int64
	Limit        int
}

// CheckpointHistory exposes immutable checkpoint versions to snapshot projectors.
type CheckpointHistory interface {
	ListCheckpoints(context.Context, CheckpointHistoryRequest) ([]CheckpointRecord, error)
}

// Store is the authoritative durable workflow state and history boundary.
type Store interface {
	Checkpointer
	CheckpointHistory
	runlog.FencedLog
}

// ResumeRequest identifies a workflow run checkpoint to resume.
type ResumeRequest struct {
	RunID        string
	CheckpointID string
	Resolutions  []InterruptResolution
}

// InterruptResolution supplies a workflow-owned interrupt resolution by ref.
type InterruptResolution struct {
	InterruptID string
	PayloadRef  string
}

type endpoint interface {
	endpointName() string
	inputType() reflect.Type
	outputType() reflect.Type
}

type runtimeNode interface {
	endpointName() string
	inputType() reflect.Type
	outputType() reflect.Type
	runAny(context.Context, any, []erasedNodeMiddleware, ...gopact.RunOption) (nodeRunResult, error)
	joinAny(context.Context, Inputs) (any, error)
	routeAny(context.Context, any) (Dispatch, error)
	hasRoute() bool
	hasJoin() bool
	hasMerge() bool
	validateGuards() error
	validateLifecycle() error
	validateBindings() error
	topologyFacts() []string
	freeze()
}

// Dispatch describes downstream delivery selected by a source node.
type Dispatch struct {
	deliveries   []delivery
	stop         bool
	settle       SettlePolicy
	explicit     bool
	source       string
	nilSource    bool
	mixedSources bool
}

// SettlePolicy describes dispatch completion policy.
type SettlePolicy struct {
	mode   string
	quorum int
}

type delivery struct {
	target          string
	payload         any
	iter            func(context.Context) iter.Seq2[any, error]
	iterCheckpoint  func() any
	iterRestore     func(context.Context, any) iter.Seq2[any, error]
	iterErr         error
	useSourceOutput bool
}

// IterOption configures one EachIter dispatch without exposing scheduler state.
type IterOption[T any] interface {
	applyIterOption(*iterConfig[T])
}

type iterOptionFunc[T any] func(*iterConfig[T])

func (option iterOptionFunc[T]) applyIterOption(config *iterConfig[T]) {
	option(config)
}

type iterConfig[T any] struct {
	checkpoint func() any
	restore    func(context.Context, any) iter.Seq2[T, error]
	err        error
}

// BuildOption configures a workflow builder.
type BuildOption interface {
	applyBuildOption(*buildConfig)
}

type buildOptionFunc func(*buildConfig)

func (f buildOptionFunc) applyBuildOption(cfg *buildConfig) {
	f(cfg)
}

type buildConfig struct {
	maxSteps                  int
	maxParallelism            int
	store                     Store
	storeSet                  bool
	checkpointLeaseDuration   time.Duration
	checkpointLeaseRenewEvery time.Duration
	idGenerators              map[gopact.IDKind]gopact.IDGenerator
	plugins                   []Plugin
	topologyVersion           string
	topologySet               bool
	checkpointTypes           []reflect.Type
	checkpointTypeErr         error
}

// WithIDGenerator replaces the default UUID generator for one identity kind.
// Explicit IDs and per-run generators take precedence.
func WithIDGenerator(kind gopact.IDKind, generator gopact.IDGenerator) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		if cfg.idGenerators == nil {
			cfg.idGenerators = make(map[gopact.IDKind]gopact.IDGenerator)
		}
		cfg.idGenerators[kind] = generator
	})
}

// WithMaxSteps limits scheduler steps for one workflow run.
func WithMaxSteps(n int) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		cfg.maxSteps = n
	})
}

// WithMaxParallelism records the workflow scheduler parallelism limit.
func WithMaxParallelism(n int) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		cfg.maxParallelism = n
	})
}

// WithCheckpointLease configures Run ownership duration and renewal frequency.
// Both values must be positive, and renewEvery must be less than duration.
func WithCheckpointLease(duration, renewEvery time.Duration) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		cfg.checkpointLeaseDuration = duration
		cfg.checkpointLeaseRenewEvery = renewEvery
	})
}

// WithStore configures the authoritative durable workflow store.
func WithStore(store Store) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		cfg.store = store
		cfg.storeSet = true
	})
}

// WithTopologyVersion identifies workflow behavior not visible in the graph shape.
func WithTopologyVersion(version string) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		cfg.topologyVersion = version
		cfg.topologySet = true
	})
}

// WithCheckpointTypes registers dynamic concrete types that cannot be inferred
// from the workflow topology, such as values stored inside interface fields or
// iterator cursors. Each item may be a typed value or a reflect.Type. Because
// encoding/gob treats T and *T as one base type, a workflow cannot register both.
func WithCheckpointTypes(values ...any) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		for _, value := range values {
			typ, ok := value.(reflect.Type)
			if !ok {
				typ = reflect.TypeOf(value)
			}
			if typ == nil {
				cfg.checkpointTypeErr = errors.New("workflow: checkpoint type is nil")
				continue
			}
			cfg.checkpointTypes = append(cfg.checkpointTypes, typ)
		}
	})
}

// WithIterReplay configures a durable typed cursor for EachIter.
func WithIterReplay[T, C any](checkpoint func() C, restore func(context.Context, C) iter.Seq2[T, error]) IterOption[T] {
	return iterOptionFunc[T](func(cfg *iterConfig[T]) {
		if checkpoint == nil || restore == nil {
			cfg.err = errors.New("workflow: iterator checkpoint and restore are required")
			return
		}
		cfg.checkpoint = func() any { return checkpoint() }
		cfg.restore = func(ctx context.Context, cursor any) iter.Seq2[T, error] {
			typed, ok := cursor.(C)
			if !ok {
				return failedIter[T](fmt.Errorf("workflow: iterator cursor type %T does not match %s", cursor, typeOf[C]()))
			}
			return restore(ctx, typed)
		}
	})
}

func failedIter[T any](cause error) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) { yield(*new(T), cause) }
}

// WithResume resumes a workflow-owned checkpoint through compiled.Invoke.
func WithResume(req ResumeRequest) gopact.RunOption {
	return workflowRunOptionFunc(func(cfg *gopact.RunConfig) {
		cfg.ConstrainRunID(req.RunID)
		if cfg.Extensions == nil {
			cfg.Extensions = make(map[string]any, 1)
		}
		existing, ok := cfg.Extensions[resumeExtensionKey]
		if ok {
			if existingReq, ok := existing.(ResumeRequest); ok && reflect.DeepEqual(existingReq, req) {
				return
			}
			cfg.Extensions[resumeExtensionKey] = resumeOptionConflict{}
			return
		}
		cfg.Extensions[resumeExtensionKey] = req
	})
}

func workflowResumeRequest(cfg gopact.RunConfig) (ResumeRequest, bool, error) {
	if cfg.Extensions == nil {
		return ResumeRequest{}, false, nil
	}
	for key := range cfg.Extensions {
		if key != resumeExtensionKey && key != controlStartExtensionKey && key != sourceAssociationExtensionKey {
			return ResumeRequest{}, false, fmt.Errorf("workflow: unknown run extension %q", key)
		}
	}
	value, ok := cfg.Extensions[resumeExtensionKey]
	if !ok {
		return ResumeRequest{}, false, nil
	}
	req, ok := value.(ResumeRequest)
	if !ok {
		return ResumeRequest{}, false, errors.New("workflow: conflicting resume options")
	}
	return req, true, nil
}

func workflowSourceAssociation(cfg gopact.RunConfig) (runlog.Association, bool, error) {
	if cfg.Extensions == nil {
		return runlog.Association{}, false, nil
	}
	value, ok := cfg.Extensions[sourceAssociationExtensionKey]
	if !ok {
		return runlog.Association{}, false, nil
	}
	association, ok := value.(runlog.Association)
	if !ok || association.SourceRunID == "" || association.SourceEventSeq <= 0 {
		return runlog.Association{}, false, errors.New("workflow: invalid source association")
	}
	return association, true, nil
}

func workflowControlStart(cfg gopact.RunConfig) (controlStart, bool, error) {
	if cfg.Extensions == nil {
		return controlStart{}, false, nil
	}
	value, ok := cfg.Extensions[controlStartExtensionKey]
	if !ok {
		return controlStart{}, false, nil
	}
	start, ok := value.(controlStart)
	if !ok || len(start.payload) == 0 || start.sessionID == "" || start.association.SourceRunID == "" ||
		start.association.SourceEventSeq <= 0 {
		return controlStart{}, false, errors.New("workflow: invalid control start")
	}
	return start, true, nil
}

// SettleAll waits for all selected branches.
func SettleAll() SettlePolicy {
	return SettlePolicy{mode: "all"}
}

// SettleAny waits for any selected branch.
func SettleAny() SettlePolicy {
	return SettlePolicy{mode: "any"}
}

// SettleQuorum waits for m selected branches.
func SettleQuorum(m int) SettlePolicy {
	return SettlePolicy{mode: "quorum", quorum: m}
}

// New creates a typed workflow builder.
func New[I, O any](name string, opts ...BuildOption) *Workflow[I, O] {
	cfg := buildConfig{
		maxSteps: defaultMaxSteps, maxParallelism: defaultMaxParallelism,
		checkpointLeaseDuration:   defaultCheckpointLeaseDuration,
		checkpointLeaseRenewEvery: defaultCheckpointLeaseRenewEvery,
		idGenerators:              make(map[gopact.IDKind]gopact.IDGenerator),
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyBuildOption(&cfg)
		}
	}
	if !cfg.storeSet {
		cfg.store = NewMemoryStore()
	}
	return &Workflow[I, O]{
		name:                      name,
		nodes:                     make(map[string]runtimeNode),
		edges:                     make(map[string][]string),
		predecessors:              make(map[string][]string),
		exits:                     make(map[string]struct{}),
		duplicateNodes:            make(map[string]struct{}),
		store:                     cfg.store,
		maxSteps:                  cfg.maxSteps,
		maxParallelism:            cfg.maxParallelism,
		checkpointLeaseDuration:   cfg.checkpointLeaseDuration,
		checkpointLeaseRenewEvery: cfg.checkpointLeaseRenewEvery,
		idGenerators:              cloneIDGenerators(cfg.idGenerators),
		plugins:                   append([]Plugin(nil), cfg.plugins...),
		topologyVersion:           cfg.topologyVersion,
		topologySet:               cfg.topologySet,
		checkpointTypes:           append([]reflect.Type(nil), cfg.checkpointTypes...),
		checkpointTypeErr:         cfg.checkpointTypeErr,
	}
}

func (wf *Workflow[I, O]) assertMutable() {
	if wf != nil && wf.compiled != nil {
		panic("workflow already compiled")
	}
}

func (n *Node[I, O]) assertMutable() {
	if n != nil && n.frozen {
		panic("workflow already compiled")
	}
}

func (wf *Workflow[I, O]) registerNode(name string, node runtimeNode) {
	if wf == nil || node == nil {
		return
	}
	wf.assertMutable()
	if _, exists := wf.nodes[name]; exists {
		wf.duplicateNodes[name] = struct{}{}
	}
	wf.nodes[name] = node
}

// Join binds upstream contributions to this target node input.
func (n *Node[I, O]) Join(fn func(context.Context, Inputs) (I, error)) {
	if n == nil {
		return
	}
	n.assertMutable()
	if n.join != nil {
		n.joinTwice = true
	}
	n.join = fn
}

// Route binds downstream dispatch selection to this source node.
func (n *Node[I, O]) Route(fn func(context.Context, O) (Dispatch, error)) {
	if n == nil {
		return
	}
	n.assertMutable()
	if n.route != nil {
		n.routeTwice = true
	}
	n.route = fn
}

func (n *Node[I, O]) newDispatch() Dispatch {
	d := Dispatch{explicit: true}
	if n == nil {
		d.nilSource = true
		return d
	}
	d.source = n.endpointName()
	return d
}

// Stop stops downstream scheduling after this source node.
func (n *Node[I, O]) Stop() Dispatch {
	d := n.newDispatch()
	d.stop = true
	return d
}

// And combines dispatches without ordering semantics.
func (d Dispatch) And(next Dispatch) Dispatch {
	d.mergeSource(next)
	d.deliveries = append(d.deliveries, next.deliveries...)
	d.stop = d.stop || next.stop
	d.explicit = d.explicit || next.explicit
	return d
}

func (d *Dispatch) mergeSource(next Dispatch) {
	d.nilSource = d.nilSource || next.nilSource
	d.mixedSources = d.mixedSources || next.mixedSources
	if d.source == "" {
		d.source = next.source
		return
	}
	if next.source != "" && d.source != next.source {
		d.mixedSources = true
	}
}

func (d Dispatch) validateSource(source string) error {
	if d.nilSource {
		return errors.New("workflow: dispatch source is nil")
	}
	if d.mixedSources {
		return errors.New("workflow: dispatch mixes sources")
	}
	if d.source != "" && d.source != source {
		return fmt.Errorf("workflow: dispatch belongs to source %q, used by route %q", d.source, source)
	}
	return nil
}

// WithSettle sets this dispatch completion policy.
func (d Dispatch) WithSettle(policy SettlePolicy) Dispatch {
	d.settle = policy
	return d
}

func (wf *Workflow[I, O]) setEntry(target endpoint) {
	if wf == nil || target == nil {
		return
	}
	wf.assertMutable()
	wf.entry = target.endpointName()
}

func (wf *Workflow[I, O]) connect(source endpoint, target endpoint) {
	if wf == nil || source == nil || target == nil {
		return
	}
	wf.assertMutable()
	sourceName := source.endpointName()
	targetName := target.endpointName()
	wf.edges[sourceName] = appendStringOnce(wf.edges[sourceName], targetName)
	wf.predecessors[targetName] = appendStringOnce(wf.predecessors[targetName], sourceName)
}

func (wf *Workflow[I, O]) addExit(source endpoint) {
	if wf == nil || source == nil {
		return
	}
	wf.assertMutable()
	wf.exits[source.endpointName()] = struct{}{}
}

func (wf *Workflow[I, O]) compile() (*compiled[I, O], error) {
	if wf == nil {
		return nil, errNilWorkflow
	}
	wf.compileMu.Lock()
	defer wf.compileMu.Unlock()
	if wf.compiled != nil {
		return wf.compiled, nil
	}
	if wf.name == "" {
		return nil, errors.New("workflow: name is required")
	}
	if wf.maxSteps <= 0 {
		return nil, fmt.Errorf("workflow: max steps must be positive, got %d", wf.maxSteps)
	}
	if wf.maxParallelism <= 0 {
		return nil, fmt.Errorf("workflow: max parallelism must be positive, got %d", wf.maxParallelism)
	}
	if wf.checkpointLeaseDuration <= 0 {
		return nil, fmt.Errorf("workflow: checkpoint lease must be positive, got %s", wf.checkpointLeaseDuration)
	}
	if wf.checkpointLeaseRenewEvery <= 0 || wf.checkpointLeaseRenewEvery >= wf.checkpointLeaseDuration {
		return nil, fmt.Errorf(
			"workflow: checkpoint lease renewal %s must be positive and less than lease %s",
			wf.checkpointLeaseRenewEvery,
			wf.checkpointLeaseDuration,
		)
	}
	for kind, generator := range wf.idGenerators {
		if kind == "" {
			return nil, errors.New("workflow: id kind is empty")
		}
		if generator == nil {
			return nil, fmt.Errorf("workflow: %s id generator is nil", kind)
		}
	}
	if isNilStore(wf.store) {
		return nil, errors.New("workflow: store is nil")
	}
	if wf.topologySet && wf.topologyVersion == "" {
		return nil, errors.New("workflow: topology version is empty")
	}
	if wf.contextTwice {
		return nil, errors.New("workflow: context is defined more than once")
	}
	if wf.contextSet && wf.contextInit == nil {
		return nil, errors.New("workflow: context initializer is nil")
	}
	if wf.checkpointTypeErr != nil {
		return nil, wf.checkpointTypeErr
	}
	if len(wf.duplicateNodes) > 0 {
		for name := range wf.duplicateNodes {
			return nil, fmt.Errorf("workflow: duplicate node %q", name)
		}
	}
	if err := validateLifecycleHooks("workflow", "before", wf.beforeWorkflow); err != nil {
		return nil, err
	}
	if err := validateLifecycleHooks("workflow", "after", wf.afterWorkflow); err != nil {
		return nil, err
	}
	if wf.entry == "" {
		return nil, errors.New("workflow: entry is required")
	}
	if len(wf.exits) == 0 {
		return nil, errors.New("workflow: exit is required")
	}
	if _, ok := wf.nodes[wf.entry]; !ok {
		return nil, fmt.Errorf("workflow: entry node %q is missing", wf.entry)
	}
	for name, node := range wf.nodes {
		if err := wf.validateNode(name, node); err != nil {
			return nil, err
		}
	}
	for exit := range wf.exits {
		if err := wf.validateExit(exit); err != nil {
			return nil, err
		}
	}
	if !typeOf[I]().AssignableTo(wf.nodes[wf.entry].inputType()) {
		return nil, fmt.Errorf(
			"workflow: input %s is not assignable to entry %q input %s",
			typeOf[I](),
			wf.entry,
			wf.nodes[wf.entry].inputType(),
		)
	}
	for source, targets := range wf.edges {
		if err := wf.validateEdges(source, targets); err != nil {
			return nil, err
		}
	}
	checkpointTypes, err := workflowCheckpointTypes(wf)
	if err != nil {
		return nil, err
	}
	if err := registerWorkflowCheckpointTypes(checkpointTypes); err != nil {
		return nil, err
	}
	plugins, err := setupPlugins(context.Background(), wf.plugins)
	if err != nil {
		return nil, err
	}
	for _, node := range wf.nodes {
		node.freeze()
	}

	compiled := &compiled[I, O]{
		name:                      wf.name,
		nodes:                     copyNodes(wf.nodes),
		edges:                     copyEdges(wf.edges),
		predecessors:              copyEdges(wf.predecessors),
		entry:                     wf.entry,
		exits:                     copyExitSet(wf.exits),
		store:                     wf.store,
		maxSteps:                  wf.maxSteps,
		maxParallelism:            wf.maxParallelism,
		checkpointLeaseDuration:   wf.checkpointLeaseDuration,
		checkpointLeaseRenewEvery: wf.checkpointLeaseRenewEvery,
		idGenerators:              cloneIDGenerators(wf.idGenerators),
		beforeWorkflow:            append([]LifecycleHook[WorkflowContext[I, O]](nil), wf.beforeWorkflow...),
		afterWorkflow:             append([]LifecycleHook[WorkflowContext[I, O]](nil), wf.afterWorkflow...),
		eventTypes:                plugins.eventTypes,
		nodeMiddlewares:           plugins.nodeMiddlewares,
		routeMiddlewares:          plugins.routeMiddlewares,
		joinMiddlewares:           plugins.joinMiddlewares,
		eventSinkWrappers:         plugins.eventSinkWrappers,
		topologyVersion:           wf.compiledTopologyVersion(plugins),
		backEdges:                 findTopologyBackEdges(wf.entry, wf.edges),
		contextKey:                wf.contextKey,
		contextType:               wf.contextType,
		contextInit:               wf.contextInit,
		activeRuns:                make(map[string]context.CancelCauseFunc),
	}
	wf.compiled = compiled
	return compiled, nil
}

func isNilStore(store Store) bool {
	if store == nil {
		return true
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Validate checks and freezes the workflow definition without executing it.
func (wf *Workflow[I, O]) Validate() error {
	_, err := wf.compile()
	return err
}

// Invoke validates, fixes, and executes the workflow.
func (wf *Workflow[I, O]) Invoke(ctx context.Context, input I, opts ...gopact.RunOption) (O, error) {
	compiled, err := wf.compile()
	if err != nil {
		var zero O
		return zero, err
	}
	return compiled.Invoke(ctx, input, opts...)
}

// InvokeStream validates, fixes, and streams workflow outputs.
func (wf *Workflow[I, O]) InvokeStream(ctx context.Context, input I, opts ...gopact.RunOption) iter.Seq2[O, error] {
	return func(yield func(O, error) bool) {
		compiled, err := wf.compile()
		if err != nil {
			var zero O
			yield(zero, err)
			return
		}
		for output, invokeErr := range compiled.InvokeStream(ctx, input, opts...) {
			if !yield(output, invokeErr) {
				return
			}
		}
	}
}

func (wf *Workflow[I, O]) validateNode(name string, node runtimeNode) error {
	if err := node.validateBindings(); err != nil {
		return err
	}
	if err := node.validateGuards(); err != nil {
		return err
	}
	if err := node.validateLifecycle(); err != nil {
		return err
	}
	if node.hasMerge() {
		if name == wf.entry {
			return fmt.Errorf("workflow: merge node %q cannot be entry", name)
		}
		if len(wf.predecessors[name]) == 0 {
			return fmt.Errorf("workflow: merge node %q requires at least one input edge", name)
		}
	}
	if len(wf.predecessors[name]) > 1 && !node.hasJoin() && !node.hasMerge() {
		return fmt.Errorf("workflow: multi-input node %q requires join or merge", name)
	}
	return nil
}

func (wf *Workflow[I, O]) validateExit(exit string) error {
	node, ok := wf.nodes[exit]
	if !ok {
		return fmt.Errorf("workflow: exit node %q is missing", exit)
	}
	if !node.outputType().AssignableTo(typeOf[O]()) {
		return fmt.Errorf("workflow: exit node %q output %s is not assignable to workflow output %s", exit, node.outputType(), typeOf[O]())
	}
	return nil
}

func (wf *Workflow[I, O]) validateEdges(source string, targets []string) error {
	sourceNode, ok := wf.nodes[source]
	if !ok {
		return fmt.Errorf("workflow: source node %q is missing", source)
	}
	for _, target := range targets {
		if err := wf.validateEdge(source, target, sourceNode); err != nil {
			return err
		}
	}
	return nil
}

func (wf *Workflow[I, O]) validateEdge(source, target string, sourceNode runtimeNode) error {
	targetNode, ok := wf.nodes[target]
	if !ok {
		return fmt.Errorf("workflow: target node %q is missing", target)
	}
	usesJoinInput := targetNode.hasJoin() || targetNode.inputType() == typeOf[Inputs]()
	usesRoute := sourceNode.hasRoute()
	if !usesJoinInput && !usesRoute && !sourceNode.outputType().AssignableTo(targetNode.inputType()) {
		return fmt.Errorf("workflow: edge %q -> %q output %s is not assignable to input %s", source, target, sourceNode.outputType(), targetNode.inputType())
	}
	return nil
}

// Invoke executes the workflow and returns exactly one committed output.
func (c *compiled[I, O]) Invoke(ctx context.Context, input I, opts ...gopact.RunOption) (O, error) {
	var zero O
	outputs, err := c.invokeAll(ctx, input, nil, opts...)
	if err != nil {
		return zero, err
	}
	if len(outputs) != 1 {
		return zero, fmt.Errorf("workflow: invoke committed %d outputs, want 1", len(outputs))
	}
	return outputs[0], nil
}

// InvokeStream executes the workflow and streams committed outputs.
func (c *compiled[I, O]) InvokeStream(ctx context.Context, input I, opts ...gopact.RunOption) iter.Seq2[O, error] {
	return func(yield func(O, error) bool) {
		stopped := false
		sink := outputSink[O](func(output O) bool {
			keep := yield(output, nil)
			stopped = !keep
			return keep
		})
		_, err := c.invokeAll(ctx, input, sink, opts...)
		if err != nil && !stopped {
			var zero O
			yield(zero, err)
		}
	}
}

type outputSink[O any] func(O) bool

type workflowExecution[I, O any] struct {
	compiled          *compiled[I, O]
	ctx               context.Context
	input             I
	sessionID         string
	runID             string
	parentRunID       string
	ownerID           string
	depth             int
	replaySinks       []gopact.EventSink
	childSinks        []gopact.EventSink
	idGenerators      map[gopact.IDKind]gopact.IDGenerator
	sequence          int64
	eventCursor       int64
	replayStatus      ReplayStatus
	executionEpoch    int64
	controlOrigin     string
	sourceRevision    string
	sourceRunID       string
	sourceEventSeq    int64
	state             runState
	outputs           []O
	outputSink        outputSink[O]
	outputIndex       int
	step              int
	checkpoint        CheckpointRecord
	cancel            context.CancelCauseFunc
	eventMu           sync.Mutex
	sinkFailure       error
	leaseHeartbeat    *checkpointLeaseHeartbeat
	leaseCtx          context.Context
	leaseCancel       context.CancelCauseFunc
	interrupts        []checkpointInterrupt
	interruptProgress *checkpointInterruptProgress
}

type checkpointLeaseHeartbeat struct {
	cancel context.CancelFunc
	done   <-chan error
	once   sync.Once
	err    error
}

func (heartbeat *checkpointLeaseHeartbeat) stop() error {
	if heartbeat == nil {
		return nil
	}
	heartbeat.once.Do(func() {
		heartbeat.cancel()
		heartbeat.err = <-heartbeat.done
	})
	return heartbeat.err
}

func (execution *workflowExecution[I, O]) startCheckpointLeaseHeartbeat() {
	record := execution.checkpoint
	if execution.compiled.store == nil || record.ID == "" || record.Status != CheckpointRunning ||
		record.OwnerID == "" || record.ClaimSequence <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(execution.ctx))
	execution.leaseCtx, execution.leaseCancel = context.WithCancelCause(context.WithoutCancel(execution.ctx))
	done := make(chan error, 1)
	execution.leaseHeartbeat = &checkpointLeaseHeartbeat{cancel: cancel, done: done}
	lease := CheckpointLease{
		RunID: record.RunID, OwnerID: record.OwnerID, ClaimSequence: record.ClaimSequence,
		Duration: execution.compiled.checkpointLeaseDuration,
	}
	go func() {
		done <- execution.runCheckpointLeaseHeartbeat(ctx, lease)
	}()
}

func (execution *workflowExecution[I, O]) runCheckpointLeaseHeartbeat(ctx context.Context, lease CheckpointLease) error {
	ticker := time.NewTicker(execution.compiled.checkpointLeaseRenewEvery)
	defer ticker.Stop()
	for {
		stopped, err := execution.renewCheckpointLeaseOnTick(ctx, ticker.C, &lease)
		if stopped {
			return err
		}
	}
}

func (execution *workflowExecution[I, O]) renewCheckpointLeaseOnTick(ctx context.Context, ticks <-chan time.Time, lease *CheckpointLease) (bool, error) {
	select {
	case <-ctx.Done():
		return true, nil
	case <-ticks:
		err := execution.renewCheckpointLease(ctx, lease)
		return err != nil, err
	}
}

func (execution *workflowExecution[I, O]) renewCheckpointLease(ctx context.Context, lease *CheckpointLease) error {
	lease.Duration = execution.compiled.checkpointLeaseDuration
	lease.ExpiresAt = time.Now().Add(execution.compiled.checkpointLeaseDuration)
	renewCtx, cancel := context.WithTimeout(ctx, execution.compiled.checkpointLeaseRenewEvery)
	err := execution.compiled.store.RenewLease(renewCtx, *lease)
	cancel()
	if err == nil || ctx.Err() != nil {
		return nil
	}
	cause := markCheckpointLeaseError(
		fmt.Errorf("%w: renew checkpoint lease: %w", ErrCheckpointLeaseLost, err),
		CheckpointRecord{RunID: lease.RunID, OwnerID: lease.OwnerID, ClaimSequence: lease.ClaimSequence},
	)
	execution.cancel(errors.Join(context.Canceled, cause))
	if execution.leaseCancel != nil {
		execution.leaseCancel(cause)
	}
	return cause
}

func (execution *workflowExecution[I, O]) finalizationContext() context.Context {
	if execution.leaseCtx != nil {
		return execution.leaseCtx
	}
	return context.WithoutCancel(execution.ctx)
}

func (execution *workflowExecution[I, O]) stopCheckpointLeaseHeartbeat() error {
	return execution.leaseHeartbeat.stop()
}

func defaultIDGenerator(kind gopact.IDKind) (string, error) {
	prefix := ""
	switch kind {
	case IDKindSession:
		prefix = "session-"
	case IDKindRun:
		prefix = "workflow-"
	case IDKindOwner:
		prefix = "workflow-owner-"
	default:
		return "", fmt.Errorf("unknown id kind %q", kind)
	}
	return prefix + uuid.New().String(), nil
}

func generateID(generator gopact.IDGenerator, kind gopact.IDKind) (string, error) {
	id, err := generator()
	if err != nil {
		return "", fmt.Errorf("workflow: generate %s id: %w", kind, err)
	}
	switch {
	case id == "":
		err = errors.New("id is required")
	case len(id) > maxGeneratedIDBytes:
		err = fmt.Errorf("id exceeds %d bytes", maxGeneratedIDBytes)
	case !utf8.ValidString(id):
		err = errors.New("id must be valid UTF-8")
	case strings.IndexByte(id, 0) >= 0:
		err = errors.New("id must not contain NUL")
	case strings.HasSuffix(id, " "):
		err = errors.New("id must not end with a space")
	}
	if err != nil {
		return "", fmt.Errorf("workflow: generate %s id: %w", kind, err)
	}
	return id, nil
}

func cloneIDGenerators(source map[gopact.IDKind]gopact.IDGenerator) map[gopact.IDKind]gopact.IDGenerator {
	cloned := make(map[gopact.IDKind]gopact.IDGenerator, len(source))
	for kind, generator := range source {
		cloned[kind] = generator
	}
	return cloned
}

func (c *compiled[I, O]) idGenerator(config gopact.RunConfig, kind gopact.IDKind) gopact.IDGenerator {
	if generator, ok := config.IDGenerator(kind); ok {
		return generator
	}
	if generator := c.idGenerators[kind]; generator != nil {
		return generator
	}
	return func() (string, error) { return defaultIDGenerator(kind) }
}

func (c *compiled[I, O]) invokeAll(ctx context.Context, input I, sink outputSink[O], opts ...gopact.RunOption) (outputs []O, err error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if c == nil {
		return nil, errNilCompiled
	}
	cfg := gopact.ResolveRunOptions(opts...)
	if err := cfg.RunConfigError(); err != nil {
		return nil, err
	}
	resumeReq, resume, err := workflowResumeRequest(cfg)
	if err != nil {
		return nil, err
	}
	control, controlled, err := workflowControlStart(cfg)
	if err != nil {
		return nil, err
	}
	if resume && controlled {
		return nil, errors.New("workflow: resume and control start are mutually exclusive")
	}
	association, associated, err := workflowSourceAssociation(cfg)
	if err != nil {
		return nil, err
	}
	if resume && resumeReq.RunID == "" {
		return nil, errors.New("workflow: resume run id is required")
	}
	sessionID := cfg.SessionID
	if !resume && sessionID == "" {
		sessionID, err = generateID(c.idGenerator(cfg, IDKindSession), IDKindSession)
		if err != nil {
			return nil, err
		}
	}
	runID := cfg.RunID
	if runID == "" {
		runID, err = generateID(c.idGenerator(cfg, IDKindRun), IDKindRun)
		if err != nil {
			return nil, err
		}
	}
	if associated && runID == association.SourceRunID {
		return nil, errors.New("workflow: control cannot reuse source run id")
	}
	ownerID, err := generateID(c.idGenerator(cfg, IDKindOwner), IDKindOwner)
	if err != nil {
		return nil, err
	}
	parentRunID := ""
	depth := 1
	if cfg.Lineage != (gopact.RunLineage{}) {
		parentRunID = cfg.Lineage.ParentRunID
		depth = cfg.Lineage.Depth
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	if err := c.registerRun(runID, cancel); err != nil {
		cancel(nil)
		return nil, err
	}
	defer c.unregisterRun(runID)
	defer cancel(nil)
	configuredSinks := cfg.EventSinks
	if associated {
		configuredSinks = associateEventSinks(configuredSinks, association)
	}
	replaySinks := applyEventSinkWrappers(configuredSinks, c.eventSinkWrappers)
	execution := workflowExecution[I, O]{
		compiled: c, ctx: runCtx, input: input,
		sessionID: sessionID, runID: runID, parentRunID: parentRunID,
		ownerID: ownerID, depth: depth,
		replaySinks: replaySinks,
		childSinks:  append([]gopact.EventSink(nil), cfg.EventSinks...), idGenerators: cfg.IDGenerators(), step: 1, cancel: cancel,
		outputSink: sink, replayStatus: ReplayUnknown, executionEpoch: 1, controlOrigin: "natural",
	}
	defer execution.stopLiveIterators()
	execution.ctx = context.WithValue(execution.ctx, eventEmitterContextKey{}, eventEmitter(execution.emitCustom))
	if !resume && !controlled {
		workflowCtx := WorkflowContext[I, O]{ctx: execution.ctx, Input: input}
		if err := runLifecycleHooks(c.beforeWorkflow, &workflowCtx); err != nil {
			return nil, fmt.Errorf("workflow: before workflow: %w", err)
		}
		execution.input = workflowCtx.Input
	}
	var contextValue any
	if !resume && !controlled {
		contextValue, err = c.initialContext(execution.input)
		if err != nil {
			return nil, err
		}
	}
	correlation := CorrelationKey{ID: runID, Epoch: initialCorrelationEpoch}
	execution.state = runState{
		queue:           []activation{{id: "act-1", node: c.entry, input: execution.input, correlation: correlation}},
		activations:     map[string]*activationRecord{},
		nextActSeq:      initialActivationSequence,
		scheduled:       map[string]int{c.entry: 1},
		completed:       map[string]int{},
		nodeVersions:    map[string]int64{},
		buckets:         map[joinBucketKey]*joinBucket{},
		correlations:    map[CorrelationKey]map[string]int{correlation: {c.entry: 1}},
		sourceSets:      map[string]*sourceSet{},
		nextSetSeq:      1,
		iterSources:     map[string]*iterSource{},
		liveIters:       map[string]*liveIterator{},
		nextIterSeq:     1,
		workflowContext: contextValue,
		hasContext:      !resume,
		contextRevision: 1,
	}
	execution.state.trackActivation(execution.state.queue[0])
	if controlled {
		payload, decodeErr := decodeCheckpointPayload[O](control.payload)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if identityErr := c.validateCheckpointIdentity(payload); identityErr != nil {
			return nil, identityErr
		}
		execution.state = payload.state()
		execution.outputs = append([]O(nil), payload.Outputs...)
		execution.step = payload.NextStep
		if execution.step <= 0 {
			execution.step = 1
		}
		execution.controlOrigin = payload.ControlOrigin
		execution.sourceRevision = payload.SourceRevisionID
	}
	if associated {
		execution.sourceRunID = association.SourceRunID
		execution.sourceEventSeq = association.SourceEventSeq
	}
	checkpoint, err := c.prepareCheckpoint(execution.ctx, checkpointPrepareRequest[O]{
		SessionID:        sessionID,
		RunID:            runID,
		OwnerID:          execution.ownerID,
		Resume:           resumeReq,
		IsResume:         resume,
		State:            execution.state,
		Outputs:          execution.outputs,
		NextStep:         execution.step,
		ControlOrigin:    execution.controlOrigin,
		SourceRevisionID: execution.sourceRevision,
		Association:      association,
		Associated:       associated,
	})
	if err != nil {
		return nil, err
	}
	if resume {
		execution.sessionID = checkpoint.SessionID
	}
	execution.checkpoint = checkpoint
	execution.startCheckpointLeaseHeartbeat()
	defer func() {
		err = errors.Join(err, execution.stopCheckpointLeaseHeartbeat())
	}()
	defer func() {
		if err != nil && execution.eventError() != nil {
			err = errors.Join(err, execution.releaseCheckpointLease())
		}
	}()
	if resume {
		complete, err := execution.resumeRun()
		if err != nil {
			return nil, err
		}
		if complete {
			return execution.outputs, nil
		}
	}
	if err := execution.startAttempt(resume); err != nil {
		return nil, err
	}
	if resume {
		if err := execution.reconcileSourceSets(); err != nil {
			return nil, execution.handleError(err)
		}
	}
	for execution.state.hasWork() {
		if err := execution.fillIterQueue(); err != nil {
			return nil, execution.handleError(err)
		}
		if len(execution.state.queue) == 0 {
			continue
		}
		if err := execution.advanceBatch(); err != nil {
			return nil, execution.handleError(err)
		}
	}
	if err := execution.finish(); err != nil {
		return nil, err
	}
	return execution.outputs, nil
}

func (c *compiled[I, O]) initialContext(input I) (any, error) {
	if c.contextInit == nil {
		return input, nil
	}
	return c.contextInit(input)
}

func (execution *workflowExecution[I, O]) releaseCheckpointLease() error {
	if err := execution.stopCheckpointLeaseHeartbeat(); err != nil {
		return err
	}
	record := execution.checkpoint
	if execution.compiled.store == nil || record.ID == "" || checkpointTerminal(record.Status) {
		return nil
	}
	payload, err := decodeCheckpointPayload[O](record.Payload)
	if err != nil {
		return err
	}
	if record.OwnerID == "" && record.LeaseExpiresAt.IsZero() && record.LeaseDuration == 0 &&
		payload.OwnerID == "" && payload.LeaseExpiresAt.IsZero() {
		return nil
	}
	meta := payload.meta()
	meta.OwnerID = ""
	meta.LeaseExpiresAt = time.Time{}
	encoded, err := encodeCheckpointPayloadWithMeta(payload.state(), payload.Outputs, payload.NextStep, meta)
	if err != nil {
		return err
	}
	record.Payload = encoded
	record.OwnerID = ""
	record.LeaseExpiresAt = time.Time{}
	record.LeaseDuration = 0
	record.UpdatedAt = time.Now()
	if err := execution.compiled.store.Save(context.WithoutCancel(execution.ctx), record, record.Version); err != nil {
		return err
	}
	record.Version++
	execution.checkpoint = record
	return nil
}

func (execution *workflowExecution[I, O]) resumeRun() (bool, error) {
	complete, err := execution.restore()
	if err != nil {
		return false, err
	}
	if !complete {
		if err := execution.bindIterSources(); err != nil {
			return false, err
		}
	}
	if err := execution.flushOutputs(); err != nil {
		if complete {
			return true, err
		}
		return false, execution.handleError(err)
	}
	return complete, nil
}

type preservedExecutionError struct{ cause error }

func (err preservedExecutionError) Error() string { return err.cause.Error() }
func (err preservedExecutionError) Unwrap() error { return err.cause }

func (execution *workflowExecution[I, O]) emitCustom(ctx context.Context, event gopact.Event) error {
	if err := validateCustomEvent(execution.compiled.eventTypes, event); err != nil {
		return err
	}
	return execution.emit(ctx, event)
}

func (execution *workflowExecution[I, O]) emitEvent(event gopact.Event) error {
	return execution.emit(execution.ctx, event)
}

func (execution *workflowExecution[I, O]) emit(ctx context.Context, event gopact.Event) error {
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	return execution.commitObservedEventLocked(ctx, event)
}

func (execution *workflowExecution[I, O]) commitObservedEventLocked(ctx context.Context, event gopact.Event) error {
	if execution.checkpoint.ID == "" {
		return execution.emitLocked(ctx, event)
	}
	if execution.sinkFailure != nil {
		return execution.sinkFailure
	}
	if cause := context.Cause(execution.ctx); execution.matchesCheckpointLeaseError(cause) {
		return cause
	}
	return execution.emitLocked(ctx, event)
}

func (execution *workflowExecution[I, O]) appendFencedEvent(ctx context.Context, event gopact.Event) error {
	return execution.appendFencedEventWithObserver(ctx, ctx, event)
}

func (execution *workflowExecution[I, O]) appendFencedEventWithObserver(storeCtx, observerCtx context.Context, event gopact.Event) error {
	appendCtx := eventSinkContext{Context: context.WithoutCancel(storeCtx)}
	record := runlog.RecordFromEvent(event)
	record.SourceRunID = execution.sourceRunID
	record.SourceEventSeq = execution.sourceEventSeq
	err := execution.compiled.store.AppendFenced(appendCtx, record, runlog.Fence{
		OwnerID: execution.checkpoint.OwnerID, ClaimSequence: execution.checkpoint.ClaimSequence,
	})
	if errors.Is(err, ErrCheckpointLeaseLost) {
		leaseError := markCheckpointLeaseError(err, execution.checkpoint)
		execution.cancel(errors.Join(context.Canceled, leaseError))
		return leaseError
	}
	if err != nil {
		return execution.recordSinkFailure(err)
	}
	if err := (eventDelivery{event: event}).emit(observerCtx, execution.replaySinks); err != nil {
		return execution.recordSinkFailure(err)
	}
	execution.eventCursor = execution.sequence
	return nil
}

func (execution *workflowExecution[I, O]) emitLocked(ctx context.Context, event gopact.Event) error {
	return execution.emitLockedWithContexts(ctx, ctx, event)
}

func (execution *workflowExecution[I, O]) emitLockedWithContexts(storeCtx, observerCtx context.Context, event gopact.Event) error {
	if execution.sinkFailure != nil {
		return execution.sinkFailure
	}
	execution.sequence++
	event = execution.runtimeEvent(event, execution.sequence)
	if execution.checkpoint.ID != "" {
		return execution.appendFencedEventWithObserver(storeCtx, observerCtx, event)
	}
	record := runlog.RecordFromEvent(event)
	record.SourceRunID = execution.sourceRunID
	record.SourceEventSeq = execution.sourceEventSeq
	if err := execution.compiled.store.Append(context.WithoutCancel(storeCtx), record); err != nil {
		return execution.recordSinkFailure(err)
	}
	delivery := eventDelivery{event: event}
	if err := delivery.emit(observerCtx, execution.replaySinks); err != nil {
		return execution.recordSinkFailure(err)
	}
	execution.eventCursor = execution.sequence
	return nil
}

func (execution *workflowExecution[I, O]) recordSinkFailure(err error) error {
	execution.sinkFailure = err
	execution.cancel(err)
	return err
}

func (execution *workflowExecution[I, O]) runtimeEvent(event gopact.Event, sequence int64) gopact.Event {
	if event.RunID == "" {
		if event.ActivationID != "" {
			event.ActivationID = execution.runID + "/" + event.ActivationID
		}
		if event.AttemptID != "" {
			event.AttemptID = execution.runID + "/" + event.AttemptID
		}
	}
	event.DefinitionID = execution.compiled.name
	event.DefinitionVersion = execution.compiled.topologyVersion
	event.SessionID = execution.sessionID
	event.RunID = execution.runID
	event.ParentRunID = execution.parentRunID
	event.ExecutionEpoch = execution.executionEpoch
	event.SourceRevisionID = execution.sourceRevision
	event.Sequence = sequence
	if event.RevisionID == "" {
		event.RevisionID = fmt.Sprintf("%s/revision-%d", execution.runID, sequence)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = "workflow"
	}
	if event.Origin == "" {
		event.Origin = execution.controlOrigin
		if event.Origin == "" {
			event.Origin = "runtime"
		}
	}
	return event
}

func (execution *workflowExecution[I, O]) eventError() error {
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	return execution.sinkFailure
}

func (execution *workflowExecution[I, O]) writeRequest(nextStep int, pending *gopact.Event) checkpointWriteRequest[O] {
	return checkpointWriteRequest[O]{
		Record: execution.checkpoint, State: execution.state, Outputs: execution.outputs,
		NextStep: nextStep, EventCursor: execution.eventCursor, PendingEvent: pending, ReplayStatus: execution.replayStatus,
		PendingInterrupts: execution.interrupts, InterruptProgress: execution.interruptProgress,
	}
}

func (execution *workflowExecution[I, O]) restore() (bool, error) {
	payload, err := decodeCheckpointPayload[O](execution.checkpoint.Payload)
	if err != nil {
		return false, err
	}
	if err := validateCheckpointInterruptProgress(execution.checkpoint, payload); err != nil {
		return false, err
	}
	execution.applyResumePayload(payload)
	status := execution.checkpoint.Status
	if payload.PendingEvent != nil {
		if err := execution.replayPending(status, payload.PendingTerm, *payload.PendingEvent); err != nil {
			return false, err
		}
		status = execution.checkpoint.Status
	}
	complete, err := resumedCheckpointResult(status)
	if err != nil || complete {
		return complete, err
	}
	if err := execution.reconcileJournal(); err != nil {
		return false, err
	}
	if execution.interruptProgress != nil {
		if err := execution.continueInterruptBatch(); err != nil {
			return false, err
		}
		return false, execution.interruptError()
	}
	return false, execution.emitEvent(gopact.Event{Type: EventCheckpointLoaded})
}

func validateCheckpointInterruptProgress[O any](record CheckpointRecord, payload checkpointPayload[O]) error {
	progress := payload.InterruptProgress
	if progress == nil {
		if (payload.PendingEvent != nil && interruptEventType(payload.PendingEvent.Type)) ||
			(record.Status == CheckpointRunning && len(payload.PendingInterrupts) > 0) {
			return fmt.Errorf("%w: checkpoint interrupt progress is missing", ErrInvalidCheckpoint)
		}
		return nil
	}
	if record.Status != CheckpointRunning || len(payload.PendingInterrupts) == 0 ||
		len(progress.Events) != len(payload.PendingInterrupts)+1 ||
		progress.Next < 0 || progress.Next > len(progress.Events) {
		return fmt.Errorf("%w: checkpoint interrupt progress is inconsistent", ErrInvalidCheckpoint)
	}
	for index, event := range progress.Events {
		if !checkpointInterruptEventIdentityMatches(record, payload, event) ||
			event.Sequence != progress.Events[0].Sequence+int64(index) {
			return fmt.Errorf("%w: checkpoint interrupt event sequence is inconsistent", ErrInvalidCheckpoint)
		}
		wantType := EventGuardInterrupted
		if index == len(progress.Events)-1 {
			wantType = EventWorkflowInterrupted
		}
		if event.Type != wantType {
			return fmt.Errorf("%w: checkpoint interrupt event type is inconsistent", ErrInvalidCheckpoint)
		}
		if index < len(payload.PendingInterrupts) && !checkpointInterruptRequestMatches(event, payload.PendingInterrupts[index].Request) {
			return fmt.Errorf("%w: checkpoint interrupt request is inconsistent", ErrInvalidCheckpoint)
		}
		if index == len(progress.Events)-1 && !checkpointWorkflowInterruptEventMatches(event, payload.PendingInterrupts[0].Request) {
			return fmt.Errorf("%w: checkpoint workflow interrupt event is inconsistent", ErrInvalidCheckpoint)
		}
	}
	confirmed := progress.Events[0].Sequence + int64(progress.Next) - 1
	if payload.EventCursor != confirmed || record.ConfirmedSequence != confirmed {
		return fmt.Errorf("%w: checkpoint interrupt cursor is inconsistent", ErrInvalidCheckpoint)
	}
	if payload.PendingEvent == nil {
		if progress.Next == 0 || record.PendingSequence != 0 {
			return fmt.Errorf("%w: checkpoint pending interrupt sequence is inconsistent", ErrInvalidCheckpoint)
		}
		return nil
	}
	if progress.Next >= len(progress.Events) || record.PendingSequence != payload.PendingEvent.Sequence ||
		!reflect.DeepEqual(*payload.PendingEvent, progress.Events[progress.Next]) {
		return fmt.Errorf("%w: checkpoint pending interrupt event is inconsistent", ErrInvalidCheckpoint)
	}
	return nil
}

func checkpointInterruptEventIdentityMatches[O any](record CheckpointRecord, payload checkpointPayload[O], event gopact.Event) bool {
	return event.RunID == record.RunID && event.SessionID == record.SessionID &&
		event.DefinitionID == record.WorkflowName && event.DefinitionVersion == record.TopologyVersion &&
		event.ExecutionEpoch == payload.ExecutionEpoch && event.SourceRevisionID == payload.SourceRevisionID &&
		event.RevisionID == fmt.Sprintf("%s/revision-%d", record.RunID, event.Sequence) &&
		event.Origin == payload.ControlOrigin && !event.Timestamp.IsZero()
}

func checkpointInterruptRequestMatches(event gopact.Event, request InterruptRequest) bool {
	var decoded InterruptRequest
	return event.Source == "workflow.guard" && event.Summary == request.Subject &&
		json.Unmarshal(event.Payload, &decoded) == nil && reflect.DeepEqual(decoded, request)
}

func checkpointWorkflowInterruptEventMatches(event gopact.Event, first InterruptRequest) bool {
	return event.Source == "workflow" && event.Summary == first.Subject && len(event.Payload) == 0
}

func interruptEventType(eventType string) bool {
	return eventType == EventGuardInterrupted || eventType == EventWorkflowInterrupted
}

func (execution *workflowExecution[I, O]) applyResumePayload(payload checkpointPayload[O]) {
	execution.state = payload.state()
	execution.state.prepareResume(len(payload.ResolvedInterrupts) > 0)
	execution.outputs = append([]O(nil), payload.Outputs...)
	execution.step = payload.NextStep
	if execution.step <= 0 {
		execution.step = 1
	}
	execution.sequence = payload.EventCursor
	execution.eventCursor = payload.EventCursor
	execution.replayStatus = execution.checkpoint.ReplayStatus
	execution.executionEpoch = payload.ExecutionEpoch
	if execution.executionEpoch <= 0 {
		execution.executionEpoch = 1
	}
	execution.controlOrigin = payload.ControlOrigin
	execution.sourceRevision = payload.SourceRevisionID
	execution.sourceRunID = payload.SourceRunID
	execution.sourceEventSeq = payload.SourceEventSeq
	execution.interrupts = copyCheckpointInterrupts(payload.PendingInterrupts)
	execution.interruptProgress = copyCheckpointInterruptProgress(payload.InterruptProgress)
	if execution.sourceRunID != "" {
		execution.restoreSourceSinks()
	}
	if len(payload.ResolvedInterrupts) > 0 {
		resolved := make(map[string][]resumedInterrupt, len(payload.ResolvedInterrupts))
		for _, item := range payload.ResolvedInterrupts {
			resolved[item.ActivationID] = append(resolved[item.ActivationID], resumedInterrupt{
				id: item.InterruptID, payloadRef: item.PayloadRef,
				guardName: item.GuardName, phase: item.Phase,
				nodeName: item.NodeName, activationID: item.ActivationID,
				childRunID: item.ChildRunID, childCheckpointID: item.ChildCheckpointID,
				candidateOutput: item.CandidateOutput, hasCandidateOutput: item.HasCandidateOutput,
			})
		}
		execution.ctx = context.WithValue(execution.ctx, resumedInterruptsContextKey{}, resolved)
	}
}

func (execution *workflowExecution[I, O]) restoreSourceSinks() {
	association := runlog.Association{SourceRunID: execution.sourceRunID, SourceEventSeq: execution.sourceEventSeq}
	associated := associateEventSinks(execution.childSinks, association)
	execution.replaySinks = applyEventSinkWrappers(associated, execution.compiled.eventSinkWrappers)
}

func (execution *workflowExecution[I, O]) replayPending(status, pendingTerm CheckpointStatus, pending gopact.Event) error {
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	if pending.Sequence != execution.sequence+1 {
		return fmt.Errorf(
			"workflow: pending event sequence %d follows %d",
			pending.Sequence,
			execution.sequence,
		)
	}
	pending = execution.runtimeEvent(pending, pending.Sequence)
	if err := execution.emitLockedWithContexts(execution.finalizationContext(), execution.ctx, pending); err != nil {
		return err
	}
	if execution.interruptProgress != nil && execution.interruptProgress.Next < len(execution.interruptProgress.Events) &&
		execution.interruptProgress.Events[execution.interruptProgress.Next].Sequence == pending.Sequence {
		execution.interruptProgress.Next++
	}
	terminal := pendingTerm
	if terminal == "" && checkpointTerminal(status) {
		terminal = status
	}
	if terminal != "" {
		if err := execution.stopCheckpointLeaseHeartbeat(); err != nil {
			return err
		}
		if err := execution.compiled.finishCheckpoint(execution.finalizationContext(), terminal, execution.writeRequest(execution.step, nil)); err != nil {
			return err
		}
		execution.checkpoint.Status = terminal
		return nil
	}
	saved, err := execution.compiled.saveCheckpoint(execution.ctx, execution.writeRequest(execution.step, nil))
	if err != nil {
		return err
	}
	execution.checkpoint = saved
	return nil
}

func (execution *workflowExecution[I, O]) reconcileJournal() error {
	if execution.compiled.store == nil {
		return nil
	}
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	for {
		records, err := execution.compiled.store.List(execution.ctx, runlog.Query{
			RunID: execution.runID, After: execution.eventCursor, Limit: journalReconcilePageSize,
		})
		if err != nil {
			return fmt.Errorf("workflow: reconcile journal: %w", err)
		}
		if len(records) == 0 {
			return nil
		}
		if err := execution.replayJournalPageLocked(records); err != nil {
			return err
		}
		saved, err := execution.compiled.saveCheckpoint(
			execution.ctx,
			execution.writeRequest(execution.step, nil),
		)
		if err != nil {
			return fmt.Errorf("workflow: confirm reconciled journal cursor: %w", err)
		}
		execution.checkpoint = saved
		if len(records) < journalReconcilePageSize {
			return nil
		}
	}
}

func (execution *workflowExecution[I, O]) replayJournalPageLocked(records []runlog.Record) error {
	for _, record := range records {
		if err := execution.validateJournalRecord(record, execution.eventCursor+1); err != nil {
			return err
		}
		if err := (eventDelivery{event: eventFromRunLogRecord(record)}).emit(execution.ctx, execution.replaySinks); err != nil {
			return execution.recordSinkFailure(err)
		}
		execution.sequence = record.Sequence
		execution.eventCursor = record.Sequence
	}
	return nil
}

func (execution *workflowExecution[I, O]) validateJournalRecord(record runlog.Record, sequence int64) error {
	switch {
	case record.Sequence != sequence:
		return fmt.Errorf("workflow: reconcile journal: sequence %d follows %d", record.Sequence, sequence-1)
	case record.SessionID != execution.sessionID || record.RunID != execution.runID:
		return fmt.Errorf("workflow: reconcile journal: sequence %d has inconsistent session or run identity", record.Sequence)
	case record.DefinitionID != execution.compiled.name || record.DefinitionVersion != execution.compiled.topologyVersion:
		return fmt.Errorf("workflow: reconcile journal: sequence %d has inconsistent workflow identity", record.Sequence)
	case record.ParentRunID != execution.parentRunID || record.ExecutionEpoch != execution.executionEpoch ||
		record.SourceRunID != execution.sourceRunID || record.SourceEventSeq != execution.sourceEventSeq ||
		record.SourceRevisionID != execution.sourceRevision:
		return fmt.Errorf("workflow: reconcile journal: sequence %d has inconsistent execution identity", record.Sequence)
	case record.RevisionID == "" || record.EventType == "" || record.Source == "" || record.Timestamp.IsZero():
		return fmt.Errorf("workflow: reconcile journal: sequence %d is incomplete", record.Sequence)
	default:
		return nil
	}
}

func eventFromRunLogRecord(record runlog.Record) gopact.Event {
	return gopact.Event{
		DefinitionID:         record.DefinitionID,
		DefinitionVersion:    record.DefinitionVersion,
		SessionID:            record.SessionID,
		RunID:                record.RunID,
		NodeID:               record.NodeID,
		ActivationID:         record.ActivationID,
		AttemptID:            record.AttemptID,
		RevisionID:           record.RevisionID,
		ParentRunID:          record.ParentRunID,
		NodeExecutionVersion: record.NodeExecutionVersion,
		ExecutionEpoch:       record.ExecutionEpoch,
		SourceRevisionID:     record.SourceRevisionID,
		Sequence:             record.Sequence,
		Type:                 record.EventType,
		Source:               record.Source,
		Origin:               record.Origin,
		Timestamp:            record.Timestamp,
		Summary:              record.Summary,
		Payload:              append(json.RawMessage(nil), record.Payload...),
		PayloadRef:           record.PayloadRef,
	}
}

func resumedCheckpointResult(status CheckpointStatus) (bool, error) {
	if !checkpointTerminal(status) {
		return false, nil
	}
	if status == CheckpointCompleted {
		return true, nil
	}
	return false, fmt.Errorf("%w: workflow checkpoint status %q cannot resume", ErrCheckpointConflict, status)
}

func (execution *workflowExecution[I, O]) startAttempt(resume bool) error {
	eventType := EventWorkflowStarted
	if resume {
		eventType = EventWorkflowResumed
	}
	if execution.controlOrigin == "external_retry" {
		eventType = EventWorkflowRetryStarted
	}
	if execution.controlOrigin == "external_jump" {
		eventType = EventWorkflowJumpStarted
	}
	return execution.commitRunningEvent(gopact.Event{Type: eventType}, execution.step)
}

func (execution *workflowExecution[I, O]) commitRunningEvent(event gopact.Event, nextStep int) error {
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	if execution.checkpoint.ID == "" {
		return execution.emitLocked(execution.ctx, event)
	}
	if execution.sinkFailure != nil {
		return execution.sinkFailure
	}
	pending := execution.runtimeEvent(event, execution.sequence+1)
	replayStatus := replayStatusForEvent(event.Type)
	request := execution.writeRequest(nextStep, &pending)
	request.ReplayStatus = replayStatus
	saved, err := execution.compiled.saveCheckpoint(execution.ctx, request)
	if err != nil {
		return err
	}
	execution.checkpoint = saved
	if err := execution.emitLocked(execution.ctx, pending); err != nil {
		return err
	}
	request = execution.writeRequest(nextStep, nil)
	request.ReplayStatus = replayStatus
	saved, err = execution.compiled.saveCheckpoint(execution.ctx, request)
	if err != nil {
		return err
	}
	execution.checkpoint = saved
	execution.replayStatus = replayStatus
	return nil
}

func replayStatusForEvent(eventType string) ReplayStatus {
	if eventType == EventWorkflowStarted {
		return ReplaySafe
	}
	return ReplayUnknown
}

func (execution *workflowExecution[I, O]) commitActivation(current activation, result nodeRunResult) error {
	if err := execution.state.removeReady(current.id); err != nil {
		return err
	}
	node := execution.compiled.nodes[current.node]
	execution.state.completed[current.node]++
	if err := execution.state.finishActivation(current.id, result, nil); err != nil {
		return err
	}
	if result.skipped {
		if err := execution.compiled.closeJoinExpectations(&execution.state, current, Dispatch{}); err != nil {
			return err
		}
		if err := execution.compiled.materializeReadyJoins(execution.ctx, &execution.state); err != nil {
			return err
		}
		event, err := execution.state.nodeEvent(current.id, EventNodeSkipped, "")
		if err != nil {
			return err
		}
		err = execution.commitRunningEvent(event, execution.step+1)
		return preserveExecutionError(err)
	}
	if err := execution.route(node, current, result.output, true); err != nil {
		return err
	}
	if err := execution.collectOutput(current.node, result.output); err != nil {
		return err
	}
	event, err := execution.state.nodeEvent(current.id, EventNodeCompleted, "")
	if err != nil {
		return err
	}
	err = execution.commitRunningEvent(event, execution.step+1)
	if err != nil {
		return preserveExecutionError(err)
	}
	return execution.flushOutputs()
}

func preserveExecutionError(err error) error {
	if err == nil {
		return nil
	}
	return preservedExecutionError{cause: err}
}

func (execution *workflowExecution[I, O]) nodeContext(ctx context.Context, current activation, step int) (context.Context, *workflowContextTxn) {
	attempt := 0
	if record := execution.state.activations[current.id]; record != nil {
		attempt = record.attempt
	}
	ctx = context.WithValue(ctx, guardMetaContextKey{}, guardMetaContext{
		runID: execution.runID, workflowName: execution.compiled.name, activationID: current.id, attempt: attempt,
	})
	ctx = context.WithValue(ctx, runInfoContextKey{}, RunInfo{
		SessionID: execution.sessionID, RunID: execution.runID,
		ParentRunID: execution.parentRunID, Depth: execution.depth,
		NodeID: current.node, ActivationID: current.id, Attempt: attempt,
	})
	if resolved, ok := execution.ctx.Value(resumedInterruptsContextKey{}).(map[string][]resumedInterrupt); ok {
		if interrupts := resolved[current.id]; len(interrupts) > 0 {
			ctx = context.WithValue(ctx, nodeResumedInterruptsContextKey{}, interrupts)
			ctx = context.WithValue(ctx, resumedInterruptContextKey{}, interrupts[0])
		}
	}
	var ordinal atomic.Int64
	factory := childOptionsFactory(func() []gopact.RunOption { return execution.childOptions(ctx, current, step, int(ordinal.Add(1))) })
	ctx = context.WithValue(ctx, childOptionsFactoryContextKey{}, factory)
	if execution.compiled.contextKey == nil {
		return ctx, nil
	}
	txn := &workflowContextTxn{
		key: execution.compiled.contextKey, value: execution.state.workflowContext,
		revision: execution.state.contextRevision,
	}
	return context.WithValue(ctx, workflowContextTxnKey{}, txn), txn
}

func (execution *workflowExecution[I, O]) commitRetry(current activation, result nodeRunResult) error {
	event, err := execution.state.nodeEvent(current.id, EventNodeRetrying, "", result)
	if err != nil {
		return err
	}
	if err := execution.state.retryActivation(current.id, result.retryInput); err != nil {
		return err
	}
	err = execution.commitRunningEvent(event, execution.step+1)
	return preserveExecutionError(err)
}

func (execution *workflowExecution[I, O]) childOptions(ctx context.Context, current activation, step, ordinal int) []gopact.RunOption {
	childRunID := fmt.Sprintf("%s-child-%s-%d-%d", execution.runID, current.id, step, ordinal)
	var resume *ResumeRequest
	if resolved, ok := ctx.Value(nodeResumedInterruptsContextKey{}).([]resumedInterrupt); ok &&
		len(resolved) > 0 && resolved[0].nodeName == current.node &&
		resolved[0].activationID == current.id && resolved[0].childRunID != "" {
		childRunID = resolved[0].childRunID
		resolutions := make([]InterruptResolution, len(resolved))
		for index, interrupt := range resolved {
			resolutions[index] = InterruptResolution{InterruptID: interrupt.id, PayloadRef: interrupt.payloadRef}
		}
		resume = &ResumeRequest{
			RunID: childRunID, CheckpointID: resolved[0].childCheckpointID, Resolutions: resolutions,
		}
	}
	options := []gopact.RunOption{
		gopact.WithSessionID(execution.sessionID),
		gopact.WithRunID(childRunID),
		gopact.WithRunLineage(gopact.RunLineage{ParentRunID: execution.runID, Depth: execution.depth + 1}),
	}
	if resume != nil {
		options = append(options, WithResume(*resume))
	}
	for kind, generator := range execution.idGenerators {
		options = append(options, gopact.WithIDGenerator(kind, generator))
	}
	for _, sink := range execution.childSinks {
		options = append(options, gopact.WithEventSink(sink))
	}
	return options
}

func (execution *workflowExecution[I, O]) handleNodeError(current activation, result nodeRunResult, cause error) error {
	var interrupt InterruptError
	if errors.As(cause, &interrupt) {
		if err := execution.state.interruptActivation(current.id); err != nil {
			return err
		}
		return execution.interrupt(current, interrupt)
	}
	if err := execution.state.finishActivation(current.id, result, cause); err != nil {
		return err
	}
	var rejection gopact.GuardRejection
	if errors.As(cause, &rejection) {
		if err := execution.emitRejection(rejection); err != nil {
			return err
		}
	}
	event, err := execution.state.nodeEvent(current.id, EventNodeFailed, "")
	if err != nil {
		return err
	}
	if err := execution.emitEvent(event); err != nil {
		return err
	}
	return fmt.Errorf("workflow: node %q: %w", current.node, cause)
}

func (execution *workflowExecution[I, O]) interrupt(current activation, interrupt InterruptError) error {
	if execution.checkpoint.ID == "" {
		return fmt.Errorf("workflow: guard interrupt requires checkpointer: %w", interrupt)
	}
	state := execution.state
	state.prioritize(current)
	requests := interruptRequests(interrupt)
	waits := checkpointInterruptsForActivation(current, interrupt, requests)
	return execution.finishInterrupt(state, waits, interrupt, requests)
}

func (execution *workflowExecution[I, O]) interruptBatch(items []batchActivation) error {
	if execution.checkpoint.ID == "" {
		return errors.New("workflow: guard interrupt requires checkpointer")
	}
	waits := make([]checkpointInterrupt, 0, len(items))
	requests := make([]InterruptRequest, 0, len(items))
	var first InterruptError
	for index, item := range items {
		var interrupt InterruptError
		if !errors.As(item.err, &interrupt) {
			return errors.New("workflow: scheduler interrupt batch contains a non-interrupt error")
		}
		if err := execution.state.interruptActivation(item.activation.id); err != nil {
			return err
		}
		itemRequests := interruptRequests(interrupt)
		waits = append(waits, checkpointInterruptsForActivation(item.activation, interrupt, itemRequests)...)
		requests = append(requests, itemRequests...)
		if index == 0 {
			first = interrupt
		}
	}
	return execution.finishInterrupt(execution.state, waits, first, requests)
}

func interruptRequests(interrupt InterruptError) []InterruptRequest {
	if len(interrupt.Requests) > 0 {
		return append([]InterruptRequest(nil), interrupt.Requests...)
	}
	return []InterruptRequest{interrupt.Request}
}

func checkpointInterruptsForActivation(current activation, interrupt InterruptError, requests []InterruptRequest) []checkpointInterrupt {
	guardName := interrupt.GuardName
	if interrupt.RunID != "" {
		guardName = ""
	}
	waits := make([]checkpointInterrupt, len(requests))
	var candidate any
	if interrupt.hasCandidateOutput {
		candidate = interrupt.candidateOutput
	}
	for index, request := range requests {
		waits[index] = checkpointInterrupt{
			Request: request, GuardName: guardName, Phase: interrupt.Phase,
			NodeName: current.node, ActivationID: current.id,
			ChildRunID: interrupt.RunID, ChildCheckpointID: interrupt.CheckpointID,
			CandidateOutput: candidate, HasCandidateOutput: interrupt.hasCandidateOutput,
		}
	}
	return waits
}

func (execution *workflowExecution[I, O]) finishInterrupt(state runState, waits []checkpointInterrupt, first InterruptError, requests []InterruptRequest) error {
	seen := make(map[string]struct{}, len(requests))
	events := make([]gopact.Event, 0, len(requests)+1)
	for _, interrupt := range requests {
		if _, ok := seen[interrupt.ID]; ok {
			return fmt.Errorf("workflow: duplicate pending interrupt id %q", interrupt.ID)
		}
		seen[interrupt.ID] = struct{}{}
		payload, err := json.Marshal(interrupt)
		if err != nil {
			return preservedExecutionError{cause: err}
		}
		event := gopact.Event{Type: EventGuardInterrupted, Source: "workflow.guard", Summary: interrupt.Subject, Payload: payload}
		events = append(events, execution.runtimeEvent(event, execution.sequence+int64(len(events))+1))
	}
	workflowEvent := gopact.Event{Type: EventWorkflowInterrupted, Source: "workflow", Summary: first.Request.Subject}
	events = append(events, execution.runtimeEvent(workflowEvent, execution.sequence+int64(len(events))+1))
	execution.state = state
	execution.interrupts = copyCheckpointInterrupts(waits)
	execution.interruptProgress = &checkpointInterruptProgress{Events: events}
	if err := execution.continueInterruptBatch(); err != nil {
		return preservedExecutionError{cause: err}
	}
	return preservedExecutionError{cause: execution.interruptError()}
}

func (execution *workflowExecution[I, O]) continueInterruptBatch() error {
	for execution.interruptProgress != nil && execution.interruptProgress.Next < len(execution.interruptProgress.Events) {
		if err := execution.commitInterruptBatchEvent(execution.interruptProgress.Events[execution.interruptProgress.Next]); err != nil {
			return err
		}
	}
	if err := execution.stopCheckpointLeaseHeartbeat(); err != nil {
		return err
	}
	progress := execution.interruptProgress
	execution.interruptProgress = nil
	request := execution.writeRequest(execution.step, nil)
	saved, err := execution.compiled.interruptCheckpoint(execution.finalizationContext(), request, execution.interrupts)
	if err != nil {
		execution.interruptProgress = progress
		return err
	}
	execution.checkpoint = saved
	return nil
}

func (execution *workflowExecution[I, O]) commitInterruptBatchEvent(event gopact.Event) error {
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	if execution.sinkFailure != nil {
		return execution.sinkFailure
	}
	request := execution.writeRequest(execution.step, &event)
	saved, err := execution.compiled.saveCheckpoint(execution.finalizationContext(), request)
	if err != nil {
		return err
	}
	execution.checkpoint = saved
	if err := execution.emitLockedWithContexts(execution.finalizationContext(), execution.ctx, event); err != nil {
		return err
	}
	execution.interruptProgress.Next++
	saved, err = execution.compiled.saveCheckpoint(execution.finalizationContext(), execution.writeRequest(execution.step, nil))
	if err != nil {
		return err
	}
	execution.checkpoint = saved
	return nil
}

func (execution *workflowExecution[I, O]) interruptError() InterruptError {
	requests := make([]InterruptRequest, len(execution.interrupts))
	for index := range execution.interrupts {
		requests[index] = execution.interrupts[index].Request
	}
	err := InterruptError{RunID: execution.runID, CheckpointID: execution.checkpoint.ID, Requests: requests}
	if len(requests) > 0 {
		err.Request = requests[0]
		err.GuardName = execution.interrupts[0].GuardName
		err.Phase = execution.interrupts[0].Phase
		err.candidateOutput = execution.interrupts[0].CandidateOutput
		err.hasCandidateOutput = execution.interrupts[0].HasCandidateOutput
	}
	return err
}

func (execution *workflowExecution[I, O]) emitRejection(rejection gopact.GuardRejection) error {
	payload, err := json.Marshal(rejection)
	if err != nil {
		return preservedExecutionError{cause: err}
	}
	summary := rejection.Reason
	if summary == "" {
		summary = rejection.Message
	}
	return execution.emitEvent(gopact.Event{Type: EventGuardRejected, Source: "workflow.guard", Summary: summary, Payload: payload})
}

func (execution *workflowExecution[I, O]) collectOutput(nodeName string, output any) error {
	if _, exit := execution.compiled.exits[nodeName]; !exit {
		return nil
	}
	typed, ok := output.(O)
	if !ok {
		return fmt.Errorf("workflow: exit node %q output type mismatch: got %T", nodeName, output)
	}
	workflowCtx := WorkflowContext[I, O]{ctx: execution.ctx, Input: execution.input, Output: typed}
	if err := runLifecycleHooks(execution.compiled.afterWorkflow, &workflowCtx); err != nil {
		return fmt.Errorf("workflow: after workflow: %w", err)
	}
	execution.outputs = append(execution.outputs, workflowCtx.Output)
	return nil
}

func (execution *workflowExecution[I, O]) flushOutputs() error {
	if execution.outputSink == nil {
		execution.outputIndex = len(execution.outputs)
		return nil
	}
	for execution.outputIndex < len(execution.outputs) {
		output := execution.outputs[execution.outputIndex]
		execution.outputIndex++
		if !execution.outputSink(output) {
			execution.cancel(context.Canceled)
			return context.Canceled
		}
	}
	return nil
}

func (execution *workflowExecution[I, O]) route(node runtimeNode, current activation, output any, materialize bool) error {
	dispatch, err := node.routeAny(execution.ctx, output)
	if err != nil {
		return fmt.Errorf("workflow: route from node %q: %w", current.node, err)
	}
	dispatch, err = execution.compiled.applyRouteMiddlewares(execution.ctx, node, current.node, output, dispatch)
	if err != nil {
		return fmt.Errorf("workflow: route middleware from node %q: %w", current.node, err)
	}
	if err := execution.compiled.scheduleNext(execution.ctx, &execution.state, current, output, dispatch); err != nil {
		return err
	}
	if materialize {
		return execution.compiled.materializeReadyJoins(execution.ctx, &execution.state)
	}
	return nil
}

func (execution *workflowExecution[I, O]) handleError(cause error) error {
	if leaseCause := context.Cause(execution.ctx); execution.matchesCheckpointLeaseError(leaseCause) {
		_ = execution.stopCheckpointLeaseHeartbeat()
		return leaseCause
	}
	if execution.matchesCheckpointLeaseError(cause) {
		execution.cancel(errors.Join(context.Canceled, cause))
		_ = execution.stopCheckpointLeaseHeartbeat()
		return cause
	}
	if canceled := execution.cancellationCause(); canceled != nil {
		return execution.cancelRun(canceled)
	}
	if preserved, ok := cause.(preservedExecutionError); ok {
		return preserved.cause
	}
	return execution.fail(cause)
}

func (execution *workflowExecution[I, O]) cancellationCause() error {
	if execution.ctx.Err() == nil {
		return nil
	}
	cause := context.Cause(execution.ctx)
	if cause != nil {
		return cause
	}
	return execution.ctx.Err()
}

func (execution *workflowExecution[I, O]) fail(cause error) error {
	event := gopact.Event{Type: EventWorkflowFailed, Summary: cause.Error()}
	return execution.commitTerminalError(CheckpointFailed, event, cause)
}

func (execution *workflowExecution[I, O]) cancelRun(cause error) error {
	if errors.Is(cause, ErrRunTerminated) {
		event := gopact.Event{Type: EventWorkflowTerminated, Summary: cause.Error(), Origin: "external_terminate"}
		return execution.commitTerminalError(CheckpointTerminated, event, cause)
	}
	event := gopact.Event{Type: EventWorkflowCanceled, Summary: cause.Error()}
	return execution.commitTerminalError(CheckpointCanceled, event, cause)
}

func (execution *workflowExecution[I, O]) commitTerminalError(status CheckpointStatus, event gopact.Event, cause error) error {
	if err := execution.commitTerminalEvent(execution.finalizationContext(), status, event); err != nil {
		return err
	}
	return cause
}

func (execution *workflowExecution[I, O]) finish() error {
	return execution.commitTerminal(CheckpointCompleted, gopact.Event{Type: EventWorkflowCompleted})
}

func (execution *workflowExecution[I, O]) commitTerminal(status CheckpointStatus, event gopact.Event) error {
	return execution.commitTerminalEvent(execution.finalizationContext(), status, event)
}

func (execution *workflowExecution[I, O]) commitTerminalEvent(storeCtx context.Context, status CheckpointStatus, event gopact.Event) error {
	execution.eventMu.Lock()
	defer execution.eventMu.Unlock()
	if execution.checkpoint.ID == "" {
		return execution.emitLocked(execution.ctx, event)
	}
	if execution.sinkFailure != nil {
		return execution.sinkFailure
	}
	pending := execution.runtimeEvent(event, execution.sequence+1)
	request := execution.writeRequest(execution.step, &pending)
	request.PendingTerm = status
	saved, err := execution.compiled.saveCheckpoint(storeCtx, request)
	if err != nil {
		return err
	}
	execution.checkpoint = saved
	if err := execution.emitLockedWithContexts(storeCtx, execution.ctx, pending); err != nil {
		return err
	}
	if err := execution.stopCheckpointLeaseHeartbeat(); err != nil {
		return err
	}
	return execution.compiled.finishCheckpoint(execution.finalizationContext(), status, execution.writeRequest(execution.step, nil))
}

func (c *compiled[I, O]) scheduleNext(ctx context.Context, state *runState, source activation, output any, dispatch Dispatch) error {
	if err := dispatch.validateSource(source.node); err != nil {
		return err
	}
	if err := dispatch.settle.validate(); err != nil {
		return err
	}
	if dispatch.stop {
		if _, ok := c.exits[source.node]; !ok {
			return fmt.Errorf("workflow: non-exit node %q returned stop", source.node)
		}
		return c.closeJoinExpectations(state, source, dispatch)
	}
	if !dispatch.explicit {
		for _, target := range c.edges[source.node] {
			dispatch.deliveries = append(dispatch.deliveries, delivery{
				target:          target,
				useSourceOutput: true,
			})
		}
	}
	set, err := c.prepareSourceSet(state, source, dispatch)
	if err != nil {
		return err
	}
	edgeSet := map[string]struct{}{}
	for _, target := range c.edges[source.node] {
		edgeSet[target] = struct{}{}
	}
	branchIndex := 0
	for index, item := range dispatch.deliveries {
		request := deliveryRequest{source: source, output: output, edges: edgeSet, item: item, deliveryIndex: index}
		if set != nil && item.iter != nil {
			request.sourceSet = set.id
		}
		if set != nil && item.iter == nil && c.materializesActivation(item) {
			request.sourceSet = set.id
			request.branchIndex = branchIndex
			branchIndex++
		}
		if err := c.scheduleDelivery(ctx, state, request); err != nil {
			return err
		}
	}
	return c.closeJoinExpectations(state, source, dispatch)
}

type deliveryRequest struct {
	source        activation
	output        any
	edges         map[string]struct{}
	item          delivery
	sourceSet     string
	branchIndex   int
	deliveryIndex int
}

func (c *compiled[I, O]) scheduleDelivery(ctx context.Context, state *runState, request deliveryRequest) error {
	if _, ok := request.edges[request.item.target]; !ok {
		return fmt.Errorf("workflow: route from %q selected undeclared target %q", request.source.node, request.item.target)
	}
	if request.item.iter != nil {
		return c.scheduleIter(ctx, state, request)
	}
	payload := request.item.payload
	if request.item.useSourceOutput {
		payload = request.output
	}
	if c.isJoinTarget(request.item.target) {
		return c.scheduleJoin(state, request, payload)
	}
	if err := validateActivationPayload(request.source.node, request.item.target, payload, c.nodes[request.item.target]); err != nil {
		return err
	}
	c.enqueue(state, enqueueRequest{
		target: request.item.target, input: payload, sourceSet: request.sourceSet,
		branchIndex: request.branchIndex, correlation: c.nextCorrelation(request.source, request.item.target),
	})
	return nil
}

func (c *compiled[I, O]) scheduleJoin(state *runState, req deliveryRequest, payload any) error {
	if !req.item.useSourceOutput {
		return fmt.Errorf("workflow: route from %q to join target %q must use source output", req.source.node, req.item.target)
	}
	source := req.source
	source.correlation = c.nextCorrelation(source, req.item.target)
	c.addContribution(state, source, req.item.target, payload)
	return nil
}

func (c *compiled[I, O]) applyRouteMiddlewares(ctx context.Context, node runtimeNode, nodeName string, output any, dispatch Dispatch) (Dispatch, error) {
	current := dispatch
	for _, middleware := range c.routeMiddlewares {
		next, matched, err := middleware.run(ctx, node, nodeName, output, current)
		if err != nil {
			return Dispatch{}, err
		}
		if matched {
			current = next
		}
	}
	return current, nil
}

func (policy SettlePolicy) validate() error {
	switch policy.mode {
	case "", "all", "any":
		return nil
	case "quorum":
		if policy.quorum <= 0 {
			return fmt.Errorf("workflow: settle quorum must be positive, got %d", policy.quorum)
		}
		return nil
	default:
		return fmt.Errorf("workflow: unknown settle policy %q", policy.mode)
	}
}

func (policy SettlePolicy) normalized() SettlePolicy {
	if policy.mode == "" {
		return SettleAll()
	}
	return policy
}

func (policy SettlePolicy) required(total int) (int, error) {
	policy = policy.normalized()
	switch policy.mode {
	case "all":
		return total, nil
	case "any":
		return 1, nil
	case "quorum":
		if policy.quorum > total {
			return 0, fmt.Errorf("workflow: settle quorum %d exceeds branch count %d", policy.quorum, total)
		}
		return policy.quorum, nil
	default:
		return 0, fmt.Errorf("workflow: unknown settle policy %q", policy.mode)
	}
}

func (policy SettlePolicy) threshold() (int, error) {
	policy = policy.normalized()
	switch policy.mode {
	case "all":
		return 0, nil
	case "any":
		return 1, nil
	case "quorum":
		return policy.quorum, nil
	default:
		return 0, fmt.Errorf("workflow: unknown settle policy %q", policy.mode)
	}
}

type checkpointPrepareRequest[O any] struct {
	SessionID        string
	RunID            string
	OwnerID          string
	Resume           ResumeRequest
	IsResume         bool
	State            runState
	Outputs          []O
	NextStep         int
	ControlOrigin    string
	SourceRevisionID string
	Association      runlog.Association
	Associated       bool
}

type checkpointWriteRequest[O any] struct {
	Record            CheckpointRecord
	State             runState
	Outputs           []O
	NextStep          int
	EventCursor       int64
	PendingEvent      *gopact.Event
	PendingTerm       CheckpointStatus
	ReplayStatus      ReplayStatus
	PendingInterrupts []checkpointInterrupt
	InterruptProgress *checkpointInterruptProgress
}

type checkpointLeaseError struct {
	cause         error
	runID         string
	ownerID       string
	claimSequence int64
}

func (err *checkpointLeaseError) Error() string { return err.cause.Error() }
func (err *checkpointLeaseError) Unwrap() error { return err.cause }

func markCheckpointLeaseError(err error, record CheckpointRecord) error {
	if !errors.Is(err, ErrCheckpointLeaseLost) {
		return err
	}
	return &checkpointLeaseError{
		cause: err, runID: record.RunID, ownerID: record.OwnerID, claimSequence: record.ClaimSequence,
	}
}

func (execution *workflowExecution[I, O]) matchesCheckpointLeaseError(cause error) bool {
	var leaseError *checkpointLeaseError
	return errors.As(cause, &leaseError) && leaseError.runID == execution.runID &&
		leaseError.ownerID == execution.checkpoint.OwnerID &&
		leaseError.claimSequence == execution.checkpoint.ClaimSequence
}

func (execution *workflowExecution[I, O]) cancelOnCheckpointLeaseError(cause error) error {
	if execution.matchesCheckpointLeaseError(cause) {
		execution.cancel(errors.Join(context.Canceled, cause))
	}
	return cause
}

func (c *compiled[I, O]) prepareCheckpoint(ctx context.Context, req checkpointPrepareRequest[O]) (CheckpointRecord, error) {
	if c.store == nil {
		if req.IsResume {
			return CheckpointRecord{}, errors.New("workflow: resume requires checkpointer")
		}
		return CheckpointRecord{}, nil
	}
	if req.IsResume {
		return c.prepareResumeCheckpoint(ctx, req)
	}
	now := time.Now()
	meta := c.checkpointMeta(checkpointPayloadMeta{
		OwnerID: req.OwnerID, LeaseExpiresAt: now.Add(c.checkpointLeaseDuration), ClaimSequence: 1,
	})
	if req.Associated {
		meta.SourceRunID = req.Association.SourceRunID
		meta.SourceEventSeq = req.Association.SourceEventSeq
	}
	if req.SourceRevisionID != "" {
		meta.ControlOrigin = req.ControlOrigin
		meta.SourceRevisionID = req.SourceRevisionID
	}
	payload, err := encodeCheckpointPayloadWithMeta(req.State, req.Outputs, req.NextStep, meta)
	if err != nil {
		return CheckpointRecord{}, err
	}
	rec := CheckpointRecord{
		ID: "checkpoint:" + req.RunID, SessionID: req.SessionID, RunID: req.RunID, WorkflowName: c.name,
		SourceRunID: meta.SourceRunID, SourceEventSeq: meta.SourceEventSeq, SourceRevisionID: meta.SourceRevisionID,
		TopologyVersion: c.topologyVersion, SchemaVersion: checkpointSchemaVersion,
		Version: 1, Status: CheckpointRunning, Payload: payload, ReplayStatus: ReplayUnknown,
		OwnerID: meta.OwnerID, LeaseExpiresAt: meta.LeaseExpiresAt,
		LeaseDuration: c.checkpointLeaseDuration, ClaimSequence: meta.ClaimSequence,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := c.store.Create(ctx, rec); err != nil {
		return CheckpointRecord{}, err
	}
	return rec, nil
}

func (c *compiled[I, O]) checkpointMeta(meta checkpointPayloadMeta) checkpointPayloadMeta {
	meta.SchemaVersion = checkpointSchemaVersion
	meta.WorkflowName = c.name
	meta.TopologyVersion = c.topologyVersion
	if meta.ExecutionEpoch <= 0 {
		meta.ExecutionEpoch = 1
	}
	if meta.ControlOrigin == "" {
		meta.ControlOrigin = "natural"
	}
	return meta
}

func (c *compiled[I, O]) validateCheckpointIdentity(payload checkpointPayload[O]) error {
	if payload.SchemaVersion != checkpointSchemaVersion {
		return fmt.Errorf("%w: checkpoint schema version %d is incompatible with %d", ErrCheckpointMismatch, payload.SchemaVersion, checkpointSchemaVersion)
	}
	if payload.WorkflowName != c.name {
		return fmt.Errorf("%w: checkpoint workflow %q does not match %q", ErrCheckpointMismatch, payload.WorkflowName, c.name)
	}
	if payload.TopologyVersion != c.topologyVersion {
		return fmt.Errorf("%w: checkpoint topology version %q does not match %q", ErrCheckpointMismatch, payload.TopologyVersion, c.topologyVersion)
	}
	return nil
}

func (c *compiled[I, O]) prepareResumeCheckpoint(ctx context.Context, req checkpointPrepareRequest[O]) (CheckpointRecord, error) {
	record, err := c.store.Load(ctx, req.Resume.RunID)
	if err != nil {
		return CheckpointRecord{}, err
	}
	if req.Resume.CheckpointID != "" && record.ID != req.Resume.CheckpointID {
		return CheckpointRecord{}, fmt.Errorf("workflow: checkpoint id %q does not match resume checkpoint %q", record.ID, req.Resume.CheckpointID)
	}
	if err := c.validateCheckpointRecordIdentity(record); err != nil {
		return CheckpointRecord{}, err
	}
	if req.SessionID != "" && record.SessionID != req.SessionID {
		return CheckpointRecord{}, fmt.Errorf(
			"%w: checkpoint session %q does not match %q",
			ErrCheckpointMismatch,
			record.SessionID,
			req.SessionID,
		)
	}
	switch record.Status {
	case CheckpointRunning, CheckpointInterrupted:
		return c.activeResumeCheckpoint(ctx, record, req)
	case CheckpointCompleted, CheckpointFailed, CheckpointCanceled, CheckpointTerminated:
		return c.terminalResumeCheckpoint(record)
	default:
		return CheckpointRecord{}, fmt.Errorf("workflow: checkpoint status %q cannot resume", record.Status)
	}
}

func (c *compiled[I, O]) activeResumeCheckpoint(ctx context.Context, record CheckpointRecord, req checkpointPrepareRequest[O]) (CheckpointRecord, error) {
	payload, err := decodeCheckpointPayload[O](record.Payload)
	if err != nil {
		return CheckpointRecord{}, err
	}
	if err := c.validateCheckpointIdentity(payload); err != nil {
		return CheckpointRecord{}, err
	}
	if err := validateCheckpointSourceIdentity(record, payload.meta()); err != nil {
		return CheckpointRecord{}, err
	}
	if err := validateCheckpointInterruptProgress(record, payload); err != nil {
		return CheckpointRecord{}, err
	}
	return c.claimCheckpoint(ctx, record, req.OwnerID, req.Resume)
}

func validateCheckpointSourceIdentity(record CheckpointRecord, meta checkpointPayloadMeta) error {
	if !validSourceLineage(record.SourceRunID, record.SourceEventSeq, record.SourceRevisionID) ||
		!validSourceLineage(meta.SourceRunID, meta.SourceEventSeq, meta.SourceRevisionID) ||
		record.SourceRunID != meta.SourceRunID || record.SourceEventSeq != meta.SourceEventSeq ||
		record.SourceRevisionID != meta.SourceRevisionID {
		return fmt.Errorf("%w: checkpoint source lineage is inconsistent", ErrCheckpointMismatch)
	}
	return nil
}

func (c *compiled[I, O]) validateCheckpointRecordIdentity(record CheckpointRecord) error {
	if record.SchemaVersion != checkpointSchemaVersion {
		return fmt.Errorf("%w: checkpoint schema version %d is incompatible with %d", ErrCheckpointMismatch, record.SchemaVersion, checkpointSchemaVersion)
	}
	if record.SessionID == "" {
		return fmt.Errorf("%w: checkpoint session is missing", ErrCheckpointMismatch)
	}
	if record.WorkflowName != c.name {
		return fmt.Errorf("%w: checkpoint workflow %q does not match %q", ErrCheckpointMismatch, record.WorkflowName, c.name)
	}
	if record.TopologyVersion != c.topologyVersion {
		return fmt.Errorf("%w: checkpoint topology version %q does not match %q", ErrCheckpointMismatch, record.TopologyVersion, c.topologyVersion)
	}
	return nil
}

func (c *compiled[I, O]) terminalResumeCheckpoint(record CheckpointRecord) (CheckpointRecord, error) {
	return CheckpointRecord{}, fmt.Errorf("%w: workflow checkpoint status %q cannot resume", ErrCheckpointConflict, record.Status)
}

func checkpointTerminal(status CheckpointStatus) bool {
	return status == CheckpointCompleted || status == CheckpointFailed || status == CheckpointCanceled || status == CheckpointTerminated
}

func (c *compiled[I, O]) claimCheckpoint(ctx context.Context, rec CheckpointRecord, ownerID string, req ResumeRequest) (CheckpointRecord, error) {
	payload, err := decodeCheckpointPayload[O](rec.Payload)
	if err != nil {
		return CheckpointRecord{}, err
	}
	if err := c.validateCheckpointIdentity(payload); err != nil {
		return CheckpointRecord{}, err
	}
	now := time.Now()
	claimSequence := rec.ClaimSequence
	if claimSequence == 0 {
		claimSequence = payload.ClaimSequence
	}
	if rec.Status == CheckpointInterrupted {
		resolved, err := resolvePendingInterrupts(payload.PendingInterrupts, req.Resolutions)
		if err != nil {
			return CheckpointRecord{}, err
		}
		payload.ResolvedInterrupts = resolved
		payload.PendingInterrupts = nil
	}
	payload.OwnerID = ownerID
	payload.LeaseExpiresAt = now.Add(c.checkpointLeaseDuration)
	payload.ClaimSequence = claimSequence + 1
	claimed, err := encodeCheckpointPayloadWithMeta(payload.state(), payload.Outputs, payload.NextStep, payload.meta())
	if err != nil {
		return CheckpointRecord{}, err
	}
	next := rec
	next.Payload = claimed
	next.Status = CheckpointRunning
	next.OwnerID = payload.OwnerID
	next.LeaseExpiresAt = payload.LeaseExpiresAt
	next.LeaseDuration = c.checkpointLeaseDuration
	next.ClaimSequence = payload.ClaimSequence
	next.UpdatedAt = now
	if err := c.store.Claim(ctx, next, rec.Version); err != nil {
		return CheckpointRecord{}, err
	}
	next.Version++
	return next, nil
}

func resolvePendingInterrupts(pending []checkpointInterrupt, resolutions []InterruptResolution) ([]checkpointInterruptResolution, error) {
	if len(pending) == 0 {
		return nil, errors.New("workflow: interrupted checkpoint is missing interrupt state")
	}
	resolved := make([]checkpointInterruptResolution, 0, len(pending))
	for _, interrupt := range pending {
		resolution, ok := findInterruptResolution(resolutions, interrupt.Request.ID)
		if !ok {
			return nil, fmt.Errorf("workflow: interrupt resolution %q is required", interrupt.Request.ID)
		}
		if resolution.PayloadRef == "" {
			return nil, fmt.Errorf("workflow: interrupt resolution %q payload ref is required", resolution.InterruptID)
		}
		resolved = append(resolved, checkpointInterruptResolution{
			InterruptID: resolution.InterruptID, PayloadRef: resolution.PayloadRef,
			GuardName: interrupt.GuardName, Phase: interrupt.Phase,
			NodeName: interrupt.NodeName, ActivationID: interrupt.ActivationID,
			ChildRunID: interrupt.ChildRunID, ChildCheckpointID: interrupt.ChildCheckpointID,
			CandidateOutput: interrupt.CandidateOutput, HasCandidateOutput: interrupt.HasCandidateOutput,
		})
	}
	return resolved, nil
}

func findInterruptResolution(resolutions []InterruptResolution, interruptID string) (InterruptResolution, bool) {
	for _, resolution := range resolutions {
		if resolution.InterruptID == interruptID {
			return resolution, true
		}
	}
	return InterruptResolution{}, false
}

func (c *compiled[I, O]) saveCheckpoint(ctx context.Context, req checkpointWriteRequest[O]) (CheckpointRecord, error) {
	if c.store == nil || req.Record.ID == "" {
		return req.Record, nil
	}
	meta, err := decodeCheckpointPayloadMeta[O](req.Record.Payload)
	if err != nil {
		return CheckpointRecord{}, err
	}
	now := time.Now()
	meta.OwnerID = req.Record.OwnerID
	meta.ClaimSequence = req.Record.ClaimSequence
	meta.LeaseExpiresAt = now.Add(c.checkpointLeaseDuration)
	meta.EventCursor = req.EventCursor
	meta.PendingEvent = req.PendingEvent
	meta.PendingTerm = req.PendingTerm
	meta.PendingInterrupts = copyCheckpointInterrupts(req.PendingInterrupts)
	meta.InterruptProgress = copyCheckpointInterruptProgress(req.InterruptProgress)
	payload, err := encodeCheckpointPayloadWithMeta(req.State, req.Outputs, req.NextStep, meta)
	if err != nil {
		return CheckpointRecord{}, err
	}
	next := req.Record
	next.Payload = payload
	next.Status = CheckpointRunning
	next.OwnerID = meta.OwnerID
	next.LeaseExpiresAt = meta.LeaseExpiresAt
	next.LeaseDuration = c.checkpointLeaseDuration
	next.ClaimSequence = meta.ClaimSequence
	next.ConfirmedSequence = req.EventCursor
	next.PendingSequence = pendingEventSequence(req.PendingEvent)
	next.ReplayStatus = req.ReplayStatus
	next.UpdatedAt = now
	if err := c.store.Save(ctx, next, req.Record.Version); err != nil {
		return CheckpointRecord{}, markCheckpointLeaseError(err, req.Record)
	}
	next.Version++
	return next, nil
}

func (c *compiled[I, O]) interruptCheckpoint(ctx context.Context, req checkpointWriteRequest[O], interrupts []checkpointInterrupt) (CheckpointRecord, error) {
	if c.store == nil || req.Record.ID == "" {
		return req.Record, nil
	}
	leaseRecord := req.Record
	meta, err := decodeCheckpointPayloadMeta[O](req.Record.Payload)
	if err != nil {
		return CheckpointRecord{}, err
	}
	now := time.Now()
	if req.PendingEvent != nil {
		meta.OwnerID = req.Record.OwnerID
		meta.LeaseExpiresAt = now.Add(c.checkpointLeaseDuration)
	} else {
		meta.OwnerID = ""
		meta.LeaseExpiresAt = time.Time{}
	}
	meta.EventCursor = req.EventCursor
	meta.PendingEvent = req.PendingEvent
	meta.PendingTerm = req.PendingTerm
	meta.PendingInterrupts = copyCheckpointInterrupts(interrupts)
	meta.InterruptProgress = copyCheckpointInterruptProgress(req.InterruptProgress)
	payload, err := encodeCheckpointPayloadWithMeta(req.State, req.Outputs, req.NextStep, meta)
	if err != nil {
		return CheckpointRecord{}, err
	}
	next := req.Record
	next.Payload = payload
	if req.PendingEvent != nil {
		next.Status = CheckpointRunning
		next.OwnerID = meta.OwnerID
		next.LeaseExpiresAt = meta.LeaseExpiresAt
		next.LeaseDuration = c.checkpointLeaseDuration
	} else {
		next.Status = CheckpointInterrupted
		next.OwnerID = ""
		next.LeaseExpiresAt = time.Time{}
		next.LeaseDuration = 0
	}
	next.ConfirmedSequence = req.EventCursor
	next.PendingSequence = pendingEventSequence(req.PendingEvent)
	next.ReplayStatus = req.ReplayStatus
	next.UpdatedAt = now
	if err := c.store.Save(ctx, next, req.Record.Version); err != nil {
		return CheckpointRecord{}, markCheckpointLeaseError(err, leaseRecord)
	}
	next.Version++
	return next, nil
}

func (c *compiled[I, O]) finishCheckpoint(ctx context.Context, status CheckpointStatus, req checkpointWriteRequest[O]) error {
	if c.store == nil || req.Record.ID == "" {
		return nil
	}
	meta, err := decodeCheckpointPayloadMeta[O](req.Record.Payload)
	if err != nil {
		return err
	}
	meta.OwnerID = ""
	meta.LeaseExpiresAt = time.Time{}
	meta.EventCursor = req.EventCursor
	meta.PendingEvent = req.PendingEvent
	meta.PendingTerm = req.PendingTerm
	meta.PendingInterrupts = copyCheckpointInterrupts(req.PendingInterrupts)
	meta.InterruptProgress = copyCheckpointInterruptProgress(req.InterruptProgress)
	payload, err := encodeCheckpointPayloadWithMeta(req.State, req.Outputs, req.NextStep, meta)
	if err != nil {
		return err
	}
	req.Record.Payload = payload
	req.Record.Status = status
	req.Record.OwnerID = ""
	req.Record.LeaseExpiresAt = time.Time{}
	req.Record.LeaseDuration = 0
	req.Record.ConfirmedSequence = req.EventCursor
	req.Record.PendingSequence = pendingEventSequence(req.PendingEvent)
	req.Record.ReplayStatus = req.ReplayStatus
	req.Record.UpdatedAt = time.Now()
	return c.store.Finish(ctx, req.Record, req.Record.Version)
}

func pendingEventSequence(event *gopact.Event) int64 {
	if event == nil {
		return 0
	}
	return event.Sequence
}

func (c *compiled[I, O]) isJoinTarget(target string) bool {
	node := c.nodes[target]
	return node != nil && (node.hasJoin() || node.hasMerge())
}

func (c *compiled[I, O]) addContribution(state *runState, source activation, target string, payload any) {
	bucket := state.joinBucket(target, source.correlation)
	bucket.contributions[source.node] = append(bucket.contributions[source.node], payload)
}

func (c *compiled[I, O]) materializeReadyJoins(ctx context.Context, state *runState) error {
	for {
		materialized, err := c.materializeJoinPass(ctx, state)
		if err != nil {
			return err
		}
		if !materialized {
			return nil
		}
	}
}

func (c *compiled[I, O]) materializeJoinPass(ctx context.Context, state *runState) (bool, error) {
	materialized := false
	for key, bucket := range state.buckets {
		if !c.joinReady(state, key, bucket) {
			continue
		}
		if err := c.materializeJoin(ctx, state, key, bucket); err != nil {
			return false, err
		}
		materialized = true
	}
	return materialized, nil
}

func (c *compiled[I, O]) materializeJoin(ctx context.Context, state *runState, key joinBucketKey, bucket *joinBucket) error {
	inputs := Inputs{contributions: copyContributions(bucket.contributions)}
	input, err := c.joinInput(ctx, key.target, inputs)
	if err != nil {
		return err
	}
	input, err = c.applyJoinMiddlewares(ctx, c.nodes[key.target], key.target, inputs, input)
	if err != nil {
		return err
	}
	c.enqueue(state, enqueueRequest{target: key.target, input: input, correlation: key.correlation})
	delete(state.buckets, key)
	return nil
}

func (c *compiled[I, O]) applyJoinMiddlewares(ctx context.Context, node runtimeNode, target string, inputs Inputs, input any) (any, error) {
	current := input
	for _, middleware := range c.joinMiddlewares {
		next, matched, err := middleware.run(ctx, node, target, inputs, current)
		if err != nil {
			return nil, err
		}
		if matched {
			current = next
		}
	}
	return current, nil
}

func (c *compiled[I, O]) joinReady(state *runState, key joinBucketKey, bucket *joinBucket) bool {
	preds := c.predecessors[key.target]
	if len(preds) == 0 || len(bucket.contributions) == 0 {
		return false
	}
	for _, pred := range preds {
		if !bucket.sourceReady(pred, c.pendingJoinActivations(state, key, pred)) {
			return false
		}
	}
	return true
}

func (c *compiled[I, O]) joinInput(ctx context.Context, target string, inputs Inputs) (any, error) {
	node := c.nodes[target]
	if node.hasJoin() {
		input, err := node.joinAny(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("workflow: join target %q: %w", target, err)
		}
		return input, nil
	}
	return inputs, nil
}

type enqueueRequest struct {
	target      string
	input       any
	sourceSet   string
	branchIndex int
	correlation CorrelationKey
}

func (c *compiled[I, O]) enqueue(state *runState, req enqueueRequest) activation {
	id := fmt.Sprintf("act-%d", state.nextActSeq)
	state.nextActSeq++
	item := activation{
		id: id, node: req.target, input: req.input, sourceSet: req.sourceSet,
		branchIndex: req.branchIndex, correlation: req.correlation,
	}
	state.queue = append(state.queue, item)
	state.trackActivation(item)
	state.scheduled[req.target]++
	state.trackCorrelation(item)
	if set := state.sourceSets[req.sourceSet]; set != nil {
		set.branches = append(set.branches, id)
	}
	return item
}

func validateActivationPayload(source, targetName string, payload any, target runtimeNode) error {
	payloadType := reflect.TypeOf(payload)
	if payloadType == nil || !payloadType.AssignableTo(target.inputType()) {
		return fmt.Errorf(
			"workflow: route from %q to %q payload %T is not assignable to input %s",
			source,
			targetName,
			payload,
			target.inputType(),
		)
	}
	return nil
}

type runState struct {
	queue           []activation
	activations     map[string]*activationRecord
	nextActSeq      int
	nextSetSeq      int
	nextCompletion  int64
	scheduled       map[string]int
	completed       map[string]int
	nodeVersions    map[string]int64
	buckets         map[joinBucketKey]*joinBucket
	correlations    map[CorrelationKey]map[string]int
	sourceSets      map[string]*sourceSet
	iterSources     map[string]*iterSource
	liveIters       map[string]*liveIterator
	nextIterSeq     int
	workflowContext any
	hasContext      bool
	contextRevision int64
}

func (state *runState) removeReady(id string) error {
	if len(state.queue) == 0 || state.queue[0].id != id {
		return fmt.Errorf("workflow: activation %q is not the next ready activation", id)
	}
	state.queue = state.queue[1:]
	return nil
}

func (state *runState) prioritize(current activation) {
	ready := make([]activation, 0, len(state.queue)+1)
	ready = append(ready, current)
	for _, item := range state.queue {
		if item.id != current.id {
			ready = append(ready, item)
		}
	}
	state.queue = ready
}

type joinBucket struct {
	contributions map[string][]any
	expectations  map[string]map[string]int
}

type activation struct {
	id          string
	node        string
	input       any
	sourceSet   string
	branchIndex int
	correlation CorrelationKey
}

type checkpointPayload[O any] struct {
	SchemaVersion      int
	WorkflowName       string
	TopologyVersion    string
	Queue              []checkpointActivation
	Activations        []checkpointActivationState
	NextActSeq         int
	NextSetSeq         int
	NextIterSeq        int
	NextCompletion     int64
	Scheduled          map[string]int
	Completed          map[string]int
	JoinBuckets        []checkpointJoinBucket
	Correlations       map[CorrelationKey]map[string]int
	SourceSets         []checkpointSourceSet
	IterSources        []checkpointIterSource
	Outputs            []O
	NextStep           int
	OwnerID            string
	LeaseExpiresAt     time.Time
	ClaimSequence      int64
	EventCursor        int64
	PendingEvent       *gopact.Event
	PendingTerm        CheckpointStatus
	PendingInterrupts  []checkpointInterrupt
	InterruptProgress  *checkpointInterruptProgress
	ResolvedInterrupts []checkpointInterruptResolution
	WorkflowContext    checkpointValue
	HasContext         bool
	ContextRevision    int64
	ExecutionEpoch     int64
	ControlOrigin      string
	SourceRevisionID   string
	SourceRunID        string
	SourceEventSeq     int64
}

type checkpointPayloadMeta struct {
	SchemaVersion      int
	WorkflowName       string
	TopologyVersion    string
	OwnerID            string
	LeaseExpiresAt     time.Time
	ClaimSequence      int64
	EventCursor        int64
	PendingEvent       *gopact.Event
	PendingTerm        CheckpointStatus
	PendingInterrupts  []checkpointInterrupt
	InterruptProgress  *checkpointInterruptProgress
	ResolvedInterrupts []checkpointInterruptResolution
	ExecutionEpoch     int64
	ControlOrigin      string
	SourceRevisionID   string
	SourceRunID        string
	SourceEventSeq     int64
}

type checkpointActivation struct {
	ID           string
	Node         string
	Input        any
	SourceSet    string
	BranchIndex  int
	Correlation  CorrelationKey
	JoinInput    map[string][]any
	HasJoinInput bool
}

type checkpointInterrupt struct {
	Request            InterruptRequest
	GuardName          string
	Phase              GuardPhase
	NodeName           string
	ActivationID       string
	ChildRunID         string
	ChildCheckpointID  string
	CandidateOutput    any
	HasCandidateOutput bool
}

type checkpointInterruptProgress struct {
	Events []gopact.Event
	Next   int
}

type checkpointInterruptResolution struct {
	InterruptID        string
	PayloadRef         string
	GuardName          string
	Phase              GuardPhase
	NodeName           string
	ActivationID       string
	ChildRunID         string
	ChildCheckpointID  string
	CandidateOutput    any
	HasCandidateOutput bool
}

func encodeCheckpointPayloadWithMeta[O any](state runState, outputs []O, nextStep int, meta checkpointPayloadMeta) ([]byte, error) {
	payload := checkpointPayload[O]{
		SchemaVersion:      meta.SchemaVersion,
		WorkflowName:       meta.WorkflowName,
		TopologyVersion:    meta.TopologyVersion,
		Queue:              make([]checkpointActivation, 0, len(state.queue)),
		Activations:        state.checkpointActivations(),
		NextActSeq:         state.nextActSeq,
		NextSetSeq:         state.nextSetSeq,
		NextIterSeq:        state.nextIterSeq,
		NextCompletion:     state.nextCompletion,
		Scheduled:          copyIntMap(state.scheduled),
		Completed:          copyIntMap(state.completed),
		JoinBuckets:        checkpointJoinBuckets(state.buckets),
		Correlations:       copyCorrelationCounts(state.correlations),
		SourceSets:         state.checkpointSourceSets(),
		IterSources:        state.checkpointIterSources(),
		Outputs:            append([]O(nil), outputs...),
		NextStep:           nextStep,
		OwnerID:            meta.OwnerID,
		LeaseExpiresAt:     meta.LeaseExpiresAt,
		ClaimSequence:      meta.ClaimSequence,
		EventCursor:        meta.EventCursor,
		PendingEvent:       copyCheckpointEvent(meta.PendingEvent),
		PendingTerm:        meta.PendingTerm,
		PendingInterrupts:  copyCheckpointInterrupts(meta.PendingInterrupts),
		InterruptProgress:  copyCheckpointInterruptProgress(meta.InterruptProgress),
		ResolvedInterrupts: copyCheckpointInterruptResolutions(meta.ResolvedInterrupts),
		WorkflowContext:    newCheckpointValue(state.workflowContext),
		HasContext:         state.hasContext,
		ContextRevision:    state.contextRevision,
		ExecutionEpoch:     meta.ExecutionEpoch,
		ControlOrigin:      meta.ControlOrigin,
		SourceRevisionID:   meta.SourceRevisionID,
		SourceRunID:        meta.SourceRunID,
		SourceEventSeq:     meta.SourceEventSeq,
	}
	for _, item := range state.queue {
		payload.Queue = append(payload.Queue, item.checkpoint())
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(payload); err != nil {
		return nil, fmt.Errorf("workflow: encode checkpoint payload: %w", err)
	}
	if buf.Len() > maxWorkflowCheckpointPayloadBytes {
		return nil, errors.New("workflow: checkpoint payload is too large")
	}
	return buf.Bytes(), nil
}

func decodeCheckpointPayload[O any](payload []byte) (checkpointPayload[O], error) {
	var decoded checkpointPayload[O]
	if len(payload) == 0 {
		return decoded, errors.New("workflow: checkpoint payload is empty")
	}
	if len(payload) > maxWorkflowCheckpointPayloadBytes {
		return decoded, errors.New("workflow: checkpoint payload is too large")
	}
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&decoded); err != nil {
		return decoded, fmt.Errorf("workflow: decode checkpoint payload: %w", err)
	}
	return decoded, nil
}

func decodeCheckpointPayloadMeta[O any](payload []byte) (checkpointPayloadMeta, error) {
	decoded, err := decodeCheckpointPayload[O](payload)
	if err != nil {
		return checkpointPayloadMeta{}, err
	}
	return decoded.meta(), nil
}

func (p checkpointPayload[O]) meta() checkpointPayloadMeta {
	return checkpointPayloadMeta{
		SchemaVersion:      p.SchemaVersion,
		WorkflowName:       p.WorkflowName,
		TopologyVersion:    p.TopologyVersion,
		OwnerID:            p.OwnerID,
		LeaseExpiresAt:     p.LeaseExpiresAt,
		ClaimSequence:      p.ClaimSequence,
		EventCursor:        p.EventCursor,
		PendingEvent:       copyCheckpointEvent(p.PendingEvent),
		PendingTerm:        p.PendingTerm,
		PendingInterrupts:  copyCheckpointInterrupts(p.PendingInterrupts),
		InterruptProgress:  copyCheckpointInterruptProgress(p.InterruptProgress),
		ResolvedInterrupts: copyCheckpointInterruptResolutions(p.ResolvedInterrupts),
		ExecutionEpoch:     p.ExecutionEpoch,
		ControlOrigin:      p.ControlOrigin,
		SourceRevisionID:   p.SourceRevisionID,
		SourceRunID:        p.SourceRunID,
		SourceEventSeq:     p.SourceEventSeq,
	}
}

func copyCheckpointEvent(event *gopact.Event) *gopact.Event {
	if event == nil {
		return nil
	}
	copied := *event
	copied.Payload = append([]byte(nil), event.Payload...)
	return &copied
}

func copyCheckpointInterrupts(interrupts []checkpointInterrupt) []checkpointInterrupt {
	return append([]checkpointInterrupt(nil), interrupts...)
}

func copyCheckpointInterruptProgress(progress *checkpointInterruptProgress) *checkpointInterruptProgress {
	if progress == nil {
		return nil
	}
	copied := &checkpointInterruptProgress{Next: progress.Next, Events: make([]gopact.Event, len(progress.Events))}
	for index := range progress.Events {
		copied.Events[index] = *copyCheckpointEvent(&progress.Events[index])
	}
	return copied
}

func copyCheckpointInterruptResolutions(resolutions []checkpointInterruptResolution) []checkpointInterruptResolution {
	return append([]checkpointInterruptResolution(nil), resolutions...)
}

func (p checkpointPayload[O]) state() runState {
	state := runState{
		queue:           make([]activation, 0, len(p.Queue)),
		activations:     map[string]*activationRecord{},
		nextActSeq:      p.NextActSeq,
		nextSetSeq:      p.NextSetSeq,
		nextIterSeq:     p.NextIterSeq,
		nextCompletion:  p.NextCompletion,
		scheduled:       copyIntMap(p.Scheduled),
		completed:       copyIntMap(p.Completed),
		nodeVersions:    map[string]int64{},
		buckets:         map[joinBucketKey]*joinBucket{},
		correlations:    copyCorrelationCounts(p.Correlations),
		sourceSets:      map[string]*sourceSet{},
		iterSources:     map[string]*iterSource{},
		liveIters:       map[string]*liveIterator{},
		workflowContext: p.WorkflowContext.runtime(),
		hasContext:      p.HasContext,
		contextRevision: p.ContextRevision,
	}
	if state.hasContext && state.contextRevision <= 0 {
		state.contextRevision = 1
	}
	state.restoreActivations(p.Activations)
	for i, item := range p.Queue {
		current := item.runtime()
		if current.id == "" {
			current.id = fmt.Sprintf("act-%d", i+1)
		}
		state.queue = append(state.queue, current)
		state.trackActivation(current)
	}
	state.restoreReadyCorrelations()
	if state.nextActSeq <= 0 {
		state.nextActSeq = len(state.queue) + 1
	}
	state.restoreJoinBuckets(p.JoinBuckets)
	state.restoreSourceSets(p.SourceSets)
	state.restoreIterSources(p.IterSources)
	return state
}

func (n *Node[I, O]) endpointName() string {
	if n == nil {
		return ""
	}
	return n.name
}

func (n *Node[I, O]) inputType() reflect.Type {
	return typeOf[I]()
}

func (n *Node[I, O]) outputType() reflect.Type {
	return typeOf[O]()
}

func (n *Node[I, O]) runAny(ctx context.Context, input any, middlewares []erasedNodeMiddleware, opts ...gopact.RunOption) (nodeRunResult, error) {
	typed, ok := input.(I)
	if !ok {
		return nodeRunResult{}, fmt.Errorf("input type mismatch: got %T, want %s", input, typeOf[I]())
	}
	return n.runGuardedResult(ctx, typed, middlewares, opts...)
}

func (n *Node[I, O]) joinAny(ctx context.Context, inputs Inputs) (any, error) {
	if n.join == nil {
		return inputs, nil
	}
	return n.join(ctx, inputs)
}

func (n *Node[I, O]) routeAny(ctx context.Context, output any) (Dispatch, error) {
	if n.route == nil {
		return Dispatch{}, nil
	}
	typed, ok := output.(O)
	if !ok {
		return Dispatch{}, fmt.Errorf("output type mismatch: got %T, want %s", output, typeOf[O]())
	}
	return n.route(ctx, typed)
}

func (n *Node[I, O]) hasRoute() bool {
	return n != nil && n.route != nil
}

func (n *Node[I, O]) hasJoin() bool {
	return n != nil && n.join != nil
}

func (n *Node[I, O]) hasMerge() bool {
	return n != nil && n.merge
}

func (in Inputs) lookup(source endpoint) (any, bool, error) {
	values, ok, err := in.lookupAll(source)
	if err != nil || !ok {
		return nil, ok, err
	}
	if len(values) != 1 {
		return nil, true, fmt.Errorf(
			"workflow: input from %q has %d contributions, want 1",
			source.endpointName(),
			len(values),
		)
	}
	return values[0], true, nil
}

func (in Inputs) lookupAll(source endpoint) ([]any, bool, error) {
	if source == nil {
		return nil, false, errors.New("workflow: input source is nil")
	}
	values := in.contributions[source.endpointName()]
	if len(values) == 0 {
		return nil, false, nil
	}
	return append([]any(nil), values...), true, nil
}

func (in Inputs) one(source endpoint) (any, error) {
	value, ok, err := in.lookup(source)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workflow: input from %q is missing", source.endpointName())
	}
	return value, nil
}

func (in Inputs) all(source endpoint) ([]any, error) {
	values, ok, err := in.lookupAll(source)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workflow: input from %q is missing", source.endpointName())
	}
	return values, nil
}

type eventDelivery struct {
	event gopact.Event
}

func (delivery eventDelivery) emit(ctx context.Context, sinks []gopact.EventSink) error {
	event := delivery.event
	if ctx != nil {
		ctx = eventSinkContext{Context: ctx}
	}
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		if err := emitEventSink(ctx, sink, event); err != nil && gopact.IsStrictEventSink(sink) {
			return fmt.Errorf("workflow: emit event: %w", err)
		}
	}
	return nil
}

type eventSinkContext struct{ context.Context }

func (ctx eventSinkContext) Value(key any) any {
	if _, internalEmitter := key.(eventEmitterContextKey); internalEmitter {
		return nil
	}
	return ctx.Context.Value(key)
}

func applyEventSinkWrappers(sinks []gopact.EventSink, wrappers []EventSinkWrapper) []gopact.EventSink {
	if len(sinks) == 0 || len(wrappers) == 0 {
		return sinks
	}
	out := make([]gopact.EventSink, 0, len(sinks))
	for _, sink := range sinks {
		strict := gopact.IsStrictEventSink(sink)
		wrapped := sink
		for i := len(wrappers) - 1; i >= 0; i-- {
			wrapped = wrappers[i](wrapped)
		}
		if strict {
			wrapped = strictWorkflowEventSink{EventSink: wrapped}
		}
		out = append(out, wrapped)
	}
	return out
}

func associateEventSinks(sinks []gopact.EventSink, association runlog.Association) []gopact.EventSink {
	associated := append([]gopact.EventSink(nil), sinks...)
	for index, sink := range associated {
		if target, ok := sink.(runlog.AssociatingSink); ok {
			associated[index] = target.Associate(association)
		}
	}
	return associated
}

type strictWorkflowEventSink struct {
	gopact.EventSink
}

func (strictWorkflowEventSink) StrictEventDelivery() {}

func emitEventSink(ctx context.Context, sink gopact.EventSink, event gopact.Event) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("event sink panic: %v", recovered)
		}
	}()
	return sink.Emit(ctx, event)
}

func typeOf[T any]() reflect.Type {
	var value T
	return reflect.TypeOf((*T)(&value)).Elem()
}

func copyNodes(in map[string]runtimeNode) map[string]runtimeNode {
	out := make(map[string]runtimeNode, len(in))
	for name, node := range in {
		out[name] = node
	}
	return out
}

func copyEdges(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for source, targets := range in {
		out[source] = append([]string(nil), targets...)
	}
	return out
}

func appendStringOnce(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func copyContributions(in map[string][]any) map[string][]any {
	out := make(map[string][]any, len(in))
	for source, values := range in {
		out[source] = append([]any(nil), values...)
	}
	return out
}

func copyIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyExitSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for name := range in {
		out[name] = struct{}{}
	}
	return out
}
