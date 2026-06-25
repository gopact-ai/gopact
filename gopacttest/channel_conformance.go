package gopacttest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

var (
	ErrTransferConformanceFailed = errors.New("gopacttest: transfer conformance failed")
	ErrChannelConformanceFailed  = errors.New("gopacttest: channel conformance failed")
)

// TransferConformanceHarness describes one Transfer implementation under test.
type TransferConformanceHarness struct {
	Target   gopact.ChannelTarget
	Transfer gopact.Transfer
	Message  gopact.SurfaceMessage
}

// TransferConformanceResult is the observed result for one transfer contract case.
type TransferConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// ChannelConformanceHarness describes one Channel implementation under test.
type ChannelConformanceHarness struct {
	Target        gopact.ChannelTarget
	NewChannel    func(events []gopact.ChannelEvent) (gopact.Channel, error)
	Payload       gopact.ChannelPayload
	InboundEvents []gopact.ChannelEvent
}

// ChannelConformanceResult is the observed result for one channel contract case.
type ChannelConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckTransferConformance runs reusable transfer contract cases for channel adapters.
func CheckTransferConformance(ctx context.Context, harness TransferConformanceHarness) []TransferConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	baseMessage := harness.Message
	if baseMessage.Type == "" {
		baseMessage = defaultSurfaceMessage(harness.Target)
	}

	return []TransferConformanceResult{
		checkTransferName(harness.Transfer),
		checkTransferSupportsTarget(harness.Transfer, harness.Target),
		checkTransferCanceledContext(harness.Transfer, copySurfaceMessageForConformance(baseMessage)),
		checkTransferConvertsMessage(ctx, harness.Transfer, harness.Target, copySurfaceMessageForConformance(baseMessage)),
		checkTransferDoesNotMutateMessage(ctx, harness.Transfer, copySurfaceMessageForConformance(baseMessage)),
	}
}

// RequireTransferConformance fails the test unless transfer satisfies the channel transfer contract.
func RequireTransferConformance(t testing.TB, harness TransferConformanceHarness) {
	t.Helper()

	for _, result := range CheckTransferConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("transfer conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

// CheckChannelConformance runs reusable channel contract cases for channel adapters.
func CheckChannelConformance(ctx context.Context, harness ChannelConformanceHarness) []ChannelConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	payload := harness.Payload
	if payload.Target == "" {
		payload.Target = harness.Target
	}
	if payload.Data == nil {
		payload.Data = "gopact conformance payload"
	}
	events := copyConformanceChannelEvents(harness.InboundEvents)
	if len(events) == 0 {
		events = []gopact.ChannelEvent{defaultChannelEvent(harness.Target)}
	}

	return []ChannelConformanceResult{
		checkChannelName(harness.NewChannel),
		checkChannelSend(ctx, harness.NewChannel, events, payload),
		checkChannelSendCanceledContext(harness.NewChannel, events, payload),
		checkChannelStreamsInboundEvent(ctx, harness.NewChannel, events),
		checkChannelClose(ctx, harness.NewChannel, events),
	}
}

// RequireChannelConformance fails the test unless channel satisfies the reusable channel contract.
func RequireChannelConformance(t testing.TB, harness ChannelConformanceHarness) {
	t.Helper()

	for _, result := range CheckChannelConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("channel conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkTransferName(transfer gopact.Transfer) TransferConformanceResult {
	if transfer == nil {
		return failedTransferConformance("has-name", errors.New("transfer is nil"))
	}
	if transfer.Name() == "" {
		return failedTransferConformance("has-name", errors.New("transfer name is empty"))
	}
	return passedTransferConformance("has-name")
}

func checkTransferSupportsTarget(transfer gopact.Transfer, target gopact.ChannelTarget) TransferConformanceResult {
	if transfer == nil {
		return failedTransferConformance("supports-target", errors.New("transfer is nil"))
	}
	if target == "" {
		return failedTransferConformance("supports-target", errors.New("target is empty"))
	}
	if !transfer.Supports(target) {
		return failedTransferConformance("supports-target", fmt.Errorf("transfer %q does not support target %q", transfer.Name(), target))
	}
	return passedTransferConformance("supports-target")
}

func checkTransferCanceledContext(transfer gopact.Transfer, message gopact.SurfaceMessage) TransferConformanceResult {
	if transfer == nil {
		return failedTransferConformance("respects-canceled-context", errors.New("transfer is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := transfer.Convert(ctx, message)
	if !errors.Is(err, context.Canceled) {
		return failedTransferConformance("respects-canceled-context", fmt.Errorf("Convert canceled context error = %v, want context.Canceled", err))
	}
	return passedTransferConformance("respects-canceled-context")
}

func checkTransferConvertsMessage(ctx context.Context, transfer gopact.Transfer, target gopact.ChannelTarget, message gopact.SurfaceMessage) TransferConformanceResult {
	if transfer == nil {
		return failedTransferConformance("converts-message", errors.New("transfer is nil"))
	}
	payload, err := transfer.Convert(ctx, message)
	if err != nil {
		return failedTransferConformance("converts-message", err)
	}
	if payload.Target != target {
		return failedTransferConformance("converts-message", fmt.Errorf("payload target = %q, want %q", payload.Target, target))
	}
	if payload.Data == nil {
		return failedTransferConformance("converts-message", errors.New("payload data is nil"))
	}
	return passedTransferConformance("converts-message")
}

func checkTransferDoesNotMutateMessage(ctx context.Context, transfer gopact.Transfer, message gopact.SurfaceMessage) TransferConformanceResult {
	if transfer == nil {
		return failedTransferConformance("does-not-mutate-message", errors.New("transfer is nil"))
	}
	before := copySurfaceMessageForConformance(message)
	_, err := transfer.Convert(ctx, message)
	if err != nil {
		return failedTransferConformance("does-not-mutate-message", err)
	}
	if !reflect.DeepEqual(message, before) {
		return failedTransferConformance("does-not-mutate-message", errors.New("transfer mutated input message"))
	}
	return passedTransferConformance("does-not-mutate-message")
}

func checkChannelName(newChannel func([]gopact.ChannelEvent) (gopact.Channel, error)) ChannelConformanceResult {
	channel, err := newConformanceChannel(newChannel, nil)
	if err != nil {
		return failedChannelConformance("has-name", err)
	}
	if channel.Name() == "" {
		return failedChannelConformance("has-name", errors.New("channel name is empty"))
	}
	return passedChannelConformance("has-name")
}

func checkChannelSend(ctx context.Context, newChannel func([]gopact.ChannelEvent) (gopact.Channel, error), events []gopact.ChannelEvent, payload gopact.ChannelPayload) ChannelConformanceResult {
	channel, err := newConformanceChannel(newChannel, events)
	if err != nil {
		return failedChannelConformance("sends-payload", err)
	}
	if err := channel.Send(ctx, payload); err != nil {
		return failedChannelConformance("sends-payload", err)
	}
	return passedChannelConformance("sends-payload")
}

func checkChannelSendCanceledContext(newChannel func([]gopact.ChannelEvent) (gopact.Channel, error), events []gopact.ChannelEvent, payload gopact.ChannelPayload) ChannelConformanceResult {
	channel, err := newConformanceChannel(newChannel, events)
	if err != nil {
		return failedChannelConformance("send-respects-canceled-context", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := channel.Send(ctx, payload); !errors.Is(err, context.Canceled) {
		return failedChannelConformance("send-respects-canceled-context", fmt.Errorf("Send canceled context error = %v, want context.Canceled", err))
	}
	return passedChannelConformance("send-respects-canceled-context")
}

func checkChannelStreamsInboundEvent(ctx context.Context, newChannel func([]gopact.ChannelEvent) (gopact.Channel, error), events []gopact.ChannelEvent) ChannelConformanceResult {
	channel, err := newConformanceChannel(newChannel, events)
	if err != nil {
		return failedChannelConformance("streams-inbound-event", err)
	}
	want := events[0]
	for got, err := range channel.Events(ctx) {
		if err != nil {
			return failedChannelConformance("streams-inbound-event", err)
		}
		if got.ID != want.ID || got.Type != want.Type || got.Channel != want.Channel {
			return failedChannelConformance("streams-inbound-event", fmt.Errorf("event = %s/%s/%s, want %s/%s/%s", got.ID, got.Channel, got.Type, want.ID, want.Channel, want.Type))
		}
		return passedChannelConformance("streams-inbound-event")
	}
	return failedChannelConformance("streams-inbound-event", errors.New("channel event stream ended without events"))
}

func checkChannelClose(ctx context.Context, newChannel func([]gopact.ChannelEvent) (gopact.Channel, error), events []gopact.ChannelEvent) ChannelConformanceResult {
	channel, err := newConformanceChannel(newChannel, events)
	if err != nil {
		return failedChannelConformance("closes", err)
	}
	if err := channel.Close(ctx); err != nil {
		return failedChannelConformance("closes", err)
	}
	return passedChannelConformance("closes")
}

func newConformanceChannel(newChannel func([]gopact.ChannelEvent) (gopact.Channel, error), events []gopact.ChannelEvent) (gopact.Channel, error) {
	if newChannel == nil {
		return nil, errors.New("channel factory is nil")
	}
	channel, err := newChannel(copyConformanceChannelEvents(events))
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, errors.New("channel factory returned nil")
	}
	return channel, nil
}

func passedTransferConformance(name string) TransferConformanceResult {
	return TransferConformanceResult{Case: name, Passed: true}
}

func failedTransferConformance(name string, err error) TransferConformanceResult {
	return TransferConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrTransferConformanceFailed, err),
	}
}

func passedChannelConformance(name string) ChannelConformanceResult {
	return ChannelConformanceResult{Case: name, Passed: true}
}

func failedChannelConformance(name string, err error) ChannelConformanceResult {
	return ChannelConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrChannelConformanceFailed, err),
	}
}

func defaultSurfaceMessage(target gopact.ChannelTarget) gopact.SurfaceMessage {
	return gopact.SurfaceMessage{
		ID:   "gopact-conformance-surface",
		Type: gopact.SurfaceMessageMessage,
		Target: gopact.SurfaceTarget{
			Channel: target,
		},
		Parts: []gopact.SurfacePart{
			{Type: gopact.SurfacePartText, Text: "gopact conformance"},
		},
		Metadata: map[string]any{"conformance": "transfer"},
	}
}

func defaultChannelEvent(target gopact.ChannelTarget) gopact.ChannelEvent {
	return gopact.ChannelEvent{
		ID:      "gopact-conformance-event",
		Channel: target,
		Type:    gopact.ChannelEventMessage,
		Text:    "gopact conformance",
		Metadata: map[string]any{
			"conformance": "channel",
		},
	}
}

func copySurfaceMessageForConformance(in gopact.SurfaceMessage) gopact.SurfaceMessage {
	out := in
	out.Target.Metadata = copyConformanceMap(in.Target.Metadata)
	out.Parts = append([]gopact.SurfacePart(nil), in.Parts...)
	for i := range out.Parts {
		out.Parts[i].Metadata = copyConformanceMap(in.Parts[i].Metadata)
	}
	out.Actions = append([]gopact.SurfaceAction(nil), in.Actions...)
	for i := range out.Actions {
		out.Actions[i].Metadata = copyConformanceMap(in.Actions[i].Metadata)
	}
	out.Artifacts = append([]gopact.ArtifactRef(nil), in.Artifacts...)
	out.Metadata = copyConformanceMap(in.Metadata)
	return out
}

func copyConformanceChannelEvents(in []gopact.ChannelEvent) []gopact.ChannelEvent {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ChannelEvent, len(in))
	for i, event := range in {
		out[i] = event
		out[i].Action.Metadata = copyConformanceMap(event.Action.Metadata)
		out[i].Metadata = copyConformanceMap(event.Metadata)
	}
	return out
}

func copyConformanceMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
