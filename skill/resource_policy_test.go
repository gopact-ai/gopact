package skill

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyResourceReaderDenySkipsRead(t *testing.T) {
	ctx := context.Background()
	base := &FakeResourceReader{
		Contents: map[string]ResourceContent{
			"references/guide.md": {Text: "read me"},
		},
	}
	reader, err := NewPolicyResourceReader(
		base,
		gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			if req.Boundary != gopact.PolicyBoundarySkill {
				t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundarySkill)
			}
			if req.Action != gopact.PolicyActionRead {
				t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionRead)
			}
			input, ok := req.Input.(PolicyInput)
			if !ok {
				t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
			}
			if input.Kind != PolicyKindResource || input.Name != "repo-review" {
				t.Fatalf("policy input = %+v, want resource for repo-review", input)
			}
			if input.Resource.Name != "guide" || input.URI != "references/guide.md" {
				t.Fatalf("resource input = %+v, uri = %q, want guide references/guide.md", input.Resource, input.URI)
			}
			return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "resource blocked"}, nil
		}),
		WithPolicyIDs(gopact.RuntimeIDs{RunID: "run-1"}),
	)
	if err != nil {
		t.Fatalf("NewPolicyResourceReader() error = %v", err)
	}

	_, err = reader.ReadResource(ctx, ResourceReadRequest{
		SkillName: "repo-review",
		Resource:  Resource{Name: "guide", URI: "references/guide.md", MIMEType: "text/markdown"},
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("ReadResource() error = %v, want ErrPolicyDenied", err)
	}
	if len(base.Reads) != 0 {
		t.Fatalf("base reads = %+v, want none", base.Reads)
	}
}

func TestPolicyResourceReaderPublishesEventsAndAllowsRead(t *testing.T) {
	ctx := context.Background()
	base := &FakeResourceReader{
		Contents: map[string]ResourceContent{
			"references/guide.md": {
				URI:      "references/guide.md",
				MIMEType: "text/markdown",
				Text:     "read me",
			},
		},
	}
	var events []gopact.Event
	reader, err := NewPolicyResourceReader(
		base,
		gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
			return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
		}),
		WithPolicyEventSink(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewPolicyResourceReader() error = %v", err)
	}

	content, err := reader.ReadResource(ctx, ResourceReadRequest{
		SkillName: "repo-review",
		Resource:  Resource{Name: "guide", URI: "references/guide.md", MIMEType: "text/markdown"},
	})
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if content.Text != "read me" {
		t.Fatalf("content text = %q, want read me", content.Text)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 events", events)
	}
	if events[0].Type != gopact.EventPolicyRequested || events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("event types = %q, %q", events[0].Type, events[1].Type)
	}
}

func TestPolicyScriptRunnerReviewReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	base := &FakeScriptRunner{
		Results: map[string]ScriptResult{
			"lint": {ScriptName: "lint", ExitCode: 0},
		},
	}
	runner, err := NewPolicyScriptRunner(base, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action != gopact.PolicyActionExec {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionExec)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if input.Kind != PolicyKindScript || input.Name != "repo-review" || input.Script.Name != "lint" {
			t.Fatalf("policy input = %+v, want script lint for repo-review", input)
		}
		if len(input.Args) != 1 || input.Args[0] != "--fix" {
			t.Fatalf("args = %+v, want --fix", input.Args)
		}
		if input.StdinBytes != 7 {
			t.Fatalf("stdin bytes = %d, want 7", input.StdinBytes)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review script"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyScriptRunner() error = %v", err)
	}

	_, err = runner.RunScript(ctx, ScriptRunRequest{
		SkillName: "repo-review",
		Script:    Script{Name: "lint", Command: []string{"go", "test", "./..."}},
		Args:      []string{"--fix"},
		Stdin:     []byte("payload"),
	})
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("RunScript() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("RunScript() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundarySkill) {
		t.Fatalf("RequiredBy = %q, want skill", interruptErr.Record.RequiredBy)
	}
	if len(base.Runs) != 0 {
		t.Fatalf("base runs = %+v, want none", base.Runs)
	}
}

func TestPolicyScriptRunnerAllowsExec(t *testing.T) {
	ctx := context.Background()
	base := &FakeScriptRunner{
		Results: map[string]ScriptResult{
			"lint": {ScriptName: "lint", ExitCode: 0, Stdout: []byte("ok\n")},
		},
	}
	runner, err := NewPolicyScriptRunner(base, gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow, Reason: "ok"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyScriptRunner() error = %v", err)
	}

	result, err := runner.RunScript(ctx, ScriptRunRequest{
		SkillName: "repo-review",
		Script:    Script{Name: "lint", Command: []string{"go", "test", "./..."}},
	})
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if result.ExitCode != 0 || string(result.Stdout) != "ok\n" {
		t.Fatalf("result = %+v, want exit 0 stdout ok", result)
	}
	if len(base.Runs) != 1 {
		t.Fatalf("base runs = %+v, want one run", base.Runs)
	}
}

func TestNewPolicyResourceReaderAndScriptRunnerRequireDependencies(t *testing.T) {
	policy := gopact.PolicyFunc(func(context.Context, gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	})
	if _, err := NewPolicyResourceReader(nil, policy); !errors.Is(err, ErrResourceReaderRequired) {
		t.Fatalf("NewPolicyResourceReader(nil, policy) error = %v, want ErrResourceReaderRequired", err)
	}
	if _, err := NewPolicyResourceReader(&FakeResourceReader{}, nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyResourceReader(reader, nil) error = %v, want ErrPolicyRequired", err)
	}
	if _, err := NewPolicyScriptRunner(nil, policy); !errors.Is(err, ErrScriptRunnerRequired) {
		t.Fatalf("NewPolicyScriptRunner(nil, policy) error = %v, want ErrScriptRunnerRequired", err)
	}
	if _, err := NewPolicyScriptRunner(&FakeScriptRunner{}, nil); !errors.Is(err, ErrPolicyRequired) {
		t.Fatalf("NewPolicyScriptRunner(runner, nil) error = %v, want ErrPolicyRequired", err)
	}
}
