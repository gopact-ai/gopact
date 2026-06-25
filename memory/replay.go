package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

const (
	// EffectTypeMemoryExtract is the standard effect type for deferred memory extraction requests.
	EffectTypeMemoryExtract = "memory_extract"

	// EffectTypeMemoryPut is the standard effect type for replayable memory writes.
	EffectTypeMemoryPut = "memory_put"

	// EffectTypeMemoryDelete is the standard effect type for replayable memory deletes.
	EffectTypeMemoryDelete = "memory_delete"

	// EffectTypeMemorySearch is the standard effect type for replayable memory searches.
	EffectTypeMemorySearch = "memory_search"

	// EffectMetadataMemory stores the memory record needed to replay a memory write.
	EffectMetadataMemory = "memory"

	// EffectMetadataMemoryID stores the memory ID needed to replay a memory delete.
	EffectMetadataMemoryID = "memory_id"

	// EffectMetadataMemoryQuery stores the query needed to replay a memory search.
	EffectMetadataMemoryQuery = "memory_query"

	// EffectMetadataMemoryExtractState stores the state needed by a host extractor.
	EffectMetadataMemoryExtractState = "memory_extract_state"

	// EffectMetadataMemoryExtractIDs stores the runtime IDs for a memory extraction request.
	EffectMetadataMemoryExtractIDs = "memory_extract_ids"

	// EffectReplayMetadataMemoryID stores the memory ID touched by a replayed memory write or delete.
	EffectReplayMetadataMemoryID = "memory_id"

	// EffectReplayMetadataMemoryIDs stores memory IDs produced by replayed extraction.
	EffectReplayMetadataMemoryIDs = "memory_ids"

	// EffectReplayMetadataMemoryCount stores the number of memories produced by replayed extraction.
	EffectReplayMetadataMemoryCount = "memory_count"

	// EffectReplayMetadataMemorySearchResult stores the SearchResult produced by a replayed memory search.
	EffectReplayMetadataMemorySearchResult = "memory_search_result"
)

var (
	ErrReplayMemoryMissing             = errors.New("memory: replay memory missing")
	ErrReplayMemoryIDMissing           = errors.New("memory: replay memory id missing")
	ErrReplayMemoryQueryMissing        = errors.New("memory: replay memory query missing")
	ErrReplayMemoryExtractStateMissing = errors.New("memory: replay memory extract state missing")
	ErrReplayMemoryExtractIDsMissing   = errors.New("memory: replay memory extract ids missing")
	ErrReplayEffectType                = errors.New("memory: replay effect type is not supported")
)

// ExtractionRequest is the input for replaying a deferred memory extraction effect.
type ExtractionRequest struct {
	State  any
	IDs    gopact.RuntimeIDs
	Effect gopact.EffectRecord
}

// Extractor extracts memories from a recorded state snapshot.
type Extractor interface {
	Extract(ctx context.Context, request ExtractionRequest) ([]Memory, error)
}

// ExtractorFunc adapts a function into an Extractor.
type ExtractorFunc func(ctx context.Context, request ExtractionRequest) ([]Memory, error)

// Extract implements Extractor.
func (f ExtractorFunc) Extract(ctx context.Context, request ExtractionRequest) ([]Memory, error) {
	return f(ctx, request)
}

// ReplayHandler replays idempotent memory effects through a Store.
type ReplayHandler struct {
	store Store
}

// NewReplayHandler creates an EffectReplayExecutor for memory effects.
func NewReplayHandler(store Store) *ReplayHandler {
	return &ReplayHandler{store: store}
}

// ExtractionReplayHandler replays deferred memory extraction effects through an Extractor and Store.
type ExtractionReplayHandler struct {
	extractor Extractor
	store     Store
}

// NewExtractionReplayHandler creates an EffectReplayExecutor for memory extraction effects.
func NewExtractionReplayHandler(extractor Extractor, store Store) *ExtractionReplayHandler {
	return &ExtractionReplayHandler{extractor: extractor, store: store}
}

// ReplayEffect implements gopact.EffectReplayExecutor.
func (h *ReplayHandler) ReplayEffect(ctx context.Context, decision gopact.EffectReplayDecision) (gopact.EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if h == nil || h.store == nil {
		return gopact.EffectReplayResult{}, errors.New("memory: replay store is nil")
	}
	if decision.Action != gopact.EffectReplayActionReplay || decision.ReplayPolicy != gopact.EffectReplayIdempotent {
		return gopact.EffectReplayResult{}, fmt.Errorf("memory: effect %q is not an idempotent replay decision", decision.Effect.ID)
	}

	switch decision.Effect.Type {
	case EffectTypeMemoryPut:
		return h.replayPut(ctx, decision.Effect)
	case EffectTypeMemoryDelete:
		return h.replayDelete(ctx, decision.Effect)
	case EffectTypeMemorySearch:
		return h.replaySearch(ctx, decision.Effect)
	default:
		return gopact.EffectReplayResult{}, fmt.Errorf("%w: %q", ErrReplayEffectType, decision.Effect.Type)
	}
}

// ReplayEffect implements gopact.EffectReplayExecutor.
func (h *ExtractionReplayHandler) ReplayEffect(ctx context.Context, decision gopact.EffectReplayDecision) (gopact.EffectReplayResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if h == nil || h.extractor == nil {
		return gopact.EffectReplayResult{}, errors.New("memory: replay extractor is nil")
	}
	if h.store == nil {
		return gopact.EffectReplayResult{}, errors.New("memory: replay store is nil")
	}
	if decision.Action != gopact.EffectReplayActionReplay || decision.ReplayPolicy != gopact.EffectReplayIdempotent {
		return gopact.EffectReplayResult{}, fmt.Errorf("memory: effect %q is not an idempotent replay decision", decision.Effect.ID)
	}
	if decision.Effect.Type != EffectTypeMemoryExtract {
		return gopact.EffectReplayResult{}, fmt.Errorf("%w: %q", ErrReplayEffectType, decision.Effect.Type)
	}

	state, err := replayMemoryExtractState(decision.Effect)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	ids, err := replayMemoryExtractIDs(decision.Effect)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	memories, err := h.extractor.Extract(ctx, ExtractionRequest{
		State:  state,
		IDs:    ids,
		Effect: copyReplayEffectRecord(decision.Effect),
	})
	if err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("memory: replay extract: %w", err)
	}

	memoryIDs := make([]ID, 0, len(memories))
	for i, item := range memories {
		item = memoryWithRuntimeScope(item, ids)
		if item.ID == "" {
			item.ID = extractedMemoryID(decision.Effect.ID, i)
		}
		id, err := h.store.Put(ctx, item)
		if err != nil {
			return gopact.EffectReplayResult{}, fmt.Errorf("memory: replay extract put: %w", err)
		}
		memoryIDs = append(memoryIDs, id)
	}
	return gopact.EffectReplayResult{
		Metadata: map[string]any{
			EffectReplayMetadataMemoryIDs:   memoryIDs,
			EffectReplayMetadataMemoryCount: len(memoryIDs),
		},
	}, nil
}

func (h *ReplayHandler) replayPut(ctx context.Context, effect gopact.EffectRecord) (gopact.EffectReplayResult, error) {
	memory, err := replayMemory(effect)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	id, err := h.store.Put(ctx, memory)
	if err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("memory: replay put: %w", err)
	}
	return gopact.EffectReplayResult{
		Metadata: map[string]any{
			EffectReplayMetadataMemoryID: id,
		},
	}, nil
}

func (h *ReplayHandler) replayDelete(ctx context.Context, effect gopact.EffectRecord) (gopact.EffectReplayResult, error) {
	id, err := replayMemoryID(effect)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	if err := h.store.Delete(ctx, id); err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("memory: replay delete: %w", err)
	}
	return gopact.EffectReplayResult{
		Metadata: map[string]any{
			EffectReplayMetadataMemoryID: id,
		},
	}, nil
}

func (h *ReplayHandler) replaySearch(ctx context.Context, effect gopact.EffectRecord) (gopact.EffectReplayResult, error) {
	query, err := replayMemoryQuery(effect)
	if err != nil {
		return gopact.EffectReplayResult{}, err
	}
	result, err := h.store.Search(ctx, query)
	if err != nil {
		return gopact.EffectReplayResult{}, fmt.Errorf("memory: replay search: %w", err)
	}
	return gopact.EffectReplayResult{
		Metadata: map[string]any{
			EffectReplayMetadataMemorySearchResult: result,
		},
	}, nil
}

func replayMemory(effect gopact.EffectRecord) (Memory, error) {
	if len(effect.Metadata) == 0 {
		return Memory{}, ErrReplayMemoryMissing
	}
	value, ok := effect.Metadata[EffectMetadataMemory]
	if !ok {
		return Memory{}, ErrReplayMemoryMissing
	}

	switch memory := value.(type) {
	case Memory:
		return copyMemory(memory), nil
	case *Memory:
		if memory == nil {
			return Memory{}, ErrReplayMemoryMissing
		}
		return copyMemory(*memory), nil
	case json.RawMessage:
		return decodeReplayMemory(memory)
	case []byte:
		return decodeReplayMemory(memory)
	case string:
		return decodeReplayMemory([]byte(memory))
	default:
		encoded, err := json.Marshal(memory)
		if err != nil {
			return Memory{}, fmt.Errorf("memory: encode replay memory: %w", err)
		}
		return decodeReplayMemory(encoded)
	}
}

func decodeReplayMemory(raw []byte) (Memory, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Memory{}, ErrReplayMemoryMissing
	}

	var memory Memory
	if err := json.Unmarshal(raw, &memory); err != nil {
		return Memory{}, fmt.Errorf("memory: decode replay memory: %w", err)
	}
	return memory, nil
}

func replayMemoryID(effect gopact.EffectRecord) (ID, error) {
	if len(effect.Metadata) == 0 {
		return "", ErrReplayMemoryIDMissing
	}
	value, ok := effect.Metadata[EffectMetadataMemoryID]
	if !ok {
		return "", ErrReplayMemoryIDMissing
	}

	switch id := value.(type) {
	case ID:
		return validateReplayMemoryID(id)
	case *ID:
		if id == nil {
			return "", ErrReplayMemoryIDMissing
		}
		return validateReplayMemoryID(*id)
	case string:
		return validateReplayMemoryID(ID(id))
	case json.RawMessage:
		return decodeReplayMemoryID(id)
	case []byte:
		return decodeReplayMemoryID(id)
	default:
		encoded, err := json.Marshal(id)
		if err != nil {
			return "", fmt.Errorf("memory: encode replay memory id: %w", err)
		}
		return decodeReplayMemoryID(encoded)
	}
}

func decodeReplayMemoryID(raw []byte) (ID, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", ErrReplayMemoryIDMissing
	}
	if raw[0] != '"' && raw[0] != 'n' && raw[0] != '{' && raw[0] != '[' {
		return validateReplayMemoryID(ID(string(raw)))
	}

	var id ID
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", fmt.Errorf("memory: decode replay memory id: %w", err)
	}
	return validateReplayMemoryID(id)
}

func validateReplayMemoryID(id ID) (ID, error) {
	if id == "" {
		return "", ErrReplayMemoryIDMissing
	}
	return id, nil
}

func replayMemoryQuery(effect gopact.EffectRecord) (Query, error) {
	if len(effect.Metadata) == 0 {
		return Query{}, ErrReplayMemoryQueryMissing
	}
	value, ok := effect.Metadata[EffectMetadataMemoryQuery]
	if !ok {
		return Query{}, ErrReplayMemoryQueryMissing
	}

	switch query := value.(type) {
	case Query:
		return query, nil
	case *Query:
		if query == nil {
			return Query{}, ErrReplayMemoryQueryMissing
		}
		return *query, nil
	case json.RawMessage:
		return decodeReplayMemoryQuery(query)
	case []byte:
		return decodeReplayMemoryQuery(query)
	case string:
		return decodeReplayMemoryQuery([]byte(query))
	default:
		encoded, err := json.Marshal(query)
		if err != nil {
			return Query{}, fmt.Errorf("memory: encode replay memory query: %w", err)
		}
		return decodeReplayMemoryQuery(encoded)
	}
}

func decodeReplayMemoryQuery(raw []byte) (Query, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Query{}, ErrReplayMemoryQueryMissing
	}

	var query Query
	if err := json.Unmarshal(raw, &query); err != nil {
		return Query{}, fmt.Errorf("memory: decode replay memory query: %w", err)
	}
	return query, nil
}

func replayMemoryExtractState(effect gopact.EffectRecord) (any, error) {
	if len(effect.Metadata) == 0 {
		return nil, ErrReplayMemoryExtractStateMissing
	}
	value, ok := effect.Metadata[EffectMetadataMemoryExtractState]
	if !ok || value == nil {
		return nil, ErrReplayMemoryExtractStateMissing
	}
	return value, nil
}

func replayMemoryExtractIDs(effect gopact.EffectRecord) (gopact.RuntimeIDs, error) {
	if len(effect.Metadata) == 0 {
		return gopact.RuntimeIDs{}, ErrReplayMemoryExtractIDsMissing
	}
	value, ok := effect.Metadata[EffectMetadataMemoryExtractIDs]
	if !ok || value == nil {
		return gopact.RuntimeIDs{}, ErrReplayMemoryExtractIDsMissing
	}

	switch ids := value.(type) {
	case gopact.RuntimeIDs:
		return ids, nil
	case *gopact.RuntimeIDs:
		if ids == nil {
			return gopact.RuntimeIDs{}, ErrReplayMemoryExtractIDsMissing
		}
		return *ids, nil
	case json.RawMessage:
		return decodeReplayRuntimeIDs(ids)
	case []byte:
		return decodeReplayRuntimeIDs(ids)
	case string:
		return decodeReplayRuntimeIDs([]byte(ids))
	default:
		encoded, err := json.Marshal(ids)
		if err != nil {
			return gopact.RuntimeIDs{}, fmt.Errorf("memory: encode replay memory extract ids: %w", err)
		}
		return decodeReplayRuntimeIDs(encoded)
	}
}

func decodeReplayRuntimeIDs(raw []byte) (gopact.RuntimeIDs, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return gopact.RuntimeIDs{}, ErrReplayMemoryExtractIDsMissing
	}

	var ids gopact.RuntimeIDs
	if err := json.Unmarshal(raw, &ids); err != nil {
		return gopact.RuntimeIDs{}, fmt.Errorf("memory: decode replay memory extract ids: %w", err)
	}
	return ids, nil
}

func memoryWithRuntimeScope(item Memory, ids gopact.RuntimeIDs) Memory {
	out := copyMemory(item)
	if out.Scope.UserID == "" {
		out.Scope.UserID = ids.UserID
	}
	if out.Scope.SessionID == "" {
		out.Scope.SessionID = ids.SessionID
	}
	if out.Scope.ThreadID == "" {
		out.Scope.ThreadID = ids.ThreadID
	}
	if out.Scope.AgentID == "" {
		out.Scope.AgentID = ids.AgentID
	}
	if out.Scope.AppID == "" {
		out.Scope.AppID = ids.AppID
	}
	return out
}

func extractedMemoryID(effectID string, index int) ID {
	return ID(fmt.Sprintf("%s:memory:%d", effectID, index+1))
}

func copyReplayEffectRecord(in gopact.EffectRecord) gopact.EffectRecord {
	in.DependsOn = append([]string(nil), in.DependsOn...)
	in.Artifacts = append([]gopact.ArtifactRef(nil), in.Artifacts...)
	if len(in.Metadata) > 0 {
		metadata := make(map[string]any, len(in.Metadata))
		for key, value := range in.Metadata {
			metadata[key] = value
		}
		in.Metadata = metadata
	}
	return in
}
