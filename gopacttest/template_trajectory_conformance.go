package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gopact-ai/gopact"
)

var ErrTemplateTrajectoryConformanceFailed = errors.New("gopacttest: template trajectory conformance failed")

// TrajectoryFramePattern matches a compact event frame in a template trajectory.
type TrajectoryFramePattern struct {
	Type gopact.EventType
	Node string
	Step *int
}

// TemplateTrajectoryConformanceHarness describes one template trajectory under test.
type TemplateTrajectoryConformanceHarness struct {
	Name               string
	Events             []gopact.Event
	RunExport          *gopact.RunExport
	RequiredEventTypes []gopact.EventType
	RequiredFrames     []TrajectoryFramePattern
}

// TemplateTrajectoryConformanceResult is the observed result for one template trajectory contract case.
type TemplateTrajectoryConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckTemplateTrajectoryConformance runs reusable template trajectory contract cases.
func CheckTemplateTrajectoryConformance(ctx context.Context, harness TemplateTrajectoryConformanceHarness) []TemplateTrajectoryConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []TemplateTrajectoryConformanceResult{failedTemplateTrajectoryConformance("context", err)}
	}
	events := append([]gopact.Event(nil), harness.Events...)
	if len(events) == 0 && harness.RunExport != nil {
		events = append([]gopact.Event(nil), harness.RunExport.Events...)
	}

	return []TemplateTrajectoryConformanceResult{
		checkTemplateTrajectoryName(harness.Name),
		checkTemplateTrajectoryRunExport(harness.RunExport),
		checkTemplateTrajectoryHasEvents(events),
		checkTemplateTrajectoryRuntimeIdentity(events),
		checkTemplateTrajectoryTerminalEvent(events),
		checkTemplateTrajectoryRequiredEventTypes(events, harness.RequiredEventTypes),
		checkTemplateTrajectoryRequiredFrames(EventFrames(events), harness.RequiredFrames),
	}
}

// RequireTemplateTrajectoryConformance fails the test unless trajectory satisfies the template contract.
func RequireTemplateTrajectoryConformance(t testing.TB, harness TemplateTrajectoryConformanceHarness) {
	t.Helper()

	for _, result := range CheckTemplateTrajectoryConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("template trajectory conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkTemplateTrajectoryName(name string) TemplateTrajectoryConformanceResult {
	if name == "" {
		return failedTemplateTrajectoryConformance("has-name", errors.New("template name is empty"))
	}
	return passedTemplateTrajectoryConformance("has-name")
}

func checkTemplateTrajectoryRunExport(export *gopact.RunExport) TemplateTrajectoryConformanceResult {
	if export == nil {
		return passedTemplateTrajectoryConformance("valid-run-export")
	}
	if err := export.Validate(); err != nil {
		return failedTemplateTrajectoryConformance("valid-run-export", err)
	}
	return passedTemplateTrajectoryConformance("valid-run-export")
}

func checkTemplateTrajectoryHasEvents(events []gopact.Event) TemplateTrajectoryConformanceResult {
	if len(events) == 0 {
		return failedTemplateTrajectoryConformance("has-events", errors.New("trajectory has no events"))
	}
	return passedTemplateTrajectoryConformance("has-events")
}

func checkTemplateTrajectoryRuntimeIdentity(events []gopact.Event) TemplateTrajectoryConformanceResult {
	seen := map[string]string{}
	for i, event := range events {
		for _, field := range templateTrajectoryRuntimeIdentityFields(event.RuntimeIDs()) {
			if field.value == "" {
				continue
			}
			want, ok := seen[field.name]
			if !ok {
				seen[field.name] = field.value
				continue
			}
			if field.value != want {
				return failedTemplateTrajectoryConformance(
					"runtime-identity",
					fmt.Errorf("event %d %s = %q, want %q", i, field.name, field.value, want),
				)
			}
		}
	}
	return passedTemplateTrajectoryConformance("runtime-identity")
}

func checkTemplateTrajectoryTerminalEvent(events []gopact.Event) TemplateTrajectoryConformanceResult {
	for _, event := range events {
		if isTemplateTrajectoryTerminalEvent(event.Type) {
			return passedTemplateTrajectoryConformance("has-terminal-event")
		}
	}
	return failedTemplateTrajectoryConformance("has-terminal-event", errors.New("trajectory has no terminal run event"))
}

func checkTemplateTrajectoryRequiredEventTypes(events []gopact.Event, required []gopact.EventType) TemplateTrajectoryConformanceResult {
	if len(required) == 0 {
		return passedTemplateTrajectoryConformance("required-event-types")
	}
	next := 0
	for _, event := range events {
		if next < len(required) && event.Type == required[next] {
			next++
		}
	}
	if next != len(required) {
		return failedTemplateTrajectoryConformance("required-event-types", fmt.Errorf("matched %d/%d required event types", next, len(required)))
	}
	return passedTemplateTrajectoryConformance("required-event-types")
}

func checkTemplateTrajectoryRequiredFrames(frames []TrajectoryFrame, required []TrajectoryFramePattern) TemplateTrajectoryConformanceResult {
	if len(required) == 0 {
		return passedTemplateTrajectoryConformance("required-frames")
	}
	next := 0
	for _, frame := range frames {
		if next < len(required) && trajectoryFrameMatchesPattern(frame, required[next]) {
			next++
		}
	}
	if next != len(required) {
		return failedTemplateTrajectoryConformance("required-frames", fmt.Errorf("matched %d/%d required frames", next, len(required)))
	}
	return passedTemplateTrajectoryConformance("required-frames")
}

func trajectoryFrameMatchesPattern(frame TrajectoryFrame, pattern TrajectoryFramePattern) bool {
	if pattern.Type == "" || frame.Type != pattern.Type {
		return false
	}
	if pattern.Node != "" && frame.Node != pattern.Node {
		return false
	}
	if pattern.Step != nil && frame.Step != *pattern.Step {
		return false
	}
	return true
}

func templateTrajectoryRuntimeIdentityFields(ids gopact.RuntimeIDs) []struct {
	name  string
	value string
} {
	return []struct {
		name  string
		value string
	}{
		{name: "user_id", value: ids.UserID},
		{name: "session_id", value: ids.SessionID},
		{name: "thread_id", value: ids.ThreadID},
		{name: "run_id", value: ids.RunID},
		{name: "agent_id", value: ids.AgentID},
		{name: "app_id", value: ids.AppID},
		{name: "call_id", value: ids.CallID},
		{name: "parent_call_id", value: ids.ParentCallID},
		{name: "trace_id", value: ids.TraceID},
	}
}

func isTemplateTrajectoryTerminalEvent(eventType gopact.EventType) bool {
	switch eventType {
	case gopact.EventRunCompleted,
		gopact.EventRunFailed,
		gopact.EventRunCanceled,
		gopact.EventRunInterrupted:
		return true
	default:
		return false
	}
}

func passedTemplateTrajectoryConformance(name string) TemplateTrajectoryConformanceResult {
	return TemplateTrajectoryConformanceResult{Case: name, Passed: true}
}

func failedTemplateTrajectoryConformance(name string, err error) TemplateTrajectoryConformanceResult {
	return TemplateTrajectoryConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrTemplateTrajectoryConformanceFailed, err),
	}
}
