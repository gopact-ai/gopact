package gopact

import (
	"encoding/json"
	"errors"
	"fmt"
)

var ErrEffectReplayVerificationFailed = errors.New("gopact: effect replay verification failed")

const (
	// VerificationCheckEffectReplay is the standard check ID prefix for step-level effect replay work.
	VerificationCheckEffectReplay = "effect-replay"

	// VerificationCheckRunEffectReplay is the standard check ID prefix for run-level effect replay work.
	VerificationCheckRunEffectReplay = "run-effect-replay"

	// VerificationEvidenceTypeEffectReplay is the evidence type for observed step-level effect replay work.
	VerificationEvidenceTypeEffectReplay = "effect_replay"

	// VerificationEvidenceTypeRunEffectReplay is the evidence type for observed run-level effect replay work.
	VerificationEvidenceTypeRunEffectReplay = "run_effect_replay"
)

// EffectReplaySnapshot is already-observed step-level effect replay work.
type EffectReplaySnapshot struct {
	ID       string
	Name     string
	Ref      string
	Plan     EffectReplayPlan
	Results  []EffectReplayResult
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RunEffectReplaySnapshot is already-observed run-level effect replay work.
type RunEffectReplaySnapshot struct {
	ID       string
	Name     string
	Ref      string
	Plan     RunEffectReplayPlan
	Results  []RunEffectReplayResult
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// EventEffectReplayPlan returns the effect replay plan carried by an event.
func EventEffectReplayPlan(event Event) (EffectReplayPlan, bool) {
	if len(event.Metadata) == 0 {
		return EffectReplayPlan{}, false
	}

	switch plan := event.Metadata[EventMetadataEffectReplayPlan].(type) {
	case EffectReplayPlan:
		return copyEffectReplayPlan(plan), true
	case *EffectReplayPlan:
		if plan == nil {
			return EffectReplayPlan{}, false
		}
		return copyEffectReplayPlan(*plan), true
	default:
		return decodeEffectReplayPlanMetadata(plan)
	}
}

// EffectReplaySnapshotFromEvent builds verification input from an event carrying an effect replay plan.
func EffectReplaySnapshotFromEvent(event Event, results []EffectReplayResult, replayErr error) (EffectReplaySnapshot, bool) {
	plan, ok := EventEffectReplayPlan(event)
	if !ok {
		return EffectReplaySnapshot{}, false
	}

	metadata := map[string]any{}
	if event.Type != "" {
		metadata["event_type"] = string(event.Type)
	}
	ids := event.RuntimeIDs()
	if ids.RunID != "" {
		metadata["run_id"] = ids.RunID
	}
	if ids.ThreadID != "" {
		metadata["thread_id"] = ids.ThreadID
	}
	if event.Node != "" {
		metadata["event_node"] = event.Node
	}
	if event.Step != 0 {
		metadata["event_step"] = event.Step
	}
	if event.StepSnapshot != nil && event.StepSnapshot.ID != "" {
		metadata["snapshot_step_id"] = event.StepSnapshot.ID
	}

	return EffectReplaySnapshot{
		Plan:     plan,
		Results:  copyEffectReplayResults(results),
		Err:      replayErr,
		Metadata: metadata,
	}, true
}

// RecordEffectReplayCheck records already-observed step-level effect replay work as verification evidence.
func RecordEffectReplayCheck(recorder *VerificationRecorder, snapshot EffectReplaySnapshot) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}

	check := effectReplayCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrEffectReplayVerificationFailed, snapshot.Err)
		}
		return ErrEffectReplayVerificationFailed
	}
	return nil
}

// RecordRunEffectReplayCheck records already-observed run-level effect replay work as verification evidence.
func RecordRunEffectReplayCheck(recorder *VerificationRecorder, snapshot RunEffectReplaySnapshot) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}

	check := runEffectReplayCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrEffectReplayVerificationFailed, snapshot.Err)
		}
		return ErrEffectReplayVerificationFailed
	}
	return nil
}

func effectReplayCheck(snapshot EffectReplaySnapshot) VerificationCheck {
	ref := effectReplayRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckEffectReplay + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "effect replay"
	}
	status := effectReplayStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = effectReplaySummary(status, snapshot)
	}
	return VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeEffectReplay,
				Ref:      ref,
				Summary:  effectReplayEvidenceSummary(status, snapshot),
				Metadata: effectReplayEvidenceMetadata(snapshot),
			},
		},
		Metadata: effectReplayCheckMetadata(snapshot),
	}
}

func runEffectReplayCheck(snapshot RunEffectReplaySnapshot) VerificationCheck {
	ref := runEffectReplayRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckRunEffectReplay + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "run effect replay"
	}
	status := runEffectReplayStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = runEffectReplaySummary(status, snapshot)
	}
	return VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeRunEffectReplay,
				Ref:      ref,
				Summary:  runEffectReplayEvidenceSummary(status, snapshot),
				Metadata: runEffectReplayEvidenceMetadata(snapshot),
			},
		},
		Metadata: runEffectReplayCheckMetadata(snapshot),
	}
}

func effectReplayStatus(snapshot EffectReplaySnapshot) VerificationStatus {
	if snapshot.Skipped || (len(snapshot.Plan.Decisions) == 0 && len(snapshot.Results) == 0 && snapshot.Err == nil) {
		return VerificationStatusSkipped
	}
	if snapshot.Err != nil || effectReplayResultMismatch(snapshot) {
		return VerificationStatusFailed
	}
	return VerificationStatusPassed
}

func runEffectReplayStatus(snapshot RunEffectReplaySnapshot) VerificationStatus {
	if snapshot.Skipped || (len(snapshot.Plan.Decisions) == 0 && len(snapshot.Results) == 0 && snapshot.Err == nil) {
		return VerificationStatusSkipped
	}
	if snapshot.Err != nil || runEffectReplayResultMismatch(snapshot) {
		return VerificationStatusFailed
	}
	return VerificationStatusPassed
}

func effectReplayResultMismatch(snapshot EffectReplaySnapshot) bool {
	return len(snapshot.Plan.Decisions) != len(snapshot.Results)
}

func runEffectReplayResultMismatch(snapshot RunEffectReplaySnapshot) bool {
	return len(snapshot.Plan.Decisions) != len(snapshot.Results)
}

func effectReplaySummary(status VerificationStatus, snapshot EffectReplaySnapshot) string {
	switch status {
	case VerificationStatusSkipped:
		return "effect replay skipped"
	case VerificationStatusFailed:
		if snapshot.Err != nil {
			return "effect replay failed: " + snapshot.Err.Error()
		}
		return "effect replay incomplete"
	default:
		if len(snapshot.Plan.Decisions) == 1 {
			return "effect replay completed with 1 decision"
		}
		return fmt.Sprintf("effect replay completed with %d decisions", len(snapshot.Plan.Decisions))
	}
}

func runEffectReplaySummary(status VerificationStatus, snapshot RunEffectReplaySnapshot) string {
	switch status {
	case VerificationStatusSkipped:
		return "run effect replay skipped"
	case VerificationStatusFailed:
		if snapshot.Err != nil {
			return "run effect replay failed: " + snapshot.Err.Error()
		}
		return "run effect replay incomplete"
	default:
		if len(snapshot.Plan.Decisions) == 1 {
			return "run effect replay completed with 1 decision"
		}
		return fmt.Sprintf("run effect replay completed with %d decisions", len(snapshot.Plan.Decisions))
	}
}

func effectReplayEvidenceSummary(status VerificationStatus, snapshot EffectReplaySnapshot) string {
	if status == VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if effectReplayResultMismatch(snapshot) {
		return fmt.Sprintf("%d planned, %d results", len(snapshot.Plan.Decisions), len(snapshot.Results))
	}
	if len(snapshot.Results) == 1 {
		return "1 replay decision result"
	}
	return fmt.Sprintf("%d replay decision results", len(snapshot.Results))
}

func runEffectReplayEvidenceSummary(status VerificationStatus, snapshot RunEffectReplaySnapshot) string {
	if status == VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if runEffectReplayResultMismatch(snapshot) {
		return fmt.Sprintf("%d planned, %d results", len(snapshot.Plan.Decisions), len(snapshot.Results))
	}
	if len(snapshot.Results) == 1 {
		return "1 replay decision result"
	}
	return fmt.Sprintf("%d replay decision results", len(snapshot.Results))
}

func effectReplayCheckMetadata(snapshot EffectReplaySnapshot) map[string]any {
	metadata := effectReplayBaseMetadata(snapshot)
	mergeSupplementalVerificationMetadata(metadata, snapshot.Metadata, effectReplayReservedMetadataKey)
	return metadata
}

func runEffectReplayCheckMetadata(snapshot RunEffectReplaySnapshot) map[string]any {
	metadata := runEffectReplayBaseMetadata(snapshot)
	mergeSupplementalVerificationMetadata(metadata, snapshot.Metadata, runEffectReplayReservedMetadataKey)
	return metadata
}

func effectReplayEvidenceMetadata(snapshot EffectReplaySnapshot) map[string]any {
	return effectReplayCheckMetadata(snapshot)
}

func runEffectReplayEvidenceMetadata(snapshot RunEffectReplaySnapshot) map[string]any {
	return runEffectReplayCheckMetadata(snapshot)
}

func effectReplayBaseMetadata(snapshot EffectReplaySnapshot) map[string]any {
	metadata := map[string]any{
		"ref":               effectReplayRef(snapshot),
		"decision_count":    len(snapshot.Plan.Decisions),
		"replay_count":      snapshot.Plan.ReplayCount,
		"skip_count":        snapshot.Plan.SkipCount,
		"record_only_count": snapshot.Plan.RecordOnlyCount,
		"result_count":      len(snapshot.Results),
	}
	if snapshot.Plan.StepID != "" {
		metadata["step_id"] = snapshot.Plan.StepID
	}
	if snapshot.Plan.Step != 0 {
		metadata["step"] = snapshot.Plan.Step
	}
	if snapshot.Plan.Node != "" {
		metadata["node"] = snapshot.Plan.Node
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if missing := len(snapshot.Plan.Decisions) - len(snapshot.Results); missing > 0 {
		metadata["missing_result_count"] = missing
	}
	if extra := len(snapshot.Results) - len(snapshot.Plan.Decisions); extra > 0 {
		metadata["extra_result_count"] = extra
	}
	if ids := effectReplayPlannedEffectIDs(snapshot.Plan); len(ids) > 0 {
		metadata["planned_effect_ids"] = ids
	}
	if ids := effectReplayResultEffectIDs(snapshot.Results); len(ids) > 0 {
		metadata["result_effect_ids"] = ids
	}
	return metadata
}

func runEffectReplayBaseMetadata(snapshot RunEffectReplaySnapshot) map[string]any {
	metadata := map[string]any{
		"ref":               runEffectReplayRef(snapshot),
		"decision_count":    len(snapshot.Plan.Decisions),
		"replay_count":      snapshot.Plan.ReplayCount,
		"skip_count":        snapshot.Plan.SkipCount,
		"record_only_count": snapshot.Plan.RecordOnlyCount,
		"result_count":      len(snapshot.Results),
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
	if missing := len(snapshot.Plan.Decisions) - len(snapshot.Results); missing > 0 {
		metadata["missing_result_count"] = missing
	}
	if extra := len(snapshot.Results) - len(snapshot.Plan.Decisions); extra > 0 {
		metadata["extra_result_count"] = extra
	}
	if ids := runEffectReplayPlannedEffectIDs(snapshot.Plan); len(ids) > 0 {
		metadata["planned_effect_ids"] = ids
	}
	if ids := runEffectReplayResultEffectIDs(snapshot.Results); len(ids) > 0 {
		metadata["result_effect_ids"] = ids
	}
	return metadata
}

func effectReplayReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"decision_count",
		"replay_count",
		"skip_count",
		"record_only_count",
		"result_count",
		"step_id",
		"step",
		"node",
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

func runEffectReplayReservedMetadataKey(key string) bool {
	switch key {
	case "run_id", "thread_id":
		return true
	default:
		return effectReplayReservedMetadataKey(key)
	}
}

func effectReplayRef(snapshot EffectReplaySnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.Plan.StepID != "" {
		return snapshot.Plan.StepID
	}
	if snapshot.Plan.Node != "" {
		return snapshot.Plan.Node
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckEffectReplay
}

func runEffectReplayRef(snapshot RunEffectReplaySnapshot) string {
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
	return VerificationCheckRunEffectReplay
}

func effectReplayPlannedEffectIDs(plan EffectReplayPlan) []string {
	if len(plan.Decisions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if id := decision.Effect.ID; id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func runEffectReplayPlannedEffectIDs(plan RunEffectReplayPlan) []string {
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

func effectReplayResultEffectIDs(results []EffectReplayResult) []string {
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		if result.EffectID != "" {
			ids = append(ids, result.EffectID)
		}
	}
	return ids
}

func runEffectReplayResultEffectIDs(results []RunEffectReplayResult) []string {
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

func copyEffectReplayPlan(in EffectReplayPlan) EffectReplayPlan {
	out := in
	if len(in.Decisions) > 0 {
		out.Decisions = make([]EffectReplayDecision, len(in.Decisions))
		for i, decision := range in.Decisions {
			out.Decisions[i] = copyEffectReplayDecision(decision)
		}
	}
	return out
}

func copyEffectReplayDecision(in EffectReplayDecision) EffectReplayDecision {
	out := in
	out.Effect = copyEffectRecord(in.Effect)
	return out
}

func copyEffectReplayResults(in []EffectReplayResult) []EffectReplayResult {
	if len(in) == 0 {
		return nil
	}
	out := make([]EffectReplayResult, len(in))
	for i, result := range in {
		out[i] = result
		out[i].Effect = copyEffectRecord(result.Effect)
		out[i].Metadata = copyAnyMap(result.Metadata)
	}
	return out
}

func decodeEffectReplayPlanMetadata(raw any) (EffectReplayPlan, bool) {
	var data []byte
	switch value := raw.(type) {
	case json.RawMessage:
		data = append([]byte(nil), value...)
	case []byte:
		data = append([]byte(nil), value...)
	default:
		var err error
		data, err = json.Marshal(value)
		if err != nil {
			return EffectReplayPlan{}, false
		}
	}

	var plan EffectReplayPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return EffectReplayPlan{}, false
	}
	return copyEffectReplayPlan(plan), true
}
