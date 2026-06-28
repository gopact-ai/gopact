package objectstore

import (
	"errors"
	"fmt"
	"sort"

	"github.com/gopact-ai/gopact"
)

var (
	ErrIndexConsistencyCheckFailed    = errors.New("checkpoint objectstore: index consistency check failed")
	ErrIndexConsistencyReportRequired = errors.New("checkpoint objectstore: index consistency report is required")
)

const (
	// VerificationCheckIndexConsistency is the standard check ID prefix for objectstore index consistency.
	VerificationCheckIndexConsistency = "checkpoint-objectstore-index"

	// VerificationEvidenceTypeIndexConsistency is the evidence type for objectstore index reports.
	VerificationEvidenceTypeIndexConsistency = "checkpoint_objectstore_index"
)

// IndexConsistencySnapshot is an already-observed objectstore index consistency result.
type IndexConsistencySnapshot struct {
	ID       string
	Name     string
	Ref      string
	Report   IndexConsistencyReport
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordIndexConsistencyCheck records an already-observed objectstore index report.
func RecordIndexConsistencyCheck(recorder *gopact.VerificationRecorder, snapshot IndexConsistencySnapshot) error {
	if recorder == nil {
		return errors.New("checkpoint objectstore: verification recorder is nil")
	}
	if !snapshot.Skipped && snapshot.Err == nil && snapshot.Report.ThreadID == "" {
		return ErrIndexConsistencyReportRequired
	}

	check := indexConsistencyCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrIndexConsistencyCheckFailed, snapshot.Err)
		}
		return ErrIndexConsistencyCheckFailed
	}
	return nil
}

func indexConsistencyCheck(snapshot IndexConsistencySnapshot) gopact.VerificationCheck {
	ref := indexConsistencyRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckIndexConsistency + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "checkpoint objectstore index"
	}

	status := gopact.VerificationStatusPassed
	if snapshot.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if snapshot.Err != nil || !snapshot.Report.Consistent {
		status = gopact.VerificationStatusFailed
	}

	summary := snapshot.Summary
	if summary == "" {
		summary = indexConsistencySummary(status, snapshot)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeIndexConsistency,
				Ref:      ref,
				Summary:  indexConsistencyEvidenceSummary(status, snapshot),
				Metadata: indexConsistencyCheckMetadata(snapshot),
			},
		},
		Metadata: indexConsistencyCheckMetadata(snapshot),
	}
}

func indexConsistencySummary(status gopact.VerificationStatus, snapshot IndexConsistencySnapshot) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "checkpoint objectstore index check skipped"
	case gopact.VerificationStatusFailed:
		if snapshot.Err != nil {
			return "checkpoint objectstore index failed: " + snapshot.Err.Error()
		}
		return "checkpoint objectstore index inconsistent"
	default:
		return "checkpoint objectstore index consistent"
	}
}

func indexConsistencyEvidenceSummary(status gopact.VerificationStatus, snapshot IndexConsistencySnapshot) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	report := snapshot.Report
	if report.Consistent {
		return "consistent"
	}
	return fmt.Sprintf(
		"duplicates=%d missing=%d wrong_thread=%d",
		len(report.DuplicateRecordIDs),
		len(report.MissingRecordIDs),
		len(report.WrongThreadRecordIDs),
	)
}

func indexConsistencyCheckMetadata(snapshot IndexConsistencySnapshot) map[string]any {
	metadata := indexConsistencyBaseMetadata(snapshot)
	if keys := sortedIndexConsistencyMetadataKeys(snapshot.Metadata); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeIndexConsistencyMetadata(metadata, snapshot.Metadata)
	return metadata
}

func indexConsistencyBaseMetadata(snapshot IndexConsistencySnapshot) map[string]any {
	report := snapshot.Report
	metadata := map[string]any{
		"ref": indexConsistencyRef(snapshot),
	}
	if indexConsistencyReportPresent(report) {
		if report.ThreadID != "" {
			metadata["thread_id"] = report.ThreadID
		}
		if report.IndexThreadID != "" {
			metadata["index_thread_id"] = report.IndexThreadID
		}
		metadata["index_exists"] = report.IndexExists
		metadata["consistent"] = report.Consistent
		metadata["repaired"] = report.Repaired
		metadata["thread_id_mismatch"] = report.ThreadIDMismatch
		addStringSliceMetadata(metadata, "indexed_record_ids", "indexed_record_count", report.IndexedRecordIDs)
		addStringSliceMetadata(metadata, "valid_record_ids", "valid_record_count", report.ValidRecordIDs)
		addStringSliceMetadata(metadata, "duplicate_record_ids", "duplicate_record_count", report.DuplicateRecordIDs)
		addStringSliceMetadata(metadata, "missing_record_ids", "missing_record_count", report.MissingRecordIDs)
		addStringSliceMetadata(metadata, "wrong_thread_record_ids", "wrong_thread_record_count", report.WrongThreadRecordIDs)
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func addStringSliceMetadata(metadata map[string]any, valuesKey string, countKey string, values []string) {
	if len(values) == 0 {
		metadata[countKey] = 0
		return
	}
	metadata[valuesKey] = append([]string(nil), values...)
	metadata[countKey] = len(values)
}

func mergeIndexConsistencyMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if indexConsistencyReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func sortedIndexConsistencyMetadataKeys(supplemental map[string]any) []string {
	if len(supplemental) == 0 {
		return nil
	}
	keys := make([]string, 0, len(supplemental))
	for key := range supplemental {
		if indexConsistencyReservedMetadataKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func indexConsistencyReservedMetadataKey(key string) bool {
	switch key {
	case "ref",
		"thread_id",
		"index_thread_id",
		"index_exists",
		"consistent",
		"repaired",
		"thread_id_mismatch",
		"indexed_record_ids",
		"indexed_record_count",
		"valid_record_ids",
		"valid_record_count",
		"duplicate_record_ids",
		"duplicate_record_count",
		"missing_record_ids",
		"missing_record_count",
		"wrong_thread_record_ids",
		"wrong_thread_record_count",
		"error",
		"metadata_keys",
		"skipped":
		return true
	default:
		return false
	}
}

func indexConsistencyReportPresent(report IndexConsistencyReport) bool {
	return report.ThreadID != "" ||
		report.IndexThreadID != "" ||
		report.IndexExists ||
		len(report.IndexedRecordIDs) > 0 ||
		len(report.ValidRecordIDs) > 0 ||
		len(report.DuplicateRecordIDs) > 0 ||
		len(report.MissingRecordIDs) > 0 ||
		len(report.WrongThreadRecordIDs) > 0 ||
		report.ThreadIDMismatch ||
		report.Consistent ||
		report.Repaired
}

func indexConsistencyRef(snapshot IndexConsistencySnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.Report.ThreadID != "" {
		return snapshot.Report.ThreadID
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckIndexConsistency
}
