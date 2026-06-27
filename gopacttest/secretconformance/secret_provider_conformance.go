// Package secretconformance provides reusable secret provider contract tests.
package secretconformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/gopact-ai/gopact"
)

// ErrSecretProviderConformanceFailed reports a failed SecretProvider conformance case.
var ErrSecretProviderConformanceFailed = errors.New("gopacttest: secret provider conformance failed")

// SecretProviderConformanceHarness describes one SecretProvider implementation under test.
type SecretProviderConformanceHarness struct {
	Provider    gopact.SecretProvider
	Ref         gopact.SecretRef
	ExpectedRaw []byte
}

// SecretProviderConformanceResult is the observed result for one SecretProvider contract case.
type SecretProviderConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckSecretProviderConformance runs reusable SecretProvider contract cases.
func CheckSecretProviderConformance(
	ctx context.Context,
	harness SecretProviderConformanceHarness,
) []SecretProviderConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return []SecretProviderConformanceResult{failedSecretProviderConformance("context", err)}
	}
	ref, expected := normalizeSecretProviderConformanceFixture(harness.Ref, harness.ExpectedRaw)

	return []SecretProviderConformanceResult{
		checkSecretProviderPresent(harness.Provider),
		checkSecretProviderCanceledContext(harness.Provider, ref),
		checkSecretProviderRejectsInvalidRef(ctx, harness.Provider),
		checkSecretProviderResolvesSecret(ctx, harness.Provider, ref, expected),
		checkSecretProviderValueRedactsOutput(ctx, harness.Provider, ref, expected),
		checkSecretProviderValueReturnsCopy(ctx, harness.Provider, ref, expected),
	}
}

// RequireSecretProviderConformance fails the test unless provider satisfies the SecretProvider contract.
func RequireSecretProviderConformance(t testing.TB, harness SecretProviderConformanceHarness) {
	t.Helper()

	for _, result := range CheckSecretProviderConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("secret provider conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkSecretProviderPresent(provider gopact.SecretProvider) SecretProviderConformanceResult {
	if provider == nil {
		return failedSecretProviderConformance("has-provider", errors.New("secret provider is nil"))
	}
	return passedSecretProviderConformance("has-provider")
}

func checkSecretProviderCanceledContext(
	provider gopact.SecretProvider,
	ref gopact.SecretRef,
) SecretProviderConformanceResult {
	if provider == nil {
		return failedSecretProviderConformance("resolve-respects-canceled-context", errors.New("secret provider is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.ResolveSecret(ctx, ref); !errors.Is(err, context.Canceled) {
		return failedSecretProviderConformance(
			"resolve-respects-canceled-context",
			fmt.Errorf(
				"resolve canceled context error kind = %s, want context.Canceled",
				secretProviderConformanceErrorKind(err),
			),
		)
	}
	return passedSecretProviderConformance("resolve-respects-canceled-context")
}

func checkSecretProviderRejectsInvalidRef(
	ctx context.Context,
	provider gopact.SecretProvider,
) SecretProviderConformanceResult {
	if provider == nil {
		return failedSecretProviderConformance("rejects-invalid-ref", errors.New("secret provider is nil"))
	}
	if _, err := provider.ResolveSecret(ctx, gopact.SecretRef{}); !errors.Is(err, gopact.ErrSecretRefInvalid) {
		return failedSecretProviderConformance(
			"rejects-invalid-ref",
			fmt.Errorf(
				"resolve invalid ref error kind = %s, want ErrSecretRefInvalid",
				secretProviderConformanceErrorKind(err),
			),
		)
	}
	return passedSecretProviderConformance("rejects-invalid-ref")
}

func checkSecretProviderResolvesSecret(
	ctx context.Context,
	provider gopact.SecretProvider,
	ref gopact.SecretRef,
	expected []byte,
) SecretProviderConformanceResult {
	value, err := resolveSecretProviderConformanceValue(ctx, provider, ref)
	if err != nil {
		return failedSecretProviderConformance("resolves-secret", err)
	}
	got := value.Bytes()
	if !bytes.Equal(got, expected) {
		return failedSecretProviderConformance(
			"resolves-secret",
			fmt.Errorf("resolved secret bytes mismatch (got length %d, want length %d)", len(got), len(expected)),
		)
	}
	return passedSecretProviderConformance("resolves-secret")
}

func checkSecretProviderValueRedactsOutput(
	ctx context.Context,
	provider gopact.SecretProvider,
	ref gopact.SecretRef,
	expected []byte,
) SecretProviderConformanceResult {
	value, err := resolveSecretProviderConformanceValue(ctx, provider, ref)
	if err != nil {
		return failedSecretProviderConformance("secret-value-redacts-output", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return failedSecretProviderConformance("secret-value-redacts-output", fmt.Errorf("marshal secret value: %w", err))
	}

	renderedValues := [][]byte{
		[]byte(value.String()),
		[]byte(fmt.Sprint(value)),
		[]byte(fmt.Sprintf("%+v", value)),
		[]byte(fmt.Sprintf("%#v", value)),
		encoded,
	}
	for _, rendered := range renderedValues {
		if secretProviderConformanceContainsRawSecret(rendered, expected) {
			return failedSecretProviderConformance("secret-value-redacts-output", errors.New("rendered secret value leaks raw secret"))
		}
		if !bytes.Contains(rendered, []byte("REDACTED")) {
			return failedSecretProviderConformance("secret-value-redacts-output", errors.New("rendered secret value is not explicitly redacted"))
		}
	}
	return passedSecretProviderConformance("secret-value-redacts-output")
}

func checkSecretProviderValueReturnsCopy(
	ctx context.Context,
	provider gopact.SecretProvider,
	ref gopact.SecretRef,
	expected []byte,
) SecretProviderConformanceResult {
	value, err := resolveSecretProviderConformanceValue(ctx, provider, ref)
	if err != nil {
		return failedSecretProviderConformance("secret-value-returns-copy", err)
	}
	got := value.Bytes()
	if !bytes.Equal(got, expected) {
		return failedSecretProviderConformance(
			"secret-value-returns-copy",
			fmt.Errorf("secret bytes mismatch before mutation (got length %d, want length %d)", len(got), len(expected)),
		)
	}
	if len(got) > 0 {
		got[0] ^= 0xff
	}
	again := value.Bytes()
	if !bytes.Equal(again, expected) {
		return failedSecretProviderConformance("secret-value-returns-copy", errors.New("secret value bytes exposed mutable backing storage"))
	}
	return passedSecretProviderConformance("secret-value-returns-copy")
}

func resolveSecretProviderConformanceValue(
	ctx context.Context,
	provider gopact.SecretProvider,
	ref gopact.SecretRef,
) (gopact.SecretValue, error) {
	if provider == nil {
		return gopact.SecretValue{}, errors.New("secret provider is nil")
	}
	if err := gopact.ValidateSecretRef(ref); err != nil {
		return gopact.SecretValue{}, err
	}
	value, err := provider.ResolveSecret(ctx, ref)
	if err != nil {
		return gopact.SecretValue{}, fmt.Errorf(
			"resolve secret failed with error kind %s",
			secretProviderConformanceErrorKind(err),
		)
	}
	return value, nil
}

func normalizeSecretProviderConformanceFixture(ref gopact.SecretRef, raw []byte) (gopact.SecretRef, []byte) {
	if err := gopact.ValidateSecretRef(ref); err != nil {
		ref = gopact.SecretRef{Name: "gopact/conformance/secret", Version: "v1"}
	}
	if len(raw) == 0 {
		raw = []byte("gopact-conformance-secret")
	}
	return ref, append([]byte(nil), raw...)
}

func secretProviderConformanceContainsRawSecret(rendered []byte, raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	redactionMarker := []byte("[REDACTED]")
	if bytes.Contains(redactionMarker, raw) {
		return false
	}
	return bytes.Contains(rendered, raw)
}

func secretProviderConformanceErrorKind(err error) string {
	switch {
	case err == nil:
		return "<nil>"
	case errors.Is(err, context.Canceled):
		return "context.Canceled"
	case errors.Is(err, gopact.ErrSecretRefInvalid):
		return "gopact.ErrSecretRefInvalid"
	default:
		return fmt.Sprintf("%T", err)
	}
}

func passedSecretProviderConformance(name string) SecretProviderConformanceResult {
	return SecretProviderConformanceResult{Case: name, Passed: true}
}

func failedSecretProviderConformance(name string, err error) SecretProviderConformanceResult {
	return SecretProviderConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrSecretProviderConformanceFailed, err),
	}
}
