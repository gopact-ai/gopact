package gopact

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNodeContextNextRunsMiddlewareAroundFinalHandler(t *testing.T) {
	var order []string
	ctx := NewNodeContext(context.Background(), NodeContextOptions{
		IDs:   RuntimeIDs{RunID: "run-1", ThreadID: "thread-1", AgentID: "agent-1", UserID: "user-1", CallID: "call-1"},
		Node:  "plan",
		Input: "before",
	})

	handler := ComposeNodeHandler(
		func(c *NodeContext) error {
			order = append(order, "final")
			c.Output = "after"
			return nil
		},
		func(c *NodeContext) error {
			order = append(order, "mw-before")
			err := c.Next()
			order = append(order, "mw-after")
			return err
		},
	)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	want := []string{"mw-before", "final", "mw-after"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if ctx.Output != "after" {
		t.Fatalf("Output = %v, want after", ctx.Output)
	}
	if ctx.IDs.CallID != "call-1" || ctx.IDs.AgentID != "agent-1" || ctx.IDs.UserID != "user-1" {
		t.Fatalf("IDs = %+v", ctx.IDs)
	}
}

func TestNodeContextMiddlewareCanShortCircuit(t *testing.T) {
	calledFinal := false
	ctx := NewNodeContext(context.Background(), NodeContextOptions{Node: "guard"})

	handler := ComposeNodeHandler(
		func(_ *NodeContext) error {
			calledFinal = true
			return nil
		},
		func(c *NodeContext) error {
			c.Output = "blocked"
			return nil
		},
	)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if calledFinal {
		t.Fatal("final handler was called, want short circuit")
	}
	if ctx.Output != "blocked" {
		t.Fatalf("Output = %v, want blocked", ctx.Output)
	}
}

func TestNodeContextNextStoresError(t *testing.T) {
	wantErr := errors.New("boom")
	ctx := NewNodeContext(context.Background(), NodeContextOptions{Node: "fail"})

	handler := ComposeNodeHandler(
		func(_ *NodeContext) error {
			return wantErr
		},
		func(c *NodeContext) error {
			return c.Next()
		},
	)

	err := handler(ctx)
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler() error = %v, want %v", err, wantErr)
	}
	if !errors.Is(ctx.Err, wantErr) {
		t.Fatalf("NodeContext.Err = %v, want %v", ctx.Err, wantErr)
	}
}

func TestNodeContextNextAfterChainReturnsNil(t *testing.T) {
	ctx := NewNodeContext(context.Background(), NodeContextOptions{})
	if err := ctx.Next(); err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
}

func TestEventSinkMiddlewareStrictFailsBeforeFinalHandler(t *testing.T) {
	wantErr := errors.New("sink down")
	calledFinal := false
	handler := ComposeEventHandler(
		func(_ *EventContext) error {
			calledFinal = true
			return nil
		},
		EventSinkMiddleware(func(ctx context.Context, event Event) error {
			return wantErr
		}),
	)

	err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler() error = %v, want %v", err, wantErr)
	}
	if calledFinal {
		t.Fatal("final handler was called, want strict sink to stop the chain")
	}
}

func TestEventSinkMiddlewareFallbackAnnotatesAndContinues(t *testing.T) {
	wantErr := errors.New("sink down")
	var emitted Event
	handler := ComposeEventHandler(
		func(c *EventContext) error {
			emitted = c.Event
			return nil
		},
		EventSinkMiddleware(func(ctx context.Context, event Event) error {
			return wantErr
		}, WithEventSinkFallback()),
	)

	if err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted})); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if emitted.Metadata[EventMetadataEventSinkError] != wantErr.Error() {
		t.Fatalf("event metadata = %+v, want sink error", emitted.Metadata)
	}
}

func TestAsyncEventSinkMiddlewareExportsEventsOnClose(t *testing.T) {
	var exported []Event
	sink := NewAsyncEventSink(func(ctx context.Context, event Event) error {
		exported = append(exported, event)
		return nil
	})
	handler := ComposeEventHandler(
		func(_ *EventContext) error { return nil },
		sink.Middleware(),
	)

	if err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted, Node: "start"})); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if len(exported) != 1 || exported[0].Node != "start" {
		t.Fatalf("exported = %+v, want start event", exported)
	}
}

func TestAsyncEventSinkDropNewestAnnotatesWhenQueueFull(t *testing.T) {
	firstReceived := make(chan struct{})
	release := make(chan struct{})
	var firstOnce sync.Once
	var releaseOnce sync.Once
	sink := NewAsyncEventSink(func(ctx context.Context, event Event) error {
		firstOnce.Do(func() { close(firstReceived) })
		<-release
		return nil
	}, WithAsyncEventSinkBuffer(1), WithAsyncEventSinkDropNewest())
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		_ = sink.Close(context.Background())
	})
	var emitted []Event
	handler := ComposeEventHandler(
		func(c *EventContext) error {
			emitted = append(emitted, c.Event)
			return nil
		},
		sink.Middleware(),
	)

	if err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted, Node: "first"})); err != nil {
		t.Fatalf("first handler() error = %v", err)
	}
	select {
	case <-firstReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async sink to receive first event")
	}
	if err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted, Node: "second"})); err != nil {
		t.Fatalf("second handler() error = %v", err)
	}
	if err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted, Node: "third"})); err != nil {
		t.Fatalf("third handler() error = %v", err)
	}

	if len(emitted) != 3 {
		t.Fatalf("emitted count = %d, want 3", len(emitted))
	}
	if emitted[2].Metadata[EventMetadataAsyncEventSinkDropped] != true {
		t.Fatalf("third event metadata = %+v, want drop marker", emitted[2].Metadata)
	}
	releaseOnce.Do(func() { close(release) })
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAsyncEventSinkCloseReturnsSinkErrors(t *testing.T) {
	wantErr := errors.New("export failed")
	sink := NewAsyncEventSink(func(ctx context.Context, event Event) error {
		return wantErr
	})
	handler := ComposeEventHandler(
		func(_ *EventContext) error { return nil },
		sink.Middleware(),
	)

	if err := handler(NewEventContext(context.Background(), Event{Type: EventRunStarted})); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	err := sink.Close(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want %v", err, wantErr)
	}
}
