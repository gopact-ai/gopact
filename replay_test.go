package gopact

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPlanEffectReplayOrdersDependencies(t *testing.T) {
	snapshot := StepSnapshot{
		ID:    "step-1",
		Step:  1,
		Node:  "write",
		Phase: StepCompleted,
		Effects: []EffectRecord{
			{
				ID:           "artifact-1",
				Type:         "artifact_export",
				Applied:      true,
				ReplayPolicy: EffectReplaySkip,
				DependsOn:    []string{"tool-1"},
			},
			{
				ID:             "tool-1",
				Type:           "tool_call",
				Applied:        true,
				ReplayPolicy:   EffectReplayIdempotent,
				IdempotencyKey: "call-1",
			},
			{
				ID:           "log-1",
				Type:         "tool_call",
				Applied:      true,
				ReplayPolicy: EffectReplayRecordOnly,
			},
		},
	}

	plan, err := PlanEffectReplay(snapshot)
	if err != nil {
		t.Fatalf("PlanEffectReplay() error = %v", err)
	}

	want := []struct {
		id     string
		action EffectReplayAction
	}{
		{id: "tool-1", action: EffectReplayActionReplay},
		{id: "artifact-1", action: EffectReplayActionSkip},
		{id: "log-1", action: EffectReplayActionRecordOnly},
	}
	if len(plan.Decisions) != len(want) {
		t.Fatalf("decision count = %d, want %d", len(plan.Decisions), len(want))
	}
	for i, expected := range want {
		if plan.Decisions[i].Effect.ID != expected.id || plan.Decisions[i].Action != expected.action {
			t.Fatalf("decision[%d] = %+v, want id %q action %q", i, plan.Decisions[i], expected.id, expected.action)
		}
	}
	if plan.ReplayCount != 1 || plan.SkipCount != 1 || plan.RecordOnlyCount != 1 {
		t.Fatalf("plan counts = replay:%d skip:%d record:%d, want 1/1/1", plan.ReplayCount, plan.SkipCount, plan.RecordOnlyCount)
	}
}

func TestPlanEffectReplayDefaultsUnspecifiedPolicyToRecordOnly(t *testing.T) {
	snapshot := StepSnapshot{
		ID:    "step-1",
		Step:  1,
		Node:  "write",
		Phase: StepCompleted,
		Effects: []EffectRecord{
			{ID: "effect-1", Applied: true},
		},
	}

	plan, err := PlanEffectReplay(snapshot)
	if err != nil {
		t.Fatalf("PlanEffectReplay() error = %v", err)
	}
	if len(plan.Decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(plan.Decisions))
	}
	if plan.Decisions[0].ReplayPolicy != EffectReplayRecordOnly {
		t.Fatalf("replay policy = %q, want record_only", plan.Decisions[0].ReplayPolicy)
	}
	if plan.Decisions[0].Action != EffectReplayActionRecordOnly {
		t.Fatalf("action = %q, want record_only", plan.Decisions[0].Action)
	}
}

func TestPlanEffectReplayRejectsCycles(t *testing.T) {
	snapshot := StepSnapshot{
		ID:    "step-1",
		Step:  1,
		Node:  "write",
		Phase: StepCompleted,
		Effects: []EffectRecord{
			{ID: "one", DependsOn: []string{"two"}, ReplayPolicy: EffectReplayRecordOnly},
			{ID: "two", DependsOn: []string{"one"}, ReplayPolicy: EffectReplayRecordOnly},
		},
	}

	if _, err := PlanEffectReplay(snapshot); err == nil {
		t.Fatal("PlanEffectReplay() error = nil, want cycle validation error")
	}
}

func TestExecuteEffectReplayCallsExecutorOnlyForReplay(t *testing.T) {
	plan := EffectReplayPlan{
		StepID: "step-1",
		Decisions: []EffectReplayDecision{
			{
				Effect: EffectRecord{ID: "tool-1"},
				Action: EffectReplayActionReplay,
			},
			{
				Effect: EffectRecord{ID: "artifact-1"},
				Action: EffectReplayActionSkip,
			},
			{
				Effect: EffectRecord{ID: "log-1"},
				Action: EffectReplayActionRecordOnly,
			},
		},
	}
	var replayed []string

	results, err := ExecuteEffectReplay(context.Background(), plan, EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		replayed = append(replayed, decision.Effect.ID)
		return EffectReplayResult{
			EffectID: decision.Effect.ID,
			Action:   decision.Action,
			Metadata: map[string]any{"replayed": true},
		}, nil
	}))
	if err != nil {
		t.Fatalf("ExecuteEffectReplay() error = %v", err)
	}
	if !reflect.DeepEqual(replayed, []string{"tool-1"}) {
		t.Fatalf("replayed = %v, want [tool-1]", replayed)
	}
	if len(results) != 3 {
		t.Fatalf("result count = %d, want 3", len(results))
	}
	if results[0].EffectID != "tool-1" || results[0].Metadata["replayed"] != true {
		t.Fatalf("replay result = %+v", results[0])
	}
	if results[1].EffectID != "artifact-1" || results[1].Action != EffectReplayActionSkip {
		t.Fatalf("skip result = %+v", results[1])
	}
	if results[2].EffectID != "log-1" || results[2].Action != EffectReplayActionRecordOnly {
		t.Fatalf("record-only result = %+v", results[2])
	}
}

func TestExecuteEffectReplayRequiresExecutorForReplay(t *testing.T) {
	plan := EffectReplayPlan{
		Decisions: []EffectReplayDecision{
			{Effect: EffectRecord{ID: "tool-1"}, Action: EffectReplayActionReplay},
		},
	}

	if _, err := ExecuteEffectReplay(context.Background(), plan, nil); err == nil {
		t.Fatal("ExecuteEffectReplay() error = nil, want missing executor error")
	}
}

func TestExecuteEffectReplayPropagatesExecutorError(t *testing.T) {
	wantErr := errors.New("replay failed")
	plan := EffectReplayPlan{
		Decisions: []EffectReplayDecision{
			{Effect: EffectRecord{ID: "tool-1"}, Action: EffectReplayActionReplay},
		},
	}

	_, err := ExecuteEffectReplay(context.Background(), plan, EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{}, wantErr
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExecuteEffectReplay() error = %v, want %v", err, wantErr)
	}
}

func TestExecuteRunEffectReplayCallsExecutorOnlyForReplay(t *testing.T) {
	plan := RunEffectReplayPlan{
		RunID:    "run-1",
		ThreadID: "thread-1",
		Decisions: []RunEffectReplayDecision{
			{
				StepID: "step-1",
				Step:   1,
				Node:   "search",
				Index:  0,
				Decision: EffectReplayDecision{
					Effect:       EffectRecord{ID: "tool-1"},
					Action:       EffectReplayActionRecordOnly,
					ReplayPolicy: EffectReplayRecordOnly,
				},
			},
			{
				StepID: "step-1",
				Step:   1,
				Node:   "search",
				Index:  1,
				Decision: EffectReplayDecision{
					Effect:       EffectRecord{ID: "artifact-1"},
					Action:       EffectReplayActionSkip,
					ReplayPolicy: EffectReplaySkip,
				},
			},
			{
				StepID: "step-2",
				Step:   2,
				Node:   "verify",
				Index:  2,
				Decision: EffectReplayDecision{
					Effect:       EffectRecord{ID: "verify-1"},
					Action:       EffectReplayActionReplay,
					ReplayPolicy: EffectReplayIdempotent,
				},
			},
		},
	}
	var replayed []string

	results, err := ExecuteRunEffectReplay(context.Background(), plan, EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		replayed = append(replayed, decision.Effect.ID)
		return EffectReplayResult{
			EffectID: decision.Effect.ID,
			Action:   decision.Action,
			Metadata: map[string]any{"replayed": true},
		}, nil
	}))
	if err != nil {
		t.Fatalf("ExecuteRunEffectReplay() error = %v", err)
	}
	if !reflect.DeepEqual(replayed, []string{"verify-1"}) {
		t.Fatalf("replayed = %v, want [verify-1]", replayed)
	}
	if len(results) != 3 {
		t.Fatalf("result count = %d, want 3", len(results))
	}
	if results[0].StepID != "step-1" || results[0].Step != 1 || results[0].Node != "search" || results[0].Index != 0 {
		t.Fatalf("result[0] step metadata = %+v", results[0])
	}
	if results[2].StepID != "step-2" || results[2].Step != 2 || results[2].Node != "verify" || results[2].Index != 2 {
		t.Fatalf("result[2] step metadata = %+v", results[2])
	}
	if results[0].Result.EffectID != "tool-1" || results[0].Result.Action != EffectReplayActionRecordOnly {
		t.Fatalf("record-only result = %+v", results[0].Result)
	}
	if results[1].Result.EffectID != "artifact-1" || results[1].Result.Action != EffectReplayActionSkip {
		t.Fatalf("skip result = %+v", results[1].Result)
	}
	if results[2].Result.EffectID != "verify-1" || results[2].Result.Metadata["replayed"] != true {
		t.Fatalf("replay result = %+v", results[2].Result)
	}
}

func TestExecuteRunEffectReplayRequiresExecutorForReplay(t *testing.T) {
	plan := RunEffectReplayPlan{
		Decisions: []RunEffectReplayDecision{
			{
				StepID: "step-1",
				Decision: EffectReplayDecision{
					Effect: EffectRecord{ID: "tool-1"},
					Action: EffectReplayActionReplay,
				},
			},
		},
	}

	if _, err := ExecuteRunEffectReplay(context.Background(), plan, nil); err == nil {
		t.Fatal("ExecuteRunEffectReplay() error = nil, want missing executor error")
	}
}

func TestExecuteRunEffectReplayPropagatesExecutorError(t *testing.T) {
	wantErr := errors.New("replay failed")
	plan := RunEffectReplayPlan{
		Decisions: []RunEffectReplayDecision{
			{
				StepID: "step-1",
				Decision: EffectReplayDecision{
					Effect: EffectRecord{ID: "log-1"},
					Action: EffectReplayActionRecordOnly,
				},
			},
			{
				StepID: "step-2",
				Decision: EffectReplayDecision{
					Effect: EffectRecord{ID: "tool-1"},
					Action: EffectReplayActionReplay,
				},
			},
		},
	}

	results, err := ExecuteRunEffectReplay(context.Background(), plan, EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{}, wantErr
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExecuteRunEffectReplay() error = %v, want %v", err, wantErr)
	}
	if len(results) != 1 || results[0].Result.EffectID != "log-1" {
		t.Fatalf("partial results = %+v, want completed record-only result", results)
	}
}

func TestEffectReplayRegistryDispatchesTargetBeforeType(t *testing.T) {
	registry := NewEffectReplayRegistry()
	if err := registry.Register("tool_call", EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{Metadata: map[string]any{"handler": "type"}}, nil
	})); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.RegisterTarget("tool_call", "tools/search", EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{Metadata: map[string]any{"handler": "target"}}, nil
	})); err != nil {
		t.Fatalf("RegisterTarget() error = %v", err)
	}
	decision := EffectReplayDecision{
		Effect:       EffectRecord{ID: "effect-1", Type: "tool_call", Target: "tools/search"},
		Action:       EffectReplayActionReplay,
		ReplayPolicy: EffectReplayIdempotent,
	}

	result, err := registry.ReplayEffect(context.Background(), decision)
	if err != nil {
		t.Fatalf("ReplayEffect() error = %v", err)
	}
	if result.Metadata["handler"] != "target" {
		t.Fatalf("handler = %v, want target", result.Metadata["handler"])
	}
	if result.EffectID != "effect-1" || result.Action != EffectReplayActionReplay || result.ReplayPolicy != EffectReplayIdempotent {
		t.Fatalf("normalized result = %+v", result)
	}
	if result.Effect.Type != "tool_call" || result.Effect.Target != "tools/search" {
		t.Fatalf("result effect = %+v, want original effect copied", result.Effect)
	}
}

func TestEffectReplayRegistryDispatchesTypeAndFallbackHandlers(t *testing.T) {
	registry := NewEffectReplayRegistry()
	if err := registry.Register("sandbox_exec", EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{Metadata: map[string]any{"handler": "type"}}, nil
	})); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.RegisterFallback(EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{Metadata: map[string]any{"handler": "fallback"}}, nil
	})); err != nil {
		t.Fatalf("RegisterFallback() error = %v", err)
	}

	typeResult, err := registry.ReplayEffect(context.Background(), EffectReplayDecision{
		Effect: EffectRecord{ID: "effect-1", Type: "sandbox_exec"},
		Action: EffectReplayActionReplay,
	})
	if err != nil {
		t.Fatalf("ReplayEffect(type) error = %v", err)
	}
	if typeResult.Metadata["handler"] != "type" {
		t.Fatalf("type handler = %v, want type", typeResult.Metadata["handler"])
	}

	fallbackResult, err := registry.ReplayEffect(context.Background(), EffectReplayDecision{
		Effect: EffectRecord{ID: "effect-2", Type: "artifact_write"},
		Action: EffectReplayActionReplay,
	})
	if err != nil {
		t.Fatalf("ReplayEffect(fallback) error = %v", err)
	}
	if fallbackResult.Metadata["handler"] != "fallback" {
		t.Fatalf("fallback handler = %v, want fallback", fallbackResult.Metadata["handler"])
	}
}

func TestEffectReplayRegistryReturnsNotFound(t *testing.T) {
	registry := NewEffectReplayRegistry()

	_, err := registry.ReplayEffect(context.Background(), EffectReplayDecision{
		Effect: EffectRecord{ID: "effect-1", Type: "tool_call"},
		Action: EffectReplayActionReplay,
	})
	if !errors.Is(err, ErrEffectReplayHandlerNotFound) {
		t.Fatalf("ReplayEffect() error = %v, want ErrEffectReplayHandlerNotFound", err)
	}
}

func TestEffectReplayRegistryRejectsInvalidRegistration(t *testing.T) {
	registry := NewEffectReplayRegistry()
	handler := EffectReplayFunc(func(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error) {
		return EffectReplayResult{}, nil
	})

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "empty type",
			run:  func() error { return registry.Register("", handler) },
		},
		{
			name: "nil handler",
			run:  func() error { return registry.Register("tool_call", nil) },
		},
		{
			name: "empty target",
			run:  func() error { return registry.RegisterTarget("tool_call", "", handler) },
		},
		{
			name: "duplicate type handler",
			run: func() error {
				if err := registry.Register("duplicate", handler); err != nil {
					return err
				}
				return registry.Register("duplicate", handler)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("registration error = nil, want error")
			}
		})
	}
}

func TestBuildRunEffectGraphOrdersCrossStepDependencies(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: RunCompleted,
		Steps: []StepSnapshot{
			{
				ID:    "step-1",
				Step:  1,
				Node:  "search",
				Phase: StepCompleted,
				Effects: []EffectRecord{
					{ID: "artifact-1", Type: "artifact_write", DependsOn: []string{"tool-1"}, ReplayPolicy: EffectReplaySkip},
					{ID: "tool-1", Type: "tool_call", ReplayPolicy: EffectReplayRecordOnly},
				},
			},
			{
				ID:    "step-2",
				Step:  2,
				Node:  "verify",
				Phase: StepCompleted,
				Effects: []EffectRecord{
					{ID: "verify-1", Type: "sandbox_exec", DependsOn: []string{"artifact-1"}, ReplayPolicy: EffectReplayRecordOnly},
				},
			},
		},
	}

	graph, err := BuildRunEffectGraph(export)
	if err != nil {
		t.Fatalf("BuildRunEffectGraph() error = %v", err)
	}

	if graph.RunID != "run-1" || graph.ThreadID != "thread-1" {
		t.Fatalf("graph ids = %q/%q, want run-1/thread-1", graph.RunID, graph.ThreadID)
	}
	got := runEffectIDs(graph.Ordered)
	want := []string{"tool-1", "artifact-1", "verify-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered effect ids = %v, want %v", got, want)
	}
	if len(graph.Nodes) != 3 || len(graph.Edges) != 2 {
		t.Fatalf("nodes/edges = %d/%d, want 3/2", len(graph.Nodes), len(graph.Edges))
	}
	if graph.Nodes[2].StepID != "step-2" || graph.Nodes[2].Node != "verify" || graph.Nodes[2].Step != 2 {
		t.Fatalf("node metadata = %+v, want step-2 verify", graph.Nodes[2])
	}

	graph.Nodes[0].Effect.ID = "mutated"
	graphAgain, err := BuildRunEffectGraph(export)
	if err != nil {
		t.Fatalf("BuildRunEffectGraph(second) error = %v", err)
	}
	if graphAgain.Nodes[0].Effect.ID != "artifact-1" {
		t.Fatal("BuildRunEffectGraph() returned mutable backing effect")
	}
}

func TestBuildRunEffectGraphRejectsInvalidRunEffects(t *testing.T) {
	tests := []struct {
		name   string
		export RunExport
	}{
		{
			name: "duplicate effect id",
			export: RunExport{
				Version: RunExportVersion,
				IDs:     RuntimeIDs{RunID: "run-1"},
				Outcome: RunCompleted,
				Steps: []StepSnapshot{
					{ID: "step-1", Step: 1, Node: "one", Phase: StepCompleted, Effects: []EffectRecord{{ID: "same"}}},
					{ID: "step-2", Step: 2, Node: "two", Phase: StepCompleted, Effects: []EffectRecord{{ID: "same"}}},
				},
			},
		},
		{
			name: "missing dependency",
			export: RunExport{
				Version: RunExportVersion,
				IDs:     RuntimeIDs{RunID: "run-1"},
				Outcome: RunCompleted,
				Steps: []StepSnapshot{
					{ID: "step-1", Step: 1, Node: "one", Phase: StepCompleted, Effects: []EffectRecord{{ID: "effect-1", DependsOn: []string{"missing"}}}},
				},
			},
		},
		{
			name: "future dependency",
			export: RunExport{
				Version: RunExportVersion,
				IDs:     RuntimeIDs{RunID: "run-1"},
				Outcome: RunCompleted,
				Steps: []StepSnapshot{
					{ID: "step-1", Step: 1, Node: "one", Phase: StepCompleted, Effects: []EffectRecord{{ID: "effect-1", DependsOn: []string{"effect-2"}}}},
					{ID: "step-2", Step: 2, Node: "two", Phase: StepCompleted, Effects: []EffectRecord{{ID: "effect-2"}}},
				},
			},
		},
		{
			name: "cycle",
			export: RunExport{
				Version: RunExportVersion,
				IDs:     RuntimeIDs{RunID: "run-1"},
				Outcome: RunCompleted,
				Steps: []StepSnapshot{
					{ID: "step-1", Step: 1, Node: "one", Phase: StepCompleted, Effects: []EffectRecord{{ID: "effect-1", DependsOn: []string{"effect-2"}}, {ID: "effect-2", DependsOn: []string{"effect-1"}}}},
				},
			},
		},
		{
			name: "empty effect id",
			export: RunExport{
				Version: RunExportVersion,
				IDs:     RuntimeIDs{RunID: "run-1"},
				Outcome: RunCompleted,
				Steps: []StepSnapshot{
					{ID: "step-1", Step: 1, Node: "one", Phase: StepCompleted, Effects: []EffectRecord{{Type: "tool_call"}}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildRunEffectGraph(tt.export); err == nil {
				t.Fatal("BuildRunEffectGraph() error = nil, want validation error")
			}
		})
	}
}

func TestPlanRunEffectReplayUsesRunEffectGraphOrder(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: RunCompleted,
		Steps: []StepSnapshot{
			{
				ID:    "step-1",
				Step:  1,
				Node:  "search",
				Phase: StepCompleted,
				Effects: []EffectRecord{
					{ID: "artifact-1", Type: "artifact_write", DependsOn: []string{"tool-1"}, ReplayPolicy: EffectReplaySkip},
					{ID: "tool-1", Type: "tool_call", ReplayPolicy: EffectReplayRecordOnly},
				},
			},
			{
				ID:    "step-2",
				Step:  2,
				Node:  "verify",
				Phase: StepCompleted,
				Effects: []EffectRecord{
					{ID: "verify-1", Type: "sandbox_exec", DependsOn: []string{"artifact-1"}, ReplayPolicy: EffectReplayIdempotent, IdempotencyKey: "verify:1"},
				},
			},
		},
	}

	plan, err := PlanRunEffectReplay(export)
	if err != nil {
		t.Fatalf("PlanRunEffectReplay() error = %v", err)
	}

	if plan.RunID != "run-1" || plan.ThreadID != "thread-1" {
		t.Fatalf("plan ids = %q/%q, want run-1/thread-1", plan.RunID, plan.ThreadID)
	}
	if plan.ReplayCount != 1 || plan.SkipCount != 1 || plan.RecordOnlyCount != 1 {
		t.Fatalf("plan counts = replay:%d skip:%d record:%d, want 1/1/1", plan.ReplayCount, plan.SkipCount, plan.RecordOnlyCount)
	}
	got := runPlanEffectIDs(plan.Decisions)
	want := []string{"tool-1", "artifact-1", "verify-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decision effect ids = %v, want %v", got, want)
	}
	if plan.Decisions[2].StepID != "step-2" || plan.Decisions[2].Step != 2 || plan.Decisions[2].Node != "verify" {
		t.Fatalf("decision step metadata = %+v", plan.Decisions[2])
	}
	if plan.Decisions[2].Decision.Action != EffectReplayActionReplay || plan.Decisions[2].Decision.IdempotencyKey != "verify:1" {
		t.Fatalf("decision[2] = %+v, want replay with idempotency key", plan.Decisions[2].Decision)
	}
}

func TestPlanRunEffectReplayPropagatesInvalidGraph(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunCompleted,
		Steps: []StepSnapshot{
			{ID: "step-1", Step: 1, Node: "one", Phase: StepCompleted, Effects: []EffectRecord{{ID: "effect-1", DependsOn: []string{"missing"}}}},
		},
	}

	if _, err := PlanRunEffectReplay(export); err == nil {
		t.Fatal("PlanRunEffectReplay() error = nil, want graph validation error")
	}
}

func runEffectIDs(nodes []RunEffectNode) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.Effect.ID)
	}
	return ids
}

func runPlanEffectIDs(decisions []RunEffectReplayDecision) []string {
	ids := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		ids = append(ids, decision.Decision.Effect.ID)
	}
	return ids
}
