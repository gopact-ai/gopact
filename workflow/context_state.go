package workflow

import (
	"context"
	"errors"
	"fmt"
)

type workflowContextKey struct{}
type workflowContextTxnKey struct{}

type workflowContextTxn struct {
	key      *workflowContextKey
	value    any
	revision int64
	changed  bool
}

// Context is a typed handle to one workflow's business state.
type Context[S any] struct { //nolint:revive // Context is the domain term fixed by the workflow contract.
	key *workflowContextKey
}

// Context defines the workflow's typed business state from its input.
func (wf *Workflow[I, O]) Context[S any](init func(I) S) *Context[S] { //nolint:revive // Context is the public domain term.
	if wf == nil {
		return &Context[S]{}
	}
	wf.assertMutable()
	if wf.contextSet {
		wf.contextTwice = true
	}
	key := &workflowContextKey{}
	wf.contextKey = key
	wf.contextType = typeOf[S]()
	wf.contextSet = true
	if init != nil {
		wf.contextInit = func(input any) (any, error) {
			typed, ok := input.(I)
			if !ok {
				return nil, fmt.Errorf("workflow: context input type %T does not match %s", input, typeOf[I]())
			}
			return init(typed), nil
		}
	}
	return &Context[S]{key: key}
}

// Get returns this Attempt's workflow context snapshot.
func (handle *Context[S]) Get(ctx context.Context) (S, error) {
	var zero S
	txn, err := handle.txn(ctx)
	if err != nil {
		return zero, err
	}
	value, ok := txn.value.(S)
	if !ok {
		return zero, fmt.Errorf("workflow: context value type %T does not match %s", txn.value, typeOf[S]())
	}
	return value, nil
}

// Set replaces the context when this Attempt commits successfully.
func (handle *Context[S]) Set(ctx context.Context, value S) error {
	txn, err := handle.txn(ctx)
	if err != nil {
		return err
	}
	txn.value = value
	txn.changed = true
	return nil
}

func (handle *Context[S]) txn(ctx context.Context) (*workflowContextTxn, error) {
	if handle == nil || handle.key == nil {
		return nil, errors.New("workflow: context handle is nil")
	}
	if ctx == nil {
		return nil, errors.New("workflow: context is unavailable")
	}
	txn, ok := ctx.Value(workflowContextTxnKey{}).(*workflowContextTxn)
	if !ok || txn == nil || txn.key != handle.key {
		return nil, errors.New("workflow: context is unavailable")
	}
	return txn, nil
}

func (state *runState) commitContext(result nodeRunResult) error {
	if !result.contextChanged {
		return nil
	}
	if result.contextBaseRevision != state.contextRevision {
		return fmt.Errorf(
			"workflow: context revision conflict: attempt based on %d, current %d",
			result.contextBaseRevision,
			state.contextRevision,
		)
	}
	state.workflowContext = result.contextValue
	state.hasContext = true
	state.contextRevision++
	return nil
}
