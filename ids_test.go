package gopact

import "testing"

func TestRuntimeIDsWithDefaultsPreservesProvidedValues(t *testing.T) {
	ids := RuntimeIDs{
		UserID:       "user-1",
		SessionID:    "session-1",
		ThreadID:     "thread-1",
		RunID:        "run-1",
		AgentID:      "agent-1",
		AppID:        "app-1",
		CallID:       "call-1",
		ParentCallID: "parent-call-1",
		TraceID:      "trace-1",
	}

	got := ids.WithDefaults(RuntimeIDs{
		UserID:       "default-user",
		SessionID:    "default-session",
		ThreadID:     "default-thread",
		RunID:        "default-run",
		AgentID:      "default-agent",
		AppID:        "default-app",
		CallID:       "default-call",
		ParentCallID: "default-parent",
		TraceID:      "default-trace",
	})

	if got != ids {
		t.Fatalf("WithDefaults() = %+v, want provided ids unchanged", got)
	}
}

func TestRuntimeIDsWithDefaultsFillsMissingValues(t *testing.T) {
	got := (RuntimeIDs{ThreadID: "thread-1"}).WithDefaults(RuntimeIDs{
		UserID:    "user-1",
		SessionID: "session-1",
		ThreadID:  "default-thread",
		RunID:     "run-1",
		AgentID:   "agent-1",
	})

	if got.UserID != "user-1" || got.SessionID != "session-1" || got.ThreadID != "thread-1" || got.RunID != "run-1" || got.AgentID != "agent-1" {
		t.Fatalf("WithDefaults() = %+v", got)
	}
}

func TestRuntimeIDsIsZero(t *testing.T) {
	if !(RuntimeIDs{}).IsZero() {
		t.Fatal("empty RuntimeIDs should be zero")
	}

	if (RuntimeIDs{RunID: "run-1"}).IsZero() {
		t.Fatal("RuntimeIDs with RunID should not be zero")
	}
}
