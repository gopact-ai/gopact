package checkpointconformance

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
)

func TestCheckCheckpointStoreConformancePassesWellBehavedStore(t *testing.T) {
	harness := CheckpointStoreConformanceHarness[string]{
		NewStore: func() CheckpointStoreConformanceStore[string] {
			return newCheckpointConformanceMemoryStore[string]()
		},
		Checkpoints: checkpointConformanceFixtures(),
	}

	results := CheckCheckpointStoreConformance(context.Background(), harness)
	if failed := failedCheckpointStoreConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckCheckpointStoreConformance() failed cases: %v", failed)
	}
	RequireCheckpointStoreConformance(t, harness)
}

func TestCheckCheckpointStoreConformanceReportsMissingLatest(t *testing.T) {
	harness := CheckpointStoreConformanceHarness[string]{
		NewStore: func() CheckpointStoreConformanceStore[string] {
			return droppingLatestCheckpointStore[string]{store: newCheckpointConformanceMemoryStore[string]()}
		},
		Checkpoints: checkpointConformanceFixtures(),
	}

	results := CheckCheckpointStoreConformance(context.Background(), harness)
	if !hasFailedCheckpointStoreConformanceCase(results, "loads-latest") {
		t.Fatalf("CheckCheckpointStoreConformance() did not report latest failure: %+v", results)
	}
}

func TestCheckCheckpointStoreConformanceReportsCheckpointMutation(t *testing.T) {
	harness := CheckpointStoreConformanceHarness[string]{
		NewStore: func() CheckpointStoreConformanceStore[string] {
			return mutatingCheckpointStore[string]{store: newCheckpointConformanceMemoryStore[string]()}
		},
		Checkpoints: checkpointConformanceFixtures(),
	}

	results := CheckCheckpointStoreConformance(context.Background(), harness)
	if !hasFailedCheckpointStoreConformanceCase(results, "does-not-mutate-checkpoint") {
		t.Fatalf("CheckCheckpointStoreConformance() did not report mutation failure: %+v", results)
	}
}

func checkpointConformanceFixtures() []graph.Checkpoint[string] {
	return []graph.Checkpoint[string]{
		{
			ID:        "checkpoint-1",
			IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			ThreadID:  "thread-1",
			Step:      1,
			Node:      "first",
			Phase:     gopact.StepCompleted,
			State:     "one",
			Queue:     []string{"second"},
			Metadata:  map[string]any{"keep": "original"},
			CreatedAt: time.Unix(1, 0),
		},
		{
			ID:        "checkpoint-2",
			IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			ThreadID:  "thread-1",
			Step:      2,
			Node:      "second",
			Phase:     gopact.StepCompleted,
			State:     "two",
			CreatedAt: time.Unix(2, 0),
		},
		{
			ID:        "checkpoint-other",
			IDs:       gopact.RuntimeIDs{RunID: "run-2", ThreadID: "thread-2"},
			ThreadID:  "thread-2",
			Step:      1,
			Node:      "other",
			Phase:     gopact.StepCompleted,
			State:     "ignored",
			CreatedAt: time.Unix(3, 0),
		},
	}
}

func failedCheckpointStoreConformanceCases(results []CheckpointStoreConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedCheckpointStoreConformanceCase(results []CheckpointStoreConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

type checkpointConformanceMemoryStore[S comparable] struct {
	byID     map[string]graph.Checkpoint[S]
	byThread map[string][]string
}

func newCheckpointConformanceMemoryStore[S comparable]() *checkpointConformanceMemoryStore[S] {
	return &checkpointConformanceMemoryStore[S]{
		byID:     make(map[string]graph.Checkpoint[S]),
		byThread: make(map[string][]string),
	}
}

func (s *checkpointConformanceMemoryStore[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.byID[checkpoint.ID]; !ok {
		s.byThread[checkpoint.ThreadID] = append(s.byThread[checkpoint.ThreadID], checkpoint.ID)
	}
	s.byID[checkpoint.ID] = checkpoint
	return nil
}

func (s *checkpointConformanceMemoryStore[S]) Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool, error) {
	if err := ctx.Err(); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	ids := s.byThread[threadID]
	if len(ids) == 0 {
		var zero graph.Checkpoint[S]
		return zero, false, nil
	}
	latest := slices.MaxFunc(ids, func(a, b string) int {
		return s.byID[a].CreatedAt.Compare(s.byID[b].CreatedAt)
	})
	return s.byID[latest], true, nil
}

func (s *checkpointConformanceMemoryStore[S]) Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error) {
	if err := ctx.Err(); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	checkpoint, ok := s.byID[id]
	return checkpoint, ok, nil
}

func (s *checkpointConformanceMemoryStore[S]) List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids := append([]string(nil), s.byThread[threadID]...)
	slices.SortFunc(ids, func(a, b string) int {
		return s.byID[a].CreatedAt.Compare(s.byID[b].CreatedAt)
	})
	out := make([]graph.Checkpoint[S], 0, len(ids))
	for _, id := range ids {
		out = append(out, s.byID[id])
	}
	return out, nil
}

type droppingLatestCheckpointStore[S comparable] struct {
	store *checkpointConformanceMemoryStore[S]
}

func (s droppingLatestCheckpointStore[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
	return s.store.Put(ctx, checkpoint)
}

func (s droppingLatestCheckpointStore[S]) Latest(ctx context.Context, _ string) (graph.Checkpoint[S], bool, error) {
	if err := ctx.Err(); err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	var zero graph.Checkpoint[S]
	return zero, false, nil
}

func (s droppingLatestCheckpointStore[S]) Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error) {
	return s.store.Get(ctx, id)
}

func (s droppingLatestCheckpointStore[S]) List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error) {
	return s.store.List(ctx, threadID)
}

type mutatingCheckpointStore[S comparable] struct {
	store *checkpointConformanceMemoryStore[S]
}

func (s mutatingCheckpointStore[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
	if checkpoint.Metadata == nil {
		checkpoint.Metadata = map[string]any{"keep": "changed"}
	} else {
		checkpoint.Metadata["keep"] = "changed"
	}
	return s.store.Put(ctx, checkpoint)
}

func (s mutatingCheckpointStore[S]) Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool, error) {
	return s.store.Latest(ctx, threadID)
}

func (s mutatingCheckpointStore[S]) Get(ctx context.Context, id string) (graph.Checkpoint[S], bool, error) {
	return s.store.Get(ctx, id)
}

func (s mutatingCheckpointStore[S]) List(ctx context.Context, threadID string) ([]graph.Checkpoint[S], error) {
	return s.store.List(ctx, threadID)
}
