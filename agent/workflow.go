package agent

import (
	"context"
	"errors"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/workflow"
)

// WorkflowAgent is an Agent domain facade over the Workflow runtime.
type WorkflowAgent struct {
	identity Identity
	workflow *workflow.Workflow[Request, Response]
}

var _ Agent = (*WorkflowAgent)(nil)

// NewWorkflowAgent binds immutable Agent identity to its Workflow definition.
func NewWorkflowAgent(identity Identity, wf *workflow.Workflow[Request, Response]) (*WorkflowAgent, error) {
	if err := validateCatalogIdentity(identity); err != nil {
		return nil, err
	}
	if wf == nil {
		return nil, errors.New("agent: workflow is nil")
	}
	if err := wf.Validate(); err != nil {
		return nil, err
	}
	return &WorkflowAgent{identity: identity, workflow: wf}, nil
}

// Identity returns the immutable Agent identity.
func (target *WorkflowAgent) Identity() Identity {
	if target == nil {
		return Identity{}
	}
	return target.identity
}

// Invoke delegates execution to the Agent's Workflow.
func (target *WorkflowAgent) Invoke(ctx context.Context, request Request, options ...gopact.RunOption) (Response, error) {
	if target == nil || target.workflow == nil {
		return Response{}, errors.New("agent: workflow agent is nil")
	}
	request.Messages = cloneMessages(request.Messages)
	request.Artifacts = cloneRefs(request.Artifacts)
	request.Metadata = cloneStringMap(request.Metadata)
	return target.workflow.Invoke(ctx, request, options...)
}
