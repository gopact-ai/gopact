package devagent

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// ReleaseBundleVersion is the current Dev Agent release evidence bundle schema version.
	ReleaseBundleVersion = 1
)

// ErrInvalidReleaseBundle reports inconsistent release evidence.
var ErrInvalidReleaseBundle = errors.New("devagent: invalid release bundle")

// ReleaseBundleInput contains already-observed Dev Agent release evidence.
type ReleaseBundleInput struct {
	Export        gopact.RunExport          `json:"run_export"`
	Report        gopact.VerificationReport `json:"verification_report"`
	EntropyAudits []gopact.EntropyAudit     `json:"entropy_audits,omitempty"`
	Review        ReviewDecision            `json:"review,omitempty"`
	Gate          GateResult                `json:"gate"`
	Action        ActionResult              `json:"action"`
	Patch         PatchProposal             `json:"patch,omitempty"`
	// Process optionally supplies already-observed sanitized process records.
	Process               ProcessRecords `json:"process,omitempty"`
	RequiredCheckIDs      []string       `json:"required_check_ids,omitempty"`
	RequiredEvidenceTypes []string       `json:"required_evidence_types,omitempty"`
	RequiredCIGates       []string       `json:"required_ci_gates,omitempty"`
	CreatedAt             time.Time      `json:"created_at,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

// ReleaseBundle is a portable proof package for an already-observed Dev Agent release decision.
type ReleaseBundle struct {
	Version               int                       `json:"version"`
	IDs                   gopact.RuntimeIDs         `json:"ids"`
	Mode                  Mode                      `json:"mode"`
	Outcome               gopact.RunOutcome         `json:"outcome"`
	RunExport             gopact.RunExport          `json:"run_export"`
	VerificationReport    gopact.VerificationReport `json:"verification_report"`
	EntropyAudits         []gopact.EntropyAudit     `json:"entropy_audits,omitempty"`
	Review                ReviewDecision            `json:"review,omitempty"`
	Gate                  GateResult                `json:"gate"`
	Action                ActionResult              `json:"action"`
	Process               ProcessRecords            `json:"process"`
	RequiredCheckIDs      []string                  `json:"required_check_ids,omitempty"`
	RequiredEvidenceTypes []string                  `json:"required_evidence_types,omitempty"`
	RequiredCIGates       []string                  `json:"required_ci_gates,omitempty"`
	CreatedAt             time.Time                 `json:"created_at"`
	Metadata              map[string]any            `json:"metadata,omitempty"`
}

// BuildReleaseBundle validates and packages already-observed release evidence.
func BuildReleaseBundle(input ReleaseBundleInput) (ReleaseBundle, error) {
	requiredCheckIDs, err := normalizeRequiredValues("required check id", input.RequiredCheckIDs)
	if err != nil {
		return ReleaseBundle{}, err
	}
	requiredEvidenceTypes, err := normalizeRequiredValues("required evidence type", input.RequiredEvidenceTypes)
	if err != nil {
		return ReleaseBundle{}, err
	}
	requiredCIGates, err := normalizeRequiredValues("required CI gate", input.RequiredCIGates)
	if err != nil {
		return ReleaseBundle{}, err
	}
	process, err := releaseBundleProcessRecords(input)
	if err != nil {
		return ReleaseBundle{}, err
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	bundle := ReleaseBundle{
		Version:               ReleaseBundleVersion,
		IDs:                   input.Export.IDs,
		Mode:                  input.Gate.Mode,
		Outcome:               input.Export.Outcome,
		RunExport:             copyReleaseRunExport(input.Export),
		VerificationReport:    copyVerificationReport(input.Report),
		EntropyAudits:         copyReviewEntropyAudits(input.EntropyAudits),
		Review:                copyReviewDecision(input.Review),
		Gate:                  copyGateResult(input.Gate),
		Action:                copyActionResult(input.Action),
		Process:               process,
		RequiredCheckIDs:      requiredCheckIDs,
		RequiredEvidenceTypes: requiredEvidenceTypes,
		RequiredCIGates:       requiredCIGates,
		CreatedAt:             createdAt,
		Metadata:              copyDevAgentMetadata(input.Metadata),
	}
	if bundle.Mode == "" {
		bundle.Mode = input.Action.Mode
	}
	if err := bundle.Validate(); err != nil {
		return ReleaseBundle{}, err
	}
	return bundle, nil
}

func releaseBundleProcessRecords(input ReleaseBundleInput) (ProcessRecords, error) {
	if processRecordsProvided(input.Process) {
		return copyReleaseProcessRecords(input.Process), nil
	}
	return BuildProcessRecords(ProcessInput{
		IDs:       input.Export.IDs,
		Action:    input.Action,
		Patch:     input.Patch,
		Review:    input.Review,
		Gate:      &input.Gate,
		CreatedAt: input.CreatedAt,
		Metadata:  input.Metadata,
	})
}

func processRecordsProvided(records ProcessRecords) bool {
	return records.Task.ID != "" ||
		len(records.Inputs) > 0 ||
		len(records.Interventions) > 0
}

// Validate checks whether the release bundle is structurally and semantically release-ready.
func (b ReleaseBundle) Validate() error {
	if b.Version != ReleaseBundleVersion {
		return fmt.Errorf("%w: version %d", ErrInvalidReleaseBundle, b.Version)
	}
	if b.IDs.RunID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidReleaseBundle)
	}
	if !b.Mode.valid() {
		return fmt.Errorf("%w: mode %q", ErrInvalidReleaseBundle, b.Mode)
	}
	if b.Mode != ModeWrite {
		return fmt.Errorf("%w: release bundle requires write mode", ErrReleaseGateRejected)
	}
	if b.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created time is required", ErrInvalidReleaseBundle)
	}
	if err := validateReleaseActionForBundle(b.Action, b.Mode); err != nil {
		return err
	}
	if err := validateReleaseReviewForBundle(b.Review); err != nil {
		return err
	}
	if err := b.RunExport.Validate(); err != nil {
		return fmt.Errorf("%w: run export: %w", ErrInvalidReleaseBundle, err)
	}
	if err := b.VerificationReport.Validate(); err != nil {
		return fmt.Errorf("%w: verification report: %w", ErrInvalidReleaseBundle, err)
	}
	if b.IDs.RunID != b.RunExport.IDs.RunID {
		return fmt.Errorf("%w: bundle run id %q does not match run export %q", ErrInvalidReleaseBundle, b.IDs.RunID, b.RunExport.IDs.RunID)
	}
	if err := validateRuntimeIDFields("run export", b.RunExport.IDs, b.IDs, true); err != nil {
		return err
	}
	if b.IDs.RunID != b.VerificationReport.IDs.RunID {
		return fmt.Errorf("%w: bundle run id %q does not match verification report %q", ErrInvalidReleaseBundle, b.IDs.RunID, b.VerificationReport.IDs.RunID)
	}
	if err := validateRuntimeIDFields("verification report", b.VerificationReport.IDs, b.IDs, false); err != nil {
		return err
	}
	if err := validateRunExportVerificationReports(b.RunExport.VerificationReports, b.VerificationReport); err != nil {
		return err
	}
	if b.Outcome != b.RunExport.Outcome {
		return fmt.Errorf("%w: bundle outcome %q does not match run export %q", ErrInvalidReleaseBundle, b.Outcome, b.RunExport.Outcome)
	}
	if b.VerificationReport.Outcome != b.RunExport.Outcome {
		return fmt.Errorf("%w: verification report outcome %q does not match run export %q", ErrInvalidReleaseBundle, b.VerificationReport.Outcome, b.RunExport.Outcome)
	}
	if len(b.RunExport.Failures) > 0 {
		return fmt.Errorf("%w: run export contains failure attribution %s", ErrReleaseGateRejected, b.RunExport.Failures[0].ID)
	}
	if b.VerificationReport.Status != gopact.VerificationStatusPassed {
		return fmt.Errorf("%w: verification status %s is not passed", ErrReleaseGateRejected, b.VerificationReport.Status)
	}
	if err := validateReleaseGateForBundle(b.Gate, b.Mode, b.VerificationReport, b.Review, b.EntropyAudits); err != nil {
		return err
	}
	if len(b.RequiredCheckIDs) > 0 {
		if reasons := requiredCheckReasons(b.VerificationReport, b.RequiredCheckIDs); len(reasons) > 0 {
			return fmt.Errorf("%w: %s", ErrReleaseGateRejected, strings.Join(reasons, "; "))
		}
	}
	if len(b.RequiredEvidenceTypes) > 0 {
		if reasons := requiredEvidenceReasons(b.VerificationReport, b.RequiredEvidenceTypes); len(reasons) > 0 {
			return fmt.Errorf("%w: %s", ErrReleaseGateRejected, strings.Join(reasons, "; "))
		}
	}
	if len(b.RequiredCIGates) > 0 {
		if reasons := requiredCIGateReasons(b.VerificationReport, b.RequiredCIGates); len(reasons) > 0 {
			return fmt.Errorf("%w: %s", ErrReleaseGateRejected, strings.Join(reasons, "; "))
		}
	}
	for i, audit := range b.EntropyAudits {
		if err := audit.Validate(); err != nil {
			return fmt.Errorf("%w: entropy audit %d: %w", ErrInvalidReleaseBundle, i, err)
		}
		if audit.IDs.RunID != "" && audit.IDs.RunID != b.IDs.RunID {
			return fmt.Errorf("%w: entropy audit %q run id %q does not match %q", ErrInvalidReleaseBundle, audit.ID, audit.IDs.RunID, b.IDs.RunID)
		}
		if err := validateRuntimeIDFields("entropy audit "+audit.ID, audit.IDs, b.IDs, false); err != nil {
			return err
		}
	}
	if err := validateReleaseProcessRecords(b.Process, b.IDs, b.Action, b.Gate, b.Review); err != nil {
		return err
	}
	if err := validateRunExportProcessRecords(b.RunExport, b.Process); err != nil {
		return err
	}
	return nil
}

func validateRunExportVerificationReports(exported []gopact.VerificationReport, want gopact.VerificationReport) error {
	if len(exported) == 0 {
		return nil
	}
	for _, report := range exported {
		if verificationReportSnapshotEqual(report, want) {
			return nil
		}
	}
	return fmt.Errorf("%w: run export verification reports do not include bundle verification report", ErrInvalidReleaseBundle)
}

func verificationReportSnapshotEqual(got, want gopact.VerificationReport) bool {
	if got.CreatedAt.IsZero() != want.CreatedAt.IsZero() {
		return false
	}
	if !got.CreatedAt.IsZero() && !got.CreatedAt.Equal(want.CreatedAt) {
		return false
	}
	got.CreatedAt = time.Time{}
	want.CreatedAt = time.Time{}
	return reflect.DeepEqual(got, want)
}

func validateRunExportProcessRecords(export gopact.RunExport, records ProcessRecords) error {
	if len(export.Tasks) > 0 && !hasMatchingTaskRecord(export.Tasks, records.Task) {
		return fmt.Errorf("%w: run export process tasks do not include bundle process task", ErrInvalidReleaseBundle)
	}
	if len(export.Inputs) > 0 {
		for _, record := range records.Inputs {
			if !hasMatchingInputRecord(export.Inputs, record) {
				return fmt.Errorf(
					"%w: run export process inputs do not include bundle process input %s",
					ErrInvalidReleaseBundle,
					record.ID,
				)
			}
		}
	}
	if len(export.Interventions) > 0 {
		for _, record := range records.Interventions {
			if !hasMatchingInterventionRecord(export.Interventions, record) {
				return fmt.Errorf(
					"%w: run export process interventions do not include bundle process intervention %s",
					ErrInvalidReleaseBundle,
					record.ID,
				)
			}
		}
	}
	return nil
}

func hasMatchingTaskRecord(records []gopact.TaskRecord, want gopact.TaskRecord) bool {
	for _, record := range records {
		if taskRecordSnapshotEqual(record, want) {
			return true
		}
	}
	return false
}

func hasMatchingInputRecord(records []gopact.InputRecord, want gopact.InputRecord) bool {
	for _, record := range records {
		if inputRecordSnapshotEqual(record, want) {
			return true
		}
	}
	return false
}

func hasMatchingInterventionRecord(records []gopact.InterventionRecord, want gopact.InterventionRecord) bool {
	for _, record := range records {
		if interventionRecordSnapshotEqual(record, want) {
			return true
		}
	}
	return false
}

func taskRecordSnapshotEqual(got, want gopact.TaskRecord) bool {
	got.CreatedAt = time.Time{}
	got.StartedAt = time.Time{}
	got.CompletedAt = time.Time{}
	want.CreatedAt = time.Time{}
	want.StartedAt = time.Time{}
	want.CompletedAt = time.Time{}
	return reflect.DeepEqual(got, want)
}

func inputRecordSnapshotEqual(got, want gopact.InputRecord) bool {
	got.CreatedAt = time.Time{}
	want.CreatedAt = time.Time{}
	return reflect.DeepEqual(got, want)
}

func interventionRecordSnapshotEqual(got, want gopact.InterventionRecord) bool {
	got.CreatedAt = time.Time{}
	got.ResolvedAt = time.Time{}
	want.CreatedAt = time.Time{}
	want.ResolvedAt = time.Time{}
	return reflect.DeepEqual(got, want)
}

func validateReleaseActionForBundle(action ActionResult, mode Mode) error {
	if !action.Status.valid() {
		return fmt.Errorf("%w: action status %q", ErrInvalidReleaseBundle, action.Status)
	}
	if action.Status != ActionAllowed {
		return fmt.Errorf("%w: release action status %s is not allowed", ErrReleaseGateRejected, action.Status)
	}
	if !action.Mode.valid() {
		return fmt.Errorf("%w: action mode %q", ErrInvalidReleaseBundle, action.Mode)
	}
	if action.Mode != mode {
		return fmt.Errorf("%w: action mode %q does not match bundle mode %q", ErrInvalidReleaseBundle, action.Mode, mode)
	}
	if action.Mode != ModeWrite {
		return fmt.Errorf("%w: release bundle requires write mode", ErrReleaseGateRejected)
	}
	if !action.Action.valid() {
		return fmt.Errorf("%w: action %q", ErrInvalidReleaseBundle, action.Action)
	}
	if action.Action != ActionRelease {
		return fmt.Errorf("%w: action %s is not release", ErrInvalidReleaseBundle, action.Action)
	}
	return nil
}

func validateReleaseReviewForBundle(review ReviewDecision) error {
	if reasons := reviewGateReasons(review, true); len(reasons) > 0 {
		return fmt.Errorf("%w: %s", ErrReleaseGateRejected, strings.Join(reasons, "; "))
	}
	if err := validateReviewDecision(review); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReleaseBundle, err)
	}
	return nil
}

func validateReleaseGateForBundle(
	gate GateResult,
	mode Mode,
	report gopact.VerificationReport,
	review ReviewDecision,
	audits []gopact.EntropyAudit,
) error {
	if !gate.Status.valid() {
		return ErrReleaseGateStatusRequired
	}
	if gate.Status != GatePassed {
		return fmt.Errorf("%w: release gate status %s is not passed", ErrReleaseGateRejected, gate.Status)
	}
	if gate.Mode != "" && gate.Mode != mode {
		return fmt.Errorf("%w: release gate mode %q does not match bundle mode %q", ErrInvalidReleaseBundle, gate.Mode, mode)
	}
	if gate.ReportStatus == "" {
		return fmt.Errorf("%w: release gate report status is required", ErrInvalidReleaseBundle)
	}
	if gate.ReportStatus != report.Status {
		return fmt.Errorf(
			"%w: release gate report status %q does not match verification report %q",
			ErrInvalidReleaseBundle,
			gate.ReportStatus,
			report.Status,
		)
	}
	if gate.ReviewStatus == ReviewUnknown {
		return fmt.Errorf("%w: release gate review status is required", ErrInvalidReleaseBundle)
	}
	if gate.ReviewStatus != review.Status {
		return fmt.Errorf(
			"%w: release gate review status %q does not match review %q",
			ErrInvalidReleaseBundle,
			gate.ReviewStatus,
			review.Status,
		)
	}
	maxSeverity, reasons, err := entropyGateReasons(audits, gopact.EntropySeverityCritical)
	if err != nil {
		return err
	}
	if len(reasons) > 0 {
		return fmt.Errorf("%w: %s", ErrReleaseGateRejected, strings.Join(reasons, "; "))
	}
	if gate.MaxEntropySeverity != maxSeverity {
		return fmt.Errorf(
			"%w: release gate max entropy severity %q does not match entropy audits %q",
			ErrInvalidReleaseBundle,
			gate.MaxEntropySeverity,
			maxSeverity,
		)
	}
	return nil
}

func validateReleaseProcessRecords(
	records ProcessRecords,
	ids gopact.RuntimeIDs,
	action ActionResult,
	gate GateResult,
	review ReviewDecision,
) error {
	if err := records.Task.Validate(); err != nil {
		return fmt.Errorf("%w: process task: %w", ErrInvalidReleaseBundle, err)
	}
	if records.Task.Status != gopact.TaskCompleted {
		return fmt.Errorf("%w: process task status %q is not completed", ErrInvalidReleaseBundle, records.Task.Status)
	}
	if err := validateProcessRunID("process task", records.Task.IDs.RunID, ids.RunID); err != nil {
		return err
	}
	if err := validateRuntimeIDFields("process task", records.Task.IDs, ids, true); err != nil {
		return err
	}
	if err := validateReleaseProcessTask(records.Task, action); err != nil {
		return err
	}
	workflowActionIndex, hasWorkflowActionIndex := workflowProcessIntMetadata(
		records.Task.Metadata,
		"workflow_action_index",
	)
	hasReleaseGateInput := false
	for i, record := range records.Inputs {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("%w: process input %d: %w", ErrInvalidReleaseBundle, i, err)
		}
		if err := validateProcessRunID(fmt.Sprintf("process input %d", i), record.IDs.RunID, ids.RunID); err != nil {
			return err
		}
		if err := validateRuntimeIDFields(fmt.Sprintf("process input %d", i), record.IDs, ids, true); err != nil {
			return err
		}
		if hasWorkflowActionIndex {
			if err := validateReleaseProcessWorkflowActionBoundary(
				record.Metadata,
				fmt.Sprintf("process input %d", i),
				workflowActionIndex,
			); err != nil {
				return err
			}
		}
		if record.Source == "devagent.release_gate" {
			hasReleaseGateInput = true
			if err := validateReleaseGateProcessInput(record, gate); err != nil {
				return err
			}
		}
	}
	if !hasReleaseGateInput {
		return fmt.Errorf("%w: process release gate input is required", ErrInvalidReleaseBundle)
	}
	hasResolvedReviewIntervention := false
	for i, record := range records.Interventions {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("%w: process intervention %d: %w", ErrInvalidReleaseBundle, i, err)
		}
		if err := validateProcessRunID(fmt.Sprintf("process intervention %d", i), record.IDs.RunID, ids.RunID); err != nil {
			return err
		}
		if err := validateRuntimeIDFields(fmt.Sprintf("process intervention %d", i), record.IDs, ids, true); err != nil {
			return err
		}
		if hasWorkflowActionIndex {
			if err := validateReleaseProcessWorkflowActionBoundary(
				record.Metadata,
				fmt.Sprintf("process intervention %d", i),
				workflowActionIndex,
			); err != nil {
				return err
			}
		}
		if record.Type != gopact.InterruptApproval {
			continue
		}
		if record.Status != gopact.InterventionResolved {
			return fmt.Errorf("%w: process review intervention status %q is not resolved", ErrInvalidReleaseBundle, record.Status)
		}
		if err := validateReviewProcessIntervention(record, review); err != nil {
			return err
		}
		hasResolvedReviewIntervention = true
	}
	if !hasResolvedReviewIntervention {
		return fmt.Errorf("%w: process resolved review intervention is required", ErrInvalidReleaseBundle)
	}
	return nil
}

func validateReleaseProcessWorkflowActionBoundary(metadata map[string]any, label string, actionIndex int) error {
	got, ok := workflowProcessIntMetadata(metadata, "workflow_action_index")
	if !ok {
		return fmt.Errorf(
			"%w: %s workflow_action_index = %v, want %d",
			ErrInvalidReleaseBundle,
			label,
			metadata["workflow_action_index"],
			actionIndex,
		)
	}
	if got != actionIndex {
		return fmt.Errorf(
			"%w: %s workflow_action_index = %d does not match process task workflow_action_index %d",
			ErrInvalidReleaseBundle,
			label,
			got,
			actionIndex,
		)
	}
	return nil
}

func validateRuntimeIDFields(label string, got, want gopact.RuntimeIDs, requireKnown bool) error {
	fields := []struct {
		name string
		got  string
		want string
	}{
		{name: "user id", got: got.UserID, want: want.UserID},
		{name: "session id", got: got.SessionID, want: want.SessionID},
		{name: "thread id", got: got.ThreadID, want: want.ThreadID},
		{name: "agent id", got: got.AgentID, want: want.AgentID},
		{name: "app id", got: got.AppID, want: want.AppID},
		{name: "call id", got: got.CallID, want: want.CallID},
		{name: "parent call id", got: got.ParentCallID, want: want.ParentCallID},
		{name: "trace id", got: got.TraceID, want: want.TraceID},
	}
	for _, field := range fields {
		if field.got == "" {
			if requireKnown && field.want != "" {
				return fmt.Errorf("%w: %s %s is required", ErrInvalidReleaseBundle, label, field.name)
			}
			continue
		}
		if field.want != "" && field.got != field.want {
			return fmt.Errorf("%w: %s %s %q does not match %q", ErrInvalidReleaseBundle, label, field.name, field.got, field.want)
		}
	}
	return nil
}

func validateReleaseProcessTask(record gopact.TaskRecord, action ActionResult) error {
	expected := map[string]string{
		"source":        "devagent",
		"mode":          string(action.Mode),
		"action":        string(action.Action),
		"action_status": string(action.Status),
	}
	for key, want := range expected {
		if err := validateProcessStringMapValue("process task metadata", record.Metadata, key, want); err != nil {
			return err
		}
	}
	if err := validateProcessAnyMapValue("process task input", record.Input, "mode", string(action.Mode)); err != nil {
		return err
	}
	if err := validateProcessAnyMapValue("process task input", record.Input, "action", string(action.Action)); err != nil {
		return err
	}
	if err := validateProcessAnyMapValue("process task output", record.Output, "status", string(action.Status)); err != nil {
		return err
	}
	return nil
}

func validateReleaseGateProcessInput(record gopact.InputRecord, gate GateResult) error {
	if record.Kind != gopact.InputExternal {
		return fmt.Errorf("%w: process release gate input kind %q is not external", ErrInvalidReleaseBundle, record.Kind)
	}
	if err := validateProcessAnyMapValue("process release gate input", record.Value, "status", string(gate.Status)); err != nil {
		return err
	}
	if gate.Mode != "" {
		if err := validateProcessAnyMapValue("process release gate input", record.Value, "mode", string(gate.Mode)); err != nil {
			return err
		}
	}
	if gate.ReportStatus != "" {
		if err := validateProcessAnyMapValue("process release gate input", record.Value, "report_status", string(gate.ReportStatus)); err != nil {
			return err
		}
	}
	if gate.ReviewStatus != ReviewUnknown {
		if err := validateProcessAnyMapValue("process release gate input", record.Value, "review_status", string(gate.ReviewStatus)); err != nil {
			return err
		}
	}
	if gate.MaxEntropySeverity != "" {
		if err := validateProcessAnyMapValue("process release gate input", record.Value, "max_entropy_severity", string(gate.MaxEntropySeverity)); err != nil {
			return err
		}
	}
	return nil
}

func validateReviewProcessIntervention(record gopact.InterventionRecord, review ReviewDecision) error {
	if review.Status != ReviewUnknown {
		if err := validateProcessStringMapValue("process review intervention", record.Metadata, "review_status", string(review.Status)); err != nil {
			return err
		}
	}
	if review.Reviewer != "" {
		if err := validateProcessStringMapValue("process review intervention", record.Metadata, "reviewer", review.Reviewer); err != nil {
			return err
		}
	}
	for _, key := range reviewGovernanceMetadataKeys() {
		want, ok := reviewGovernanceStringMetadata(review.Metadata, key)
		if !ok {
			continue
		}
		if err := validateProcessStringMapValue("process review intervention", record.Metadata, key, want); err != nil {
			return err
		}
	}
	return nil
}

func reviewGovernanceMetadataKeys() []string {
	return []string{
		"review_prompt_id",
		"review_prompt_version",
		"review_eval_id",
		"review_eval_version",
		"review_policy_ref",
	}
}

func reviewGovernanceStringMetadata(metadata map[string]any, key string) (string, bool) {
	value, ok := metadata[key]
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func validateProcessRunID(label, got, want string) error {
	if got == "" {
		return fmt.Errorf("%w: %s run id is required", ErrInvalidReleaseBundle, label)
	}
	if got != want {
		return fmt.Errorf("%w: %s run id %q does not match %q", ErrInvalidReleaseBundle, label, got, want)
	}
	return nil
}

func validateProcessAnyMapValue(label string, value any, key, want string) error {
	values, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: %s must be an object", ErrInvalidReleaseBundle, label)
	}
	return validateProcessStringMapValue(label, values, key, want)
}

func validateProcessStringMapValue(label string, values map[string]any, key, want string) error {
	raw, ok := values[key]
	if !ok {
		return fmt.Errorf("%w: %s %s is required", ErrInvalidReleaseBundle, label, key)
	}
	got, ok := raw.(string)
	if !ok {
		return fmt.Errorf("%w: %s %s must be a string", ErrInvalidReleaseBundle, label, key)
	}
	if got != want {
		return fmt.Errorf("%w: %s %s %q does not match %q", ErrInvalidReleaseBundle, label, key, got, want)
	}
	return nil
}

func normalizeRequiredValues(label string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: %s is required", ErrInvalidReleaseBundle, label)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}
