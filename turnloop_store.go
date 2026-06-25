package gopact

import (
	"context"
	"errors"
	"sync"
	"time"
)

// TurnLoopStore persists TurnLoop queue state across TurnLoop instances.
type TurnLoopStore interface {
	Load(ctx context.Context) (TurnLoopState, bool, error)
	Save(ctx context.Context, state TurnLoopState) error
}

// TurnLoopState is the durable queue snapshot for a TurnLoop.
type TurnLoopState struct {
	Pending       []TurnInputRecord `json:"pending,omitempty"`
	PendingEvents []Event           `json:"pending_events,omitempty"`
	Interrupted   *TurnInputRecord  `json:"interrupted,omitempty"`
	InputSeq      uint64            `json:"input_seq,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
}

// WithTurnLoopStore restores and persists TurnLoop queue state.
func WithTurnLoopStore(ctx context.Context, store TurnLoopStore) TurnLoopOption {
	return func(loop *TurnLoop) error {
		if loop == nil {
			return nil
		}
		if store == nil {
			return errors.New("gopact: turn loop store is nil")
		}
		if ctx == nil {
			ctx = context.TODO()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		loop.store = store
		state, ok, err := store.Load(ctx)
		if err != nil {
			return err
		}
		if ok {
			loop.applyState(state)
		}
		return nil
	}
}

// MemoryTurnLoopStore stores TurnLoop state in memory.
type MemoryTurnLoopStore struct {
	mu    sync.RWMutex
	state TurnLoopState
	has   bool
}

// NewMemoryTurnLoopStore creates an empty in-memory TurnLoop store.
func NewMemoryTurnLoopStore() *MemoryTurnLoopStore {
	return &MemoryTurnLoopStore{}
}

// Load returns the stored TurnLoop state.
func (s *MemoryTurnLoopStore) Load(ctx context.Context) (TurnLoopState, bool, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return TurnLoopState{}, false, err
	}
	if s == nil {
		return TurnLoopState{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.has {
		return TurnLoopState{}, false, nil
	}
	return copyTurnLoopState(s.state), true, nil
}

// Save stores a TurnLoop state snapshot.
func (s *MemoryTurnLoopStore) Save(ctx context.Context, state TurnLoopState) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = copyTurnLoopState(state)
	s.has = true
	return nil
}

func copyTurnLoopState(in TurnLoopState) TurnLoopState {
	out := in
	out.Pending = copyTurnInputRecords(in.Pending)
	out.PendingEvents = copyEvents(in.PendingEvents)
	if in.Interrupted != nil {
		record := copyTurnInputRecord(*in.Interrupted)
		out.Interrupted = &record
	}
	return out
}
