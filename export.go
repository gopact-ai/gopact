package gopact

import (
	"errors"
	"fmt"
	"iter"
	"time"
)

const (
	// RunExportVersion is the current run export schema version.
	RunExportVersion = 1
)

// RunOutcome describes the terminal state captured by a run export.
type RunOutcome string

const (
	RunCompleted   RunOutcome = "completed"
	RunFailed      RunOutcome = "failed"
	RunCanceled    RunOutcome = "canceled"
	RunInterrupted RunOutcome = "interrupted"
)

// RunExport is a versioned, provider-neutral run export.
type RunExport struct {
	Version             int                  `json:"version"`
	IDs                 RuntimeIDs           `json:"ids"`
	Outcome             RunOutcome           `json:"outcome"`
	Events              []Event              `json:"events,omitempty"`
	Steps               []StepSnapshot       `json:"steps,omitempty"`
	Tasks               []TaskRecord         `json:"tasks,omitempty"`
	Inputs              []InputRecord        `json:"inputs,omitempty"`
	Interventions       []InterventionRecord `json:"interventions,omitempty"`
	Failures            []FailureAttribution `json:"failures,omitempty"`
	EntropyAudits       []EntropyAudit       `json:"entropy_audits,omitempty"`
	VerificationReports []VerificationReport `json:"verification_reports,omitempty"`
	CreatedAt           time.Time            `json:"created_at,omitempty"`
	Metadata            map[string]any       `json:"metadata,omitempty"`
}

// RunRecorder builds a versioned RunExport from an event stream.
type RunRecorder struct {
	ids                 RuntimeIDs
	outcome             RunOutcome
	events              []Event
	steps               []StepSnapshot
	tasks               []TaskRecord
	inputs              []InputRecord
	interventions       []InterventionRecord
	failures            []FailureAttribution
	entropyAudits       []EntropyAudit
	verificationReports []VerificationReport
	createdAt           time.Time
	metadata            map[string]any
}

// NewRunRecorder creates an empty run recorder.
func NewRunRecorder() *RunRecorder {
	return &RunRecorder{}
}

// Record appends one event to the run export under construction.
func (r *RunRecorder) Record(event Event) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	ids := event.RuntimeIDs()
	if err := r.mergeIDs(ids); err != nil {
		return err
	}
	if r.createdAt.IsZero() {
		r.createdAt = event.CreatedAt
		if r.createdAt.IsZero() {
			r.createdAt = now()
		}
	}
	outcome, terminal := runOutcomeForEvent(event.Type)
	if terminal && r.outcome != "" {
		return fmt.Errorf("gopact: run outcome already recorded as %q", r.outcome)
	}
	recorded := copyEvent(event)
	r.events = append(r.events, recorded)
	if event.StepSnapshot != nil && event.StepSnapshot.Phase != StepRunning {
		r.steps = append(r.steps, copyStepSnapshot(*event.StepSnapshot))
	}
	if report, ok := verificationReportFromEvent(event); ok {
		if err := r.RecordVerificationReport(report); err != nil {
			return err
		}
	}
	if failure, ok := failureAttributionFromFailedEvent(event, r.verificationReports); ok {
		if err := r.recordFailure(failure); err != nil {
			return err
		}
	}
	if terminal {
		r.outcome = outcome
	}
	return nil
}

// RecordTask appends one task process record to the run export under construction.
func (r *RunRecorder) RecordTask(record TaskRecord) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if err := r.mergeIDs(record.IDs); err != nil {
		return err
	}
	r.tasks = append(r.tasks, copyTaskRecord(record))
	return nil
}

// RecordInput appends one input process record to the run export under construction.
func (r *RunRecorder) RecordInput(record InputRecord) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if err := r.mergeIDs(record.IDs); err != nil {
		return err
	}
	r.inputs = append(r.inputs, copyInputRecord(record))
	return nil
}

// RecordIntervention appends one intervention process record to the run export under construction.
func (r *RunRecorder) RecordIntervention(record InterventionRecord) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if err := r.mergeIDs(record.IDs); err != nil {
		return err
	}
	r.interventions = append(r.interventions, copyInterventionRecord(record))
	return nil
}

// RecordFailure appends one failure attribution to the run export under construction.
func (r *RunRecorder) RecordFailure(record FailureAttribution) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	return r.recordFailure(record)
}

// RecordEntropyAudit appends one entropy audit to the run export under construction.
func (r *RunRecorder) RecordEntropyAudit(record EntropyAudit) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if err := r.mergeIDs(record.IDs); err != nil {
		return err
	}
	r.entropyAudits = append(r.entropyAudits, copyEntropyAudit(record))
	return nil
}

// RecordVerificationReport appends one verification report to the run export under construction.
func (r *RunRecorder) RecordVerificationReport(report VerificationReport) error {
	if r == nil {
		return errors.New("gopact: run recorder is nil")
	}
	if err := report.Validate(); err != nil {
		return err
	}
	if err := r.mergeIDs(report.IDs); err != nil {
		return err
	}
	r.verificationReports = append(r.verificationReports, copyVerificationReport(report))
	return nil
}

// Export returns a validated run export.
func (r *RunRecorder) Export() (RunExport, error) {
	if r == nil {
		return RunExport{}, errors.New("gopact: run recorder is nil")
	}
	export := RunExport{
		Version:             RunExportVersion,
		IDs:                 r.ids,
		Outcome:             r.outcome,
		Events:              copyEvents(r.events),
		Steps:               copyStepSnapshots(r.steps),
		Tasks:               copyTaskRecords(r.tasks),
		Inputs:              copyInputRecords(r.inputs),
		Interventions:       copyInterventionRecords(r.interventions),
		Failures:            copyFailureAttributions(r.failures),
		EntropyAudits:       copyEntropyAudits(r.entropyAudits),
		VerificationReports: copyVerificationReports(r.verificationReports),
		CreatedAt:           r.createdAt,
		Metadata:            copyAnyMap(r.metadata),
	}
	if export.CreatedAt.IsZero() {
		export.CreatedAt = now()
	}
	if err := export.Validate(); err != nil {
		return RunExport{}, err
	}
	return export, nil
}

// ReplayRunExport replays the recorded event stream from a run export.
func ReplayRunExport(export RunExport) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if err := export.Validate(); err != nil {
			yield(Event{Type: EventRunFailed, IDs: export.IDs, CreatedAt: now(), Err: err}, err)
			return
		}
		for _, event := range export.Events {
			if !yield(copyEvent(event), nil) {
				return
			}
		}
	}
}

// Validate checks the minimum integrity required for import.
func (e RunExport) Validate() error {
	if e.Version <= 0 {
		return errors.New("gopact: run export version is required")
	}
	if e.IDs.RunID == "" {
		return errors.New("gopact: run export run id is required")
	}
	if e.Outcome == "" {
		return errors.New("gopact: run export outcome is required")
	}
	if !e.Outcome.valid() {
		return fmt.Errorf("gopact: run export outcome %q is invalid", e.Outcome)
	}
	for i, step := range e.Steps {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported step %d: %w", i, err)
		}
	}
	for i, task := range e.Tasks {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported task %d: %w", i, err)
		}
	}
	for i, input := range e.Inputs {
		if err := input.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported input %d: %w", i, err)
		}
	}
	for i, intervention := range e.Interventions {
		if err := intervention.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported intervention %d: %w", i, err)
		}
	}
	for i, failure := range e.Failures {
		if err := failure.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported failure attribution %d: %w", i, err)
		}
	}
	for i, audit := range e.EntropyAudits {
		if err := audit.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported entropy audit %d: %w", i, err)
		}
	}
	for i, report := range e.VerificationReports {
		if err := report.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid exported verification report %d: %w", i, err)
		}
	}
	return nil
}

func (r *RunRecorder) recordFailure(record FailureAttribution) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if err := r.mergeIDs(record.IDs); err != nil {
		return err
	}
	r.failures = append(r.failures, copyFailureAttribution(record))
	return nil
}

func (r *RunRecorder) mergeIDs(ids RuntimeIDs) error {
	ids = runScopedRuntimeIDs(ids)
	if err := validateRunRecorderRuntimeIDs(r.ids, ids); err != nil {
		return err
	}
	r.ids = ids.WithDefaults(r.ids)
	return nil
}

func runScopedRuntimeIDs(ids RuntimeIDs) RuntimeIDs {
	ids.CallID = ""
	ids.ParentCallID = ""
	return ids
}

func validateRunRecorderRuntimeIDs(current, next RuntimeIDs) error {
	fields := []struct {
		name    string
		current string
		next    string
	}{
		{name: "run ids", current: current.RunID, next: next.RunID},
		{name: "thread ids", current: current.ThreadID, next: next.ThreadID},
		{name: "user ids", current: current.UserID, next: next.UserID},
		{name: "session ids", current: current.SessionID, next: next.SessionID},
		{name: "agent ids", current: current.AgentID, next: next.AgentID},
		{name: "app ids", current: current.AppID, next: next.AppID},
		{name: "trace ids", current: current.TraceID, next: next.TraceID},
	}
	for _, field := range fields {
		if field.current == "" || field.next == "" || field.current == field.next {
			continue
		}
		return fmt.Errorf("gopact: run recorder mixed %s %q and %q", field.name, field.current, field.next)
	}
	return nil
}

func (o RunOutcome) valid() bool {
	switch o {
	case RunCompleted, RunFailed, RunCanceled, RunInterrupted:
		return true
	default:
		return false
	}
}

func runOutcomeForEvent(eventType EventType) (RunOutcome, bool) {
	switch eventType {
	case EventRunCompleted:
		return RunCompleted, true
	case EventRunFailed:
		return RunFailed, true
	case EventRunCanceled:
		return RunCanceled, true
	case EventRunInterrupted:
		return RunInterrupted, true
	default:
		return "", false
	}
}

func verificationReportFromEvent(event Event) (VerificationReport, bool) {
	if len(event.Metadata) == 0 {
		return VerificationReport{}, false
	}
	value, ok := event.Metadata[EventMetadataVerificationReport]
	if !ok {
		return VerificationReport{}, false
	}
	switch report := value.(type) {
	case VerificationReport:
		return copyVerificationReport(report), true
	case *VerificationReport:
		if report == nil {
			return VerificationReport{}, false
		}
		return copyVerificationReport(*report), true
	default:
		return VerificationReport{}, false
	}
}

func copyStepSnapshots(in []StepSnapshot) []StepSnapshot {
	if len(in) == 0 {
		return nil
	}
	out := make([]StepSnapshot, len(in))
	for i, snapshot := range in {
		out[i] = copyStepSnapshot(snapshot)
	}
	return out
}

func copyStepSnapshot(in StepSnapshot) StepSnapshot {
	out := in
	out.Queue = append([]string(nil), in.Queue...)
	if in.Pending != nil {
		pending := *in.Pending
		pending.Metadata = copyAnyMap(in.Pending.Metadata)
		out.Pending = &pending
	}
	out.Effects = copyEffectRecords(in.Effects)
	out.Artifacts = copyArtifactRefs(in.Artifacts)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}
