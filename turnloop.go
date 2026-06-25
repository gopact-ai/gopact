package gopact

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"time"
)

var (
	errNilTurnLoopRunner = errors.New("gopact: turn loop runner is nil")
	errNilTurnLoopPolicy = errors.New("gopact: turn loop policy is nil")

	// ErrTurnLoopClosed is returned when a closed TurnLoop receives new work.
	ErrTurnLoopClosed = errors.New("gopact: turn loop closed")
)

// TurnLoop coordinates one user turn at a time around a Runner.
type TurnLoop struct {
	runner *Runner
	store  TurnLoopStore
	policy Policy

	mu              sync.Mutex
	closed          bool
	activeCancel    context.CancelFunc
	activeIDs       RuntimeIDs
	activeToken     uint64
	cancelReasons   map[uint64]string
	tokenSeq        atomic.Uint64
	inputSeq        atomic.Uint64
	closeMu         sync.Mutex
	runnerClosed    bool
	storeClosed     bool
	policyMetadata  map[string]any
	schemaValidator JSONSchemaValidator
	pending         []TurnInputRecord
	pendingEvents   []Event
	interrupted     *TurnInputRecord
	inputMerge      TurnInputMergeFunc
}

// TurnLoopOption configures a TurnLoop.
type TurnLoopOption func(*TurnLoop) error

// TurnOption configures one turn.
type TurnOption func(*turnConfig)

type turnConfig struct {
	ids     RuntimeIDs
	preempt bool
	resume  *ResumeRequest
}

// TurnInputKind describes the origin of a queued turn input.
type TurnInputKind string

const (
	TurnInputUser   TurnInputKind = "user"
	TurnInputResume TurnInputKind = "resume"
)

// TurnInputRecord is a pending TurnLoop input waiting to be merged into a run.
type TurnInputRecord struct {
	ID        string           `json:"id,omitempty"`
	Kind      TurnInputKind    `json:"kind,omitempty"`
	Input     any              `json:"input,omitempty"`
	Resume    *ResumeRequest   `json:"resume,omitempty"`
	Interrupt *InterruptRecord `json:"interrupt,omitempty"`
	IDs       RuntimeIDs       `json:"ids,omitempty"`
	CreatedAt time.Time        `json:"created_at,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
}

// TurnInputBatch is passed to the runner when a turn starts with queued inputs.
type TurnInputBatch struct {
	Current     any               `json:"current,omitempty"`
	Interrupted *TurnInputRecord  `json:"interrupted,omitempty"`
	Pending     []TurnInputRecord `json:"pending,omitempty"`
}

// TurnInputMergeRequest is the immutable input observed by a TurnLoop merge strategy.
type TurnInputMergeRequest struct {
	Current     any               `json:"current,omitempty"`
	Interrupted *TurnInputRecord  `json:"interrupted,omitempty"`
	Pending     []TurnInputRecord `json:"pending,omitempty"`
	Resume      *ResumeRequest    `json:"resume,omitempty"`
	IDs         RuntimeIDs        `json:"ids,omitempty"`
}

// TurnInputMergeResult is the input and audit metadata produced by a TurnLoop merge strategy.
type TurnInputMergeResult struct {
	Input    any            `json:"input,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TurnLoopPolicyInput is the stable policy input for TurnLoop resume gates.
type TurnLoopPolicyInput struct {
	Current     any               `json:"current,omitempty"`
	Interrupted *TurnInputRecord  `json:"interrupted,omitempty"`
	Resume      ResumeRequest     `json:"resume,omitempty"`
	Queued      *TurnInputRecord  `json:"queued,omitempty"`
	Pending     []TurnInputRecord `json:"pending,omitempty"`
}

// TurnInputMergeFunc merges current, queued, and interrupted turn inputs into one runner input.
type TurnInputMergeFunc func(ctx context.Context, request TurnInputMergeRequest) (TurnInputMergeResult, error)

// NewTurnLoop creates a turn loop around a runner.
func NewTurnLoop(runner *Runner, opts ...TurnLoopOption) (*TurnLoop, error) {
	if runner == nil {
		return nil, errNilTurnLoopRunner
	}
	loop := &TurnLoop{runner: runner}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(loop); err != nil {
			return nil, err
		}
	}
	return loop, nil
}

// WithTurnRuntimeIDs sets identity for one turn.
func WithTurnRuntimeIDs(ids RuntimeIDs) TurnOption {
	return func(cfg *turnConfig) {
		cfg.ids = ids
	}
}

// WithPreempt cancels the currently active turn before starting this one.
func WithPreempt() TurnOption {
	return func(cfg *turnConfig) {
		cfg.preempt = true
	}
}

// WithResume marks this turn as resuming a previous interrupt.
func WithResume(resume ResumeRequest) TurnOption {
	return func(cfg *turnConfig) {
		cfg.resume = &resume
	}
}

// WithTurnInputMerge sets the strategy used to merge current, queued, and interrupted turn input.
func WithTurnInputMerge(merge TurnInputMergeFunc) TurnLoopOption {
	return func(loop *TurnLoop) error {
		if merge == nil {
			return errors.New("gopact: turn input merge is nil")
		}
		loop.inputMerge = merge
		return nil
	}
}

// WithTurnPolicy authorizes TurnLoop control-plane resume gates.
func WithTurnPolicy(policy Policy) TurnLoopOption {
	return func(loop *TurnLoop) error {
		if policy == nil {
			return errNilTurnLoopPolicy
		}
		loop.policy = policy
		return nil
	}
}

// WithTurnPolicyMetadata attaches metadata to TurnLoop policy requests.
func WithTurnPolicyMetadata(metadata map[string]any) TurnLoopOption {
	return func(loop *TurnLoop) error {
		loop.policyMetadata = copyAnyMap(metadata)
		return nil
	}
}

// WithTurnJSONSchemaValidator sets the validator used for TurnLoop resume gates.
func WithTurnJSONSchemaValidator(validator JSONSchemaValidator) TurnLoopOption {
	return func(loop *TurnLoop) error {
		if validator == nil {
			return errors.New("gopact: turn json schema validator is nil")
		}
		loop.schemaValidator = validator
		return nil
	}
}

// Push queues user input for the next turn run.
func (l *TurnLoop) Push(ctx context.Context, input any, opts ...TurnOption) (TurnInputRecord, error) {
	if l == nil || l.runner == nil {
		return TurnInputRecord{}, errNilTurnLoopRunner
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnInputRecord{}, err
	}
	if err := l.closedError(); err != nil {
		return TurnInputRecord{}, err
	}

	cfg := turnConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.resume != nil {
		if err := l.validateResumeRequest(ctx, *cfg.resume); err != nil {
			return TurnInputRecord{}, fmt.Errorf("gopact: queue resume request: %w", err)
		}
		return l.enqueue(ctx, TurnInputResume, nil, cfg.resume, cfg)
	}
	return l.enqueue(ctx, TurnInputUser, input, nil, cfg)
}

// Resume queues a resume request for the next turn run.
func (l *TurnLoop) Resume(ctx context.Context, resume ResumeRequest, opts ...TurnOption) (TurnInputRecord, error) {
	if err := resume.Validate(); err != nil {
		return TurnInputRecord{}, fmt.Errorf("gopact: queue resume request: %w", err)
	}
	opts = append(opts, WithResume(resume))
	return l.Push(ctx, nil, opts...)
}

// Pending returns a copy of queued turn inputs.
func (l *TurnLoop) Pending() []TurnInputRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return copyTurnInputRecords(l.pending)
}

// Interrupted returns the turn input that most recently reached an interrupt boundary.
func (l *TurnLoop) Interrupted() (TurnInputRecord, bool) {
	if l == nil {
		return TurnInputRecord{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interrupted == nil {
		return TurnInputRecord{}, false
	}
	return copyTurnInputRecord(*l.interrupted), true
}

// Run starts one turn and streams turn plus runner events.
func (l *TurnLoop) Run(ctx context.Context, input any, opts ...TurnOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if l == nil || l.runner == nil {
			yield(turnEvent(EventTurnFailed, RuntimeIDs{}, nil, errNilTurnLoopRunner), errNilTurnLoopRunner)
			return
		}
		if ctx == nil {
			ctx = context.TODO()
		}

		cfg := turnConfig{}
		for _, opt := range opts {
			if opt != nil {
				opt(&cfg)
			}
		}

		if err := l.closedError(); err != nil {
			yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
			return
		}
		pending, pendingEvents, err := l.drainPending(ctx)
		if err != nil {
			yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
			return
		}
		interrupted, hasInterrupted := l.currentInterrupted()
		if cfg.resume != nil {
			if err := cfg.resume.Validate(); err != nil {
				yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
				return
			}
			if err := validateResumeAgainstInterrupted(ctx, l.schemaValidator, interrupted, hasInterrupted, *cfg.resume); err != nil {
				yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
				return
			}
		}
		for _, event := range pendingEvents {
			if !yield(event.WithRuntimeDefaults(cfg.ids), nil) {
				return
			}
		}
		policyEvents, policyErr := l.authorizeResumePolicies(ctx, cfg.ids, input, interrupted, hasInterrupted, pending, cfg.resume)
		for _, event := range policyEvents {
			if !yield(event.WithRuntimeDefaults(cfg.ids), nil) {
				return
			}
		}
		if policyErr != nil {
			eventType := EventTurnFailed
			if errors.Is(policyErr, ErrInterrupted) {
				eventType = EventTurnInterrupted
			}
			yield(turnEvent(eventType, cfg.ids, nil, policyErr), policyErr)
			return
		}

		if cfg.preempt {
			if l.cancelActive() && !yield(turnEvent(EventTurnPreempted, cfg.ids, nil, nil), nil) {
				return
			}
		}

		runCtx, cancel := context.WithCancel(ctx)
		token, err := l.setActive(cancel, cfg.ids)
		if err != nil {
			cancel()
			yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
			return
		}
		defer l.clearActive(token)

		if !yield(turnEvent(EventTurnStarted, cfg.ids, nil, nil), nil) {
			return
		}
		currentRecord := l.newTurnInputRecord(TurnInputUser, input, nil, cfg.ids)
		includeInterrupted := hasInterrupted && cfg.resume != nil
		mergeRequest := TurnInputMergeRequest{
			Current: input,
			Pending: copyTurnInputRecords(pending),
			IDs:     cfg.ids,
		}
		if includeInterrupted {
			record := copyTurnInputRecord(interrupted)
			mergeRequest.Interrupted = &record
		}
		if cfg.resume != nil {
			resume := copyResumeRequest(*cfg.resume)
			mergeRequest.Resume = &resume
		}
		mergeResult, emitMergeEvent, err := l.mergeTurnInput(runCtx, mergeRequest)
		if err != nil {
			yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
			return
		}
		runInput := mergeResult.Input
		if emitMergeEvent {
			if !yield(turnEvent(EventTurnInputMerged, cfg.ids, l.turnInputMergeMetadata(mergeRequest, mergeResult), nil), nil) {
				return
			}
		}
		if cfg.resume != nil {
			metadata := copyAnyMap(cfg.resume.Metadata)
			if metadata == nil {
				metadata = make(map[string]any)
			}
			metadata["interrupt_id"] = cfg.resume.InterruptID
			metadata["payload"] = cfg.resume.Payload
			if !yield(turnEvent(EventTurnResumed, cfg.ids, metadata, nil), nil) {
				return
			}
		}

		runOpts := []RunOption{WithRuntimeIDs(cfg.ids)}
		if l.schemaValidator != nil {
			runOpts = append(runOpts, WithJSONSchemaValidator(l.schemaValidator))
		}
		if cfg.resume != nil {
			runOpts = append(runOpts, WithResumeRequest(*cfg.resume))
		}

		var terminalErr error
		var terminalEvent Event
		for event, err := range l.runner.Run(runCtx, runInput, runOpts...) {
			if err != nil {
				terminalErr = err
				terminalEvent = event
				event.Err = err
				if !yield(event.WithRuntimeDefaults(cfg.ids), nil) {
					return
				}
				break
			}
			if !yield(event.WithRuntimeDefaults(cfg.ids), nil) {
				return
			}
		}

		if terminalErr != nil {
			eventType := EventTurnFailed
			metadata := map[string]any(nil)
			if errors.Is(terminalErr, context.Canceled) {
				eventType = EventTurnCanceled
				metadata = l.cancelMetadata(token)
			} else if errors.Is(terminalErr, ErrInterrupted) {
				if interrupt := interruptRecordFromEvent(terminalEvent, terminalErr); interrupt != nil {
					currentRecord.Interrupt = interrupt
				}
				if err := l.setInterrupted(ctx, currentRecord); err != nil {
					yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
					return
				}
				eventType = EventTurnInterrupted
			}
			yield(turnEvent(eventType, cfg.ids, metadata, terminalErr), terminalErr)
			return
		}
		if err := l.clearInterrupted(ctx); err != nil {
			yield(turnEvent(EventTurnFailed, cfg.ids, nil, err), err)
			return
		}
		yield(turnEvent(EventTurnCompleted, cfg.ids, nil, nil), nil)
	}
}

// Cancel cancels the currently active turn.
func (l *TurnLoop) Cancel(reason string) bool {
	if l == nil {
		return false
	}
	return l.cancelActive(reason)
}

// Close cancels the active turn, then closes runner-level resources.
func (l *TurnLoop) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if l == nil || l.runner == nil {
		return errNilTurnLoopRunner
	}
	l.markClosed()

	l.closeMu.Lock()
	defer l.closeMu.Unlock()

	if !l.runnerClosed {
		l.cancelActive()
		if err := l.runner.Close(ctx); err != nil {
			return err
		}
		l.runnerClosed = true
	}
	if l.storeClosed {
		return nil
	}
	closer, ok := l.store.(interface {
		Close(context.Context) error
	})
	if !ok {
		l.storeClosed = true
		return nil
	}
	if err := closer.Close(ctx); err != nil {
		return err
	}
	l.storeClosed = true
	return nil
}

// DefaultTurnInputMerge preserves the built-in TurnLoop input merge behavior.
func DefaultTurnInputMerge(ctx context.Context, request TurnInputMergeRequest) (TurnInputMergeResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnInputMergeResult{}, err
	}
	if len(request.Pending) == 0 && request.Interrupted == nil {
		return TurnInputMergeResult{Input: request.Current}, nil
	}
	batch := TurnInputBatch{
		Current: request.Current,
		Pending: copyTurnInputRecords(request.Pending),
	}
	if request.Interrupted != nil {
		record := copyTurnInputRecord(*request.Interrupted)
		batch.Interrupted = &record
	}
	return TurnInputMergeResult{Input: batch}, nil
}

func (l *TurnLoop) mergeTurnInput(ctx context.Context, request TurnInputMergeRequest) (TurnInputMergeResult, bool, error) {
	merge := DefaultTurnInputMerge
	hasCustomMerge := l != nil && l.inputMerge != nil
	if hasCustomMerge {
		merge = l.inputMerge
	}
	result, err := merge(ctx, copyTurnInputMergeRequest(request))
	if err != nil {
		return TurnInputMergeResult{}, false, fmt.Errorf("gopact: merge turn input: %w", err)
	}
	result.Metadata = copyAnyMap(result.Metadata)
	emitMergeEvent := hasCustomMerge || len(request.Pending) > 0 || request.Interrupted != nil || len(result.Metadata) > 0
	return result, emitMergeEvent, nil
}

func (l *TurnLoop) validateResumeRequest(ctx context.Context, resume ResumeRequest) error {
	if err := resume.Validate(); err != nil {
		return err
	}
	interrupted, ok := l.currentInterrupted()
	return validateResumeAgainstInterrupted(ctx, l.schemaValidator, interrupted, ok, resume)
}

func validateResumeAgainstInterrupted(ctx context.Context, validator JSONSchemaValidator, interrupted TurnInputRecord, ok bool, resume ResumeRequest) error {
	if !ok || interrupted.Interrupt == nil {
		return nil
	}
	if resume.InterruptID != interrupted.Interrupt.ID {
		return fmt.Errorf("gopact: resume interrupt id %q does not match pending interrupt %q", resume.InterruptID, interrupted.Interrupt.ID)
	}
	return ValidateResumePayloadWithValidator(ctx, validator, *interrupted.Interrupt, resume)
}

func (l *TurnLoop) authorizeResumePolicies(ctx context.Context, ids RuntimeIDs, current any, interrupted TurnInputRecord, hasInterrupted bool, pending []TurnInputRecord, resume *ResumeRequest) ([]Event, error) {
	if l == nil || l.policy == nil {
		return nil, nil
	}
	var events []Event
	for _, record := range pending {
		if record.Kind != TurnInputResume || record.Resume == nil {
			continue
		}
		record := copyTurnInputRecord(record)
		nextEvents, err := l.authorizeResumePolicy(ctx, ids, current, interrupted, hasInterrupted, pending, *record.Resume, &record)
		events = append(events, nextEvents...)
		if err != nil {
			return events, err
		}
	}
	if resume != nil {
		nextEvents, err := l.authorizeResumePolicy(ctx, ids, current, interrupted, hasInterrupted, pending, *resume, nil)
		events = append(events, nextEvents...)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}

func (l *TurnLoop) authorizeResumePolicy(ctx context.Context, ids RuntimeIDs, current any, interrupted TurnInputRecord, hasInterrupted bool, pending []TurnInputRecord, resume ResumeRequest, queued *TurnInputRecord) ([]Event, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input := TurnLoopPolicyInput{
		Current: current,
		Resume:  copyResumeRequest(resume),
		Pending: copyTurnInputRecords(pending),
	}
	if hasInterrupted {
		record := copyTurnInputRecord(interrupted)
		input.Interrupted = &record
	}
	if queued != nil {
		record := copyTurnInputRecord(*queued)
		input.Queued = &record
	}
	req := PolicyRequest{
		IDs:      ids,
		Boundary: PolicyBoundaryTurn,
		Action:   PolicyActionResume,
		Input:    input,
		Metadata: copyAnyMap(l.policyMetadata),
	}
	events := []Event{NewPolicyRequestedEvent(req)}
	decision, err := l.policy.Decide(ctx, req)
	if err != nil {
		return events, fmt.Errorf("gopact: turn loop resume policy: %w", err)
	}
	events = append(events, NewPolicyDecidedEvent(req, decision))
	if decision.Action == PolicyReview {
		return events, NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return events, &PolicyDeniedError{Decision: decision, Request: req}
	}
	return events, nil
}

func (l *TurnLoop) turnInputMergeMetadata(request TurnInputMergeRequest, result TurnInputMergeResult) map[string]any {
	metadata := map[string]any{
		"pending_count":   len(request.Pending),
		"has_interrupted": request.Interrupted != nil,
	}
	for key, value := range result.Metadata {
		metadata[key] = value
	}
	return metadata
}

func (l *TurnLoop) enqueue(ctx context.Context, kind TurnInputKind, input any, resume *ResumeRequest, cfg turnConfig) (TurnInputRecord, error) {
	if err := l.closedError(); err != nil {
		return TurnInputRecord{}, err
	}
	preempted := false
	if cfg.preempt {
		preempted = l.cancelActive()
	}

	record := l.newTurnInputRecord(kind, input, resume, cfg.ids)
	if resume != nil {
		record.Metadata = copyAnyMap(resume.Metadata)
	}

	event := turnEvent(EventTurnInputReceived, cfg.ids, turnInputMetadata(record), nil)

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return TurnInputRecord{}, ErrTurnLoopClosed
	}
	l.pending = append(l.pending, copyTurnInputRecord(record))
	if preempted {
		l.pendingEvents = append(l.pendingEvents, turnEvent(EventTurnPreempted, cfg.ids, nil, nil))
	}
	l.pendingEvents = append(l.pendingEvents, event)
	state := l.stateLocked()
	l.mu.Unlock()

	if err := l.saveState(ctx, state); err != nil {
		return TurnInputRecord{}, err
	}
	return record, nil
}

func (l *TurnLoop) newTurnInputRecord(kind TurnInputKind, input any, resume *ResumeRequest, ids RuntimeIDs) TurnInputRecord {
	record := TurnInputRecord{
		ID:        fmt.Sprintf("turn-input:%d", l.inputSeq.Add(1)),
		Kind:      kind,
		Input:     input,
		IDs:       ids,
		CreatedAt: time.Now(),
	}
	if resume != nil {
		copied := *resume
		copied.Metadata = copyAnyMap(resume.Metadata)
		record.Resume = &copied
	}
	return record
}

func (l *TurnLoop) applyState(state TurnLoopState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = copyTurnInputRecords(state.Pending)
	l.pendingEvents = copyEvents(state.PendingEvents)
	if state.Interrupted != nil {
		record := copyTurnInputRecord(*state.Interrupted)
		l.interrupted = &record
	} else {
		l.interrupted = nil
	}
	l.inputSeq.Store(state.InputSeq)
}

func (l *TurnLoop) stateLocked() TurnLoopState {
	state := TurnLoopState{
		Pending:       copyTurnInputRecords(l.pending),
		PendingEvents: copyEvents(l.pendingEvents),
		InputSeq:      l.inputSeq.Load(),
		UpdatedAt:     time.Now(),
	}
	if l.interrupted != nil {
		record := copyTurnInputRecord(*l.interrupted)
		state.Interrupted = &record
	}
	return state
}

func (l *TurnLoop) saveState(ctx context.Context, state TurnLoopState) error {
	if l == nil || l.store == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := l.store.Save(ctx, state); err != nil {
		return fmt.Errorf("gopact: save turn loop state: %w", err)
	}
	return nil
}

func (l *TurnLoop) drainPending(ctx context.Context) ([]TurnInputRecord, []Event, error) {
	l.mu.Lock()

	pending := copyTurnInputRecords(l.pending)
	events := append([]Event(nil), l.pendingEvents...)
	l.pending = nil
	l.pendingEvents = nil
	state := l.stateLocked()
	l.mu.Unlock()
	if err := l.saveState(ctx, state); err != nil {
		return nil, nil, err
	}
	return pending, events, nil
}

func (l *TurnLoop) currentInterrupted() (TurnInputRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interrupted == nil {
		return TurnInputRecord{}, false
	}
	return copyTurnInputRecord(*l.interrupted), true
}

func (l *TurnLoop) setInterrupted(ctx context.Context, record TurnInputRecord) error {
	copied := copyTurnInputRecord(record)
	l.mu.Lock()
	l.interrupted = &copied
	state := l.stateLocked()
	l.mu.Unlock()
	return l.saveState(ctx, state)
}

func (l *TurnLoop) clearInterrupted(ctx context.Context) error {
	l.mu.Lock()
	l.interrupted = nil
	state := l.stateLocked()
	l.mu.Unlock()
	return l.saveState(ctx, state)
}

func (l *TurnLoop) setActive(cancel context.CancelFunc, ids RuntimeIDs) (uint64, error) {
	token := l.tokenSeq.Add(1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, ErrTurnLoopClosed
	}
	l.activeCancel = cancel
	l.activeIDs = ids
	l.activeToken = token
	return token, nil
}

func (l *TurnLoop) clearActive(token uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.activeToken == token {
		l.activeCancel = nil
		l.activeIDs = RuntimeIDs{}
		l.activeToken = 0
	}
}

func (l *TurnLoop) cancelActive(reason ...string) bool {
	l.mu.Lock()
	cancel := l.activeCancel
	token := l.activeToken
	l.mu.Unlock()
	if cancel == nil {
		return false
	}
	l.setCancelReason(token, reason...)
	cancel()
	return true
}

func (l *TurnLoop) closedError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrTurnLoopClosed
	}
	return nil
}

func (l *TurnLoop) markClosed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
}

func (l *TurnLoop) setCancelReason(token uint64, reason ...string) {
	if token == 0 || len(reason) == 0 || reason[0] == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancelReasons == nil {
		l.cancelReasons = make(map[uint64]string)
	}
	l.cancelReasons[token] = reason[0]
}

func (l *TurnLoop) cancelMetadata(token uint64) map[string]any {
	if token == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	reason := l.cancelReasons[token]
	delete(l.cancelReasons, token)
	if reason == "" {
		return nil
	}
	return map[string]any{"reason": reason}
}

func turnEvent(eventType EventType, ids RuntimeIDs, metadata map[string]any, err error) Event {
	return Event{
		Type:      eventType,
		IDs:       ids,
		RunID:     ids.RunID,
		ThreadID:  ids.ThreadID,
		Metadata:  metadata,
		CreatedAt: now(),
		Err:       err,
	}
}

func turnInputMetadata(record TurnInputRecord) map[string]any {
	metadata := copyAnyMap(record.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["input_id"] = record.ID
	metadata["kind"] = string(record.Kind)
	if record.Resume != nil {
		metadata["interrupt_id"] = record.Resume.InterruptID
		if record.Resume.CheckpointID != "" {
			metadata["checkpoint_id"] = record.Resume.CheckpointID
		}
		if record.Resume.StepID != "" {
			metadata["step_id"] = record.Resume.StepID
		}
	}
	return metadata
}

func copyTurnInputRecords(in []TurnInputRecord) []TurnInputRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]TurnInputRecord, len(in))
	for i, record := range in {
		out[i] = copyTurnInputRecord(record)
	}
	return out
}

func copyTurnInputRecord(in TurnInputRecord) TurnInputRecord {
	out := in
	out.Metadata = copyAnyMap(in.Metadata)
	if in.Interrupt != nil {
		interrupt := copyInterruptRecord(*in.Interrupt)
		out.Interrupt = &interrupt
	}
	if in.Resume != nil {
		resume := *in.Resume
		resume.Metadata = copyAnyMap(in.Resume.Metadata)
		out.Resume = &resume
	}
	return out
}

func interruptRecordFromEvent(event Event, err error) *InterruptRecord {
	if event.StepSnapshot != nil && event.StepSnapshot.Pending != nil {
		record := copyInterruptRecord(*event.StepSnapshot.Pending)
		return &record
	}
	var interruptErr *InterruptError
	if errors.As(err, &interruptErr) {
		record := copyInterruptRecord(interruptErr.Record)
		return &record
	}
	return nil
}

func copyTurnInputMergeRequest(in TurnInputMergeRequest) TurnInputMergeRequest {
	out := in
	out.Pending = copyTurnInputRecords(in.Pending)
	if in.Interrupted != nil {
		record := copyTurnInputRecord(*in.Interrupted)
		out.Interrupted = &record
	}
	if in.Resume != nil {
		resume := copyResumeRequest(*in.Resume)
		out.Resume = &resume
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
