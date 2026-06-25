package gopact

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRecordEffectReplayCheckRecordsCompletedReplayEvidence(t *testing.T) {
	plan := effectReplayVerificationPlan()
	results := []EffectReplayResult{
		{
			EffectID:     "tool-1",
			Action:       EffectReplayActionRecordOnly,
			ReplayPolicy: EffectReplayRecordOnly,
		},
		{
			EffectID:     "verify-1",
			Action:       EffectReplayActionReplay,
			ReplayPolicy: EffectReplayIdempotent,
		},
	}

	recorder := NewVerificationRecorder()
	err := RecordEffectReplayCheck(recorder, EffectReplaySnapshot{
		Plan:    plan,
		Results: results,
	})
	if err != nil {
		t.Fatalf("RecordEffectReplayCheck() error = %v", err)
	}

	check := singleReplayCheck(t, recorder)
	if check.ID != VerificationCheckEffectReplay+":step-1" {
		t.Fatalf("check ID = %q, want step effect replay check", check.ID)
	}
	if check.Status != VerificationStatusPassed {
		t.Fatalf("check status = %q, want passed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeEffectReplay {
		t.Fatalf("check evidence = %+v, want effect replay evidence", check.Evidence)
	}
	assertStepReplayMetadata(t, check.Metadata, 2, 1, 1, 0, 2)
}

func TestRecordEffectReplayCheckPreservesCanonicalMetadata(t *testing.T) {
	plan := effectReplayVerificationPlan()
	results := []EffectReplayResult{
		{
			EffectID:     "tool-1",
			Action:       EffectReplayActionRecordOnly,
			ReplayPolicy: EffectReplayRecordOnly,
		},
		{
			EffectID:     "verify-1",
			Action:       EffectReplayActionReplay,
			ReplayPolicy: EffectReplayIdempotent,
		},
	}

	recorder := NewVerificationRecorder()
	err := RecordEffectReplayCheck(recorder, EffectReplaySnapshot{
		Plan:    plan,
		Results: results,
		Metadata: map[string]any{
			"ref":               "forged-ref",
			"decision_count":    999,
			"replay_count":      999,
			"record_only_count": 999,
			"skip_count":        999,
			"result_count":      999,
			"step_id":           "forged-step",
			"step":              999,
			"node":              "forged-node",
			"planned_effect_ids": []string{
				"forged-plan",
			},
			"result_effect_ids": []string{
				"forged-result",
			},
			"source": "resume",
		},
	})
	if err != nil {
		t.Fatalf("RecordEffectReplayCheck() error = %v", err)
	}

	metadata := singleReplayCheck(t, recorder).Metadata
	if metadata["ref"] != "step-1" ||
		metadata["decision_count"] != 2 ||
		metadata["replay_count"] != 1 ||
		metadata["record_only_count"] != 1 ||
		metadata["skip_count"] != 0 ||
		metadata["result_count"] != 2 ||
		metadata["step_id"] != "step-1" ||
		metadata["step"] != 1 ||
		metadata["node"] != "verify" {
		t.Fatalf("metadata = %+v, want canonical effect replay fields preserved", metadata)
	}
	assertStringSliceMetadata(t, metadata, "planned_effect_ids", []string{"tool-1", "verify-1"})
	assertStringSliceMetadata(t, metadata, "result_effect_ids", []string{"tool-1", "verify-1"})
	if metadata["source"] != "resume" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
}

func TestRecordEffectReplayCheckFailsIncompleteReplay(t *testing.T) {
	plan := effectReplayVerificationPlan()
	results := []EffectReplayResult{
		{
			EffectID:     "tool-1",
			Action:       EffectReplayActionRecordOnly,
			ReplayPolicy: EffectReplayRecordOnly,
		},
	}

	recorder := NewVerificationRecorder()
	err := RecordEffectReplayCheck(recorder, EffectReplaySnapshot{
		Plan:    plan,
		Results: results,
	})
	if !errors.Is(err, ErrEffectReplayVerificationFailed) {
		t.Fatalf("RecordEffectReplayCheck() error = %v, want replay verification failure", err)
	}

	check := singleReplayCheck(t, recorder)
	if check.Status != VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["missing_result_count"] != 1 {
		t.Fatalf("check metadata = %+v, want one missing result", check.Metadata)
	}
}

func TestRecordEffectReplayCheckSkipsEmptyPlan(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordEffectReplayCheck(recorder, EffectReplaySnapshot{
		Plan: EffectReplayPlan{StepID: "step-1", Step: 1, Node: "verify"},
	})
	if err != nil {
		t.Fatalf("RecordEffectReplayCheck(empty) error = %v", err)
	}

	check := singleReplayCheck(t, recorder)
	if check.Status != VerificationStatusSkipped {
		t.Fatalf("check status = %q, want skipped", check.Status)
	}
	assertStepReplayMetadata(t, check.Metadata, 0, 0, 0, 0, 0)
}

func TestEventEffectReplayPlanReturnsPlanCopyFromMetadata(t *testing.T) {
	plan := effectReplayVerificationPlan()
	plan.Decisions[0].Effect.Metadata = map[string]any{"source": "checkpoint"}
	event := Event{
		Type: EventStepImported,
		Metadata: map[string]any{
			EventMetadataEffectReplayPlan: plan,
		},
	}

	got, ok := EventEffectReplayPlan(event)
	if !ok {
		t.Fatal("EventEffectReplayPlan() ok = false, want true")
	}
	if got.StepID != "step-1" || got.Step != 1 || got.Node != "verify" {
		t.Fatalf("EventEffectReplayPlan() = %+v, want step identity", got)
	}
	if len(got.Decisions) != 2 {
		t.Fatalf("EventEffectReplayPlan() decisions = %d, want 2", len(got.Decisions))
	}

	got.Decisions[0].Effect.ID = "mutated"
	got.Decisions[0].Effect.Metadata["source"] = "mutated"
	again, ok := EventEffectReplayPlan(event)
	if !ok {
		t.Fatal("EventEffectReplayPlan() second ok = false, want true")
	}
	if again.Decisions[0].Effect.ID != "tool-1" {
		t.Fatalf("EventEffectReplayPlan() returned mutable decision effect")
	}
	if again.Decisions[0].Effect.Metadata["source"] != "checkpoint" {
		t.Fatalf("EventEffectReplayPlan() returned mutable decision metadata")
	}
}

func TestEventEffectReplayPlanReturnsPlanFromDecodedMetadata(t *testing.T) {
	data, err := json.Marshal(effectReplayVerificationPlan())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	got, ok := EventEffectReplayPlan(Event{
		Type: EventCheckpointLoaded,
		Metadata: map[string]any{
			EventMetadataEffectReplayPlan: decoded,
		},
	})
	if !ok {
		t.Fatal("EventEffectReplayPlan() ok = false, want true for decoded metadata")
	}
	if got.StepID != "step-1" || got.Step != 1 || got.Node != "verify" {
		t.Fatalf("EventEffectReplayPlan() = %+v, want step identity", got)
	}
	if len(got.Decisions) != 2 || got.Decisions[1].Effect.ID != "verify-1" {
		t.Fatalf("EventEffectReplayPlan() decisions = %+v, want decoded decisions", got.Decisions)
	}
	if got.Decisions[1].ReplayPolicy != EffectReplayIdempotent || got.Decisions[1].Action != EffectReplayActionReplay {
		t.Fatalf("EventEffectReplayPlan() decision = %+v, want replay policy/action", got.Decisions[1])
	}
}

func TestEventEffectReplayPlanReturnsFalseWithoutPlan(t *testing.T) {
	_, ok := EventEffectReplayPlan(Event{
		Type:     EventCheckpointLoaded,
		Metadata: map[string]any{EventMetadataEffectReplayPlan: "bad"},
	})
	if ok {
		t.Fatal("EventEffectReplayPlan() ok = true, want false for wrong metadata type")
	}

	_, ok = EventEffectReplayPlan(Event{Type: EventCheckpointLoaded})
	if ok {
		t.Fatal("EventEffectReplayPlan() ok = true, want false without metadata")
	}
}

func TestEffectReplaySnapshotFromEventBuildsRecorderSnapshot(t *testing.T) {
	plan := effectReplayVerificationPlan()
	results := []EffectReplayResult{
		{
			EffectID:     "tool-1",
			Action:       EffectReplayActionRecordOnly,
			ReplayPolicy: EffectReplayRecordOnly,
			Effect:       EffectRecord{ID: "tool-1", Type: "tool_call", Metadata: map[string]any{"source": "recorded"}},
			Metadata:     map[string]any{"replayed": false},
		},
		{
			EffectID:     "verify-1",
			Action:       EffectReplayActionReplay,
			ReplayPolicy: EffectReplayIdempotent,
			Effect:       EffectRecord{ID: "verify-1", Type: "sandbox_exec"},
		},
	}
	event := Event{
		Type: EventStepImported,
		IDs: RuntimeIDs{
			RunID:    "run-1",
			ThreadID: "thread-1",
		},
		Node: "verify",
		Step: 1,
		Metadata: map[string]any{
			EventMetadataEffectReplayPlan: plan,
		},
	}

	snapshot, ok := EffectReplaySnapshotFromEvent(event, results, nil)
	if !ok {
		t.Fatal("EffectReplaySnapshotFromEvent() ok = false, want true")
	}
	if snapshot.Plan.StepID != "step-1" || len(snapshot.Results) != 2 {
		t.Fatalf("EffectReplaySnapshotFromEvent() = %+v, want plan and results", snapshot)
	}
	if snapshot.Metadata["event_type"] != string(EventStepImported) {
		t.Fatalf("snapshot metadata event_type = %v, want %q", snapshot.Metadata["event_type"], EventStepImported)
	}
	if snapshot.Metadata["run_id"] != "run-1" || snapshot.Metadata["thread_id"] != "thread-1" {
		t.Fatalf("snapshot metadata IDs = %+v, want run/thread IDs", snapshot.Metadata)
	}
	if snapshot.Metadata["event_node"] != "verify" || snapshot.Metadata["event_step"] != 1 {
		t.Fatalf("snapshot metadata event step = %+v, want node/step", snapshot.Metadata)
	}

	results[0].Effect.ID = "mutated"
	results[0].Effect.Metadata["source"] = "mutated"
	results[0].Metadata["replayed"] = true
	if snapshot.Results[0].Effect.ID != "tool-1" {
		t.Fatalf("EffectReplaySnapshotFromEvent() returned mutable result effect")
	}
	if snapshot.Results[0].Effect.Metadata["source"] != "recorded" {
		t.Fatalf("EffectReplaySnapshotFromEvent() returned mutable result effect metadata")
	}
	if snapshot.Results[0].Metadata["replayed"] != false {
		t.Fatalf("EffectReplaySnapshotFromEvent() returned mutable result metadata")
	}

	recorder := NewVerificationRecorder()
	if err := RecordEffectReplayCheck(recorder, snapshot); err != nil {
		t.Fatalf("RecordEffectReplayCheck(snapshot) error = %v", err)
	}
	check := singleReplayCheck(t, recorder)
	if check.ID != VerificationCheckEffectReplay+":step-1" {
		t.Fatalf("check ID = %q, want step effect replay check", check.ID)
	}
	if check.Metadata["event_type"] != string(EventStepImported) {
		t.Fatalf("check metadata event_type = %v, want event type", check.Metadata["event_type"])
	}
}

func TestEffectReplaySnapshotFromEventReturnsFalseWithoutPlan(t *testing.T) {
	_, ok := EffectReplaySnapshotFromEvent(Event{Type: EventCheckpointLoaded}, nil, nil)
	if ok {
		t.Fatal("EffectReplaySnapshotFromEvent() ok = true, want false without replay plan")
	}
}

func TestRecordRunEffectReplayCheckRecordsCompletedReplayEvidence(t *testing.T) {
	plan := runEffectReplayVerificationPlan()
	results := []RunEffectReplayResult{
		{
			StepID: "step-1",
			Step:   1,
			Node:   "search",
			Index:  0,
			Result: EffectReplayResult{
				EffectID:     "tool-1",
				Action:       EffectReplayActionRecordOnly,
				ReplayPolicy: EffectReplayRecordOnly,
			},
		},
		{
			StepID: "step-2",
			Step:   2,
			Node:   "verify",
			Index:  1,
			Result: EffectReplayResult{
				EffectID:     "verify-1",
				Action:       EffectReplayActionReplay,
				ReplayPolicy: EffectReplayIdempotent,
			},
		},
	}

	recorder := NewVerificationRecorder()
	err := RecordRunEffectReplayCheck(recorder, RunEffectReplaySnapshot{
		Plan:    plan,
		Results: results,
	})
	if err != nil {
		t.Fatalf("RecordRunEffectReplayCheck() error = %v", err)
	}

	check := singleReplayCheck(t, recorder)
	if check.ID != VerificationCheckRunEffectReplay+":run-1" {
		t.Fatalf("check ID = %q, want run effect replay check", check.ID)
	}
	if check.Status != VerificationStatusPassed {
		t.Fatalf("check status = %q, want passed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeRunEffectReplay {
		t.Fatalf("check evidence = %+v, want run effect replay evidence", check.Evidence)
	}
	assertReplayMetadata(t, check.Metadata, 2, 1, 1, 0, 2)
}

func TestRecordRunEffectReplayCheckPreservesCanonicalMetadata(t *testing.T) {
	plan := runEffectReplayVerificationPlan()
	results := []RunEffectReplayResult{
		{
			StepID: "step-1",
			Step:   1,
			Node:   "search",
			Index:  0,
			Result: EffectReplayResult{
				EffectID:     "tool-1",
				Action:       EffectReplayActionRecordOnly,
				ReplayPolicy: EffectReplayRecordOnly,
			},
		},
		{
			StepID: "step-2",
			Step:   2,
			Node:   "verify",
			Index:  1,
			Result: EffectReplayResult{
				EffectID:     "verify-1",
				Action:       EffectReplayActionReplay,
				ReplayPolicy: EffectReplayIdempotent,
			},
		},
	}

	recorder := NewVerificationRecorder()
	err := RecordRunEffectReplayCheck(recorder, RunEffectReplaySnapshot{
		Plan:    plan,
		Results: results,
		Metadata: map[string]any{
			"ref":               "forged-ref",
			"decision_count":    999,
			"replay_count":      999,
			"record_only_count": 999,
			"skip_count":        999,
			"result_count":      999,
			"run_id":            "forged-run",
			"thread_id":         "forged-thread",
			"planned_effect_ids": []string{
				"forged-plan",
			},
			"result_effect_ids": []string{
				"forged-result",
			},
			"source": "resume",
		},
	})
	if err != nil {
		t.Fatalf("RecordRunEffectReplayCheck() error = %v", err)
	}

	metadata := singleReplayCheck(t, recorder).Metadata
	if metadata["ref"] != "run-1" ||
		metadata["decision_count"] != 2 ||
		metadata["replay_count"] != 1 ||
		metadata["record_only_count"] != 1 ||
		metadata["skip_count"] != 0 ||
		metadata["result_count"] != 2 ||
		metadata["run_id"] != "run-1" ||
		metadata["thread_id"] != "thread-1" {
		t.Fatalf("metadata = %+v, want canonical run effect replay fields preserved", metadata)
	}
	assertStringSliceMetadata(t, metadata, "planned_effect_ids", []string{"tool-1", "verify-1"})
	assertStringSliceMetadata(t, metadata, "result_effect_ids", []string{"tool-1", "verify-1"})
	if metadata["source"] != "resume" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}
}

func TestRecordRunEffectReplayCheckFailsIncompleteReplay(t *testing.T) {
	plan := runEffectReplayVerificationPlan()
	results := []RunEffectReplayResult{
		{
			StepID: "step-1",
			Step:   1,
			Node:   "search",
			Index:  0,
			Result: EffectReplayResult{
				EffectID:     "tool-1",
				Action:       EffectReplayActionRecordOnly,
				ReplayPolicy: EffectReplayRecordOnly,
			},
		},
	}

	recorder := NewVerificationRecorder()
	err := RecordRunEffectReplayCheck(recorder, RunEffectReplaySnapshot{
		Plan:    plan,
		Results: results,
	})
	if !errors.Is(err, ErrEffectReplayVerificationFailed) {
		t.Fatalf("RecordRunEffectReplayCheck() error = %v, want replay verification failure", err)
	}

	check := singleReplayCheck(t, recorder)
	if check.Status != VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["missing_result_count"] != 1 {
		t.Fatalf("check metadata = %+v, want one missing result", check.Metadata)
	}
}

func TestRecordRunEffectReplayCheckSkipsEmptyPlan(t *testing.T) {
	recorder := NewVerificationRecorder()
	err := RecordRunEffectReplayCheck(recorder, RunEffectReplaySnapshot{
		Plan: RunEffectReplayPlan{RunID: "run-1", ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("RecordRunEffectReplayCheck(empty) error = %v", err)
	}

	check := singleReplayCheck(t, recorder)
	if check.Status != VerificationStatusSkipped {
		t.Fatalf("check status = %q, want skipped", check.Status)
	}
	assertReplayMetadata(t, check.Metadata, 0, 0, 0, 0, 0)
}

func effectReplayVerificationPlan() EffectReplayPlan {
	return EffectReplayPlan{
		StepID:          "step-1",
		Step:            1,
		Node:            "verify",
		ReplayCount:     1,
		RecordOnlyCount: 1,
		Decisions: []EffectReplayDecision{
			{
				Effect:       EffectRecord{ID: "tool-1", Type: "tool_call"},
				Action:       EffectReplayActionRecordOnly,
				ReplayPolicy: EffectReplayRecordOnly,
			},
			{
				Effect:         EffectRecord{ID: "verify-1", Type: "sandbox_exec"},
				Action:         EffectReplayActionReplay,
				ReplayPolicy:   EffectReplayIdempotent,
				IdempotencyKey: "verify:1",
			},
		},
	}
}

func runEffectReplayVerificationPlan() RunEffectReplayPlan {
	return RunEffectReplayPlan{
		RunID:           "run-1",
		ThreadID:        "thread-1",
		ReplayCount:     1,
		RecordOnlyCount: 1,
		Decisions: []RunEffectReplayDecision{
			{
				StepID: "step-1",
				Step:   1,
				Node:   "search",
				Index:  0,
				Decision: EffectReplayDecision{
					Effect:       EffectRecord{ID: "tool-1", Type: "tool_call"},
					Action:       EffectReplayActionRecordOnly,
					ReplayPolicy: EffectReplayRecordOnly,
				},
			},
			{
				StepID: "step-2",
				Step:   2,
				Node:   "verify",
				Index:  1,
				Decision: EffectReplayDecision{
					Effect:         EffectRecord{ID: "verify-1", Type: "sandbox_exec"},
					Action:         EffectReplayActionReplay,
					ReplayPolicy:   EffectReplayIdempotent,
					IdempotencyKey: "verify:1",
				},
			},
		},
	}
}

func singleReplayCheck(t *testing.T, recorder *VerificationRecorder) VerificationCheck {
	t.Helper()
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("recorded checks = %d, want 1", len(checks))
	}
	return checks[0]
}

func assertStepReplayMetadata(t *testing.T, metadata map[string]any, decisionCount int, replayCount int, recordOnlyCount int, skipCount int, resultCount int) {
	t.Helper()
	if metadata["decision_count"] != decisionCount {
		t.Fatalf("metadata decision_count = %v, want %d", metadata["decision_count"], decisionCount)
	}
	if metadata["replay_count"] != replayCount {
		t.Fatalf("metadata replay_count = %v, want %d", metadata["replay_count"], replayCount)
	}
	if metadata["record_only_count"] != recordOnlyCount {
		t.Fatalf("metadata record_only_count = %v, want %d", metadata["record_only_count"], recordOnlyCount)
	}
	if metadata["skip_count"] != skipCount {
		t.Fatalf("metadata skip_count = %v, want %d", metadata["skip_count"], skipCount)
	}
	if metadata["result_count"] != resultCount {
		t.Fatalf("metadata result_count = %v, want %d", metadata["result_count"], resultCount)
	}
	if metadata["step_id"] != "step-1" {
		t.Fatalf("metadata step_id = %v, want step-1", metadata["step_id"])
	}
	if metadata["node"] != "verify" {
		t.Fatalf("metadata node = %v, want verify", metadata["node"])
	}
}

func assertReplayMetadata(t *testing.T, metadata map[string]any, decisionCount int, replayCount int, recordOnlyCount int, skipCount int, resultCount int) {
	t.Helper()
	if metadata["decision_count"] != decisionCount {
		t.Fatalf("metadata decision_count = %v, want %d", metadata["decision_count"], decisionCount)
	}
	if metadata["replay_count"] != replayCount {
		t.Fatalf("metadata replay_count = %v, want %d", metadata["replay_count"], replayCount)
	}
	if metadata["record_only_count"] != recordOnlyCount {
		t.Fatalf("metadata record_only_count = %v, want %d", metadata["record_only_count"], recordOnlyCount)
	}
	if metadata["skip_count"] != skipCount {
		t.Fatalf("metadata skip_count = %v, want %d", metadata["skip_count"], skipCount)
	}
	if metadata["result_count"] != resultCount {
		t.Fatalf("metadata result_count = %v, want %d", metadata["result_count"], resultCount)
	}
	if metadata["run_id"] != "run-1" {
		t.Fatalf("metadata run_id = %v, want run-1", metadata["run_id"])
	}
}

func assertStringSliceMetadata(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()
	got, ok := metadata[key].([]string)
	if !ok {
		t.Fatalf("metadata[%q] = %#v, want []string", key, metadata[key])
	}
	if len(got) != len(want) {
		t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
		}
	}
}
