package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type blockingSaveCheckpointer struct {
	*MemoryCheckpointer
	blocked chan struct{}
	release chan struct{}
	once    sync.Once
}

func (store *blockingSaveCheckpointer) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	if err := store.MemoryCheckpointer.Save(ctx, record, version); err != nil {
		return err
	}
	store.once.Do(func() {
		close(store.blocked)
		<-store.release
	})
	return nil
}

func TestEventFromRunLogRecordPreservesEventEnvelope(t *testing.T) {
	timestamp := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	record := runlog.Record{
		DefinitionID: "definition", DefinitionVersion: "version", SessionID: "session", RunID: "run",
		NodeID: "node", ActivationID: "activation", AttemptID: "attempt", RevisionID: "revision",
		ParentRunID: "parent", NodeExecutionVersion: 4, ExecutionEpoch: 5, SourceRevisionID: "source-revision",
		Sequence: 6, EventType: "event.type", Source: "source", Origin: "origin", Timestamp: timestamp,
		Summary: "summary", Payload: []byte(`{"key":"value"}`), PayloadRef: "payload-ref",
	}
	want := gopact.Event{
		DefinitionID: "definition", DefinitionVersion: "version", SessionID: "session", RunID: "run",
		NodeID: "node", ActivationID: "activation", AttemptID: "attempt", RevisionID: "revision",
		ParentRunID: "parent", NodeExecutionVersion: 4, ExecutionEpoch: 5, SourceRevisionID: "source-revision",
		Sequence: 6, Type: "event.type", Source: "source", Origin: "origin", Timestamp: timestamp,
		Summary: "summary", Payload: []byte(`{"key":"value"}`), PayloadRef: "payload-ref",
	}
	got := eventFromRunLogRecord(record)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eventFromRunLogRecord() = %+v, want %+v", got, want)
	}
	got.Payload[0] = 'x'
	if record.Payload[0] == 'x' {
		t.Fatal("event payload aliases durable runlog record")
	}
}

func TestWorkflowJournalReconciliationRejectsGapAndIdentityMismatch(t *testing.T) {
	execution := workflowExecution[string, string]{
		compiled:       &compiled[string, string]{name: "definition", topologyVersion: "version"},
		sessionID:      "session",
		runID:          "run",
		parentRunID:    "parent",
		executionEpoch: 2,
		sourceRevision: "source-revision",
		sourceRunID:    "source-run",
		sourceEventSeq: 5,
	}
	valid := runlog.Record{
		DefinitionID: "definition", DefinitionVersion: "version", SessionID: "session", RunID: "run",
		RevisionID: "revision", ParentRunID: "parent", Sequence: 7, ExecutionEpoch: 2,
		SourceRunID: "source-run", SourceEventSeq: 5, SourceRevisionID: "source-revision",
		EventType: "event.type", Source: "source", Timestamp: time.Now().UTC(),
	}
	if err := execution.validateJournalRecord(valid, 7); err != nil {
		t.Fatalf("validateJournalRecord(valid) error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*runlog.Record)
	}{
		{name: "sequence gap", mutate: func(record *runlog.Record) { record.Sequence++ }},
		{name: "session mismatch", mutate: func(record *runlog.Record) { record.SessionID = "other" }},
		{name: "run mismatch", mutate: func(record *runlog.Record) { record.RunID = "other" }},
		{name: "workflow mismatch", mutate: func(record *runlog.Record) { record.DefinitionID = "other" }},
		{name: "topology mismatch", mutate: func(record *runlog.Record) { record.DefinitionVersion = "other" }},
		{name: "parent mismatch", mutate: func(record *runlog.Record) { record.ParentRunID = "other" }},
		{name: "epoch mismatch", mutate: func(record *runlog.Record) { record.ExecutionEpoch++ }},
		{name: "source run mismatch", mutate: func(record *runlog.Record) { record.SourceRunID = "other" }},
		{name: "source event mismatch", mutate: func(record *runlog.Record) { record.SourceEventSeq++ }},
		{name: "source revision mismatch", mutate: func(record *runlog.Record) { record.SourceRevisionID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			test.mutate(&record)
			if err := execution.validateJournalRecord(record, 7); err == nil {
				t.Fatal("validateJournalRecord() error = nil, want rejection")
			}
		})
	}
}

func TestWorkflowJournalAheadRejectsSourceLineageMismatchBeforeWrites(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runlog.Record)
	}{
		{name: "source run", mutate: func(record *runlog.Record) { record.SourceRunID = "other" }},
		{name: "source event", mutate: func(record *runlog.Record) { record.SourceEventSeq++ }},
		{name: "source revision", mutate: func(record *runlog.Record) { record.SourceRevisionID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled := &compiled[string, string]{name: "definition", topologyVersion: "version"}
			checkpoint := CheckpointRecord{RunID: "run", SessionID: "session", ConfirmedSequence: 0}
			store := &recordingCheckpointer{records: map[string]CheckpointRecord{"run": checkpoint}}
			journal := runlog.NewMemoryLog()
			record := runlog.Record{
				DefinitionID: "definition", DefinitionVersion: "version", SessionID: "session", RunID: "run",
				RevisionID: "revision", Sequence: 1, ExecutionEpoch: 2, SourceRunID: "source-run",
				SourceEventSeq: 5, SourceRevisionID: "source-revision", EventType: "audit.custom",
				Source: "workflow", Timestamp: time.Now().UTC(),
			}
			test.mutate(&record)
			if err := journal.Append(t.Context(), record); err != nil {
				t.Fatal(err)
			}
			compiled.checkpointer, compiled.journal = store, journal
			delivered := 0
			execution := workflowExecution[string, string]{
				compiled: compiled, ctx: t.Context(), sessionID: "session", runID: "run", checkpoint: checkpoint,
				executionEpoch: 2, sourceRunID: "source-run", sourceEventSeq: 5, sourceRevision: "source-revision",
				replaySinks: []gopact.EventSink{gopact.EventSinkFunc(func(context.Context, gopact.Event) error {
					delivered++
					return nil
				})},
			}
			if err := execution.reconcileJournal(); err == nil {
				t.Fatal("reconcileJournal() error = nil, want lineage mismatch")
			}
			if delivered != 0 || len(store.saved) != 0 {
				t.Fatalf("delivered = %d, saves = %d, want zero writes and events", delivered, len(store.saved))
			}
		})
	}
}

func TestWorkflowEventSinkCannotSynchronouslyReenterEmitter(t *testing.T) {
	wf := New[string, string]("sink-reentry")
	node := testNode(wf, "work", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	var once sync.Once
	var reentryErr error
	done := make(chan error, 1)
	go func() {
		_, err := wf.Invoke(
			context.Background(),
			"input",
			gopact.WithEventHandler(func(ctx context.Context, _ gopact.Event) error {
				once.Do(func() {
					reentryErr = Emit(ctx, gopact.Event{Type: "audit.reentrant"})
				})
				return nil
			}),
		)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Invoke() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Invoke() deadlocked when an event sink called Emit")
	}
	if reentryErr == nil || reentryErr.Error() != "workflow: event emitter is not available" {
		t.Fatalf("Emit() error = %v, want event emitter unavailable", reentryErr)
	}
}

type reentrantFencedStore struct {
	*MemoryStore
	once       sync.Once
	reentryErr error
}

func (store *reentrantFencedStore) AppendFenced(ctx context.Context, record runlog.Record, fence runlog.Fence) error {
	store.once.Do(func() {
		store.reentryErr = Emit(ctx, gopact.Event{Type: "audit.reentrant"})
	})
	return store.MemoryStore.AppendFenced(ctx, record, fence)
}

func TestWorkflowFencedJournalCannotSynchronouslyReenterEmitter(t *testing.T) {
	store := &reentrantFencedStore{MemoryStore: NewMemoryStore()}
	wf := New[string, string](
		"fenced-journal-reentry",
		WithCheckpointer(store),
		WithJournal(store),
	)
	node := testNode(wf, "work", func(ctx context.Context, input string) (string, error) {
		return input, Emit(ctx, gopact.Event{Type: "audit.custom"})
	})
	wf.Entry(node)
	wf.Exit(node)
	done := make(chan error, 1)
	go func() {
		_, err := wf.Invoke(context.Background(), "input")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Invoke() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Invoke() deadlocked when a fenced journal called Emit")
	}
	if store.reentryErr == nil || store.reentryErr.Error() != "workflow: event emitter is not available" {
		t.Fatalf("Emit() error = %v, want event emitter unavailable", store.reentryErr)
	}
}

func TestWorkflowJournalReconciliationConfirmsEachPage(t *testing.T) {
	wf := New[string, string]("journal-pages")
	node := testNode(wf, "work", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	state := journalReconcileTestState()
	payload, err := encodeCheckpointPayloadWithMeta[string](
		state,
		nil,
		1,
		compiled.checkpointMeta(checkpointPayloadMeta{}),
	)
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	checkpoint := workflowCheckpointRecord(compiled, "run-1", 1, CheckpointRunning, payload)
	store := &recordingCheckpointer{records: map[string]CheckpointRecord{"run-1": checkpoint}}
	journal := runlog.NewMemoryLog()
	for sequence := int64(1); sequence <= journalReconcilePageSize+1; sequence++ {
		if err := journal.Append(context.Background(), runlog.Record{
			DefinitionID: compiled.name, DefinitionVersion: compiled.topologyVersion,
			SessionID: checkpoint.SessionID, RunID: checkpoint.RunID,
			RevisionID: fmt.Sprintf("run-1/revision-%d", sequence), Sequence: sequence,
			ExecutionEpoch: 1, EventType: "audit.custom", Source: "workflow", Origin: "natural",
			Timestamp: time.Date(2026, time.July, 13, 12, 0, 0, int(sequence), time.UTC),
		}); err != nil {
			t.Fatalf("Append(%d) error = %v", sequence, err)
		}
	}
	compiled.checkpointer = store
	compiled.journal = journal
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	var replayed []int64
	execution := workflowExecution[string, string]{
		compiled: compiled, ctx: ctx, sessionID: checkpoint.SessionID, runID: checkpoint.RunID,
		state: state, step: 1, checkpoint: checkpoint, cancel: cancel,
		replaySinks: []gopact.EventSink{strictWorkflowEventSink{EventSink: gopact.EventSinkFunc(
			func(_ context.Context, event gopact.Event) error {
				replayed = append(replayed, event.Sequence)
				return nil
			},
		)}},
		executionEpoch: 1, controlOrigin: "natural",
	}
	if err := execution.reconcileJournal(); err != nil {
		t.Fatalf("reconcileJournal() error = %v", err)
	}
	if len(replayed) != journalReconcilePageSize+1 || execution.eventCursor != journalReconcilePageSize+1 {
		t.Fatalf("replayed = %d cursor = %d, want %d", len(replayed), execution.eventCursor, journalReconcilePageSize+1)
	}
	if len(store.saved) != 2 {
		t.Fatalf("saved checkpoints = %d, want two page confirmations", len(store.saved))
	}
	if store.saved[0].ConfirmedSequence != journalReconcilePageSize ||
		store.saved[1].ConfirmedSequence != journalReconcilePageSize+1 {
		t.Fatalf("saved cursors = %v, want %d then %d", []int64{
			store.saved[0].ConfirmedSequence,
			store.saved[1].ConfirmedSequence,
		}, journalReconcilePageSize, journalReconcilePageSize+1)
	}
	for index, sequence := range replayed {
		if sequence != int64(index+1) {
			t.Fatalf("replayed[%d] = %d, want %d", index, sequence, index+1)
		}
	}
}

func TestWorkflowCommittedAndCustomEventsReserveSequenceSerially(t *testing.T) {
	wf := New[string, string]("sequence-serialization")
	node := testNode(wf, "work", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := &blockingSaveCheckpointer{
		MemoryCheckpointer: NewMemoryCheckpointer(),
		blocked:            make(chan struct{}),
		release:            make(chan struct{}),
	}
	compiled.checkpointer = store
	state := journalReconcileTestState()
	payload, err := encodeCheckpointPayloadWithMeta[string](
		state,
		nil,
		1,
		compiled.checkpointMeta(checkpointPayloadMeta{}),
	)
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	checkpoint := workflowCheckpointRecord(compiled, "run-1", 1, CheckpointRunning, payload)
	if err := store.Create(context.Background(), checkpoint); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	journal := runlog.NewMemoryLog()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	execution := workflowExecution[string, string]{
		compiled:   compiled,
		ctx:        ctx,
		sessionID:  checkpoint.SessionID,
		runID:      checkpoint.RunID,
		ownerID:    "owner",
		state:      state,
		step:       1,
		checkpoint: checkpoint,
		cancel:     cancel,
		eventSinks: []gopact.EventSink{
			strictWorkflowEventSink{EventSink: runlog.NewSink(journal)},
		},
		executionEpoch: 1,
		controlOrigin:  "natural",
	}
	commitDone := make(chan error, 1)
	go func() {
		commitDone <- execution.commitRunningEvent(gopact.Event{Type: EventWorkflowStarted}, 1)
	}()
	<-store.blocked
	if execution.eventMu.TryLock() {
		execution.eventMu.Unlock()
		close(store.release)
		<-commitDone
		t.Fatal("event sequence lock was not held across pending checkpoint persistence")
	}
	customDone := make(chan error, 1)
	go func() {
		customDone <- execution.emitEvent(gopact.Event{Type: "audit.custom"})
	}()
	close(store.release)
	if err := <-commitDone; err != nil {
		t.Fatalf("commitRunningEvent() error = %v", err)
	}
	if err := <-customDone; err != nil {
		t.Fatalf("emitEvent() error = %v", err)
	}
	records, err := journal.List(context.Background(), runlog.Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %+v, want two events", records)
	}
	wantRevisions := []string{"run-1/revision-1", "run-1/revision-2"}
	for index, record := range records {
		wantSequence := int64(index + 1)
		wantRevision := wantRevisions[index]
		if record.Sequence != wantSequence || record.RevisionID != wantRevision {
			t.Fatalf("record %d = sequence %d revision %q, want %d and %q", index, record.Sequence, record.RevisionID, wantSequence, wantRevision)
		}
	}
}

func TestWorkflowStaleOwnerCannotAppendRawEventAfterClaim(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
	}{
		{name: "custom", eventType: "audit.custom"},
		{name: "guard rejected", eventType: EventGuardRejected},
		{name: "node failed", eventType: EventNodeFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wf := New[string, string]("stale-owner-fence")
			node := testNode(wf, "work", func(_ context.Context, input string) (string, error) { return input, nil })
			wf.Entry(node)
			wf.Exit(node)
			compiled, err := wf.compile()
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			state := journalReconcileTestState()
			past := time.Now().Add(-time.Minute)
			payload, err := encodeCheckpointPayloadWithMeta[string](
				state,
				nil,
				1,
				compiled.checkpointMeta(checkpointPayloadMeta{
					OwnerID: "owner-old", LeaseExpiresAt: past, ClaimSequence: 1,
				}),
			)
			if err != nil {
				t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
			}
			checkpoint := workflowCheckpointRecord(compiled, "run-1", 1, CheckpointRunning, payload)
			checkpoint.OwnerID = "owner-old"
			checkpoint.LeaseExpiresAt = past
			checkpoint.ClaimSequence = 1
			store := NewMemoryCheckpointer()
			if err := store.Create(t.Context(), checkpoint); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			claimed := checkpoint
			claimed.OwnerID = "owner-new"
			claimed.LeaseExpiresAt = time.Now().Add(time.Minute)
			claimed.ClaimSequence++
			if err := store.Claim(t.Context(), claimed, checkpoint.Version); err != nil {
				t.Fatalf("Claim() error = %v", err)
			}
			journal := runlog.NewMemoryLog()
			compiled.checkpointer = store
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			execution := workflowExecution[string, string]{
				compiled: compiled, ctx: ctx, sessionID: checkpoint.SessionID, runID: checkpoint.RunID,
				ownerID: "owner-old", state: state, step: 1, checkpoint: checkpoint, cancel: cancel,
				eventSinks: []gopact.EventSink{
					strictWorkflowEventSink{EventSink: runlog.NewSink(journal)},
				},
				executionEpoch: 1, controlOrigin: "natural",
			}
			if err := execution.emitEvent(gopact.Event{Type: test.eventType}); !errors.Is(err, ErrCheckpointLeaseLost) {
				t.Fatalf("emitEvent() error = %v, want ErrCheckpointLeaseLost", err)
			}
			if cause := context.Cause(ctx); !errors.Is(cause, ErrCheckpointLeaseLost) {
				t.Fatalf("execution context cause = %v, want ErrCheckpointLeaseLost", cause)
			}
			records, err := journal.List(t.Context(), runlog.Query{RunID: checkpoint.RunID})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(records) != 0 {
				t.Fatalf("records = %+v, want stale owner to append nothing", records)
			}
		})
	}
}

func TestWorkflowObservedEventUsesFencedJournalWithoutCheckpointHistory(t *testing.T) {
	wf := New[string, string]("fenced-observed-event")
	node := testNode(wf, "work", func(_ context.Context, input string) (string, error) { return input, nil })
	wf.Entry(node)
	wf.Exit(node)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	state := journalReconcileTestState()
	now := time.Now().UTC()
	payload, err := encodeCheckpointPayloadWithMeta[string](
		state,
		nil,
		1,
		compiled.checkpointMeta(checkpointPayloadMeta{
			OwnerID: "owner-1", LeaseExpiresAt: now.Add(time.Minute), ClaimSequence: 1,
		}),
	)
	if err != nil {
		t.Fatalf("encodeCheckpointPayloadWithMeta() error = %v", err)
	}
	checkpoint := workflowCheckpointRecord(compiled, "run-1", 1, CheckpointRunning, payload)
	checkpoint.OwnerID = "owner-1"
	checkpoint.LeaseExpiresAt = now.Add(time.Minute)
	checkpoint.ClaimSequence = 1
	store := NewMemoryStore()
	if err := store.Create(t.Context(), checkpoint); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	compiled.checkpointer = store
	compiled.journal = store
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	execution := workflowExecution[string, string]{
		compiled: compiled, ctx: ctx, sessionID: checkpoint.SessionID, runID: checkpoint.RunID,
		ownerID: checkpoint.OwnerID, state: state, step: 1, checkpoint: checkpoint, cancel: cancel,
		eventSinks: []gopact.EventSink{
			strictWorkflowEventSink{EventSink: runlog.NewSink(store)},
		},
		executionEpoch: 1, controlOrigin: "natural",
	}
	if err := execution.emitEvent(gopact.Event{Type: "audit.custom"}); err != nil {
		t.Fatalf("emitEvent() error = %v", err)
	}
	loaded, err := store.Load(t.Context(), checkpoint.RunID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	history, err := store.ListCheckpoints(t.Context(), CheckpointHistoryRequest{RunID: checkpoint.RunID})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: checkpoint.RunID})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if loaded.Version != checkpoint.Version || len(history) != 1 {
		t.Fatalf("checkpoint version = %d history = %d, want version %d and one state snapshot", loaded.Version, len(history), checkpoint.Version)
	}
	if len(records) != 1 || records[0].EventType != "audit.custom" || records[0].Sequence != 1 {
		t.Fatalf("records = %+v, want one fenced custom event", records)
	}
}

func TestWorkflowSeparateFencedStoresUseCheckpointReservation(t *testing.T) {
	checkpointStore := NewMemoryStore()
	journalStore := NewMemoryStore()
	wf := New[string, string](
		"separate-fenced-stores",
		WithCheckpointer(checkpointStore),
		WithJournal(journalStore),
	)
	node := testNode(wf, "work", func(ctx context.Context, input string) (string, error) {
		return input, Emit(ctx, gopact.Event{Type: "audit.custom"})
	})
	wf.Entry(node)
	wf.Exit(node)
	if _, err := wf.Invoke(t.Context(), "input", gopact.WithRunID("run-separate-stores")); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	records, err := journalStore.List(t.Context(), runlog.Query{RunID: "run-separate-stores"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	found := false
	for _, record := range records {
		found = found || record.EventType == "audit.custom"
	}
	if !found {
		t.Fatalf("records = %+v, want custom event through separate journal", records)
	}
	history, err := checkpointStore.ListCheckpoints(t.Context(), CheckpointHistoryRequest{RunID: "run-separate-stores"})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(history) < 2 {
		t.Fatalf("checkpoint history = %d, want fallback reservation versions", len(history))
	}
}

func journalReconcileTestState() runState {
	return runState{
		queue:        []activation{{id: "act-1", node: "work", input: "input"}},
		activations:  map[string]*activationRecord{},
		scheduled:    map[string]int{"work": 1},
		completed:    map[string]int{},
		nodeVersions: map[string]int64{},
		buckets:      map[joinBucketKey]*joinBucket{},
		correlations: map[CorrelationKey]map[string]int{},
		sourceSets:   map[string]*sourceSet{},
		iterSources:  map[string]*iterSource{},
		liveIters:    map[string]*liveIterator{},
	}
}

func TestWorkflowResumeReconcilesJournalAheadOfCheckpoint(t *testing.T) {
	store := NewMemoryStore()
	wf := New[string, string](
		"journal-reconcile",
		WithStrictCheckpointer(store),
		WithStrictJournal(store),
	)
	bodyRuns := 0
	node := testNode(wf, "work", func(ctx context.Context, input string) (string, error) {
		bodyRuns++
		if err := Emit(ctx, gopact.Event{
			Type: "audit.custom", Summary: "side effect accepted", Payload: []byte(`{"accepted":true}`),
		}); err != nil {
			return "", err
		}
		return input + "-done", nil
	})
	wf.Entry(node)
	wf.Exit(node)

	sinkErr := errors.New("consumer unavailable")
	_, err := wf.Invoke(
		context.Background(),
		"input",
		gopact.WithRunID("run-1"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == "audit.custom" {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
	records, err := store.List(context.Background(), runlog.Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var accepted runlog.Record
	for _, record := range records {
		if record.EventType == "audit.custom" {
			accepted = record
			break
		}
	}
	if accepted.Sequence == 0 {
		t.Fatalf("records = %+v, want journaled audit.custom", records)
	}
	checkpoint, err := store.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if checkpoint.ConfirmedSequence >= accepted.Sequence {
		t.Fatalf("checkpoint cursor = %d, want behind journal sequence %d", checkpoint.ConfirmedSequence, accepted.Sequence)
	}

	checkpointSinkErr := errors.New("checkpoint consumer unavailable")
	var firstResume []gopact.Event
	_, err = wf.Invoke(
		context.Background(),
		"ignored",
		WithResume(ResumeRequest{RunID: "run-1"}),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			firstResume = append(firstResume, event)
			if event.Type == EventCheckpointLoaded {
				return checkpointSinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, checkpointSinkErr) {
		t.Fatalf("first Resume() error = %v, want %v", err, checkpointSinkErr)
	}
	if len(firstResume) < 2 || firstResume[0].Type != "audit.custom" ||
		firstResume[0].Sequence != accepted.Sequence || firstResume[1].Type != EventCheckpointLoaded {
		t.Fatalf("first resumed events = %+v, want reconciled custom event before checkpoint.loaded", firstResume)
	}
	failedCheckpointLoaded := firstResume[1]

	var resumed []gopact.Event
	output, err := wf.Invoke(
		context.Background(),
		"ignored",
		WithResume(ResumeRequest{RunID: "run-1"}),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			resumed = append(resumed, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("second Resume() error = %v", err)
	}
	if output != "input-done" {
		t.Fatalf("second Resume() output = %q, want input-done", output)
	}
	if len(resumed) == 0 || resumed[0].Type != EventCheckpointLoaded || resumed[0].Sequence != failedCheckpointLoaded.Sequence {
		t.Fatalf("second resumed events = %+v, want failed checkpoint.loaded replayed first", resumed)
	}
	if bodyRuns != 2 {
		t.Fatalf("body runs = %d, want resumed running activation to execute again", bodyRuns)
	}

	records, err = store.List(context.Background(), runlog.Query{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() after resume error = %v", err)
	}
	for index, record := range records {
		want := int64(index + 1)
		if record.Sequence != want {
			t.Fatalf("record %d sequence = %d, want %d", index, record.Sequence, want)
		}
	}
}

func TestWorkflowResumeReplaysNodeFailedBeforeRetryingActivation(t *testing.T) {
	store := NewMemoryStore()
	wantErr := errors.New("node failed")
	bodyRuns := 0
	wf := New[string, string](
		"node-failed-reconcile",
		WithStrictCheckpointer(store),
		WithStrictJournal(store),
	)
	node := testNode(wf, "work", func(_ context.Context, _ string) (string, error) {
		bodyRuns++
		return "", wantErr
	})
	wf.Entry(node)
	wf.Exit(node)

	sinkErr := errors.New("node failed consumer unavailable")
	_, err := wf.Invoke(
		t.Context(),
		"input",
		gopact.WithRunID("run-node-failed"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventNodeFailed {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: "run-node-failed"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var firstFailure runlog.Record
	for _, record := range records {
		if record.EventType == EventNodeFailed {
			firstFailure = record
			break
		}
	}
	if firstFailure.Sequence == 0 {
		t.Fatalf("records = %+v, want journaled node.failed", records)
	}

	var resumed []gopact.Event
	_, err = wf.Invoke(
		t.Context(),
		"ignored",
		WithResume(ResumeRequest{RunID: "run-node-failed"}),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			resumed = append(resumed, event)
			return nil
		}),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Resume() error = %v, want %v", err, wantErr)
	}
	if bodyRuns != 2 {
		t.Fatalf("body runs = %d, want failed activation retried once", bodyRuns)
	}
	if len(resumed) == 0 || resumed[0].Type != EventNodeFailed ||
		resumed[0].Sequence != firstFailure.Sequence || resumed[0].RevisionID != firstFailure.RevisionID ||
		!resumed[0].Timestamp.Equal(firstFailure.Timestamp) {
		t.Fatalf("resumed events = %+v, want original node.failed replayed first", resumed)
	}
	records, err = store.List(t.Context(), runlog.Query{RunID: "run-node-failed"})
	if err != nil {
		t.Fatalf("List() after Resume error = %v", err)
	}
	matchingSequence := 0
	for _, record := range records {
		if record.Sequence == firstFailure.Sequence {
			matchingSequence++
		}
	}
	if matchingSequence != 1 {
		t.Fatalf("sequence %d records = %d, want one idempotent journal record", firstFailure.Sequence, matchingSequence)
	}
}

func TestWorkflowResumeReplaysGuardRejectedBeforeRetryingActivation(t *testing.T) {
	store := NewMemoryStore()
	guardCalls := 0
	bodyRuns := 0
	wf := New[string, string](
		"guard-rejected-reconcile",
		WithStrictCheckpointer(store),
		WithStrictJournal(store),
	)
	node := testNode(wf, "work", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input, nil
	})
	node.Guard(BeforeRun("policy", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			guardCalls++
			return GuardReject[string, string]{
				Rejection: gopact.GuardRejection{Reason: "blocked"},
			}, nil
		},
	)))
	wf.Entry(node)
	wf.Exit(node)

	sinkErr := errors.New("guard rejected consumer unavailable")
	_, err := wf.Invoke(
		t.Context(),
		"input",
		gopact.WithRunID("run-guard-rejected"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventGuardRejected {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, sinkErr)
	}
	records, err := store.List(t.Context(), runlog.Query{RunID: "run-guard-rejected"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var firstRejection runlog.Record
	for _, record := range records {
		if record.EventType == EventGuardRejected {
			firstRejection = record
			break
		}
	}
	if firstRejection.Sequence == 0 {
		t.Fatalf("records = %+v, want journaled guard.rejected", records)
	}

	var resumed []gopact.Event
	_, err = wf.Invoke(
		t.Context(),
		"ignored",
		WithResume(ResumeRequest{RunID: "run-guard-rejected"}),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			resumed = append(resumed, event)
			return nil
		}),
	)
	var rejection gopact.GuardRejection
	if !errors.As(err, &rejection) {
		t.Fatalf("Resume() error = %v, want GuardRejection", err)
	}
	if guardCalls != 2 || bodyRuns != 0 {
		t.Fatalf("guard calls = %d body runs = %d, want 2 and 0", guardCalls, bodyRuns)
	}
	if len(resumed) == 0 || resumed[0].Type != EventGuardRejected ||
		resumed[0].Sequence != firstRejection.Sequence || resumed[0].RevisionID != firstRejection.RevisionID ||
		!resumed[0].Timestamp.Equal(firstRejection.Timestamp) {
		t.Fatalf("resumed events = %+v, want original guard.rejected replayed first", resumed)
	}
}
