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

func TestValidateSecretRefRejectsEmptyName(t *testing.T) {
	if err := ValidateSecretRef(SecretRef{Name: " provider/openai "}); err != nil {
		t.Fatalf("ValidateSecretRef(valid) error = %v", err)
	}

	err := ValidateSecretRef(SecretRef{})
	if !errors.Is(err, ErrSecretRefInvalid) {
		t.Fatalf("ValidateSecretRef(empty) error = %v, want ErrSecretRefInvalid", err)
	}
}
