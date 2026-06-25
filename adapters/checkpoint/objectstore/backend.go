// Package objectstore adapts conditional object clients to checkpoint.RowBackend.
package objectstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact/checkpoint"
)

const defaultMaxIndexCASRetries = 8

var (
	ErrClientRequired                    = errors.New("checkpoint objectstore: client is required")
	ErrNotFound                          = errors.New("checkpoint objectstore: not found")
	ErrPreconditionFailed                = errors.New("checkpoint objectstore: precondition failed")
	ErrNotFoundMatcherRequired           = errors.New("checkpoint objectstore: not found matcher is required")
	ErrPreconditionFailedMatcherRequired = errors.New("checkpoint objectstore: precondition failed matcher is required")
	ErrIndexCASRetriesRequired           = errors.New("checkpoint objectstore: index cas retries is required")
	ErrIndexCASConflict                  = errors.New("checkpoint objectstore: index cas conflict")
	ErrInvalidIndexObject                = errors.New("checkpoint objectstore: invalid index object")
	ErrUnsafePrefix                      = errors.New("checkpoint objectstore: unsafe prefix")
)

// Client is the minimal conditional object storage contract consumed by Backend.
type Client interface {
	GetObject(ctx context.Context, key string) (Object, error)
	PutObject(ctx context.Context, object Object, precondition Precondition) (Object, error)
}

// Object is one provider object payload plus its native CAS version.
type Object struct {
	Key       string
	Data      []byte
	Version   string
	UpdatedAt time.Time
	Metadata  map[string]string
}

// Precondition describes the CAS condition attached to a write.
type Precondition struct {
	IfAbsent  bool
	IfVersion string
}

// Backend persists checkpoint records and thread indexes through conditional object writes.
type Backend struct {
	client               Client
	prefix               string
	isNotFound           func(error) bool
	isPreconditionFailed func(error) bool
	maxIndexCASRetries   int
}

var _ checkpoint.RowBackend = (*Backend)(nil)

// IndexConsistencyReport describes the current thread index state before any
// optional repair write. ValidRecordIDs is the sanitized index order that
// RepairIndex writes back when Repaired is true.
type IndexConsistencyReport struct {
	ThreadID             string
	IndexThreadID        string
	IndexExists          bool
	IndexedRecordIDs     []string
	ValidRecordIDs       []string
	DuplicateRecordIDs   []string
	MissingRecordIDs     []string
	WrongThreadRecordIDs []string
	ThreadIDMismatch     bool
	Consistent           bool
	Repaired             bool
}

// Option configures an object checkpoint backend.
type Option func(*backendConfig) error

type backendConfig struct {
	prefix               string
	isNotFound           func(error) bool
	isPreconditionFailed func(error) bool
	maxIndexCASRetries   int
}

// NewBackend creates a checkpoint row backend backed by a conditional object client.
func NewBackend(client Client, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	cfg := backendConfig{
		isNotFound: func(err error) bool {
			return errors.Is(err, ErrNotFound)
		},
		isPreconditionFailed: func(err error) bool {
			return errors.Is(err, ErrPreconditionFailed)
		},
		maxIndexCASRetries: defaultMaxIndexCASRetries,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.isNotFound == nil {
		return nil, ErrNotFoundMatcherRequired
	}
	if cfg.isPreconditionFailed == nil {
		return nil, ErrPreconditionFailedMatcherRequired
	}
	if cfg.maxIndexCASRetries <= 0 {
		return nil, ErrIndexCASRetriesRequired
	}
	return &Backend{
		client:               client,
		prefix:               cfg.prefix,
		isNotFound:           cfg.isNotFound,
		isPreconditionFailed: cfg.isPreconditionFailed,
		maxIndexCASRetries:   cfg.maxIndexCASRetries,
	}, nil
}

// WithPrefix scopes all physical object keys under prefix.
func WithPrefix(prefix string) Option {
	return func(cfg *backendConfig) error {
		normalized, err := normalizePrefix(prefix)
		if err != nil {
			return err
		}
		cfg.prefix = normalized
		return nil
	}
}

// WithNotFound lets provider adapters map native object not-found errors.
func WithNotFound(match func(error) bool) Option {
	return func(cfg *backendConfig) error {
		if match == nil {
			return ErrNotFoundMatcherRequired
		}
		cfg.isNotFound = func(err error) bool {
			return errors.Is(err, ErrNotFound) || match(err)
		}
		return nil
	}
}

// WithPreconditionFailed lets provider adapters map native conditional-write failures.
func WithPreconditionFailed(match func(error) bool) Option {
	return func(cfg *backendConfig) error {
		if match == nil {
			return ErrPreconditionFailedMatcherRequired
		}
		cfg.isPreconditionFailed = func(err error) bool {
			return errors.Is(err, ErrPreconditionFailed) || match(err)
		}
		return nil
	}
}

// WithMaxIndexCASRetries limits index update retries after conditional-write conflicts.
func WithMaxIndexCASRetries(maxRetries int) Option {
	return func(cfg *backendConfig) error {
		if maxRetries <= 0 {
			return ErrIndexCASRetriesRequired
		}
		cfg.maxIndexCASRetries = maxRetries
		return nil
	}
}

// UpsertRecord stores or replaces one checkpoint record and then CAS-updates its thread index.
func (b *Backend) UpsertRecord(ctx context.Context, record checkpoint.Record) error {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.ID == "" {
		return errors.New("checkpoint objectstore: record id is required")
	}
	raw, err := encode(record)
	if err != nil {
		return err
	}
	if _, err := b.client.PutObject(ctx, Object{
		Key:  b.recordKey(record.ID),
		Data: raw,
	}, Precondition{}); err != nil {
		return fmt.Errorf("checkpoint objectstore: put record: %w", err)
	}
	if err := b.updateThreadIndex(ctx, record.ThreadID, record.ID); err != nil {
		return err
	}
	return nil
}

// GetRecord returns one checkpoint record by id.
func (b *Backend) GetRecord(ctx context.Context, id string) (checkpoint.Record, bool, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return checkpoint.Record{}, false, err
	}
	object, err := b.client.GetObject(ctx, b.recordKey(id))
	if b.isNotFound(err) {
		return checkpoint.Record{}, false, nil
	}
	if err != nil {
		return checkpoint.Record{}, false, fmt.Errorf("checkpoint objectstore: get record: %w", err)
	}
	record, err := decodeRecord(object.Data)
	if err != nil {
		return checkpoint.Record{}, false, err
	}
	return record, true, nil
}

// ListRecords returns checkpoint records for one thread.
func (b *Backend) ListRecords(ctx context.Context, threadID string) ([]checkpoint.Record, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	object, err := b.client.GetObject(ctx, b.threadKey(threadID))
	if b.isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint objectstore: get thread index: %w", err)
	}
	index, err := decodeThreadIndex(object.Data)
	if err != nil {
		return nil, err
	}
	records := make([]checkpoint.Record, 0, len(index.IDs))
	seen := make(map[string]struct{}, len(index.IDs))
	for _, id := range index.IDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		object, err := b.client.GetObject(ctx, b.recordKey(id))
		if b.isNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("checkpoint objectstore: get indexed record %q: %w", id, err)
		}
		record, err := decodeRecord(object.Data)
		if err != nil {
			return nil, err
		}
		if record.ThreadID != threadID {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// VerifyIndex checks whether one thread index only references existing records
// that belong to the same thread, without mutating storage.
func (b *Backend) VerifyIndex(ctx context.Context, threadID string) (IndexConsistencyReport, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return IndexConsistencyReport{}, err
	}
	_, report, err := b.inspectThreadIndex(ctx, threadID)
	return report, err
}

// RepairIndex rewrites one thread index with the verified record IDs that still
// exist and belong to the requested thread. It uses the same CAS retry policy as
// normal checkpoint writes.
func (b *Backend) RepairIndex(ctx context.Context, threadID string) (IndexConsistencyReport, error) {
	ctx = safeContext(ctx)
	if err := ctx.Err(); err != nil {
		return IndexConsistencyReport{}, err
	}
	var lastReport IndexConsistencyReport
	for attempt := 0; attempt < b.maxIndexCASRetries; attempt++ {
		object, report, err := b.inspectThreadIndex(ctx, threadID)
		if err != nil {
			return report, err
		}
		lastReport = report
		if report.Consistent {
			return report, nil
		}
		raw, err := encode(threadIndex{
			ThreadID: threadID,
			IDs:      append([]string(nil), report.ValidRecordIDs...),
		})
		if err != nil {
			return report, err
		}
		precondition := Precondition{IfAbsent: true}
		if report.IndexExists {
			if object.Version == "" {
				return report, ErrInvalidIndexObject
			}
			precondition = Precondition{IfVersion: object.Version}
		}
		_, err = b.client.PutObject(ctx, Object{
			Key:  b.threadKey(threadID),
			Data: raw,
		}, precondition)
		if b.isPreconditionFailed(err) {
			continue
		}
		if err != nil {
			return report, fmt.Errorf("checkpoint objectstore: repair thread index: %w", err)
		}
		report.Repaired = true
		return report, nil
	}
	if lastReport.ThreadID == "" {
		lastReport.ThreadID = threadID
	}
	return lastReport, ErrIndexCASConflict
}

func (b *Backend) inspectThreadIndex(ctx context.Context, threadID string) (Object, IndexConsistencyReport, error) {
	object, index, exists, err := b.getThreadIndex(ctx, threadID)
	if err != nil {
		return Object{}, IndexConsistencyReport{ThreadID: threadID}, err
	}
	report := IndexConsistencyReport{
		ThreadID:         threadID,
		IndexThreadID:    index.ThreadID,
		IndexExists:      exists,
		IndexedRecordIDs: append([]string(nil), index.IDs...),
	}
	if !exists {
		report.Consistent = true
		return object, report, nil
	}
	if index.ThreadID != "" && index.ThreadID != threadID {
		report.ThreadIDMismatch = true
	}
	seen := make(map[string]struct{}, len(index.IDs))
	for _, id := range index.IDs {
		if _, ok := seen[id]; ok {
			report.DuplicateRecordIDs = appendUniqueString(report.DuplicateRecordIDs, id)
			continue
		}
		seen[id] = struct{}{}
		recordObject, err := b.client.GetObject(ctx, b.recordKey(id))
		if b.isNotFound(err) {
			report.MissingRecordIDs = appendUniqueString(report.MissingRecordIDs, id)
			continue
		}
		if err != nil {
			return object, report, fmt.Errorf("checkpoint objectstore: get indexed record %q: %w", id, err)
		}
		record, err := decodeRecord(recordObject.Data)
		if err != nil {
			return object, report, err
		}
		if record.ThreadID != threadID {
			report.WrongThreadRecordIDs = appendUniqueString(report.WrongThreadRecordIDs, id)
			continue
		}
		report.ValidRecordIDs = append(report.ValidRecordIDs, id)
	}
	report.Consistent = !report.ThreadIDMismatch &&
		len(report.DuplicateRecordIDs) == 0 &&
		len(report.MissingRecordIDs) == 0 &&
		len(report.WrongThreadRecordIDs) == 0
	return object, report, nil
}

func (b *Backend) updateThreadIndex(ctx context.Context, threadID string, recordID string) error {
	for attempt := 0; attempt < b.maxIndexCASRetries; attempt++ {
		object, index, exists, err := b.getThreadIndex(ctx, threadID)
		if err != nil {
			return err
		}
		if containsID(index.IDs, recordID) {
			return nil
		}
		index.IDs = append(index.IDs, recordID)
		raw, err := encode(index)
		if err != nil {
			return err
		}
		precondition := Precondition{IfAbsent: true}
		if exists {
			if object.Version == "" {
				return ErrInvalidIndexObject
			}
			precondition = Precondition{IfVersion: object.Version}
		}
		_, err = b.client.PutObject(ctx, Object{
			Key:  b.threadKey(threadID),
			Data: raw,
		}, precondition)
		if b.isPreconditionFailed(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("checkpoint objectstore: put thread index: %w", err)
		}
		return nil
	}
	return ErrIndexCASConflict
}

func (b *Backend) getThreadIndex(ctx context.Context, threadID string) (Object, threadIndex, bool, error) {
	object, err := b.client.GetObject(ctx, b.threadKey(threadID))
	if b.isNotFound(err) {
		return Object{}, threadIndex{ThreadID: threadID}, false, nil
	}
	if err != nil {
		return Object{}, threadIndex{}, false, fmt.Errorf("checkpoint objectstore: get thread index: %w", err)
	}
	index, err := decodeThreadIndex(object.Data)
	if err != nil {
		return Object{}, threadIndex{}, false, err
	}
	if index.ThreadID == "" {
		index.ThreadID = threadID
	}
	return object, index, true, nil
}

type threadIndex struct {
	ThreadID string   `json:"thread_id,omitempty"`
	IDs      []string `json:"ids,omitempty"`
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", nil
	}
	for _, part := range strings.Split(prefix, "/") {
		if part == "." || part == ".." || strings.Contains(part, `\`) {
			return "", fmt.Errorf("%w: %q", ErrUnsafePrefix, prefix)
		}
	}
	return prefix, nil
}

func (b *Backend) recordKey(id string) string {
	return joinKey(b.prefix, "checkpoint", "records", encodeKeyPart(id)+".json")
}

func (b *Backend) threadKey(threadID string) string {
	return joinKey(b.prefix, "checkpoint", "threads", encodeKeyPart(threadID)+".json")
}

func joinKey(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

func encode(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("checkpoint objectstore: encode object: %w", err)
	}
	return raw, nil
}

func decodeRecord(raw []byte) (checkpoint.Record, error) {
	var record checkpoint.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return checkpoint.Record{}, fmt.Errorf("checkpoint objectstore: decode record: %w", err)
	}
	return record, nil
}

func decodeThreadIndex(raw []byte) (threadIndex, error) {
	var index threadIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return threadIndex{}, fmt.Errorf("checkpoint objectstore: decode thread index: %w", err)
	}
	return index, nil
}

func encodeKeyPart(part string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(part))
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	if containsID(values, value) {
		return values
	}
	return append(values, value)
}

func isThreadIndexKey(key string) bool {
	return strings.Contains(key, "/checkpoint/threads/") || strings.HasPrefix(key, "checkpoint/threads/")
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}
