package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestReplayHandlerInvokesToolFromRecordedEffectArgs(t *testing.T) {
	ctx := context.Background()
	var seenCallID string
	registry := NewRegistry(WithToolMiddleware(func(c *gopact.ToolContext) error {
		seenCallID = c.IDs.CallID
		return c.Next()
	}))
	err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return gopact.ToolResult{}, err
			}
			return gopact.ToolResult{Content: input.Text}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					Applied:        true,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
					Metadata: map[string]any{
						EffectMetadataToolArgs: json.RawMessage(`{"text":"replayed"}`),
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	results, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(registry))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	if seenCallID != "call-1" {
		t.Fatalf("tool call id = %q, want call-1", seenCallID)
	}
	toolResult, ok := results[0].Metadata[EffectReplayMetadataToolResult].(gopact.ToolResult)
	if !ok {
		t.Fatalf("tool result metadata = %#v, want ToolResult", results[0].Metadata[EffectReplayMetadataToolResult])
	}
	if toolResult.Content != "replayed" {
		t.Fatalf("tool result content = %q, want replayed", toolResult.Content)
	}
}

func TestReplayHandlerReturnsRecordedCommitWithoutInvokingTool(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCommitStore()
	err := store.Store(ctx, CommitRecord{
		IdempotencyKey: "idem-1",
		EffectID:       "call-1",
		ToolName:       "local.echo",
		Result:         gopact.ToolResult{Content: "cached"},
		Metadata:       map[string]any{"receipt": "commit-1"},
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	registry := NewRegistry()
	err = registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(context.Context, json.RawMessage) (gopact.ToolResult, error) {
			t.Fatal("tool should not be invoked when a commit record exists")
			return gopact.ToolResult{}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	results, err := gopact.ExecuteEffectReplay(ctx, replayPlanWithToolCall("idem-1"), NewReplayHandler(registry, WithReplayCommitStore(store)))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	toolResult, ok := results[0].Metadata[EffectReplayMetadataToolResult].(gopact.ToolResult)
	if !ok {
		t.Fatalf("tool result metadata = %#v, want ToolResult", results[0].Metadata[EffectReplayMetadataToolResult])
	}
	if toolResult.Content != "cached" {
		t.Fatalf("tool result content = %q, want cached", toolResult.Content)
	}
	if got, ok := results[0].Metadata[EffectReplayMetadataCommitHit].(bool); !ok || !got {
		t.Fatalf("commit hit metadata = %#v, want true", results[0].Metadata[EffectReplayMetadataCommitHit])
	}
}

func TestReplayHandlerStoresSuccessfulReplayCommit(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCommitStore()
	invocations := 0
	registry := NewRegistry()
	err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			invocations++
			return gopact.ToolResult{
				Content: string(args),
				Metadata: map[string]any{
					"attempt": invocations,
				},
			}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	handler := NewReplayHandler(registry, WithReplayCommitStore(store))
	results, err := gopact.ExecuteEffectReplay(ctx, replayPlanWithToolCall("idem-1"), handler)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() first error = %v", err)
	}
	if invocations != 1 {
		t.Fatalf("tool invocations after first replay = %d, want 1", invocations)
	}
	if got, ok := results[0].Metadata[EffectReplayMetadataCommitStored].(bool); !ok || !got {
		t.Fatalf("commit stored metadata = %#v, want true", results[0].Metadata[EffectReplayMetadataCommitStored])
	}
	record, ok, err := store.Load(ctx, "idem-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if record.Result.Content != `{"text":"replayed"}` {
		t.Fatalf("stored result content = %q, want replayed args", record.Result.Content)
	}

	_, err = gopact.ExecuteEffectReplay(ctx, replayPlanWithToolCall("idem-1"), handler)
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() second error = %v", err)
	}
	if invocations != 1 {
		t.Fatalf("tool invocations after second replay = %d, want 1", invocations)
	}
}

func TestMemoryCommitStorePreservesFirstRecordAndDefensiveCopies(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCommitStore()
	record := CommitRecord{
		IdempotencyKey: "idem-1",
		EffectID:       "call-1",
		ToolName:       "local.echo",
		Result: gopact.ToolResult{
			Content:  "first",
			Metadata: map[string]any{"source": "original"},
			Commit: &gopact.ToolCommit{
				IdempotencyKey: "idem-1",
				Metadata:       map[string]any{"receipt": "original"},
			},
		},
		Metadata: map[string]any{"stored_by": "test"},
	}
	if err := store.Store(ctx, record); err != nil {
		t.Fatalf("Store(first) error = %v", err)
	}
	record.Result.Content = "mutated"
	record.Result.Metadata["source"] = "mutated"
	record.Result.Commit.Metadata["receipt"] = "mutated"
	record.Metadata["stored_by"] = "mutated"
	if err := store.Store(ctx, CommitRecord{
		IdempotencyKey: "idem-1",
		EffectID:       "call-2",
		ToolName:       "local.echo",
		Result:         gopact.ToolResult{Content: "second"},
	}); err != nil {
		t.Fatalf("Store(second) error = %v", err)
	}

	got, ok, err := store.Load(ctx, "idem-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if got.Result.Content != "first" {
		t.Fatalf("loaded content = %q, want first", got.Result.Content)
	}
	if got.Result.Metadata["source"] != "original" {
		t.Fatalf("loaded result metadata = %#v, want original", got.Result.Metadata)
	}
	if got.Result.Commit.Metadata["receipt"] != "original" {
		t.Fatalf("loaded commit metadata = %#v, want original", got.Result.Commit.Metadata)
	}
	if got.Metadata["stored_by"] != "test" {
		t.Fatalf("loaded record metadata = %#v, want test", got.Metadata)
	}

	got.Result.Metadata["source"] = "mutated after load"
	again, ok, err := store.Load(ctx, "idem-1")
	if err != nil {
		t.Fatalf("Load(second) error = %v", err)
	}
	if !ok {
		t.Fatal("Load(second) ok = false, want true")
	}
	if again.Result.Metadata["source"] != "original" {
		t.Fatalf("loaded result metadata after caller mutation = %#v, want original", again.Result.Metadata)
	}
}

func TestMemoryCommitStoreRejectsInvalidRecordAndRespectsContext(t *testing.T) {
	store := NewMemoryCommitStore()
	if err := store.Store(context.Background(), CommitRecord{}); !errors.Is(err, ErrCommitRecordRequired) {
		t.Fatalf("Store(empty) error = %v, want ErrCommitRecordRequired", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Store(ctx, CommitRecord{IdempotencyKey: "idem-1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store(canceled) error = %v, want context.Canceled", err)
	}
	_, _, err := store.Load(ctx, "idem-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load(canceled) error = %v, want context.Canceled", err)
	}
}

func TestReplayHandlerRejectsMissingRecordedArgs(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(registry))
	if !errors.Is(err, ErrReplayArgsMissing) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayArgsMissing", err)
	}
}

func TestReplayHandlerRejectsNonToolCallEffect(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "effect-1",
					Type:           "artifact_write",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
					Metadata: map[string]any{
						EffectMetadataToolArgs: json.RawMessage(`{}`),
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}

	_, err := gopact.ExecuteEffectReplay(ctx, plan, NewReplayHandler(registry))
	if !errors.Is(err, ErrReplayEffectType) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want ErrReplayEffectType", err)
	}
}

func TestReplayHandlerCanBeRegisteredInEffectReplayRegistry(t *testing.T) {
	ctx := context.Background()
	toolRegistry := NewRegistry()
	err := toolRegistry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{Content: string(args)}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	replayRegistry := gopact.NewEffectReplayRegistry()
	if err := replayRegistry.Register(EffectTypeToolCall, NewReplayHandler(toolRegistry)); err != nil {
		t.Fatalf("Register(replay handler) error = %v", err)
	}
	plan := gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: "idem-1",
					Metadata: map[string]any{
						EffectMetadataToolArgs: map[string]any{"text": "via registry"},
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
	toolResult := results[0].Metadata[EffectReplayMetadataToolResult].(gopact.ToolResult)
	if toolResult.Content != `{"text":"via registry"}` {
		t.Fatalf("tool result content = %q, want encoded map args", toolResult.Content)
	}
}

func replayPlanWithToolCall(idempotencyKey string) gopact.EffectReplayPlan {
	return gopact.EffectReplayPlan{
		Decisions: []gopact.EffectReplayDecision{
			{
				Effect: gopact.EffectRecord{
					ID:             "call-1",
					Type:           EffectTypeToolCall,
					Target:         "local.echo",
					Applied:        true,
					ReplayPolicy:   gopact.EffectReplayIdempotent,
					IdempotencyKey: idempotencyKey,
					Metadata: map[string]any{
						EffectMetadataToolArgs: json.RawMessage(`{"text":"replayed"}`),
					},
				},
				Action:       gopact.EffectReplayActionReplay,
				ReplayPolicy: gopact.EffectReplayIdempotent,
			},
		},
	}
}
