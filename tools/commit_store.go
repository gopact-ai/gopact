package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrCommitRecordRequired is returned when a commit record cannot be stored safely.
	ErrCommitRecordRequired = errors.New("tools: commit record is required")
)

// CommitRecord is the host-owned durable result for an idempotent tool replay key.
type CommitRecord struct {
	IdempotencyKey string
	EffectID       string
	ToolName       string
	Result         gopact.ToolResult
	Metadata       map[string]any
	CreatedAt      time.Time
}

// CommitStore stores already committed idempotent tool results.
type CommitStore interface {
	Load(ctx context.Context, idempotencyKey string) (CommitRecord, bool, error)
	Store(ctx context.Context, record CommitRecord) error
}

// MemoryCommitStore is an in-memory CommitStore for tests and single-process hosts.
type MemoryCommitStore struct {
	mu      sync.RWMutex
	records map[string]CommitRecord
}

var _ CommitStore = (*MemoryCommitStore)(nil)

// NewMemoryCommitStore creates an empty in-memory commit store.
func NewMemoryCommitStore() *MemoryCommitStore {
	return &MemoryCommitStore{
		records: make(map[string]CommitRecord),
	}
}

// Load returns the first committed record for an idempotency key.
func (s *MemoryCommitStore) Load(ctx context.Context, idempotencyKey string) (CommitRecord, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return CommitRecord{}, false, err
	}
	if s == nil {
		return CommitRecord{}, false, nil
	}
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return CommitRecord{}, false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.records[key]
	if !ok {
		return CommitRecord{}, false, nil
	}
	return copyCommitRecord(record), true, nil
}

// Store preserves the first committed record for an idempotency key.
func (s *MemoryCommitStore) Store(ctx context.Context, record CommitRecord) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := strings.TrimSpace(record.IdempotencyKey)
	if key == "" {
		return ErrCommitRecordRequired
	}
	if s == nil {
		return errors.New("tools: commit store is nil")
	}

	stored := copyCommitRecord(record)
	stored.IdempotencyKey = key
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.records == nil {
		s.records = make(map[string]CommitRecord)
	}
	if _, exists := s.records[key]; exists {
		return nil
	}
	s.records[key] = stored
	return nil
}

func copyCommitRecord(record CommitRecord) CommitRecord {
	out := record
	out.Result = copyToolResult(record.Result)
	out.Metadata = copyMetadata(record.Metadata)
	return out
}

func copyToolResult(result gopact.ToolResult) gopact.ToolResult {
	out := result
	out.Artifacts = copyArtifactRefs(result.Artifacts)
	out.Effects = copyEffectRecords(result.Effects)
	out.Events = copyEvents(result.Events)
	if result.Commit != nil {
		commit := *result.Commit
		commit.Metadata = copyMetadata(result.Commit.Metadata)
		out.Commit = &commit
	}
	out.Metadata = copyMetadata(result.Metadata)
	return out
}

func copyEffectRecords(in []gopact.EffectRecord) []gopact.EffectRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.EffectRecord, len(in))
	for i, effect := range in {
		out[i] = effect
		out[i].DependsOn = append([]string(nil), effect.DependsOn...)
		out[i].Artifacts = copyArtifactRefs(effect.Artifacts)
		if effect.Sandbox != nil {
			sandbox := *effect.Sandbox
			sandbox.Command = append([]string(nil), effect.Sandbox.Command...)
			sandbox.Metadata = copyMetadata(effect.Sandbox.Metadata)
			out[i].Sandbox = &sandbox
		}
		out[i].Metadata = copyMetadata(effect.Metadata)
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
		out[i].Metadata = copyMetadata(ref.Metadata)
	}
	return out
}

func copyEvents(in []gopact.Event) []gopact.Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.Event, len(in))
	for i, event := range in {
		out[i] = event
		out[i].Artifacts = copyArtifactRefs(event.Artifacts)
		if event.Result != nil {
			result := copyToolResult(*event.Result)
			out[i].Result = &result
		}
		out[i].Metadata = copyMetadata(event.Metadata)
	}
	return out
}
