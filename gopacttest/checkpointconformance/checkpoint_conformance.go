// Package checkpointconformance provides reusable checkpoint store contract tests.
package checkpointconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

// ErrCheckpointStoreConformanceFailed is returned when a checkpoint store violates the conformance harness.
var ErrCheckpointStoreConformanceFailed = errors.New("gopacttest: checkpoint store conformance failed")

// CheckpointStoreConformanceStore is the checkpoint store surface tested by the reusable conformance cases.
type CheckpointStoreConformanceStore[S any] interface {
	Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error
	Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool, error)
	Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error)
	List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error)
}

// CheckpointStoreConformanceHarness describes one checkpoint store implementation under test.
type CheckpointStoreConformanceHarness[S any] struct {
	NewStore    func() CheckpointStoreConformanceStore[S]
	Checkpoints []graph.Checkpoint[S]
}

// CheckpointStoreConformanceResult is the observed result for one checkpoint store contract case.
type CheckpointStoreConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckCheckpointStoreConformance runs reusable checkpoint store contract cases.
func CheckCheckpointStoreConformance[S any](ctx context.Context, harness CheckpointStoreConformanceHarness[S]) []CheckpointStoreConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	checkpoints := normalizeCheckpointConformanceCheckpoints(harness.Checkpoints)

	return []CheckpointStoreConformanceResult{
		checkCheckpointStoreFactory(harness.NewStore),
		checkCheckpointStorePutCanceledContext(harness.NewStore, copyCheckpointForConformance(checkpoints[0])),
		checkCheckpointStoreLatestCanceledContext(harness.NewStore),
		checkCheckpointStoreGetCanceledContext(harness.NewStore),
		checkCheckpointStoreListCanceledContext(harness.NewStore),
		checkCheckpointStoreLoadsLatest(ctx, harness.NewStore, copyCheckpointsForConformance(checkpoints)),
		checkCheckpointStoreGetsByID(ctx, harness.NewStore, copyCheckpointsForConformance(checkpoints)),
		checkCheckpointStoreListsThread(ctx, harness.NewStore, copyCheckpointsForConformance(checkpoints)),
		checkCheckpointStoreDoesNotMutateCheckpoint(ctx, harness.NewStore, copyCheckpointForConformance(checkpoints[0])),
	}
}

// RequireCheckpointStoreConformance fails the test unless store satisfies the checkpoint store contract.
func RequireCheckpointStoreConformance[S any](t testing.TB, harness CheckpointStoreConformanceHarness[S]) {
	t.Helper()

	for _, result := range CheckCheckpointStoreConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("checkpoint store conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkCheckpointStoreFactory[S any](newStore func() CheckpointStoreConformanceStore[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("has-store-factory", err)
	}
	if store == nil {
		return failedCheckpointStoreConformance("has-store-factory", errors.New("checkpoint store is nil"))
	}
	return passedCheckpointStoreConformance("has-store-factory")
}

func checkCheckpointStorePutCanceledContext[S any](newStore func() CheckpointStoreConformanceStore[S], checkpoint graph.Checkpoint[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("put-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, checkpoint); !errors.Is(err, context.Canceled) {
		return failedCheckpointStoreConformance("put-respects-canceled-context", fmt.Errorf("Put canceled context error = %v, want context.Canceled", err))
	}
	return passedCheckpointStoreConformance("put-respects-canceled-context")
}

func checkCheckpointStoreLatestCanceledContext[S any](newStore func() CheckpointStoreConformanceStore[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("latest-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.Latest(ctx, "thread"); !errors.Is(err, context.Canceled) {
		return failedCheckpointStoreConformance("latest-respects-canceled-context", fmt.Errorf("Latest canceled context error = %v, want context.Canceled", err))
	}
	return passedCheckpointStoreConformance("latest-respects-canceled-context")
}

func checkCheckpointStoreGetCanceledContext[S any](newStore func() CheckpointStoreConformanceStore[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("get-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.Get(ctx, "checkpoint"); !errors.Is(err, context.Canceled) {
		return failedCheckpointStoreConformance("get-respects-canceled-context", fmt.Errorf("Get canceled context error = %v, want context.Canceled", err))
	}
	return passedCheckpointStoreConformance("get-respects-canceled-context")
}

func checkCheckpointStoreListCanceledContext[S any](newStore func() CheckpointStoreConformanceStore[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("list-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.List(ctx, "thread"); !errors.Is(err, context.Canceled) {
		return failedCheckpointStoreConformance("list-respects-canceled-context", fmt.Errorf("List canceled context error = %v, want context.Canceled", err))
	}
	return passedCheckpointStoreConformance("list-respects-canceled-context")
}

func checkCheckpointStoreLoadsLatest[S any](ctx context.Context, newStore func() CheckpointStoreConformanceStore[S], checkpoints []graph.Checkpoint[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("loads-latest", err)
	}
	if err := seedCheckpointConformanceStore(ctx, store, checkpoints); err != nil {
		return failedCheckpointStoreConformance("loads-latest", err)
	}
	want := checkpoints[1]
	got, ok, err := store.Latest(ctx, want.ThreadID)
	if err != nil {
		return failedCheckpointStoreConformance("loads-latest", err)
	}
	if !ok {
		return failedCheckpointStoreConformance("loads-latest", errors.New("Latest returned ok=false"))
	}
	if got.ID != want.ID || got.ThreadID != want.ThreadID || got.Step != want.Step || got.Node != want.Node {
		return failedCheckpointStoreConformance("loads-latest", fmt.Errorf("Latest = %s/%s/%d/%s, want %s/%s/%d/%s", got.ID, got.ThreadID, got.Step, got.Node, want.ID, want.ThreadID, want.Step, want.Node))
	}
	return passedCheckpointStoreConformance("loads-latest")
}

func checkCheckpointStoreGetsByID[S any](ctx context.Context, newStore func() CheckpointStoreConformanceStore[S], checkpoints []graph.Checkpoint[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("gets-by-id", err)
	}
	if err := seedCheckpointConformanceStore(ctx, store, checkpoints); err != nil {
		return failedCheckpointStoreConformance("gets-by-id", err)
	}
	want := checkpoints[0]
	got, ok, err := store.Get(ctx, want.ID)
	if err != nil {
		return failedCheckpointStoreConformance("gets-by-id", err)
	}
	if !ok {
		return failedCheckpointStoreConformance("gets-by-id", errors.New("Get returned ok=false"))
	}
	if got.ID != want.ID || got.ThreadID != want.ThreadID || got.Step != want.Step || got.Node != want.Node {
		return failedCheckpointStoreConformance("gets-by-id", fmt.Errorf("Get = %s/%s/%d/%s, want %s/%s/%d/%s", got.ID, got.ThreadID, got.Step, got.Node, want.ID, want.ThreadID, want.Step, want.Node))
	}
	if _, ok, err := store.Get(ctx, "gopact-conformance-missing-checkpoint"); err != nil || ok {
		return failedCheckpointStoreConformance("gets-by-id", fmt.Errorf("Get missing ok=%v err=%v, want false nil", ok, err))
	}
	return passedCheckpointStoreConformance("gets-by-id")
}

func checkCheckpointStoreListsThread[S any](ctx context.Context, newStore func() CheckpointStoreConformanceStore[S], checkpoints []graph.Checkpoint[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("lists-thread", err)
	}
	if err := seedCheckpointConformanceStore(ctx, store, checkpoints); err != nil {
		return failedCheckpointStoreConformance("lists-thread", err)
	}
	list, err := store.List(ctx, checkpoints[0].ThreadID)
	if err != nil {
		return failedCheckpointStoreConformance("lists-thread", err)
	}
	if len(list) != 2 {
		return failedCheckpointStoreConformance("lists-thread", fmt.Errorf("List count = %d, want 2", len(list)))
	}
	if list[0].ID != checkpoints[0].ID || list[1].ID != checkpoints[1].ID {
		return failedCheckpointStoreConformance("lists-thread", fmt.Errorf("List ids = %q/%q, want %q/%q", list[0].ID, list[1].ID, checkpoints[0].ID, checkpoints[1].ID))
	}
	return passedCheckpointStoreConformance("lists-thread")
}

func checkCheckpointStoreDoesNotMutateCheckpoint[S any](ctx context.Context, newStore func() CheckpointStoreConformanceStore[S], checkpoint graph.Checkpoint[S]) CheckpointStoreConformanceResult {
	store, err := newCheckpointConformanceStore(newStore)
	if err != nil {
		return failedCheckpointStoreConformance("does-not-mutate-checkpoint", err)
	}
	before := copyCheckpointForConformance(checkpoint)
	if err := store.Put(ctx, checkpoint); err != nil {
		return failedCheckpointStoreConformance("does-not-mutate-checkpoint", err)
	}
	if !reflect.DeepEqual(checkpoint, before) {
		return failedCheckpointStoreConformance("does-not-mutate-checkpoint", errors.New("store mutated input checkpoint"))
	}
	return passedCheckpointStoreConformance("does-not-mutate-checkpoint")
}

func newCheckpointConformanceStore[S any](newStore func() CheckpointStoreConformanceStore[S]) (CheckpointStoreConformanceStore[S], error) {
	if newStore == nil {
		return nil, errors.New("checkpoint store factory is nil")
	}
	store := newStore()
	if store == nil {
		return nil, errors.New("checkpoint store factory returned nil")
	}
	return store, nil
}

func seedCheckpointConformanceStore[S any](ctx context.Context, store CheckpointStoreConformanceStore[S], checkpoints []graph.Checkpoint[S]) error {
	for _, checkpoint := range checkpoints {
		if err := store.Put(ctx, copyCheckpointForConformance(checkpoint)); err != nil {
			return err
		}
	}
	return nil
}

func passedCheckpointStoreConformance(name string) CheckpointStoreConformanceResult {
	return CheckpointStoreConformanceResult{Case: name, Passed: true}
}

func failedCheckpointStoreConformance(name string, err error) CheckpointStoreConformanceResult {
	return CheckpointStoreConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrCheckpointStoreConformanceFailed, err),
	}
}

func normalizeCheckpointConformanceCheckpoints[S any](in []graph.Checkpoint[S]) []graph.Checkpoint[S] {
	if len(in) >= 3 && in[0].ThreadID != "" && in[0].ThreadID == in[1].ThreadID && in[2].ThreadID != in[0].ThreadID {
		return copyCheckpointsForConformance(in)
	}
	return defaultCheckpointConformanceCheckpoints[S]()
}

func defaultCheckpointConformanceCheckpoints[S any]() []graph.Checkpoint[S] {
	var zero S
	return []graph.Checkpoint[S]{
		{
			ID:        "gopact-conformance-checkpoint-1",
			IDs:       gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread"},
			ThreadID:  "gopact-conformance-thread",
			Step:      1,
			Node:      "first",
			Phase:     gopact.StepCompleted,
			State:     zero,
			Queue:     []string{"second"},
			Metadata:  map[string]any{"conformance": "checkpoint"},
			CreatedAt: time.Unix(1, 0),
		},
		{
			ID:        "gopact-conformance-checkpoint-2",
			IDs:       gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread"},
			ThreadID:  "gopact-conformance-thread",
			Step:      2,
			Node:      "second",
			Phase:     gopact.StepCompleted,
			State:     zero,
			CreatedAt: time.Unix(2, 0),
		},
		{
			ID:        "gopact-conformance-checkpoint-other",
			IDs:       gopact.RuntimeIDs{RunID: "gopact-conformance-other-run", ThreadID: "gopact-conformance-other-thread"},
			ThreadID:  "gopact-conformance-other-thread",
			Step:      1,
			Node:      "other",
			Phase:     gopact.StepCompleted,
			State:     zero,
			CreatedAt: time.Unix(3, 0),
		},
	}
}

func copyCheckpointsForConformance[S any](in []graph.Checkpoint[S]) []graph.Checkpoint[S] {
	if len(in) == 0 {
		return nil
	}
	out := make([]graph.Checkpoint[S], len(in))
	for i, checkpoint := range in {
		out[i] = copyCheckpointForConformance(checkpoint)
	}
	return out
}

func copyCheckpointForConformance[S any](in graph.Checkpoint[S]) graph.Checkpoint[S] {
	out := in
	out.Queue = append([]string(nil), in.Queue...)
	out.Pending = copyInterruptRecordForConformance(in.Pending)
	out.Effects = copyEffectRecordsForConformance(in.Effects)
	out.Metadata = copyConformanceAnyMap(in.Metadata)
	return out
}

func copyInterruptRecordForConformance(in *gopact.InterruptRecord) *gopact.InterruptRecord {
	if in == nil {
		return nil
	}
	out := *in
	out.Prompt = copyConformanceMessages([]gopact.Message{in.Prompt})[0]
	out.ResumeSchema = copyConformanceJSONSchema(in.ResumeSchema)
	out.Metadata = copyConformanceAnyMap(in.Metadata)
	return &out
}

func copyEffectRecordsForConformance(in []gopact.EffectRecord) []gopact.EffectRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.EffectRecord, len(in))
	for i, effect := range in {
		out[i] = effect
		out[i].DependsOn = append([]string(nil), effect.DependsOn...)
		out[i].Artifacts = copyArtifactRefsForConformance(effect.Artifacts)
		if effect.Sandbox != nil {
			sandbox := *effect.Sandbox
			sandbox.Command = append([]string(nil), effect.Sandbox.Command...)
			sandbox.Metadata = copyConformanceAnyMap(effect.Sandbox.Metadata)
			out[i].Sandbox = &sandbox
		}
		out[i].Metadata = copyConformanceAnyMap(effect.Metadata)
	}
	return out
}

func copyArtifactRefsForConformance(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Metadata = copyConformanceAnyMap(ref.Metadata)
	}
	return out
}
