package workflow

import (
	"errors"
	"strings"
	"testing"
)

func TestResolvePendingInterruptsRequiresExactUniqueSet(t *testing.T) {
	pending := []checkpointInterrupt{
		{
			Request: InterruptRequest{ID: "first"}, GuardName: "first-guard",
			NodeName: "first-node", ActivationID: "first-activation",
		},
		{
			Request: InterruptRequest{ID: "second"}, GuardName: "second-guard",
			NodeName: "second-node", ActivationID: "second-activation",
		},
	}
	tests := []struct {
		name        string
		resolutions []InterruptResolution
		wantErr     string
	}{
		{
			name: "exact in arbitrary order",
			resolutions: []InterruptResolution{
				{InterruptID: "second", PayloadRef: "artifact://second"},
				{InterruptID: "first", PayloadRef: "artifact://first"},
			},
		},
		{
			name: "missing",
			resolutions: []InterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://first"},
			},
			wantErr: `interrupt resolution "second" is required`,
		},
		{
			name: "duplicate",
			resolutions: []InterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://first"},
				{InterruptID: "first", PayloadRef: "artifact://other"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
			},
			wantErr: `duplicate interrupt resolution "first"`,
		},
		{
			name: "extra",
			resolutions: []InterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://first"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
				{InterruptID: "extra", PayloadRef: "artifact://extra"},
			},
			wantErr: `interrupt resolution "extra" is unexpected`,
		},
		{
			name: "empty id",
			resolutions: []InterruptResolution{
				{PayloadRef: "artifact://first"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
			},
			wantErr: "interrupt resolution id is required",
		},
		{
			name: "empty payload ref",
			resolutions: []InterruptResolution{
				{InterruptID: "first"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
			},
			wantErr: `interrupt resolution "first" payload ref is required`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := resolvePendingInterrupts(pending, test.resolutions)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("resolvePendingInterrupts() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePendingInterrupts() error = %v", err)
			}
			if len(resolved) != 2 ||
				resolved[0].InterruptID != "first" || resolved[0].PayloadRef != "artifact://first" ||
				resolved[0].GuardName != "first-guard" || resolved[0].ActivationID != "first-activation" ||
				resolved[1].InterruptID != "second" || resolved[1].PayloadRef != "artifact://second" ||
				resolved[1].GuardName != "second-guard" || resolved[1].ActivationID != "second-activation" {
				t.Fatalf("resolvePendingInterrupts() = %+v, want pending-order associations", resolved)
			}
		})
	}
}

func TestResolvePendingInterruptsRejectsCorruptPendingIDs(t *testing.T) {
	tests := []struct {
		name    string
		pending []checkpointInterrupt
	}{
		{name: "missing"},
		{name: "empty", pending: []checkpointInterrupt{{Request: InterruptRequest{}}}},
		{
			name: "duplicate",
			pending: []checkpointInterrupt{
				{Request: InterruptRequest{ID: "approval"}},
				{Request: InterruptRequest{ID: "approval"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolvePendingInterrupts(test.pending, nil)
			if !errors.Is(err, ErrInvalidCheckpoint) {
				t.Fatalf("resolvePendingInterrupts() error = %v, want ErrInvalidCheckpoint", err)
			}
		})
	}
}
