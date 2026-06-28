package devagent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

// ErrReleaseBundleConformanceFailed reports a failed release bundle conformance case.
var ErrReleaseBundleConformanceFailed = errors.New("devagent: release bundle conformance failed")

// ReleaseBundleConformanceHarness describes one release bundle under test.
type ReleaseBundleConformanceHarness struct {
	Bundle                ReleaseBundle
	RequiredCheckIDs      []string
	RequiredEvidenceTypes []string
	RequiredCIGates       []string
}

// ReleaseBundleConformanceResult is the observed result for one release bundle contract case.
type ReleaseBundleConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckReleaseBundleConformance runs reusable release bundle contract cases.
func CheckReleaseBundleConformance(ctx context.Context, harness ReleaseBundleConformanceHarness) []ReleaseBundleConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []ReleaseBundleConformanceResult{failedReleaseBundleConformance("context", err)}
	}

	requiredCheckIDs, checkIDErr := releaseBundleConformanceRequiredValues(
		"required check id",
		harness.RequiredCheckIDs,
		harness.Bundle.RequiredCheckIDs,
	)
	requiredEvidenceTypes, evidenceTypeErr := releaseBundleConformanceRequiredValues(
		"required evidence type",
		harness.RequiredEvidenceTypes,
		harness.Bundle.RequiredEvidenceTypes,
	)
	requiredCIGates, ciGateErr := releaseBundleConformanceRequiredValues(
		"required CI gate",
		harness.RequiredCIGates,
		harness.Bundle.RequiredCIGates,
	)

	results := []ReleaseBundleConformanceResult{
		checkReleaseBundleValid(harness.Bundle),
		checkReleaseBundleRequiredValues("required-check-ids", checkIDErr, func() []string {
			return requiredCheckReasons(harness.Bundle.VerificationReport, requiredCheckIDs)
		}),
		checkReleaseBundleRequiredValues("required-evidence-types", evidenceTypeErr, func() []string {
			return requiredEvidenceReasons(harness.Bundle.VerificationReport, requiredEvidenceTypes)
		}),
		checkReleaseBundleRequiredValues("required-ci-gates", ciGateErr, func() []string {
			return requiredCIGateReasons(harness.Bundle.VerificationReport, requiredCIGates)
		}),
		checkReleaseBundleWorkflowAlignment(harness.Bundle),
		checkReleaseBundleEvidence(harness.Bundle, requiredCheckIDs, requiredEvidenceTypes, requiredCIGates),
	}
	return results
}

// RequireReleaseBundleConformance fails the test unless bundle satisfies the release bundle contract.
func RequireReleaseBundleConformance(t testing.TB, harness ReleaseBundleConformanceHarness) {
	t.Helper()

	for _, result := range CheckReleaseBundleConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("release bundle conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkReleaseBundleValid(bundle ReleaseBundle) ReleaseBundleConformanceResult {
	if err := bundle.Validate(); err != nil {
		return failedReleaseBundleConformance("valid-bundle", err)
	}
	return passedReleaseBundleConformance("valid-bundle")
}

func checkReleaseBundleRequiredValues(
	name string,
	normalizeErr error,
	reasons func() []string,
) ReleaseBundleConformanceResult {
	if normalizeErr != nil {
		return failedReleaseBundleConformance(name, normalizeErr)
	}
	if missing := reasons(); len(missing) > 0 {
		return failedReleaseBundleConformance(name, errors.New(strings.Join(missing, "; ")))
	}
	return passedReleaseBundleConformance(name)
}

func checkReleaseBundleWorkflowAlignment(bundle ReleaseBundle) ReleaseBundleConformanceResult {
	workflowID := workflowProcessStringMetadata(bundle.Process.Task.Metadata, "workflow_id")
	if workflowID == "" && bundle.Process.Task.ParentID == "" {
		return passedReleaseBundleConformance("workflow-release-alignment")
	}
	if workflowID == "" {
		return failedReleaseBundleConformance(
			"workflow-release-alignment",
			errors.New("bundle process task workflow_id is required when parent id is set"),
		)
	}
	if bundle.Process.Task.ParentID != workflowID {
		return failedReleaseBundleConformance(
			"workflow-release-alignment",
			fmt.Errorf("bundle process task parent id = %q, want workflow_id %q", bundle.Process.Task.ParentID, workflowID),
		)
	}
	parent, ok := releaseBundleWorkflowParentTask(bundle.RunExport.Tasks, workflowID)
	if !ok {
		return failedReleaseBundleConformance(
			"workflow-release-alignment",
			fmt.Errorf("run export workflow parent task %q is required", workflowID),
		)
	}
	if err := validateReleaseBundleWorkflowSummary(bundle, parent); err != nil {
		return failedReleaseBundleConformance("workflow-release-alignment", err)
	}
	return passedReleaseBundleConformance("workflow-release-alignment")
}

func checkReleaseBundleEvidence(
	bundle ReleaseBundle,
	requiredCheckIDs []string,
	requiredEvidenceTypes []string,
	requiredCIGates []string,
) ReleaseBundleConformanceResult {
	recorder := gopact.NewVerificationRecorder()
	if err := RecordReleaseBundleCheck(recorder, bundle); err != nil {
		return failedReleaseBundleConformance("release-bundle-evidence", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		return failedReleaseBundleConformance(
			"release-bundle-evidence",
			fmt.Errorf("release bundle check count = %d, want 1", len(checks)),
		)
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusPassed {
		return failedReleaseBundleConformance(
			"release-bundle-evidence",
			fmt.Errorf("release bundle check status = %s, want passed", check.Status),
		)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeReleaseBundle {
		return failedReleaseBundleConformance(
			"release-bundle-evidence",
			fmt.Errorf("release bundle evidence = %+v, want one %s evidence", check.Evidence, VerificationEvidenceTypeReleaseBundle),
		)
	}
	for _, metadata := range []map[string]any{check.Metadata, check.Evidence[0].Metadata} {
		if err := releaseBundleConformanceMetadata(metadata, requiredCheckIDs, requiredEvidenceTypes, requiredCIGates); err != nil {
			return failedReleaseBundleConformance("release-bundle-evidence", err)
		}
	}
	return passedReleaseBundleConformance("release-bundle-evidence")
}

func releaseBundleWorkflowParentTask(records []gopact.TaskRecord, id string) (gopact.TaskRecord, bool) {
	for _, record := range records {
		if record.ID == id {
			return record, true
		}
	}
	return gopact.TaskRecord{}, false
}

func validateReleaseBundleWorkflowSummary(bundle ReleaseBundle, parent gopact.TaskRecord) error {
	actionIndex, ok := workflowProcessIntMetadata(bundle.Process.Task.Metadata, "workflow_action_index")
	if !ok {
		return fmt.Errorf("bundle process task workflow_action_index = %v, want integer", bundle.Process.Task.Metadata["workflow_action_index"])
	}
	output, ok := parent.Output.(map[string]any)
	if !ok {
		return fmt.Errorf("workflow parent output = %T, want map", parent.Output)
	}
	summaries, err := workflowProcessActionSummaries(output)
	if err != nil {
		return err
	}
	if actionIndex < 1 || actionIndex > len(summaries) {
		return fmt.Errorf("workflow release action index = %d, want 1..%d", actionIndex, len(summaries))
	}
	summary := summaries[actionIndex-1]
	for _, expected := range []struct {
		name string
		got  string
		want string
	}{
		{name: "task_id", got: workflowProcessStringMetadata(summary, "task_id"), want: bundle.Process.Task.ID},
		{name: "mode", got: workflowProcessStringMetadata(summary, "mode"), want: workflowProcessStringMetadata(bundle.Process.Task.Metadata, "mode")},
		{name: "action", got: workflowProcessStringMetadata(summary, "action"), want: workflowProcessStringMetadata(bundle.Process.Task.Metadata, "action")},
		{name: "action_status", got: workflowProcessStringMetadata(summary, "action_status"), want: workflowProcessStringMetadata(bundle.Process.Task.Metadata, "action_status")},
		{name: "release_gate_input_id", got: workflowProcessStringMetadata(summary, "release_gate_input_id"), want: releaseGateProcessInputID(bundle.Process.Inputs)},
		{name: "resume_input_id", got: workflowProcessStringMetadata(summary, "resume_input_id"), want: workflowResumeInputID(bundle.Process.Inputs)},
		{name: "review_intervention_id", got: workflowProcessStringMetadata(summary, "review_intervention_id"), want: reviewProcessInterventionID(bundle.Process.Interventions)},
	} {
		if expected.want != "" && expected.got != expected.want {
			return fmt.Errorf("workflow release summary %s = %q, want %q", expected.name, expected.got, expected.want)
		}
	}
	for _, expected := range []struct {
		name string
		got  any
		want int
	}{
		{name: "input_count", got: summary["input_count"], want: releaseBundleWorkflowInputCount(bundle.Process.Inputs, actionIndex)},
		{name: "intervention_count", got: summary["intervention_count"], want: releaseBundleWorkflowInterventionCount(bundle.Process.Interventions, actionIndex)},
	} {
		got, ok := workflowProcessIntMetadata(summary, expected.name)
		if !ok || got != expected.want {
			return fmt.Errorf("workflow release summary %s = %v, want %d", expected.name, expected.got, expected.want)
		}
	}
	return nil
}

func releaseBundleWorkflowInputCount(records []gopact.InputRecord, actionIndex int) int {
	count := 0
	for _, record := range records {
		if got, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok && got == actionIndex {
			count++
		}
	}
	return count
}

func releaseBundleWorkflowInterventionCount(records []gopact.InterventionRecord, actionIndex int) int {
	count := 0
	for _, record := range records {
		if got, ok := workflowProcessIntMetadata(record.Metadata, "workflow_action_index"); ok && got == actionIndex {
			count++
		}
	}
	return count
}

func releaseBundleConformanceRequiredValues(label string, explicit, bundled []string) ([]string, error) {
	if len(explicit) > 0 {
		return normalizeRequiredValues(label, explicit)
	}
	return normalizeRequiredValues(label, bundled)
}

func releaseBundleConformanceMetadata(
	metadata map[string]any,
	requiredCheckIDs []string,
	requiredEvidenceTypes []string,
	requiredCIGates []string,
) error {
	if err := releaseBundleConformanceMetadataSlice(metadata, "required_check_ids", requiredCheckIDs); err != nil {
		return err
	}
	if err := releaseBundleConformanceMetadataSlice(metadata, "required_evidence_types", requiredEvidenceTypes); err != nil {
		return err
	}
	return releaseBundleConformanceMetadataSlice(metadata, "required_ci_gates", requiredCIGates)
}

func releaseBundleConformanceMetadataSlice(metadata map[string]any, key string, want []string) error {
	if len(want) == 0 {
		return nil
	}
	got, ok := metadata[key].([]string)
	if !ok {
		return fmt.Errorf("release bundle metadata %s = %T, want []string", key, metadata[key])
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("release bundle metadata %s = %#v, want %#v", key, got, want)
	}
	return nil
}

func passedReleaseBundleConformance(name string) ReleaseBundleConformanceResult {
	return ReleaseBundleConformanceResult{Case: name, Passed: true}
}

func failedReleaseBundleConformance(name string, err error) ReleaseBundleConformanceResult {
	return ReleaseBundleConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrReleaseBundleConformanceFailed, err),
	}
}
