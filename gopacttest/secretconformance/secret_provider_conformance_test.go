package secretconformance

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckSecretProviderConformancePassesWellBehavedProvider(t *testing.T) {
	ref := gopact.SecretRef{Name: "provider/openai", Version: "2026-06"}
	raw := []byte("test-secret-value")
	provider := gopact.SecretProviderFunc(func(ctx context.Context, got gopact.SecretRef) (gopact.SecretValue, error) {
		if err := ctx.Err(); err != nil {
			return gopact.SecretValue{}, err
		}
		if got != ref {
			t.Fatalf("ResolveSecret ref = %+v, want %+v", got, ref)
		}
		return gopact.NewSecretValue(raw), nil
	})

	harness := SecretProviderConformanceHarness{
		Provider:    provider,
		Ref:         ref,
		ExpectedRaw: raw,
	}
	results := CheckSecretProviderConformance(context.Background(), harness)
	if failed := failedSecretProviderConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckSecretProviderConformance() failed cases: %v", failed)
	}
	RequireSecretProviderConformance(t, harness)
}

func TestSecretProviderConformanceAllowsRawSecretOverlappingRedactionToken(t *testing.T) {
	ref := gopact.SecretRef{Name: "provider/openai", Version: "2026-06"}
	raw := []byte("RED")
	provider := gopact.SecretProviderFunc(func(ctx context.Context, got gopact.SecretRef) (gopact.SecretValue, error) {
		if err := ctx.Err(); err != nil {
			return gopact.SecretValue{}, err
		}
		if err := gopact.ValidateSecretRef(got); err != nil {
			return gopact.SecretValue{}, err
		}
		if got != ref {
			return gopact.SecretValue{}, errors.New("unexpected ref")
		}
		return gopact.NewSecretValue(raw), nil
	})

	results := CheckSecretProviderConformance(context.Background(), SecretProviderConformanceHarness{
		Provider:    provider,
		Ref:         ref,
		ExpectedRaw: raw,
	})
	if failed := failedSecretProviderConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckSecretProviderConformance() failed cases: %v", failed)
	}
}

func TestCheckSecretProviderConformanceReportsBrokenProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider gopact.SecretProvider
		want     string
	}{
		{name: "nil provider", provider: nil, want: "has-provider"},
		{name: "ignores canceled context", provider: brokenSecretProvider{fault: "ignore_context"}, want: "resolve-respects-canceled-context"},
		{name: "accepts invalid ref", provider: brokenSecretProvider{fault: "accept_invalid_ref"}, want: "rejects-invalid-ref"},
		{name: "returns wrong secret", provider: brokenSecretProvider{fault: "wrong_secret"}, want: "resolves-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := CheckSecretProviderConformance(context.Background(), SecretProviderConformanceHarness{
				Provider:    tt.provider,
				Ref:         gopact.SecretRef{Name: "provider/openai", Version: "2026-06"},
				ExpectedRaw: []byte("expected-secret"),
			})
			if !hasFailedSecretProviderConformanceCase(results, tt.want) {
				t.Fatalf("CheckSecretProviderConformance() did not report %s: %+v", tt.want, results)
			}
		})
	}
}

type brokenSecretProvider struct {
	fault string
}

func (p brokenSecretProvider) ResolveSecret(ctx context.Context, ref gopact.SecretRef) (gopact.SecretValue, error) {
	if p.fault != "ignore_context" {
		if err := ctx.Err(); err != nil {
			return gopact.SecretValue{}, err
		}
	}
	if p.fault != "accept_invalid_ref" {
		if err := gopact.ValidateSecretRef(ref); err != nil {
			return gopact.SecretValue{}, err
		}
	}
	if p.fault == "accept_invalid_ref" && ref.Name == "" {
		return gopact.NewSecretValue([]byte("accepted-invalid-ref")), nil
	}
	if p.fault == "wrong_secret" {
		return gopact.NewSecretValue([]byte("wrong-secret")), nil
	}
	if p.fault == "leaky_error" {
		return gopact.SecretValue{}, errors.New("very-sensitive-expected-secret")
	}
	return gopact.NewSecretValue([]byte("expected-secret")), nil
}

func failedSecretProviderConformanceCases(results []SecretProviderConformanceResult) []string {
	failed := []string{}
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedSecretProviderConformanceCase(results []SecretProviderConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

func TestSecretProviderConformanceDoesNotExposeRawSecretInFailureHelpers(t *testing.T) {
	results := CheckSecretProviderConformance(context.Background(), SecretProviderConformanceHarness{
		Provider:    brokenSecretProvider{fault: "leaky_error"},
		Ref:         gopact.SecretRef{Name: "provider/openai"},
		ExpectedRaw: []byte("very-sensitive-expected-secret"),
	})

	for _, result := range results {
		if result.Err == nil {
			continue
		}
		if bytes.Contains([]byte(result.Err.Error()), []byte("very-sensitive-expected-secret")) {
			t.Fatalf("conformance error leaked raw secret: %v", result.Err)
		}
		if errors.Is(result.Err, ErrSecretProviderConformanceFailed) {
			return
		}
	}
	t.Fatal("expected at least one conformance failure wrapping ErrSecretProviderConformanceFailed")
}
