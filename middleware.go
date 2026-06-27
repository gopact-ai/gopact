package gopact

import "context"

// NodeHandler handles one node execution boundary.
type NodeHandler func(*NodeContext) error

// NodeContext carries node input, output, runtime identity, and middleware state.
type NodeContext struct {
	Context  context.Context
	IDs      RuntimeIDs
	Node     string
	Step     int
	Input    any
	Output   any
	Err      error
	Effects  []EffectRecord
	Metadata map[string]any

	handlers []NodeHandler
	index    int
}

// NodeContextOptions configures a NodeContext.
type NodeContextOptions struct {
	IDs      RuntimeIDs
	Node     string
	Step     int
	Input    any
	Output   any
	Effects  []EffectRecord
	Metadata map[string]any
}

// NewNodeContext creates a node middleware context.
func NewNodeContext(ctx context.Context, opts NodeContextOptions) *NodeContext {
	if ctx == nil {
		ctx = context.TODO()
	}
	return &NodeContext{
		Context:  ctx,
		IDs:      opts.IDs,
		Node:     opts.Node,
		Step:     opts.Step,
		Input:    opts.Input,
		Output:   opts.Output,
		Effects:  copyEffectRecords(opts.Effects),
		Metadata: copyAnyMap(opts.Metadata),
		index:    -1,
	}
}

// AddEffect appends an external effect observed while executing the node.
func (c *NodeContext) AddEffect(effect EffectRecord) {
	if c == nil {
		return
	}
	c.Effects = append(c.Effects, copyEffectRecord(effect))
}

// Next advances to the next handler in the chain.
func (c *NodeContext) Next() error {
	if c == nil {
		return nil
	}
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}
	err := c.handlers[c.index](c)
	if err != nil {
		c.Err = err
	}
	return err
}

func copyEffectRecords(in []EffectRecord) []EffectRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]EffectRecord, len(in))
	for i, effect := range in {
		out[i] = copyEffectRecord(effect)
	}
	return out
}

func copyEffectRecord(in EffectRecord) EffectRecord {
	out := in
	out.DependsOn = append([]string(nil), in.DependsOn...)
	out.Artifacts = copyArtifactRefs(in.Artifacts)
	if in.Sandbox != nil {
		sandbox := *in.Sandbox
		sandbox.Command = append([]string(nil), in.Sandbox.Command...)
		sandbox.Metadata = copyAnyMap(in.Sandbox.Metadata)
		out.Sandbox = &sandbox
	}
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyArtifactRefs(in []ArtifactRef) []ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]ArtifactRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Metadata = copyAnyMap(ref.Metadata)
	}
	return out
}

func copyEvents(in []Event) []Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]Event, len(in))
	for i, event := range in {
		out[i] = copyEvent(event)
	}
	return out
}

func copyEvent(in Event) Event {
	out := in
	if in.StepSnapshot != nil {
		snapshot := copyStepSnapshot(*in.StepSnapshot)
		out.StepSnapshot = &snapshot
	}
	if in.Message != nil {
		message := *in.Message
		message.Parts = copyContentParts(in.Message.Parts)
		message.ToolCalls = copyToolCalls(in.Message.ToolCalls)
		out.Message = &message
	}
	if in.ToolCall != nil {
		toolCall := *in.ToolCall
		toolCall.Arguments = append([]byte(nil), in.ToolCall.Arguments...)
		out.ToolCall = &toolCall
	}
	if in.Result != nil {
		result := *in.Result
		result.Artifacts = copyArtifactRefs(in.Result.Artifacts)
		result.Effects = copyEffectRecords(in.Result.Effects)
		result.Events = copyEvents(in.Result.Events)
		result.Metadata = copyAnyMap(in.Result.Metadata)
		out.Result = &result
	}
	out.Artifacts = copyArtifactRefs(in.Artifacts)
	if in.PolicyRequest != nil {
		req := copyPolicyRequest(*in.PolicyRequest)
		out.PolicyRequest = &req
	}
	if in.PolicyDecision != nil {
		decision := copyPolicyDecision(*in.PolicyDecision)
		out.PolicyDecision = &decision
	}
	out.Redaction.Fields = append([]string(nil), in.Redaction.Fields...)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyContentParts(in []ContentPart) []ContentPart {
	if len(in) == 0 {
		return nil
	}
	out := make([]ContentPart, len(in))
	for i, part := range in {
		out[i] = part
		out[i].Metadata = copyAnyMap(part.Metadata)
	}
	return out
}

func copyToolCalls(in []ToolCall) []ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolCall, len(in))
	for i, toolCall := range in {
		out[i] = toolCall
		out[i].Arguments = append([]byte(nil), toolCall.Arguments...)
	}
	return out
}

func copyPolicyRequest(in PolicyRequest) PolicyRequest {
	out := in
	if input, ok := in.Input.(PromptInjectionPolicyInput); ok {
		out.Input = copyPromptInjectionPolicyInput(input)
	}
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

func copyPolicyDecision(in PolicyDecision) PolicyDecision {
	out := in
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}

// ComposeNodeHandler composes middleware around a final handler.
func ComposeNodeHandler(final NodeHandler, middlewares ...NodeHandler) NodeHandler {
	return func(c *NodeContext) error {
		if c == nil {
			c = NewNodeContext(context.TODO(), NodeContextOptions{})
		}
		if final == nil {
			final = func(_ *NodeContext) error { return nil }
		}
		handlers := make([]NodeHandler, 0, len(middlewares)+1)
		handlers = append(handlers, middlewares...)
		handlers = append(handlers, final)
		c.handlers = handlers
		c.index = -1
		return c.Next()
	}
}
