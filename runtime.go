package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// RunConfig is the cross-runtime invocation configuration.
type RunConfig struct {
	SessionID      string
	RunID          string
	Lineage        RunLineage
	EventSinks     []EventSink
	Extensions     map[string]any
	sessionIDSet   bool
	sessionIDError error
	runIDSet       bool
	runIDError     error
	lineageSet     bool
	lineageError   error
}

// RunOption mutates invocation configuration.
type RunOption interface {
	ApplyRunOption(*RunConfig)
}

type runOptionFunc func(*RunConfig)

func (f runOptionFunc) ApplyRunOption(cfg *RunConfig) {
	f(cfg)
}

// ResolveRunOptions materializes run options into one config.
func ResolveRunOptions(opts ...RunOption) RunConfig {
	var cfg RunConfig
	for _, opt := range opts {
		if opt != nil {
			opt.ApplyRunOption(&cfg)
		}
	}
	return cfg
}

// ErrRunConfig reports invalid per-call run configuration.
var ErrRunConfig = errors.New("gopact: invalid run config")

// RunConfigError returns core run config errors.
func (c RunConfig) RunConfigError() error {
	err := errors.Join(c.sessionIDError, c.runIDError, c.lineageError)
	if err == nil {
		return nil
	}
	return errors.Join(ErrRunConfig, err)
}

// ConstrainSessionID fixes the session identity from a runtime-owned option.
func (c *RunConfig) ConstrainSessionID(sessionID string) {
	if sessionID == "" {
		c.sessionIDError = errors.Join(c.sessionIDError, errors.New("gopact: session id is required"))
		return
	}
	if c.sessionIDSet && c.SessionID != sessionID {
		c.sessionIDError = errors.Join(c.sessionIDError, errors.New("gopact: conflicting session id"))
		return
	}
	c.SessionID = sessionID
	c.sessionIDSet = true
}

// WithSessionID fixes the session identity for one invocation.
func WithSessionID(sessionID string) RunOption {
	return runOptionFunc(func(cfg *RunConfig) {
		cfg.ConstrainSessionID(sessionID)
	})
}

// ConstrainRunID fixes the run identity from a runtime-owned option.
func (c *RunConfig) ConstrainRunID(runID string) {
	if runID == "" {
		c.runIDError = errors.Join(c.runIDError, errors.New("gopact: run id is required"))
		return
	}
	if c.runIDSet && c.RunID != runID {
		c.runIDError = errors.Join(c.runIDError, errors.New("gopact: conflicting run id"))
		return
	}
	c.RunID = runID
	c.runIDSet = true
}

// WithRunID fixes the run identity for one invocation.
func WithRunID(runID string) RunOption {
	return runOptionFunc(func(cfg *RunConfig) {
		cfg.ConstrainRunID(runID)
	})
}

// RunLineage identifies one nested run.
type RunLineage struct {
	ParentRunID string
	Depth       int
}

// ConstrainRunLineage fixes nested-run lineage from a runtime-owned option.
func (c *RunConfig) ConstrainRunLineage(lineage RunLineage) {
	if lineage.ParentRunID == "" || lineage.Depth <= 1 {
		c.lineageError = errors.Join(c.lineageError, errors.New("gopact: invalid run lineage"))
		return
	}
	if c.lineageSet && c.Lineage != lineage {
		c.lineageError = errors.Join(c.lineageError, errors.New("gopact: conflicting run lineage"))
		return
	}
	c.Lineage = lineage
	c.lineageSet = true
}

// WithRunLineage fixes nested-run lineage for one invocation.
func WithRunLineage(lineage RunLineage) RunOption {
	return runOptionFunc(func(cfg *RunConfig) {
		cfg.ConstrainRunLineage(lineage)
	})
}

// WithEventSink attaches a best-effort event sink to one invocation.
func WithEventSink(sink EventSink) RunOption {
	return runOptionFunc(func(cfg *RunConfig) {
		if sink != nil {
			cfg.EventSinks = append(cfg.EventSinks, sink)
		}
	})
}

// WithStrictEventSink attaches an event sink whose failure stops execution.
func WithStrictEventSink(sink EventSink) RunOption {
	return runOptionFunc(func(cfg *RunConfig) {
		if sink != nil {
			cfg.EventSinks = append(cfg.EventSinks, strictEventSink{EventSink: sink})
		}
	})
}

// WithEventHandler attaches a function event handler to one invocation.
func WithEventHandler(handler EventHandler) RunOption {
	return WithEventSink(EventSinkFunc(handler))
}

// WithStrictEventHandler attaches an event handler whose failure stops execution.
func WithStrictEventHandler(handler EventHandler) RunOption {
	return WithStrictEventSink(EventSinkFunc(handler))
}

// Event is the shared process event envelope.
type Event struct {
	DefinitionID         string
	DefinitionVersion    string
	SessionID            string
	RunID                string
	NodeID               string
	ActivationID         string
	AttemptID            string
	RevisionID           string
	ParentRunID          string
	NodeExecutionVersion int64
	ExecutionEpoch       int64
	SourceRevisionID     string
	Sequence             int64
	Type                 string
	Source               string
	Origin               string
	Timestamp            time.Time
	Summary              string
	Payload              json.RawMessage
	PayloadRef           string
}

// EventSink receives process events.
type EventSink interface {
	Emit(context.Context, Event) error
}

// EventHandler receives one process event.
type EventHandler func(context.Context, Event) error

// Emit implements EventSink.
func (h EventHandler) Emit(ctx context.Context, event Event) error {
	if h == nil {
		return nil
	}
	return h(ctx, event)
}

// EventSinkFunc adapts a function into an EventSink.
type EventSinkFunc func(context.Context, Event) error

// Emit implements EventSink.
func (f EventSinkFunc) Emit(ctx context.Context, event Event) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

type strictEventSink struct {
	EventSink
}

func (strictEventSink) StrictEventDelivery() {}

// IsStrictEventSink reports whether sink failures must stop execution.
func IsStrictEventSink(sink EventSink) bool {
	_, ok := sink.(interface{ StrictEventDelivery() })
	return ok
}
