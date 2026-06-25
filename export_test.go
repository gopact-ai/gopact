package gopact

import (
	"errors"
	"reflect"
	"testing"
)

func TestRunExportValidateAcceptsCompletedRun(t *testing.T) {
	export := RunExport{
		Version: 1,
		IDs:     RuntimeIDs{RunID: "run-1"},
		Outcome: RunCompleted,
		Steps: []StepSnapshot{
			{ID: "step-1", Step: 1, Node: "plan", Phase: StepCompleted},
		},
	}

	if err := export.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRunExportValidateAcceptsProcessRecords(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
		Outcome: RunCompleted,
		Tasks: []TaskRecord{
			{
				ID:     "task-1",
				Name:   "update docs",
				Status: TaskCompleted,
				IDs:    RuntimeIDs{RunID: "run-1", UserID: "user-1"},
				Input:  "fix design docs",
				Output: "docs updated",
			},
		},
		Inputs: []InputRecord{
			{
				ID:     "input-1",
				Kind:   InputUser,
				IDs:    RuntimeIDs{RunID: "run-1", UserID: "user-1"},
				Source: "chat",
				Value:  "please continue",
			},
		},
		Interventions: []InterventionRecord{
			{
				ID:     "approval-1",
				Type:   InterruptApproval,
				Status: InterventionResolved,
				IDs:    RuntimeIDs{RunID: "run-1", UserID: "user-1"},
				Request: &InterruptRecord{
					ID:   "interrupt-1",
					Type: InterruptApproval,
				},
				Resume: &ResumeRequest{InterruptID: "interrupt-1", Payload: map[string]any{"approved": true}},
			},
		},
		Failures: []FailureAttribution{
			{
				ID:      "failure-1",
				Kind:    FailureVerification,
				IDs:     RuntimeIDs{RunID: "run-1", UserID: "user-1"},
				Node:    "verify",
				Step:    3,
				Summary: "verification failed",
				Error:   "unit tests failed",
				Evidence: []VerificationEvidence{
					{Type: "command", Ref: "go test ./...", Summary: "exit 1"},
				},
			},
		},
		EntropyAudits: []EntropyAudit{
			{
				ID:     "entropy-1",
				Status: VerificationStatusPartial,
				IDs:    RuntimeIDs{RunID: "run-1", UserID: "user-1"},
				Findings: []EntropyFinding{
					{
						ID:       "entropy-finding-1",
						Category: EntropyResidue,
						Severity: EntropySeverityMedium,
						Summary:  "temporary file left behind",
						Evidence: []VerificationEvidence{
							{Type: "file", Ref: "tmp/output.log", Summary: "unexpected generated file"},
						},
					},
				},
			},
		},
		VerificationReports: []VerificationReport{
			{
				Version: RunExportVersion,
				IDs:     RuntimeIDs{RunID: "run-1", UserID: "user-1"},
				Outcome: RunCompleted,
				Status:  VerificationStatusPassed,
				Checks: []VerificationCheck{
					{
						ID:       "unit-tests",
						Status:   VerificationStatusPassed,
						Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
					},
				},
				PassedCount: 1,
				CreatedAt:   now(),
			},
		},
	}

	if err := export.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRunExportValidateRejectsInvalidRun(t *testing.T) {
	tests := []struct {
		name   string
		export RunExport
	}{
		{name: "missing version", export: RunExport{IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted}},
		{name: "missing run id", export: RunExport{Version: 1, Outcome: RunCompleted}},
		{name: "missing outcome", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}}},
		{name: "invalid outcome", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunOutcome("unknown")}},
		{name: "invalid step", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted, Steps: []StepSnapshot{{ID: "step-1", Step: -1, Node: "plan", Phase: StepCompleted}}}},
		{name: "invalid task", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted, Tasks: []TaskRecord{{ID: "task-1"}}}},
		{name: "invalid input", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted, Inputs: []InputRecord{{ID: "input-1"}}}},
		{name: "invalid intervention", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted, Interventions: []InterventionRecord{{ID: "intervention-1"}}}},
		{name: "invalid failure attribution", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunFailed, Failures: []FailureAttribution{{ID: "failure-1"}}}},
		{name: "invalid entropy audit", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted, EntropyAudits: []EntropyAudit{{ID: "entropy-1"}}}},
		{name: "invalid verification report", export: RunExport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted, VerificationReports: []VerificationReport{{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.export.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestRunRecorderRecordsProcessRecordsAndCopies(t *testing.T) {
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"}
	task := TaskRecord{
		ID:       "task-1",
		Name:     "write tests",
		Status:   TaskCompleted,
		IDs:      ids,
		Metadata: map[string]any{"suite": "unit"},
	}
	input := InputRecord{
		ID:       "input-1",
		Kind:     InputUser,
		IDs:      ids,
		Source:   "chat",
		Value:    "continue",
		Metadata: map[string]any{"channel": "tui"},
	}
	intervention := InterventionRecord{
		ID:     "approval-1",
		Type:   InterruptApproval,
		Status: InterventionResolved,
		IDs:    ids,
		Request: &InterruptRecord{
			ID:       "interrupt-1",
			Type:     InterruptApproval,
			Metadata: map[string]any{"risk": "medium"},
		},
		Resume: &ResumeRequest{
			InterruptID: "interrupt-1",
			Metadata:    map[string]any{"operator": "user-1"},
		},
		Metadata: map[string]any{"surface": "lark"},
	}
	failure := FailureAttribution{
		ID:      "failure-1",
		Kind:    FailureTool,
		IDs:     ids,
		Node:    "call_tool",
		Step:    2,
		Summary: "tool failed",
		Error:   "exit status 1",
		Evidence: []VerificationEvidence{
			{Type: "event", Ref: "run-1:2:tool", Summary: "tool node failed", Metadata: map[string]any{"node": "call_tool"}},
		},
		Metadata: map[string]any{"owner": "tooling"},
	}
	entropy := EntropyAudit{
		ID:     "entropy-1",
		Status: VerificationStatusPartial,
		IDs:    ids,
		Findings: []EntropyFinding{
			{
				ID:       "finding-1",
				Category: EntropyDependency,
				Severity: EntropySeverityHigh,
				Summary:  "dependency changed without matching tests",
				Evidence: []VerificationEvidence{
					{Type: "diff", Ref: "go.mod", Summary: "dependency update", Metadata: map[string]any{"module": "example.com/dep"}},
				},
				Metadata: map[string]any{"owner": "deps"},
			},
		},
		Metadata: map[string]any{"source": "reviewer"},
	}
	report := VerificationReport{
		Version: RunExportVersion,
		IDs:     ids,
		Outcome: RunCompleted,
		Status:  VerificationStatusPassed,
		Checks: []VerificationCheck{
			{
				ID:       "unit-tests",
				Status:   VerificationStatusPassed,
				Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0", Metadata: map[string]any{"suite": "unit"}}},
				Metadata: map[string]any{"kind": "required"},
			},
		},
		PassedCount: 1,
		CreatedAt:   now(),
		Metadata:    map[string]any{"source": "verify-node"},
	}
	recorder := NewRunRecorder()

	if err := recorder.RecordTask(task); err != nil {
		t.Fatalf("RecordTask() error = %v", err)
	}
	if err := recorder.RecordInput(input); err != nil {
		t.Fatalf("RecordInput() error = %v", err)
	}
	if err := recorder.RecordIntervention(intervention); err != nil {
		t.Fatalf("RecordIntervention() error = %v", err)
	}
	if err := recorder.RecordFailure(failure); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}
	if err := recorder.RecordEntropyAudit(entropy); err != nil {
		t.Fatalf("RecordEntropyAudit() error = %v", err)
	}
	if err := recorder.RecordVerificationReport(report); err != nil {
		t.Fatalf("RecordVerificationReport() error = %v", err)
	}
	if err := recorder.Record(Event{Type: EventRunStarted, IDs: ids}); err != nil {
		t.Fatalf("Record(run started) error = %v", err)
	}
	if err := recorder.Record(Event{Type: EventRunCompleted, IDs: ids}); err != nil {
		t.Fatalf("Record(run completed) error = %v", err)
	}

	task.Metadata["suite"] = "mutated"
	input.Metadata["channel"] = "mutated"
	intervention.Request.Metadata["risk"] = "mutated"
	intervention.Resume.Metadata["operator"] = "mutated"
	intervention.Metadata["surface"] = "mutated"
	failure.Evidence[0].Metadata["node"] = "mutated"
	failure.Metadata["owner"] = "mutated"
	entropy.Findings[0].Evidence[0].Metadata["module"] = "mutated"
	entropy.Findings[0].Metadata["owner"] = "mutated"
	entropy.Metadata["source"] = "mutated"
	report.Checks[0].Evidence[0].Metadata["suite"] = "mutated"
	report.Checks[0].Metadata["kind"] = "mutated"
	report.Metadata["source"] = "mutated"

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.Tasks) != 1 || export.Tasks[0].Metadata["suite"] != "unit" {
		t.Fatalf("tasks = %+v, want copied task record", export.Tasks)
	}
	if len(export.Inputs) != 1 || export.Inputs[0].Metadata["channel"] != "tui" {
		t.Fatalf("inputs = %+v, want copied input record", export.Inputs)
	}
	if len(export.Interventions) != 1 ||
		export.Interventions[0].Request.Metadata["risk"] != "medium" ||
		export.Interventions[0].Resume.Metadata["operator"] != "user-1" ||
		export.Interventions[0].Metadata["surface"] != "lark" {
		t.Fatalf("interventions = %+v, want copied intervention record", export.Interventions)
	}
	if len(export.Failures) != 1 ||
		export.Failures[0].Metadata["owner"] != "tooling" ||
		export.Failures[0].Evidence[0].Metadata["node"] != "call_tool" {
		t.Fatalf("failures = %+v, want copied failure attribution", export.Failures)
	}
	if len(export.EntropyAudits) != 1 ||
		export.EntropyAudits[0].Metadata["source"] != "reviewer" ||
		export.EntropyAudits[0].Findings[0].Metadata["owner"] != "deps" ||
		export.EntropyAudits[0].Findings[0].Evidence[0].Metadata["module"] != "example.com/dep" {
		t.Fatalf("entropy audits = %+v, want copied entropy audit", export.EntropyAudits)
	}
	if len(export.VerificationReports) != 1 ||
		export.VerificationReports[0].Metadata["source"] != "verify-node" ||
		export.VerificationReports[0].Checks[0].Metadata["kind"] != "required" ||
		export.VerificationReports[0].Checks[0].Evidence[0].Metadata["suite"] != "unit" {
		t.Fatalf("verification reports = %+v, want copied verification report", export.VerificationReports)
	}
}

func TestRunRecorderRejectsInvalidProcessRecord(t *testing.T) {
	recorder := NewRunRecorder()
	if err := recorder.RecordTask(TaskRecord{ID: "task-1"}); err == nil {
		t.Fatal("RecordTask() error = nil, want validation error")
	}
	if err := recorder.RecordInput(InputRecord{ID: "input-1"}); err == nil {
		t.Fatal("RecordInput() error = nil, want validation error")
	}
	if err := recorder.RecordIntervention(InterventionRecord{ID: "approval-1"}); err == nil {
		t.Fatal("RecordIntervention() error = nil, want validation error")
	}
	if err := recorder.RecordFailure(FailureAttribution{ID: "failure-1"}); err == nil {
		t.Fatal("RecordFailure() error = nil, want validation error")
	}
	if err := recorder.RecordEntropyAudit(EntropyAudit{ID: "entropy-1"}); err == nil {
		t.Fatal("RecordEntropyAudit() error = nil, want validation error")
	}
	if err := recorder.RecordVerificationReport(VerificationReport{Version: 1, IDs: RuntimeIDs{RunID: "run-1"}, Outcome: RunCompleted}); err == nil {
		t.Fatal("RecordVerificationReport() error = nil, want validation error")
	}
}

func TestRunRecorderExtractsVerificationReportFromEventMetadata(t *testing.T) {
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	report := VerificationReport{
		Version: RunExportVersion,
		IDs:     ids,
		Outcome: RunCompleted,
		Status:  VerificationStatusPassed,
		Checks: []VerificationCheck{
			{
				ID:       "unit-tests",
				Status:   VerificationStatusPassed,
				Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 0"}},
			},
		},
		PassedCount: 1,
		CreatedAt:   now(),
	}
	recorder := NewRunRecorder()
	events := []Event{
		{Type: EventRunStarted, IDs: ids},
		{
			Type: EventNodeCompleted,
			IDs:  ids,
			Node: "verify",
			Step: 2,
			Metadata: map[string]any{
				EventMetadataVerificationReport: report,
			},
		},
		{Type: EventRunCompleted, IDs: ids},
	}

	for _, event := range events {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(%s) error = %v", event.Type, err)
		}
	}
	report.Checks[0].Evidence[0].Ref = "mutated"

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.VerificationReports) != 1 {
		t.Fatalf("VerificationReports = %+v, want one report from event metadata", export.VerificationReports)
	}
	if export.VerificationReports[0].Checks[0].Evidence[0].Ref != "go test ./..." {
		t.Fatalf("VerificationReports[0] = %+v, want copied report from metadata", export.VerificationReports[0])
	}
}

func TestRunRecorderDerivesFailureAttributionFromRunFailedEvent(t *testing.T) {
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	wantErr := errors.New("boom")
	recorder := NewRunRecorder()
	if err := recorder.Record(Event{Type: EventRunStarted, IDs: ids}); err != nil {
		t.Fatalf("Record(run started) error = %v", err)
	}
	if err := recorder.Record(Event{
		Type: EventRunFailed,
		IDs:  ids,
		Node: "verify",
		Step: 3,
		Err:  wantErr,
	}); err != nil {
		t.Fatalf("Record(run failed) error = %v", err)
	}

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.Failures) != 1 {
		t.Fatalf("Failures = %+v, want one derived failure attribution", export.Failures)
	}
	failure := export.Failures[0]
	if failure.ID != "run-1:failure:verify:3" || failure.Kind != FailureKind("verification") {
		t.Fatalf("derived failure identity = %+v, want verification failure", failure)
	}
	if failure.IDs != ids || failure.Node != "verify" || failure.Step != 3 {
		t.Fatalf("derived failure location = %+v, want run/thread verify step 3", failure)
	}
	if failure.Error != wantErr.Error() {
		t.Fatalf("derived failure error = %q, want %q", failure.Error, wantErr)
	}
	if len(failure.Evidence) != 1 || failure.Evidence[0].Type != "event" || failure.Evidence[0].Ref != "run-1:verify:3:run_failed" {
		t.Fatalf("derived failure evidence = %+v, want run_failed event evidence", failure.Evidence)
	}
}

func TestRunRecorderDerivesFailureAttributionKindFromBoundarySignals(t *testing.T) {
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	tests := []struct {
		name  string
		event Event
		want  FailureKind
	}{
		{
			name: "model node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "call_model",
				Step: 1,
				Err:  errors.New("provider unavailable"),
			},
			want: FailureModel,
		},
		{
			name: "tool node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "call_tool",
				Step: 2,
				Err:  errors.New("tool failed"),
			},
			want: FailureTool,
		},
		{
			name: "policy denial",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Step: 3,
				Err: &PolicyDeniedError{
					Decision: PolicyDecision{Action: PolicyDeny, Reason: "blocked"},
					Request:  PolicyRequest{IDs: ids, Boundary: PolicyBoundaryTool, Action: PolicyActionInvoke},
				},
			},
			want: FailurePolicy,
		},
		{
			name: "verification node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "verify",
				Step: 4,
				Err:  errors.New("verifier failed"),
			},
			want: FailureKind("verification"),
		},
		{
			name: "context node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "build_context",
				Step: 5,
				Err:  errors.New("context budget exceeded"),
			},
			want: FailureKind("context"),
		},
		{
			name: "feedback node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "review",
				Step: 6,
				Err:  errors.New("review rejected"),
			},
			want: FailureKind("feedback"),
		},
		{
			name: "recovery node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "resume",
				Step: 7,
				Err:  errors.New("resume payload invalid"),
			},
			want: FailureKind("recovery"),
		},
		{
			name: "entropy node",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Node: "entropy_audit",
				Step: 8,
				Err:  errors.New("entropy gate failed"),
			},
			want: FailureKind("entropy"),
		},
		{
			name: "explicit metadata",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Step: 9,
				Metadata: map[string]any{
					EventMetadataFailureKind: string(FailureExternal),
				},
				Err: errors.New("upstream unavailable"),
			},
			want: FailureExternal,
		},
		{
			name: "explicit unknown metadata",
			event: Event{
				Type: EventRunFailed,
				IDs:  ids,
				Step: 10,
				Metadata: map[string]any{
					EventMetadataFailureKind: "unknown",
				},
				Err: errors.New("unclassified failure"),
			},
			want: FailureKind("unknown"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := NewRunRecorder()
			if err := recorder.Record(Event{Type: EventRunStarted, IDs: ids}); err != nil {
				t.Fatalf("Record(run started) error = %v", err)
			}
			if err := recorder.Record(tt.event); err != nil {
				t.Fatalf("Record(run failed) error = %v", err)
			}
			export, err := recorder.Export()
			if err != nil {
				t.Fatalf("Export() error = %v", err)
			}
			if len(export.Failures) != 1 {
				t.Fatalf("Failures = %+v, want one derived failure", export.Failures)
			}
			if export.Failures[0].Kind != tt.want {
				t.Fatalf("failure kind = %q, want %q", export.Failures[0].Kind, tt.want)
			}
		})
	}
}

func TestRunRecorderAttributesRunFailedToVerificationReport(t *testing.T) {
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	report := VerificationReport{
		Version: RunExportVersion,
		IDs:     ids,
		Outcome: RunCompleted,
		Status:  VerificationStatusFailed,
		Checks: []VerificationCheck{
			{
				ID:       "template-gate",
				Status:   VerificationStatusFailed,
				Evidence: []VerificationEvidence{{Type: "command", Ref: "go test ./...", Summary: "exit 1"}},
			},
		},
		FailedCount: 1,
		CreatedAt:   now(),
	}
	recorder := NewRunRecorder()
	events := []Event{
		{Type: EventRunStarted, IDs: ids},
		{
			Type: EventNodeFailed,
			IDs:  ids,
			Node: "verify",
			Step: 3,
			Metadata: map[string]any{
				EventMetadataVerificationReport: report,
			},
			Err: errors.New("verification failed"),
		},
		{Type: EventRunFailed, IDs: ids, Node: "verify", Step: 3, Err: errors.New("verification failed")},
	}

	for _, event := range events {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record(%s) error = %v", event.Type, err)
		}
	}

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(export.VerificationReports) != 1 || export.VerificationReports[0].Status != VerificationStatusFailed {
		t.Fatalf("VerificationReports = %+v, want failed report", export.VerificationReports)
	}
	if len(export.Failures) != 1 {
		t.Fatalf("Failures = %+v, want one verification failure", export.Failures)
	}
	failure := export.Failures[0]
	if failure.Kind != FailureVerification {
		t.Fatalf("failure kind = %q, want verification", failure.Kind)
	}
	if len(failure.Evidence) != 2 || failure.Evidence[1].Type != "verification_report" || failure.Evidence[1].Ref != "run-1:verification_report:failed" {
		t.Fatalf("failure evidence = %+v, want verification report evidence", failure.Evidence)
	}
}

func TestRunRecorderExportsEventsStepsAndOutcome(t *testing.T) {
	ids := RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}
	completed := &StepSnapshot{
		ID:     "step-1",
		Step:   1,
		Node:   "plan",
		Phase:  StepCompleted,
		Output: "done",
	}
	recorder := NewRunRecorder()

	events := []Event{
		{Type: EventRunStarted, IDs: ids},
		{Type: EventNodeStarted, IDs: ids, Step: 1, Node: "plan", StepSnapshot: &StepSnapshot{ID: "step-1", Step: 1, Node: "plan", Phase: StepRunning}},
		{Type: EventNodeCompleted, IDs: ids, Step: 1, Node: "plan", StepSnapshot: completed},
		{Type: EventRunCompleted, IDs: ids},
	}
	for _, event := range events {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	completed.Node = "mutated"

	export, err := recorder.Export()
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if export.Version != RunExportVersion {
		t.Fatalf("Version = %d, want %d", export.Version, RunExportVersion)
	}
	if export.IDs.RunID != "run-1" || export.IDs.ThreadID != "thread-1" {
		t.Fatalf("IDs = %+v, want run/thread ids", export.IDs)
	}
	if export.Outcome != RunCompleted {
		t.Fatalf("Outcome = %q, want completed", export.Outcome)
	}
	if len(export.Events) != len(events) {
		t.Fatalf("Events count = %d, want %d", len(export.Events), len(events))
	}
	if len(export.Steps) != 1 {
		t.Fatalf("Steps count = %d, want 1 stable step", len(export.Steps))
	}
	if export.Steps[0].Node != "plan" || export.Steps[0].Phase != StepCompleted {
		t.Fatalf("Steps[0] = %+v, want completed plan step", export.Steps[0])
	}
}

func TestRunRecorderRejectsMixedRunIDs(t *testing.T) {
	recorder := NewRunRecorder()
	if err := recorder.Record(Event{Type: EventRunStarted, IDs: RuntimeIDs{RunID: "run-1"}}); err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}
	if err := recorder.Record(Event{Type: EventRunStarted, IDs: RuntimeIDs{RunID: "run-2"}}); err == nil {
		t.Fatal("Record(second) error = nil, want mixed run id error")
	}
}

func TestRunRecorderRequiresTerminalOutcome(t *testing.T) {
	recorder := NewRunRecorder()
	if err := recorder.Record(Event{Type: EventRunStarted, IDs: RuntimeIDs{RunID: "run-1"}}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if _, err := recorder.Export(); err == nil {
		t.Fatal("Export() error = nil, want missing outcome error")
	}
}

func TestReplayRunExportYieldsRecordedEvents(t *testing.T) {
	export := RunExport{
		Version: RunExportVersion,
		IDs:     RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
		Outcome: RunCompleted,
		Events: []Event{
			{Type: EventRunStarted, IDs: RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}},
			{Type: EventRunCompleted, IDs: RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}},
		},
	}

	var got []Event
	for event, err := range ReplayRunExport(export) {
		if err != nil {
			t.Fatalf("ReplayRunExport() error = %v", err)
		}
		got = append(got, event)
	}
	if !reflect.DeepEqual(EventTypes(got), []EventType{EventRunStarted, EventRunCompleted}) {
		t.Fatalf("replayed event types = %v", EventTypes(got))
	}

	got[0].Type = EventRunFailed
	if export.Events[0].Type != EventRunStarted {
		t.Fatal("ReplayRunExport() returned mutable backing event")
	}
}

func TestReplayRunExportRejectsInvalidExport(t *testing.T) {
	var gotErr error
	for _, err := range ReplayRunExport(RunExport{}) {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("ReplayRunExport() error = nil, want validation error")
	}
}

func EventTypes(events []Event) []EventType {
	types := make([]EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}
