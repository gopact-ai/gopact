package gopacttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/agent"
	"github.com/gopact-ai/gopact/workflow"
)

func TestAgentConformance(t *testing.T) {
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
	RequireAgentConformance(t, AgentConformanceCase{
		Agent:   target,
		Request: agent.Request{Messages: []gopact.Message{gopact.UserMessage("hello")}},
		Validate: func(response agent.Response) error {
			if len(response.Message.Parts) != 1 || response.Message.Parts[0].Text != "hello" {
				return errors.New("unexpected echo response")
			}
			return nil
		},
	})
}
