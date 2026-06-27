// Package modelreview adapts a model call into a Dev Agent review decision.
package modelreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

var (
	// ErrModelRequired is returned when a model-backed reviewer has no model.
	ErrModelRequired = errors.New("modelreview: model is required")
	// ErrReviewerRequired is returned when a reviewer identity is required but missing.
	ErrReviewerRequired = errors.New("modelreview: reviewer is required")
	// ErrPromptBuilderRequired is returned when no model review prompt builder is supplied.
	ErrPromptBuilderRequired = errors.New("modelreview: prompt builder is required")
	// ErrParserRequired is returned when no model decision parser is supplied.
	ErrParserRequired = errors.New("modelreview: parser is required")
	// ErrInvalidModelDecision is returned when a model response cannot be parsed into a review decision.
	ErrInvalidModelDecision = errors.New("modelreview: invalid model decision")
	// ErrGovernanceFieldRequired is returned when required governance metadata is missing.
	ErrGovernanceFieldRequired = errors.New("modelreview: governance field is required")
	// ErrInvalidGovernanceField is returned when an unknown governance field is required.
	ErrInvalidGovernanceField = errors.New("modelreview: invalid governance field")
)

// PromptBuilder turns review input evidence into one model request.
type PromptBuilder func(input devagent.ReviewInput) (gopact.ModelRequest, error)

// Parser turns the model response message into a Dev Agent review decision.
type Parser func(message gopact.Message) (devagent.ReviewDecision, error)

// Governance identifies the prompt and evaluation policy used for a model review.
type Governance struct {
	PromptID      string
	PromptVersion string
	EvalID        string
	EvalVersion   string
	PolicyRef     string
	Metadata      map[string]any
}

// GovernanceField names model review governance metadata that can be required at construction time.
type GovernanceField string

const (
	GovernanceFieldPromptID      GovernanceField = "review_prompt_id"
	GovernanceFieldPromptVersion GovernanceField = "review_prompt_version"
	GovernanceFieldEvalID        GovernanceField = "review_eval_id"
	GovernanceFieldEvalVersion   GovernanceField = "review_eval_version"
	GovernanceFieldPolicyRef     GovernanceField = "review_policy_ref"
)

type config struct {
	reviewer                 string
	promptBuilder            PromptBuilder
	parser                   Parser
	governance               Governance
	requiredGovernanceFields []GovernanceField
}

// Option configures a model-backed reviewer.
type Option func(*config) error

// WithReviewer sets the fallback reviewer identity when the model omits reviewer.
func WithReviewer(reviewer string) Option {
	return func(cfg *config) error {
		reviewer = strings.TrimSpace(reviewer)
		if reviewer == "" {
			return ErrReviewerRequired
		}
		cfg.reviewer = reviewer
		return nil
	}
}

// WithPromptBuilder replaces the default evidence-to-model-request builder.
func WithPromptBuilder(builder PromptBuilder) Option {
	return func(cfg *config) error {
		if builder == nil {
			return ErrPromptBuilderRequired
		}
		cfg.promptBuilder = builder
		return nil
	}
}

// WithParser replaces the default JSON decision parser.
func WithParser(parser Parser) Option {
	return func(cfg *config) error {
		if parser == nil {
			return ErrParserRequired
		}
		cfg.parser = parser
		return nil
	}
}

// WithGovernance annotates model review requests and decisions with prompt/eval metadata.
func WithGovernance(governance Governance) Option {
	return func(cfg *config) error {
		cfg.governance = copyGovernance(governance)
		return nil
	}
}

// WithRequiredGovernanceFields requires governance metadata before model review can be constructed.
func WithRequiredGovernanceFields(fields ...GovernanceField) Option {
	return func(cfg *config) error {
		if len(fields) == 0 {
			return ErrGovernanceFieldRequired
		}
		required := make([]GovernanceField, 0, len(fields))
		seen := make(map[GovernanceField]struct{}, len(fields))
		for _, field := range fields {
			if !field.valid() {
				return fmt.Errorf("%w: %q", ErrInvalidGovernanceField, field)
			}
			if _, ok := seen[field]; ok {
				continue
			}
			seen[field] = struct{}{}
			required = append(required, field)
		}
		cfg.requiredGovernanceFields = required
		return nil
	}
}

// Reviewer calls a host-injected model and parses an explicit review decision from its response.
type Reviewer struct {
	model gopact.ChatModel
	cfg   config
}

var _ devagent.Reviewer = (*Reviewer)(nil)

// New creates a reviewer backed by model.
func New(model gopact.ChatModel, opts ...Option) (*Reviewer, error) {
	if model == nil {
		return nil, ErrModelRequired
	}
	cfg := config{
		reviewer:      "model-review",
		promptBuilder: BuildRequest,
		parser:        ParseJSONDecision,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if err := validateRequiredGovernance(cfg.governance, cfg.requiredGovernanceFields); err != nil {
		return nil, err
	}
	return &Reviewer{model: model, cfg: cfg}, nil
}

// Review asks the injected model to review already-collected evidence.
func (r *Reviewer) Review(ctx context.Context, input devagent.ReviewInput) (devagent.ReviewDecision, error) {
	if r == nil || r.model == nil {
		return devagent.ReviewDecision{}, ErrModelRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return devagent.ReviewDecision{}, err
	}
	builder := r.cfg.promptBuilder
	if builder == nil {
		builder = BuildRequest
	}
	request, err := builder(input)
	if err != nil {
		return devagent.ReviewDecision{}, fmt.Errorf("modelreview: build request: %w", err)
	}
	reviewGovernanceMetadata := buildGovernanceMetadata(r.cfg.governance)
	request.Metadata = mergeMetadataPreservingBase(request.Metadata, reviewGovernanceMetadata, "adapter", "purpose")
	message, err := r.model.Generate(ctx, request)
	if err != nil {
		return devagent.ReviewDecision{}, fmt.Errorf("modelreview: generate: %w", err)
	}
	parser := r.cfg.parser
	if parser == nil {
		parser = ParseJSONDecision
	}
	decision, err := parser(message)
	if err != nil {
		return devagent.ReviewDecision{}, err
	}
	if strings.TrimSpace(decision.Reviewer) == "" {
		decision.Reviewer = r.cfg.reviewer
	}
	if decision.Metadata == nil {
		decision.Metadata = map[string]any{}
	}
	if _, ok := decision.Metadata["adapter"]; !ok {
		decision.Metadata["adapter"] = "modelreview"
	}
	decision.Metadata = mergeMetadata(decision.Metadata, reviewGovernanceMetadata)
	decision.Metadata["adapter"] = "modelreview"
	return decision, nil
}

// BuildRequest builds the default review prompt for a model-backed reviewer.
func BuildRequest(input devagent.ReviewInput) (gopact.ModelRequest, error) {
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return gopact.ModelRequest{}, err
	}
	return gopact.ModelRequest{
		IDs: input.Report.IDs,
		Messages: []gopact.Message{
			{
				Role: gopact.RoleSystem,
				Content: strings.Join([]string{
					"You are a Dev Agent reviewer.",
					"Use only the supplied evidence.",
					"Return a single JSON object with status, reviewer, summary, and optional metadata.",
					"status must be either approved or rejected.",
				}, "\n"),
			},
			{
				Role:    gopact.RoleUser,
				Content: string(raw),
			},
		},
		ResponseSchema: decisionSchema(),
		Capabilities: []gopact.Capability{
			gopact.CapabilityJSONSchema,
		},
		Metadata: map[string]any{
			"adapter": "modelreview",
			"purpose": "devagent_review",
		},
	}, nil
}

// ParseJSONDecision parses a model response containing a JSON review decision.
func ParseJSONDecision(message gopact.Message) (devagent.ReviewDecision, error) {
	raw := extractJSONObject(message.Text())
	if raw == "" {
		return devagent.ReviewDecision{}, fmt.Errorf("%w: json object is required", ErrInvalidModelDecision)
	}
	var decision devagent.ReviewDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return devagent.ReviewDecision{}, fmt.Errorf("%w: %v", ErrInvalidModelDecision, err)
	}
	switch decision.Status {
	case devagent.ReviewApproved, devagent.ReviewRejected:
		return decision, nil
	default:
		return devagent.ReviewDecision{}, fmt.Errorf("%w: status %q", ErrInvalidModelDecision, decision.Status)
	}
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

func (f GovernanceField) valid() bool {
	switch f {
	case GovernanceFieldPromptID,
		GovernanceFieldPromptVersion,
		GovernanceFieldEvalID,
		GovernanceFieldEvalVersion,
		GovernanceFieldPolicyRef:
		return true
	default:
		return false
	}
}

func validateRequiredGovernance(governance Governance, required []GovernanceField) error {
	for _, field := range required {
		if strings.TrimSpace(governanceValue(governance, field)) == "" {
			return fmt.Errorf("%w: %s", ErrGovernanceFieldRequired, field)
		}
	}
	return nil
}

func governanceValue(governance Governance, field GovernanceField) string {
	switch field {
	case GovernanceFieldPromptID:
		return governance.PromptID
	case GovernanceFieldPromptVersion:
		return governance.PromptVersion
	case GovernanceFieldEvalID:
		return governance.EvalID
	case GovernanceFieldEvalVersion:
		return governance.EvalVersion
	case GovernanceFieldPolicyRef:
		return governance.PolicyRef
	default:
		return ""
	}
}

func decisionSchema() gopact.JSONSchema {
	return gopact.JSONSchema{
		"type": "object",
		"required": []string{
			"status",
		},
		"properties": map[string]any{
			"status": map[string]any{
				"type": "string",
				"enum": []string{
					string(devagent.ReviewApproved),
					string(devagent.ReviewRejected),
				},
			},
			"reviewer": map[string]any{"type": "string"},
			"summary":  map[string]any{"type": "string"},
			"metadata": map[string]any{"type": "object"},
		},
	}
}

func copyGovernance(in Governance) Governance {
	out := in
	out.Metadata = copyMetadata(in.Metadata)
	return out
}

func buildGovernanceMetadata(governance Governance) map[string]any {
	metadata := copyMetadata(governance.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	if strings.TrimSpace(governance.PromptID) != "" {
		metadata["review_prompt_id"] = strings.TrimSpace(governance.PromptID)
	}
	if strings.TrimSpace(governance.PromptVersion) != "" {
		metadata["review_prompt_version"] = strings.TrimSpace(governance.PromptVersion)
	}
	if strings.TrimSpace(governance.EvalID) != "" {
		metadata["review_eval_id"] = strings.TrimSpace(governance.EvalID)
	}
	if strings.TrimSpace(governance.EvalVersion) != "" {
		metadata["review_eval_version"] = strings.TrimSpace(governance.EvalVersion)
	}
	if strings.TrimSpace(governance.PolicyRef) != "" {
		metadata["review_policy_ref"] = strings.TrimSpace(governance.PolicyRef)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func mergeMetadata(base, extra map[string]any) map[string]any {
	metadata := copyMetadata(base)
	if metadata == nil && len(extra) > 0 {
		metadata = map[string]any{}
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return metadata
}

func mergeMetadataPreservingBase(base, extra map[string]any, preservedKeys ...string) map[string]any {
	metadata := mergeMetadata(base, nil)
	if metadata == nil && len(extra) > 0 {
		metadata = map[string]any{}
	}
	preserved := make(map[string]struct{}, len(preservedKeys))
	for _, key := range preservedKeys {
		if _, ok := base[key]; ok {
			preserved[key] = struct{}{}
		}
	}
	for key, value := range extra {
		if _, ok := preserved[key]; ok {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func copyMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
