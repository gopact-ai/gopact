package gopact

import "testing"

func TestStepSnapshotValidateAcceptsCompletedStep(t *testing.T) {
	snapshot := StepSnapshot{
		ID:     "step-1",
		Step:   1,
		Node:   "plan",
		Phase:  StepCompleted,
		IDs:    RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Input:  "before",
		Output: "after",
	}

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestStepSnapshotValidateAcceptsInterruptedStepWithPendingRecord(t *testing.T) {
	snapshot := StepSnapshot{
		ID:    "step-1",
		Step:  1,
		Node:  "approve",
		Phase: StepInterrupted,
		Pending: &InterruptRecord{
			ID:     "interrupt-1",
			Type:   InterruptApproval,
			Reason: "approve tool call",
		},
		Queue: []string{"act"},
	}

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestStepSnapshotValidateAcceptsEffectGraph(t *testing.T) {
	snapshot := StepSnapshot{
		ID:    "step-1",
		Step:  1,
		Node:  "act",
		Phase: StepCompleted,
		Effects: []EffectRecord{
			{
				ID:           "tool-1",
				Type:         "tool_call",
				Target:       "local.shell",
				Applied:      true,
				ReplayPolicy: EffectReplayRecordOnly,
			},
			{
				ID:           "artifact-1",
				Type:         "artifact_write",
				Target:       "artifact://result",
				Applied:      true,
				DependsOn:    []string{"tool-1"},
				ReplayPolicy: EffectReplaySkip,
				Artifacts: []ArtifactRef{{
					ID:     "artifact-1",
					Name:   "result.txt",
					URI:    "memory://artifact-1",
					SHA256: "sha",
				}},
			},
			{
				ID:             "exec-1",
				Type:           "sandbox_exec",
				Target:         "sandbox://local-1",
				Applied:        true,
				DependsOn:      []string{"artifact-1"},
				ReplayPolicy:   EffectReplayIdempotent,
				IdempotencyKey: "exec:go-test",
				Sandbox: &SandboxEffect{
					SessionID: "local-1",
					Operation: "exec",
					Command:   []string{"go", "test", "./..."},
					ExitCode:  0,
				},
			},
		},
	}

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestStepSnapshotValidateRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name     string
		snapshot StepSnapshot
	}{
		{name: "missing id", snapshot: StepSnapshot{Step: 1, Node: "plan", Phase: StepCompleted}},
		{name: "missing node", snapshot: StepSnapshot{ID: "step-1", Step: 1, Phase: StepCompleted}},
		{name: "missing phase", snapshot: StepSnapshot{ID: "step-1", Step: 1, Node: "plan"}},
		{name: "invalid phase", snapshot: StepSnapshot{ID: "step-1", Step: 1, Node: "plan", Phase: StepPhase("unknown")}},
		{name: "invalid step", snapshot: StepSnapshot{ID: "step-1", Step: -1, Node: "plan", Phase: StepCompleted}},
		{name: "interrupted without pending", snapshot: StepSnapshot{ID: "step-1", Step: 1, Node: "plan", Phase: StepInterrupted}},
		{name: "interrupted with invalid pending", snapshot: StepSnapshot{ID: "step-1", Step: 1, Node: "plan", Phase: StepInterrupted, Pending: &InterruptRecord{ID: "interrupt-1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.snapshot.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestStepSnapshotValidateRejectsInvalidEffects(t *testing.T) {
	tests := []struct {
		name    string
		effects []EffectRecord
	}{
		{
			name: "empty dependency id",
			effects: []EffectRecord{{
				ID:        "effect-1",
				Type:      "artifact_write",
				DependsOn: []string{""},
			}},
		},
		{
			name: "duplicate effect id",
			effects: []EffectRecord{
				{ID: "effect-1", Type: "tool_call"},
				{ID: "effect-1", Type: "artifact_write"},
			},
		},
		{
			name: "invalid replay policy",
			effects: []EffectRecord{{
				ID:           "effect-1",
				Type:         "tool_call",
				ReplayPolicy: EffectReplayPolicy("unknown"),
			}},
		},
		{
			name: "idempotent without key",
			effects: []EffectRecord{{
				ID:           "effect-1",
				Type:         "sandbox_exec",
				ReplayPolicy: EffectReplayIdempotent,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := StepSnapshot{
				ID:      "step-1",
				Step:    1,
				Node:    "act",
				Phase:   StepCompleted,
				Effects: tt.effects,
			}
			if err := snapshot.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want invalid effect error")
			}
		})
	}
}

func TestStepExportValidateAcceptsVersionedExport(t *testing.T) {
	export := StepExport{
		Version: 1,
		Step: StepSnapshot{
			ID:    "step-1",
			Step:  1,
			Node:  "act",
			Phase: StepCompleted,
		},
	}

	if err := export.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestStepExportValidateRejectsInvalidExport(t *testing.T) {
	tests := []struct {
		name   string
		export StepExport
	}{
		{name: "missing version", export: StepExport{Step: StepSnapshot{ID: "step-1", Step: 1, Node: "act", Phase: StepCompleted}}},
		{name: "invalid step", export: StepExport{Version: 1, Step: StepSnapshot{ID: "step-1", Step: -1, Node: "act", Phase: StepCompleted}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.export.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}
