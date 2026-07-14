package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

const (
	defaultHistoryRecordLimit = 10_000
	historyPageSize           = 256
)

var errInconsistentRunLifecycle = errors.New("workflow: inconsistent run lifecycle")

// SnapshotStore loads a read model for a run.
type SnapshotStore interface {
	Load(context.Context, SnapshotRequest) (Snapshot, error)
}

// SnapshotRequest identifies the snapshot range to load.
type SnapshotRequest struct {
	RunID string
	After int64
	Limit int
}

// SessionRunsRequest identifies the session whose runs should be listed.
type SessionRunsRequest struct {
	SessionID string
	// MaxRecords defaults to 10,000 and may only lower that safety bound.
	MaxRecords int
}

// RunSummary is a session-scoped presentation view of one run.
type RunSummary struct {
	SessionID         string
	RunID             string
	DefinitionID      string
	DefinitionVersion string
	ParentRunID       string
	Status            CheckpointStatus
	StartedAt         time.Time
	UpdatedAt         time.Time
	EndedAt           time.Time
}

// ListSessionRuns groups a session's append-ordered records into run summaries.
func ListSessionRuns(ctx context.Context, log runlog.Log, request SessionRunsRequest) ([]RunSummary, error) {
	if request.SessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", runlog.ErrInvalidQuery)
	}
	if request.MaxRecords < 0 || request.MaxRecords > defaultHistoryRecordLimit {
		return nil, fmt.Errorf("%w: max records must be between 0 and %d", runlog.ErrInvalidQuery, defaultHistoryRecordLimit)
	}
	if log == nil {
		return nil, fmt.Errorf("workflow: list session runs: %w", runlog.ErrNilLog)
	}
	limit := request.MaxRecords
	if limit == 0 {
		limit = defaultHistoryRecordLimit
	}
	queryLimit := limit + 1
	records, err := log.List(ctx, runlog.Query{SessionID: request.SessionID, Limit: queryLimit})
	if err != nil {
		return nil, fmt.Errorf("workflow: list session runs: %w", err)
	}
	if len(records) > limit {
		return nil, fmt.Errorf("%w: session %q exceeds %d records", ErrHistoryLimitExceeded, request.SessionID, limit)
	}
	summaries := make([]RunSummary, 0)
	byRunID := make(map[string]int)
	for _, record := range records {
		index, exists := byRunID[record.RunID]
		if !exists {
			index = len(summaries)
			byRunID[record.RunID] = index
			summaries = append(summaries, RunSummary{
				SessionID:         record.SessionID,
				RunID:             record.RunID,
				DefinitionID:      record.DefinitionID,
				DefinitionVersion: record.DefinitionVersion,
				ParentRunID:       record.ParentRunID,
				StartedAt:         record.Timestamp,
			})
		}
		summary := &summaries[index]
		if record.SessionID != request.SessionID || record.SessionID != summary.SessionID ||
			record.DefinitionID != summary.DefinitionID || record.DefinitionVersion != summary.DefinitionVersion ||
			record.ParentRunID != summary.ParentRunID {
			return nil, fmt.Errorf("workflow: run %q contains inconsistent session, definition, or parent identity", record.RunID)
		}
		if err := summary.applyLifecycle(record); err != nil {
			return nil, fmt.Errorf("%w: run %q event %q after status %q", err, record.RunID, record.EventType, summary.Status)
		}
		summary.UpdatedAt = record.Timestamp
	}
	return summaries, nil
}

func (summary *RunSummary) applyLifecycle(record runlog.Record) error {
	switch record.EventType {
	case EventWorkflowStarted, EventWorkflowRetryStarted, EventWorkflowJumpStarted:
		return summary.start()
	case EventWorkflowResumed:
		return summary.resume()
	case EventWorkflowInterrupted:
		return summary.interrupt()
	case EventWorkflowCompleted:
		return summary.end(CheckpointCompleted, record.Timestamp)
	case EventWorkflowFailed:
		return summary.end(CheckpointFailed, record.Timestamp)
	case EventWorkflowCanceled:
		return summary.end(CheckpointCanceled, record.Timestamp)
	case EventWorkflowTerminated:
		return summary.end(CheckpointTerminated, record.Timestamp)
	}
	return nil
}

func (summary *RunSummary) start() error {
	if summary.Status != "" {
		return errInconsistentRunLifecycle
	}
	summary.Status = CheckpointRunning
	return nil
}

func (summary *RunSummary) resume() error {
	if summary.Status != CheckpointRunning && summary.Status != CheckpointInterrupted {
		return errInconsistentRunLifecycle
	}
	summary.Status = CheckpointRunning
	return nil
}

func (summary *RunSummary) interrupt() error {
	if summary.Status != CheckpointRunning {
		return errInconsistentRunLifecycle
	}
	summary.Status = CheckpointInterrupted
	summary.EndedAt = time.Time{}
	return nil
}

func (summary *RunSummary) end(status CheckpointStatus, timestamp time.Time) error {
	if summary.Status != CheckpointRunning && summary.Status != CheckpointInterrupted {
		return errInconsistentRunLifecycle
	}
	summary.Status = status
	summary.EndedAt = timestamp
	return nil
}

// RunMeta is the snapshot run identity view.
type RunMeta struct {
	SessionID        string
	RunID            string
	ParentRunID      string
	SourceRunID      string
	SourceEventSeq   int64
	SourceRevisionID string
}

// Snapshot is a read model built from runlog/checkpoint/artifact refs.
type Snapshot struct {
	RunMeta         RunMeta
	WorkflowName    string
	TopologyVersion string
	Timeline        []runlog.Record
	Checkpoints     []CheckpointView
	Missing         []MissingRef
}

// CheckpointView identifies one stable workflow checkpoint without exposing its payload.
type CheckpointView struct {
	ID            string
	Version       int64
	EventSeq      int64
	Status        CheckpointStatus
	SchemaVersion int
	ReplayStatus  ReplayStatus
	Root          bool
}

// MissingRef marks a missing artifact or view input.
type MissingRef struct {
	Kind string
	Ref  string
}

// Snapshot loads the execution view saved by this workflow's configured stores.
func (wf *Workflow[I, O]) Snapshot(ctx context.Context, request SnapshotRequest) (Snapshot, error) {
	compiled, err := wf.compile()
	if err != nil {
		return Snapshot{}, err
	}
	history, ok := compiled.checkpointer.(CheckpointHistory)
	if !ok {
		return Snapshot{}, errors.New("workflow: checkpointer does not provide checkpoint history")
	}
	return NewRunLogSnapshotStore(compiled.journal, history).Load(ctx, request)
}

// RunLogSnapshotStore builds snapshots from a RunLog.
type RunLogSnapshotStore struct {
	log         runlog.Log
	checkpoints CheckpointHistory
}

// NewRunLogSnapshotStore creates a snapshot store over log and checkpoint history.
func NewRunLogSnapshotStore(log runlog.Log, checkpoints CheckpointHistory) RunLogSnapshotStore {
	return RunLogSnapshotStore{log: log, checkpoints: checkpoints}
}

// Load implements SnapshotStore.
func (s RunLogSnapshotStore) Load(ctx context.Context, req SnapshotRequest) (Snapshot, error) {
	if s.log == nil {
		return Snapshot{}, errors.New("workflow: runlog is nil")
	}
	if s.checkpoints == nil {
		return Snapshot{}, errors.New("workflow: checkpoint history is nil")
	}
	if req.RunID == "" {
		return Snapshot{}, errors.New("workflow: snapshot run id is required")
	}
	queryLimit := req.Limit
	boundedDefault := queryLimit == 0
	if boundedDefault {
		queryLimit = defaultHistoryRecordLimit + 1
	}
	records, err := s.log.List(ctx, runlog.Query{RunID: req.RunID, After: req.After, Limit: queryLimit})
	if err != nil {
		return Snapshot{}, err
	}
	if boundedDefault && len(records) > defaultHistoryRecordLimit {
		return Snapshot{}, fmt.Errorf(
			"%w: run %q exceeds %d timeline records",
			ErrHistoryLimitExceeded,
			req.RunID,
			defaultHistoryRecordLimit,
		)
	}
	snapshot := Snapshot{RunMeta: RunMeta{RunID: req.RunID}, Timeline: cloneRunLogRecords(records)}
	if err := snapshot.projectTimeline(records); err != nil {
		return Snapshot{}, err
	}
	if err := snapshot.projectCheckpointHistory(ctx, s.checkpoints); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (snapshot *Snapshot) projectTimeline(records []runlog.Record) error {
	for index, record := range records {
		if record.RunID != snapshot.RunMeta.RunID {
			return fmt.Errorf("workflow: runlog returned record for run %q, want %q", record.RunID, snapshot.RunMeta.RunID)
		}
		if index == 0 {
			snapshot.RunMeta.SessionID = record.SessionID
			snapshot.RunMeta.ParentRunID = record.ParentRunID
			snapshot.RunMeta.SourceRunID = record.SourceRunID
			snapshot.RunMeta.SourceEventSeq = record.SourceEventSeq
			snapshot.RunMeta.SourceRevisionID = record.SourceRevisionID
		} else if !snapshot.sameLineage(record) {
			return errors.New("workflow: runlog contains inconsistent run lineage")
		}
		if record.PayloadRef != "" && record.Payload == nil {
			snapshot.Missing = append(snapshot.Missing, MissingRef{Kind: "event_payload", Ref: record.PayloadRef})
		}
	}
	return nil
}

func (snapshot Snapshot) sameLineage(record runlog.Record) bool {
	return record.SessionID == snapshot.RunMeta.SessionID && record.ParentRunID == snapshot.RunMeta.ParentRunID &&
		record.SourceRunID == snapshot.RunMeta.SourceRunID && record.SourceEventSeq == snapshot.RunMeta.SourceEventSeq &&
		record.SourceRevisionID == snapshot.RunMeta.SourceRevisionID
}

type checkpointProjection struct {
	snapshot     *Snapshot
	wanted       map[int64]struct{}
	latest       map[int64]CheckpointView
	afterVersion int64
	root         int64
	total        int
}

func (snapshot *Snapshot) projectCheckpointHistory(ctx context.Context, history CheckpointHistory) error {
	wanted := make(map[int64]struct{}, len(snapshot.Timeline))
	for _, record := range snapshot.Timeline {
		wanted[record.Sequence] = struct{}{}
	}
	projection := checkpointProjection{
		snapshot: snapshot,
		wanted:   wanted,
		latest:   make(map[int64]CheckpointView, len(wanted)),
	}
	for {
		limit := min(historyPageSize, defaultHistoryRecordLimit-projection.total+1)
		records, err := history.ListCheckpoints(ctx, CheckpointHistoryRequest{
			RunID: snapshot.RunMeta.RunID, AfterVersion: projection.afterVersion, Limit: limit,
		})
		if err != nil {
			return err
		}
		if len(records) > limit {
			return fmt.Errorf("workflow: checkpoint history returned %d records for limit %d", len(records), limit)
		}
		if len(records) == 0 {
			break
		}
		if err := projection.accept(records); err != nil {
			return err
		}
		if len(records) < limit {
			break
		}
	}
	for _, record := range snapshot.Timeline {
		checkpoint, ok := projection.latest[record.Sequence]
		if !ok {
			continue
		}
		checkpoint.Root = record.Sequence == projection.root
		snapshot.Checkpoints = append(snapshot.Checkpoints, checkpoint)
	}
	return nil
}

func (projection *checkpointProjection) accept(records []CheckpointRecord) error {
	for _, record := range records {
		projection.total++
		if projection.total > defaultHistoryRecordLimit {
			return fmt.Errorf(
				"%w: run %q exceeds %d checkpoints",
				ErrHistoryLimitExceeded,
				projection.snapshot.RunMeta.RunID,
				defaultHistoryRecordLimit,
			)
		}
		if record.Version <= projection.afterVersion {
			return fmt.Errorf("%w: checkpoint history is not ordered after version %d", ErrCheckpointMismatch, projection.afterVersion)
		}
		projection.afterVersion = record.Version
		if err := projection.snapshot.acceptCheckpointIdentity(record); err != nil {
			return err
		}
		if record.ConfirmedSequence <= 0 || record.PendingSequence != 0 {
			continue
		}
		if projection.root == 0 || record.ConfirmedSequence < projection.root {
			projection.root = record.ConfirmedSequence
		}
		if _, ok := projection.wanted[record.ConfirmedSequence]; ok {
			projection.latest[record.ConfirmedSequence] = workflowCheckpointView(record, false)
		}
	}
	return nil
}

func (snapshot *Snapshot) acceptCheckpointIdentity(record CheckpointRecord) error {
	if record.RunID != snapshot.RunMeta.RunID {
		return fmt.Errorf("%w: snapshot checkpoint run %q does not match %q", ErrCheckpointMismatch, record.RunID, snapshot.RunMeta.RunID)
	}
	if record.SessionID == "" {
		return fmt.Errorf("%w: snapshot checkpoint session id is empty", ErrCheckpointMismatch)
	}
	if snapshot.RunMeta.SessionID == "" {
		snapshot.RunMeta.SessionID = record.SessionID
		snapshot.RunMeta.SourceRunID = record.SourceRunID
		snapshot.RunMeta.SourceEventSeq = record.SourceEventSeq
		snapshot.RunMeta.SourceRevisionID = record.SourceRevisionID
	} else if record.SessionID != snapshot.RunMeta.SessionID {
		return fmt.Errorf("%w: snapshot checkpoint session %q does not match %q", ErrCheckpointMismatch, record.SessionID, snapshot.RunMeta.SessionID)
	}
	if record.SourceRunID != snapshot.RunMeta.SourceRunID || record.SourceEventSeq != snapshot.RunMeta.SourceEventSeq ||
		record.SourceRevisionID != snapshot.RunMeta.SourceRevisionID {
		return fmt.Errorf("%w: snapshot checkpoint source lineage is inconsistent", ErrCheckpointMismatch)
	}
	if record.SchemaVersion != checkpointSchemaVersion {
		return fmt.Errorf("%w: snapshot checkpoint schema is incompatible", ErrCheckpointMismatch)
	}
	if snapshot.WorkflowName == "" {
		snapshot.WorkflowName = record.WorkflowName
		snapshot.TopologyVersion = record.TopologyVersion
		return nil
	}
	if record.WorkflowName != snapshot.WorkflowName || record.TopologyVersion != snapshot.TopologyVersion || record.SchemaVersion != checkpointSchemaVersion {
		return fmt.Errorf("%w: snapshot checkpoint identity is inconsistent", ErrCheckpointMismatch)
	}
	return nil
}

func workflowCheckpointView(record CheckpointRecord, root bool) CheckpointView {
	return CheckpointView{
		ID: record.ID, Version: record.Version, EventSeq: record.ConfirmedSequence,
		Status: record.Status, SchemaVersion: record.SchemaVersion, ReplayStatus: record.ReplayStatus, Root: root,
	}
}

// ForkRequest asks a loaded snapshot to start a new run.
type ForkRequest struct {
	SourceRunID  string
	FromEventSeq int64
	Patch        ForkPatch
}

// ForkPatch contains overrides for a new forked run.
type ForkPatch struct {
	WorkflowInput *InputPatch
}

// InputPatch replaces the workflow input for a fork.
type InputPatch struct {
	Value any
}

// Fork starts a new run from a safe root snapshot and workflow input patch.
func (s Snapshot) Fork[I, O any](ctx context.Context, target *Workflow[I, O], req ForkRequest, opts ...gopact.RunOption) (O, error) {
	var zero O
	if target == nil {
		return zero, errNilWorkflow
	}
	compiled, err := target.compile()
	if err != nil {
		return zero, err
	}
	if req.SourceRunID == "" || req.SourceRunID != s.RunMeta.RunID {
		return zero, fmt.Errorf(
			"workflow: fork source run id %q does not match snapshot run id %q",
			req.SourceRunID,
			s.RunMeta.RunID,
		)
	}
	if len(s.Timeline) == 0 {
		return zero, errors.New("workflow: fork snapshot has empty timeline")
	}
	if req.FromEventSeq <= 0 {
		return zero, errors.New("workflow: fork event sequence must be positive")
	}
	checkpoint, ok := s.checkpoint(req.FromEventSeq)
	if !ok {
		return zero, fmt.Errorf("workflow: fork event sequence %d is not a stable checkpoint boundary", req.FromEventSeq)
	}
	if !checkpoint.Root || checkpoint.ReplayStatus != ReplaySafe {
		return zero, errors.New("workflow: generic fork requires a safe root checkpoint")
	}
	if s.WorkflowName != compiled.name || s.TopologyVersion != compiled.topologyVersion || checkpoint.SchemaVersion != checkpointSchemaVersion {
		return zero, fmt.Errorf("%w: fork target does not match snapshot topology", ErrCheckpointMismatch)
	}
	if req.Patch.WorkflowInput == nil {
		return zero, errors.New("workflow: fork workflow input patch is required")
	}
	input, ok := req.Patch.WorkflowInput.Value.(I)
	if !ok {
		return zero, fmt.Errorf(
			"workflow: fork workflow input has type %T, want %s",
			req.Patch.WorkflowInput.Value,
			typeOf[I](),
		)
	}
	config := gopact.ResolveRunOptions(opts...)
	if err := config.RunConfigError(); err != nil {
		return zero, err
	}
	if config.RunID == s.RunMeta.RunID {
		return zero, errors.New("workflow: fork cannot reuse source run id")
	}
	association := forkAssociationOption{association: runlog.Association{SourceRunID: req.SourceRunID, SourceEventSeq: req.FromEventSeq}}
	forkOptions := append([]gopact.RunOption(nil), opts...)
	forkOptions = append(forkOptions, gopact.WithSessionID(s.RunMeta.SessionID), association)
	return compiled.Invoke(ctx, input, forkOptions...)
}

func (s Snapshot) checkpoint(sequence int64) (CheckpointView, bool) {
	for _, checkpoint := range s.Checkpoints {
		if checkpoint.EventSeq == sequence {
			return checkpoint, true
		}
	}
	return CheckpointView{}, false
}

type forkAssociationOption struct {
	association runlog.Association
}

func (option forkAssociationOption) ApplyRunOption(config *gopact.RunConfig) {
	if config.Extensions == nil {
		config.Extensions = make(map[string]any)
	}
	config.Extensions[sourceAssociationExtensionKey] = option.association
}

func cloneRunLogRecords(records []runlog.Record) []runlog.Record {
	cloned := make([]runlog.Record, len(records))
	for index, record := range records {
		record.Payload = append([]byte(nil), record.Payload...)
		if record.Metadata != nil {
			record.Metadata = cloneRunLogMetadata(record.Metadata)
		}
		cloned[index] = record
	}
	return cloned
}

func cloneRunLogMetadata(metadata map[string]string) map[string]string {
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
