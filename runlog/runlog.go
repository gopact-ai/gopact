// Package runlog provides a cross-runtime append-only execution log.
package runlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrNilLog reports an operation without a Log target.
	ErrNilLog = errors.New("runlog: log is nil")
	// ErrInvalidRecord reports a record without valid event identity.
	ErrInvalidRecord = errors.New("runlog: invalid record")
	// ErrInvalidQuery reports invalid query bounds.
	ErrInvalidQuery = errors.New("runlog: invalid query")
	// ErrHistoryCompacted reports a query whose requested history is no longer retained.
	ErrHistoryCompacted = errors.New("runlog: history compacted")
	// ErrConflict reports different content for an existing run sequence.
	ErrConflict = errors.New("runlog: record conflict")
)

// Log is an append-only durable execution log.
type Log interface {
	Append(context.Context, Record) error
	List(context.Context, Query) ([]Record, error)
}

// Fence identifies the workflow ownership claim authorizing one append.
type Fence struct {
	OwnerID       string
	ClaimSequence int64
}

// FencedLog atomically validates workflow ownership and appends one record.
// A combined workflow Store must serialize AppendFenced with Claim, RenewLease,
// Save, Finish, and ownership release so none can change the checked claim
// between validation and append. A rejected or expired fence must not append or
// alter a record and must return the workflow lease-lost sentinel expected by
// the caller.
type FencedLog interface {
	Log
	AppendFenced(context.Context, Record, Fence) error
}

// Association identifies the historical point that created an independent fork.
type Association struct {
	SourceRunID    string
	SourceEventSeq int64
}

// AssociatingSink returns a sink configured with fork source metadata.
type AssociatingSink interface {
	Associate(Association) gopact.EventSink
}

// Record is the generic read-model input for one accepted runtime event.
type Record struct {
	DefinitionID         string
	DefinitionVersion    string
	SessionID            string
	RunID                string
	NodeID               string
	ActivationID         string
	AttemptID            string
	RevisionID           string
	ParentRunID          string
	SourceRunID          string
	SourceEventSeq       int64
	Sequence             int64
	NodeExecutionVersion int64
	ExecutionEpoch       int64
	SourceRevisionID     string
	EventType            string
	Phase                string
	Source               string
	Origin               string
	Summary              string
	Payload              json.RawMessage
	PayloadRef           string
	CheckpointID         string
	ErrorKind            string
	ErrorMessage         string
	Timestamp            time.Time
	Metadata             map[string]string
}

// Query filters records while preserving append order.
type Query struct {
	SessionID string
	RunID     string
	After     int64
	Limit     int
}

// Sink projects accepted runtime events into a Log.
type Sink struct {
	log         Log
	association Association
}

// NewSink creates an event sink backed by log.
func NewSink(log Log) Sink {
	return Sink{log: log}
}

// Associate returns an independent sink that records fork source metadata.
func (s Sink) Associate(association Association) gopact.EventSink {
	s.association = association
	return s
}

// Emit implements gopact.EventSink.
func (s Sink) Emit(ctx context.Context, event gopact.Event) error {
	if s.log == nil {
		return ErrNilLog
	}
	record := RecordFromEvent(event)
	record.SourceRunID = s.association.SourceRunID
	record.SourceEventSeq = s.association.SourceEventSeq
	return s.log.Append(ctx, record)
}

// RecordFromEvent projects an event into its durable RunLog representation.
func RecordFromEvent(event gopact.Event) Record {
	return Record{
		DefinitionID:         event.DefinitionID,
		DefinitionVersion:    event.DefinitionVersion,
		SessionID:            event.SessionID,
		RunID:                event.RunID,
		NodeID:               event.NodeID,
		ActivationID:         event.ActivationID,
		AttemptID:            event.AttemptID,
		RevisionID:           event.RevisionID,
		ParentRunID:          event.ParentRunID,
		Sequence:             event.Sequence,
		NodeExecutionVersion: event.NodeExecutionVersion,
		ExecutionEpoch:       event.ExecutionEpoch,
		SourceRevisionID:     event.SourceRevisionID,
		EventType:            event.Type,
		Source:               event.Source,
		Origin:               event.Origin,
		Summary:              event.Summary,
		Payload:              append(json.RawMessage(nil), event.Payload...),
		PayloadRef:           event.PayloadRef,
		Timestamp:            event.Timestamp,
	}
}

// MemoryLog is an in-memory Log for tests and local execution views.
type MemoryLog struct {
	mu      sync.RWMutex
	records []Record
	byKey   map[recordKey]Record
}

type recordKey struct {
	runID    string
	sequence int64
}

// NewMemoryLog creates an empty in-memory log.
func NewMemoryLog() *MemoryLog {
	return &MemoryLog{byKey: make(map[recordKey]Record)}
}

// Append appends record idempotently by run ID and sequence.
func (l *MemoryLog) Append(ctx context.Context, record Record) error {
	if l == nil {
		return ErrNilLog
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRecord(record); err != nil {
		return err
	}
	record = cloneRecord(record)
	key := recordKey{runID: record.RunID, sequence: record.Sequence}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byKey == nil {
		l.byKey = make(map[recordKey]Record)
	}
	if existing, ok := l.byKey[key]; ok {
		if reflect.DeepEqual(existing, record) {
			return nil
		}
		return fmt.Errorf("%w for %s/%d", ErrConflict, record.RunID, record.Sequence)
	}
	l.byKey[key] = record
	l.records = append(l.records, record)
	return nil
}

// List returns records matching query in global append order.
func (l *MemoryLog) List(ctx context.Context, query Query) ([]Record, error) {
	if l == nil {
		return nil, ErrNilLog
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if query.After < 0 || query.Limit < 0 {
		return nil, fmt.Errorf("%w: after and limit must not be negative", ErrInvalidQuery)
	}
	if query.SessionID != "" && query.RunID == "" && query.After != 0 {
		return nil, fmt.Errorf("%w: after requires a run id for session queries", ErrInvalidQuery)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Record, 0, min(query.Limit, len(l.records)))
	for _, record := range l.records {
		if query.SessionID != "" && record.SessionID != query.SessionID {
			continue
		}
		if query.RunID != "" && record.RunID != query.RunID {
			continue
		}
		if record.Sequence <= query.After {
			continue
		}
		out = append(out, cloneRecord(record))
		if query.Limit > 0 && len(out) == query.Limit {
			break
		}
	}
	return out, nil
}

func validateRecord(record Record) error {
	switch {
	case record.SessionID == "":
		return fmt.Errorf("%w: session id is required", ErrInvalidRecord)
	case record.RunID == "":
		return fmt.Errorf("%w: run id is required", ErrInvalidRecord)
	case record.Sequence <= 0:
		return fmt.Errorf("%w: sequence must be positive", ErrInvalidRecord)
	case record.EventType == "":
		return fmt.Errorf("%w: event type is required", ErrInvalidRecord)
	case record.Source == "":
		return fmt.Errorf("%w: source is required", ErrInvalidRecord)
	case record.Timestamp.IsZero():
		return fmt.Errorf("%w: timestamp is required", ErrInvalidRecord)
	case (record.SourceRunID == "") != (record.SourceEventSeq == 0):
		return fmt.Errorf("%w: fork source run and event sequence must be set together", ErrInvalidRecord)
	case record.SourceEventSeq < 0:
		return fmt.Errorf("%w: source event sequence must not be negative", ErrInvalidRecord)
	case record.SourceRunID == "" && record.SourceRevisionID != "":
		return fmt.Errorf("%w: source revision requires a source run", ErrInvalidRecord)
	default:
		return nil
	}
}

func cloneRecord(record Record) Record {
	record.Payload = append(json.RawMessage(nil), record.Payload...)
	if record.Metadata != nil {
		metadata := make(map[string]string, len(record.Metadata))
		for key, value := range record.Metadata {
			metadata[key] = value
		}
		record.Metadata = metadata
	}
	return record
}
