package gopacttest

import (
	"context"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCheckTransferConformancePassesWellBehavedTransfer(t *testing.T) {
	harness := TransferConformanceHarness{
		Target: "test",
		Transfer: gopact.TransferFunc{
			NameValue: "test-transfer",
			Targets:   []gopact.ChannelTarget{"test"},
			ConvertFunc: func(_ context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
				return gopact.ChannelPayload{
					Target: "test",
					Data:   msg.ID,
				}, nil
			},
		},
		Message: gopact.SurfaceMessage{
			ID:   "surface-1",
			Type: gopact.SurfaceMessageMessage,
			Parts: []gopact.SurfacePart{
				{Type: gopact.SurfacePartText, Text: "hello"},
			},
			Metadata: map[string]any{"keep": "original"},
		},
	}

	results := CheckTransferConformance(context.Background(), harness)
	if failed := failedTransferConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckTransferConformance() failed cases: %v", failed)
	}
	RequireTransferConformance(t, harness)
}

func TestCheckTransferConformanceReportsUnsupportedTarget(t *testing.T) {
	harness := TransferConformanceHarness{
		Target: "test",
		Transfer: gopact.TransferFunc{
			NameValue: "bad-transfer",
			Targets:   []gopact.ChannelTarget{"other"},
			ConvertFunc: func(_ context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
				return gopact.ChannelPayload{Target: "test", Data: msg.ID}, nil
			},
		},
		Message: gopact.SurfaceMessage{ID: "surface-1", Type: gopact.SurfaceMessageMessage},
	}

	results := CheckTransferConformance(context.Background(), harness)
	if !hasFailedTransferConformanceCase(results, "supports-target") {
		t.Fatalf("CheckTransferConformance() did not report supports-target failure: %+v", results)
	}
}

func TestCheckTransferConformanceReportsInputMutation(t *testing.T) {
	harness := TransferConformanceHarness{
		Target:   "test",
		Transfer: mutatingTransfer{},
		Message: gopact.SurfaceMessage{
			ID:       "surface-1",
			Type:     gopact.SurfaceMessageMessage,
			Metadata: map[string]any{"keep": "original"},
		},
	}

	results := CheckTransferConformance(context.Background(), harness)
	if !hasFailedTransferConformanceCase(results, "does-not-mutate-message") {
		t.Fatalf("CheckTransferConformance() did not report mutation failure: %+v", results)
	}
}

func TestCheckChannelConformancePassesWellBehavedChannel(t *testing.T) {
	harness := ChannelConformanceHarness{
		Target: "test",
		NewChannel: func(events []gopact.ChannelEvent) (gopact.Channel, error) {
			return &channelConformanceChannel{
				name:   "test-channel",
				events: append([]gopact.ChannelEvent(nil), events...),
			}, nil
		},
		Payload: gopact.ChannelPayload{Target: "test", Data: "hello"},
		InboundEvents: []gopact.ChannelEvent{
			{
				ID:      "event-1",
				Channel: "test",
				Type:    gopact.ChannelEventMessage,
				Text:    "resume",
			},
		},
	}

	results := CheckChannelConformance(context.Background(), harness)
	if failed := failedChannelConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckChannelConformance() failed cases: %v", failed)
	}
	RequireChannelConformance(t, harness)
}

func TestCheckChannelConformanceReportsMissingInboundEvent(t *testing.T) {
	harness := ChannelConformanceHarness{
		Target: "test",
		NewChannel: func(_ []gopact.ChannelEvent) (gopact.Channel, error) {
			return &channelConformanceChannel{name: "bad-channel"}, nil
		},
		Payload: gopact.ChannelPayload{Target: "test", Data: "hello"},
		InboundEvents: []gopact.ChannelEvent{
			{
				ID:      "event-1",
				Channel: "test",
				Type:    gopact.ChannelEventMessage,
				Text:    "resume",
			},
		},
	}

	results := CheckChannelConformance(context.Background(), harness)
	if !hasFailedChannelConformanceCase(results, "streams-inbound-event") {
		t.Fatalf("CheckChannelConformance() did not report missing inbound event: %+v", results)
	}
}

func failedTransferConformanceCases(results []TransferConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedTransferConformanceCase(results []TransferConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

func failedChannelConformanceCases(results []ChannelConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedChannelConformanceCase(results []ChannelConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

type channelConformanceChannel struct {
	name     string
	events   []gopact.ChannelEvent
	sent     []gopact.ChannelPayload
	closed   bool
	sendErr  error
	closeErr error
}

func (c *channelConformanceChannel) Name() string {
	return c.name
}

func (c *channelConformanceChannel) Send(ctx context.Context, payload gopact.ChannelPayload) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, payload)
	return nil
}

func (c *channelConformanceChannel) Events(ctx context.Context) iter.Seq2[gopact.ChannelEvent, error] {
	return func(yield func(gopact.ChannelEvent, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(gopact.ChannelEvent{}, err)
			return
		}
		for _, event := range c.events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

func (c *channelConformanceChannel) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closeErr != nil {
		return c.closeErr
	}
	c.closed = true
	return nil
}

type mutatingTransfer struct{}

func (mutatingTransfer) Name() string {
	return "mutating-transfer"
}

func (mutatingTransfer) Supports(target gopact.ChannelTarget) bool {
	return target == "test"
}

func (mutatingTransfer) Convert(ctx context.Context, msg gopact.SurfaceMessage) (gopact.ChannelPayload, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ChannelPayload{}, err
	}
	msg.Metadata["keep"] = "changed"
	return gopact.ChannelPayload{Target: "test", Data: msg.ID}, nil
}
