package gopact

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// LogLevel controls SDK log filtering.
type LogLevel int

const (
	LevelUnknown LogLevel = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the stable uppercase level name.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

func (l LogLevel) valid() bool {
	return l >= LevelDebug && l <= LevelError
}

// Attr is a structured logger attribute.
type Attr struct {
	Key   string
	Value any
}

// String creates a string logger attribute.
func String(key, value string) Attr {
	return Attr{Key: key, Value: value}
}

// Any creates a generic logger attribute.
func Any(key string, value any) Attr {
	return Attr{Key: key, Value: value}
}

// Logger is the SDK logging interface.
type Logger interface {
	Log(ctx context.Context, level LogLevel, msg string, attrs ...Attr)
}

type textLogger struct {
	mu sync.Mutex
	w  io.Writer
}

// NewTextLogger creates a deterministic, minimal text logger.
func NewTextLogger(w io.Writer) Logger {
	if w == nil {
		w = io.Discard
	}
	return &textLogger{w: w}
}

func (l *textLogger) Log(_ context.Context, level LogLevel, msg string, attrs ...Attr) {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, _ = fmt.Fprintf(l.w, "level=%s msg=%q", level.String(), msg)
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		_, _ = fmt.Fprintf(l.w, " %s=%v", attr.Key, attr.Value)
	}
	_, _ = fmt.Fprintln(l.w)
}
