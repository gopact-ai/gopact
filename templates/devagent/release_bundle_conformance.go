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
