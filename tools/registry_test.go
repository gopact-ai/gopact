package tools

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRegistryVisibleDeferredSearchAndPromote(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")
	mustRegisterTool(t, registry, "local", "apply_patch", DeferredTool, "applies a patch")

	visible, err := registry.Visible(ctx, Scope{})
	if err != nil {
		t.Fatalf("Visible() error = %v", err)
	}
	if got := names(visible); !reflect.DeepEqual(got, []string{"local.echo"}) {
		t.Fatalf("Visible() names = %v, want [local.echo]", got)
	}

	deferred, err := registry.Deferred(ctx, Scope{})
	if err != nil {
		t.Fatalf("Deferred() error = %v", err)
	}
	if got := names(deferred); !reflect.DeepEqual(got, []string{"local.apply_patch"}) {
		t.Fatalf("Deferred() names = %v, want [local.apply_patch]", got)
	}

	results, err := registry.Search(ctx, SearchQuery{Text: "patch"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := names(results); !reflect.DeepEqual(got, []string{"local.apply_patch"}) {
		t.Fatalf("Search() names = %v, want [local.apply_patch]", got)
	}

	if err := registry.Promote(ctx, []string{"local.apply_patch"}, Scope{}); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	visible, err = registry.Visible(ctx, Scope{})
	if err != nil {
		t.Fatalf("Visible() after promote error = %v", err)
	}
	if got := names(visible); !reflect.DeepEqual(got, []string{"local.apply_patch", "local.echo"}) {
		t.Fatalf("Visible() after promote names = %v", got)
	}
}

func TestRegistryRejectsInvalidRegistration(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	tests := []struct {
		name string
		tool gopact.Tool
		opts RegisterOptions
	}{
		{name: "nil tool", tool: nil, opts: RegisterOptions{Namespace: "local", Visibility: VisibleTool}},
		{name: "empty namespace", tool: testTool("echo", "echoes input"), opts: RegisterOptions{Visibility: VisibleTool}},
		{name: "unknown visibility", tool: testTool("echo", "echoes input"), opts: RegisterOptions{Namespace: "local", Visibility: Visibility("unknown")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := registry.Register(ctx, tt.tool, tt.opts); err == nil {
				t.Fatal("Register() error = nil, want validation error")
			}
		})
	}
}

func TestRegistryRejectsDuplicateTool(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	err := registry.Register(ctx, testTool("echo", "echoes input"), RegisterOptions{
		Namespace:  "local",
		Visibility: VisibleTool,
	})
	if !errors.Is(err, ErrToolExists) {
		t.Fatalf("Register() error = %v, want %v", err, ErrToolExists)
	}
}

func TestRegistryPromoteRejectsUnknownTool(t *testing.T) {
	err := NewRegistry().Promote(context.Background(), []string{"missing.tool"}, Scope{})
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Promote() error = %v, want %v", err, ErrToolNotFound)
	}
}

func TestRegistryInvokeAppliesToolMiddlewareAroundTool(t *testing.T) {
	ctx := context.Background()
	var order []string
	registry := NewRegistry(WithToolMiddleware(func(c *ToolContext) error {
		order = append(order, "before")
		c.Args = json.RawMessage(`{"text":"from middleware"}`)
		if err := c.Next(); err != nil {
			return err
		}
		order = append(order, "after")
		result := c.Result
		result.Content += " after"
		c.Result = result
		return nil
	}))
	err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo", Description: "echoes input"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return gopact.ToolResult{}, err
			}
			return gopact.ToolResult{Content: input.Text}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"original"}`), Scope{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Content != "from middleware after" {
		t.Fatalf("result.Content = %q, want from middleware after", result.Content)
	}
	if !reflect.DeepEqual(order, []string{"before", "after"}) {
		t.Fatalf("order = %v, want [before after]", order)
	}
}

func TestRegistryInvokeReturnsToolCallEffect(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{IDs: gopact.RuntimeIDs{CallID: "call-1"}})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("effect count = %d, want 1", len(result.Effects))
	}
	effect := result.Effects[0]
	if effect.ID != "call-1" || effect.Type != "tool_call" || effect.Target != "local.echo" || !effect.Applied {
		t.Fatalf("effect = %+v, want applied tool_call for local.echo", effect)
	}
	if effect.ReplayPolicy != gopact.EffectReplayRecordOnly {
		t.Fatalf("effect.ReplayPolicy = %q, want record_only", effect.ReplayPolicy)
	}
	if effect.Metadata["source"] != SourceLocal {
		t.Fatalf("effect metadata = %+v, want source local", effect.Metadata)
	}
}

func TestRegistryInvokePreservesToolResultOnError(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	toolErr := errors.New("tool failed after evidence")
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "delegate", Description: "delegates work"},
		InvokeFunc: func(context.Context, json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{
				Content: "partial evidence",
				Events: []gopact.Event{{
					Type: gopact.EventA2ATaskFailed,
					Metadata: map[string]any{
						"a2a_task_id": "child-task-1",
					},
				}},
			}, toolErr
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.InvokeVisible(ctx, "local.delegate", json.RawMessage(`{}`), Scope{})
	if !errors.Is(err, toolErr) {
		t.Fatalf("InvokeVisible() error = %v, want %v", err, toolErr)
	}
	if result.Content != "partial evidence" {
		t.Fatalf("result content = %q, want partial evidence", result.Content)
	}
	if len(result.Events) != 1 || result.Events[0].Type != gopact.EventA2ATaskFailed {
		t.Fatalf("result events = %+v, want preserved failed A2A event", result.Events)
	}
}

func TestRegistryInvokeUsesToolCommitForReplayableToolCallEffect(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "write", Description: "writes a file"},
		InvokeFunc: func(_ context.Context, _ json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{
				Content: "written",
				Commit: &gopact.ToolCommit{
					IdempotencyKey: "write:file.txt:sha256",
					Metadata:       map[string]any{"commit_ref": "file.txt"},
				},
			}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	args := json.RawMessage(`{"path":"file.txt","content":"hello"}`)
	result, err := registry.Invoke(ctx, "local.write", args, Scope{IDs: gopact.RuntimeIDs{CallID: "call-1"}})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("effect count = %d, want 1", len(result.Effects))
	}
	effect := result.Effects[0]
	if effect.ReplayPolicy != gopact.EffectReplayIdempotent {
		t.Fatalf("effect replay policy = %q, want idempotent", effect.ReplayPolicy)
	}
	if effect.IdempotencyKey != "write:file.txt:sha256" {
		t.Fatalf("effect idempotency key = %q, want tool commit key", effect.IdempotencyKey)
	}
	if effect.Metadata[EffectMetadataToolArgs] == nil {
		t.Fatalf("effect metadata = %+v, want replay args", effect.Metadata)
	}
	if effect.Metadata["tool_commit_metadata"].(map[string]any)["commit_ref"] != "file.txt" {
		t.Fatalf("effect metadata = %+v, want commit metadata", effect.Metadata)
	}
}

func TestRegistryInvokeStoresEmptyJSONArgsForReplayableNoArgTool(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "status", Description: "checks status"},
		InvokeFunc: func(_ context.Context, _ json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{
				Content: "ok",
				Commit: &gopact.ToolCommit{
					IdempotencyKey: "status:once",
				},
			}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Invoke(ctx, "local.status", nil, Scope{IDs: gopact.RuntimeIDs{CallID: "call-1"}})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	args, err := replayArgs(result.Effects[0])
	if err != nil {
		t.Fatalf("replayArgs() error = %v", err)
	}
	if string(args) != `{}` {
		t.Fatalf("replay args = %s, want {}", args)
	}
}

func TestRegistryInvokeRejectsIdempotentToolCommitWithoutKey(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "write", Description: "writes a file"},
		InvokeFunc: func(_ context.Context, _ json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{
				Commit: &gopact.ToolCommit{ReplayPolicy: gopact.EffectReplayIdempotent},
			}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := registry.Invoke(ctx, "local.write", json.RawMessage(`{}`), Scope{})
	if err == nil {
		t.Fatal("Invoke() error = nil, want missing idempotency key error")
	}
}

func TestRegistryInvokeVisibleOnlyInvokesModelVisibleTools(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	visibleCalled := false
	deferredCalled := false
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "visible", Description: "visible tool"},
		InvokeFunc: func(_ context.Context, _ json.RawMessage) (gopact.ToolResult, error) {
			visibleCalled = true
			return gopact.ToolResult{Content: "visible"}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool}); err != nil {
		t.Fatalf("Register(visible) error = %v", err)
	}
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "deferred", Description: "deferred tool"},
		InvokeFunc: func(_ context.Context, _ json.RawMessage) (gopact.ToolResult, error) {
			deferredCalled = true
			return gopact.ToolResult{Content: "deferred"}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: DeferredTool}); err != nil {
		t.Fatalf("Register(deferred) error = %v", err)
	}

	result, err := registry.InvokeVisible(ctx, "local.visible", json.RawMessage(`{}`), Scope{})
	if err != nil {
		t.Fatalf("InvokeVisible(visible) error = %v", err)
	}
	if result.Content != "visible" || !visibleCalled {
		t.Fatalf("visible result = %+v, called = %v, want executed visible tool", result, visibleCalled)
	}

	_, err = registry.InvokeVisible(ctx, "local.deferred", json.RawMessage(`{}`), Scope{})
	if !errors.Is(err, ErrToolNotVisible) {
		t.Fatalf("InvokeVisible(deferred) error = %v, want %v", err, ErrToolNotVisible)
	}
	if deferredCalled {
		t.Fatal("deferred tool invoked before promotion")
	}

	if err := registry.Promote(ctx, []string{"local.deferred"}, Scope{}); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	result, err = registry.InvokeVisible(ctx, "local.deferred", json.RawMessage(`{}`), Scope{})
	if err != nil {
		t.Fatalf("InvokeVisible(promoted deferred) error = %v", err)
	}
	if result.Content != "deferred" || !deferredCalled {
		t.Fatalf("promoted result = %+v, called = %v, want executed deferred tool after promote", result, deferredCalled)
	}
}

func TestRegistryInvokeIncludesMiddlewareEffects(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry(WithToolMiddleware(func(c *gopact.ToolContext) error {
		if err := c.Next(); err != nil {
			return err
		}
		c.AddEffect(gopact.EffectRecord{
			ID:      "artifact-1",
			Type:    "artifact_write",
			Target:  "artifact://result",
			Applied: true,
		})
		return nil
	}))
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if len(result.Effects) != 2 {
		t.Fatalf("effect count = %d, want default tool call plus middleware effect", len(result.Effects))
	}
	if result.Effects[1].Type != "artifact_write" || result.Effects[1].Target != "artifact://result" {
		t.Fatalf("middleware effect = %+v, want artifact_write", result.Effects[1])
	}
}

func TestRegistryInvokeIncludesMiddlewareEvents(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry(WithToolMiddleware(gopact.ToolPolicyMiddleware(gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	}))))
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{IDs: gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"}})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got := len(result.Events); got != 2 {
		t.Fatalf("event count = %d, want 2", got)
	}
	if result.Events[0].Type != gopact.EventPolicyRequested || result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %v, want policy requested/decided", eventTypes(result.Events))
	}
}

func TestRegistryInvokePassesScopeMetadataToPolicy(t *testing.T) {
	ctx := context.Background()
	policy := gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Metadata["tool_owner"] != "registry" {
			t.Fatalf("policy metadata owner = %#v, want registry", req.Metadata["tool_owner"])
		}
		resume, ok := req.Metadata[gopact.MetadataResumeRequest].(gopact.ResumeRequest)
		if !ok {
			t.Fatalf("policy metadata resume request = %#v, want ResumeRequest", req.Metadata[gopact.MetadataResumeRequest])
		}
		if resume.InterruptID != "policy:call-1" {
			t.Fatalf("resume interrupt id = %q, want policy:call-1", resume.InterruptID)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})
	registry := NewRegistry(WithToolMiddleware(gopact.ToolPolicyMiddleware(policy)))
	if err := registry.Register(ctx, testTool("echo", "echoes input"), RegisterOptions{
		Namespace:  "local",
		Visibility: VisibleTool,
		Metadata:   map[string]any{"tool_owner": "registry"},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{
		IDs: gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"},
		Metadata: map[string]any{
			gopact.MetadataResumeRequest: gopact.ResumeRequest{InterruptID: "policy:call-1"},
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
}

func TestRegistryInvokePropagatesScopeRuntimeIDsToToolContext(t *testing.T) {
	ctx := context.Background()
	var got gopact.RuntimeIDs
	registry := NewRegistry()
	if err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "inspect_ids"},
		InvokeFunc: func(ctx context.Context, _ json.RawMessage) (gopact.ToolResult, error) {
			var ok bool
			got, ok = gopact.RuntimeIDsFromContext(ctx)
			if !ok {
				t.Fatal("RuntimeIDsFromContext() ok = false, want true")
			}
			return gopact.ToolResult{Content: "ok"}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := registry.Invoke(ctx, "local.inspect_ids", json.RawMessage(`{}`), Scope{
		IDs: gopact.RuntimeIDs{
			RunID:        "run-1",
			ThreadID:     "thread-1",
			CallID:       "call-1",
			ParentCallID: "parent-call",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got.RunID != "run-1" || got.ThreadID != "thread-1" || got.CallID != "call-1" || got.ParentCallID != "parent-call" {
		t.Fatalf("runtime ids from context = %+v, want scope ids", got)
	}
}

func TestRegistryInvokeReturnsMiddlewareEventsOnError(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry(WithToolMiddleware(gopact.ToolPolicyMiddleware(gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "needs approval"}, nil
	}))))
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{IDs: gopact.RuntimeIDs{RunID: "run-1", CallID: "call-1"}})
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("Invoke() error = %v, want ErrInterrupted", err)
	}
	if got := len(result.Events); got != 2 {
		t.Fatalf("event count = %d, want policy requested/decided", got)
	}
	if result.Events[0].Type != gopact.EventPolicyRequested || result.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %v, want policy requested/decided", eventTypes(result.Events))
	}
	if len(result.Effects) != 0 {
		t.Fatalf("effect count = %d, want no applied tool call effect on interrupt", len(result.Effects))
	}
}

func TestRegistryInvokeToolMiddlewareCanShortCircuit(t *testing.T) {
	ctx := context.Background()
	called := false
	registry := NewRegistry(WithToolMiddleware(func(c *ToolContext) error {
		c.Result = gopact.ToolResult{Content: "cached"}
		return nil
	}))
	err := registry.Register(ctx, gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{Name: "echo"},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			called = true
			return gopact.ToolResult{Content: string(args)}, nil
		},
	}, RegisterOptions{Namespace: "local", Visibility: VisibleTool})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"original"}`), Scope{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if called {
		t.Fatal("tool invoked after middleware short-circuit")
	}
	if result.Content != "cached" {
		t.Fatalf("result.Content = %q, want cached", result.Content)
	}
}

func TestRegistryInvokeToolMiddlewareError(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("tool middleware failed")
	registry := NewRegistry(WithToolMiddleware(func(_ *ToolContext) error {
		return wantErr
	}))
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	_, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke() error = %v, want %v", err, wantErr)
	}
}

func TestRegistryInvokeUsesPluginHostToolMiddleware(t *testing.T) {
	ctx := context.Background()
	host := gopact.NewPluginHost()
	host.UseToolMiddleware(func(c *gopact.ToolContext) error {
		if err := c.Next(); err != nil {
			return err
		}
		result := c.Result
		result.Content += " from plugin"
		c.Result = result
		return nil
	})

	registry := NewRegistry(WithPluginHost(host))
	mustRegisterTool(t, registry, "local", "echo", VisibleTool, "echoes input")

	result, err := registry.Invoke(ctx, "local.echo", json.RawMessage(`{"text":"hello"}`), Scope{})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Content != `{"text":"hello"} from plugin` {
		t.Fatalf("result.Content = %q, want plugin suffix", result.Content)
	}
}

func TestRegistryInvokeRejectsUnknownTool(t *testing.T) {
	_, err := NewRegistry().Invoke(context.Background(), "missing.tool", nil, Scope{})
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Invoke() error = %v, want %v", err, ErrToolNotFound)
	}
}

func mustRegisterTool(t *testing.T, registry *Registry, namespace string, name string, visibility Visibility, description string) {
	t.Helper()

	err := registry.Register(context.Background(), testTool(name, description), RegisterOptions{
		Namespace:  namespace,
		Visibility: visibility,
		Source:     SourceLocal,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

func testTool(name string, description string) gopact.Tool {
	return gopact.ToolFunc{
		SpecValue: gopact.ToolSpec{
			Name:        name,
			Description: description,
			InputSchema: gopact.JSONSchema{"type": "object"},
		},
		InvokeFunc: func(_ context.Context, args json.RawMessage) (gopact.ToolResult, error) {
			return gopact.ToolResult{Content: string(args)}, nil
		},
	}
}

func names(infos []ToolInfo) []string {
	got := make([]string, 0, len(infos))
	for _, info := range infos {
		got = append(got, info.Name)
	}
	return got
}

func eventTypes(events []gopact.Event) []gopact.EventType {
	got := make([]gopact.EventType, 0, len(events))
	for _, event := range events {
		got = append(got, event.Type)
	}
	return got
}
