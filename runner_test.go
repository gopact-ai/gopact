package gopact

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRunnerRunDecoratesEventsWithRuntimeIDs(t *testing.T) {
	resetDefaultsForTest(t)

	runnable := fakeRunnable{
		events: []Event{
			{Type: EventRunStarted},
			{Type: EventRunCompleted, IDs: RuntimeIDs{CallID: "call-1"}},
		},
	}
	runner, err := NewRunner(runnable, WithRunnerRuntimeIDs(RuntimeIDs{AgentID: "agent-1"}))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(context.Background(), "input", WithRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].IDs.RunID != "run-1" || events[0].IDs.ThreadID != "thread-1" || events[0].IDs.AgentID != "agent-1" {
		t.Fatalf("first event IDs = %+v", events[0].IDs)
	}
	if events[1].IDs.CallID != "call-1" {
		t.Fatalf("second event CallID = %q, want call-1", events[1].IDs.CallID)
	}
}

func TestRunnerRunPassesResolvedRuntimeIDsToRunnable(t *testing.T) {
	runnable := &recordingOptionsRunnable{}
	runner, err := NewRunner(runnable, WithRunnerRuntimeIDs(RuntimeIDs{AgentID: "agent-1"}))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = collectRootEvents(runner.Run(context.Background(), "input", WithRuntimeIDs(RuntimeIDs{RunID: "run-1"})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if runnable.ids.RunID != "run-1" || runnable.ids.AgentID != "agent-1" {
		t.Fatalf("runnable IDs = %+v, want resolved run and runner ids", runnable.ids)
	}
}

func TestResolveRunOptionsReturnsRuntimeIDs(t *testing.T) {
	got := ResolveRunOptions(
		WithRuntimeIDs(RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"}),
		nil,
	)

	if got.IDs.RunID != "run-1" || got.IDs.ThreadID != "thread-1" {
		t.Fatalf("ResolveRunOptions() IDs = %+v, want run/thread ids", got.IDs)
	}
}

func TestResolveRunOptionsReturnsResumeBoundary(t *testing.T) {
	step := StepExport{
		Version: 1,
		Step: StepSnapshot{
			ID:    "run-1:2",
			Step:  2,
			Node:  "call_tool",
			Phase: StepInterrupted,
			Pending: &InterruptRecord{
				ID:   "policy:call-1",
				Type: InterruptApproval,
			},
		},
	}
	resume := ResumeRequest{
		StepID:      "run-1:2",
		InterruptID: "policy:call-1",
		Payload:     map[string]any{"approved": true},
	}

	got := ResolveRunOptions(
		WithStepExport(step),
		WithResumeRequest(resume),
	)

	if got.StepExport == nil || got.StepExport.Step.ID != "run-1:2" {
		t.Fatalf("StepExport = %+v, want run-1:2", got.StepExport)
	}
	if got.ResumeRequest == nil || got.ResumeRequest.InterruptID != "policy:call-1" {
		t.Fatalf("ResumeRequest = %+v, want policy:call-1", got.ResumeRequest)
	}
}

func TestNewRunnerRejectsNilRunnable(t *testing.T) {
	if _, err := NewRunner(nil); err == nil {
		t.Fatal("NewRunner() error = nil, want nil runnable error")
	}
}

func TestRunnerRunReportsNilRunner(t *testing.T) {
	var runner *Runner

	events, err := collectRootEvents(runner.Run(context.Background(), "input"))
	if err == nil {
		t.Fatal("Run() error = nil, want nil runner error")
	}
	if len(events) != 1 || events[0].Type != EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestRunnerRunPropagatesRunnableError(t *testing.T) {
	wantErr := errors.New("failed")
	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
		err:    wantErr,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(context.Background(), "input"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if len(events) != 2 || events[1].Type != EventRunFailed {
		t.Fatalf("events = %+v, want terminal run_failed event", events)
	}
}

func TestRunnerRunAppliesEventMiddlewareBeforeYield(t *testing.T) {
	runnable := fakeRunnable{
		events: []Event{
			{Type: EventRunStarted},
			{Type: EventRunCompleted},
		},
	}
	runner, err := NewRunner(runnable, WithRunnerEventMiddleware(func(c *EventContext) error {
		event := c.Event
		if event.Metadata == nil {
			event.Metadata = make(map[string]any)
		}
		event.Metadata["middleware"] = "seen"
		c.Event = event
		return c.Next()
	}))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(context.Background(), "input"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	for _, event := range events {
		if event.Metadata["middleware"] != "seen" {
			t.Fatalf("event metadata = %+v, want middleware marker", event.Metadata)
		}
	}
}

func TestRunnerRunEventMiddlewareCanDropEvent(t *testing.T) {
	runnable := fakeRunnable{
		events: []Event{
			{Type: EventRunStarted},
			{Type: EventModelMessage},
			{Type: EventRunCompleted},
		},
	}
	runner, err := NewRunner(runnable, WithRunnerEventMiddleware(func(c *EventContext) error {
		if c.Event.Type == EventModelMessage {
			return nil
		}
		return c.Next()
	}))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(context.Background(), "input"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := eventTypes(events)
	want := []EventType{EventRunStarted, EventRunCompleted}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestRunnerRunEventMiddlewareErrorFailsRun(t *testing.T) {
	wantErr := errors.New("event middleware failed")
	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
	}, WithRunnerEventMiddleware(func(_ *EventContext) error {
		return wantErr
	}))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(context.Background(), "input"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if len(events) != 1 || events[0].Type != EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestRunnerRunEventSinkFallbackDoesNotFailRun(t *testing.T) {
	wantErr := errors.New("sink down")
	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
	}, WithRunnerEventMiddleware(EventSinkMiddleware(func(ctx context.Context, event Event) error {
		return wantErr
	}, WithEventSinkFallback())))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(context.Background(), "input"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != EventRunStarted {
		t.Fatalf("events = %+v, want run_started", events)
	}
	if events[0].Metadata[EventMetadataEventSinkError] != wantErr.Error() {
		t.Fatalf("event metadata = %+v, want sink error", events[0].Metadata)
	}
}

func TestRunnerRunUsesPluginHostEventMiddlewareAndSubscribers(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var subscriberEvents []Event
	host.UseEventMiddleware(func(c *EventContext) error {
		event := c.Event
		event.Node = "from-middleware"
		c.Event = event
		return c.Next()
	})
	host.Subscribe(func(ctx context.Context, event Event) error {
		subscriberEvents = append(subscriberEvents, event)
		return nil
	})

	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
	}, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(ctx, "input"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 || events[0].Node != "from-middleware" {
		t.Fatalf("events = %+v, want middleware node marker", events)
	}
	if len(subscriberEvents) != 1 || subscriberEvents[0].Node != "from-middleware" {
		t.Fatalf("subscriber events = %+v, want modified event", subscriberEvents)
	}
}

func TestRunnerRunPluginSubscriberFallbackDoesNotFailRun(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("subscriber down")
	host := NewPluginHost(WithPluginFailureFallback())
	host.Subscribe(func(ctx context.Context, event Event) error {
		return wantErr
	})

	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
	}, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(ctx, "input"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != EventRunStarted {
		t.Fatalf("events = %+v, want run_started", events)
	}
	failureMessages, ok := events[0].Metadata[EventMetadataPluginSubscriberErrors].([]string)
	if !ok || !reflect.DeepEqual(failureMessages, []string{wantErr.Error()}) {
		t.Fatalf("event metadata = %+v, want subscriber error", events[0].Metadata)
	}
}

func TestRunnerCloseClosesPluginHost(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var order []string
	if err := host.Install(ctx, &closePlugin{name: "audit", order: &order}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	runner, err := NewRunner(fakeRunnable{}, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	if err := runner.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	expected := []string{"setup:audit", "close:audit"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
}

func TestRunnerCloseReturnsPluginHostError(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	wantErr := errors.New("close failed")
	if err := host.Install(ctx, &closePlugin{name: "audit", err: wantErr}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	runner, err := NewRunner(fakeRunnable{}, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	err = runner.Close(ctx)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want %v", err, wantErr)
	}
}

func TestRunnerRunFailsAfterPluginHostClosed(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	runner, err := NewRunner(fakeRunnable{
		events: []Event{{Type: EventRunStarted}},
	}, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	events, err := collectRootEvents(runner.Run(ctx, "input"))
	if err == nil {
		t.Fatal("Run() error = nil, want closed host error")
	}
	if len(events) != 1 || events[0].Type != EventRunFailed {
		t.Fatalf("events = %+v, want single run_failed event", events)
	}
}

func TestRunnerCloseWaitsForActiveRunBeforeClosingPlugins(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var order []string
	plugin := &closeNotifyPlugin{
		name:         "audit",
		order:        &order,
		closeStarted: make(chan struct{}),
	}
	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	runnable := newBlockingRunnable()
	runner, err := NewRunner(runnable, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	runDone := make(chan collectResult, 1)
	go func() {
		events, err := collectRootEvents(runner.Run(ctx, "input"))
		runDone <- collectResult{events: events, err: err}
	}()
	<-runnable.started

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- runner.Close(ctx)
	}()

	select {
	case <-plugin.closeStarted:
		t.Fatalf("plugin closed before active run finished, order = %v", order)
	case err := <-closeDone:
		t.Fatalf("Close() completed before active run finished with error %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(runnable.release)
	result := <-runDone
	if result.err != nil {
		t.Fatalf("Run() error = %v", result.err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	expected := []string{"setup:audit", "close:audit"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
}

type fakeRunnable struct {
	events []Event
	err    error
}

func (f fakeRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for _, event := range f.events {
			if !yield(event, nil) {
				return
			}
		}
		if f.err != nil {
			yield(Event{Type: EventRunFailed, Err: f.err}, f.err)
		}
	}
}

type recordingOptionsRunnable struct {
	ids RuntimeIDs
}

func (r *recordingOptionsRunnable) Run(ctx context.Context, input any, opts ...RunOption) iter.Seq2[Event, error] {
	r.ids = ResolveRunOptions(opts...).IDs
	return func(yield func(Event, error) bool) {
		if !yield(Event{Type: EventRunStarted}, nil) {
			return
		}
		yield(Event{Type: EventRunCompleted}, nil)
	}
}

func collectRootEvents(seq iter.Seq2[Event, error]) ([]Event, error) {
	var events []Event
	for event, err := range seq {
		events = append(events, event)
		if err != nil {
			return events, err
		}
	}
	return events, nil
}

func eventTypes(events []Event) []EventType {
	types := make([]EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

type blockingRunnable struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingRunnable() *blockingRunnable {
	return &blockingRunnable{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *blockingRunnable) Run(ctx context.Context, _ any, _ ...RunOption) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		r.once.Do(func() { close(r.started) })
		if !yield(Event{Type: EventRunStarted}, nil) {
			return
		}
		select {
		case <-r.release:
			yield(Event{Type: EventRunCompleted}, nil)
		case <-ctx.Done():
			yield(Event{Type: EventRunFailed, Err: ctx.Err()}, ctx.Err())
		}
	}
}

type closeNotifyPlugin struct {
	name         string
	order        *[]string
	closeStarted chan struct{}
	once         sync.Once
}

func (p *closeNotifyPlugin) Name() string {
	return p.name
}

func (p *closeNotifyPlugin) Setup(ctx context.Context, host *PluginHost) error {
	if p.order != nil {
		*p.order = append(*p.order, "setup:"+p.name)
	}
	return nil
}

func (p *closeNotifyPlugin) Close(ctx context.Context) error {
	p.once.Do(func() { close(p.closeStarted) })
	if p.order != nil {
		*p.order = append(*p.order, "close:"+p.name)
	}
	return nil
}
