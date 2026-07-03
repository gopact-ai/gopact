package repositorychecks

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact/gopacttest/graphconformance"
)

func TestWorkflowOrchestrationMatrixDocumentsImplementedAndPlannedCapabilities(t *testing.T) {
	matrix := loadWorkflowOrchestrationMatrix(t)

	if matrix.Version != 1 {
		t.Fatalf("workflow orchestration matrix version = %d, want 1", matrix.Version)
	}
	if matrix.Scope != "core-workflow-orchestration" {
		t.Fatalf("workflow orchestration matrix scope = %q, want core-workflow-orchestration", matrix.Scope)
	}
	if matrix.ProofCommand != "go test -count=1 ./graph ./gopacttest/graphconformance" {
		t.Fatalf("workflow orchestration matrix proof command = %q", matrix.ProofCommand)
	}

	capabilities := map[string]workflowOrchestrationCapability{}
	for _, capability := range matrix.Capabilities {
		if capability.ID == "" {
			t.Fatal("workflow orchestration capability has empty id")
		}
		if capabilities[capability.ID].ID != "" {
			t.Fatalf("workflow orchestration capability %q is duplicated", capability.ID)
		}
		capabilities[capability.ID] = capability
	}

	conformanceCases := graphConformanceCases(t)
	for _, expected := range expectedCompletedWorkflowCapabilities() {
		capability, ok := capabilities[expected.id]
		if !ok {
			t.Fatalf("workflow orchestration matrix missing completed capability %q", expected.id)
		}
		if capability.Status != "completed" {
			t.Fatalf("workflow orchestration capability %q status = %q, want completed", expected.id, capability.Status)
		}
		if capability.Package != "graph" {
			t.Fatalf("workflow orchestration capability %q package = %q, want graph", expected.id, capability.Package)
		}
		if capability.OfflineProof != matrix.ProofCommand {
			t.Fatalf("workflow orchestration capability %q proof = %q, want matrix proof command", expected.id, capability.OfflineProof)
		}
		if capability.Boundary == "" {
			t.Fatalf("workflow orchestration capability %q boundary is empty", expected.id)
		}
		for _, conformanceCase := range expected.conformanceCases {
			if !slices.Contains(capability.ConformanceCases, conformanceCase) {
				t.Fatalf("workflow orchestration capability %q missing conformance case %q", expected.id, conformanceCase)
			}
			if !conformanceCases[conformanceCase] {
				t.Fatalf("workflow orchestration capability %q references unknown conformance case %q", expected.id, conformanceCase)
			}
		}
	}

	for _, id := range []string{
		"parallel-fanout-executor",
		"human-review-node-template",
		"graph-state-schema-guards",
		"durable-background-scheduler",
	} {
		capability, ok := capabilities[id]
		if !ok {
			t.Fatalf("workflow orchestration matrix missing planned capability %q", id)
		}
		if capability.Status != "planned" {
			t.Fatalf("workflow orchestration capability %q status = %q, want planned", id, capability.Status)
		}
		if capability.Gap == "" {
			t.Fatalf("workflow orchestration planned capability %q gap is empty", id)
		}
		if len(capability.ConformanceCases) != 0 {
			t.Fatalf("workflow orchestration planned capability %q has completed conformance cases: %v", id, capability.ConformanceCases)
		}
		if capability.TargetRepo == "" {
			t.Fatalf("workflow orchestration planned capability %q target_repo is empty", id)
		}
	}
}

func TestWorkflowOrchestrationMatrixIsIndexed(t *testing.T) {
	for _, path := range []string{
		filepath.Join("doc", "FEATURES.md"),
		filepath.Join("doc", "FEATURES_zh.md"),
		filepath.Join("doc", "design", "index.md"),
		filepath.Join("doc", "design", "index_zh.md"),
		filepath.Join("doc", "design", "self-bootstrap-roadmap.md"),
		filepath.Join("doc", "design", "self-bootstrap-roadmap_zh.md"),
	} {
		body := readTextFile(t, path)
		if !strings.Contains(body, "workflow-orchestration-matrix.json") {
			t.Fatalf("%s missing workflow orchestration matrix link", path)
		}
	}
}

type workflowOrchestrationMatrix struct {
	Version      int                               `json:"version"`
	Scope        string                            `json:"scope"`
	ProofCommand string                            `json:"proof_command"`
	Capabilities []workflowOrchestrationCapability `json:"capabilities"`
}

type workflowOrchestrationCapability struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	Package          string   `json:"package"`
	TargetRepo       string   `json:"target_repo"`
	OfflineProof     string   `json:"offline_proof"`
	ConformanceCases []string `json:"conformance_cases"`
	Boundary         string   `json:"boundary"`
	Gap              string   `json:"gap"`
}

func loadWorkflowOrchestrationMatrix(t *testing.T) workflowOrchestrationMatrix {
	t.Helper()

	raw := readFile(t, filepath.Join("doc", "design", "workflow-orchestration-matrix.json"))
	var matrix workflowOrchestrationMatrix
	if err := json.Unmarshal(raw, &matrix); err != nil {
		t.Fatalf("decode workflow orchestration matrix: %v", err)
	}
	return matrix
}

func graphConformanceCases(t *testing.T) map[string]bool {
	t.Helper()

	cases := map[string]bool{}
	for _, result := range graphconformance.CheckGraphConformance(t.Context()) {
		if !result.Passed {
			t.Fatalf("graph conformance case %q failed: %v", result.Case, result.Err)
		}
		cases[result.Case] = true
	}
	return cases
}

func expectedCompletedWorkflowCapabilities() []struct {
	id               string
	conformanceCases []string
} {
	return []struct {
		id               string
		conformanceCases []string
	}{
		{
			id: "branch-routing",
			conformanceCases: []string{
				"branch-routes-selected-target",
				"branch-routes-multiple-targets",
				"branch-can-end-with-no-targets",
				"branch-rejects-missing-target",
				"branch-resume-uses-checkpoint-queue",
			},
		},
		{
			id: "dag-fan-in",
			conformanceCases: []string{
				"dag-fan-in-runs-join-after-parents",
				"dag-fan-in-stops-when-parent-fails",
				"dag-fan-in-preserves-edge-order",
			},
		},
		{
			id: "dynamic-fan-out",
			conformanceCases: []string{
				"dynamic-fan-out-resumes-incomplete-targets",
				"dynamic-fan-out-runs-all-targets",
				"dynamic-fan-out-empty-completes",
				"dynamic-fan-out-stops-on-target-failure",
			},
		},
		{
			id:               "loop-control",
			conformanceCases: []string{"loop-branch-exits", "loop-step-limit-fails"},
		},
		{
			id:               "step-export-import",
			conformanceCases: []string{"step-export-resumes-completed-boundary"},
		},
		{
			id:               "interrupt-resume",
			conformanceCases: []string{"interrupted-step-export-resumes-with-request"},
		},
		{
			id: "runnable-subgraph",
			conformanceCases: []string{
				"runnable-node-runs-subgraph",
				"runnable-node-streams-nested-events",
				"runnable-node-inherits-runtime-ids",
				"runnable-node-checkpoint-inheritance-isolation",
			},
		},
		{
			id:               "node-emitted-events",
			conformanceCases: []string{"node-emits-nested-events"},
		},
		{
			id: "failure-and-cancel-propagation",
			conformanceCases: []string{
				"failed-node-stops-successors",
				"canceled-node-stops-successors",
			},
		},
		{
			id:               "workflow-visualization-export",
			conformanceCases: []string{"topology-export-stable"},
		},
	}
}
