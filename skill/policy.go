package skill

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrRegistryRequired is returned when a policy wrapper is created without a registry.
	ErrRegistryRequired = errors.New("skill: registry is required")
	// ErrResourceReaderRequired is returned when a policy wrapper is created without a resource reader.
	ErrResourceReaderRequired = errors.New("skill: resource reader is required")
	// ErrScriptRunnerRequired is returned when a policy wrapper is created without a script runner.
	ErrScriptRunnerRequired = errors.New("skill: script runner is required")
	// ErrPolicyRequired is returned when a policy wrapper is created without a policy.
	ErrPolicyRequired = errors.New("skill: policy is required")
)

// PolicyKind identifies the skill operation kind being authorized.
type PolicyKind string

const (
	// PolicyKind values identify the skill operation being authorized.
	PolicyKindSkill    PolicyKind = "skill"
	PolicyKindResource PolicyKind = "resource"
	PolicyKindScript   PolicyKind = "script"
)

// PolicyInput is the stable policy input for skill operations.
type PolicyInput struct {
	Kind       PolicyKind     `json:"kind,omitempty"`
	Name       string         `json:"name,omitempty"`
	Skill      Skill          `json:"skill,omitempty"`
	Query      Query          `json:"query,omitempty"`
	Resource   Resource       `json:"resource,omitempty"`
	Script     Script         `json:"script,omitempty"`
	URI        string         `json:"uri,omitempty"`
	Args       []string       `json:"args,omitempty"`
	EnvKeys    []string       `json:"env_keys,omitempty"`
	StdinBytes int            `json:"stdin_bytes,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyOption configures a policy-wrapped skill component.
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

// PolicyRegistry authorizes skill registry operations before delegating.
type PolicyRegistry struct {
	next   *Registry
	policy gopact.Policy
	cfg    policyConfig
}

// PolicyResourceReader authorizes skill resource reads before delegating.
type PolicyResourceReader struct {
	next   ResourceReader
	policy gopact.Policy
	cfg    policyConfig
}

// PolicyScriptRunner authorizes skill script execution before delegating.
type PolicyScriptRunner struct {
	next   ScriptRunner
	policy gopact.Policy
	cfg    policyConfig
}

// NewPolicyRegistry wraps a skill registry with policy checks.
func NewPolicyRegistry(next *Registry, policy gopact.Policy, opts ...PolicyOption) (*PolicyRegistry, error) {
	if next == nil {
		return nil, ErrRegistryRequired
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
	return &PolicyRegistry{next: next, policy: policy, cfg: cfg}, nil
}

// NewPolicyResourceReader wraps a skill resource reader with policy checks.
func NewPolicyResourceReader(next ResourceReader, policy gopact.Policy, opts ...PolicyOption) (*PolicyResourceReader, error) {
	if next == nil {
		return nil, ErrResourceReaderRequired
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
	return &PolicyResourceReader{next: next, policy: policy, cfg: cfg}, nil
}

// NewPolicyScriptRunner wraps a skill script runner with policy checks.
func NewPolicyScriptRunner(next ScriptRunner, policy gopact.Policy, opts ...PolicyOption) (*PolicyScriptRunner, error) {
	if next == nil {
		return nil, ErrScriptRunnerRequired
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
	return &PolicyScriptRunner{next: next, policy: policy, cfg: cfg}, nil
}

var (
	_ ResourceReader = (*PolicyResourceReader)(nil)
	_ ScriptRunner   = (*PolicyScriptRunner)(nil)
)

// ReadResource authorizes reading a declared skill resource.
func (r *PolicyResourceReader) ReadResource(ctx context.Context, req ResourceReadRequest) (ResourceContent, error) {
	input := PolicyInput{
		Kind:     PolicyKindResource,
		Name:     req.SkillName,
		Resource: req.Resource,
		URI:      resourceKey(req.Resource),
		Metadata: copyAnyMap(req.Metadata),
	}
	if err := authorize(ctx, r.policy, r.cfg, gopact.PolicyActionRead, input); err != nil {
		return ResourceContent{}, err
	}
	return r.next.ReadResource(ctx, req)
}

// RunScript authorizes executing a declared skill script.
func (r *PolicyScriptRunner) RunScript(ctx context.Context, req ScriptRunRequest) (ScriptResult, error) {
	input := PolicyInput{
		Kind:       PolicyKindScript,
		Name:       req.SkillName,
		Script:     copyScript(req.Script),
		Args:       append([]string(nil), req.Args...),
		EnvKeys:    sortedStringKeys(req.Env),
		StdinBytes: len(req.Stdin),
		Metadata:   copyAnyMap(req.Metadata),
	}
	if err := authorize(ctx, r.policy, r.cfg, gopact.PolicyActionExec, input); err != nil {
		return ScriptResult{}, err
	}
	return r.next.RunScript(ctx, req)
}

// Register authorizes registering a skill.
func (r *PolicyRegistry) Register(ctx context.Context, skill Skill) error {
	input := PolicyInput{
		Kind:  PolicyKindSkill,
		Name:  skill.Name,
		Skill: copySkill(skill),
	}
	if err := authorize(ctx, r.policy, r.cfg, gopact.PolicyActionCreate, input); err != nil {
		return err
	}
	return r.next.Register(ctx, skill)
}

// Get authorizes reading a skill by name.
func (r *PolicyRegistry) Get(ctx context.Context, name string) (Skill, error) {
	if err := authorize(ctx, r.policy, r.cfg, gopact.PolicyActionGet, PolicyInput{Kind: PolicyKindSkill, Name: name}); err != nil {
		return Skill{}, err
	}
	return r.next.Get(ctx, name)
}

// Search authorizes searching skills.
func (r *PolicyRegistry) Search(ctx context.Context, query Query) ([]Skill, error) {
	if err := authorize(ctx, r.policy, r.cfg, gopact.PolicyActionSearch, PolicyInput{Kind: PolicyKindSkill, Query: query}); err != nil {
		return nil, err
	}
	return r.next.Search(ctx, query)
}

// Activate authorizes activating a skill by name.
func (r *PolicyRegistry) Activate(ctx context.Context, name string) (Activation, error) {
	if err := authorize(ctx, r.policy, r.cfg, gopact.PolicyActionActivate, PolicyInput{Kind: PolicyKindSkill, Name: name}); err != nil {
		return Activation{}, err
	}
	return r.next.Activate(ctx, name)
}

func authorize(ctx context.Context, policy gopact.Policy, cfg policyConfig, action gopact.PolicyRequestAction, input PolicyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := gopact.PolicyRequest{
		IDs:      cfg.ids,
		Boundary: gopact.PolicyBoundarySkill,
		Action:   action,
		Input:    copyPolicyInput(input),
		Metadata: copyAnyMap(cfg.metadata),
	}
	if err := publishPolicyEvent(ctx, cfg, gopact.NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("skill: policy: %w", err)
	}
	if err := publishPolicyEvent(ctx, cfg, gopact.NewPolicyDecidedEvent(req, decision)); err != nil {
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

func publishPolicyEvent(ctx context.Context, cfg policyConfig, event gopact.Event) error {
	if cfg.sink == nil {
		return nil
	}
	if err := cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("skill: policy event sink: %w", err)
	}
	return nil
}

func copyPolicyInput(input PolicyInput) PolicyInput {
	input.Skill = copySkill(input.Skill)
	input.Script = copyScript(input.Script)
	input.Args = append([]string(nil), input.Args...)
	input.EnvKeys = append([]string(nil), input.EnvKeys...)
	input.Metadata = copyAnyMap(input.Metadata)
	return input
}

func copyScript(script Script) Script {
	script.Command = append([]string(nil), script.Command...)
	return script
}

func sortedStringKeys(in map[string]string) []string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
