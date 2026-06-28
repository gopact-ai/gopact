package memory

import (
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRecordReplayCheckRecordsPassedMemoryReplay(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	plan := gopact.RunEffectReplayPlan{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		ReplayCount: 1,
		Decisions: []gopact.RunEffectReplayDecision{
			{
				StepID: "step-1",
				Step:   1,
				Node:   "call_model",
				Decision: gopact.EffectReplayDecision{
					Effect:       gopact.EffectRecord{ID: "memory-1", Type: EffectTypeMemoryPut},
					Action:       gopact.EffectReplayActionReplay,
					ReplayPolicy: gopact.EffectReplayIdempotent,
				},
			},
		},
	}
	results := []gopact.RunEffectReplayResult{
		{
			StepID: "step-1",
			Step:   1,
			Node:   "call_model",
			Result: gopact.EffectReplayResult{
				EffectID: "memory-1",
				Action:   gopact.EffectReplayActionReplay,
				Metadata: map[string]any{
					EffectReplayMetadataMemoryID: ID("memory-1"),
				},
			},
		},
	}

	if err := RecordReplayCheck(recorder, ReplayVerificationSnapshot{
		Plan:    plan,
		Results: results,
	}); err != nil {
		t.Fatalf("RecordReplayCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one check", checks)
	}
	check := checks[0]
	if check.ID != VerificationCheckMemoryReplay+":run-1" || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed memory replay check", check)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeMemoryReplay || check.Evidence[0].Ref != "run-1" {
		t.Fatalf("evidence = %+v, want memory replay evidence", check.Evidence)
	}
	if check.Metadata["replay_count"] != 1 || check.Metadata["result_count"] != 1 || check.Metadata["thread_id"] != "thread-1" {
		t.Fatalf("metadata = %+v, want replay/result/thread metadata", check.Metadata)
	}
}

func TestRecordReplayCheckPreservesCanonicalMetadata(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	plan := gopact.RunEffectReplayPlan{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		ReplayCount: 1,
		Decisions: []gopact.RunEffectReplayDecision{
			{
				StepID: "step-1",
				Step:   1,
				Node:   "call_model",
				Decision: gopact.EffectReplayDecision{
					Effect:       gopact.EffectRecord{ID: "memory-1", Type: EffectTypeMemoryPut},
					Action:       gopact.EffectReplayActionReplay,
					ReplayPolicy: gopact.EffectReplayIdempotent,
				},
			},
		},
	}
	results := []gopact.RunEffectReplayResult{
		{
			StepID: "step-1",
			Step:   1,
			Node:   "call_model",
			Result: gopact.EffectReplayResult{
				EffectID: "memory-1",
				Action:   gopact.EffectReplayActionReplay,
			},
		},
	}

	err := RecordReplayCheck(recorder, ReplayVerificationSnapshot{
		Plan:    plan,
		Results: results,
		Metadata: map[string]any{
			"ref":                "forged-ref",
			"decision_count":     999,
			"replay_count":       999,
			"result_count":       999,
			"run_id":             "forged-run",
			"thread_id":          "forged-thread",
			"planned_effect_ids": []string{"forged-plan"},
			"result_effect_ids":  []string{"forged-result"},
			"planned_step_ids":   []string{"forged-step"},
			"result_step_ids":    []string{"forged-result-step"},
			"source":             "worker",
		},
	})
	if err != nil {
		t.Fatalf("RecordReplayCheck() error = %v", err)
	}

	check := recorder.Checks()[0]
	metadata := check.Metadata
	if metadata["ref"] != "run-1" ||
		metadata["decision_count"] != 1 ||
		metadata["replay_count"] != 1 ||
		metadata["result_count"] != 1 ||
		metadata["run_id"] != "run-1" ||
		metadata["thread_id"] != "thread-1" {
		t.Fatalf("metadata = %+v, want canonical memory replay fields preserved", metadata)
	}
	assertStringSliceMetadata(t, metadata, "planned_effect_ids", []string{"memory-1"})
	assertStringSliceMetadata(t, metadata, "result_effect_ids", []string{"memory-1"})
	assertStringSliceMetadata(t, metadata, "planned_step_ids", []string{"step-1"})
	assertStringSliceMetadata(t, metadata, "result_step_ids", []string{"step-1"})
	if metadata["source"] != "worker" {
		t.Fatalf("metadata = %+v, want supplemental metadata preserved", metadata)
	}

	evidenceMetadata := check.Evidence[0].Metadata
	if evidenceMetadata["ref"] != "run-1" ||
		evidenceMetadata["decision_count"] != 1 ||
		evidenceMetadata["replay_count"] != 1 ||
		evidenceMetadata["result_count"] != 1 ||
		evidenceMetadata["run_id"] != "run-1" ||
		evidenceMetadata["thread_id"] != "thread-1" {
		t.Fatalf("evidence metadata = %+v, want canonical memory replay fields preserved", evidenceMetadata)
	}
	assertStringSliceMetadata(t, evidenceMetadata, "planned_effect_ids", []string{"memory-1"})
	assertStringSliceMetadata(t, evidenceMetadata, "result_effect_ids", []string{"memory-1"})
	assertStringSliceMetadata(t, evidenceMetadata, "planned_step_ids", []string{"step-1"})
	assertStringSliceMetadata(t, evidenceMetadata, "result_step_ids", []string{"step-1"})
	if evidenceMetadata["source"] != "worker" {
		t.Fatalf("evidence metadata = %+v, want supplemental metadata preserved", evidenceMetadata)
	}
}

func TestRecordReplayCheckRecordsFailedBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()
	replayErr := errors.New("worker failed")

	err := RecordReplayCheck(recorder, ReplayVerificationSnapshot{
		Plan: gopact.RunEffectReplayPlan{
			RunID:       "run-1",
			ThreadID:    "thread-1",
			ReplayCount: 1,
		},
		Err: replayErr,
	})
	if !errors.Is(err, ErrReplayVerificationFailed) || !errors.Is(err, replayErr) {
		t.Fatalf("RecordReplayCheck() error = %v, want replay verification failure joined with worker error", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}
	if checks[0].Evidence[0].Metadata["error"] != replayErr.Error() {
		t.Fatalf("evidence metadata = %+v, want error", checks[0].Evidence[0].Metadata)
	}
}

func TestRecordReplayCheckFailsWhenReplayResultsAreMissing(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordReplayCheck(recorder, ReplayVerificationSnapshot{
		Plan: gopact.RunEffectReplayPlan{
			RunID:       "run-1",
			ReplayCount: 2,
		},
		Results: []gopact.RunEffectReplayResult{{Result: gopact.EffectReplayResult{EffectID: "memory-1"}}},
	})
	if !errors.Is(err, ErrReplayVerificationFailed) {
		t.Fatalf("RecordReplayCheck() error = %v, want ErrReplayVerificationFailed", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusFailed {
		t.Fatalf("checks = %+v, want one failed check", checks)
	}
	if checks[0].Metadata["missing_result_count"] != 1 {
		t.Fatalf("metadata = %+v, want missing result count", checks[0].Metadata)
	}
}

func TestRecordReplayCheckRejectsNilRecorder(t *testing.T) {
	if err := RecordReplayCheck(nil, ReplayVerificationSnapshot{}); err == nil {
		t.Fatal("RecordReplayCheck(nil) error = nil, want error")
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
