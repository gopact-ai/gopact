package memory

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestReplayHandlerPutsRecordedMemory(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-1",
					Type:           EffectTypeMemoryPut,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:preference",
					Metadata: map[string]any{
						EffectMetadataMemory: Memory{
							Scope:   Scope{UserID: "user-1"},
							Type:    TypeSemantic,
							Content: "prefers short status updates",
						},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	memoryID, ok := results[0].Metadata[EffectReplayMetadataMemoryID].(ID)
	if !ok {
		t.Fatalf("memory id metadata = %#v, want memory.ID", results[0].Metadata[EffectReplayMetadataMemoryID])
	}

	got, err := store.Get(ctx, memoryID)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", memoryID, err)
	}
	if got.Content != "prefers short status updates" || got.Scope.UserID != "user-1" {
		t.Fatalf("memory = %+v, want replayed scoped memory", got)
	}
}

func TestReplayHandlerCanBeRegisteredInEffectReplayRegistry(t *testing.T) {
	ctx := context.Background()
	store := New()
	replayRegistry := gopact.NewEffectReplayRegistry()
	if err := replayRegistry.Register(EffectTypeMemoryPut, NewReplayHandler(store)); err != nil {
		t.Fatalf("Register(replay handler) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-1",
					Type:           EffectTypeMemoryPut,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:profile",
					Metadata: map[string]any{
						EffectMetadataMemory: map[string]any{
							"Scope":   map[string]any{"UserID": "user-1"},
							"Type":    string(TypeProfile),
							"Content": "uses Go for agent SDK work",
						},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, replayRegistry)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	memoryID := results[0].Metadata[EffectReplayMetadataMemoryID].(ID)
	got, err := store.Get(ctx, memoryID)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", memoryID, err)
	}
	if got.Type != TypeProfile || got.Content != "uses Go for agent SDK work" {
		t.Fatalf("memory = %+v, want profile memory from map metadata", got)
	}
}

func TestReplayHandlerDeletesRecordedMemory(t *testing.T) {
	ctx := context.Background()
	store := New()
	memoryID, err := store.Put(ctx, Memory{
		Scope:   Scope{UserID: "user-1"},
		Type:    TypeProfile,
		Content: "temporary profile memory",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-delete-1",
					Type:           EffectTypeMemoryDelete,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:delete",
					Metadata: map[string]any{
						EffectMetadataMemoryID: memoryID,
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	gotID, ok := results[0].Metadata[EffectReplayMetadataMemoryID].(ID)
	if !ok {
		t.Fatalf("memory id metadata = %#v, want memory.ID", results[0].Metadata[EffectReplayMetadataMemoryID])
	}
	if gotID != memoryID {
		t.Fatalf("memory id metadata = %q, want %q", gotID, memoryID)
	}
	_, err = store.Get(ctx, memoryID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after replay delete error = %v, want ErrNotFound", err)
	}
}

func TestReplayHandlerSearchesRecordedQuery(t *testing.T) {
	ctx := context.Background()
	store := New()
	aliceID, err := store.Put(ctx, Memory{
		Scope:   Scope{UserID: "alice"},
		Type:    TypeProfile,
		Content: "likes go sdk design",
	})
	if err != nil {
		t.Fatalf("Put(alice) error = %v", err)
	}
	_, err = store.Put(ctx, Memory{
		Scope:   Scope{UserID: "bob"},
		Type:    TypeProfile,
		Content: "likes go sdk design",
	})
	if err != nil {
		t.Fatalf("Put(bob) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-search-1",
					Type:           EffectTypeMemorySearch,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:alice:search:go",
					Metadata: map[string]any{
						EffectMetadataMemoryQuery: Query{
							Scope: Scope{UserID: "alice"},
							Types: []Type{TypeProfile},
							Text:  "go",
						},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	searchResult, ok := results[0].Metadata[EffectReplayMetadataMemorySearchResult].(SearchResult)
	if !ok {
		t.Fatalf("search result metadata = %#v, want memory.SearchResult", results[0].Metadata[EffectReplayMetadataMemorySearchResult])
	}
	if len(searchResult.Memories) != 1 || searchResult.Memories[0].ID != aliceID {
		t.Fatalf("search result = %+v, want only alice memory %q", searchResult, aliceID)
	}
}

func TestReplayHandlerRejectsMissingMemory(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-1",
					Type:           EffectTypeMemoryPut,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:missing",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrReplayMemoryMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayMemoryMissing", err)
	}
}

func TestReplayHandlerRejectsDeleteWithoutMemoryID(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-delete-1",
					Type:           EffectTypeMemoryDelete,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:delete",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrReplayMemoryIDMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayMemoryIDMissing", err)
	}
}

func TestReplayHandlerRejectsSearchWithoutQuery(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-search-1",
					Type:           EffectTypeMemorySearch,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:search",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrReplayMemoryQueryMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayMemoryQueryMissing", err)
	}
}

func TestReplayHandlerRejectsUnsupportedEffect(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "effect-1",
					Type:           "tool_call",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory:user-1:wrong-type",
					Metadata: map[string]any{
						EffectMetadataMemory: Memory{Type: TypeSemantic, Content: "remember this"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(store))
	if !errors.Is(err, ErrReplayEffectType) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayEffectType", err)
	}
}

func TestExtractionReplayHandlerExtractsAndStoresRecordedMemories(t *testing.T) {
	ctx := context.Background()
	store := New()
	state := map[string]any{
		"messages": []any{"remember resumable steps"},
	}
	ids := gopact.RuntimeIDs{
		UserID:    "user-1",
		SessionID: "session-1",
		ThreadID:  "thread-1",
		RunID:     "run-1",
		AgentID:   "agent-1",
		AppID:     "app-1",
	}
	var gotRequest ExtractionRequest
	handler := NewExtractionReplayHandler(ExtractorFunc(func(_ context.Context, request ExtractionRequest) ([]Memory, error) {
		gotRequest = request
		return []Memory{
			{
				Type:    TypeSemantic,
				Content: "prefers resumable workflow steps",
				Metadata: map[string]any{
					"source": "extractor",
				},
			},
		}, nil
	}), store)
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-extract-1",
					Type:           EffectTypeMemoryExtract,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory_extract:memory-extract-1",
					Metadata: map[string]any{
						EffectMetadataMemoryExtractState: state,
						EffectMetadataMemoryExtractIDs:   ids,
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, handler)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if !reflect.DeepEqual(gotRequest.State, state) {
		t.Fatalf("extract request state = %#v, want %#v", gotRequest.State, state)
	}
	if gotRequest.IDs != ids {
		t.Fatalf("extract request IDs = %+v, want %+v", gotRequest.IDs, ids)
	}
	if gotRequest.Effect.ID != "memory-extract-1" {
		t.Fatalf("extract request effect ID = %q, want memory-extract-1", gotRequest.Effect.ID)
	}
	memoryIDs, ok := results[0].Metadata[EffectReplayMetadataMemoryIDs].([]ID)
	if !ok {
		t.Fatalf("memory IDs metadata = %#v, want []memory.ID", results[0].Metadata[EffectReplayMetadataMemoryIDs])
	}
	if len(memoryIDs) != 1 || memoryIDs[0] != ID("memory-extract-1:memory:1") {
		t.Fatalf("memory IDs = %#v, want deterministic extracted memory ID", memoryIDs)
	}
	if count, ok := results[0].Metadata[EffectReplayMetadataMemoryCount].(int); !ok || count != 1 {
		t.Fatalf("memory count metadata = %#v, want 1", results[0].Metadata[EffectReplayMetadataMemoryCount])
	}
	got, err := store.Get(ctx, memoryIDs[0])
	if err != nil {
		t.Fatalf("Get(%q) error = %v", memoryIDs[0], err)
	}
	if got.Content != "prefers resumable workflow steps" || got.Scope.UserID != "user-1" || got.Scope.SessionID != "session-1" || got.Scope.ThreadID != "thread-1" || got.Scope.AgentID != "agent-1" || got.Scope.AppID != "app-1" {
		t.Fatalf("stored memory = %+v, want runtime-scoped extracted memory", got)
	}
}

func TestExtractionReplayHandlerCanBeRegisteredInEffectReplayRegistry(t *testing.T) {
	ctx := context.Background()
	store := New()
	replayRegistry := gopact.NewEffectReplayRegistry()
	handler := NewExtractionReplayHandler(ExtractorFunc(func(_ context.Context, request ExtractionRequest) ([]Memory, error) {
		if request.IDs.UserID != "user-2" || request.IDs.RunID != "run-2" {
			t.Fatalf("extract request IDs = %+v, want decoded runtime IDs", request.IDs)
		}
		return []Memory{
			{
				ID:      "profile-1",
				Type:    TypeProfile,
				Content: "uses replay registry for memory extraction",
			},
		}, nil
	}), store)
	if err := replayRegistry.Register(EffectTypeMemoryExtract, handler); err != nil {
		t.Fatalf("Register(extraction replay handler) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-extract-2",
					Type:           EffectTypeMemoryExtract,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory_extract:memory-extract-2",
					Metadata: map[string]any{
						EffectMetadataMemoryExtractState: map[string]any{"final": "answer"},
						EffectMetadataMemoryExtractIDs: map[string]any{
							"user_id": "user-2",
							"run_id":  "run-2",
						},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, replayRegistry)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	memoryIDs := results[0].Metadata[EffectReplayMetadataMemoryIDs].([]ID)
	if len(memoryIDs) != 1 || memoryIDs[0] != ID("profile-1") {
		t.Fatalf("memory IDs = %#v, want explicit extracted memory ID", memoryIDs)
	}
	got, err := store.Get(ctx, "profile-1")
	if err != nil {
		t.Fatalf("Get(profile-1) error = %v", err)
	}
	if got.Type != TypeProfile || got.Content != "uses replay registry for memory extraction" || got.Scope.UserID != "user-2" {
		t.Fatalf("stored memory = %+v, want extracted profile memory", got)
	}
}

func TestExtractionReplayHandlerRejectsMissingExtractState(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-extract-1",
					Type:           EffectTypeMemoryExtract,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory_extract:memory-extract-1",
					Metadata: map[string]any{
						EffectMetadataMemoryExtractIDs: gopact.RuntimeIDs{UserID: "user-1"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewExtractionReplayHandler(ExtractorFunc(func(_ context.Context, _ ExtractionRequest) ([]Memory, error) {
		t.Fatal("extractor should not be called when state is missing")
		return nil, nil
	}), store))
	if !errors.Is(err, ErrReplayMemoryExtractStateMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayMemoryExtractStateMissing", err)
	}
}

func TestExtractionReplayHandlerRejectsMissingExtractIDs(t *testing.T) {
	ctx := context.Background()
	store := New()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "memory-extract-1",
					Type:           EffectTypeMemoryExtract,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "memory_extract:memory-extract-1",
					Metadata: map[string]any{
						EffectMetadataMemoryExtractState: map[string]any{"final": "answer"},
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewExtractionReplayHandler(ExtractorFunc(func(_ context.Context, _ ExtractionRequest) ([]Memory, error) {
		t.Fatal("extractor should not be called when IDs are missing")
		return nil, nil
	}), store))
	if !errors.Is(err, ErrReplayMemoryExtractIDsMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayMemoryExtractIDsMissing", err)
	}
}
