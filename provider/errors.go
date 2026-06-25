package provider

import (
	"errors"
	"fmt"
)

var (
	// ErrProviderExists is returned when registering a duplicate provider name.
	ErrProviderExists = errors.New("provider: provider already exists")
	// ErrRouteNotFound is returned when no configured route can serve a request.
	ErrRouteNotFound = errors.New("provider: route not found")
)

// ErrorClass is a normalized provider error category.
type ErrorClass string

const (
	// ErrorUnknown is the fallback provider error class.
	ErrorUnknown ErrorClass = "unknown"
	// ErrorRateLimited marks provider rate-limit failures.
	ErrorRateLimited ErrorClass = "rate_limited"
	// ErrorTimeout marks provider timeout failures.
	ErrorTimeout ErrorClass = "timeout"
	// ErrorUnavailable marks temporary provider unavailability.
	ErrorUnavailable ErrorClass = "unavailable"
	// ErrorQuotaExceeded marks quota exhaustion failures.
	ErrorQuotaExceeded ErrorClass = "quota_exceeded"
	// ErrorInvalidRequest marks provider-side request validation failures.
	ErrorInvalidRequest ErrorClass = "invalid_request"
	// ErrorUnauthorized marks provider authentication or authorization failures.
	ErrorUnauthorized ErrorClass = "unauthorized"
	// ErrorCapabilityMismatch marks requests that require unsupported model capabilities.
	ErrorCapabilityMismatch ErrorClass = "capability_mismatch"
)

// Error wraps provider errors with a normalized class.
type Error struct {
	Class    ErrorClass
	Provider string
	Model    string
	Err      error
}

// ErrorOption configures a provider error.
type ErrorOption func(*Error)

// NewError creates a normalized provider error.
func NewError(class ErrorClass, err error, opts ...ErrorOption) *Error {
	if class == "" {
		class = ErrorUnknown
	}
	if err == nil {
		err = errors.New(string(class))
	}
	providerErr := &Error{Class: class, Err: err}
	for _, opt := range opts {
		if opt != nil {
			opt(providerErr)
		}
	}
	return providerErr
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Provider == "" && e.Model == "" {
		return fmt.Sprintf("provider: %s: %v", e.Class, e.Err)
	}
	return fmt.Sprintf("provider: %s provider=%s model=%s: %v", e.Class, e.Provider, e.Model, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// WithErrorProvider records the provider that produced the error.
func WithErrorProvider(provider string) ErrorOption {
	return func(err *Error) {
		err.Provider = provider
	}
}

// WithErrorModel records the model that produced the error.
func WithErrorModel(model string) ErrorOption {
	return func(err *Error) {
		err.Model = model
	}
}

// Classify returns the normalized provider error class.
func Classify(err error) ErrorClass {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr.Class
	}
	return ErrorUnknown
}
