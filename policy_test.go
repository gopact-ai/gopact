package gopact

import (
	"context"
	"errors"
	"testing"
)

func TestPolicyDecisionAllowed(t *testing.T) {
	if !(PolicyDecision{Action: PolicyAllow}).Allowed() {
		t.Fatal("allow decision should be allowed")
	}
}

func TestPolicyDecisionAllowedRejectsDenyAndReview(t *testing.T) {
	tests := []PolicyDecision{
		{Action: PolicyDeny},
		{Action: PolicyReview},
		{},
	}

	for _, decision := range tests {
		if decision.Allowed() {
			t.Fatalf("decision %+v should not be allowed", decision)
		}
	}
}

func TestPolicyFuncDecide(t *testing.T) {
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		if req.Boundary != PolicyBoundaryModel {
			t.Fatalf("boundary = %q, want %q", req.Boundary, PolicyBoundaryModel)
		}
		return PolicyDecision{Action: PolicyAllow}, nil
	})

	decision, err := policy.Decide(context.Background(), PolicyRequest{Boundary: PolicyBoundaryModel})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed() {
		t.Fatalf("Decide() decision = %+v, want allow", decision)
	}
}

func TestModelPolicyMiddlewareBlocksDeniedRequest(t *testing.T) {
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		if req.Boundary != PolicyBoundaryModel {
			t.Fatalf("boundary = %q, want %q", req.Boundary, PolicyBoundaryModel)
		}
		if req.Action != PolicyActionGenerate {
			t.Fatalf("action = %q, want %q", req.Action, PolicyActionGenerate)
		}
		if req.IDs.RunID != "run-1" {
			t.Fatalf("IDs = %+v, want run-1", req.IDs)
		}
		if _, ok := req.Input.(ModelRequest); !ok {
			t.Fatalf("input type = %T, want ModelRequest", req.Input)
		}
		return PolicyDecision{Action: PolicyDeny, Reason: "blocked"}, nil
	})
	finalCalled := false
	handler := ComposeModelHandler(func(_ *ModelContext) error {
		finalCalled = true
		return nil
	}, ModelPolicyMiddleware(policy))

	modelCtx := NewModelContext(context.Background(), ModelContextOptions{
		Request: ModelRequest{IDs: RuntimeIDs{RunID: "run-1"}},
	})
	err := handler(modelCtx)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("handler error = %v, want ErrPolicyDenied", err)
	}
	if finalCalled {
		t.Fatal("model final handler should not run after policy denial")
	}
}

func TestToolPolicyMiddlewareBlocksDeniedInvocation(t *testing.T) {
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		if req.Boundary != PolicyBoundaryTool {
			t.Fatalf("boundary = %q, want %q", req.Boundary, PolicyBoundaryTool)
		}
		if req.Action != PolicyActionInvoke {
			t.Fatalf("action = %q, want %q", req.Action, PolicyActionInvoke)
		}
		if req.IDs.CallID != "call-1" {
			t.Fatalf("IDs = %+v, want call-1", req.IDs)
		}
		if _, ok := req.Input.(ToolPolicyInput); !ok {
			t.Fatalf("input type = %T, want ToolPolicyInput", req.Input)
		}
		return PolicyDecision{Action: PolicyDeny, Reason: "blocked"}, nil
	})
	finalCalled := false
	handler := ComposeToolHandler(func(_ *ToolContext) error {
		finalCalled = true
		return nil
	}, ToolPolicyMiddleware(policy))

	toolCtx := NewToolContext(context.Background(), ToolContextOptions{
		Name: "shell.exec",
		IDs:  RuntimeIDs{CallID: "call-1"},
		Args: []byte(`{"cmd":"date"}`),
	})
	err := handler(toolCtx)
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("handler error = %v, want ErrPolicyDenied", err)
	}
	if finalCalled {
		t.Fatal("tool final handler should not run after policy denial")
	}
}

func TestToolPolicyMiddlewareReviewReturnsApprovalInterrupt(t *testing.T) {
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{
			Action: PolicyReview,
			Reason: "needs approval",
			Metadata: map[string]any{
				"risk": "high",
			},
		}, nil
	})
	finalCalled := false
	handler := ComposeToolHandler(func(_ *ToolContext) error {
		finalCalled = true
		return nil
	}, ToolPolicyMiddleware(policy))

	toolCtx := NewToolContext(context.Background(), ToolContextOptions{
		Name: "shell.exec",
		IDs:  RuntimeIDs{RunID: "run-1", CallID: "call-1"},
		Args: []byte(`{"cmd":"rm -rf"}`),
	})
	err := handler(toolCtx)
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("handler error = %v, want ErrInterrupted", err)
	}
	var interruptErr *InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("handler error = %T, want *InterruptError", err)
	}
	if interruptErr.Record.Type != InterruptApproval {
		t.Fatalf("interrupt type = %q, want approval", interruptErr.Record.Type)
	}
	if interruptErr.Record.ID != "policy:call-1" {
		t.Fatalf("interrupt id = %q, want policy:call-1", interruptErr.Record.ID)
	}
	if interruptErr.Record.Metadata["policy_action"] != PolicyReview {
		t.Fatalf("interrupt metadata = %+v, want policy review action", interruptErr.Record.Metadata)
	}
	if finalCalled {
		t.Fatal("tool final handler should not run after policy review")
	}
}

func TestToolPolicyMiddlewareRecordsPolicyEvents(t *testing.T) {
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{Action: PolicyAllow, Reason: "safe"}, nil
	})
	handler := ComposeToolHandler(func(_ *ToolContext) error {
		return nil
	}, ToolPolicyMiddleware(policy))

	toolCtx := NewToolContext(context.Background(), ToolContextOptions{
		Name: "shell.exec",
		IDs:  RuntimeIDs{RunID: "run-1", CallID: "call-1"},
	})
	if err := handler(toolCtx); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if got := len(toolCtx.Events); got != 2 {
		t.Fatalf("policy event count = %d, want 2", got)
	}
	if toolCtx.Events[0].Type != EventPolicyRequested || toolCtx.Events[1].Type != EventPolicyDecided {
		t.Fatalf("policy event types = %v, want requested/decided", eventTypes(toolCtx.Events))
	}
	if toolCtx.Events[1].PolicyDecision == nil || toolCtx.Events[1].PolicyDecision.Action != PolicyAllow {
		t.Fatalf("policy decided event = %+v, want allow decision", toolCtx.Events[1])
	}
}

func TestModelPolicyMiddlewareRecordsPolicyEvents(t *testing.T) {
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{Action: PolicyAllow, Reason: "safe"}, nil
	})
	handler := ComposeModelHandler(func(_ *ModelContext) error {
		return nil
	}, ModelPolicyMiddleware(policy))

	modelCtx := NewModelContext(context.Background(), ModelContextOptions{
		Request: ModelRequest{IDs: RuntimeIDs{RunID: "run-1", CallID: "model-call-1"}},
	})
	if err := handler(modelCtx); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if got := len(modelCtx.Events); got != 2 {
		t.Fatalf("policy event count = %d, want 2", got)
	}
	if modelCtx.Events[0].Type != EventPolicyRequested || modelCtx.Events[1].Type != EventPolicyDecided {
		t.Fatalf("policy event types = %v, want requested/decided", eventTypes(modelCtx.Events))
	}
	if modelCtx.Events[1].PolicyDecision == nil || modelCtx.Events[1].PolicyDecision.Action != PolicyAllow {
		t.Fatalf("policy decided event = %+v, want allow decision", modelCtx.Events[1])
	}
}

func TestPolicyMiddlewarePropagatesPolicyError(t *testing.T) {
	wantErr := errors.New("policy unavailable")
	policy := PolicyFunc(func(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{}, wantErr
	})
	handler := ComposeModelHandler(func(_ *ModelContext) error {
		t.Fatal("model final handler should not run after policy error")
		return nil
	}, ModelPolicyMiddleware(policy))

	err := handler(NewModelContext(context.Background(), ModelContextOptions{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v, want %v", err, wantErr)
	}
}
