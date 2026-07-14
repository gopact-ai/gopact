package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

var (
	// ErrRunTerminated reports an explicit terminal control command.
	ErrRunTerminated = errors.New("workflow: run terminated by control")
	// ErrRunNotActive reports cancellation of a run not executing in this process.
	ErrRunNotActive = errors.New("workflow: run is not active")
	// ErrRunActive reports a duplicate active RunID on one Workflow.
	ErrRunActive = errors.New("workflow: run is already active")
)

// RetryRequest selects one historical node Attempt from a failed Run.
type RetryRequest struct {
	RunID                string
	NodeID               string
	NodeExecutionVersion int64
}

// JumpRequest selects a historical context revision for an external jump.
type JumpRequest struct {
	RunID          string
	FromRevisionID string
	ContextPatch   any
}

// JumpTo creates a new Run from one failed Run revision.
func (wf *Workflow[I, O]) JumpTo[NI, NO any](ctx context.Context, target *Node[NI, NO], request JumpRequest, input NI, opts ...gopact.RunOption) (O, error) {
	var zero O
	compiled, err := wf.compile()
	if err != nil {
		return zero, err
	}
	return compiled.JumpTo(ctx, target, request, input, opts...)
}

// JumpTo creates a new Activation for target in the same Run.
func (c *compiled[I, O]) JumpTo[NI, NO any](ctx context.Context, target *Node[NI, NO], request JumpRequest, input NI, opts ...gopact.RunOption) (O, error) {
	var zero O
	if c == nil {
		return zero, errNilCompiled
	}
	if target == nil || c.nodes[target.endpointName()] != target {
		return zero, errors.New("workflow: jump target does not belong to this workflow")
	}
	start, err := c.prepareJump(ctx, target.endpointName(), input, request)
	if err != nil {
		return zero, err
	}
	options := c.controlOptions(opts, start)
	var workflowInput I
	return c.Invoke(ctx, workflowInput, options...)
}

// Terminate irreversibly stops an active run in this process.
func (wf *Workflow[I, O]) Terminate(runID string) error {
	compiled, err := wf.compile()
	if err != nil {
		return err
	}
	return compiled.terminate(runID)
}

func (c *compiled[I, O]) terminate(runID string) error {
	if c == nil {
		return errNilCompiled
	}
	if runID == "" {
		return errors.New("workflow: cancel run id is required")
	}
	c.activeMu.Lock()
	cancel := c.activeRuns[runID]
	c.activeMu.Unlock()
	if cancel == nil {
		return ErrRunNotActive
	}
	cancel(errors.Join(context.Canceled, ErrRunTerminated))
	return nil
}

func (c *compiled[I, O]) registerRun(runID string, cancel context.CancelCauseFunc) error {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeRuns == nil {
		c.activeRuns = make(map[string]context.CancelCauseFunc)
	}
	if c.activeRuns[runID] != nil {
		return ErrRunActive
	}
	c.activeRuns[runID] = cancel
	return nil
}

func (c *compiled[I, O]) unregisterRun(runID string) {
	c.activeMu.Lock()
	delete(c.activeRuns, runID)
	c.activeMu.Unlock()
}

// Retry creates a new Run from one historical Activation in a failed Run.
func (wf *Workflow[I, O]) Retry(ctx context.Context, request RetryRequest, opts ...gopact.RunOption) (O, error) {
	var zero O
	compiled, err := wf.compile()
	if err != nil {
		return zero, err
	}
	return compiled.Retry(ctx, request, opts...)
}

// Retry creates a new Run from one historical Activation in a failed Run.
func (c *compiled[I, O]) Retry(ctx context.Context, request RetryRequest, opts ...gopact.RunOption) (O, error) {
	var zero O
	if c == nil {
		return zero, errNilCompiled
	}
	start, err := c.prepareRetry(ctx, request)
	if err != nil {
		return zero, err
	}
	options := c.controlOptions(opts, start)
	var input I
	return c.Invoke(ctx, input, options...)
}

type controlStart struct {
	payload     []byte
	sessionID   string
	association runlog.Association
}

type controlStartOption struct{ start controlStart }

func (option controlStartOption) ApplyRunOption(config *gopact.RunConfig) {
	if config.Extensions == nil {
		config.Extensions = make(map[string]any)
	}
	config.Extensions[controlStartExtensionKey] = option.start
	config.Extensions[sourceAssociationExtensionKey] = option.start.association
}

func (c *compiled[I, O]) controlOptions(options []gopact.RunOption, start controlStart) []gopact.RunOption {
	result := append([]gopact.RunOption(nil), options...)
	return append(result, gopact.WithSessionID(start.sessionID), controlStartOption{start: start})
}

func (c *compiled[I, O]) prepareRetry(ctx context.Context, request RetryRequest) (controlStart, error) {
	if request.RunID == "" || request.NodeID == "" || request.NodeExecutionVersion <= 0 {
		return controlStart{}, errors.New("workflow: retry requires run id, node id, and positive node execution version")
	}
	history, ok := c.checkpointer.(CheckpointHistory)
	if !ok {
		return controlStart{}, errors.New("workflow: checkpointer does not provide checkpoint history")
	}
	source, err := c.retrySource(ctx, request)
	if err != nil {
		return controlStart{}, err
	}
	sourceCheckpoint, ok, err := checkpointAtSequence(ctx, history, request.RunID, source.Sequence)
	if err != nil {
		return controlStart{}, err
	}
	if !ok {
		return controlStart{}, fmt.Errorf("workflow: retry source revision %q has no stable checkpoint", source.RevisionID)
	}
	head, err := c.checkpointer.Load(ctx, request.RunID)
	if err != nil {
		return controlStart{}, err
	}
	if head.Status != CheckpointFailed {
		return controlStart{}, fmt.Errorf("workflow: retry requires a failed run, current status %q", head.Status)
	}
	payload, err := c.retryPayload(source, sourceCheckpoint, head)
	if err != nil {
		return controlStart{}, err
	}
	return controlStart{
		payload: payload, sessionID: head.SessionID,
		association: runlog.Association{SourceRunID: request.RunID, SourceEventSeq: source.Sequence},
	}, nil
}

func (c *compiled[I, O]) prepareJump(ctx context.Context, target string, input any, request JumpRequest) (controlStart, error) {
	if request.RunID == "" || request.FromRevisionID == "" {
		return controlStart{}, errors.New("workflow: jump requires run id and source revision id")
	}
	history, ok := c.checkpointer.(CheckpointHistory)
	if !ok {
		return controlStart{}, errors.New("workflow: checkpointer does not provide checkpoint history")
	}
	source, err := c.controlSource(ctx, request.RunID, request.FromRevisionID)
	if err != nil {
		return controlStart{}, err
	}
	sourceCheckpoint, ok, err := checkpointAtSequence(ctx, history, request.RunID, source.Sequence)
	if err != nil {
		return controlStart{}, err
	}
	if !ok {
		return controlStart{}, fmt.Errorf("workflow: jump source revision %q has no stable checkpoint", source.RevisionID)
	}
	head, err := c.checkpointer.Load(ctx, request.RunID)
	if err != nil {
		return controlStart{}, err
	}
	if head.Status != CheckpointFailed {
		return controlStart{}, fmt.Errorf("workflow: jump requires a failed run, current status %q", head.Status)
	}
	payload, err := c.jumpPayload(jumpPayloadRequest{
		source: source, checkpoint: sourceCheckpoint, head: head,
		target: target, input: input, contextPatch: request.ContextPatch,
	})
	if err != nil {
		return controlStart{}, err
	}
	return controlStart{
		payload: payload, sessionID: head.SessionID,
		association: runlog.Association{SourceRunID: request.RunID, SourceEventSeq: source.Sequence},
	}, nil
}

type jumpPayloadRequest struct {
	source           runlog.Record
	checkpoint, head CheckpointRecord
	target           string
	input            any
	contextPatch     any
}

func (c *compiled[I, O]) jumpPayload(request jumpPayloadRequest) ([]byte, error) {
	sourcePayload, err := decodeCheckpointPayload[O](request.checkpoint.Payload)
	if err != nil {
		return nil, err
	}
	headPayload, err := decodeCheckpointPayload[O](request.head.Payload)
	if err != nil {
		return nil, err
	}
	if err := c.validateCheckpointIdentity(sourcePayload); err != nil {
		return nil, err
	}
	if err := c.validateCheckpointIdentity(headPayload); err != nil {
		return nil, err
	}
	state := sourcePayload.state()
	headState := headPayload.state()
	mergeRetryHead(&state, headState)
	if err := c.patchJumpContext(&state, request.contextPatch); err != nil {
		return nil, err
	}
	state.contextRevision++
	state.queue = nil
	state.activations = headState.activations
	state.scheduled = make(map[string]int)
	state.completed = make(map[string]int)
	state.buckets = make(map[joinBucketKey]*joinBucket)
	state.correlations = make(map[CorrelationKey]map[string]int)
	state.sourceSets = make(map[string]*sourceSet)
	state.iterSources = make(map[string]*iterSource)
	state.liveIters = make(map[string]*liveIterator)
	resetControlAttempts(&state)
	activation := c.enqueue(&state, enqueueRequest{
		target: request.target, input: request.input,
		correlation: CorrelationKey{ID: request.source.RunID, Epoch: initialCorrelationEpoch},
	})
	state.activations[activation.id].origin = "external_jump"
	meta := headPayload.meta()
	meta.OwnerID = ""
	meta.LeaseExpiresAt = time.Time{}
	meta.EventCursor = 0
	meta.PendingEvent = nil
	meta.PendingTerm = ""
	meta.PendingInterrupts = nil
	meta.ResolvedInterrupts = nil
	meta.ExecutionEpoch = 1
	meta.ControlOrigin = "external_jump"
	meta.SourceRevisionID = request.source.RevisionID
	meta.SourceRunID = request.source.RunID
	meta.SourceEventSeq = request.source.Sequence
	return encodeCheckpointPayloadWithMeta[O](state, nil, 1, meta)
}

func (c *compiled[I, O]) patchJumpContext(state *runState, patch any) error {
	if patch == nil {
		return nil
	}
	expected := c.contextType
	if expected == nil {
		expected = typeOf[I]()
	}
	actual := reflect.TypeOf(patch)
	if actual == nil || !actual.AssignableTo(expected) {
		return fmt.Errorf("workflow: jump context type %T does not match %s", patch, expected)
	}
	state.workflowContext = patch
	state.hasContext = true
	return nil
}

func (c *compiled[I, O]) controlSource(ctx context.Context, runID, revisionID string) (runlog.Record, error) {
	record, found, err := findRunLogRecord(ctx, c.journal, runID, func(record runlog.Record) bool {
		return record.RevisionID == revisionID
	})
	if err != nil {
		return runlog.Record{}, err
	}
	if found {
		return record, nil
	}
	return runlog.Record{}, errors.New("workflow: source revision was not found")
}

func (c *compiled[I, O]) retrySource(ctx context.Context, request RetryRequest) (runlog.Record, error) {
	record, found, err := findRunLogRecord(ctx, c.journal, request.RunID, func(record runlog.Record) bool {
		return record.EventType == EventNodeStarted && record.NodeID == request.NodeID &&
			record.NodeExecutionVersion == request.NodeExecutionVersion
	})
	if err != nil {
		return runlog.Record{}, err
	}
	if found {
		return record, nil
	}
	return runlog.Record{}, errors.New("workflow: retry source node execution was not found")
}

type runLogMatcher func(runlog.Record) bool

type runLogSearch struct {
	runID string
	match runLogMatcher
	after int64
	total int
}

func findRunLogRecord(ctx context.Context, log runlog.Log, runID string, match runLogMatcher) (runlog.Record, bool, error) {
	if log == nil {
		return runlog.Record{}, false, runlog.ErrNilLog
	}
	search := runLogSearch{runID: runID, match: match}
	for {
		limit := min(historyPageSize, defaultHistoryRecordLimit-search.total+1)
		records, err := log.List(ctx, runlog.Query{RunID: runID, After: search.after, Limit: limit})
		if err != nil {
			return runlog.Record{}, false, err
		}
		if len(records) > limit {
			return runlog.Record{}, false, fmt.Errorf("workflow: runlog returned %d records for limit %d", len(records), limit)
		}
		if len(records) == 0 {
			return runlog.Record{}, false, nil
		}
		record, found, err := search.accept(records)
		if err != nil || found {
			return record, found, err
		}
		if len(records) < limit {
			return runlog.Record{}, false, nil
		}
	}
}

func (search *runLogSearch) accept(records []runlog.Record) (runlog.Record, bool, error) {
	for _, record := range records {
		search.total++
		if search.total > defaultHistoryRecordLimit {
			return runlog.Record{}, false, fmt.Errorf(
				"%w: run %q exceeds %d records",
				ErrHistoryLimitExceeded,
				search.runID,
				defaultHistoryRecordLimit,
			)
		}
		if record.RunID != search.runID || record.Sequence != search.after+1 {
			return runlog.Record{}, false, fmt.Errorf("workflow: runlog history is not contiguous after sequence %d", search.after)
		}
		search.after = record.Sequence
		if search.match(record) {
			return record, true, nil
		}
	}
	return runlog.Record{}, false, nil
}

func (c *compiled[I, O]) retryPayload(source runlog.Record, checkpoint, head CheckpointRecord) ([]byte, error) {
	var nodeFacts NodeEventPayload
	if err := json.Unmarshal(source.Payload, &nodeFacts); err != nil {
		return nil, fmt.Errorf("workflow: decode retry source facts: %w", err)
	}
	sourcePayload, err := decodeCheckpointPayload[O](checkpoint.Payload)
	if err != nil {
		return nil, err
	}
	headPayload, err := decodeCheckpointPayload[O](head.Payload)
	if err != nil {
		return nil, err
	}
	if err := c.validateCheckpointIdentity(sourcePayload); err != nil {
		return nil, err
	}
	if err := c.validateCheckpointIdentity(headPayload); err != nil {
		return nil, err
	}
	state := sourcePayload.state()
	record := state.activations[nodeFacts.ActivationID]
	if record == nil || record.activation.node != source.NodeID {
		return nil, errors.New("workflow: retry source activation is missing")
	}
	if record.activation.sourceSet != "" {
		return nil, errors.New("workflow: retry of a fan-out branch is not supported")
	}
	record.phase = activationReady
	record.origin = "external_retry"
	record.result = nodeRunResult{}
	record.hasResult = false
	record.cause = nil
	headState := headPayload.state()
	state.activations = headState.activations
	state.activations[nodeFacts.ActivationID] = record
	state.queue = []activation{record.activation}
	mergeRetryHead(&state, headState)
	resetControlAttempts(&state)
	state.contextRevision++
	meta := headPayload.meta()
	meta.OwnerID = ""
	meta.LeaseExpiresAt = time.Time{}
	meta.EventCursor = 0
	meta.PendingEvent = nil
	meta.PendingTerm = ""
	meta.PendingInterrupts = nil
	meta.ResolvedInterrupts = nil
	meta.ExecutionEpoch = 1
	meta.ControlOrigin = "external_retry"
	meta.SourceRevisionID = source.RevisionID
	meta.SourceRunID = source.RunID
	meta.SourceEventSeq = source.Sequence
	return encodeCheckpointPayloadWithMeta(state, sourcePayload.Outputs, 1, meta)
}

func resetControlAttempts(state *runState) {
	state.nodeVersions = make(map[string]int64)
	for _, record := range state.activations {
		record.attempt = 0
		record.nodeExecutionVersion = 0
	}
}

type checkpointSearch struct {
	runID        string
	sequence     int64
	afterVersion int64
	total        int
	selected     CheckpointRecord
}

func checkpointAtSequence(ctx context.Context, history CheckpointHistory, runID string, sequence int64) (CheckpointRecord, bool, error) {
	search := checkpointSearch{runID: runID, sequence: sequence}
	for {
		limit := min(historyPageSize, defaultHistoryRecordLimit-search.total+1)
		records, err := history.ListCheckpoints(ctx, CheckpointHistoryRequest{
			RunID: runID, AfterVersion: search.afterVersion, Limit: limit,
		})
		if err != nil {
			return CheckpointRecord{}, false, err
		}
		if len(records) > limit {
			return CheckpointRecord{}, false, fmt.Errorf("workflow: checkpoint history returned %d records for limit %d", len(records), limit)
		}
		if len(records) == 0 {
			return search.result()
		}
		if err := search.accept(records); err != nil {
			return CheckpointRecord{}, false, err
		}
		if len(records) < limit {
			return search.result()
		}
	}
}

func (search *checkpointSearch) accept(records []CheckpointRecord) error {
	for _, record := range records {
		search.total++
		if search.total > defaultHistoryRecordLimit {
			return fmt.Errorf(
				"%w: run %q exceeds %d checkpoints",
				ErrHistoryLimitExceeded,
				search.runID,
				defaultHistoryRecordLimit,
			)
		}
		if record.RunID != search.runID || record.Version <= search.afterVersion {
			return fmt.Errorf("%w: checkpoint history is not ordered after version %d", ErrCheckpointMismatch, search.afterVersion)
		}
		search.afterVersion = record.Version
		if record.ConfirmedSequence == search.sequence && record.PendingSequence == 0 {
			search.selected = record
		}
	}
	return nil
}

func (search checkpointSearch) result() (CheckpointRecord, bool, error) {
	return search.selected, search.selected.ID != "", nil
}

func mergeRetryHead(state *runState, head runState) {
	state.nextActSeq = max(state.nextActSeq, head.nextActSeq)
	state.nextSetSeq = max(state.nextSetSeq, head.nextSetSeq)
	state.nextIterSeq = max(state.nextIterSeq, head.nextIterSeq)
	state.nextCompletion = max(state.nextCompletion, head.nextCompletion)
	state.contextRevision = max(state.contextRevision, head.contextRevision)
	for node, version := range head.nodeVersions {
		state.nodeVersions[node] = max(state.nodeVersions[node], version)
	}
}
