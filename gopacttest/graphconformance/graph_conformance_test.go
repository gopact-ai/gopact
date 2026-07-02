package graphconformance

import "testing"

func TestRequireGraphConformancePasses(t *testing.T) {
	RequireGraphConformance(t)
}

func TestCheckGraphConformanceCoversSelfBootstrapCases(t *testing.T) {
	results := CheckGraphConformance(t.Context())
	got := map[string]bool{}
	for _, result := range results {
		got[result.Case] = true
	}
	for _, want := range []string{
		"branch-routes-selected-target",
		"branch-routes-multiple-targets",
		"branch-can-end-with-no-targets",
		"branch-rejects-missing-target",
		"branch-resume-uses-checkpoint-queue",
		"dag-fan-in-runs-join-after-parents",
		"dag-fan-in-stops-when-parent-fails",
		"dag-fan-in-preserves-edge-order",
		"dynamic-fan-out-resumes-incomplete-targets",
		"dynamic-fan-out-runs-all-targets",
		"dynamic-fan-out-empty-completes",
		"dynamic-fan-out-stops-on-target-failure",
		"loop-branch-exits",
		"loop-step-limit-fails",
		"runnable-node-runs-subgraph",
		"runnable-node-streams-nested-events",
		"node-emits-nested-events",
		"runnable-node-inherits-runtime-ids",
		"runnable-node-checkpoint-inheritance-isolation",
		"failed-node-stops-successors",
		"canceled-node-stops-successors",
	} {
		if !got[want] {
			t.Fatalf("conformance cases = %v, want %q", got, want)
		}
	}
}
