package memory

import (
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

var ErrReplayVerificationFailed = errors.New("memory: replay verification failed")

const (
	// VerificationCheckMemoryReplay is the standard check ID prefix for memory replay work.
	VerificationCheckMemoryReplay = "memory-replay"

	// VerificationEvidenceTypeMemoryReplay is the evidence type for observed memory replay work.
	VerificationEvidenceTypeMemoryReplay = "memory_replay"
)

// ReplayVerificationSnapshot is already-observed memory replay work.
type ReplayVerificationSnapshot struct {
	ID       string
	Name     string
	Ref      string
	Plan     gopact.RunEffectReplayPlan
	Results  []gopact.RunEffectReplayResult
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordReplayCheck records already-observed memory replay work as verification evidence.
func RecordReplayCheck(recorder *gopact.VerificationRecorder, snapshot ReplayVerificationSnapshot) error {
	if recorder == nil {
		return errors.New("memory: verification recorder is nil")
	}

	check := replayVerificationCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrReplayVerificationFailed, snapshot.Err)
		}
		return ErrReplayVerificationFailed
	}
	return nil
}

func replayVerificationCheck(snapshot ReplayVerificationSnapshot) gopact.VerificationCheck {
	ref := replayVerificationRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckMemoryReplay + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "memory replay"
	}
	status := replayVerificationStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = replayVerificationSummary(status, snapshot)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeMemoryReplay,
				Ref:      ref,
				Summary:  replayVerificationEvidenceSummary(status, snapshot),
				Metadata: replayVerificationEvidenceMetadata(snapshot),
			},
		},
		Metadata: replayVerificationCheckMetadata(snapshot),
	}
}

func replayVerificationStatus(snapshot ReplayVerificationSnapshot) gopact.VerificationStatus {
	if snapshot.Skipped || (snapshot.Plan.ReplayCount == 0 && len(snapshot.Results) == 0 && snapshot.Err == nil) {
		return gopact.VerificationStatusSkipped
	}
	if snapshot.Err != nil || replayResultMismatch(snapshot) {
		return gopact.VerificationStatusFailed
	}
	return gopact.VerificationStatusPassed
}

func replayResultMismatch(snapshot ReplayVerificationSnapshot) bool {
	return snapshot.Plan.ReplayCount != len(snapshot.Results)
}

func replayVerificationSummary(status gopact.VerificationStatus, snapshot ReplayVerificationSnapshot) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "memory replay skipped"
	case gopact.VerificationStatusFailed:
		if snapshot.Err != nil {
			return "memory replay failed: " + snapshot.Err.Error()
		}
		return "memory replay incomplete"
	default:
		if snapshot.Plan.ReplayCount == 1 {
			return "memory replay completed with 1 effect"
		}
		return fmt.Sprintf("memory replay completed with %d effects", snapshot.Plan.ReplayCount)
	}
}

func replayVerificationEvidenceSummary(status gopact.VerificationStatus, snapshot ReplayVerificationSnapshot) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if replayResultMismatch(snapshot) {
		return fmt.Sprintf("%d planned, %d results", snapshot.Plan.ReplayCount, len(snapshot.Results))
	}
	if len(snapshot.Results) == 1 {
		return "1 replay result"
	}
	return fmt.Sprintf("%d replay results", len(snapshot.Results))
}

func replayVerificationCheckMetadata(snapshot ReplayVerificationSnapshot) map[string]any {
	metadata := replayVerificationBaseMetadata(snapshot)
	mergeReplayVerificationMetadata(metadata, snapshot.Metadata)
	return metadata
}

func replayVerificationEvidenceMetadata(snapshot ReplayVerificationSnapshot) map[string]any {
	return replayVerificationCheckMetadata(snapshot)
}

func replayVerificationBaseMetadata(snapshot ReplayVerificationSnapshot) map[string]any {
	metadata := map[string]any{
		"ref":            replayVerificationRef(snapshot),
		"decision_count": len(snapshot.Plan.Decisions),
		"replay_count":   snapshot.Plan.ReplayCount,
		"result_count":   len(snapshot.Results),
	}
	if snapshot.Plan.RunID != "" {
		metadata["run_id"] = snapshot.Plan.RunID
	}
	if snapshot.Plan.ThreadID != "" {
		metadata["thread_id"] = snapshot.Plan.ThreadID
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if missing := snapshot.Plan.ReplayCount - len(snapshot.Results); missing > 0 {
		metadata["missing_result_count"] = missing
	}
	if extra := len(snapshot.Results) - snapshot.Plan.ReplayCount; extra > 0 {
		metadata["extra_result_count"] = extra
	}
	if ids := replayPlanEffectIDs(snapshot.Plan); len(ids) > 0 {
		metadata["planned_effect_ids"] = ids
	}
	if ids := replayResultEffectIDs(snapshot.Results); len(ids) > 0 {
		metadata["result_effect_ids"] = ids
	}
	return metadata
}

func mergeReplayVerificationMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if replayVerificationReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func replayVerificationReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"decision_count",
		"replay_count",
		"result_count",
		"run_id",
		"thread_id",
		"skipped",
		"error",
		"missing_result_count",
		"extra_result_count",
		"planned_effect_ids",
		"result_effect_ids":
		return true
	default:
		return false
	}
}

func replayVerificationRef(snapshot ReplayVerificationSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.Plan.RunID != "" {
		return snapshot.Plan.RunID
	}
	if snapshot.Plan.ThreadID != "" {
		return snapshot.Plan.ThreadID
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckMemoryReplay
}

func replayPlanEffectIDs(plan gopact.RunEffectReplayPlan) []string {
	if len(plan.Decisions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if id := decision.Decision.Effect.ID; id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func replayResultEffectIDs(results []gopact.RunEffectReplayResult) []string {
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		if result.Result.EffectID != "" {
			ids = append(ids, result.Result.EffectID)
		}
	}
	return ids
}
