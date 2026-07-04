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
		"parallel-fan-out-runs-targets-concurrently",
		"parallel-fan-out-cancels-siblings-on-failure",
		"parallel-fan-out-merge-error-stops-successors",
		"parallel-fan-out-checkpointing-falls-back-to-sequential",
		"loop-branch-exits",
		"loop-step-limit-fails",
		"step-export-resumes-completed-boundary",
		"interrupted-step-export-resumes-with-request",
		"step-export-verifies-artifacts-before-resume",
		"runnable-node-runs-subgraph",
		"runnable-node-streams-nested-events",
		"node-emits-nested-events",
		"runnable-node-inherits-runtime-ids",
		"runnable-node-checkpoint-inheritance-isolation",
		"topology-export-stable",
		"schema-guard-rejects-invalid-node-input",
		"schema-guard-rejects-invalid-node-output",
		"schema-guard-rejects-invalid-resume-state",
		"schema-guard-exports-topology-contracts",
		"node-middleware-records-effects",
		"failed-node-stops-successors",
		"canceled-node-stops-successors",
	} {
		if !got[want] {
			t.Fatalf("conformance cases = %v, want %q", got, want)
		}
	}
}
