package gopacttest

import (
	"context"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckTemplateTrajectoryConformancePassesTrajectory(t *testing.T) {
	step := 1
	harness := TemplateTrajectoryConformanceHarness{
		Name: "react",
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
			{Type: gopact.EventNodeStarted, Node: "call_model", Step: 1},
			{Type: gopact.EventNodeCompleted, Node: "call_model", Step: 1},
			{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		},
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventNodeCompleted,
			gopact.EventRunCompleted,
		},
		RequiredFrames: []TrajectoryFramePattern{
			{Type: gopact.EventNodeCompleted, Node: "call_model", Step: &step},
		},
	}

	results := CheckTemplateTrajectoryConformance(context.Background(), harness)
	if failed := failedTemplateTrajectoryConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckTemplateTrajectoryConformance() failed cases: %v", failed)
	}
	RequireTemplateTrajectoryConformance(t, harness)
}

func TestCheckTemplateTrajectoryConformanceReportsMissingTerminalEvent(t *testing.T) {
	harness := TemplateTrajectoryConformanceHarness{
		Name: "react",
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
			{Type: gopact.EventNodeCompleted, Node: "call_model", Step: 1},
		},
	}

	results := CheckTemplateTrajectoryConformance(context.Background(), harness)
	if !hasFailedTemplateTrajectoryConformanceCase(results, "has-terminal-event") {
		t.Fatalf("CheckTemplateTrajectoryConformance() did not report missing terminal event: %+v", results)
	}
}

func TestCheckTemplateTrajectoryConformanceReportsMissingRequiredFrame(t *testing.T) {
	step := 1
	harness := TemplateTrajectoryConformanceHarness{
		Name: "react",
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
			{Type: gopact.EventNodeCompleted, Node: "call_tool", Step: 1},
			{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		},
		RequiredFrames: []TrajectoryFramePattern{
			{Type: gopact.EventNodeCompleted, Node: "call_model", Step: &step},
		},
	}

	results := CheckTemplateTrajectoryConformance(context.Background(), harness)
	if !hasFailedTemplateTrajectoryConformanceCase(results, "required-frames") {
		t.Fatalf("CheckTemplateTrajectoryConformance() did not report missing required frame: %+v", results)
	}
}

func TestCheckTemplateTrajectoryConformancePassesRequiredFrameMetadata(t *testing.T) {
	step := 3
	harness := TemplateTrajectoryConformanceHarness{
		Name: "devagent",
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
			{
				Type: gopact.EventNodeCompleted,
				Node: "devagent.release_gate",
				Step: 3,
				Metadata: map[string]any{
					"action": "release",
					"mode":   "write",
				},
			},
			{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		},
		RequiredFrames: []TrajectoryFramePattern{
			{
				Type: gopact.EventNodeCompleted,
				Node: "devagent.release_gate",
				Step: &step,
				Metadata: map[string]any{
					"action": "release",
				},
			},
		},
	}

	results := CheckTemplateTrajectoryConformance(context.Background(), harness)
	if failed := failedTemplateTrajectoryConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckTemplateTrajectoryConformance() failed cases: %v", failed)
	}
}

func TestCheckTemplateTrajectoryConformanceReportsRequiredFrameMetadataDrift(t *testing.T) {
	step := 3
	harness := TemplateTrajectoryConformanceHarness{
		Name: "devagent",
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
			{
				Type: gopact.EventNodeCompleted,
				Node: "devagent.release_gate",
				Step: 3,
				Metadata: map[string]any{
					"action": "plan",
				},
			},
			{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		},
		RequiredFrames: []TrajectoryFramePattern{
			{
				Type: gopact.EventNodeCompleted,
				Node: "devagent.release_gate",
				Step: &step,
				Metadata: map[string]any{
					"action": "release",
				},
			},
		},
	}

	results := CheckTemplateTrajectoryConformance(context.Background(), harness)
	if !hasFailedTemplateTrajectoryConformanceCase(results, "required-frames") {
		t.Fatalf("CheckTemplateTrajectoryConformance() did not report required frame metadata drift: %+v", results)
	}
}

func TestCheckTemplateTrajectoryConformanceReportsMixedRuntimeIdentity(t *testing.T) {
	harness := TemplateTrajectoryConformanceHarness{
		Name: "react",
		Events: []gopact.Event{
			{
				Type: gopact.EventRunStarted,
				IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			},
			{
				Type: gopact.EventNodeCompleted,
				Node: "call_model",
				Step: 1,
				IDs:  gopact.RuntimeIDs{RunID: "run-2", ThreadID: "thread-1"},
			},
			{
				Type: gopact.EventRunCompleted,
				IDs:  gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
			},
		},
	}

	results := CheckTemplateTrajectoryConformance(context.Background(), harness)
	if !hasFailedTemplateTrajectoryConformanceCase(results, "runtime-identity") {
		t.Fatalf("CheckTemplateTrajectoryConformance() did not report mixed runtime identity: %+v", results)
	}
}

func failedTemplateTrajectoryConformanceCases(results []TemplateTrajectoryConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedTemplateTrajectoryConformanceCase(results []TemplateTrajectoryConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}
