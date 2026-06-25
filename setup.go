package gopact

import (
	"context"
	"errors"
	"sync/atomic"
)

// DefaultsSnapshot is an immutable copy of SDK-wide defaults.
type DefaultsSnapshot struct {
	Logger     Logger
	LogLevel   LogLevel
	RuntimeIDs RuntimeIDs
}

// Log emits a log line after applying the configured minimum level.
func (d DefaultsSnapshot) Log(ctx context.Context, level LogLevel, msg string, attrs ...Attr) {
	if d.Logger == nil || !level.valid() || level < d.LogLevel {
		return
	}
	d.Logger.Log(ctx, level, msg, attrs...)
}

// SetupOption mutates SDK-wide defaults.
type SetupOption func(*DefaultsSnapshot) error

var globalDefaults atomic.Pointer[DefaultsSnapshot]

// Defaults returns a copy of current SDK defaults.
func Defaults() DefaultsSnapshot {
	if defaults := globalDefaults.Load(); defaults != nil {
		return *defaults
	}
	return builtInDefaults()
}

// Setup installs SDK-wide defaults used when call-level options do not override them.
func Setup(opts ...SetupOption) error {
	next := Defaults()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&next); err != nil {
			return err
		}
	}
	if next.Logger == nil {
		return errors.New("gopact: logger is required")
	}
	if !next.LogLevel.valid() {
		return errors.New("gopact: log level is invalid")
	}
	globalDefaults.Store(&next)
	return nil
}

// WithLogger sets the SDK-wide logger.
func WithLogger(logger Logger) SetupOption {
	return func(defaults *DefaultsSnapshot) error {
		if logger == nil {
			return errors.New("gopact: logger is nil")
		}
		defaults.Logger = logger
		return nil
	}
}

// WithLogLevel sets the SDK-wide minimum log level.
func WithLogLevel(level LogLevel) SetupOption {
	return func(defaults *DefaultsSnapshot) error {
		if !level.valid() {
			return errors.New("gopact: log level is invalid")
		}
		defaults.LogLevel = level
		return nil
	}
}

// WithDefaultRuntimeIDs sets SDK-wide runtime identity defaults.
func WithDefaultRuntimeIDs(ids RuntimeIDs) SetupOption {
	return func(defaults *DefaultsSnapshot) error {
		defaults.RuntimeIDs = ids
		return nil
	}
}

func builtInDefaults() DefaultsSnapshot {
	return DefaultsSnapshot{
		Logger:   NewTextLogger(nil),
		LogLevel: LevelWarn,
	}
}
