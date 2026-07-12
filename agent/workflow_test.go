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
