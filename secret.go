package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrSecretRefInvalid is returned when a secret reference is empty or malformed.
var ErrSecretRefInvalid = errors.New("gopact: secret reference invalid")

// SecretRef is a stable, non-secret reference to a secret owned by the host application.
type SecretRef struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// NewSecretRef creates a secret reference from a host-owned name.
func NewSecretRef(name string) SecretRef {
	return SecretRef{Name: strings.TrimSpace(name)}
}

// String returns the printable secret reference. It never resolves the secret value.
func (r SecretRef) String() string {
	name := strings.TrimSpace(r.Name)
	version := strings.TrimSpace(r.Version)
	if version == "" {
		return name
	}
	return name + "@" + version
}

// ValidateSecretRef checks that a secret reference can be passed to a provider.
func ValidateSecretRef(ref SecretRef) error {
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrSecretRefInvalid)
	}
	return nil
}

// SecretValue is a resolved secret. Its textual and JSON forms are always redacted.
type SecretValue struct {
	value []byte
}

// NewSecretValue creates a resolved secret value by copying b.
func NewSecretValue(b []byte) SecretValue {
	return SecretValue{value: append([]byte(nil), b...)}
}

// Bytes returns a copy of the raw secret bytes for the adapter that explicitly requested it.
func (v SecretValue) Bytes() []byte {
	return append([]byte(nil), v.value...)
}

// String returns a stable redacted representation.
func (v SecretValue) String() string {
	return "[REDACTED]"
}

// Format ensures fmt never prints raw secret bytes for any verb.
func (v SecretValue) Format(s fmt.State, _ rune) {
	_, _ = fmt.Fprint(s, v.String())
}

// MarshalJSON redacts secret values if they are accidentally marshaled.
func (v SecretValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

// SecretProvider resolves host-owned secret references. Implementations must not leak values.
type SecretProvider interface {
	ResolveSecret(ctx context.Context, ref SecretRef) (SecretValue, error)
}

// SecretProviderFunc adapts a function into a SecretProvider.
type SecretProviderFunc func(ctx context.Context, ref SecretRef) (SecretValue, error)

// ResolveSecret calls f.
func (f SecretProviderFunc) ResolveSecret(ctx context.Context, ref SecretRef) (SecretValue, error) {
	if f == nil {
		return SecretValue{}, errors.New("gopact: secret provider function is nil")
	}
	if err := ValidateSecretRef(ref); err != nil {
		return SecretValue{}, err
	}
	return f(ctx, ref)
}
