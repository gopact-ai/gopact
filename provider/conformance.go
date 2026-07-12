package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// ConformanceCase verifies one provider-neutral model behavior.
type ConformanceCase struct {
	Name string
	Run  func(context.Context, gopact.Model) error
}

// ConformanceSpec configures a provider conformance harness.
type ConformanceSpec struct {
	Cases []ConformanceCase
}

// ConformanceHarness runs provider-neutral model checks.
type ConformanceHarness struct {
	cases []ConformanceCase
}

// NewConformanceHarness creates a provider conformance harness.
func NewConformanceHarness(spec ConformanceSpec) *ConformanceHarness {
	return &ConformanceHarness{cases: append([]ConformanceCase(nil), spec.Cases...)}
}

// StandardConformanceCases returns the default offline conformance cases.
func StandardConformanceCases() []ConformanceCase {
	return []ConformanceCase{
		{
			Name: "new_request_isolates_messages",
			Run: func(_ context.Context, model gopact.Model) error {
				req := model.NewRequest(gopact.UserMessage("hello"))
				if len(req.Messages) != 1 {
					return fmt.Errorf("new request messages = %d, want 1", len(req.Messages))
				}
				req.Messages[0].Parts[0].Text = "mutated"
				next := model.NewRequest(gopact.UserMessage("hello"))
				if len(next.Messages) != 1 || next.Messages[0].Parts[0].Text != "hello" {
					return errors.New("new request did not isolate message mutation")
				}
				return nil
			},
		},
		{
			Name: "invoke_accepts_materialized_request",
			Run: func(ctx context.Context, model gopact.Model) error {
				_, err := model.Invoke(ctx, model.NewRequest(gopact.UserMessage("hello")))
				return err
			},
		},
	}
}

// Run executes all conformance cases against model.
func (h *ConformanceHarness) Run(ctx context.Context, model gopact.Model) error {
	if model == nil {
		return errors.New("provider: model is nil")
	}
	cases := h.cases
	if len(cases) == 0 {
		cases = StandardConformanceCases()
	}
	for _, c := range cases {
		if c.Name == "" {
			return errors.New("provider: conformance case name is required")
		}
		if c.Run == nil {
			return fmt.Errorf("provider: conformance case %q has nil run function", c.Name)
		}
		if err := c.Run(ctx, model); err != nil {
			return fmt.Errorf("provider: conformance case %q: %w", c.Name, err)
		}
	}
	return nil
}
