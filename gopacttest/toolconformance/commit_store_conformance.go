// Package toolconformance provides reusable tool adapter contract tests.
package toolconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/tools"
)

// ErrCommitStoreConformanceFailed reports a failed CommitStore conformance case.
var ErrCommitStoreConformanceFailed = errors.New("gopacttest: commit store conformance failed")

// CommitStoreConformanceHarness describes one CommitStore implementation under test.
type CommitStoreConformanceHarness struct {
	NewStore func() tools.CommitStore
	Record   tools.CommitRecord
}

// CommitStoreConformanceResult is the observed result for one CommitStore contract case.
type CommitStoreConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckCommitStoreConformance runs reusable tools.CommitStore contract cases.
func CheckCommitStoreConformance(ctx context.Context, harness CommitStoreConformanceHarness) []CommitStoreConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []CommitStoreConformanceResult{failedCommitStoreConformance("context", err)}
	}
	record := normalizeCommitStoreConformanceRecord(harness.Record)

	return []CommitStoreConformanceResult{
		checkCommitStoreFactory(harness.NewStore),
		checkCommitStoreLoadMissing(ctx, harness.NewStore),
		checkCommitStoreContext(harness.NewStore, copyCommitStoreConformanceRecord(record)),
		checkCommitStoreInvalidRecord(ctx, harness.NewStore),
		checkCommitStoreStoreLoad(ctx, harness.NewStore, copyCommitStoreConformanceRecord(record)),
		checkCommitStorePreservesFirstRecord(ctx, harness.NewStore, copyCommitStoreConformanceRecord(record)),
		checkCommitStoreStoreDoesNotMutateInput(ctx, harness.NewStore, copyCommitStoreConformanceRecord(record)),
		checkCommitStoreLoadReturnsCopy(ctx, harness.NewStore, copyCommitStoreConformanceRecord(record)),
	}
}

// RequireCommitStoreConformance fails the test unless store satisfies the CommitStore contract.
func RequireCommitStoreConformance(t testing.TB, harness CommitStoreConformanceHarness) {
	t.Helper()

	for _, result := range CheckCommitStoreConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("commit store conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkCommitStoreFactory(newStore func() tools.CommitStore) CommitStoreConformanceResult {
	if newStore == nil {
		return failedCommitStoreConformance("factory", errors.New("new store is required"))
	}
	if store := newStore(); store == nil {
		return failedCommitStoreConformance("factory", errors.New("new store returned nil"))
	}
	return passedCommitStoreConformance("factory")
}

func checkCommitStoreLoadMissing(ctx context.Context, newStore func() tools.CommitStore) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("load-missing", err)
	}
	record, ok, err := store.Load(ctx, "missing-key")
	if err != nil {
		return failedCommitStoreConformance("load-missing", err)
	}
	if ok {
		return failedCommitStoreConformance("load-missing", fmt.Errorf("load(missing-key) ok = true with record %+v", record))
	}
	if !reflect.DeepEqual(record, tools.CommitRecord{}) {
		return failedCommitStoreConformance("load-missing", fmt.Errorf("load(missing-key) record = %+v, want zero", record))
	}
	return passedCommitStoreConformance("load-missing")
}

func checkCommitStoreContext(newStore func() tools.CommitStore, record tools.CommitRecord) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Store(ctx, record); !errors.Is(err, context.Canceled) {
		return failedCommitStoreConformance("context", fmt.Errorf("store(canceled) error = %v, want context.Canceled", err))
	}
	_, _, err = store.Load(ctx, record.IdempotencyKey)
	if !errors.Is(err, context.Canceled) {
		return failedCommitStoreConformance("context", fmt.Errorf("load(canceled) error = %v, want context.Canceled", err))
	}
	return passedCommitStoreConformance("context")
}

func checkCommitStoreInvalidRecord(ctx context.Context, newStore func() tools.CommitStore) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("invalid-record", err)
	}
	if err := store.Store(ctx, tools.CommitRecord{IdempotencyKey: "   "}); err == nil {
		return failedCommitStoreConformance("invalid-record", errors.New("store(empty key) error = nil, want error"))
	}
	return passedCommitStoreConformance("invalid-record")
}

func checkCommitStoreStoreLoad(
	ctx context.Context,
	newStore func() tools.CommitStore,
	record tools.CommitRecord,
) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("store-load", err)
	}
	if err := store.Store(ctx, record); err != nil {
		return failedCommitStoreConformance("store-load", err)
	}
	got, ok, err := store.Load(ctx, record.IdempotencyKey)
	if err != nil {
		return failedCommitStoreConformance("store-load", err)
	}
	if !ok {
		return failedCommitStoreConformance("store-load", errors.New("load(stored key) ok = false, want true"))
	}
	if !reflect.DeepEqual(got, record) {
		return failedCommitStoreConformance("store-load", fmt.Errorf("load(stored key) = %+v, want %+v", got, record))
	}
	return passedCommitStoreConformance("store-load")
}

func checkCommitStorePreservesFirstRecord(
	ctx context.Context,
	newStore func() tools.CommitStore,
	record tools.CommitRecord,
) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("store-preserves-first-record", err)
	}
	first := copyCommitStoreConformanceRecord(record)
	second := copyCommitStoreConformanceRecord(record)
	second.EffectID = "effect-second"
	second.Result.Content = "second"
	second.Metadata["commit"] = "second"
	if err := store.Store(ctx, first); err != nil {
		return failedCommitStoreConformance("store-preserves-first-record", err)
	}
	if err := store.Store(ctx, second); err != nil {
		return failedCommitStoreConformance("store-preserves-first-record", err)
	}
	got, ok, err := store.Load(ctx, record.IdempotencyKey)
	if err != nil {
		return failedCommitStoreConformance("store-preserves-first-record", err)
	}
	if !ok {
		return failedCommitStoreConformance("store-preserves-first-record", errors.New("load(stored key) ok = false, want true"))
	}
	if got.EffectID != first.EffectID || got.Result.Content != first.Result.Content || got.Metadata["commit"] != first.Metadata["commit"] {
		return failedCommitStoreConformance(
			"store-preserves-first-record",
			fmt.Errorf("load(stored key) = %+v, want first record %+v", got, first),
		)
	}
	return passedCommitStoreConformance("store-preserves-first-record")
}

func checkCommitStoreStoreDoesNotMutateInput(
	ctx context.Context,
	newStore func() tools.CommitStore,
	record tools.CommitRecord,
) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("store-does-not-mutate-input", err)
	}
	before := copyCommitStoreConformanceRecord(record)
	if err := store.Store(ctx, record); err != nil {
		return failedCommitStoreConformance("store-does-not-mutate-input", err)
	}
	if !reflect.DeepEqual(record, before) {
		return failedCommitStoreConformance(
			"store-does-not-mutate-input",
			fmt.Errorf("store mutated input record: got %+v, want %+v", record, before),
		)
	}
	return passedCommitStoreConformance("store-does-not-mutate-input")
}

func checkCommitStoreLoadReturnsCopy(
	ctx context.Context,
	newStore func() tools.CommitStore,
	record tools.CommitRecord,
) CommitStoreConformanceResult {
	store, err := newCommitStoreConformanceStore(newStore)
	if err != nil {
		return failedCommitStoreConformance("load-returns-copy", err)
	}
	if err := store.Store(ctx, record); err != nil {
		return failedCommitStoreConformance("load-returns-copy", err)
	}
	loaded, ok, err := store.Load(ctx, record.IdempotencyKey)
	if err != nil {
		return failedCommitStoreConformance("load-returns-copy", err)
	}
	if !ok {
		return failedCommitStoreConformance("load-returns-copy", errors.New("load(stored key) ok = false, want true"))
	}
	if err := requireLoadedCommitStoreConformanceMutableState(loaded); err != nil {
		return failedCommitStoreConformance("load-returns-copy", err)
	}
	loaded.Metadata["commit"] = "mutated"
	loaded.Result.Metadata["result"] = "mutated"
	loaded.Result.Commit.Metadata["commit"] = "mutated"

	again, ok, err := store.Load(ctx, record.IdempotencyKey)
	if err != nil {
		return failedCommitStoreConformance("load-returns-copy", err)
	}
	if !ok {
		return failedCommitStoreConformance("load-returns-copy", errors.New("load(stored key) after mutation ok = false, want true"))
	}
	if err := requireLoadedCommitStoreConformanceMutableState(again); err != nil {
		return failedCommitStoreConformance("load-returns-copy", err)
	}
	if again.Metadata["commit"] != record.Metadata["commit"] ||
		again.Result.Metadata["result"] != record.Result.Metadata["result"] ||
		again.Result.Commit.Metadata["commit"] != record.Result.Commit.Metadata["commit"] {
		return failedCommitStoreConformance(
			"load-returns-copy",
			fmt.Errorf("load returned shared mutable state: got %+v, want %+v", again, record),
		)
	}
	return passedCommitStoreConformance("load-returns-copy")
}

func newCommitStoreConformanceStore(newStore func() tools.CommitStore) (tools.CommitStore, error) {
	if newStore == nil {
		return nil, errors.New("new store is required")
	}
	store := newStore()
	if store == nil {
		return nil, errors.New("new store returned nil")
	}
	return store, nil
}

func normalizeCommitStoreConformanceRecord(record tools.CommitRecord) tools.CommitRecord {
	if strings.TrimSpace(record.IdempotencyKey) == "" {
		return defaultCommitStoreConformanceRecord()
	}
	out := copyCommitStoreConformanceRecord(record)
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	if _, ok := out.Metadata["commit"]; !ok {
		out.Metadata["commit"] = "original"
	}
	if out.Result.Metadata == nil {
		out.Result.Metadata = map[string]any{}
	}
	if _, ok := out.Result.Metadata["result"]; !ok {
		out.Result.Metadata["result"] = "original"
	}
	if out.Result.Commit == nil {
		out.Result.Commit = &gopact.ToolCommit{}
	}
	if strings.TrimSpace(out.Result.Commit.IdempotencyKey) == "" {
		out.Result.Commit.IdempotencyKey = out.IdempotencyKey
	}
	if out.Result.Commit.Metadata == nil {
		out.Result.Commit.Metadata = map[string]any{}
	}
	if _, ok := out.Result.Commit.Metadata["commit"]; !ok {
		out.Result.Commit.Metadata["commit"] = "original"
	}
	return out
}

func requireLoadedCommitStoreConformanceMutableState(record tools.CommitRecord) error {
	if record.Metadata == nil {
		return errors.New("load(stored key) metadata map is nil")
	}
	if record.Result.Metadata == nil {
		return errors.New("load(stored key) result metadata map is nil")
	}
	if record.Result.Commit == nil {
		return errors.New("load(stored key) result commit is nil")
	}
	if record.Result.Commit.Metadata == nil {
		return errors.New("load(stored key) result commit metadata map is nil")
	}
	return nil
}

func defaultCommitStoreConformanceRecord() tools.CommitRecord {
	createdAt := time.Unix(100, 0).UTC()
	return tools.CommitRecord{
		IdempotencyKey: "idem-1",
		EffectID:       "effect-1",
		ToolName:       "local.echo",
		Result: gopact.ToolResult{
			Content: "first",
			Metadata: map[string]any{
				"result": "original",
			},
			Commit: &gopact.ToolCommit{
				IdempotencyKey: "idem-1",
				Metadata: map[string]any{
					"commit": "original",
				},
			},
		},
		Metadata: map[string]any{
			"commit": "original",
		},
		CreatedAt: createdAt,
	}
}

func copyCommitStoreConformanceRecord(record tools.CommitRecord) tools.CommitRecord {
	out := record
	out.Result = copyCommitStoreConformanceToolResult(record.Result)
	out.Metadata = copyCommitStoreConformanceMap(record.Metadata)
	return out
}

func copyCommitStoreConformanceToolResult(result gopact.ToolResult) gopact.ToolResult {
	out := result
	out.Metadata = copyCommitStoreConformanceMap(result.Metadata)
	if result.Commit != nil {
		commit := *result.Commit
		commit.Metadata = copyCommitStoreConformanceMap(result.Commit.Metadata)
		out.Commit = &commit
	}
	return out
}

func copyCommitStoreConformanceMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func passedCommitStoreConformance(name string) CommitStoreConformanceResult {
	return CommitStoreConformanceResult{Case: name, Passed: true}
}

func failedCommitStoreConformance(name string, err error) CommitStoreConformanceResult {
	return CommitStoreConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrCommitStoreConformanceFailed, err),
	}
}
