package agent

import (
	"context"
	"errors"
	"iter"
	"strings"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/workflow"
)

// WorkflowAgent is an Agent domain facade whose execution semantics come from
// its configured Workflow runtime.
type WorkflowAgent struct {
	identity Identity
	workflow *workflow.Workflow[Request, Response]
}

var _ StreamingAgent = (*WorkflowAgent)(nil)

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
	return target.workflow.Invoke(ctx, request.Clone(), options...)
}

// InvokeStream streams each committed Workflow response as one Agent chunk.
func (target *WorkflowAgent) InvokeStream(ctx context.Context, request Request, options ...gopact.RunOption) iter.Seq2[Chunk, error] {
	return func(yield func(Chunk, error) bool) {
		if target == nil || target.workflow == nil {
			yield(Chunk{}, errors.New("agent: workflow agent is nil"))
			return
		}
		for response, err := range target.workflow.InvokeStream(ctx, request.Clone(), options...) {
			if err != nil {
				yield(Chunk{}, err)
				return
			}
			if !yield(responseChunk(response), nil) {
				return
			}
		}
	}
}

func responseChunk(response Response) Chunk {
	message := cloneMessage(response.Message)
	artifacts := cloneRefs(response.Artifacts)
	for i := range artifacts {
		message.Parts = append(message.Parts, gopact.MessagePart{
			Type: gopact.MessagePartTypeArtifact,
			Ref:  &artifacts[i],
		})
	}
	var text strings.Builder
	for _, part := range message.Parts {
		if part.Type == gopact.MessagePartTypeText {
			text.WriteString(part.Text)
		}
	}
	return Chunk{Text: text.String(), Parts: message.Parts}
}
