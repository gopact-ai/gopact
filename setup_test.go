package gopact

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDefaultsUseWarningLevelLogger(t *testing.T) {
	resetDefaultsForTest(t)

	defaults := Defaults()
	if defaults.LogLevel != LevelWarn {
		t.Fatalf("Defaults().LogLevel = %v, want %v", defaults.LogLevel, LevelWarn)
	}

	var buf bytes.Buffer
	defaults.Logger = NewTextLogger(&buf)
	defaults.Log(context.Background(), LevelInfo, "ignored")
	defaults.Log(context.Background(), LevelWarn, "visible", String("key", "value"))

	got := buf.String()
	if strings.Contains(got, "ignored") {
		t.Fatalf("default logger emitted info log: %q", got)
	}
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "visible") || !strings.Contains(got, "key=value") {
		t.Fatalf("warn log output = %q", got)
	}
}

func TestSetupOverridesLoggerLevelAndRuntimeIDs(t *testing.T) {
	resetDefaultsForTest(t)

	logger := &recordingLogger{}
	err := Setup(
		WithLogger(logger),
		WithLogLevel(LevelDebug),
		WithDefaultRuntimeIDs(RuntimeIDs{AppID: "app-1", AgentID: "agent-1"}),
	)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	defaults := Defaults()
	if defaults.Logger != logger {
		t.Fatal("Setup() did not install custom logger")
	}
	if defaults.LogLevel != LevelDebug {
		t.Fatalf("Defaults().LogLevel = %v, want debug", defaults.LogLevel)
	}
	if defaults.RuntimeIDs.AppID != "app-1" || defaults.RuntimeIDs.AgentID != "agent-1" {
		t.Fatalf("Defaults().RuntimeIDs = %+v", defaults.RuntimeIDs)
	}
}

func TestSetupRejectsInvalidOptions(t *testing.T) {
	resetDefaultsForTest(t)

	tests := []struct {
		name string
		opts []SetupOption
	}{
		{name: "nil logger", opts: []SetupOption{WithLogger(nil)}},
		{name: "unknown level", opts: []SetupOption{WithLogLevel(LevelUnknown)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Setup(tt.opts...); err == nil {
				t.Fatal("Setup() error = nil, want validation error")
			}
		})
	}
}

type recordingLogger struct {
	entries []recordedLog
}

type recordedLog struct {
	level LogLevel
	msg   string
	attrs []Attr
}

func (r *recordingLogger) Log(ctx context.Context, level LogLevel, msg string, attrs ...Attr) {
	r.entries = append(r.entries, recordedLog{
		level: level,
		msg:   msg,
		attrs: append([]Attr(nil), attrs...),
	})
}
