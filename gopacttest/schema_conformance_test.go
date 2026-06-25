package gopacttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckPortableJSONSchemaValidatorConformancePassesDefaultValidator(t *testing.T) {
	results := CheckPortableJSONSchemaValidatorConformance(context.Background(), nil)
	if len(results) == 0 {
		t.Fatal("CheckPortableJSONSchemaValidatorConformance() returned no cases")
	}
	for _, result := range results {
		if !result.Passed {
			t.Fatalf("case %q passed = false, err = %v", result.Case.Name, result.Err)
		}
	}
}

func TestCheckPortableJSONSchemaValidatorConformanceReportsBadValidator(t *testing.T) {
	validator := gopact.JSONSchemaValidatorFunc(func(context.Context, gopact.JSONSchema, any) error {
		return nil
	})

	results := CheckPortableJSONSchemaValidatorConformance(context.Background(), validator)
	if !hasFailedConformanceCase(results, "rejects-required-field") {
		t.Fatalf("CheckPortableJSONSchemaValidatorConformance() did not report required-field failure: %+v", results)
	}
}

func TestCheckPortableJSONSchemaValidatorConformancePreservesUnexpectedErrors(t *testing.T) {
	wantErr := errors.New("validator offline")
	validator := gopact.JSONSchemaValidatorFunc(func(context.Context, gopact.JSONSchema, any) error {
		return wantErr
	})

	results := CheckPortableJSONSchemaValidatorConformance(context.Background(), validator)
	if len(results) == 0 || results[0].Err == nil || !errors.Is(results[0].Err, wantErr) {
		t.Fatalf("first result error = %v, want %v", resultError(results), wantErr)
	}
}

func hasFailedConformanceCase(results []JSONSchemaValidatorConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case.Name == name && !result.Passed {
			return true
		}
	}
	return false
}

func resultError(results []JSONSchemaValidatorConformanceResult) error {
	if len(results) == 0 {
		return nil
	}
	return results[0].Err
}
