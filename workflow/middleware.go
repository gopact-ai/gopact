package workflow

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// NodeNext executes the wrapped node body.
type NodeNext[I, O any] func() error

// NodeMiddleware wraps a node boundary.
type NodeMiddleware[I, O any] func(*NodeContext[I, O], NodeNext[I, O]) error

// RouteContext is the mutable route middleware context.
type RouteContext[I, O any] struct {
	ctx      context.Context
	NodeName string
	Output   O
	Dispatch Dispatch
}

// Context returns the current run context.
func (c *RouteContext[I, O]) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// RouteMiddleware rewrites a route dispatch.
type RouteMiddleware[I, O any] func(*RouteContext[I, O]) error

// JoinContext is the mutable join middleware context.
type JoinContext[I any] struct {
	ctx      context.Context
	NodeName string
	Inputs   Inputs
	Input    I
}

// Context returns the current run context.
func (c *JoinContext[I]) Context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// JoinMiddleware rewrites a joined target input.
type JoinMiddleware[I any] func(*JoinContext[I]) error

type erasedNodeMiddleware struct {
	name string
	run  func(context.Context, runtimeNode, any, []gopact.RunOption, nodeInvoker) (any, bool, error)
}

type nodeInvoker func(context.Context, any, ...gopact.RunOption) (any, error)

type erasedRouteMiddleware struct {
	name string
	run  func(context.Context, runtimeNode, string, any, Dispatch) (Dispatch, bool, error)
}

type erasedJoinMiddleware struct {
	name string
	run  func(context.Context, runtimeNode, string, Inputs, any) (any, bool, error)
}

func eraseNodeMiddleware[I, O any](name string, mw NodeMiddleware[I, O]) erasedNodeMiddleware {
	return erasedNodeMiddleware{name: name, run: func(ctx context.Context, node runtimeNode, input any, opts []gopact.RunOption, next nodeInvoker) (any, bool, error) {
		if node.inputType() != typeOf[I]() || node.outputType() != typeOf[O]() {
			return nil, false, nil
		}
		typedInput, ok := input.(I)
		if !ok {
			return nil, true, fmt.Errorf(
				"workflow: middleware %q input type mismatch: got %T, want %s",
				name,
				input,
				typeOf[I](),
			)
		}
		middlewareCtx := NodeContext[I, O]{ctx: ctx, Input: typedInput}
		called := false
		err := invokeCallbackError(fmt.Sprintf("node middleware %q", name), func() error {
			return mw(&middlewareCtx, func() error {
				if called {
					return fmt.Errorf("workflow: node middleware %q called next more than once", name)
				}
				called = true
				output, err := next(ctx, middlewareCtx.Input, opts...)
				if err != nil {
					return err
				}
				typedOutput, ok := output.(O)
				if !ok {
					return fmt.Errorf(
						"workflow: middleware %q output type mismatch: got %T, want %s",
						name,
						output,
						typeOf[O](),
					)
				}
				middlewareCtx.Output = typedOutput
				return nil
			})
		})
		if err != nil {
			return nil, true, err
		}
		if !called {
			return nil, true, fmt.Errorf("workflow: node middleware %q did not call next", name)
		}
		return middlewareCtx.Output, true, nil
	}}
}

func eraseRouteMiddleware[I, O any](name string, mw RouteMiddleware[I, O]) erasedRouteMiddleware {
	return erasedRouteMiddleware{name: name, run: func(ctx context.Context, node runtimeNode, nodeName string, output any, dispatch Dispatch) (Dispatch, bool, error) {
		if node.outputType() != typeOf[O]() {
			return Dispatch{}, false, nil
		}
		typedOutput, ok := output.(O)
		if !ok {
			return Dispatch{}, true, fmt.Errorf(
				"workflow: route middleware %q output type mismatch: got %T, want %s",
				name,
				output,
				typeOf[O](),
			)
		}
		middlewareCtx := RouteContext[I, O]{ctx: ctx, NodeName: nodeName, Output: typedOutput, Dispatch: dispatch}
		if err := invokeCallbackError(fmt.Sprintf("route middleware %q", name), func() error {
			return mw(&middlewareCtx)
		}); err != nil {
			return Dispatch{}, true, err
		}
		return middlewareCtx.Dispatch, true, nil
	}}
}

func eraseJoinMiddleware[I any](name string, mw JoinMiddleware[I]) erasedJoinMiddleware {
	return erasedJoinMiddleware{name: name, run: func(ctx context.Context, node runtimeNode, nodeName string, inputs Inputs, input any) (any, bool, error) {
		if node.inputType() != typeOf[I]() {
			return nil, false, nil
		}
		typedInput, ok := input.(I)
		if !ok {
			return nil, true, fmt.Errorf(
				"workflow: join middleware %q input type mismatch: got %T, want %s",
				name,
				input,
				typeOf[I](),
			)
		}
		middlewareCtx := JoinContext[I]{ctx: ctx, NodeName: nodeName, Inputs: inputs, Input: typedInput}
		if err := invokeCallbackError(fmt.Sprintf("join middleware %q", name), func() error {
			return mw(&middlewareCtx)
		}); err != nil {
			return nil, true, err
		}
		return middlewareCtx.Input, true, nil
	}}
}
