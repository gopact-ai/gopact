package provider

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestRouterGenerateUsesPrimaryCandidate(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	fake := &Fake{
		NameValue: "primary",
		ModelsValue: []ModelInfo{
			{Name: "fast", Capabilities: []Capability{CapabilityToolCalling}},
		},
		Response: ResponseText("primary response"),
	}
	mustRegister(t, registry, fake)

	router := mustRouter(t, registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name: "coding",
				Candidates: []Candidate{
					{Provider: "primary", Model: "fast"},
				},
			},
		},
	})

	got, err := router.Generate(ctx, gopact.ModelRequest{
		IDs:      gopact.RuntimeIDs{RunID: "run-1"},
		Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Message.Text() != "primary response" {
		t.Fatalf("Message.Text() = %q, want primary response", got.Message.Text())
	}
	if got.Route.Provider != "primary" || got.Route.Model != "fast" || got.Route.Attempt != 1 {
		t.Fatalf("Route = %+v", got.Route)
	}
	if got.Usage.TotalTokens != 2 {
		t.Fatalf("Usage.TotalTokens = %d, want 2", got.Usage.TotalTokens)
	}
}

func TestRouterGenerateAppliesModelMiddlewareAroundProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	var order []string
	fake := &Fake{
		NameValue: "primary",
		GenerateFunc: func(_ context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error) {
			order = append(order, "provider")
			if req.Metadata["middleware"] != "before" {
				t.Fatalf("request metadata = %+v, want middleware marker", req.Metadata)
			}
			return ResponseText("provider response"), nil
		},
	}
	mustRegister(t, registry, fake)

	router, err := NewRouter(registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name: "coding",
				Candidates: []Candidate{
					{Provider: "primary", Model: "fast"},
				},
			},
		},
	}, WithModelMiddleware(func(c *ModelContext) error {
		order = append(order, "before")
		req := c.Request
		if req.Metadata == nil {
			req.Metadata = make(map[string]any)
		}
		req.Metadata["middleware"] = "before"
		c.Request = req

		if err := c.Next(); err != nil {
			return err
		}

		order = append(order, "after")
		response := c.Response
		response.Message.Content = response.Message.Text() + " after"
		c.Response = response
		return nil
	}))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	got, err := router.Generate(ctx, gopact.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if got.Message.Text() != "provider response after" {
		t.Fatalf("Message.Text() = %q, want provider response after", got.Message.Text())
	}
	if !reflect.DeepEqual(order, []string{"before", "provider", "after"}) {
		t.Fatalf("order = %v", order)
	}
}

func TestRouterGenerateModelMiddlewareCanShortCircuit(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	fake := &Fake{
		NameValue: "primary",
		GenerateFunc: func(_ context.Context, _ gopact.ModelRequest) (gopact.ModelResponse, error) {
			t.Fatal("provider should not run when middleware short-circuits")
			return gopact.ModelResponse{}, nil
		},
	}
	mustRegister(t, registry, fake)

	router, err := NewRouter(registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name: "coding",
				Candidates: []Candidate{
					{Provider: "primary", Model: "fast"},
				},
			},
		},
	}, WithModelMiddleware(func(c *ModelContext) error {
		c.Response = ResponseText("cached response")
		return nil
	}))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	got, err := router.Generate(ctx, gopact.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Message.Text() != "cached response" {
		t.Fatalf("Message.Text() = %q, want cached response", got.Message.Text())
	}
	if got.Route.Provider != "primary" || got.Route.Model != "fast" {
		t.Fatalf("Route = %+v, want primary fast", got.Route)
	}
}

func TestRouterGenerateIncludesMiddlewareEvents(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegister(t, registry, &Fake{NameValue: "primary", Response: ResponseText("response")})

	router, err := NewRouter(registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name:       "coding",
				Candidates: []Candidate{{Provider: "primary", Model: "fast"}},
			},
		},
	}, WithModelMiddleware(gopact.ModelPolicyMiddleware(gopact.PolicyFunc(func(_ context.Context, _ gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	}))))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	got, err := router.Generate(ctx, gopact.ModelRequest{IDs: gopact.RuntimeIDs{RunID: "run-1", CallID: "model-call-1"}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(got.Events) != 2 {
		t.Fatalf("event count = %d, want 2", len(got.Events))
	}
	if got.Events[0].Type != gopact.EventPolicyRequested || got.Events[1].Type != gopact.EventPolicyDecided {
		t.Fatalf("events = %+v, want policy requested/decided", got.Events)
	}
}

func TestRouterStreamModelMiddlewareErrorProducesAttemptFailedEvent(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("model middleware failed")
	registry := NewRegistry()
	mustRegister(t, registry, &Fake{NameValue: "primary", Response: ResponseText("unused")})

	router, err := NewRouter(registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name: "coding",
				Candidates: []Candidate{
					{Provider: "primary", Model: "fast"},
				},
			},
		},
	}, WithModelMiddleware(func(_ *ModelContext) error {
		return wantErr
	}))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	events, err := gopacttest.CollectEvents(router.Stream(ctx, gopact.ModelRequest{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Stream() error = %v, want %v", err, wantErr)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventModelRoutePlanned,
		gopact.EventModelProviderAttemptStarted,
		gopact.EventModelProviderAttemptFailed,
	)
}

func TestRouterGenerateUsesPluginHostModelMiddleware(t *testing.T) {
	ctx := context.Background()
	host := gopact.NewPluginHost()
	host.UseModelMiddleware(func(c *gopact.ModelContext) error {
		if err := c.Next(); err != nil {
			return err
		}
		response := c.Response
		response.Message.Content = response.Message.Text() + " from plugin"
		c.Response = response
		return nil
	})

	registry := NewRegistry()
	mustRegister(t, registry, &Fake{NameValue: "primary", Response: ResponseText("response")})
	router, err := NewRouter(registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name:       "coding",
				Candidates: []Candidate{{Provider: "primary", Model: "fast"}},
			},
		},
	}, WithPluginHost(host))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	got, err := router.Generate(ctx, gopact.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Message.Text() != "response from plugin" {
		t.Fatalf("Message.Text() = %q, want response from plugin", got.Message.Text())
	}
}

func TestRouterStreamEmitsRouteAndAttemptEvents(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegister(t, registry, &Fake{
		NameValue:     "primary",
		GenerateError: NewError(ErrorRateLimited, errors.New("rate limited")),
	})
	mustRegister(t, registry, &Fake{
		NameValue: "fallback",
		Response:  ResponseText("fallback response"),
	})

	router := mustRouter(t, registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name: "coding",
				Candidates: []Candidate{
					{Provider: "primary", Model: "fast"},
					{Provider: "fallback", Model: "steady"},
				},
				Fallback: FallbackPolicy{
					OnErrors:    []ErrorClass{ErrorRateLimited},
					MaxAttempts: 2,
				},
			},
		},
	})

	events, err := gopacttest.CollectEvents(router.Stream(ctx, gopact.ModelRequest{
		IDs: gopact.RuntimeIDs{RunID: "run-1", ThreadID: "thread-1"},
	}))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	gopacttest.RequireEventTypes(t, events,
		gopact.EventModelRoutePlanned,
		gopact.EventModelProviderAttemptStarted,
		gopact.EventModelProviderAttemptFailed,
		gopact.EventModelProviderFallbackStarted,
		gopact.EventModelProviderAttemptStarted,
		gopact.EventModelProviderAttemptCompleted,
		gopact.EventModelMessage,
	)
	if events[0].ModelRoute.Provider != "primary" || events[3].ModelRoute.Provider != "fallback" {
		t.Fatalf("route events = %+v / %+v", events[0].ModelRoute, events[3].ModelRoute)
	}
	if events[0].CreatedAt.IsZero() {
		t.Fatal("route event CreatedAt is zero")
	}
	if events[len(events)-1].Message.Text() != "fallback response" {
		t.Fatalf("final message = %+v", events[len(events)-1].Message)
	}
}

func TestRouterDoesNotFallbackForNonFallbackableError(t *testing.T) {
	ctx := context.Background()
	wantErr := NewError(ErrorInvalidRequest, errors.New("bad request"))
	registry := NewRegistry()
	mustRegister(t, registry, &Fake{NameValue: "primary", GenerateError: wantErr})
	mustRegister(t, registry, &Fake{NameValue: "fallback", Response: ResponseText("unused")})

	router := mustRouter(t, registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name: "coding",
				Candidates: []Candidate{
					{Provider: "primary", Model: "fast"},
					{Provider: "fallback", Model: "steady"},
				},
				Fallback: FallbackPolicy{
					OnErrors:    []ErrorClass{ErrorRateLimited},
					MaxAttempts: 2,
				},
			},
		},
	})

	events, err := gopacttest.CollectEvents(router.Stream(ctx, gopact.ModelRequest{}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Stream() error = %v, want %v", err, wantErr)
	}
	gopacttest.RequireEventTypes(t, events,
		gopact.EventModelRoutePlanned,
		gopact.EventModelProviderAttemptStarted,
		gopact.EventModelProviderAttemptFailed,
	)
}

func TestRouterSkipsCandidateWithoutRequiredCapabilities(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	mustRegister(t, registry, &Fake{
		NameValue: "plain",
		ModelsValue: []ModelInfo{
			{Name: "small"},
		},
		Response: ResponseText("plain"),
	})
	mustRegister(t, registry, &Fake{
		NameValue: "tooling",
		ModelsValue: []ModelInfo{
			{Name: "tools", Capabilities: []Capability{CapabilityToolCalling}},
		},
		Response: ResponseText("tool response"),
	})

	router := mustRouter(t, registry, RouteSet{
		Default: "coding",
		Routes: []Route{
			{
				Name:    "coding",
				Require: []Capability{CapabilityToolCalling},
				Candidates: []Candidate{
					{Provider: "plain", Model: "small"},
					{Provider: "tooling", Model: "tools"},
				},
			},
		},
	})

	got, err := router.Generate(ctx, gopact.ModelRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Route.Provider != "tooling" {
		t.Fatalf("Route.Provider = %q, want tooling", got.Route.Provider)
	}
}

func TestRouterReturnsErrorWhenNoRouteMatches(t *testing.T) {
	router := mustRouter(t, NewRegistry(), RouteSet{
		Routes: []Route{{Name: "known", Candidates: []Candidate{{Provider: "missing", Model: "x"}}}},
	})

	_, err := router.Generate(context.Background(), gopact.ModelRequest{RouteHint: "missing"})
	if !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("Generate() error = %v, want %v", err, ErrRouteNotFound)
	}
}

func TestRouteSetValidateRejectsInvalidRoutes(t *testing.T) {
	tests := []struct {
		name   string
		routes RouteSet
	}{
		{name: "empty", routes: RouteSet{}},
		{name: "missing route name", routes: RouteSet{Routes: []Route{{Candidates: []Candidate{{Provider: "p", Model: "m"}}}}}},
		{name: "missing candidates", routes: RouteSet{Routes: []Route{{Name: "coding"}}}},
		{name: "missing provider", routes: RouteSet{Routes: []Route{{Name: "coding", Candidates: []Candidate{{Model: "m"}}}}}},
		{name: "missing model", routes: RouteSet{Routes: []Route{{Name: "coding", Candidates: []Candidate{{Provider: "p"}}}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.routes.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestCapabilityHelpers(t *testing.T) {
	got := MissingCapabilities(
		[]Capability{CapabilityToolCalling, CapabilityStreaming},
		[]Capability{CapabilityToolCalling},
	)
	want := []Capability{CapabilityStreaming}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingCapabilities() = %v, want %v", got, want)
	}
}

func mustRegister(t *testing.T, registry *Registry, provider Provider) {
	t.Helper()

	if err := registry.Register(context.Background(), provider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

func mustRouter(t *testing.T, registry *Registry, routes RouteSet) *Router {
	t.Helper()

	router, err := NewRouter(registry, routes)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func modelRequest(model string) gopact.ModelRequest {
	return gopact.ModelRequest{Model: model}
}
