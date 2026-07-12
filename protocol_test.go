package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestInvokableFunc(t *testing.T) {
	inv := InvokableFunc[string, int](func(_ context.Context, input string, _ ...RunOption) (int, error) {
		return len(input), nil
	})
	got, err := inv.Invoke(context.Background(), "core")
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got != 4 {
		t.Fatalf("Invoke() = %d, want 4", got)
	}
}

func TestEventHandlerIsBestEffortByDefault(t *testing.T) {
	cfg := ResolveRunOptions(WithEventHandler(func(context.Context, Event) error { return nil }))
	if IsStrictEventSink(cfg.EventSinks[0]) {
		t.Fatal("IsStrictEventSink() = true, want false")
	}
}

func TestStrictEventHandlerReturnsFailure(t *testing.T) {
	sinkErr := errors.New("sink failed")
	cfg := ResolveRunOptions(WithStrictEventHandler(func(context.Context, Event) error {
		return sinkErr
	}))
	if !IsStrictEventSink(cfg.EventSinks[0]) {
		t.Fatal("IsStrictEventSink() = false, want true")
	}
	if err := cfg.EventSinks[0].Emit(context.Background(), Event{}); !errors.Is(err, sinkErr) {
		t.Fatalf("Emit() error = %v, want %v", err, sinkErr)
	}
}

func TestEventHandlerSink(t *testing.T) {
	var got Event
	cfg := ResolveRunOptions(
		WithRunID("run-1"),
		WithEventHandler(func(_ context.Context, event Event) error {
			got = event
			return nil
		}),
	)
	if err := cfg.EventSinks[0].Emit(context.Background(), Event{RunID: cfg.RunID, Type: "run.started"}); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	if got.Type != "run.started" || got.RunID != "run-1" {
		t.Fatalf("event = %+v, want run started with runtime id", got)
	}
}

func TestWithSessionIDConstrainsOneIdentity(t *testing.T) {
	config := ResolveRunOptions(WithSessionID("session-1"), WithSessionID("session-1"))
	if err := config.RunConfigError(); err != nil {
		t.Fatal(err)
	}
	if config.SessionID != "session-1" {
		t.Fatalf("session id = %q, want session-1", config.SessionID)
	}
}

func TestWithSessionIDRejectsEmptyAndConflict(t *testing.T) {
	for _, options := range [][]RunOption{
		{WithSessionID("")},
		{WithSessionID("session-1"), WithSessionID("session-2")},
	} {
		err := ResolveRunOptions(options...).RunConfigError()
		if !errors.Is(err, ErrRunConfig) {
			t.Fatalf("error = %v, want ErrRunConfig", err)
		}
	}
}

func TestWithRunIDRejectsConflict(t *testing.T) {
	cfg := ResolveRunOptions(WithRunID("run-1"), WithRunID("run-2"))
	if cfg.RunConfigError() == nil {
		t.Fatal("RunConfigError() = nil, want conflict")
	}
}

func TestWithRunLineageRejectsConflict(t *testing.T) {
	first := RunLineage{ParentRunID: "parent-1", Depth: 2}
	second := RunLineage{ParentRunID: "parent-2", Depth: 2}
	cfg := ResolveRunOptions(WithRunLineage(first), WithRunLineage(second))
	if cfg.RunConfigError() == nil {
		t.Fatal("RunConfigError() = nil, want lineage conflict")
	}
}

func TestWithRunLineageAcceptsRepeatedIdentity(t *testing.T) {
	lineage := RunLineage{ParentRunID: "parent-1", Depth: 2}
	cfg := ResolveRunOptions(WithRunLineage(lineage), WithRunLineage(lineage))
	if err := cfg.RunConfigError(); err != nil {
		t.Fatal(err)
	}
	if cfg.Lineage != lineage {
		t.Fatalf("lineage = %+v, want %+v", cfg.Lineage, lineage)
	}
}

func TestWithRunLineageRejectsInvalidIdentity(t *testing.T) {
	for _, lineage := range []RunLineage{
		{Depth: 2},
		{ParentRunID: "parent-1", Depth: 1},
	} {
		if err := ResolveRunOptions(WithRunLineage(lineage)).RunConfigError(); !errors.Is(err, ErrRunConfig) {
			t.Fatalf("lineage %+v error = %v, want ErrRunConfig", lineage, err)
		}
	}
}

func TestModelRequestCopyOnWriteMethods(t *testing.T) {
	req := ModelRequest{}
	next := req.WithTemperature(0.2).WithTools(ToolSpec{Name: "search"})
	if req.Temperature != nil || len(req.Tools) != 0 {
		t.Fatalf("original request mutated: %+v", req)
	}
	if next.Temperature == nil || *next.Temperature != 0.2 || len(next.Tools) != 1 {
		t.Fatalf("next request = %+v, want temperature and tool", next)
	}
}

func TestToolOutcomeVariants(t *testing.T) {
	cases := []struct {
		outcome ToolOutcome
		callID  string
		name    string
	}{
		{outcome: ToolResultOutcome{CallID: "c1", Name: "search"}, callID: "c1", name: "search"},
		{outcome: ToolRejectedOutcome{CallID: "c2", Name: "write"}, callID: "c2", name: "write"},
		{outcome: ToolErrorOutcome{CallID: "c3", Name: "shell"}, callID: "c3", name: "shell"},
		{outcome: ToolInterruptOutcome{CallID: "c4", Name: "deploy"}, callID: "c4", name: "deploy"},
	}
	for _, tc := range cases {
		if tc.outcome.ToolCallID() != tc.callID || tc.outcome.ToolName() != tc.name {
			t.Fatalf("outcome = %#v, want call %q and tool %q", tc.outcome, tc.callID, tc.name)
		}
	}
	interrupt := ToolInterruptOutcome{
		CallID: "c5",
		Name:   "reviewer",
		Interrupt: ToolInterrupt{
			InterruptID:  "approval-1",
			RunID:        "child-1",
			CheckpointID: "checkpoint-1",
		},
	}
	if interrupt.Interrupt.RunID != "child-1" || interrupt.Interrupt.CheckpointID != "checkpoint-1" {
		t.Fatalf("interrupt = %+v, want child association", interrupt)
	}
}
