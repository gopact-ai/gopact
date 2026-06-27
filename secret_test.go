package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSecretValueRedactsStringJSONAndCopiesBytes(t *testing.T) {
	raw := []byte("super-secret-token")
	value := NewSecretValue(raw)
	raw[0] = 'X'

	got := value.Bytes()
	if string(got) != "super-secret-token" {
		t.Fatalf("SecretValue.Bytes() = %q, want original secret copy", got)
	}
	got[0] = 'Y'
	if string(value.Bytes()) != "super-secret-token" {
		t.Fatalf("SecretValue.Bytes() exposed mutable backing storage")
	}

	for _, rendered := range []string{
		value.String(),
		fmt.Sprint(value),
		fmt.Sprintf("%+v", value),
	} {
		if strings.Contains(rendered, "super-secret-token") {
			t.Fatalf("rendered secret value %q leaks raw secret", rendered)
		}
		if !strings.Contains(rendered, "REDACTED") {
			t.Fatalf("rendered secret value %q should be explicitly redacted", rendered)
		}
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(SecretValue) error = %v", err)
	}
	if strings.Contains(string(encoded), "super-secret-token") {
		t.Fatalf("json secret value %s leaks raw secret", encoded)
	}
	if !strings.Contains(string(encoded), "REDACTED") {
		t.Fatalf("json secret value %s should be explicitly redacted", encoded)
	}
}

func TestSecretProviderFuncResolvesSecretByReference(t *testing.T) {
	wantRef := SecretRef{Name: "provider/openai", Version: "2026-06"}
	provider := SecretProviderFunc(func(ctx context.Context, ref SecretRef) (SecretValue, error) {
		if ctx == nil {
			t.Fatal("ResolveSecret context is nil")
		}
		if ref != wantRef {
			t.Fatalf("ResolveSecret ref = %+v, want %+v", ref, wantRef)
		}
		return NewSecretValue([]byte("api-key")), nil
	})

	value, err := provider.ResolveSecret(context.Background(), wantRef)
	if err != nil {
		t.Fatalf("ResolveSecret() error = %v", err)
	}
	if string(value.Bytes()) != "api-key" {
		t.Fatalf("resolved secret = %q, want api-key", value.Bytes())
	}
}

func TestSecretProviderFuncRejectsNilFunction(t *testing.T) {
	var provider SecretProviderFunc

	_, err := provider.ResolveSecret(context.Background(), SecretRef{Name: "provider/openai"})
	if err == nil {
		t.Fatal("nil SecretProviderFunc ResolveSecret() error = nil, want error")
	}
}

func TestPolicySecretProviderDenySkipsResolve(t *testing.T) {
	ref := SecretRef{Name: "provider/openai", Version: "2026-06"}
	provider := &recordingSecretProvider{value: NewSecretValue([]byte("raw-secret"))}
	var gotReq PolicyRequest
	events := []Event{}
	wrapped, err := NewPolicySecretProvider(
		provider,
		PolicyFunc(func(_ context.Context, req PolicyRequest) (PolicyDecision, error) {
			gotReq = req
			return PolicyDecision{Action: PolicyDeny, Reason: "secret not delegated"}, nil
		}),
		WithSecretPolicyIDs(RuntimeIDs{RunID: "run-1", UserID: "user-1"}),
		WithSecretPolicyMetadata(map[string]any{"source": "test"}),
		WithSecretPolicyEventSink(func(_ context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicySecretProvider() error = %v", err)
	}

	_, err = wrapped.ResolveSecret(context.Background(), ref)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("ResolveSecret() error = %v, want ErrPolicyDenied", err)
	}
	if provider.calls != 0 {
		t.Fatalf("underlying resolves = %d, want 0", provider.calls)
	}
	if gotReq.Boundary != PolicyBoundarySecret {
		t.Fatalf("policy boundary = %q, want secret", gotReq.Boundary)
	}
	if gotReq.Action != PolicyActionResolve {
		t.Fatalf("policy action = %q, want resolve", gotReq.Action)
	}
	if gotReq.IDs.RunID != "run-1" || gotReq.IDs.UserID != "user-1" {
		t.Fatalf("policy IDs = %+v, want run/user", gotReq.IDs)
	}
	if gotReq.Metadata["source"] != "test" {
		t.Fatalf("policy metadata = %+v, want source", gotReq.Metadata)
	}
	input, ok := gotReq.Input.(SecretPolicyInput)
	if !ok {
		t.Fatalf("policy input type = %T, want SecretPolicyInput", gotReq.Input)
	}
	if input.Ref != ref {
		t.Fatalf("policy input ref = %+v, want %+v", input.Ref, ref)
	}
	if len(events) != 2 || events[0].Type != EventPolicyRequested || events[1].Type != EventPolicyDecided {
		t.Fatalf("policy events = %+v, want requested/decided", events)
	}
}

func TestNewPolicySecretProviderRequiresDependencies(t *testing.T) {
	if _, err := NewPolicySecretProvider(nil, PolicyFunc(func(context.Context, PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{Action: PolicyAllow}, nil
	})); !errors.Is(err, ErrSecretProviderRequired) {
		t.Fatalf("NewPolicySecretProvider(nil, policy) error = %v, want ErrSecretProviderRequired", err)
	}
	if _, err := NewPolicySecretProvider(&recordingSecretProvider{}, nil); !errors.Is(err, ErrSecretPolicyRequired) {
		t.Fatalf("NewPolicySecretProvider(provider, nil) error = %v, want ErrSecretPolicyRequired", err)
	}
}

func TestPolicySecretProviderReviewReturnsApprovalInterrupt(t *testing.T) {
	provider := &recordingSecretProvider{value: NewSecretValue([]byte("raw-secret"))}
	wrapped, err := NewPolicySecretProvider(
		provider,
		PolicyFunc(func(_ context.Context, _ PolicyRequest) (PolicyDecision, error) {
			return PolicyDecision{Action: PolicyReview, Reason: "confirm secret delegation"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicySecretProvider() error = %v", err)
	}

	_, err = wrapped.ResolveSecret(context.Background(), SecretRef{Name: "provider/openai"})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("ResolveSecret() error = %v, want ErrInterrupted", err)
	}
	if provider.calls != 0 {
		t.Fatalf("underlying resolves = %d, want 0", provider.calls)
	}
	var interruptErr *InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("ResolveSecret() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(PolicyBoundarySecret) {
		t.Fatalf("RequiredBy = %q, want secret", interruptErr.Record.RequiredBy)
	}
}

func TestPolicySecretProviderAllowDoesNotExposeRawSecretInPolicyEvents(t *testing.T) {
	ref := SecretRef{Name: "provider/openai", Version: "2026-06"}
	provider := &recordingSecretProvider{value: NewSecretValue([]byte("very-sensitive-api-key"))}
	events := []Event{}
	wrapped, err := NewPolicySecretProvider(
		provider,
		PolicyFunc(func(_ context.Context, req PolicyRequest) (PolicyDecision, error) {
			input, ok := req.Input.(SecretPolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want SecretPolicyInput", req.Input)
			}
			if input.Ref != ref {
				t.Fatalf("policy ref = %+v, want %+v", input.Ref, ref)
			}
			return PolicyDecision{Action: PolicyAllow}, nil
		}),
		WithSecretPolicyEventSink(func(_ context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicySecretProvider() error = %v", err)
	}

	value, err := wrapped.ResolveSecret(context.Background(), ref)
	if err != nil {
		t.Fatalf("ResolveSecret() error = %v", err)
	}
	if string(value.Bytes()) != "very-sensitive-api-key" {
		t.Fatalf("resolved secret = %q, want raw value", value.Bytes())
	}
	encodedEvents, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("json.Marshal(events) error = %v", err)
	}
	if strings.Contains(string(encodedEvents), "very-sensitive-api-key") {
		t.Fatalf("policy events leak raw secret: %s", encodedEvents)
	}
}

func TestPolicySecretProviderRejectsInvalidRefBeforePolicy(t *testing.T) {
	provider := &recordingSecretProvider{value: NewSecretValue([]byte("raw-secret"))}
	policyCalled := false
	wrapped, err := NewPolicySecretProvider(
		provider,
		PolicyFunc(func(context.Context, PolicyRequest) (PolicyDecision, error) {
			policyCalled = true
			return PolicyDecision{Action: PolicyAllow}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicySecretProvider() error = %v", err)
	}

	_, err = wrapped.ResolveSecret(context.Background(), SecretRef{})
	if !errors.Is(err, ErrSecretRefInvalid) {
		t.Fatalf("ResolveSecret() error = %v, want ErrSecretRefInvalid", err)
	}
	if policyCalled {
		t.Fatal("policy should not run for invalid secret ref")
	}
	if provider.calls != 0 {
		t.Fatalf("underlying resolves = %d, want 0", provider.calls)
	}
}

func TestValidateSecretRefRejectsEmptyName(t *testing.T) {
	if err := ValidateSecretRef(SecretRef{Name: " provider/openai "}); err != nil {
		t.Fatalf("ValidateSecretRef(valid) error = %v", err)
	}

	err := ValidateSecretRef(SecretRef{})
	if !errors.Is(err, ErrSecretRefInvalid) {
		t.Fatalf("ValidateSecretRef(empty) error = %v, want ErrSecretRefInvalid", err)
	}
}

type recordingSecretProvider struct {
	value SecretValue
	calls int
}

func (p *recordingSecretProvider) ResolveSecret(context.Context, SecretRef) (SecretValue, error) {
	p.calls++
	return p.value, nil
}
