// Package agenttool adapts a local agent runnable into a model-visible tool.
package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/a2a"
)

var (
	ErrRunnableRequired = errors.New("agenttool: runnable is required")
	ErrAgentRequired    = errors.New("agenttool: a2a agent is required")
	ErrNameRequired     = errors.New("agenttool: name is required")
	ErrInvalidArgs      = errors.New("agenttool: invalid args")
)

// Input is the default JSON payload accepted by an agent tool.
type Input struct {
	Input    string           `json:"input,omitempty"`
	Messages []gopact.Message `json:"messages,omitempty"`
	Metadata map[string]any   `json:"metadata,omitempty"`
}

// InputMapper converts raw tool args into the child runnable input.
type InputMapper func(ctx context.Context, args json.RawMessage) (any, error)

// ResultMapper converts child agent events into a tool result.
type ResultMapper func(ctx context.Context, events []gopact.Event) (gopact.ToolResult, error)

// Tool exposes one local runnable as a gopact.Tool.
type Tool struct {
	name         string
	description  string
	inputSchema  gopact.JSONSchema
	runnable     gopact.Runnable
	inputMapper  InputMapper
	resultMapper ResultMapper
}

// Option configures a Tool.
type Option func(*Tool) error

// New creates an agent-backed tool.
func New(name string, runnable gopact.Runnable, opts ...Option) (*Tool, error) {
	tool := &Tool{
		name:         name,
		runnable:     runnable,
		inputSchema:  defaultInputSchema(),
		inputMapper:  defaultInputMapper,
		resultMapper: defaultResultMapper,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(tool); err != nil {
			return nil, err
		}
	}
	if tool.name == "" {
		return nil, ErrNameRequired
	}
	if tool.runnable == nil {
		return nil, ErrRunnableRequired
	}
	if tool.inputMapper == nil {
		return nil, errors.New("agenttool: input mapper is required")
	}
	if tool.resultMapper == nil {
		return nil, errors.New("agenttool: result mapper is required")
	}
	return tool, nil
}

// NewFromCard creates an agent-backed tool from an A2A agent card.
func NewFromCard(card a2a.AgentCard, runnable gopact.Runnable, opts ...Option) (*Tool, error) {
	spec, err := SpecFromCard(card)
	if err != nil {
		return nil, err
	}
	options := []Option{
		WithDescription(spec.Description),
		WithInputSchema(spec.InputSchema),
	}
	options = append(options, opts...)
	return New(spec.Name, runnable, options...)
}

// WithDescription sets the model-facing tool description.
func WithDescription(description string) Option {
	return func(tool *Tool) error {
		tool.description = description
		return nil
	}
}

// WithInputSchema sets the model-facing input schema.
func WithInputSchema(schema gopact.JSONSchema) Option {
	return func(tool *Tool) error {
		tool.inputSchema = copySchema(schema)
		return nil
	}
}

// WithInputMapper sets the tool-args to child-input mapper.
func WithInputMapper(mapper InputMapper) Option {
	return func(tool *Tool) error {
		if mapper == nil {
			return errors.New("agenttool: input mapper is required")
		}
		tool.inputMapper = mapper
		return nil
	}
}

// WithResultMapper sets the child-events to tool-result mapper.
func WithResultMapper(mapper ResultMapper) Option {
	return func(tool *Tool) error {
		if mapper == nil {
			return errors.New("agenttool: result mapper is required")
		}
		tool.resultMapper = mapper
		return nil
	}
}

// Spec returns the model-visible tool spec.
func (t *Tool) Spec(_ context.Context) (gopact.ToolSpec, error) {
	if t == nil {
		return gopact.ToolSpec{}, ErrRunnableRequired
	}
	if t.name == "" {
		return gopact.ToolSpec{}, ErrNameRequired
	}
	return gopact.ToolSpec{
		Name:        t.name,
		Description: t.description,
		InputSchema: copySchema(t.inputSchema),
	}, nil
}

// Invoke runs the child agent and returns its events/artifacts as a tool result.
func (t *Tool) Invoke(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if t == nil || t.runnable == nil {
		return gopact.ToolResult{}, ErrRunnableRequired
	}
	input, err := t.inputMapper(ctx, args)
	if err != nil {
		return gopact.ToolResult{}, err
	}
	ids := childRuntimeIDs(ctx, t.name)
	events, runErr := collectChildEvents(t.runnable.Run(ctx, input, gopact.WithRuntimeIDs(ids)), ids)
	result, resultErr := t.resultMapper(ctx, events)
	if result.Metadata == nil {
		result.Metadata = make(map[string]any)
	}
	result.Metadata["agent_name"] = t.name
	result.Metadata["child_call_id"] = ids.CallID
	result.Metadata["parent_call_id"] = ids.ParentCallID
	result.Metadata["child_event_count"] = len(events)
	result.Events = append(result.Events, events...)
	if runErr != nil {
		if resultErr != nil {
			return result, errors.Join(fmt.Errorf("agenttool: run child agent %q: %w", t.name, runErr), resultErr)
		}
		return result, fmt.Errorf("agenttool: run child agent %q: %w", t.name, runErr)
	}
	return result, resultErr
}

// SpecFromCard converts an A2A agent card into a model-visible tool spec.
func SpecFromCard(card a2a.AgentCard) (gopact.ToolSpec, error) {
	if card.Name == "" {
		return gopact.ToolSpec{}, ErrNameRequired
	}
	description := card.Description
	if description == "" && len(card.Capabilities) > 0 {
		description = "agent capabilities: " + strings.Join(card.Capabilities, ", ")
	}
	return gopact.ToolSpec{
		Name:        card.Name,
		Description: description,
		InputSchema: defaultInputSchema(),
	}, nil
}

// CardFromSpec converts a model-visible tool spec into an A2A agent card.
func CardFromSpec(spec gopact.ToolSpec) a2a.AgentCard {
	return a2a.AgentCard{
		Name:        spec.Name,
		Description: spec.Description,
		Metadata: map[string]any{
			"tool_schema": copySchema(spec.InputSchema),
		},
	}
}

func defaultInputMapper(_ context.Context, args json.RawMessage) (any, error) {
	input, err := decodeInput(args)
	if err != nil {
		return nil, err
	}
	if len(input.Messages) > 0 {
		return append([]gopact.Message(nil), input.Messages...), nil
	}
	return gopact.Message{Role: gopact.RoleUser, Content: input.Input}, nil
}

func decodeInput(args json.RawMessage) (Input, error) {
	if len(args) == 0 {
		return Input{}, nil
	}
	var input Input
	if err := json.Unmarshal(args, &input); err != nil {
		var text string
		if err := json.Unmarshal(args, &text); err != nil {
			return Input{}, fmt.Errorf("%w: %v", ErrInvalidArgs, err)
		}
		input.Input = text
	}
	input.Messages = append([]gopact.Message(nil), input.Messages...)
	input.Metadata = copyAnyMap(input.Metadata)
	return input, nil
}

func defaultResultMapper(_ context.Context, events []gopact.Event) (gopact.ToolResult, error) {
	var result gopact.ToolResult
	for _, event := range events {
		if event.Message != nil && event.Message.Role == gopact.RoleAssistant {
			if text := event.Message.Text(); text != "" {
				result.Content = text
			}
		}
		if event.Result != nil && event.Result.Content != "" && result.Content == "" {
			result.Content = event.Result.Content
		}
		result.Artifacts = append(result.Artifacts, copyArtifactRefs(event.Artifacts)...)
		if event.Result != nil {
			result.Artifacts = append(result.Artifacts, copyArtifactRefs(event.Result.Artifacts)...)
		}
	}
	result.Artifacts = dedupeArtifactRefs(result.Artifacts)
	return result, nil
}

func collectChildEvents(seq iter.Seq2[gopact.Event, error], ids gopact.RuntimeIDs) ([]gopact.Event, error) {
	var events []gopact.Event
	for event, err := range seq {
		event = event.WithRuntimeDefaults(ids)
		events = append(events, event)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}

func childRuntimeIDs(ctx context.Context, name string) gopact.RuntimeIDs {
	parent, _ := gopact.RuntimeIDsFromContext(ctx)
	child := parent
	child.ParentCallID = parent.CallID
	child.CallID = childCallID(parent.CallID, name)
	return child
}

func childCallID(parentCallID string, name string) string {
	if parentCallID == "" {
		return "agent:" + name
	}
	return parentCallID + ":agent:" + name
}

func defaultInputSchema() gopact.JSONSchema {
	return gopact.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "task or message for the child agent",
			},
			"messages": map[string]any{
				"type":        "array",
				"description": "optional chat messages for the child agent",
			},
		},
	}
}

func copySchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	copy(out, in)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
	}
	return out
}

func dedupeArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]gopact.ArtifactRef, 0, len(in))
	for _, ref := range in {
		key := ref.ID
		if key == "" {
			key = ref.URI
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, ref)
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
