package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/workflow"
)

func TestWorkflowAgentInvokesWorkflow(t *testing.T) {
	identity := testIdentity()
	wf := workflow.New[Request, Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	respond := wf.Node("respond", func(_ context.Context, request Request) (Response, error) {
		return Response{Message: request.Messages[0]}, nil
	})
	wf.Entry(respond)
	wf.Exit(respond)
	target, err := NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}
	var events []gopact.Event
	response, err := target.Invoke(
		context.Background(),
		Request{Messages: []gopact.Message{gopact.UserMessage("hello")}},
		gopact.WithRunID("agent-workflow-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if target.Identity() != identity || response.Message.Parts[0].Text != "hello" {
		t.Fatalf("Identity/response = %+v/%+v", target.Identity(), response)
	}
	if len(events) == 0 || events[0].DefinitionID != identity.Name || events[0].Type != workflow.EventWorkflowStarted ||
		events[len(events)-1].Type != workflow.EventWorkflowCompleted {
		t.Fatalf("events = %+v, want Workflow-owned lifecycle", events)
	}
}

func TestWorkflowAgentStreamsCommittedResponses(t *testing.T) {
	identity := testIdentity()
	wf := workflow.New[Request, Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	respond := wf.Node("respond", func(_ context.Context, request Request) (Response, error) {
		return Response{Message: request.Messages[0]}, nil
	})
	wf.Entry(respond)
	wf.Exit(respond)
	target, err := NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}
	var _ StreamingAgent = target

	var chunks []Chunk
	for chunk, streamErr := range target.InvokeStream(
		context.Background(),
		Request{Messages: []gopact.Message{gopact.UserMessage("hello stream")}},
		gopact.WithRunID("agent-stream-run"),
	) {
		if streamErr != nil {
			t.Fatalf("InvokeStream() error = %v", streamErr)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 1 || chunks[0].Text != "hello stream" ||
		len(chunks[0].Parts) != 1 || chunks[0].Parts[0].Text != "hello stream" {
		t.Fatalf("chunks = %+v, want one cloned text chunk", chunks)
	}
}

func TestWorkflowAgentStreamPreservesArtifactsAndMessageParts(t *testing.T) {
	identity := testIdentity()
	wf := workflow.New[Request, Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	existingRef := gopact.ArtifactRef{URI: "artifact://existing", Kind: "image", Digest: "sha256:existing"}
	response := Response{
		Message: gopact.Message{Role: gopact.MessageRoleAssistant, Parts: []gopact.MessagePart{
			{Type: gopact.MessagePartTypeText, Text: "hello "},
			{Type: "reasoning", Text: "kept"},
			{Type: gopact.MessagePartTypeArtifact, Ref: &existingRef},
			{Type: gopact.MessagePartTypeText, Text: "world"},
		}},
		Artifacts: []gopact.ArtifactRef{{
			URI: "artifact://response", Kind: "document", Digest: "sha256:response",
		}},
	}
	respond := wf.Node("respond", func(context.Context, Request) (Response, error) {
		return response, nil
	})
	wf.Entry(respond)
	wf.Exit(respond)
	target, err := NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}

	var chunks []Chunk
	for chunk, streamErr := range target.InvokeStream(context.Background(), Request{}) {
		if streamErr != nil {
			t.Fatalf("InvokeStream() error = %v", streamErr)
		}
		chunks = append(chunks, chunk)
	}
	existingRef.URI = "artifact://mutated"
	response.Artifacts[0].URI = "artifact://mutated"
	if len(chunks) != 1 || chunks[0].Text != "hello world" || len(chunks[0].Parts) != 5 {
		t.Fatalf("chunks = %+v, want text and all message/artifact parts", chunks)
	}
	parts := chunks[0].Parts
	if parts[1].Type != "reasoning" || parts[1].Text != "kept" || parts[2].Ref == nil ||
		parts[2].Ref.URI != "artifact://existing" || parts[4].Type != gopact.MessagePartTypeArtifact ||
		parts[4].Ref == nil || *parts[4].Ref != (gopact.ArtifactRef{
		URI: "artifact://response", Kind: "document", Digest: "sha256:response",
	}) {
		t.Fatalf("parts = %+v, want cloned existing and projected response artifacts", parts)
	}
}

func TestWorkflowAgentStreamPreservesSentinelError(t *testing.T) {
	identity := testIdentity()
	wf := workflow.New[Request, Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	wantErr := errors.New("sentinel")
	fail := wf.Node("fail", func(context.Context, Request) (Response, error) {
		return Response{}, wantErr
	})
	wf.Entry(fail)
	wf.Exit(fail)
	target, err := NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}

	var gotErr error
	for _, streamErr := range target.InvokeStream(context.Background(), Request{}) {
		gotErr = streamErr
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("InvokeStream() error = %v, want errors.Is sentinel", gotErr)
	}
}

func TestWorkflowAgentStreamPreservesCanceledContext(t *testing.T) {
	identity := testIdentity()
	wf := workflow.New[Request, Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	respond := wf.Node("respond", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	})
	wf.Entry(respond)
	wf.Exit(respond)
	target, err := NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var gotErr error
	for _, streamErr := range target.InvokeStream(ctx, Request{}) {
		gotErr = streamErr
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("InvokeStream() error = %v, want errors.Is context.Canceled", gotErr)
	}
}

func TestWorkflowAgentForwardsResumeOption(t *testing.T) {
	identity := testIdentity()
	bodyRuns := 0
	wf := workflow.New[Request, Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	respond := wf.Node("respond", func(_ context.Context, request Request) (Response, error) {
		bodyRuns++
		return Response{Message: request.Messages[0]}, nil
	})
	respond.Guard(workflow.BeforeRun("approval", workflow.GuardFunc[Request, Response](
		func(context.Context, workflow.GuardContext[Request, Response]) (workflow.GuardDecision[Request, Response], error) {
			return workflow.GuardInterrupt[Request, Response]{Request: workflow.InterruptRequest{
				ID: "approval-1", Subject: "agent response", ResolutionSchemaRef: "schema://approval",
			}}, nil
		},
	)))
	wf.Entry(respond)
	wf.Exit(respond)
	target, err := NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}
	request := Request{Messages: []gopact.Message{gopact.UserMessage("approved")}}
	_, err = target.Invoke(context.Background(), request, gopact.WithRunID("resume-agent"))
	var interrupted workflow.InterruptError
	if !errors.As(err, &interrupted) {
		t.Fatalf("Invoke() error = %v, want workflow InterruptError", err)
	}
	response, err := target.Invoke(context.Background(), Request{}, workflow.WithResume(workflow.ResumeRequest{
		RunID: interrupted.RunID, CheckpointID: interrupted.CheckpointID,
		Resolutions: []workflow.InterruptResolution{{InterruptID: "approval-1", PayloadRef: "artifact://approved"}},
	}))
	if err != nil {
		t.Fatalf("resume Invoke() error = %v", err)
	}
	if bodyRuns != 1 || response.Message.Parts[0].Text != "approved" {
		t.Fatalf("body runs/response = %d/%+v, want one resumed body", bodyRuns, response)
	}
}
