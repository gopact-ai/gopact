package channelreview

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/templates/devagent"
)

func TestReviewerReviewReturnsApprovedDecisionFromChannelAction(t *testing.T) {
	reviewer, err := New(channelWithEvents(
		gopact.ChannelEvent{
			ID:      "event-1",
			Channel: "lark",
			Type:    gopact.ChannelEventAction,
			Action: gopact.SurfaceAction{
				ID:   "approve",
				Type: gopact.SurfaceActionSubmit,
				Metadata: map[string]any{
					"reviewer": "alice",
				},
			},
			Text: "looks good",
			Metadata: map[string]any{
				"source": "lark-card",
			},
		},
	))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := devagent.Review(context.Background(), reviewer, devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Patch: devagent.PatchProposal{
			ID:      "patch-1",
			Summary: "update docs",
		},
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewApproved || decision.Reviewer != "alice" || decision.Summary != "looks good" {
		t.Fatalf("decision = %+v, want approved decision from channel action", decision)
	}
	if decision.Metadata["adapter"] != "channelreview" ||
		decision.Metadata["channel"] != "lark" ||
		decision.Metadata["event_id"] != "event-1" ||
		decision.Metadata["action_id"] != "approve" ||
		decision.Metadata["source"] != "lark-card" {
		t.Fatalf("metadata = %+v, want channel review metadata", decision.Metadata)
	}
}

func TestReviewerReviewReturnsRejectedDecisionFromPayload(t *testing.T) {
	reviewer, err := New(channelWithEvents(
		gopact.ChannelEvent{
			ID:      "event-2",
			Channel: "ci",
			Type:    gopact.ChannelEventAction,
			Action: gopact.SurfaceAction{
				ID:   "submit",
				Type: gopact.SurfaceActionSubmit,
			},
			Payload: map[string]any{
				"review_status": "rejected",
				"reviewer":      "ci-review",
				"summary":       "tests failed",
			},
		},
	), WithReviewer("fallback-reviewer"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Status != devagent.ReviewRejected || decision.Reviewer != "ci-review" || decision.Summary != "tests failed" {
		t.Fatalf("decision = %+v, want rejected decision from payload", decision)
	}
	if decision.Metadata["channel"] != "ci" || decision.Metadata["event_id"] != "event-2" {
		t.Fatalf("metadata = %+v, want event identity metadata", decision.Metadata)
	}
}

func TestReviewerReviewPreservesCanonicalMetadata(t *testing.T) {
	reviewer, err := New(channelWithEvents(
		gopact.ChannelEvent{
			ID:      "event-3",
			Channel: "lark",
			Type:    gopact.ChannelEventAction,
			Action: gopact.SurfaceAction{
				ID:   "approve",
				Type: gopact.SurfaceActionSubmit,
				Metadata: map[string]any{
					"adapter":   "spoofed-action",
					"channel":   "spoofed-action",
					"event_id":  "spoofed-action",
					"action_id": "spoofed-action",
					"reviewer":  "alice",
					"source":    "action-card",
				},
			},
			Metadata: map[string]any{
				"adapter":   "spoofed-event",
				"channel":   "spoofed-event",
				"event_id":  "spoofed-event",
				"action_id": "spoofed-event",
				"source":    "event-card",
			},
		},
	))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	if decision.Metadata["adapter"] != "channelreview" ||
		decision.Metadata["channel"] != "lark" ||
		decision.Metadata["event_id"] != "event-3" ||
		decision.Metadata["action_id"] != "approve" ||
		decision.Metadata["source"] != "action-card" {
		t.Fatalf("metadata = %+v, want canonical channel review metadata and supplemental action metadata", decision.Metadata)
	}
}

func TestReviewerReviewSendsPromptThroughTransferBeforeWaitingForDecision(t *testing.T) {
	channel := &recordingChannel{
		name: "tui",
		events: []gopact.ChannelEvent{
			{
				ID:      "event-4",
				Channel: "tui",
				Type:    gopact.ChannelEventAction,
				Action: gopact.SurfaceAction{
					ID:   "devagent.review.approve",
					Type: gopact.SurfaceActionSubmit,
				},
				Metadata: map[string]any{
					"reviewer": "terminal-user",
				},
			},
		},
	}
	var converted gopact.SurfaceMessage
	transfer := gopact.TransferFunc{
		NameValue: "test-transfer",
		Targets:   []gopact.ChannelTarget{"tui"},
		ConvertFunc: func(_ context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
			converted = msg
			return gopact.ChannelPayload{Target: "tui", Data: "review prompt"}, nil
		},
	}
	reviewer, err := New(channel, WithPrompt(transfer), WithReviewer("fallback-reviewer"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{
		Mode: devagent.ModeWrite,
		Patch: devagent.PatchProposal{
			ID:      "patch-1",
			Summary: "update docs",
		},
		Report: gopact.VerificationReport{
			IDs:    gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", UserID: "user-1"},
			Status: gopact.VerificationStatusPassed,
		},
	})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if decision.Status != devagent.ReviewApproved {
		t.Fatalf("decision = %+v, want approved decision after prompt", decision)
	}
	if len(channel.sent) != 1 || channel.sent[0].Target != "tui" || channel.sent[0].Data != "review prompt" {
		t.Fatalf("sent payloads = %+v, want one transferred prompt payload", channel.sent)
	}
	if converted.Type != gopact.SurfaceMessageApproval || converted.Target.Channel != "tui" {
		t.Fatalf("converted prompt = %+v, want approval surface for tui", converted)
	}
	if converted.ID != "devagent-review:run-1" || converted.IDs.RunID != "run-1" || converted.IDs.ThreadID != "thread-1" {
		t.Fatalf("converted prompt identity = %+v, want report runtime ids", converted)
	}
	if len(converted.Actions) != 2 ||
		converted.Actions[0].ID != "devagent.review.approve" ||
		converted.Actions[1].ID != "devagent.review.reject" {
		t.Fatalf("converted prompt actions = %+v, want approve/reject actions", converted.Actions)
	}
	text := converted.Parts[0].Text
	if !strings.Contains(text, "update docs") || !strings.Contains(text, "verification: passed") {
		t.Fatalf("converted prompt text = %q, want patch and verification summary", text)
	}
	if converted.Metadata["adapter"] != "channelreview" ||
		converted.Metadata["mode"] != "write" ||
		converted.Metadata["patch_id"] != "patch-1" ||
		converted.Metadata["report_status"] != "passed" {
		t.Fatalf("converted prompt metadata = %+v, want review metadata", converted.Metadata)
	}
}

func TestReviewerReviewPropagatesPromptSendError(t *testing.T) {
	sendErr := errors.New("send failed")
	channel := &recordingChannel{name: "tui", sendErr: sendErr}
	transfer := gopact.TransferFunc{
		NameValue: "test-transfer",
		Targets:   []gopact.ChannelTarget{"tui"},
		ConvertFunc: func(_ context.Context, _ gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
			return gopact.ChannelPayload{Target: "tui", Data: "review prompt"}, nil
		},
	}
	reviewer, err := New(channel, WithPrompt(transfer), WithReviewer("fallback-reviewer"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{})
	if !errors.Is(err, sendErr) {
		t.Fatalf("Review() error = %v, want wrapped prompt send error", err)
	}
}

func TestReviewerReviewRejectsUnsupportedPromptTransferTarget(t *testing.T) {
	channel := &recordingChannel{name: "lark"}
	transfer := gopact.TransferFunc{
		NameValue: "test-transfer",
		Targets:   []gopact.ChannelTarget{"tui"},
		ConvertFunc: func(_ context.Context, _ gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
			return gopact.ChannelPayload{}, nil
		},
	}
	reviewer, err := New(channel, WithPrompt(transfer), WithReviewer("fallback-reviewer"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{})
	if !errors.Is(err, ErrTransferUnsupported) {
		t.Fatalf("Review() error = %v, want ErrTransferUnsupported", err)
	}
}

func TestReviewerReviewSkipsUnrelatedEvents(t *testing.T) {
	reviewer, err := New(channelWithEvents(
		gopact.ChannelEvent{
			ID:      "message-1",
			Channel: "tui",
			Type:    gopact.ChannelEventMessage,
			Text:    "not a review",
		},
		gopact.ChannelEvent{
			ID:      "cancel-1",
			Channel: "tui",
			Type:    gopact.ChannelEventCancel,
		},
		gopact.ChannelEvent{
			ID:      "event-3",
			Channel: "tui",
			Type:    gopact.ChannelEventAction,
			Action: gopact.SurfaceAction{
				ID:   "devagent.review.approve",
				Type: gopact.SurfaceActionSubmit,
			},
			Metadata: map[string]any{
				"reviewer": "terminal-user",
			},
		},
	))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision, err := reviewer.Review(context.Background(), devagent.ReviewInput{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if decision.Status != devagent.ReviewApproved || decision.Reviewer != "terminal-user" {
		t.Fatalf("decision = %+v, want approval after unrelated events", decision)
	}
}

func TestReviewerReviewReturnsErrorWhenStreamEndsWithoutDecision(t *testing.T) {
	reviewer, err := New(channelWithEvents(
		gopact.ChannelEvent{
			ID:      "message-1",
			Channel: "lark",
			Type:    gopact.ChannelEventMessage,
			Text:    "hello",
		},
	), WithReviewer("alice"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("Review() error = %v, want ErrDecisionRequired", err)
	}
}

func TestReviewerReviewPropagatesEventError(t *testing.T) {
	streamErr := errors.New("callback failed")
	reviewer, err := New(channelWithEventError(streamErr))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = reviewer.Review(context.Background(), devagent.ReviewInput{})
	if !errors.Is(err, streamErr) {
		t.Fatalf("Review() error = %v, want wrapped stream error", err)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	if reviewer, err := New(nil); reviewer != nil || !errors.Is(err, ErrChannelRequired) {
		t.Fatalf("New(nil) reviewer=%v err=%v, want ErrChannelRequired", reviewer, err)
	}
	if reviewer, err := New(channelWithEvents(), WithReviewer("   ")); reviewer != nil || !errors.Is(err, ErrReviewerRequired) {
		t.Fatalf("New(empty reviewer) reviewer=%v err=%v, want ErrReviewerRequired", reviewer, err)
	}
	if reviewer, err := New(channelWithEvents(), WithActionIDs("approve", "")); reviewer != nil || !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("New(empty reject id) reviewer=%v err=%v, want ErrDecisionRequired", reviewer, err)
	}
	if reviewer, err := New(channelWithEvents(), WithPrompt(nil)); reviewer != nil || !errors.Is(err, ErrTransferRequired) {
		t.Fatalf("New(nil transfer) reviewer=%v err=%v, want ErrTransferRequired", reviewer, err)
	}
	if reviewer, err := New(channelWithEvents(), WithPromptBuilder(nil)); reviewer != nil || !errors.Is(err, ErrPromptBuilderRequired) {
		t.Fatalf("New(nil prompt builder) reviewer=%v err=%v, want ErrPromptBuilderRequired", reviewer, err)
	}
}

func channelWithEvents(events ...gopact.ChannelEvent) gopact.Channel {
	return gopact.ChannelFunc{
		NameValue: "test-channel",
		SendFunc: func(_ context.Context, _ gopact.ChannelPayload) error {
			return nil
		},
		EventsFunc: func(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
			return func(yield func(gopact.ChannelEvent, error) bool) {
				for _, event := range events {
					if err := ctx.Err(); err != nil {
						yield(gopact.ChannelEvent{}, err)
						return
					}
					if !yield(event, nil) {
						return
					}
				}
			}
		},
	}
}

func channelWithEventError(err error) gopact.Channel {
	return gopact.ChannelFunc{
		NameValue: "test-channel",
		SendFunc: func(_ context.Context, _ gopact.ChannelPayload) error {
			return nil
		},
		EventsFunc: func(_ context.Context) iter.Seq2[gopact.ChannelEvent, error] {
			return func(yield func(gopact.ChannelEvent, error) bool) {
				yield(gopact.ChannelEvent{}, err)
			}
		},
	}
}

type recordingChannel struct {
	name    string
	sent    []gopact.ChannelPayload
	events  []gopact.ChannelEvent
	sendErr error
}

func (c *recordingChannel) Name() string {
	if c.name == "" {
		return "channel"
	}
	return c.name
}

func (c *recordingChannel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, payload)
	return nil
}

func (c *recordingChannel) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		for _, event := range c.events {
			if err := ctx.Err(); err != nil {
				yield(gopact.ChannelEvent{}, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
	}
}

func (c *recordingChannel) Close(ctx context.Context) error {
	return ctx.Err()
}
