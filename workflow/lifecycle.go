package workflow

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// LifecycleHook runs at a workflow or node boundary.
type LifecycleHook[C any] struct {
	Name string
	Fn   func(*C) error
}

// Hook creates a lifecycle hook value.
func Hook[C any](name string, fn func(*C) error) LifecycleHook[C] {
	return LifecycleHook[C]{Name: name, Fn: fn}
}

// WorkflowContext is the mutable workflow boundary context.
type WorkflowContext[I, O any] struct { //nolint:revive // Paired with NodeContext; Context is ambiguous.
	ctx    context.Context
	Input  I
	Output O
}

// Context returns the current run context.
func (c *WorkflowContext[I, O]) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// NodeContext is the mutable node boundary context.
type NodeContext[I, O any] struct {
	ctx    context.Context
	Input  I
	Output O
}

// Context returns the current run context.
func (c *NodeContext[I, O]) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// BeforeWorkflow binds hooks that run before workflow input reaches entry.
func (wf *Workflow[I, O]) BeforeWorkflow(hooks ...LifecycleHook[WorkflowContext[I, O]]) {
	if wf == nil {
		return
	}
	wf.assertMutable()
	wf.beforeWorkflow = append(wf.beforeWorkflow, hooks...)
}

// AfterWorkflow binds hooks that run before each workflow output is returned.
func (wf *Workflow[I, O]) AfterWorkflow(hooks ...LifecycleHook[WorkflowContext[I, O]]) {
	if wf == nil {
		return
	}
	wf.assertMutable()
	wf.afterWorkflow = append(wf.afterWorkflow, hooks...)
}

// Before binds hooks that run before this node body.
func (n *Node[I, O]) Before(hooks ...LifecycleHook[NodeContext[I, O]]) {
	if n == nil {
		return
	}
	n.assertMutable()
	n.before = append(n.before, hooks...)
}

// After binds hooks that run after this node body returns a candidate output.
func (n *Node[I, O]) After(hooks ...LifecycleHook[NodeContext[I, O]]) {
	if n == nil {
		return
	}
	n.assertMutable()
	n.after = append(n.after, hooks...)
}

func validateLifecycleHooks[C any](owner, phase string, hooks []LifecycleHook[C]) error {
	seen := map[string]struct{}{}
	for _, hook := range hooks {
		if hook.Name == "" {
			return fmt.Errorf("workflow: %s %s hook has empty name", owner, phase)
		}
		if hook.Fn == nil {
			return fmt.Errorf("workflow: %s %s hook %q has nil function", owner, phase, hook.Name)
		}
		if _, ok := seen[hook.Name]; ok {
			return fmt.Errorf("workflow: duplicate %s %s hook %q", owner, phase, hook.Name)
		}
		seen[hook.Name] = struct{}{}
	}
	return nil
}

func runLifecycleHooks[C any](hooks []LifecycleHook[C], ctx *C) error {
	for _, hook := range hooks {
		if err := hook.run(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (hook LifecycleHook[C]) run(ctx *C) error {
	eventCtx := lifecycleContext(ctx)
	if err := hook.emit(eventCtx, EventLifecycleHookStarted); err != nil {
		return err
	}
	if err := hook.Fn(ctx); err != nil {
		if emitErr := hook.emit(eventCtx, EventLifecycleHookFailed); emitErr != nil {
			return emitErr
		}
		return err
	}
	return hook.emit(eventCtx, EventLifecycleHookCompleted)
}

func (hook LifecycleHook[C]) emit(ctx context.Context, eventType string) error {
	if ctx == nil {
		return nil
	}
	return emitRuntimeEvent(ctx, gopact.Event{Type: eventType, Summary: hook.Name})
}

func lifecycleContext(ctx any) context.Context {
	entity, ok := ctx.(interface{ Context() context.Context })
	if !ok {
		return nil
	}
	return entity.Context()
}
