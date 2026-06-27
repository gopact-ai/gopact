package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/a2a"
	"github.com/gopact-ai/gopact/gopacttest"
	"github.com/gopact-ai/gopact/tools"
)

func TestSpecFromCardAndCardFromSpec(t *testing.T) {
	card := a2a.AgentCard{
		Name:         "planner",
		Description:  "plans tasks",
		Capabilities: []string{"planning", "review"},
		Metadata:     map[string]any{"owner": "agents"},
	}

	spec, err := SpecFromCard(card)
	if err != nil {
		t.Fatalf("SpecFromCard() error = %v", err)
	}
	if spec.Name != "planner" || spec.Description != "plans tasks" {
		t.Fatalf("spec = %+v, want planner tool spec", spec)
	}
	if spec.InputSchema["type"] != "object" {
		t.Fatalf("spec schema = %+v, want object schema", spec.InputSchema)
	}

	got := CardFromSpec(spec)
	if got.Name != "planner" || got.Description != "plans tasks" {
		t.Fatalf("CardFromSpec() = %+v, want planner card", got)
	}
}

func TestLocalAgentToolRunsChildAgentAndReturnsEventsWithCallChain(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "result.txt", URI: "memory://artifact-1"}
	var childInput any
	var childIDs gopact.RuntimeIDs
	child := runnableFunc(func(_ context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		cfg := gopact.ResolveRunOptions(opts...)
		childInput = input
		childIDs = cfg.IDs
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted, IDs: cfg.IDs}, nil)
			yield(gopact.Event{
				Type:    gopact.EventModelMessage,
				IDs:     cfg.IDs,
				Message: &gopact.Message{Role: gopact.RoleAssistant, Content: "child answer"},
			}, nil)
			yield(gopact.Event{
				Type:      gopact.EventToolResult,
				IDs:       cfg.IDs,
				Artifacts: []gopact.ArtifactRef{artifact},
				Result:    &gopact.ToolResult{Content: "artifact ready", Artifacts: []gopact.ArtifactRef{artifact}},
			}, nil)
			yield(gopact.Event{Type: gopact.EventRunCompleted, IDs: cfg.IDs}, nil)
		}
	})
	tool, err := New("planner", child, WithDescription("plans tasks"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(ctx, tool, tools.RegisterOptions{Namespace: "agents", Visibility: tools.VisibleTool, Source: tools.SourceA2A}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Invoke(ctx, "agents.planner", json.RawMessage(`{"input":"write tests"}`), tools.Scope{
		IDs: gopact.RuntimeIDs{
			RunID:    "parent-run",
			ThreadID: "thread-1",
			CallID:   "parent-call",
			UserID:   "user-1",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	message, ok := childInput.(gopact.Message)
	if !ok || message.Role != gopact.RoleUser || message.Content != "write tests" {
		t.Fatalf("child input = %#v, want user message from tool args", childInput)
	}
	if childIDs.RunID != "parent-run" || childIDs.ThreadID != "thread-1" || childIDs.UserID != "user-1" {
		t.Fatalf("child runtime ids = %+v, want parent run/thread/user ids", childIDs)
	}
	if childIDs.ParentCallID != "parent-call" || childIDs.CallID == "" || childIDs.CallID == "parent-call" {
		t.Fatalf("child call ids = %+v, want child call under parent-call", childIDs)
	}
	if result.Content != "child answer" {
		t.Fatalf("result content = %q, want child answer", result.Content)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].ID != artifact.ID {
		t.Fatalf("result artifacts = %+v, want child artifact", result.Artifacts)
	}
	if len(result.Events) != 4 {
		t.Fatalf("result events = %d, want 4 child events", len(result.Events))
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/local_child_run.golden.json", result.Events)
	gopacttest.RequireTemplateTrajectoryConformance(t, gopacttest.TemplateTrajectoryConformanceHarness{
		Name:   "agenttool local child run",
		Events: result.Events,
		RequiredEventTypes: []gopact.EventType{
			gopact.EventRunStarted,
			gopact.EventModelMessage,
			gopact.EventToolResult,
			gopact.EventRunCompleted,
		},
	})
	for _, event := range result.Events {
		ids := event.RuntimeIDs()
		if ids.ParentCallID != "parent-call" || ids.CallID != childIDs.CallID {
			t.Fatalf("child event ids = %+v, want parent/child call chain", ids)
		}
	}
	if result.Metadata["agent_name"] != "planner" || result.Metadata["child_event_count"] != 4 {
		t.Fatalf("result metadata = %+v, want agent metadata", result.Metadata)
	}
}

func TestLocalAgentToolReturnsChildEventsOnFailure(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("child failed")
	child := runnableFunc(func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
		cfg := gopact.ResolveRunOptions(opts...)
		return func(yield func(gopact.Event, error) bool) {
			yield(gopact.Event{Type: gopact.EventRunStarted, IDs: cfg.IDs}, nil)
			yield(gopact.Event{Type: gopact.EventRunFailed, IDs: cfg.IDs, Err: wantErr}, wantErr)
		}
	})
	tool, err := New("planner", child)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := tool.Invoke(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), json.RawMessage(`{"input":"write tests"}`))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want child error", err)
	}
	if len(result.Events) != 2 || result.Events[1].Type != gopact.EventRunFailed {
		t.Fatalf("result events = %+v, want child failure events", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/local_child_failure.golden.json", result.Events)
}

func TestRemoteA2AToolSendsTaskWithRuntimeIDsAndReturnsArtifacts(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	var sentTask a2a.Task
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			sentTask = task
			return a2a.Result{
				TaskID:    task.ID,
				Output:    "planned",
				Artifacts: []gopact.ArtifactRef{artifact},
				Metadata:  map[string]any{"route": "remote"},
			}, nil
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(ctx, tool, tools.RegisterOptions{Namespace: "agents", Visibility: tools.VisibleTool, Source: tools.SourceA2A}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Invoke(ctx, "agents.planner", json.RawMessage(`{"input":"write tests","metadata":{"priority":"high"}}`), tools.Scope{
		IDs: gopact.RuntimeIDs{
			RunID:    "parent-run",
			ThreadID: "thread-1",
			CallID:   "parent-call",
			UserID:   "user-1",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	if sentTask.ID == "" || sentTask.ID != sentTask.IDs.CallID {
		t.Fatalf("sent task id = %q ids = %+v, want task id mapped to child call id", sentTask.ID, sentTask.IDs)
	}
	if sentTask.IDs.RunID != "parent-run" || sentTask.IDs.ThreadID != "thread-1" || sentTask.IDs.UserID != "user-1" {
		t.Fatalf("sent task ids = %+v, want parent run/thread/user ids", sentTask.IDs)
	}
	if sentTask.IDs.ParentCallID != "parent-call" || sentTask.IDs.CallID == "parent-call" {
		t.Fatalf("sent task call ids = %+v, want child call under parent-call", sentTask.IDs)
	}
	if sentTask.Input != "write tests" || sentTask.Metadata["priority"] != "high" {
		t.Fatalf("sent task = %+v, want input and metadata from tool args", sentTask)
	}
	if result.Content != "planned" {
		t.Fatalf("result content = %q, want planned", result.Content)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].ID != artifact.ID {
		t.Fatalf("result artifacts = %+v, want A2A artifact", result.Artifacts)
	}
	if result.Metadata["agent_name"] != "planner" ||
		result.Metadata["a2a_task_id"] != sentTask.ID ||
		result.Metadata["child_call_id"] != sentTask.IDs.CallID ||
		result.Metadata["parent_call_id"] != "parent-call" ||
		result.Metadata["route"] != "remote" {
		t.Fatalf("result metadata = %+v, want A2A metadata", result.Metadata)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventA2ATaskSent ||
		result.Events[1].Type != gopact.EventA2ATaskCompleted {
		t.Fatalf("result events = %+v, want sent/completed A2A events", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_send_success.golden.json", result.Events)
	for _, event := range result.Events {
		ids := event.RuntimeIDs()
		if ids.ParentCallID != "parent-call" || ids.CallID != sentTask.IDs.CallID {
			t.Fatalf("A2A event ids = %+v, want parent/child call chain", ids)
		}
	}
	if len(result.Events[1].Artifacts) != 1 || result.Events[1].Artifacts[0].ID != artifact.ID {
		t.Fatalf("completed event artifacts = %+v, want A2A artifact", result.Events[1].Artifacts)
	}
}

func TestRemoteA2AToolUsesDiscoveredCardForSpec(t *testing.T) {
	ctx := context.Background()
	registry := a2a.NewRegistry()
	result, err := registry.Discover(ctx, a2a.DiscovererFunc(func(ctx context.Context, query a2a.DiscoveryQuery) (a2a.DiscoveryResult, error) {
		return a2a.DiscoveryResult{
			Card: a2a.AgentCard{
				Name:         "planner",
				Description:  "plans tasks from catalog",
				URL:          query.URL,
				Capabilities: []string{"planning"},
			},
		}, nil
	}), a2a.DiscoveryQuery{Name: "planner", URL: "https://agents.example/planner"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "fallback description"},
	}
	tool, err := NewA2A(remote, WithCard(result.Card))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	spec, err := tool.Spec(ctx)
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}
	if spec.Name != "planner" || spec.Description != "plans tasks from catalog" {
		t.Fatalf("Spec() = %+v, want discovered card details", spec)
	}
}

func TestRemoteA2AToolReturnsFailureEventOnSendError(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("remote failed")
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, _ a2a.Task) (a2a.Result, error) {
			return a2a.Result{}, wantErr
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Invoke(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), json.RawMessage(`{"input":"write tests"}`))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want remote error", err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventA2ATaskSent ||
		result.Events[1].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("result events = %+v, want sent/failed A2A events", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_send_failure.golden.json", result.Events)
	if result.Events[1].Error() == "" {
		t.Fatalf("failure event error is empty, want remote error")
	}
	if result.Metadata["agent_name"] != "planner" || result.Metadata["parent_call_id"] != "parent-call" {
		t.Fatalf("result metadata = %+v, want failure metadata", result.Metadata)
	}
}

func TestRemoteA2AToolPolicyDenySkipsSendAndReturnsPolicyEvents(t *testing.T) {
	ctx := context.Background()
	sendCalled := false
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, _ a2a.Task) (a2a.Result, error) {
			sendCalled = true
			return a2a.Result{}, nil
		},
	}
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Boundary != gopact.PolicyBoundaryA2A {
			t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryA2A)
		}
		if req.Action != gopact.PolicyActionSend {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionSend)
		}
		input, ok := req.Input.(A2APolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want A2APolicyInput", req.Input)
		}
		if input.AgentName != "planner" || input.Task.Input != "write tests" {
			t.Fatalf("policy input = %+v, want planner task", input)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "remote agent blocked"}, nil
	})
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Invoke(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), json.RawMessage(`{"input":"write tests"}`))
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Invoke() error = %v, want policy denial", err)
	}
	if sendCalled {
		t.Fatal("remote Send should not run after policy denial")
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("result events = %+v, want policy requested/decided", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_policy_deny.golden.json", result.Events)
	if result.Events[1].PolicyDecision == nil || result.Events[1].PolicyDecision.Action != gopact.PolicyDeny {
		t.Fatalf("policy decision event = %+v, want deny", result.Events[1])
	}
	if result.Metadata["agent_name"] != "planner" || result.Metadata["parent_call_id"] != "parent-call" {
		t.Fatalf("result metadata = %+v, want agent policy metadata", result.Metadata)
	}
}

func TestRemoteA2AToolPolicyReviewReturnsApprovalInterrupt(t *testing.T) {
	ctx := context.Background()
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			t.Fatal("remote Send should not run before approval")
			return a2a.Result{}, nil
		},
	}
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{
			Action: gopact.PolicyReview,
			Reason: "remote agent needs approval",
			Metadata: map[string]any{
				"risk": "medium",
			},
		}, nil
	})
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Invoke(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), json.RawMessage(`{"input":"write tests"}`))
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Invoke() error = %v, want interrupt", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Invoke() error = %T, want *InterruptError", err)
	}
	if interruptErr.Record.Type != gopact.InterruptApproval || interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryA2A) {
		t.Fatalf("interrupt record = %+v, want A2A approval", interruptErr.Record)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("result events = %+v, want policy requested/decided", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_policy_review.golden.json", result.Events)
}

func TestRemoteA2AToolPolicyReviewInterruptIDIncludesA2AAction(t *testing.T) {
	ctx := gopact.ContextWithRuntimeIDs(context.Background(), gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	})
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "approval required"}, nil
	})
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			t.Fatal("remote Send should not run before approval")
			return a2a.Result{}, nil
		},
		CancelFunc: func(ctx context.Context, taskID string) error {
			t.Fatal("remote Cancel should not run before approval")
			return nil
		},
	}
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	_, sendErr := tool.Invoke(ctx, json.RawMessage(`{"input":"write tests"}`))
	var sendInterrupt *gopact.InterruptError
	if !errors.As(sendErr, &sendInterrupt) {
		t.Fatalf("Invoke() error = %T, want *InterruptError", sendErr)
	}

	_, cancelErr := tool.Cancel(ctx, "task-1")
	var cancelInterrupt *gopact.InterruptError
	if !errors.As(cancelErr, &cancelInterrupt) {
		t.Fatalf("Cancel() error = %T, want *InterruptError", cancelErr)
	}

	if sendInterrupt.Record.ID == cancelInterrupt.Record.ID {
		t.Fatalf("interrupt IDs both = %q, want action-scoped send/cancel IDs", sendInterrupt.Record.ID)
	}
	if sendInterrupt.Record.ID != "policy:parent-call:agent:planner:a2a:send" {
		t.Fatalf("send interrupt ID = %q, want action-scoped send ID", sendInterrupt.Record.ID)
	}
	if cancelInterrupt.Record.ID != "policy:parent-call:agent:planner:a2a:cancel" {
		t.Fatalf("cancel interrupt ID = %q, want action-scoped cancel ID", cancelInterrupt.Record.ID)
	}
}

func TestRemoteA2AToolAuthAttachesAuthBeforePolicyAndSend(t *testing.T) {
	ctx := context.Background()
	var sentTask a2a.Task
	var sentAuth a2a.Auth
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			var ok bool
			sentAuth, ok = a2a.AuthFromContext(ctx)
			if !ok {
				t.Fatal("Send context missing A2A auth")
			}
			sentTask = task
			return a2a.Result{TaskID: task.ID, Output: "planned"}, nil
		},
	}
	auth := a2a.AuthenticatorFunc(func(ctx context.Context, req a2a.AuthRequest) (a2a.Auth, error) {
		if req.AgentName != "planner" || req.Action != gopact.PolicyActionSend {
			t.Fatalf("auth request = %+v, want planner send", req)
		}
		if req.Task == nil || req.Task.Input != "write tests" {
			t.Fatalf("auth task = %+v, want task input", req.Task)
		}
		return a2a.Auth{
			Scheme:        "bearer",
			Principal:     "svc-planner",
			CredentialRef: "secret://a2a/planner",
			Metadata:      map[string]any{"tenant": "tenant-1"},
		}, nil
	})
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		input, ok := req.Input.(A2APolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want A2APolicyInput", req.Input)
		}
		if input.Task.Auth == nil ||
			input.Task.Auth.Scheme != "bearer" ||
			input.Task.Auth.Principal != "svc-planner" ||
			input.Task.Auth.CredentialRef != "secret://a2a/planner" {
			t.Fatalf("policy task auth = %+v, want injected auth", input.Task.Auth)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})
	tool, err := NewA2A(remote, WithAuth(auth), WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Invoke(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), json.RawMessage(`{"input":"write tests"}`))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if sentTask.Auth == nil || sentTask.Auth.Principal != "svc-planner" || sentAuth.Principal != "svc-planner" {
		t.Fatalf("sent task auth = %+v context auth = %+v, want injected auth", sentTask.Auth, sentAuth)
	}
	if sentTask.Metadata["auth_scheme"] != "bearer" ||
		sentTask.Metadata["auth_principal"] != "svc-planner" ||
		sentTask.Metadata["auth_credential_ref"] != "secret://a2a/planner" {
		t.Fatalf("sent task metadata = %+v, want auth audit metadata", sentTask.Metadata)
	}
	if len(result.Events) != 4 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided ||
		result.Events[2].Type != gopact.EventA2ATaskSent ||
		result.Events[3].Type != gopact.EventA2ATaskCompleted {
		t.Fatalf("result events = %+v, want policy/sent/completed", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_auth_send.golden.json", result.Events)
	if result.Events[0].PolicyRequest.Metadata["auth_principal"] != "svc-planner" ||
		result.Events[1].PolicyRequest.Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("policy event metadata = requested:%+v decided:%+v, want auth principal",
			result.Events[0].PolicyRequest.Metadata,
			result.Events[1].PolicyRequest.Metadata)
	}
	if result.Events[2].Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("sent event metadata = %+v, want auth principal", result.Events[2].Metadata)
	}
	if result.Events[3].Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("completed event metadata = %+v, want auth principal", result.Events[3].Metadata)
	}
	if result.Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("result metadata = %+v, want auth principal", result.Metadata)
	}
}

func TestRemoteA2AToolAuthSendFailureAttachesAuditMetadata(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("remote send failed")
	var sentAuth a2a.Auth
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			if task.Input != "write tests" {
				t.Fatalf("sent task input = %q, want write tests", task.Input)
			}
			var ok bool
			sentAuth, ok = a2a.AuthFromContext(ctx)
			if !ok {
				t.Fatal("Send context missing A2A auth")
			}
			return a2a.Result{}, wantErr
		},
	}
	auth := a2a.AuthenticatorFunc(func(ctx context.Context, req a2a.AuthRequest) (a2a.Auth, error) {
		if req.AgentName != "planner" || req.Action != gopact.PolicyActionSend {
			t.Fatalf("auth request = %+v, want planner send", req)
		}
		return a2a.Auth{
			Scheme:        "bearer",
			Principal:     "svc-planner",
			CredentialRef: "secret://a2a/planner",
		}, nil
	})
	tool, err := NewA2A(remote, WithAuth(auth))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Invoke(
		gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}),
		json.RawMessage(`{"input":"write tests"}`),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want remote send error", err)
	}
	if sentAuth.Principal != "svc-planner" {
		t.Fatalf("send context auth = %+v, want injected auth", sentAuth)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventA2ATaskSent ||
		result.Events[1].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("result events = %+v, want sent/failed events", result.Events)
	}
	if result.Events[1].Metadata["auth_scheme"] != "bearer" ||
		result.Events[1].Metadata["auth_principal"] != "svc-planner" ||
		result.Events[1].Metadata["auth_credential_ref"] != "secret://a2a/planner" {
		t.Fatalf("failed event metadata = %+v, want auth audit metadata", result.Events[1].Metadata)
	}
	if result.Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("result metadata = %+v, want auth principal", result.Metadata)
	}
}

func TestRemoteA2AToolCancelAuthAttachesAuditMetadata(t *testing.T) {
	ctx := context.Background()
	var cancelAuth a2a.Auth
	var policyAuth a2a.Auth
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			if taskID != "task-1" {
				t.Fatalf("cancel task ID = %q, want task-1", taskID)
			}
			var ok bool
			cancelAuth, ok = a2a.AuthFromContext(ctx)
			if !ok {
				t.Fatal("Cancel context missing A2A auth")
			}
			return nil
		},
	}
	auth := a2a.AuthenticatorFunc(func(ctx context.Context, req a2a.AuthRequest) (a2a.Auth, error) {
		if req.AgentName != "planner" || req.Action != gopact.PolicyActionCancel || req.TaskID != "task-1" {
			t.Fatalf("auth request = %+v, want planner cancel for task-1", req)
		}
		if req.Task != nil {
			t.Fatalf("auth task = %+v, want nil task for cancel", req.Task)
		}
		return a2a.Auth{
			Scheme:        "bearer",
			Principal:     "svc-planner",
			CredentialRef: "secret://a2a/planner",
			Metadata:      map[string]any{"tenant": "tenant-1"},
		}, nil
	})
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		input, ok := req.Input.(A2ACancelPolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want A2ACancelPolicyInput", req.Input)
		}
		if input.TaskID != "task-1" {
			t.Fatalf("policy task ID = %q, want task-1", input.TaskID)
		}
		var okAuth bool
		policyAuth, okAuth = a2a.AuthFromContext(ctx)
		if !okAuth {
			t.Fatal("Policy context missing A2A auth")
		}
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})
	tool, err := NewA2A(remote, WithAuth(auth), WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Cancel(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), "task-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelAuth.Principal != "svc-planner" {
		t.Fatalf("cancel context auth = %+v, want injected auth", cancelAuth)
	}
	if policyAuth.Principal != "svc-planner" {
		t.Fatalf("policy context auth = %+v, want injected auth", policyAuth)
	}
	if len(result.Events) != 3 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided ||
		result.Events[2].Type != gopact.EventA2ATaskCanceled {
		t.Fatalf("result events = %+v, want policy/canceled events", result.Events)
	}
	if result.Events[2].Metadata["auth_scheme"] != "bearer" ||
		result.Events[2].Metadata["auth_principal"] != "svc-planner" ||
		result.Events[2].Metadata["auth_credential_ref"] != "secret://a2a/planner" {
		t.Fatalf("canceled event metadata = %+v, want auth audit metadata", result.Events[2].Metadata)
	}
	if result.Events[0].PolicyRequest.Metadata["auth_principal"] != "svc-planner" ||
		result.Events[1].PolicyRequest.Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("cancel policy event metadata = requested:%+v decided:%+v, want auth principal",
			result.Events[0].PolicyRequest.Metadata,
			result.Events[1].PolicyRequest.Metadata)
	}
	if result.Metadata["auth_principal"] != "svc-planner" {
		t.Fatalf("result metadata = %+v, want auth principal", result.Metadata)
	}
}

func TestRemoteA2AToolTimeoutCancelsSendAndReturnsFailureEvent(t *testing.T) {
	ctx := context.Background()
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		SendFunc: func(ctx context.Context, task a2a.Task) (a2a.Result, error) {
			_ = task
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("Send context has no deadline")
			}
			<-ctx.Done()
			return a2a.Result{}, ctx.Err()
		},
	}
	tool, err := NewA2A(remote, WithTimeout(time.Millisecond))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Invoke(ctx, json.RawMessage(`{"input":"write tests"}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Invoke() error = %v, want deadline exceeded", err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventA2ATaskSent ||
		result.Events[1].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("result events = %+v, want sent/failed", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_timeout.golden.json", result.Events)
	if result.Events[1].Error() == "" {
		t.Fatal("failure event error is empty, want timeout error")
	}
}

func TestRemoteA2AToolStreamsStatusAndCompletedEvents(t *testing.T) {
	ctx := context.Background()
	artifact := gopact.ArtifactRef{ID: "artifact-1", Name: "plan.md", URI: "memory://artifact-1"}
	var streamedTask a2a.Task
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			streamedTask = task
			return func(yield func(a2a.TaskEvent, error) bool) {
				if !yield(a2a.TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Message:  "outline ready",
					Metadata: map[string]any{"phase": "outline"},
				}, nil) {
					return
				}
				if !yield(a2a.TaskEvent{
					TaskID:    task.ID,
					IDs:       task.IDs,
					Artifacts: []gopact.ArtifactRef{artifact},
					Metadata:  map[string]any{"phase": "draft"},
				}, nil) {
					return
				}
				if !yield(a2a.TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Status:   a2a.TaskStatusRunning,
					Message:  "drafting",
					Metadata: map[string]any{"progress": 0.5},
				}, nil) {
					return
				}
				yield(a2a.TaskEvent{
					TaskID: task.ID,
					IDs:    task.IDs,
					Status: a2a.TaskStatusCompleted,
					Result: &a2a.Result{
						TaskID:    task.ID,
						Output:    "planned",
						Artifacts: []gopact.ArtifactRef{artifact},
						Metadata:  map[string]any{"route": "stream"},
					},
				}, nil)
			}
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	events, err := collectEvents(tool.Stream(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	}), json.RawMessage(`{"input":"write tests"}`)))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if streamedTask.ID == "" || streamedTask.ID != streamedTask.IDs.CallID {
		t.Fatalf("streamed task = %+v, want task id mapped to child call id", streamedTask)
	}
	if len(events) != 5 ||
		events[0].Type != gopact.EventA2ATaskSent ||
		events[1].Type != gopact.EventA2AMessageReceived ||
		events[2].Type != gopact.EventA2AArtifactUpdated ||
		events[3].Type != gopact.EventA2ATaskStatusUpdated ||
		events[4].Type != gopact.EventA2ATaskCompleted {
		t.Fatalf("Stream() events = %+v, want sent/message/artifact/status/completed", events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_stream.golden.json", events)
	if events[1].Metadata["a2a_message"] != "outline ready" ||
		events[1].Metadata["phase"] != "outline" {
		t.Fatalf("message event metadata = %+v, want streamed message metadata", events[1].Metadata)
	}
	if len(events[2].Artifacts) != 1 || events[2].Artifacts[0].ID != artifact.ID ||
		events[2].Metadata["phase"] != "draft" {
		t.Fatalf("artifact event = %+v metadata=%+v, want streamed artifact", events[2].Artifacts, events[2].Metadata)
	}
	if events[3].Metadata["a2a_status"] != string(a2a.TaskStatusRunning) ||
		events[3].Metadata["a2a_message"] != "drafting" ||
		events[3].Metadata["progress"] != 0.5 {
		t.Fatalf("status event metadata = %+v, want status message and progress", events[3].Metadata)
	}
	if len(events[4].Artifacts) != 1 || events[4].Artifacts[0].ID != artifact.ID {
		t.Fatalf("completed artifacts = %+v, want streamed artifact", events[4].Artifacts)
	}
	for _, event := range events {
		ids := event.RuntimeIDs()
		if ids.ParentCallID != "parent-call" || ids.CallID != streamedTask.IDs.CallID {
			t.Fatalf("stream event ids = %+v, want parent/child call chain", ids)
		}
	}
}

func TestRemoteA2AToolStreamNotSupportedReturnsFailedEvent(t *testing.T) {
	ctx := context.Background()
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	events, err := collectEvents(tool.Stream(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	}), json.RawMessage(`{"input":"write tests"}`)))
	if !errors.Is(err, a2a.ErrStreamNotSupported) {
		t.Fatalf("Stream() error = %v, want stream unsupported", err)
	}
	if len(events) != 2 ||
		events[0].Type != gopact.EventA2ATaskSent ||
		events[1].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("Stream() events = %+v, want sent/failed", events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_stream_not_supported.golden.json", events)
	if events[1].Error() == "" {
		t.Fatal("failure event error is empty, want stream unsupported error")
	}
	childCallID := events[0].RuntimeIDs().CallID
	for _, event := range events {
		ids := event.RuntimeIDs()
		if ids.ParentCallID != "parent-call" || ids.CallID != childCallID {
			t.Fatalf("stream failure event ids = %+v, want parent/child call chain", ids)
		}
	}
}

func TestRemoteA2AToolStreamFailureReturnsFailedEvent(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("stream failed")
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			return func(yield func(a2a.TaskEvent, error) bool) {
				if !yield(a2a.TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Message:  "outline ready",
					Metadata: map[string]any{"phase": "outline"},
				}, nil) {
					return
				}
				yield(a2a.TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Status:   a2a.TaskStatusFailed,
					Message:  "remote stream failed",
					Metadata: map[string]any{"phase": "draft"},
					Err:      wantErr,
				}, wantErr)
			}
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	events, err := collectEvents(tool.Stream(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	}), json.RawMessage(`{"input":"write tests"}`)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Stream() error = %v, want stream failure", err)
	}
	if len(events) != 3 ||
		events[0].Type != gopact.EventA2ATaskSent ||
		events[1].Type != gopact.EventA2AMessageReceived ||
		events[2].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("Stream() events = %+v, want sent/message/failed", events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_stream_failure.golden.json", events)
	if events[2].Error() == "" {
		t.Fatal("failure event error is empty, want stream error")
	}
	if events[2].Metadata["a2a_status"] != string(a2a.TaskStatusFailed) ||
		events[2].Metadata["a2a_message"] != "remote stream failed" ||
		events[2].Metadata["phase"] != "draft" {
		t.Fatalf("failure event metadata = %+v, want failed status and streamed metadata", events[2].Metadata)
	}
	childCallID := events[0].RuntimeIDs().CallID
	for _, event := range events {
		ids := event.RuntimeIDs()
		if ids.ParentCallID != "parent-call" || ids.CallID != childCallID {
			t.Fatalf("stream failure event ids = %+v, want parent/child call chain", ids)
		}
	}
}

func TestRemoteA2AToolStreamCanceledReturnsCanceledEvent(t *testing.T) {
	ctx := context.Background()
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			return func(yield func(a2a.TaskEvent, error) bool) {
				if !yield(a2a.TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Status:   a2a.TaskStatusRunning,
					Message:  "waiting for approval",
					Metadata: map[string]any{"phase": "approval"},
				}, nil) {
					return
				}
				yield(a2a.TaskEvent{
					TaskID:   task.ID,
					IDs:      task.IDs,
					Status:   a2a.TaskStatusCanceled,
					Message:  "remote task canceled",
					Metadata: map[string]any{"reason": "user_cancel"},
				}, nil)
			}
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	events, err := collectEvents(tool.Stream(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	}), json.RawMessage(`{"input":"write tests"}`)))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(events) != 3 ||
		events[0].Type != gopact.EventA2ATaskSent ||
		events[1].Type != gopact.EventA2ATaskStatusUpdated ||
		events[2].Type != gopact.EventA2ATaskCanceled {
		t.Fatalf("Stream() events = %+v, want sent/status/canceled", events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_stream_canceled.golden.json", events)
	if events[2].Error() != "" {
		t.Fatalf("canceled event error = %q, want empty", events[2].Error())
	}
	if events[2].Metadata["a2a_status"] != string(a2a.TaskStatusCanceled) ||
		events[2].Metadata["a2a_message"] != "remote task canceled" ||
		events[2].Metadata["reason"] != "user_cancel" {
		t.Fatalf("canceled event metadata = %+v, want canceled status and streamed metadata", events[2].Metadata)
	}
	childCallID := events[0].RuntimeIDs().CallID
	for _, event := range events {
		ids := event.RuntimeIDs()
		if ids.ParentCallID != "parent-call" || ids.CallID != childCallID {
			t.Fatalf("stream canceled event ids = %+v, want parent/child call chain", ids)
		}
	}
}

func TestRemoteA2AToolStreamPolicyDenySkipsStreamAndReturnsPolicyEvents(t *testing.T) {
	ctx := context.Background()
	streamCalled := false
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, _ a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			streamCalled = true
			return func(_ func(a2a.TaskEvent, error) bool) {}
		},
	}
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionSend {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionSend)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "remote stream blocked"}, nil
	})
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	events, err := collectEvents(tool.Stream(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	}), json.RawMessage(`{"input":"write tests"}`)))
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Stream() error = %v, want policy denial", err)
	}
	if streamCalled {
		t.Fatal("remote Stream should not run after policy denial")
	}
	if len(events) != 2 ||
		events[0].Type != gopact.EventPolicyRequested ||
		events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("Stream() events = %+v, want policy requested/decided", events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_stream_policy_deny.golden.json", events)
}

func TestRemoteA2AToolStreamPolicyReviewReturnsApprovalInterrupt(t *testing.T) {
	ctx := context.Background()
	streamCalled := false
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		StreamFunc: func(ctx context.Context, _ a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
			streamCalled = true
			return func(_ func(a2a.TaskEvent, error) bool) {}
		},
	}
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionSend {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionSend)
		}
		input, ok := req.Input.(A2APolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want A2APolicyInput", req.Input)
		}
		if input.AgentName != "planner" || input.Task.Input != "write tests" {
			t.Fatalf("policy input = %+v, want planner stream task", input)
		}
		return gopact.PolicyDecision{
			Action: gopact.PolicyReview,
			Reason: "remote stream needs approval",
			Metadata: map[string]any{
				"risk": "medium",
			},
		}, nil
	})
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	events, err := collectEvents(tool.Stream(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{
		RunID:  "run-1",
		CallID: "parent-call",
	}), json.RawMessage(`{"input":"write tests"}`)))
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Stream() error = %v, want interrupt", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Stream() error = %T, want *InterruptError", err)
	}
	if interruptErr.Record.Type != gopact.InterruptApproval || interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryA2A) {
		t.Fatalf("interrupt record = %+v, want A2A approval", interruptErr.Record)
	}
	if interruptErr.Record.Metadata["policy_request_action"] != gopact.PolicyActionSend {
		t.Fatalf("interrupt metadata = %+v, want send action", interruptErr.Record.Metadata)
	}
	if streamCalled {
		t.Fatal("remote Stream should not run before approval")
	}
	if len(events) != 2 ||
		events[0].Type != gopact.EventPolicyRequested ||
		events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("Stream() events = %+v, want policy requested/decided", events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_stream_policy_review.golden.json", events)
}

func TestRemoteA2AToolCancelTaskReturnsCanceledEvent(t *testing.T) {
	ctx := context.Background()
	var canceledTaskID string
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			canceledTaskID = taskID
			return nil
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Cancel(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), "task-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceledTaskID != "task-1" {
		t.Fatalf("canceled task ID = %q, want task-1", canceledTaskID)
	}
	if len(result.Events) != 1 || result.Events[0].Type != gopact.EventA2ATaskCanceled {
		t.Fatalf("result events = %+v, want canceled event", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_cancel.golden.json", result.Events)
	if result.Metadata["agent_name"] != "planner" ||
		result.Metadata["a2a_task_id"] != "task-1" ||
		result.Metadata["parent_call_id"] != "parent-call" {
		t.Fatalf("result metadata = %+v, want cancel metadata", result.Metadata)
	}
}

func TestRemoteA2AToolCancelFailureReturnsFailedEvent(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("remote cancel failed")
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			if taskID != "task-1" {
				t.Fatalf("cancel task ID = %q, want task-1", taskID)
			}
			return wantErr
		},
	}
	tool, err := NewA2A(remote)
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Cancel(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), "task-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Cancel() error = %v, want remote cancel error", err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("result events = %+v, want failed event", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_cancel_failure.golden.json", result.Events)
	if result.Events[0].Error() == "" {
		t.Fatal("failure event error is empty, want remote cancel error")
	}
	ids := result.Events[0].RuntimeIDs()
	if ids.ParentCallID != "parent-call" {
		t.Fatalf("cancel failure ids = %+v, want parent call chain", ids)
	}
	if result.Metadata["agent_name"] != "planner" ||
		result.Metadata["a2a_task_id"] != "task-1" ||
		result.Metadata["parent_call_id"] != "parent-call" {
		t.Fatalf("result metadata = %+v, want cancel failure metadata", result.Metadata)
	}
}

func TestRemoteA2AToolCancelPolicyDenySkipsCancelAndReturnsPolicyEvents(t *testing.T) {
	ctx := context.Background()
	cancelCalled := false
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			cancelCalled = true
			return nil
		},
	}
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Boundary != gopact.PolicyBoundaryA2A {
			t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryA2A)
		}
		if req.Action != gopact.PolicyActionCancel {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionCancel)
		}
		input, ok := req.Input.(A2ACancelPolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want A2ACancelPolicyInput", req.Input)
		}
		if input.AgentName != "planner" || input.TaskID != "task-1" {
			t.Fatalf("policy input = %+v, want planner task cancel", input)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "cancel blocked"}, nil
	})
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Cancel(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), "task-1")
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Cancel() error = %v, want policy denial", err)
	}
	if cancelCalled {
		t.Fatal("remote Cancel should not run after policy denial")
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("result events = %+v, want policy requested/decided", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_cancel_policy_deny.golden.json", result.Events)
}

func TestRemoteA2AToolCancelPolicyReviewSkipsCancelAndReturnsApprovalInterrupt(t *testing.T) {
	ctx := context.Background()
	cancelCalled := false
	remote := a2a.FakeAgent{
		CardValue: a2a.AgentCard{Name: "planner", Description: "plans tasks"},
		CancelFunc: func(ctx context.Context, taskID string) error {
			cancelCalled = true
			return nil
		},
	}
	policy := gopact.PolicyFunc(func(ctx context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Boundary != gopact.PolicyBoundaryA2A {
			t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryA2A)
		}
		if req.Action != gopact.PolicyActionCancel {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionCancel)
		}
		input, ok := req.Input.(A2ACancelPolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want A2ACancelPolicyInput", req.Input)
		}
		if input.AgentName != "planner" || input.TaskID != "task-1" {
			t.Fatalf("policy input = %+v, want planner task cancel", input)
		}
		return gopact.PolicyDecision{
			Action: gopact.PolicyReview,
			Reason: "cancel needs approval",
			Metadata: map[string]any{
				"risk": "medium",
			},
		}, nil
	})
	tool, err := NewA2A(remote, WithPolicy(policy))
	if err != nil {
		t.Fatalf("NewA2A() error = %v", err)
	}

	result, err := tool.Cancel(gopact.ContextWithRuntimeIDs(ctx, gopact.RuntimeIDs{RunID: "run-1", CallID: "parent-call"}), "task-1")
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Cancel() error = %v, want interrupt", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("Cancel() error = %T, want *InterruptError", err)
	}
	if interruptErr.Record.Type != gopact.InterruptApproval || interruptErr.Record.RequiredBy != string(gopact.PolicyBoundaryA2A) {
		t.Fatalf("interrupt record = %+v, want A2A approval", interruptErr.Record)
	}
	if interruptErr.Record.Metadata["policy_request_action"] != gopact.PolicyActionCancel {
		t.Fatalf("interrupt metadata = %+v, want cancel action", interruptErr.Record.Metadata)
	}
	if cancelCalled {
		t.Fatal("remote Cancel should not run before approval")
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != gopact.EventPolicyRequested ||
		result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("result events = %+v, want policy requested/decided", result.Events)
	}
	gopacttest.RequireGoldenTrajectoryFrames(t, "testdata/a2a_cancel_policy_review.golden.json", result.Events)
}

func TestNewA2ARejectsInvalidAgent(t *testing.T) {
	if _, err := NewA2A(nil); !errors.Is(err, ErrAgentRequired) {
		t.Fatalf("NewA2A(nil) error = %v, want %v", err, ErrAgentRequired)
	}
	if _, err := NewA2A(a2a.FakeAgent{}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("NewA2A(missing name) error = %v, want %v", err, ErrNameRequired)
	}
}

type runnableFunc func(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error]

func (f runnableFunc) Run(ctx context.Context, input any, opts ...gopact.RunOption) iter.Seq2[gopact.Event, error] {
	return f(ctx, input, opts...)
}

func collectEvents(seq iter.Seq2[gopact.Event, error]) ([]gopact.Event, error) {
	var events []gopact.Event
	for event, err := range seq {
		events = append(events, event)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}
