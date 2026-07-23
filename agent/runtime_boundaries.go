package agent

import (
	"context"
	"errors"

	"github.com/gopact-ai/gopact"
)

// ToolExecutor executes one normalized tool call.
type ToolExecutor interface {
	ExecuteTool(context.Context, gopact.ToolCall) (gopact.ToolOutcome, error)
}

// Tool is one model-visible tool capability.
type Tool interface {
	Spec() gopact.ToolSpec
}

// InvokableTool executes as a typed Workflow child boundary.
type InvokableTool interface {
	Tool
	gopact.Invokable[gopact.ToolCall, gopact.ToolOutcome]
}

// DirectTool executes in-process without a child Workflow boundary. It has no
// independent checkpoint, retry, or isolation; callers own those guarantees.
type DirectTool interface {
	Tool
	ToolExecutor
}

// ToolExecutorFunc adapts a function into a ToolExecutor.
type ToolExecutorFunc func(context.Context, gopact.ToolCall) (gopact.ToolOutcome, error)

// ExecuteTool implements ToolExecutor.
func (executor ToolExecutorFunc) ExecuteTool(ctx context.Context, call gopact.ToolCall) (gopact.ToolOutcome, error) {
	if executor == nil {
		return nil, errors.New("agent: tool executor is nil")
	}
	return executor(ctx, call)
}
