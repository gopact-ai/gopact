package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestWorkflowCallbackPanicsFailRun(t *testing.T) {
	targets := []string{
		"node body",
		"guard",
		"node middleware",
		"after node",
		"route",
		"route middleware",
		"join",
		"join middleware",
		"after workflow",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			store := &recordingCheckpointer{}
			cause := fmt.Errorf("panic from %s", target)
			compiled := compileCallbackPanicWorkflow(t, store, target, cause)
			var events []string

			err, recovered := invokeCallbackPanicWorkflow(compiled, func(event gopact.Event) {
				events = append(events, event.Type)
			})

			if recovered != nil {
				t.Fatalf("Invoke() panic = %v, want error", recovered)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("Invoke() error = %v, want panic cause %v", err, cause)
			}
			if len(events) == 0 || events[len(events)-1] != EventWorkflowFailed {
				t.Fatalf("events = %v, want final %q", events, EventWorkflowFailed)
			}
			if len(store.finished) == 0 || store.finished[len(store.finished)-1].Status != CheckpointFailed {
				t.Fatalf("finished checkpoints = %+v, want final status %q", store.finished, CheckpointFailed)
			}
		})
	}
}

func TestNodeMiddlewareCannotSwallowDownstreamPanic(t *testing.T) {
	store := &recordingCheckpointer{}
	cause := errors.New("node body panic")
	wf := New[int, int](
		"middleware-panic",
		WithStore(storeWithCheckpointer(store)),
		WithPlugins(eventTypePlugin{
			name: "middleware-panic",
			register: func(registry *Registry) error {
				if err := registry.RegisterNodeMiddleware[int, int](
					"outer-fallback",
					func(ctx *NodeContext[int, int], next NodeNext[int, int]) error {
						if err := next(); err != nil {
							ctx.Output = 42
							return nil
						}
						return nil
					},
				); err != nil {
					return err
				}
				return registry.RegisterNodeMiddleware[int, int](
					"inner",
					func(_ *NodeContext[int, int], next NodeNext[int, int]) error {
						return next()
					},
				)
			},
		}),
	)
	plan := testNode(wf, "plan", func(context.Context, int) (int, error) {
		panic(cause)
	})
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var events []string

	err, recovered := invokeCallbackPanicWorkflow(compiled, func(event gopact.Event) {
		events = append(events, event.Type)
	})

	if recovered != nil {
		t.Fatalf("Invoke() panic = %v, want error", recovered)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("Invoke() error = %v, want panic cause %v", err, cause)
	}
	if len(events) == 0 || events[len(events)-1] != EventWorkflowFailed {
		t.Fatalf("events = %v, want final %q", events, EventWorkflowFailed)
	}
	if len(store.finished) == 0 || store.finished[len(store.finished)-1].Status != CheckpointFailed {
		t.Fatalf("finished checkpoints = %+v, want final status %q", store.finished, CheckpointFailed)
	}
}

func TestWorkflowNonErrorCallbackPanicFailsRun(t *testing.T) {
	store := &recordingCheckpointer{}
	const cause = "route panic value"
	compiled := compileCallbackPanicWorkflow(t, store, "route", cause)
	var events []string

	err, recovered := invokeCallbackPanicWorkflow(compiled, func(event gopact.Event) {
		events = append(events, event.Type)
	})

	if recovered != nil {
		t.Fatalf("Invoke() panic = %v, want error", recovered)
	}
	if err == nil || !strings.Contains(err.Error(), cause) {
		t.Fatalf("Invoke() error = %v, want panic value %q", err, cause)
	}
	if len(events) == 0 || events[len(events)-1] != EventWorkflowFailed {
		t.Fatalf("events = %v, want final %q", events, EventWorkflowFailed)
	}
	if len(store.finished) == 0 || store.finished[len(store.finished)-1].Status != CheckpointFailed {
		t.Fatalf("finished checkpoints = %+v, want final status %q", store.finished, CheckpointFailed)
	}
}

func TestWorkflowStartupCallbackPanicsReturnErrorsWithoutTerminalFact(t *testing.T) {
	for _, target := range []string{"before workflow", "context initializer"} {
		t.Run(target, func(t *testing.T) {
			store := &recordingCheckpointer{}
			cause := fmt.Errorf("panic from %s", target)
			wf := New[int, int]("startup-callback-panic", WithStore(storeWithCheckpointer(store)))
			if target == "before workflow" {
				wf.BeforeWorkflow(Hook("before", func(*WorkflowContext[int, int]) error {
					panic(cause)
				}))
			} else {
				wf.Context(func(int) int {
					panic(cause)
				})
			}
			plan := testNode(wf, "plan", func(_ context.Context, input int) (int, error) {
				return input, nil
			})
			wf.Entry(plan)
			wf.Exit(plan)
			compiled, err := wf.compile()
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			var events []string

			err, recovered := invokeCallbackPanicWorkflow(compiled, func(event gopact.Event) {
				events = append(events, event.Type)
			})

			if recovered != nil {
				t.Fatalf("Invoke() panic = %v, want error", recovered)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("Invoke() error = %v, want panic cause %v", err, cause)
			}
			for _, event := range events {
				if event == EventWorkflowStarted || event == EventWorkflowFailed {
					t.Fatalf("events = %v, want no run terminal before workflow start", events)
				}
			}
			if len(store.finished) != 0 {
				t.Fatalf("finished checkpoints = %+v, want none before workflow start", store.finished)
			}
		})
	}
}

func compileCallbackPanicWorkflow(t *testing.T, store *recordingCheckpointer, target string, cause any) *compiled[int, int] {
	t.Helper()
	panicAt := func(name string) {
		if target == name {
			panic(cause)
		}
	}
	wf := New[int, int](
		"callback-panic",
		WithStore(storeWithCheckpointer(store)),
		WithPlugins(eventTypePlugin{
			name: "callback-panic-middleware",
			register: func(registry *Registry) error {
				if err := registry.RegisterNodeMiddleware[int, int](
					"node",
					func(_ *NodeContext[int, int], next NodeNext[int, int]) error {
						panicAt("node middleware")
						return next()
					},
				); err != nil {
					return err
				}
				if err := registry.RegisterRouteMiddleware[int, int](
					"route",
					func(*RouteContext[int, int]) error {
						panicAt("route middleware")
						return nil
					},
				); err != nil {
					return err
				}
				return registry.RegisterJoinMiddleware[int](
					"join",
					func(*JoinContext[int]) error {
						panicAt("join middleware")
						return nil
					},
				)
			},
		}),
	)
	plan := testNode(wf, "plan", func(_ context.Context, input int) (int, error) {
		panicAt("node body")
		return input, nil
	})
	report := testNode(wf, "report", func(_ context.Context, input int) (int, error) {
		return input, nil
	})
	plan.Guard(BeforeRun("guard", GuardFunc[int, int](
		func(context.Context, GuardContext[int, int]) (GuardDecision[int, int], error) {
			panicAt("guard")
			return GuardAllow[int, int]{}, nil
		},
	)))
	report.After(Hook("after", func(*NodeContext[int, int]) error {
		panicAt("after node")
		return nil
	}))
	plan.Route(func(_ context.Context, _ int) (Dispatch, error) {
		panicAt("route")
		return plan.To(report), nil
	})
	report.Join(func(_ context.Context, inputs Inputs) (int, error) {
		panicAt("join")
		return inputs.One(plan)
	})
	wf.AfterWorkflow(Hook("after", func(*WorkflowContext[int, int]) error {
		panicAt("after workflow")
		return nil
	}))
	wf.Entry(plan)
	wf.Edge(plan, report)
	wf.Exit(report)

	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func invokeCallbackPanicWorkflow(compiled *compiled[int, int], observe func(gopact.Event)) (err error, recovered any) {
	defer func() {
		recovered = recover()
	}()
	_, err = compiled.Invoke(
		context.Background(),
		1,
		gopact.WithRunID("callback-panic-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			observe(event)
			return nil
		}),
	)
	return err, nil
}
