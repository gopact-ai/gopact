package checkpoint

import (
	"errors"
	"fmt"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrVerificationCheckFailed is returned when checkpoint verification evidence records a failed check.
	ErrVerificationCheckFailed = errors.New("checkpoint: verification check failed")
	// ErrVerificationRecordRequired is returned when checkpoint verification has no record to inspect.
	ErrVerificationRecordRequired = errors.New("checkpoint: verification record is required")
)

const (
	// VerificationCheckCheckpoint is the standard check ID prefix for observed checkpoints.
	VerificationCheckCheckpoint = "checkpoint"

	// VerificationEvidenceTypeCheckpoint is the evidence type for observed checkpoint records.
	VerificationEvidenceTypeCheckpoint = "checkpoint"
)

// VerificationSnapshot is an already-observed checkpoint record.
type VerificationSnapshot struct {
	ID       string
	Name     string
	Ref      string
	Record   Record
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordVerificationCheck records an already-observed checkpoint as verification evidence.
func RecordVerificationCheck(recorder *gopact.VerificationRecorder, snapshot VerificationSnapshot) error {
	if recorder == nil {
		return errors.New("checkpoint: verification recorder is nil")
	}
	if !snapshot.Skipped && snapshot.Err == nil && !verificationSnapshotHasRef(snapshot) {
		return ErrVerificationRecordRequired
	}

	check := verificationCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrVerificationCheckFailed, snapshot.Err)
		}
		return ErrVerificationCheckFailed
	}
	return nil
}

func verificationCheck(snapshot VerificationSnapshot) gopact.VerificationCheck {
	ref := verificationRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckCheckpoint + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "checkpoint"
	}

	status := gopact.VerificationStatusPassed
	if snapshot.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if snapshot.Err != nil {
		status = gopact.VerificationStatusFailed
	}

	summary := snapshot.Summary
	if summary == "" {
		summary = verificationSummary(status, snapshot)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeCheckpoint,
				Ref:      ref,
				Summary:  verificationEvidenceSummary(status, snapshot),
				Metadata: verificationEvidenceMetadata(snapshot),
			},
		},
		Metadata: verificationCheckMetadata(snapshot),
	}
}

func verificationSummary(status gopact.VerificationStatus, snapshot VerificationSnapshot) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "checkpoint check skipped"
	case gopact.VerificationStatusFailed:
		if snapshot.Err != nil {
			return "checkpoint failed: " + snapshot.Err.Error()
		}
		return "checkpoint failed"
	default:
		return "checkpoint captured"
	}
}

func verificationEvidenceSummary(status gopact.VerificationStatus, snapshot VerificationSnapshot) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	if snapshot.Record.Step != 0 || snapshot.Record.Node != "" {
		return fmt.Sprintf("step %d %s", snapshot.Record.Step, snapshot.Record.Node)
	}
	return "checkpoint captured"
}

func verificationCheckMetadata(snapshot VerificationSnapshot) map[string]any {
	metadata := verificationBaseMetadata(snapshot)
	mergeVerificationMetadata(metadata, snapshot.Metadata)
	return metadata
}

func verificationEvidenceMetadata(snapshot VerificationSnapshot) map[string]any {
	return verificationCheckMetadata(snapshot)
}

func verificationBaseMetadata(snapshot VerificationSnapshot) map[string]any {
	record := snapshot.Record
	metadata := map[string]any{
		"ref": verificationRef(snapshot),
	}
	if record.ID != "" {
		metadata["checkpoint_id"] = record.ID
	}
	if record.SchemaVersion != "" {
		metadata["schema_version"] = record.SchemaVersion
	}
	addRuntimeIDMetadata(metadata, record.IDs)
	if threadID := verificationThreadID(record); threadID != "" {
		metadata["thread_id"] = threadID
	}
	if record.Step != 0 {
		metadata["step"] = record.Step
	}
	if record.Node != "" {
		metadata["node"] = record.Node
	}
	if record.Phase != "" {
		metadata["phase"] = string(record.Phase)
	}
	if record.StateCodec != "" {
		metadata["state_codec"] = record.StateCodec
	}
	if record.StateHash != "" {
		metadata["state_hash"] = record.StateHash
	}
	if len(record.State) > 0 || record.StateHash != "" || record.StateCodec != "" {
		metadata["state_size_bytes"] = len(record.State)
	}
	if len(record.Queue) > 0 {
		metadata["queue"] = append([]string(nil), record.Queue...)
		metadata["queue_count"] = len(record.Queue)
	}
	if record.Pending != nil {
		metadata["pending_interrupt_id"] = record.Pending.ID
		if record.Pending.Type != "" {
			metadata["pending_interrupt_type"] = string(record.Pending.Type)
		}
	}
	if len(record.Effects) > 0 {
		metadata["effect_count"] = len(record.Effects)
		metadata["artifact_count"] = verificationArtifactCount(record.Effects)
	}
	if record.ConfigVersion != "" {
		metadata["config_version"] = record.ConfigVersion
	}
	if !record.CreatedAt.IsZero() {
		metadata["created_at"] = record.CreatedAt.Format(time.RFC3339Nano)
	}
	if len(record.Metadata) > 0 {
		metadata["checkpoint_metadata"] = copyVerificationMetadata(record.Metadata)
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func addRuntimeIDMetadata(metadata map[string]any, ids gopact.RuntimeIDs) {
	if ids.UserID != "" {
		metadata["user_id"] = ids.UserID
	}
	if ids.SessionID != "" {
		metadata["session_id"] = ids.SessionID
	}
	if ids.ThreadID != "" {
		metadata["thread_id"] = ids.ThreadID
	}
	if ids.RunID != "" {
		metadata["run_id"] = ids.RunID
	}
	if ids.AgentID != "" {
		metadata["agent_id"] = ids.AgentID
	}
	if ids.AppID != "" {
		metadata["app_id"] = ids.AppID
	}
	if ids.CallID != "" {
		metadata["call_id"] = ids.CallID
	}
	if ids.ParentCallID != "" {
		metadata["parent_call_id"] = ids.ParentCallID
	}
	if ids.TraceID != "" {
		metadata["trace_id"] = ids.TraceID
	}
}

func mergeVerificationMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if verificationReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func verificationReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"checkpoint_id",
		"schema_version",
		"user_id",
		"session_id",
		"thread_id",
		"run_id",
		"agent_id",
		"app_id",
		"call_id",
		"parent_call_id",
		"trace_id",
		"step",
		"node",
		"phase",
		"state_codec",
		"state_hash",
		"state_size_bytes",
		"queue",
		"queue_count",
		"pending_interrupt_id",
		"pending_interrupt_type",
		"effect_count",
		"artifact_count",
		"config_version",
		"created_at",
		"checkpoint_metadata",
		"error",
		"skipped":
		return true
	default:
		return false
	}
}

func verificationThreadID(record Record) string {
	if record.ThreadID != "" {
		return record.ThreadID
	}
	return record.IDs.ThreadID
}

func verificationArtifactCount(effects []gopact.EffectRecord) int {
	var count int
	for _, effect := range effects {
		count += len(effect.Artifacts)
	}
	return count
}

func verificationRef(snapshot VerificationSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.Record.ID != "" {
		return snapshot.Record.ID
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return "checkpoint"
}

func verificationSnapshotHasRef(snapshot VerificationSnapshot) bool {
	return snapshot.Ref != "" || snapshot.Record.ID != "" || snapshot.ID != ""
}

func copyVerificationMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
