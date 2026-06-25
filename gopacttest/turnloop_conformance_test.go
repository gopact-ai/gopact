package gopacttest

import (
	"context"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestCheckTurnLoopStoreConformancePassesMemoryStore(t *testing.T) {
	harness := TurnLoopStoreConformanceHarness{
		NewStore: func() gopact.TurnLoopStore {
			return gopact.NewMemoryTurnLoopStore()
		},
		State: turnLoopConformanceState(),
	}

	results := CheckTurnLoopStoreConformance(context.Background(), harness)
	if failed := failedTurnLoopStoreConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckTurnLoopStoreConformance() failed cases: %v", failed)
	}
	RequireTurnLoopStoreConformance(t, harness)
}

func TestCheckTurnLoopStoreConformanceReportsDroppedSave(t *testing.T) {
	harness := TurnLoopStoreConformanceHarness{
		NewStore: func() gopact.TurnLoopStore {
			return droppingTurnLoopStore{}
		},
		State: turnLoopConformanceState(),
	}

	results := CheckTurnLoopStoreConformance(context.Background(), harness)
	if !hasFailedTurnLoopStoreConformanceCase(results, "saves-and-loads-state") {
		t.Fatalf("CheckTurnLoopStoreConformance() did not report save/load failure: %+v", results)
	}
}

func TestCheckTurnLoopStoreConformanceReportsStateMutation(t *testing.T) {
	harness := TurnLoopStoreConformanceHarness{
		NewStore: func() gopact.TurnLoopStore {
			return mutatingTurnLoopStore{store: gopact.NewMemoryTurnLoopStore()}
		},
		State: turnLoopConformanceState(),
	}

	results := CheckTurnLoopStoreConformance(context.Background(), harness)
	if !hasFailedTurnLoopStoreConformanceCase(results, "does-not-mutate-state") {
		t.Fatalf("CheckTurnLoopStoreConformance() did not report mutation failure: %+v", results)
	}
}

func turnLoopConformanceState() gopact.TurnLoopState {
	return gopact.TurnLoopState{
		Pending: []gopact.TurnInputRecord{
			{
				ID:        "turn-input-1",
				Kind:      gopact.TurnInputUser,
				Input:     "hello",
				IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
				CreatedAt: time.Unix(1, 0),
				Metadata:  map[string]any{"keep": "original"},
			},
		},
		PendingEvents: []gopact.Event{
			{Type: gopact.EventTurnInputReceived, IDs: gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}},
		},
		Interrupted: &gopact.TurnInputRecord{
			ID:        "turn-input-interrupted",
			Kind:      gopact.TurnInputUser,
			Input:     "needs approval",
			IDs:       gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			CreatedAt: time.Unix(2, 0),
			Metadata:  map[string]any{"interrupt": "yes"},
		},
		InputSeq:  2,
		UpdatedAt: time.Unix(3, 0),
	}
}

func failedTurnLoopStoreConformanceCases(results []TurnLoopStoreConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedTurnLoopStoreConformanceCase(results []TurnLoopStoreConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

type droppingTurnLoopStore struct{}

func (droppingTurnLoopStore) Load(ctx context.Context) (gopact.TurnLoopState, bool, error) {
	if err := ctx.Err(); err != nil {
		return gopact.TurnLoopState{}, false, err
	}
	return gopact.TurnLoopState{}, false, nil
}

func (droppingTurnLoopStore) Save(ctx context.Context, _ gopact.TurnLoopState) error {
	return ctx.Err()
}

type mutatingTurnLoopStore struct {
	store gopact.TurnLoopStore
}

func (s mutatingTurnLoopStore) Load(ctx context.Context) (gopact.TurnLoopState, bool, error) {
	return s.store.Load(ctx)
}

func (s mutatingTurnLoopStore) Save(ctx context.Context, state gopact.TurnLoopState) error {
	state.Pending[0].Metadata["keep"] = "changed"
	return s.store.Save(ctx, state)
}
