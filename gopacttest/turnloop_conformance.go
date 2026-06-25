package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

var ErrTurnLoopStoreConformanceFailed = errors.New("gopacttest: turn loop store conformance failed")

// TurnLoopStoreConformanceHarness describes one TurnLoopStore implementation under test.
type TurnLoopStoreConformanceHarness struct {
	NewStore func() gopact.TurnLoopStore
	State    gopact.TurnLoopState
}

// TurnLoopStoreConformanceResult is the observed result for one turn loop store contract case.
type TurnLoopStoreConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckTurnLoopStoreConformance runs reusable TurnLoopStore contract cases.
func CheckTurnLoopStoreConformance(ctx context.Context, harness TurnLoopStoreConformanceHarness) []TurnLoopStoreConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	state := harness.State
	if len(state.Pending) == 0 && state.Interrupted == nil && state.InputSeq == 0 {
		state = defaultTurnLoopConformanceState()
	}

	return []TurnLoopStoreConformanceResult{
		checkTurnLoopStoreFactory(harness.NewStore),
		checkTurnLoopStoreLoadMissing(ctx, harness.NewStore),
		checkTurnLoopStoreSaveCanceledContext(harness.NewStore, copyTurnLoopStateForConformance(state)),
		checkTurnLoopStoreLoadCanceledContext(harness.NewStore),
		checkTurnLoopStoreSavesAndLoadsState(ctx, harness.NewStore, copyTurnLoopStateForConformance(state)),
		checkTurnLoopStoreDoesNotMutateState(ctx, harness.NewStore, copyTurnLoopStateForConformance(state)),
		checkTurnLoopStoreLoadReturnsCopy(ctx, harness.NewStore, copyTurnLoopStateForConformance(state)),
	}
}

// RequireTurnLoopStoreConformance fails the test unless store satisfies the TurnLoopStore contract.
func RequireTurnLoopStoreConformance(t testing.TB, harness TurnLoopStoreConformanceHarness) {
	t.Helper()

	for _, result := range CheckTurnLoopStoreConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("turn loop store conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkTurnLoopStoreFactory(newStore func() gopact.TurnLoopStore) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("has-store-factory", err)
	}
	if store == nil {
		return failedTurnLoopStoreConformance("has-store-factory", errors.New("turn loop store is nil"))
	}
	return passedTurnLoopStoreConformance("has-store-factory")
}

func checkTurnLoopStoreLoadMissing(ctx context.Context, newStore func() gopact.TurnLoopStore) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("loads-missing", err)
	}
	_, ok, err := store.Load(ctx)
	if err != nil {
		return failedTurnLoopStoreConformance("loads-missing", err)
	}
	if ok {
		return failedTurnLoopStoreConformance("loads-missing", errors.New("Load on empty store returned ok=true"))
	}
	return passedTurnLoopStoreConformance("loads-missing")
}

func checkTurnLoopStoreSaveCanceledContext(newStore func() gopact.TurnLoopStore, state gopact.TurnLoopState) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("save-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(ctx, state); !errors.Is(err, context.Canceled) {
		return failedTurnLoopStoreConformance("save-respects-canceled-context", fmt.Errorf("Save canceled context error = %v, want context.Canceled", err))
	}
	return passedTurnLoopStoreConformance("save-respects-canceled-context")
}

func checkTurnLoopStoreLoadCanceledContext(newStore func() gopact.TurnLoopStore) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("load-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.Load(ctx); !errors.Is(err, context.Canceled) {
		return failedTurnLoopStoreConformance("load-respects-canceled-context", fmt.Errorf("Load canceled context error = %v, want context.Canceled", err))
	}
	return passedTurnLoopStoreConformance("load-respects-canceled-context")
}

func checkTurnLoopStoreSavesAndLoadsState(ctx context.Context, newStore func() gopact.TurnLoopStore, state gopact.TurnLoopState) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("saves-and-loads-state", err)
	}
	if err := store.Save(ctx, state); err != nil {
		return failedTurnLoopStoreConformance("saves-and-loads-state", err)
	}
	got, ok, err := store.Load(ctx)
	if err != nil {
		return failedTurnLoopStoreConformance("saves-and-loads-state", err)
	}
	if !ok {
		return failedTurnLoopStoreConformance("saves-and-loads-state", errors.New("Load returned ok=false after Save"))
	}
	if got.InputSeq != state.InputSeq || len(got.Pending) != len(state.Pending) || len(got.PendingEvents) != len(state.PendingEvents) || (got.Interrupted == nil) != (state.Interrupted == nil) {
		return failedTurnLoopStoreConformance("saves-and-loads-state", fmt.Errorf("loaded state shape = pending:%d events:%d interrupted:%v seq:%d, want pending:%d events:%d interrupted:%v seq:%d", len(got.Pending), len(got.PendingEvents), got.Interrupted != nil, got.InputSeq, len(state.Pending), len(state.PendingEvents), state.Interrupted != nil, state.InputSeq))
	}
	if len(state.Pending) > 0 && got.Pending[0].ID != state.Pending[0].ID {
		return failedTurnLoopStoreConformance("saves-and-loads-state", fmt.Errorf("loaded pending[0].id = %q, want %q", got.Pending[0].ID, state.Pending[0].ID))
	}
	return passedTurnLoopStoreConformance("saves-and-loads-state")
}

func checkTurnLoopStoreDoesNotMutateState(ctx context.Context, newStore func() gopact.TurnLoopStore, state gopact.TurnLoopState) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("does-not-mutate-state", err)
	}
	before := copyTurnLoopStateForConformance(state)
	if err := store.Save(ctx, state); err != nil {
		return failedTurnLoopStoreConformance("does-not-mutate-state", err)
	}
	if !reflect.DeepEqual(state, before) {
		return failedTurnLoopStoreConformance("does-not-mutate-state", errors.New("store mutated input state"))
	}
	return passedTurnLoopStoreConformance("does-not-mutate-state")
}

func checkTurnLoopStoreLoadReturnsCopy(ctx context.Context, newStore func() gopact.TurnLoopStore, state gopact.TurnLoopState) TurnLoopStoreConformanceResult {
	store, err := newTurnLoopConformanceStore(newStore)
	if err != nil {
		return failedTurnLoopStoreConformance("load-returns-copy", err)
	}
	if err := store.Save(ctx, state); err != nil {
		return failedTurnLoopStoreConformance("load-returns-copy", err)
	}
	got, ok, err := store.Load(ctx)
	if err != nil {
		return failedTurnLoopStoreConformance("load-returns-copy", err)
	}
	if !ok {
		return failedTurnLoopStoreConformance("load-returns-copy", errors.New("Load returned ok=false after Save"))
	}
	if len(got.Pending) == 0 || got.Pending[0].Metadata == nil {
		return failedTurnLoopStoreConformance("load-returns-copy", errors.New("loaded state is missing pending metadata for copy check"))
	}
	got.Pending[0].Metadata["keep"] = "changed"

	again, ok, err := store.Load(ctx)
	if err != nil {
		return failedTurnLoopStoreConformance("load-returns-copy", err)
	}
	if !ok {
		return failedTurnLoopStoreConformance("load-returns-copy", errors.New("second Load returned ok=false"))
	}
	if again.Pending[0].Metadata["keep"] != state.Pending[0].Metadata["keep"] {
		return failedTurnLoopStoreConformance("load-returns-copy", errors.New("mutating loaded state changed stored state"))
	}
	return passedTurnLoopStoreConformance("load-returns-copy")
}

func newTurnLoopConformanceStore(newStore func() gopact.TurnLoopStore) (gopact.TurnLoopStore, error) {
	if newStore == nil {
		return nil, errors.New("turn loop store factory is nil")
	}
	store := newStore()
	if store == nil {
		return nil, errors.New("turn loop store factory returned nil")
	}
	return store, nil
}

func passedTurnLoopStoreConformance(name string) TurnLoopStoreConformanceResult {
	return TurnLoopStoreConformanceResult{Case: name, Passed: true}
}

func failedTurnLoopStoreConformance(name string, err error) TurnLoopStoreConformanceResult {
	return TurnLoopStoreConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrTurnLoopStoreConformanceFailed, err),
	}
}

func defaultTurnLoopConformanceState() gopact.TurnLoopState {
	return gopact.TurnLoopState{
		Pending: []gopact.TurnInputRecord{
			{
				ID:        "gopact-conformance-turn-input",
				Kind:      gopact.TurnInputUser,
				Input:     "gopact conformance",
				IDs:       gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread"},
				CreatedAt: time.Unix(1, 0),
				Metadata:  map[string]any{"conformance": "turnloop"},
			},
		},
		PendingEvents: []gopact.Event{
			{Type: gopact.EventTurnInputReceived, IDs: gopact.RuntimeIDs{RunID: "gopact-conformance-run", ThreadID: "gopact-conformance-thread"}, CreatedAt: time.Unix(2, 0)},
		},
		InputSeq:  1,
		UpdatedAt: time.Unix(3, 0),
	}
}

func copyTurnLoopStateForConformance(in gopact.TurnLoopState) gopact.TurnLoopState {
	out := in
	out.Pending = copyTurnInputRecordsForConformance(in.Pending)
	out.PendingEvents = copyEventsForConformance(in.PendingEvents)
	if in.Interrupted != nil {
		record := copyTurnInputRecordForConformance(*in.Interrupted)
		out.Interrupted = &record
	}
	return out
}

func copyTurnInputRecordsForConformance(in []gopact.TurnInputRecord) []gopact.TurnInputRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.TurnInputRecord, len(in))
	for i, record := range in {
		out[i] = copyTurnInputRecordForConformance(record)
	}
	return out
}

func copyTurnInputRecordForConformance(in gopact.TurnInputRecord) gopact.TurnInputRecord {
	out := in
	out.Resume = copyResumeRequestForConformance(in.Resume)
	out.Interrupt = copyInterruptRecordForConformance(in.Interrupt)
	out.Metadata = copyConformanceAnyMap(in.Metadata)
	return out
}

func copyResumeRequestForConformance(in *gopact.ResumeRequest) *gopact.ResumeRequest {
	if in == nil {
		return nil
	}
	out := *in
	out.Metadata = copyConformanceAnyMap(in.Metadata)
	out.Payload = copyConformanceAnyValue(in.Payload)
	return &out
}

func copyEventsForConformance(in []gopact.Event) []gopact.Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.Event, len(in))
	for i, event := range in {
		out[i] = event
		if event.Message != nil {
			message := copyConformanceMessages([]gopact.Message{*event.Message})[0]
			out[i].Message = &message
		}
		if event.ToolCall != nil {
			call := *event.ToolCall
			call.Arguments = append([]byte(nil), event.ToolCall.Arguments...)
			out[i].ToolCall = &call
		}
		out[i].Artifacts = copyArtifactRefsForConformance(event.Artifacts)
		out[i].Metadata = copyConformanceAnyMap(event.Metadata)
	}
	return out
}
