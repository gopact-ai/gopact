package gopact

import (
	"context"
	"errors"
	"fmt"
	"iter"
)

var (
	errNilRunner   = errors.New("gopact: runner is nil")
	errNilRunnable = errors.New("gopact: runnable is nil")
)

// Runnable is the minimal stream contract accepted by the root Runner facade.
type Runnable interface {
	Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error]
}

// Runner applies SDK defaults around a runnable stream.
type Runner struct {
	runnable         Runnable
	ids              RuntimeIDs
	eventMiddlewares []EventHandler
	pluginHost       *PluginHost
}

// RunnerOption configures a Runner instance.
type RunnerOption func(*Runner) error

// RunOption configures a single Runner call.
type RunOption func(*RunConfig)

// RunConfig is the resolved configuration for a single run call.
type RunConfig struct {
	IDs                 RuntimeIDs
	StepExport          *StepExport
	ResumeRequest       *ResumeRequest
	JSONSchemaValidator JSONSchemaValidator
}

// ResolveRunOptions returns the configuration described by run options.
func ResolveRunOptions(opts ...RunOption) RunConfig {
	cfg := RunConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// NewRunner creates a root SDK runner facade.
func NewRunner(runnable Runnable, opts ...RunnerOption) (*Runner, error) {
	if runnable == nil {
		return nil, errNilRunnable
	}
	runner := &Runner{runnable: runnable}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(runner); err != nil {
			return nil, err
		}
	}
	return runner, nil
}

// WithRunnerRuntimeIDs sets identity defaults for all calls through a Runner.
func WithRunnerRuntimeIDs(ids RuntimeIDs) RunnerOption {
	return func(runner *Runner) error {
		runner.ids = ids
		return nil
	}
}

// WithRunnerEventMiddleware wraps events emitted by this Runner.
func WithRunnerEventMiddleware(middlewares ...EventHandler) RunnerOption {
	return func(runner *Runner) error {
		for _, middleware := range middlewares {
			if middleware != nil {
				runner.eventMiddlewares = append(runner.eventMiddlewares, middleware)
			}
		}
		return nil
	}
}

// WithPluginHost attaches a plugin host to this Runner.
func WithPluginHost(host *PluginHost) RunnerOption {
	return func(runner *Runner) error {
		if host == nil {
			return errors.New("gopact: plugin host is nil")
		}
		runner.pluginHost = host
		return nil
	}
}

// WithRuntimeIDs sets identity values for one run call.
func WithRuntimeIDs(ids RuntimeIDs) RunOption {
	return func(cfg *RunConfig) {
		cfg.IDs = ids
	}
}

// WithStepExport imports a resumable step boundary for this run call.
func WithStepExport(export StepExport) RunOption {
	return func(cfg *RunConfig) {
		cfg.StepExport = &export
	}
}

// WithResumeRequest supplies external input for an interrupted step boundary.
func WithResumeRequest(request ResumeRequest) RunOption {
	return func(cfg *RunConfig) {
		cfg.ResumeRequest = &request
	}
}

// WithJSONSchemaValidator sets the validator used for schema-guarded resume
// boundaries in this run call.
func WithJSONSchemaValidator(validator JSONSchemaValidator) RunOption {
	return func(cfg *RunConfig) {
		cfg.JSONSchemaValidator = validator
	}
}

// Run streams events from the wrapped runnable with SDK identity defaults applied.
func (r *Runner) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if r == nil || r.runnable == nil {
			yield(Event{Type: EventRunFailed, Err: errNilRunner, CreatedAt: now()}, errNilRunner)
			return
		}

		cfg := ResolveRunOptions(opts...)
		contextIDs, _ := RuntimeIDsFromContext(ctx)
		ids := cfg.IDs.WithDefaults(r.ids).WithDefaults(contextIDs).WithDefaults(Defaults().RuntimeIDs)
		if r.pluginHost != nil {
			if err := r.pluginHost.beginRun(ctx); err != nil {
				event := Event{
					Type:      EventRunFailed,
					IDs:       ids,
					RunID:     ids.RunID,
					ThreadID:  ids.ThreadID,
					CreatedAt: now(),
					Err:       err,
				}.WithRuntimeDefaults(ids)
				yield(event, err)
				return
			}
			defer r.pluginHost.endRun()
		}
		middlewares := r.eventMiddlewareChain()

		runOpts := append([]RunOption(nil), opts...)
		runOpts = append(runOpts, WithRuntimeIDs(ids))
		for event, err := range r.runnable.Run(ctx, input, runOpts...) {
			event = event.WithRuntimeDefaults(ids)
			if err != nil {
				event.Err = err
			}
			if !r.emitEvent(ctx, yield, event, err, middlewares) {
				return
			}
		}
	}
}

// Close releases runner-level plugin resources.
func (r *Runner) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if r == nil {
		return errNilRunner
	}
	if r.pluginHost == nil {
		return nil
	}
	return r.pluginHost.Close(ctx)
}

func (r *Runner) eventMiddlewareChain() []EventHandler {
	var middlewares []EventHandler
	middlewares = append(middlewares, r.eventMiddlewares...)
	if r.pluginHost != nil {
		middlewares = append(middlewares, r.pluginHost.EventMiddlewares()...)
	}
	return middlewares
}

func (r *Runner) emitEvent(ctx context.Context, yield func(Event, error) bool, event Event, streamErr error, middlewares []EventHandler) bool {
	emitted := false
	keepGoing := true

	final := func(c *EventContext) error {
		if r.pluginHost != nil {
			event, err := r.pluginHost.publish(c.Context, c.Event)
			if err != nil {
				return fmt.Errorf("gopact: publish event: %w", err)
			}
			c.Event = event
		}
		emitted = true
		if !yield(c.Event, streamErr) {
			keepGoing = false
		}
		return nil
	}

	handler := ComposeEventHandler(final, middlewares...)
	if err := handler(NewEventContext(ctx, event)); err != nil {
		failed := Event{
			Type:      EventRunFailed,
			IDs:       event.RuntimeIDs(),
			RunID:     event.RuntimeIDs().RunID,
			ThreadID:  event.RuntimeIDs().ThreadID,
			CreatedAt: now(),
			Err:       err,
		}.WithRuntimeDefaults(event.RuntimeIDs())
		yield(failed, err)
		return false
	}

	if streamErr != nil {
		if !emitted {
			yield(event, streamErr)
		}
		return false
	}
	return keepGoing
}
