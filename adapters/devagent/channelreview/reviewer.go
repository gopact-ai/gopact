// Package channelreview adapts channel actions into Dev Agent review decisions.
package channelreview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

var (
	// ErrChannelRequired is returned when a channel-backed reviewer has no channel.
	ErrChannelRequired = errors.New("channelreview: channel is required")
	// ErrDecisionRequired is returned when a channel action cannot be mapped to a review decision.
	ErrDecisionRequired = errors.New("channelreview: review decision is required")
	// ErrReviewerRequired is returned when a reviewer identity is required but missing.
	ErrReviewerRequired = errors.New("channelreview: reviewer is required")
	// ErrTransferRequired is returned when no transfer is supplied for the target channel.
	ErrTransferRequired = errors.New("channelreview: transfer is required")
	// ErrTransferUnsupported is returned when a transfer does not support the channel target.
	ErrTransferUnsupported = errors.New("channelreview: transfer does not support channel")
	// ErrPromptBuilderRequired is returned when no review prompt builder is supplied.
	ErrPromptBuilderRequired = errors.New("channelreview: prompt builder is required")
)

// PromptBuilder builds the platform-neutral review prompt sent before waiting for a decision.
type PromptBuilder func(input devagent.ReviewInput, approveID string, rejectID string) gopact.SurfaceMessage

type config struct {
	reviewer      string
	approveID     string
	rejectID      string
	transfer      gopact.Transfer
	promptBuilder PromptBuilder
}

// Option configures a channel-backed reviewer.
type Option func(*config) error

// WithReviewer sets the fallback reviewer identity when the event does not carry one.
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

// WithActionIDs sets explicit action IDs used to approve or reject a review.
func WithActionIDs(approveID string, rejectID string) Option {
	return func(cfg *config) error {
		approveID = strings.TrimSpace(approveID)
		rejectID = strings.TrimSpace(rejectID)
		if approveID == "" || rejectID == "" {
			return ErrDecisionRequired
		}
		cfg.approveID = approveID
		cfg.rejectID = rejectID
		return nil
	}
}

// WithPrompt sends a review approval prompt through transfer before waiting for channel events.
func WithPrompt(transfer gopact.Transfer) Option {
	return func(cfg *config) error {
		if transfer == nil {
			return ErrTransferRequired
		}
		cfg.transfer = transfer
		return nil
	}
}

// WithPromptBuilder replaces the default platform-neutral review prompt builder.
func WithPromptBuilder(builder PromptBuilder) Option {
	return func(cfg *config) error {
		if builder == nil {
			return ErrPromptBuilderRequired
		}
		cfg.promptBuilder = builder
		return nil
	}
}

// Reviewer consumes channel events and returns the first explicit Dev Agent review decision.
type Reviewer struct {
	channel gopact.Channel
	cfg     config
}

var _ devagent.Reviewer = (*Reviewer)(nil)

// New creates a reviewer that waits for approval or rejection events from channel.
func New(channel gopact.Channel, opts ...Option) (*Reviewer, error) {
	if channel == nil {
		return nil, ErrChannelRequired
	}
	cfg := config{
		approveID:     "devagent.review.approve",
		rejectID:      "devagent.review.reject",
		promptBuilder: BuildPrompt,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	return &Reviewer{channel: channel, cfg: cfg}, nil
}

// Review returns the first approved or rejected decision observed on the channel event stream.
func (r *Reviewer) Review(ctx context.Context, input devagent.ReviewInput) (devagent.ReviewDecision, error) {
	if r == nil || r.channel == nil {
		return devagent.ReviewDecision{}, ErrChannelRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return devagent.ReviewDecision{}, err
	}
	if err := r.sendPrompt(ctx, input); err != nil {
		return devagent.ReviewDecision{}, err
	}

	for event, err := range r.channel.Events(ctx) {
		if err != nil {
			return devagent.ReviewDecision{}, fmt.Errorf("channelreview: events: %w", err)
		}
		decision, ok, err := r.decision(event)
		if err != nil {
			return devagent.ReviewDecision{}, err
		}
		if ok {
			return decision, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return devagent.ReviewDecision{}, err
	}
	return devagent.ReviewDecision{}, ErrDecisionRequired
}

// BuildPrompt builds the default review prompt consumed by transfer adapters.
func BuildPrompt(input devagent.ReviewInput, approveID string, rejectID string) gopact.SurfaceMessage {
	ids := input.Report.IDs
	target := gopact.SurfaceTarget{}
	metadata := promptMetadata(input)
	text := promptText(input)
	return gopact.SurfaceMessage{
		ID:     promptID(input),
		IDs:    ids,
		Type:   gopact.SurfaceMessageApproval,
		Target: target,
		Parts: []gopact.SurfacePart{
			{
				Type: gopact.SurfacePartText,
				Text: text,
			},
		},
		Actions: []gopact.SurfaceAction{
			{
				ID:    approveID,
				Type:  gopact.SurfaceActionSubmit,
				Label: "Approve",
				IDs:   ids,
				Payload: map[string]any{
					"review_status": string(devagent.ReviewApproved),
				},
				Metadata: map[string]any{
					"review_status": string(devagent.ReviewApproved),
				},
			},
			{
				ID:    rejectID,
				Type:  gopact.SurfaceActionSubmit,
				Label: "Reject",
				IDs:   ids,
				Payload: map[string]any{
					"review_status": string(devagent.ReviewRejected),
				},
				Metadata: map[string]any{
					"review_status": string(devagent.ReviewRejected),
				},
			},
		},
		Metadata: metadata,
	}
}

func (r *Reviewer) sendPrompt(ctx context.Context, input devagent.ReviewInput) error {
	if r.cfg.transfer == nil {
		return nil
	}
	target := gopact.ChannelTarget(r.channel.Name())
	if target != "" && !r.cfg.transfer.Supports(target) {
		return fmt.Errorf("%w: transfer %s channel %s", ErrTransferUnsupported, r.cfg.transfer.Name(), target)
	}
	builder := r.cfg.promptBuilder
	if builder == nil {
		builder = BuildPrompt
	}
	msg := builder(input, r.cfg.approveID, r.cfg.rejectID)
	if msg.Target.Channel == "" {
		msg.Target.Channel = target
	}
	payload, err := r.cfg.transfer.Convert(ctx, msg)
	if err != nil {
		return fmt.Errorf("channelreview: convert prompt: %w", err)
	}
	if err := r.channel.Send(ctx, payload); err != nil {
		return fmt.Errorf("channelreview: send prompt: %w", err)
	}
	return nil
}

func (r *Reviewer) decision(event gopact.ChannelEvent) (devagent.ReviewDecision, bool, error) {
	if event.Type != gopact.ChannelEventAction {
		return devagent.ReviewDecision{}, false, nil
	}
	status, ok := r.reviewStatus(event)
	if !ok {
		return devagent.ReviewDecision{}, false, nil
	}
	reviewer := firstString(
		stringFromPayload(event.Payload, "reviewer", "user", "user_id"),
		stringFromMap(event.Action.Metadata, "reviewer", "user", "user_id"),
		stringFromMap(event.Metadata, "reviewer", "user", "user_id"),
		r.cfg.reviewer,
	)
	if reviewer == "" {
		return devagent.ReviewDecision{}, false, ErrReviewerRequired
	}
	return devagent.ReviewDecision{
		Status:   status,
		Reviewer: reviewer,
		Summary: firstString(
			event.Text,
			stringFromPayload(event.Payload, "summary", "reason", "message"),
			stringFromMap(event.Action.Metadata, "summary", "reason", "message"),
			stringFromMap(event.Metadata, "summary", "reason", "message"),
		),
		Metadata: r.metadata(event),
	}, true, nil
}

func promptID(input devagent.ReviewInput) string {
	if input.Report.IDs.RunID != "" {
		return "devagent-review:" + input.Report.IDs.RunID
	}
	if input.Patch.ID != "" {
		return "devagent-review:" + input.Patch.ID
	}
	return "devagent-review"
}

func promptText(input devagent.ReviewInput) string {
	lines := []string{"Dev Agent review requested"}
	if input.Mode != "" {
		lines = append(lines, "mode: "+string(input.Mode))
	}
	if input.Patch.ID != "" {
		lines = append(lines, "patch: "+input.Patch.ID)
	}
	if input.Patch.Summary != "" {
		lines = append(lines, "summary: "+input.Patch.Summary)
	}
	if input.Report.Status != "" {
		lines = append(lines, "verification: "+string(input.Report.Status))
	}
	if input.Gate.Status != "" {
		lines = append(lines, "gate: "+string(input.Gate.Status))
	}
	return strings.Join(lines, "\n")
}

func promptMetadata(input devagent.ReviewInput) map[string]any {
	metadata := map[string]any{
		"adapter": "channelreview",
	}
	if input.Mode != "" {
		metadata["mode"] = string(input.Mode)
	}
	if input.Patch.ID != "" {
		metadata["patch_id"] = input.Patch.ID
	}
	if input.Patch.Summary != "" {
		metadata["patch_summary"] = input.Patch.Summary
	}
	if input.Report.Status != "" {
		metadata["report_status"] = string(input.Report.Status)
	}
	if input.Gate.Status != "" {
		metadata["gate_status"] = string(input.Gate.Status)
	}
	return metadata
}

func (r *Reviewer) reviewStatus(event gopact.ChannelEvent) (devagent.ReviewStatus, bool) {
	if status, ok := statusFromActionID(event.Action.ID, r.cfg); ok {
		return status, true
	}
	if status, ok := statusFromString(firstString(
		stringFromPayload(event.Payload, "review_status", "status", "decision", "action"),
		stringFromMap(event.Action.Metadata, "review_status", "status", "decision", "action"),
		stringFromMap(event.Metadata, "review_status", "status", "decision", "action"),
	)); ok {
		return status, true
	}
	if status, ok := statusFromBoolPayload(event.Payload); ok {
		return status, true
	}
	return "", false
}

func statusFromActionID(id string, cfg config) (devagent.ReviewStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "approve", "approved", "accept", "accepted", strings.ToLower(cfg.approveID):
		return devagent.ReviewApproved, true
	case "reject", "rejected", "deny", "denied", strings.ToLower(cfg.rejectID):
		return devagent.ReviewRejected, true
	default:
		return "", false
	}
}

func statusFromString(raw string) (devagent.ReviewStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approve", "approved", "accept", "accepted", "yes", "allow", "allowed":
		return devagent.ReviewApproved, true
	case "reject", "rejected", "deny", "denied", "no", "block", "blocked":
		return devagent.ReviewRejected, true
	default:
		return "", false
	}
}

func statusFromBoolPayload(payload any) (devagent.ReviewStatus, bool) {
	approved, ok := boolFromPayload(payload, "approved", "approve")
	if ok && approved {
		return devagent.ReviewApproved, true
	}
	rejected, ok := boolFromPayload(payload, "rejected", "reject", "denied")
	if ok && rejected {
		return devagent.ReviewRejected, true
	}
	return "", false
}

func (r *Reviewer) metadata(event gopact.ChannelEvent) map[string]any {
	channel := string(event.Channel)
	if channel == "" && r.channel != nil {
		channel = r.channel.Name()
	}
	metadata := map[string]any{
		"adapter": "channelreview",
	}
	if channel != "" {
		metadata["channel"] = channel
	}
	if event.ID != "" {
		metadata["event_id"] = event.ID
	}
	if event.Action.ID != "" {
		metadata["action_id"] = event.Action.ID
	}
	copyInto(metadata, event.Metadata)
	copyInto(metadata, event.Action.Metadata)
	return metadata
}

func stringFromPayload(payload any, keys ...string) string {
	switch value := payload.(type) {
	case map[string]any:
		return stringFromMap(value, keys...)
	case map[string]string:
		for _, key := range keys {
			if text := strings.TrimSpace(value[key]); text != "" {
				return text
			}
		}
		return ""
	default:
		return ""
	}
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}

func boolFromPayload(payload any, keys ...string) (bool, bool) {
	values, ok := payload.(map[string]any)
	if !ok {
		return false, false
	}
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		decision, ok := value.(bool)
		if ok {
			return decision, true
		}
	}
	return false, false
}

func firstString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func copyInto(dst map[string]any, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}
