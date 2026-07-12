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

// RetryRequest selects one historical node Attempt to retry in the same Run.
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

// JumpTo creates a new Activation for target in the same Run.
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
	if err := c.prepareJump(ctx, target.endpointName(), input, request); err != nil {
		return zero, err
	}
	options := append([]gopact.RunOption(nil), opts...)
	options = append(options, WithResume(ResumeRequest{RunID: request.RunID}))
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

// Retry retries one historical Activation without creating a new Run.
func (wf *Workflow[I, O]) Retry(ctx context.Context, request RetryRequest, opts ...gopact.RunOption) (O, error) {
	var zero O
	compiled, err := wf.compile()
	if err != nil {
		return zero, err
	}
	return compiled.Retry(ctx, request, opts...)
}

// Retry retries one historical Activation without creating a new Run.
func (c *compiled[I, O]) Retry(ctx context.Context, request RetryRequest, opts ...gopact.RunOption) (O, error) {
	var zero O
	if c == nil {
		return zero, errNilCompiled
	}
	if err := c.prepareRetry(ctx, request); err != nil {
		return zero, err
	}
	options := append([]gopact.RunOption(nil), opts...)
	options = append(options, WithResume(ResumeRequest{RunID: request.RunID}))
	var input I
	return c.Invoke(ctx, input, options...)
}

func (c *compiled[I, O]) prepareRetry(ctx context.Context, request RetryRequest) error {
	if request.RunID == "" || request.NodeID == "" || request.NodeExecutionVersion <= 0 {
		return errors.New("workflow: retry requires run id, node id, and positive node execution version")
	}
	controller, ok := c.checkpointer.(CheckpointController)
	if !ok {
		return errors.New("workflow: checkpointer does not support control")
	}
	history, ok := c.checkpointer.(CheckpointHistory)
	if !ok {
		return errors.New("workflow: checkpointer does not provide checkpoint history")
	}
	source, err := c.retrySource(ctx, request)
	if err != nil {
		return err
	}
	checkpoints, err := history.ListCheckpoints(ctx, CheckpointHistoryRequest{RunID: request.RunID})
	if err != nil {
		return err
	}
	sourceCheckpoint, ok := checkpointAtSequence(checkpoints, source.Sequence)
	if !ok {
		return fmt.Errorf("workflow: retry source revision %q has no stable checkpoint", source.RevisionID)
	}
	head, err := c.checkpointer.Load(ctx, request.RunID)
	if err != nil {
		return err
	}
	if !checkpointTerminal(head.Status) {
		return fmt.Errorf("workflow: retry requires a terminal run, current status %q", head.Status)
	}
	payload, err := c.retryPayload(source, sourceCheckpoint, head)
	if err != nil {
		return err
	}
	next := head
	next.Status = CheckpointRunning
	next.Payload = payload
	next.ConfirmedSequence = head.ConfirmedSequence
	next.PendingSequence = 0
	next.ReplayStatus = ReplayUnknown
	next.UpdatedAt = time.Now()
	return controller.Reopen(ctx, next, head.Version)
}

func (c *compiled[I, O]) prepareJump(ctx context.Context, target string, input any, request JumpRequest) error {
	if request.RunID == "" || request.FromRevisionID == "" {
		return errors.New("workflow: jump requires run id and source revision id")
	}
	controller, ok := c.checkpointer.(CheckpointController)
	if !ok {
		return errors.New("workflow: checkpointer does not support control")
	}
	history, ok := c.checkpointer.(CheckpointHistory)
	if !ok {
		return errors.New("workflow: checkpointer does not provide checkpoint history")
	}
	source, err := c.controlSource(ctx, request.RunID, request.FromRevisionID)
	if err != nil {
		return err
	}
	checkpoints, err := history.ListCheckpoints(ctx, CheckpointHistoryRequest{RunID: request.RunID})
	if err != nil {
		return err
	}
	sourceCheckpoint, ok := checkpointAtSequence(checkpoints, source.Sequence)
	if !ok {
		return fmt.Errorf("workflow: jump source revision %q has no stable checkpoint", source.RevisionID)
	}
	head, err := c.checkpointer.Load(ctx, request.RunID)
	if err != nil {
		return err
	}
	if !checkpointTerminal(head.Status) {
		return fmt.Errorf("workflow: jump requires a terminal run, current status %q", head.Status)
	}
	payload, err := c.jumpPayload(jumpPayloadRequest{
		source: source, checkpoint: sourceCheckpoint, head: head,
		target: target, input: input, contextPatch: request.ContextPatch,
	})
	if err != nil {
		return err
	}
	next := head
	next.Status = CheckpointRunning
	next.Payload = payload
	next.ConfirmedSequence = head.ConfirmedSequence
	next.PendingSequence = 0
	next.ReplayStatus = ReplayUnknown
	next.UpdatedAt = time.Now()
	return controller.Reopen(ctx, next, head.Version)
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
	epoch := max(int64(1), headPayload.ExecutionEpoch) + 1
	activation := c.enqueue(&state, enqueueRequest{
		target: request.target, input: request.input,
		correlation: CorrelationKey{ID: request.source.RunID, Epoch: int(epoch)},
	})
	state.activations[activation.id].origin = "external_jump"
	meta := headPayload.meta()
	meta.OwnerID = ""
	meta.LeaseExpiresAt = time.Time{}
	meta.EventCursor = headPayload.EventCursor
	meta.PendingEvent = nil
	meta.PendingTerm = ""
	meta.PendingInterrupts = nil
	meta.ResolvedInterrupts = nil
	meta.ExecutionEpoch = epoch
	meta.ControlOrigin = "external_jump"
	meta.SourceRevisionID = request.source.RevisionID
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
	records, err := c.journal.List(ctx, runlog.Query{RunID: runID})
	if err != nil {
		return runlog.Record{}, err
	}
	for _, record := range records {
		if record.RevisionID == revisionID {
			return record, nil
		}
	}
	return runlog.Record{}, errors.New("workflow: source revision was not found")
}

func (c *compiled[I, O]) retrySource(ctx context.Context, request RetryRequest) (runlog.Record, error) {
	records, err := c.journal.List(ctx, runlog.Query{RunID: request.RunID})
	if err != nil {
		return runlog.Record{}, err
	}
	for _, record := range records {
		if record.EventType == EventNodeStarted && record.NodeID == request.NodeID &&
			record.NodeExecutionVersion == request.NodeExecutionVersion {
			return record, nil
		}
	}
	return runlog.Record{}, errors.New("workflow: retry source node execution was not found")
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
	state.contextRevision++
	meta := headPayload.meta()
	meta.OwnerID = ""
	meta.LeaseExpiresAt = time.Time{}
	meta.EventCursor = headPayload.EventCursor
	meta.PendingEvent = nil
	meta.PendingTerm = ""
	meta.PendingInterrupts = nil
	meta.ResolvedInterrupts = nil
	meta.ExecutionEpoch = max(int64(1), headPayload.ExecutionEpoch) + 1
	meta.ControlOrigin = "external_retry"
	meta.SourceRevisionID = source.RevisionID
	return encodeCheckpointPayloadWithMeta(state, sourcePayload.Outputs, sourcePayload.NextStep, meta)
}

func checkpointAtSequence(records []CheckpointRecord, sequence int64) (CheckpointRecord, bool) {
	var selected CheckpointRecord
	for _, record := range records {
		if record.ConfirmedSequence == sequence && record.PendingSequence == 0 && record.Version > selected.Version {
			selected = record
		}
	}
	return selected, selected.ID != ""
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
