package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestPluginHostInstallsPluginMiddlewareAndSubscriber(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var order []string
	plugin := &testPlugin{name: "audit", order: &order}

	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	handlers := host.NodeMiddlewares()
	if len(handlers) != 1 {
		t.Fatalf("NodeMiddlewares() count = %d, want 1", len(handlers))
	}
	nodeCtx := NewNodeContext(ctx, NodeContextOptions{Metadata: map[string]any{"order": &order}})
	handler := ComposeNodeHandler(func(_ *NodeContext) error {
		order = append(order, "final")
		return nil
	}, handlers...)
	if err := handler(nodeCtx); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"plugin-before", "final", "plugin-after"}) {
		t.Fatalf("order = %v", order)
	}

	event := Event{Type: EventRunStarted}
	if err := host.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(plugin.events) != 1 || plugin.events[0].Type != EventRunStarted {
		t.Fatalf("plugin events = %+v", plugin.events)
	}
}

func TestPluginHostRejectsInvalidPlugin(t *testing.T) {
	host := NewPluginHost()
	if err := host.Install(context.Background(), nil); err == nil {
		t.Fatal("Install() error = nil, want nil plugin error")
	}
	if err := host.Install(context.Background(), &testPlugin{}); err == nil {
		t.Fatal("Install() error = nil, want missing plugin name error")
	}
}

func TestPluginHostRejectsDuplicatePlugin(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	plugin := &testPlugin{name: "audit"}
	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	err := host.Install(ctx, plugin)
	if !errors.Is(err, ErrPluginExists) {
		t.Fatalf("Install() error = %v, want %v", err, ErrPluginExists)
	}
}

func TestPluginHostReturnsPluginDescriptorsInInstallOrder(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	telemetry := &describedPlugin{
		name: "otel",
		descriptor: PluginDescriptor{
			Version:      "v1.0.0",
			Capabilities: []PluginCapability{PluginCapabilityTelemetry},
			Metadata:     map[string]string{"vendor": "opentelemetry"},
		},
		setup: func(ctx context.Context, host *PluginHost) error {
			host.UseEventMiddleware(func(c *EventContext) error { return c.Next() })
			host.Subscribe(func(ctx context.Context, event Event) error { return nil })
			return nil
		},
	}
	policy := &describedPlugin{
		name: "policy",
		descriptor: PluginDescriptor{
			Name:         "policy",
			Capabilities: []PluginCapability{PluginCapabilityPolicy},
		},
		setup: func(ctx context.Context, host *PluginHost) error {
			host.UseModelMiddleware(func(c *ModelContext) error { return c.Next() })
			host.UseToolMiddleware(func(c *ToolContext) error { return c.Next() })
			return nil
		},
	}

	if err := host.Install(ctx, telemetry); err != nil {
		t.Fatalf("Install(telemetry) error = %v", err)
	}
	if err := host.Install(ctx, policy); err != nil {
		t.Fatalf("Install(policy) error = %v", err)
	}

	got := host.PluginDescriptors()
	want := []PluginDescriptor{
		{
			Name:    "otel",
			Version: "v1.0.0",
			Capabilities: []PluginCapability{
				PluginCapabilityTelemetry,
				PluginCapabilityEventMiddleware,
				PluginCapabilityEventSubscriber,
			},
			Metadata: map[string]string{"vendor": "opentelemetry"},
		},
		{
			Name: "policy",
			Capabilities: []PluginCapability{
				PluginCapabilityPolicy,
				PluginCapabilityModelMiddleware,
				PluginCapabilityToolMiddleware,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PluginDescriptors() = %+v, want %+v", got, want)
	}
}

func TestPluginHostPluginDescriptorsAreCopied(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	plugin := &describedPlugin{
		name: "audit",
		descriptor: PluginDescriptor{
			Capabilities: []PluginCapability{PluginCapabilityTelemetry},
			Metadata:     map[string]string{"owner": "platform"},
		},
	}

	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	first := host.PluginDescriptors()
	first[0].Capabilities[0] = PluginCapabilityPolicy
	first[0].Metadata["owner"] = "mutated"

	second := host.PluginDescriptors()
	if second[0].Capabilities[0] != PluginCapabilityTelemetry {
		t.Fatalf("capability = %q, want %q", second[0].Capabilities[0], PluginCapabilityTelemetry)
	}
	if second[0].Metadata["owner"] != "platform" {
		t.Fatalf("metadata owner = %q, want platform", second[0].Metadata["owner"])
	}
}

func TestPluginHostRejectsInvalidPluginDescriptor(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	err := host.Install(ctx, &describedPlugin{
		name: "audit",
		descriptor: PluginDescriptor{
			Name:         "other",
			Capabilities: []PluginCapability{PluginCapabilityTelemetry},
		},
	})
	if !errors.Is(err, ErrInvalidPluginDescriptor) {
		t.Fatalf("Install() error = %v, want %v", err, ErrInvalidPluginDescriptor)
	}

	err = host.Install(ctx, &describedPlugin{
		name: "audit",
		descriptor: PluginDescriptor{
			Capabilities: []PluginCapability{""},
		},
	})
	if !errors.Is(err, ErrInvalidPluginDescriptor) {
		t.Fatalf("Install() error = %v, want %v", err, ErrInvalidPluginDescriptor)
	}
	if got := len(host.PluginDescriptors()); got != 0 {
		t.Fatalf("PluginDescriptors() count = %d, want 0", got)
	}
}

func TestPluginHostEventMiddlewareCanBeInstalledByPlugin(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	plugin := &eventPlugin{name: "events"}

	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	var events []Event
	handler := ComposeEventHandler(func(c *EventContext) error {
		events = append(events, c.Event)
		return nil
	}, host.EventMiddlewares()...)
	if err := handler(NewEventContext(ctx, Event{Type: EventRunStarted})); err != nil {
		t.Fatalf("handler() error = %v", err)
	}

	if len(events) != 1 || events[0].Node != "plugin-event" {
		t.Fatalf("events = %+v, want plugin-event node marker", events)
	}
}

func TestPluginHostPublishStrictSubscriberFailureReturnsError(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	wantErr := errors.New("subscriber failed")
	host.Subscribe(func(ctx context.Context, event Event) error {
		return wantErr
	})

	err := host.Publish(ctx, Event{Type: EventRunStarted})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want %v", err, wantErr)
	}
}

func TestPluginHostPublishFallbackContinuesAfterSubscriberFailure(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost(WithPluginFailureFallback())
	wantErr := errors.New("subscriber failed")
	var order []string
	host.Subscribe(func(ctx context.Context, event Event) error {
		order = append(order, "first")
		return wantErr
	})
	host.Subscribe(func(ctx context.Context, event Event) error {
		order = append(order, "second")
		return nil
	})

	if err := host.Publish(ctx, Event{Type: EventRunStarted}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("order = %v, want both subscribers called", order)
	}
}

func TestPluginHostModelAndToolMiddlewareCanBeInstalledByPlugin(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	plugin := &moduleMiddlewarePlugin{name: "modules"}

	if err := host.Install(ctx, plugin); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	modelHandler := ComposeModelHandler(func(c *ModelContext) error {
		c.Response = ModelResponse{Message: Message{Role: RoleAssistant, Content: c.Request.Messages[0].Text()}}
		return nil
	}, host.ModelMiddlewares()...)
	modelCtx := NewModelContext(ctx, ModelContextOptions{
		Request: ModelRequest{Messages: []Message{{Role: RoleUser, Content: "hello"}}},
	})
	if err := modelHandler(modelCtx); err != nil {
		t.Fatalf("model handler error = %v", err)
	}
	if modelCtx.Response.Message.Text() != "hello model" {
		t.Fatalf("model response = %q, want hello model", modelCtx.Response.Message.Text())
	}

	toolHandler := ComposeToolHandler(func(c *ToolContext) error {
		c.Result = ToolResult{Content: string(c.Args)}
		return nil
	}, host.ToolMiddlewares()...)
	toolCtx := NewToolContext(ctx, ToolContextOptions{
		Name: "local.echo",
		Args: json.RawMessage(`{"text":"hello"}`),
	})
	if err := toolHandler(toolCtx); err != nil {
		t.Fatalf("tool handler error = %v", err)
	}
	if toolCtx.Result.Content != `{"text":"hello"} tool` {
		t.Fatalf("tool result = %q, want tool suffix", toolCtx.Result.Content)
	}
}

func TestPluginHostCloseClosesPluginsInReverseInstallOrder(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var order []string
	first := &closePlugin{name: "first", order: &order}
	second := &closePlugin{name: "second", order: &order}

	if err := host.Install(ctx, first); err != nil {
		t.Fatalf("Install(first) error = %v", err)
	}
	if err := host.Install(ctx, second); err != nil {
		t.Fatalf("Install(second) error = %v", err)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	expected := []string{"setup:first", "setup:second", "close:second", "close:first"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
}

func TestPluginHostCloseReturnsJoinedErrors(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	firstErr := errors.New("first close failed")
	secondErr := errors.New("second close failed")

	if err := host.Install(ctx, &closePlugin{name: "first", err: firstErr}); err != nil {
		t.Fatalf("Install(first) error = %v", err)
	}
	if err := host.Install(ctx, &closePlugin{name: "second", err: secondErr}); err != nil {
		t.Fatalf("Install(second) error = %v", err)
	}

	err := host.Close(ctx)
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("Close() error = %v, want both close errors", err)
	}
}

func TestPluginHostCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	var order []string
	if err := host.Install(ctx, &closePlugin{name: "audit", order: &order}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	expected := []string{"setup:audit", "close:audit"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
}

func TestPluginHostRejectsInstallAfterClose(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := host.Install(ctx, &testPlugin{name: "audit"}); err == nil {
		t.Fatal("Install() error = nil, want closed host error")
	}
}

func TestPluginHostRollsBackSetupFailure(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	wantErr := errors.New("setup failed")

	err := host.Install(ctx, &setupFailPlugin{name: "broken", err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Install() error = %v, want %v", err, wantErr)
	}
	if got := len(host.NodeMiddlewares()); got != 0 {
		t.Fatalf("node middleware count = %d, want 0", got)
	}
	if got := len(host.EventMiddlewares()); got != 0 {
		t.Fatalf("event middleware count = %d, want 0", got)
	}
	if got := len(host.ModelMiddlewares()); got != 0 {
		t.Fatalf("model middleware count = %d, want 0", got)
	}
	if got := len(host.ToolMiddlewares()); got != 0 {
		t.Fatalf("tool middleware count = %d, want 0", got)
	}
	if got := len(host.PluginDescriptors()); got != 0 {
		t.Fatalf("plugin descriptor count = %d, want 0", got)
	}
	if err := host.Publish(ctx, Event{Type: EventRunStarted}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

type describedPlugin struct {
	name       string
	descriptor PluginDescriptor
	setup      func(context.Context, *PluginHost) error
}

func (p *describedPlugin) Name() string {
	return p.name
}

func (p *describedPlugin) Descriptor() PluginDescriptor {
	return p.descriptor
}

func (p *describedPlugin) Setup(ctx context.Context, host *PluginHost) error {
	if p.setup == nil {
		return nil
	}
	return p.setup(ctx, host)
}

func (p *describedPlugin) Close(ctx context.Context) error {
	return nil
}

type testPlugin struct {
	name   string
	events []Event
	order  *[]string
}

func (p *testPlugin) Name() string {
	return p.name
}

func (p *testPlugin) Setup(ctx context.Context, host *PluginHost) error {
	host.UseNodeMiddleware(func(c *NodeContext) error {
		order := p.order
		if order == nil {
			order = c.Metadata["order"].(*[]string)
		}
		*order = append(*order, "plugin-before")
		err := c.Next()
		*order = append(*order, "plugin-after")
		return err
	})
	host.Subscribe(func(ctx context.Context, event Event) error {
		p.events = append(p.events, event)
		return nil
	})
	return nil
}

func (p *testPlugin) Close(ctx context.Context) error {
	return nil
}

type eventPlugin struct {
	name string
}

func (p *eventPlugin) Name() string {
	return p.name
}

func (p *eventPlugin) Setup(ctx context.Context, host *PluginHost) error {
	host.UseEventMiddleware(func(c *EventContext) error {
		event := c.Event
		event.Node = "plugin-event"
		c.Event = event
		return c.Next()
	})
	return nil
}

func (p *eventPlugin) Close(ctx context.Context) error {
	return nil
}

type closePlugin struct {
	name  string
	order *[]string
	err   error
}

func (p *closePlugin) Name() string {
	return p.name
}

func (p *closePlugin) Setup(ctx context.Context, host *PluginHost) error {
	if p.order != nil {
		*p.order = append(*p.order, "setup:"+p.name)
	}
	return nil
}

func (p *closePlugin) Close(ctx context.Context) error {
	if p.order != nil {
		*p.order = append(*p.order, "close:"+p.name)
	}
	return p.err
}

type setupFailPlugin struct {
	name string
	err  error
}

func (p *setupFailPlugin) Name() string {
	return p.name
}

func (p *setupFailPlugin) Setup(ctx context.Context, host *PluginHost) error {
	host.UseNodeMiddleware(func(c *NodeContext) error { return c.Next() })
	host.UseEventMiddleware(func(c *EventContext) error { return c.Next() })
	host.UseModelMiddleware(func(c *ModelContext) error { return c.Next() })
	host.UseToolMiddleware(func(c *ToolContext) error { return c.Next() })
	host.Subscribe(func(ctx context.Context, event Event) error {
		return errors.New("subscriber should have been rolled back")
	})
	return p.err
}

func (p *setupFailPlugin) Close(ctx context.Context) error {
	return nil
}

type moduleMiddlewarePlugin struct {
	name string
}

func (p *moduleMiddlewarePlugin) Name() string {
	return p.name
}

func (p *moduleMiddlewarePlugin) Setup(ctx context.Context, host *PluginHost) error {
	host.UseModelMiddleware(func(c *ModelContext) error {
		if err := c.Next(); err != nil {
			return err
		}
		response := c.Response
		response.Message.Content = response.Message.Text() + " model"
		c.Response = response
		return nil
	})
	host.UseToolMiddleware(func(c *ToolContext) error {
		if err := c.Next(); err != nil {
			return err
		}
		result := c.Result
		result.Content += " tool"
		c.Result = result
		return nil
	})
	return nil
}

func (p *moduleMiddlewarePlugin) Close(ctx context.Context) error {
	return nil
}
