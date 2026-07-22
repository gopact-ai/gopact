package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
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

func TestWorkflowRejectsNodeInterruptWithoutIDBeforePersisting(t *testing.T) {
	store := NewMemoryStore()
	wf := New[string, string]("invalid-node-interrupt", WithStore(store))
	plan := testNode(wf, "plan", func(context.Context, string) (string, error) {
		return "", InterruptError{Request: InterruptRequest{}}
	})
	wf.Entry(plan)
	wf.Exit(plan)

	_, err := wf.Invoke(context.Background(), "input", gopact.WithRunID("invalid-node-interrupt"))
	if err == nil || !strings.Contains(err.Error(), "interrupt id is required") {
		t.Fatalf("Invoke() error = %v, want interrupt id error", err)
	}
	checkpoint, loadErr := store.Load(context.Background(), "invalid-node-interrupt")
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if checkpoint.Status == CheckpointInterrupted {
		t.Fatalf("checkpoint status = %q, must not persist invalid interrupt", checkpoint.Status)
	}
}

func TestValidateResolvedInterruptReplay(t *testing.T) {
	resolved := []checkpointInterruptResolution{
		{InterruptID: "first", PayloadRef: "artifact://first"},
		{InterruptID: "second", PayloadRef: "artifact://second"},
	}
	tests := []struct {
		name    string
		replay  []InterruptResolution
		wantErr string
	}{
		{name: "omitted"},
		{
			name: "exact in arbitrary order",
			replay: []InterruptResolution{
				{InterruptID: "second", PayloadRef: "artifact://second"},
				{InterruptID: "first", PayloadRef: "artifact://first"},
			},
		},
		{
			name:    "missing",
			replay:  []InterruptResolution{{InterruptID: "first", PayloadRef: "artifact://first"}},
			wantErr: `interrupt resolution "second" is required`,
		},
		{
			name: "duplicate",
			replay: []InterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://first"},
				{InterruptID: "first", PayloadRef: "artifact://first"},
			},
			wantErr: `duplicate interrupt resolution "first"`,
		},
		{
			name: "extra",
			replay: []InterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://first"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
				{InterruptID: "extra", PayloadRef: "artifact://extra"},
			},
			wantErr: `interrupt resolution "extra" is unexpected`,
		},
		{
			name: "empty id",
			replay: []InterruptResolution{
				{PayloadRef: "artifact://first"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
			},
			wantErr: "interrupt resolution id is required",
		},
		{
			name: "empty payload ref",
			replay: []InterruptResolution{
				{InterruptID: "first"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
			},
			wantErr: `interrupt resolution "first" payload ref is required`,
		},
		{
			name: "payload ref mismatch",
			replay: []InterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://other"},
				{InterruptID: "second", PayloadRef: "artifact://second"},
			},
			wantErr: `interrupt resolution "first" payload ref does not match checkpoint`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateResolvedInterruptReplay(resolved, test.replay)
			if test.wantErr == "" && err != nil {
				t.Fatalf("validateResolvedInterruptReplay() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("validateResolvedInterruptReplay() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateResolvedInterruptReplayRejectsCorruptCheckpoint(t *testing.T) {
	tests := []struct {
		name     string
		resolved []checkpointInterruptResolution
	}{
		{name: "empty id", resolved: []checkpointInterruptResolution{{PayloadRef: "artifact://first"}}},
		{name: "empty payload ref", resolved: []checkpointInterruptResolution{{InterruptID: "first"}}},
		{
			name: "duplicate id",
			resolved: []checkpointInterruptResolution{
				{InterruptID: "first", PayloadRef: "artifact://first"},
				{InterruptID: "first", PayloadRef: "artifact://other"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateResolvedInterruptReplay(test.resolved, nil); !errors.Is(err, ErrInvalidCheckpoint) {
				t.Fatalf("validateResolvedInterruptReplay() error = %v, want ErrInvalidCheckpoint", err)
			}
		})
	}
}

func TestWorkflowRunningResumeAcceptsOnlyExactResolutionReplay(t *testing.T) {
	store := &recordingCheckpointer{}
	guardCalls := 0
	bodyRuns := 0
	wf := New[string, string]("resolution-replay", WithStore(storeWithCheckpointer(store)))
	plan := testNode(wf, "plan", func(_ context.Context, input string) (string, error) {
		bodyRuns++
		return input + "!", nil
	})
	plan.Guard(BeforeRun("approval", GuardFunc[string, string](
		func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
			guardCalls++
			return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval"}}, nil
		},
	)))
	wf.Entry(plan)
	wf.Exit(plan)
	compiled, err := wf.compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = compiled.Invoke(context.Background(), "input", gopact.WithRunID("resolution-replay"))
	var interrupt InterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("Invoke() error = %v, want InterruptError", err)
	}
	resolution := InterruptResolution{InterruptID: "approval", PayloadRef: "artifact://approved"}
	sinkErr := errors.New("checkpoint loaded sink failed")
	_, err = compiled.Invoke(
		context.Background(),
		"ignored",
		WithResume(ResumeRequest{RunID: "resolution-replay", Resolutions: []InterruptResolution{resolution}}),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventCheckpointLoaded {
				return sinkErr
			}
			return nil
		}),
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("first resume error = %v, want sink failure", err)
	}
	if guardCalls != 1 || bodyRuns != 0 {
		t.Fatalf("calls after first resume = guard %d body %d, want 1/0", guardCalls, bodyRuns)
	}
	runningCheckpoint := store.records["resolution-replay"]
	if runningCheckpoint.Status != CheckpointRunning {
		t.Fatalf("checkpoint status = %q, want %q", runningCheckpoint.Status, CheckpointRunning)
	}
	runningPayload, err := decodeCheckpointPayload[string](runningCheckpoint.Payload)
	if err != nil {
		t.Fatalf("decode running checkpoint payload error = %v", err)
	}
	runningMeta := runningPayload.meta()
	runningMeta.LeaseExpiresAt = time.Now().Add(-time.Second)
	runningCheckpoint.LeaseExpiresAt = runningMeta.LeaseExpiresAt
	runningCheckpoint.Payload, err = encodeCheckpointPayloadWithMeta(
		runningPayload.state(), runningPayload.Outputs, runningPayload.NextStep, runningMeta,
	)
	if err != nil {
		t.Fatalf("encode expired running checkpoint payload error = %v", err)
	}
	store.records["resolution-replay"] = runningCheckpoint
	_, err = compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "resolution-replay",
		Resolutions: []InterruptResolution{{
			InterruptID: "approval", PayloadRef: "artifact://conflicting",
		}},
	}))
	if err == nil || !strings.Contains(err.Error(), "payload ref") {
		t.Fatalf("conflicting replay error = %v, want payload ref mismatch", err)
	}
	if current := store.records["resolution-replay"]; !reflect.DeepEqual(current, runningCheckpoint) {
		t.Fatalf("checkpoint after conflicting replay = %+v, want unchanged running checkpoint", current)
	}
	got, err := compiled.Invoke(context.Background(), "ignored", WithResume(ResumeRequest{
		RunID: "resolution-replay", Resolutions: []InterruptResolution{resolution},
	}))
	if err != nil || got != "input!" {
		t.Fatalf("exact replay Invoke() = %q, %v, want input!", got, err)
	}
	if guardCalls != 1 || bodyRuns != 1 {
		t.Fatalf("final calls = guard %d body %d, want 1/1", guardCalls, bodyRuns)
	}
}
