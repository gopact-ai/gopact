package provider

import (
	"errors"
	"testing"
)

func TestClassifyReturnsWrappedProviderErrorClass(t *testing.T) {
	err := NewError(ErrorUnavailable, errors.New("upstream down"))

	if got := Classify(err); got != ErrorUnavailable {
		t.Fatalf("Classify() = %q, want unavailable", got)
	}
}

func TestClassifyUnknownError(t *testing.T) {
	if got := Classify(errors.New("plain")); got != ErrorUnknown {
		t.Fatalf("Classify() = %q, want unknown", got)
	}
}

func TestErrorUnwrap(t *testing.T) {
	base := errors.New("rate limited")
	err := NewError(ErrorRateLimited, base, WithErrorProvider("primary"), WithErrorModel("fast"))

	if !errors.Is(err, base) {
		t.Fatalf("errors.Is(%v, %v) = false, want true", err, base)
	}
	if err.Provider != "primary" || err.Model != "fast" {
		t.Fatalf("provider error = %+v", err)
	}
}
