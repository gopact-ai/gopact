package gopacttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/agent"
	"github.com/gopact-ai/gopact/workflow"
)

func TestDirectAgentConformance(t *testing.T) {
	RequireAgentConformance(t, AgentConformanceCase{
		Agent: directEchoAgent{identity: agent.Identity{
			Name: "direct-echo", Description: "echoes without a Workflow", Version: "v1",
		}},
		Request:  agent.Request{Messages: []gopact.Message{gopact.UserMessage("hello")}},
		Validate: validateEchoResponse,
	})
}

func TestWorkflowAgentConformance(t *testing.T) {
	identity := agent.Identity{Name: "echo", Description: "echoes the request", Version: "v1"}
	wf := workflow.New[agent.Request, agent.Response](identity.Name, workflow.WithTopologyVersion(identity.Version))
	echo := wf.Node("echo", func(_ context.Context, input agent.Request) (agent.Response, error) {
		if len(input.Messages) == 0 {
			return agent.Response{}, errors.New("message is required")
		}
		return agent.Response{Message: input.Messages[0]}, nil
	})
	wf.Entry(echo)
	wf.Exit(echo)
	target, err := agent.NewWorkflowAgent(identity, wf)
	if err != nil {
		t.Fatal(err)
	}
	RequireWorkflowAgentConformance(t, AgentConformanceCase{
		Agent:    target,
		Request:  agent.Request{Messages: []gopact.Message{gopact.UserMessage("hello")}},
		Validate: validateEchoResponse,
	})
}

func validateEchoResponse(response agent.Response) error {
	if len(response.Message.Parts) != 1 || response.Message.Parts[0].Text != "hello" {
		return errors.New("unexpected echo response")
	}
	return nil
}

type directEchoAgent struct{ identity agent.Identity }

func (target directEchoAgent) Identity() agent.Identity { return target.identity }

func (target directEchoAgent) Invoke(
	ctx context.Context,
	request agent.Request,
	_ ...gopact.RunOption,
) (agent.Response, error) {
	if err := ctx.Err(); err != nil {
		return agent.Response{}, err
	}
	if len(request.Messages) == 0 {
		return agent.Response{}, errors.New("message is required")
	}
	return agent.Response{Message: request.Messages[0]}, nil
}
