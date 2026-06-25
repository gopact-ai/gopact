package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/gopact-ai/gopact"
)

var (
	ErrClientRequired  = errors.New("mcp: client is required")
	ErrManagerRequired = errors.New("mcp: manager is required")
	ErrPolicyRequired  = errors.New("mcp: policy is required")
)

// PolicyKind identifies the MCP operation kind being authorized.
type PolicyKind string

const (
	PolicyKindServer      PolicyKind = "server"
	PolicyKindTool        PolicyKind = "tool"
	PolicyKindTools       PolicyKind = "tools"
	PolicyKindResource    PolicyKind = "resource"
	PolicyKindResources   PolicyKind = "resources"
	PolicyKindPrompt      PolicyKind = "prompt"
	PolicyKindPrompts     PolicyKind = "prompts"
	PolicyKindSampling    PolicyKind = "sampling"
	PolicyKindElicitation PolicyKind = "elicitation"
)

// PolicyInput is the stable policy input for MCP manager and client operations.
type PolicyInput struct {
	Kind        PolicyKind          `json:"kind,omitempty"`
	Server      string              `json:"server,omitempty"`
	Servers     []string            `json:"servers,omitempty"`
	Name        string              `json:"name,omitempty"`
	URI         string              `json:"uri,omitempty"`
	Args        json.RawMessage     `json:"args,omitempty"`
	PromptArgs  map[string]any      `json:"prompt_args,omitempty"`
	Sampling    *SamplingRequest    `json:"sampling,omitempty"`
	Elicitation *ElicitationRequest `json:"elicitation,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyOption configures a policy-wrapped MCP manager or client.
type PolicyOption func(*policyConfig)

// WithPolicyIDs sets the runtime ids used in policy requests and events.
func WithPolicyIDs(ids gopact.RuntimeIDs) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.ids = ids
	}
}

// WithPolicyMetadata sets metadata copied into every policy request.
func WithPolicyMetadata(metadata map[string]any) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// WithPolicyEventSink publishes policy requested/decided events to sink.
func WithPolicyEventSink(sink gopact.EventSubscriber) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.sink = sink
	}
}

// PolicyManager authorizes MCP manager operations before delegating to Manager.
type PolicyManager struct {
	next   *Manager
	policy gopact.Policy
	cfg    policyConfig
}

// PolicyClient authorizes MCP client operations before delegating.
type PolicyClient struct {
	next   Client
	policy gopact.Policy
	cfg    policyConfig
}

// PolicySamplingHandler authorizes MCP sampling requests before delegating.
type PolicySamplingHandler struct {
	next   SamplingHandler
	policy gopact.Policy
	cfg    policyConfig
}

// PolicyElicitationHandler authorizes MCP elicitation requests before delegating.
type PolicyElicitationHandler struct {
	next   ElicitationHandler
	policy gopact.Policy
	cfg    policyConfig
}

// NewPolicyManager wraps an MCP manager with policy checks.
func NewPolicyManager(next *Manager, policy gopact.Policy, opts ...PolicyOption) (*PolicyManager, error) {
	if next == nil {
		return nil, ErrManagerRequired
	}
	if policy == nil {
		return nil, ErrPolicyRequired
	}
	cfg := policyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicyManager{next: next, policy: policy, cfg: cfg}, nil
}

// NewPolicyClient wraps an MCP client with policy checks.
func NewPolicyClient(next Client, policy gopact.Policy, opts ...PolicyOption) (*PolicyClient, error) {
	if next == nil {
		return nil, ErrClientRequired
	}
	if policy == nil {
		return nil, ErrPolicyRequired
	}
	cfg := policyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicyClient{next: next, policy: policy, cfg: cfg}, nil
}

var _ Client = (*PolicyClient)(nil)
var _ SamplingHandler = (*PolicySamplingHandler)(nil)
var _ ElicitationHandler = (*PolicyElicitationHandler)(nil)

// NewPolicySamplingHandler wraps a sampling handler with policy checks.
func NewPolicySamplingHandler(next SamplingHandler, policy gopact.Policy, opts ...PolicyOption) (*PolicySamplingHandler, error) {
	if next == nil {
		return nil, ErrSamplingHandlerRequired
	}
	if policy == nil {
		return nil, ErrPolicyRequired
	}
	cfg := policyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicySamplingHandler{next: next, policy: policy, cfg: cfg}, nil
}

// NewPolicyElicitationHandler wraps an elicitation handler with policy checks.
func NewPolicyElicitationHandler(next ElicitationHandler, policy gopact.Policy, opts ...PolicyOption) (*PolicyElicitationHandler, error) {
	if next == nil {
		return nil, ErrElicitationHandlerRequired
	}
	if policy == nil {
		return nil, ErrPolicyRequired
	}
	cfg := policyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicyElicitationHandler{next: next, policy: policy, cfg: cfg}, nil
}

// CreateMessage authorizes a server-initiated sampling request.
func (h *PolicySamplingHandler) CreateMessage(ctx context.Context, request SamplingRequest) (SamplingResponse, error) {
	ctx = normalizeContext(ctx)
	if h == nil || h.next == nil {
		return SamplingResponse{}, ErrSamplingHandlerRequired
	}
	input := PolicyInput{Kind: PolicyKindSampling}
	copied := copySamplingRequest(request)
	input.Sampling = &copied
	if err := authorize(ctx, h.policy, h.cfg, gopact.PolicyActionGenerate, input); err != nil {
		return SamplingResponse{}, err
	}
	return h.next.CreateMessage(ctx, request)
}

// Elicit authorizes a server-initiated elicitation request.
func (h *PolicyElicitationHandler) Elicit(ctx context.Context, request ElicitationRequest) (ElicitationResponse, error) {
	ctx = normalizeContext(ctx)
	if h == nil || h.next == nil {
		return ElicitationResponse{}, ErrElicitationHandlerRequired
	}
	input := PolicyInput{Kind: PolicyKindElicitation}
	copied := copyElicitationRequest(request)
	input.Elicitation = &copied
	if err := authorize(ctx, h.policy, h.cfg, gopact.PolicyActionReceive, input); err != nil {
		return ElicitationResponse{}, err
	}
	return h.next.Elicit(ctx, request)
}

// Tools authorizes listing MCP client tools.
func (c *PolicyClient) Tools(ctx context.Context) ([]ToolInfo, error) {
	ctx = normalizeContext(ctx)
	if err := c.authorize(ctx, gopact.PolicyActionList, PolicyInput{Kind: PolicyKindTools}); err != nil {
		return nil, err
	}
	return c.next.Tools(ctx)
}

// CallTool authorizes calling an MCP tool.
func (c *PolicyClient) CallTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	ctx = normalizeContext(ctx)
	input := PolicyInput{Kind: PolicyKindTool, Name: name, Args: append(json.RawMessage(nil), args...)}
	if err := c.authorize(ctx, gopact.PolicyActionInvoke, input); err != nil {
		return ToolResult{}, err
	}
	return c.next.CallTool(ctx, name, args)
}

// Resources authorizes listing MCP client resources.
func (c *PolicyClient) Resources(ctx context.Context) ([]Resource, error) {
	ctx = normalizeContext(ctx)
	if err := c.authorize(ctx, gopact.PolicyActionList, PolicyInput{Kind: PolicyKindResources}); err != nil {
		return nil, err
	}
	return c.next.Resources(ctx)
}

// ReadResource authorizes reading an MCP resource.
func (c *PolicyClient) ReadResource(ctx context.Context, uri string) (ResourceContent, error) {
	ctx = normalizeContext(ctx)
	if err := c.authorize(ctx, gopact.PolicyActionRead, PolicyInput{Kind: PolicyKindResource, URI: uri}); err != nil {
		return ResourceContent{}, err
	}
	return c.next.ReadResource(ctx, uri)
}

// Prompts authorizes listing MCP client prompts.
func (c *PolicyClient) Prompts(ctx context.Context) ([]Prompt, error) {
	ctx = normalizeContext(ctx)
	if err := c.authorize(ctx, gopact.PolicyActionList, PolicyInput{Kind: PolicyKindPrompts}); err != nil {
		return nil, err
	}
	return c.next.Prompts(ctx)
}

// GetPrompt authorizes getting an MCP prompt.
func (c *PolicyClient) GetPrompt(ctx context.Context, name string, args map[string]any) (PromptContent, error) {
	ctx = normalizeContext(ctx)
	input := PolicyInput{Kind: PolicyKindPrompt, Name: name, PromptArgs: copyAnyMap(args)}
	if err := c.authorize(ctx, gopact.PolicyActionGet, input); err != nil {
		return PromptContent{}, err
	}
	return c.next.GetPrompt(ctx, name, args)
}

// Connect authorizes connecting an MCP server.
func (m *PolicyManager) Connect(ctx context.Context, server Server) error {
	ctx = normalizeContext(ctx)
	serverName := ""
	if server != nil {
		serverName = server.Name()
	}
	input := PolicyInput{Kind: PolicyKindServer, Server: serverName}
	if err := m.authorize(ctx, gopact.PolicyActionConnect, input); err != nil {
		return err
	}
	return m.next.Connect(ctx, server)
}

// Tools authorizes listing MCP tools.
func (m *PolicyManager) Tools(ctx context.Context) ([]ToolInfo, error) {
	ctx = normalizeContext(ctx)
	input := PolicyInput{Kind: PolicyKindTools, Servers: m.serverNames()}
	if err := m.authorize(ctx, gopact.PolicyActionList, input); err != nil {
		return nil, err
	}
	return m.next.Tools(ctx)
}

// Resources authorizes listing MCP resources.
func (m *PolicyManager) Resources(ctx context.Context) ([]Resource, error) {
	ctx = normalizeContext(ctx)
	input := PolicyInput{Kind: PolicyKindResources, Servers: m.serverNames()}
	if err := m.authorize(ctx, gopact.PolicyActionList, input); err != nil {
		return nil, err
	}
	return m.next.Resources(ctx)
}

// Prompts authorizes listing MCP prompts.
func (m *PolicyManager) Prompts(ctx context.Context) ([]Prompt, error) {
	ctx = normalizeContext(ctx)
	input := PolicyInput{Kind: PolicyKindPrompts, Servers: m.serverNames()}
	if err := m.authorize(ctx, gopact.PolicyActionList, input); err != nil {
		return nil, err
	}
	return m.next.Prompts(ctx)
}

func (c *PolicyClient) authorize(ctx context.Context, action gopact.PolicyRequestAction, input PolicyInput) error {
	if c == nil || c.next == nil {
		return ErrClientRequired
	}
	if c.policy == nil {
		return ErrPolicyRequired
	}
	return authorize(ctx, c.policy, c.cfg, action, input)
}

func (m *PolicyManager) authorize(ctx context.Context, action gopact.PolicyRequestAction, input PolicyInput) error {
	if m == nil || m.next == nil {
		return ErrManagerRequired
	}
	if m.policy == nil {
		return ErrPolicyRequired
	}
	return authorize(ctx, m.policy, m.cfg, action, input)
}

func authorize(ctx context.Context, policy gopact.Policy, cfg policyConfig, action gopact.PolicyRequestAction, input PolicyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := gopact.PolicyRequest{
		IDs:      cfg.ids,
		Boundary: gopact.PolicyBoundaryMCP,
		Action:   action,
		Input:    copyPolicyInput(input),
		Metadata: copyAnyMap(cfg.metadata),
	}
	if err := publishPolicyEvent(ctx, cfg.sink, gopact.NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("mcp: policy: %w", err)
	}
	if err := publishPolicyEvent(ctx, cfg.sink, gopact.NewPolicyDecidedEvent(req, decision)); err != nil {
		return err
	}
	if decision.Action == gopact.PolicyReview {
		return gopact.NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return &gopact.PolicyDeniedError{Decision: decision, Request: req}
	}
	return nil
}

func publishPolicyEvent(ctx context.Context, sink gopact.EventSubscriber, event gopact.Event) error {
	if sink == nil {
		return nil
	}
	if err := sink(ctx, event); err != nil {
		return fmt.Errorf("mcp: policy event sink: %w", err)
	}
	return nil
}

func (m *PolicyManager) serverNames() []string {
	servers := m.next.snapshot()
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func copyPolicyInput(input PolicyInput) PolicyInput {
	input.Servers = append([]string(nil), input.Servers...)
	input.Args = append(json.RawMessage(nil), input.Args...)
	input.PromptArgs = copyAnyMap(input.PromptArgs)
	if input.Sampling != nil {
		copied := copySamplingRequest(*input.Sampling)
		input.Sampling = &copied
	}
	if input.Elicitation != nil {
		copied := copyElicitationRequest(*input.Elicitation)
		input.Elicitation = &copied
	}
	return input
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.TODO()
	}
	return ctx
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
