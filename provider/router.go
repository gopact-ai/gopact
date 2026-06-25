package provider

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/gopact-ai/gopact"
)

// Router routes model requests across registered providers and candidates.
type Router struct {
	registry         *Registry
	routes           RouteSet
	modelMiddlewares []gopact.ModelHandler
	pluginHost       *gopact.PluginHost
}

// RouterOption configures a provider router.
type RouterOption func(*Router) error

// NewRouter creates a provider router.
func NewRouter(registry *Registry, routes RouteSet, opts ...RouterOption) (*Router, error) {
	if registry == nil {
		return nil, errors.New("provider: registry is nil")
	}
	if err := routes.Validate(); err != nil {
		return nil, err
	}
	router := &Router{registry: registry, routes: routes}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(router); err != nil {
			return nil, err
		}
	}
	return router, nil
}

// WithModelMiddleware wraps every routed provider call.
func WithModelMiddleware(middlewares ...gopact.ModelHandler) RouterOption {
	return func(router *Router) error {
		for _, middleware := range middlewares {
			if middleware != nil {
				router.modelMiddlewares = append(router.modelMiddlewares, middleware)
			}
		}
		return nil
	}
}

// WithPluginHost attaches model middleware installed by plugins.
func WithPluginHost(host *gopact.PluginHost) RouterOption {
	return func(router *Router) error {
		if host == nil {
			return errors.New("provider: plugin host is nil")
		}
		router.pluginHost = host
		return nil
	}
}

// Validate checks route set integrity.
func (s RouteSet) Validate() error {
	if len(s.Routes) == 0 {
		return errors.New("provider: route set is empty")
	}
	seen := make(map[string]struct{}, len(s.Routes))
	for i, route := range s.Routes {
		if route.Name == "" {
			return fmt.Errorf("provider: route %d name is required", i)
		}
		if _, ok := seen[route.Name]; ok {
			return fmt.Errorf("provider: duplicate route %q", route.Name)
		}
		seen[route.Name] = struct{}{}
		if len(route.Candidates) == 0 {
			return fmt.Errorf("provider: route %q candidates are required", route.Name)
		}
		for j, candidate := range route.Candidates {
			if candidate.Provider == "" {
				return fmt.Errorf("provider: route %q candidate %d provider is required", route.Name, j)
			}
			if candidate.Model == "" {
				return fmt.Errorf("provider: route %q candidate %d model is required", route.Name, j)
			}
		}
	}
	if s.Default != "" {
		if _, ok := seen[s.Default]; !ok {
			return fmt.Errorf("provider: default route %q: %w", s.Default, ErrRouteNotFound)
		}
	}
	return nil
}

// Plan creates an auditable route plan.
func (r *Router) Plan(ctx context.Context, req RouteRequest) (RoutePlan, error) {
	if r == nil {
		return RoutePlan{}, errors.New("provider: router is nil")
	}
	route, err := r.selectRoute(req.Request.RouteHint, req.Hints.Route)
	if err != nil {
		return RoutePlan{}, err
	}

	required := append([]Capability(nil), route.Require...)
	required = append(required, req.Request.Capabilities...)
	candidates, err := r.compatibleCandidates(ctx, route.Candidates, required)
	if err != nil {
		return RoutePlan{}, err
	}
	if len(candidates) == 0 {
		return RoutePlan{}, NewError(ErrorCapabilityMismatch, errors.New("no compatible provider candidate"))
	}

	return RoutePlan{
		RouteName:     route.Name,
		Primary:       candidates[0],
		Fallbacks:     append([]Candidate(nil), candidates[1:]...),
		Reason:        "selected first compatible candidate",
		ConfigVersion: r.routes.ConfigVersion,
	}, nil
}

// Generate runs the request through the selected provider route.
func (r *Router) Generate(ctx context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error) {
	var response gopact.ModelResponse
	for event, err := range r.Stream(ctx, req) {
		if event.Type == gopact.EventPolicyRequested || event.Type == gopact.EventPolicyDecided {
			response.Events = append(response.Events, event)
		}
		if event.Type == gopact.EventModelMessage && event.Message != nil {
			response.Message = *event.Message
			if event.ModelRoute != nil {
				response.Route = *event.ModelRoute
			}
			if event.Usage != nil {
				response.Usage = *event.Usage
			}
		}
		if err != nil {
			return gopact.ModelResponse{}, err
		}
	}
	return response, nil
}

// Stream emits route and attempt events while running a model request.
func (r *Router) Stream(ctx context.Context, req gopact.ModelRequest) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		plan, route, err := r.planWithRoute(ctx, req)
		if err != nil {
			yield(modelEvent(gopact.EventModelProviderAttemptFailed, req.IDs, gopact.ModelRoute{}, nil, nil, err), err)
			return
		}

		plannedRoute := routeForCandidate(plan.RouteName, plan.Primary, 1, plan.ConfigVersion, plan.Reason)
		if !yield(modelEvent(gopact.EventModelRoutePlanned, req.IDs, plannedRoute, nil, nil, nil), nil) {
			return
		}

		candidates := append([]Candidate{plan.Primary}, plan.Fallbacks...)
		for i, candidate := range candidates {
			attempt := i + 1
			modelRoute := routeForCandidate(plan.RouteName, candidate, attempt, plan.ConfigVersion, "attempt candidate")
			if !yield(modelEvent(gopact.EventModelProviderAttemptStarted, req.IDs, modelRoute, nil, nil, nil), nil) {
				return
			}

			response, err := r.invokeCandidate(ctx, req, modelRoute, candidate)
			if err == nil {
				for _, event := range response.Events {
					if !yield(event.WithRuntimeDefaults(req.IDs), nil) {
						return
					}
				}
				if !yield(modelEvent(gopact.EventModelProviderAttemptCompleted, req.IDs, modelRoute, nil, &response.Usage, nil), nil) {
					return
				}
				message := response.Message
				if !yield(modelEvent(gopact.EventModelMessage, req.IDs, response.Route, &message, &response.Usage, nil), nil) {
					return
				}
				return
			}

			failover := decideFailover(route.Fallback, candidates, attempt, err)
			if !failover.UseFallback {
				yield(modelEvent(gopact.EventModelProviderAttemptFailed, req.IDs, modelRoute, nil, nil, err), err)
				return
			}
			if !yield(modelEvent(gopact.EventModelProviderAttemptFailed, req.IDs, modelRoute, nil, nil, err), nil) {
				return
			}
			if route.Fallback.Backoff > 0 {
				timer := time.NewTimer(route.Fallback.Backoff)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					yield(modelEvent(gopact.EventModelProviderAttemptFailed, req.IDs, modelRoute, nil, nil, ctx.Err()), ctx.Err())
					return
				case <-timer.C:
				}
			}
			nextRoute := routeForCandidate(plan.RouteName, failover.Candidate, attempt+1, plan.ConfigVersion, failover.Reason)
			if !yield(modelEvent(gopact.EventModelProviderFallbackStarted, req.IDs, nextRoute, nil, nil, nil), nil) {
				return
			}
		}
	}
}

func (r *Router) planWithRoute(ctx context.Context, req gopact.ModelRequest) (RoutePlan, Route, error) {
	route, err := r.selectRoute(req.RouteHint, "")
	if err != nil {
		return RoutePlan{}, Route{}, err
	}
	plan, err := r.Plan(ctx, RouteRequest{IDs: req.IDs, Request: req})
	return plan, route, err
}

func (r *Router) selectRoute(hints ...string) (Route, error) {
	for _, hint := range hints {
		if hint == "" {
			continue
		}
		for _, route := range r.routes.Routes {
			if route.Name == hint {
				return route, nil
			}
		}
		return Route{}, fmt.Errorf("%w: %s", ErrRouteNotFound, hint)
	}

	if r.routes.Default != "" {
		for _, route := range r.routes.Routes {
			if route.Name == r.routes.Default {
				return route, nil
			}
		}
	}
	return r.routes.Routes[0], nil
}

func (r *Router) compatibleCandidates(ctx context.Context, candidates []Candidate, required []Capability) ([]Candidate, error) {
	var compatible []Candidate
	for _, candidate := range candidates {
		provider, ok := r.registry.Resolve(ctx, candidate.Provider)
		if !ok {
			continue
		}
		models, err := provider.Models(ctx)
		if err != nil {
			return nil, fmt.Errorf("provider: list models for %q: %w", candidate.Provider, err)
		}
		if modelSupports(models, candidate.Model, required) {
			compatible = append(compatible, candidate)
		}
	}
	return compatible, nil
}

func modelSupports(models []ModelInfo, model string, required []Capability) bool {
	if len(models) == 0 {
		return len(required) == 0
	}
	for _, info := range models {
		if info.Name != model {
			continue
		}
		return len(MissingCapabilities(required, info.Capabilities)) == 0
	}
	return false
}

func (r *Router) invokeCandidate(ctx context.Context, req gopact.ModelRequest, modelRoute gopact.ModelRoute, candidate Candidate) (gopact.ModelResponse, error) {
	provider, ok := r.registry.Resolve(ctx, candidate.Provider)
	if !ok {
		return gopact.ModelResponse{}, fmt.Errorf("provider: resolve %q: %w", candidate.Provider, ErrRouteNotFound)
	}
	req.Model = candidate.Model
	modelCtx := gopact.NewModelContext(ctx, gopact.ModelContextOptions{
		Request: req,
		Route:   modelRoute,
	})
	final := func(c *gopact.ModelContext) error {
		response, err := provider.Generate(c.Context, c.Request)
		if err != nil {
			return err
		}
		c.Response = response
		return nil
	}

	handler := gopact.ComposeModelHandler(final, r.modelMiddlewareChain()...)
	err := handler(modelCtx)
	if err != nil {
		annotateProviderError(err, candidate)
		return gopact.ModelResponse{}, err
	}
	response := modelCtx.Response
	response.Route = modelRoute
	response.Events = append(response.Events, modelCtx.Events...)
	return response, nil
}

func (r *Router) modelMiddlewareChain() []gopact.ModelHandler {
	var middlewares []gopact.ModelHandler
	middlewares = append(middlewares, r.modelMiddlewares...)
	if r.pluginHost != nil {
		middlewares = append(middlewares, r.pluginHost.ModelMiddlewares()...)
	}
	return middlewares
}

func annotateProviderError(err error, candidate Candidate) {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		if providerErr.Provider == "" {
			providerErr.Provider = candidate.Provider
		}
		if providerErr.Model == "" {
			providerErr.Model = candidate.Model
		}
	}
}

func decideFailover(policy FallbackPolicy, candidates []Candidate, attempt int, err error) FailoverDecision {
	if policy.MaxAttempts > 0 && attempt >= policy.MaxAttempts {
		return FailoverDecision{Reason: "max attempts reached"}
	}
	if attempt >= len(candidates) {
		return FailoverDecision{Reason: "no fallback candidate"}
	}
	class := Classify(err)
	if len(policy.OnErrors) > 0 && !containsErrorClass(policy.OnErrors, class) {
		return FailoverDecision{Reason: "error class is not fallbackable"}
	}
	return FailoverDecision{
		UseFallback: true,
		Candidate:   candidates[attempt],
		Reason:      "fallback after " + string(class),
	}
}

func containsErrorClass(classes []ErrorClass, class ErrorClass) bool {
	for _, candidate := range classes {
		if candidate == class {
			return true
		}
	}
	return false
}

func routeForCandidate(routeName string, candidate Candidate, attempt int, configVersion string, reason string) gopact.ModelRoute {
	return gopact.ModelRoute{
		RouteName:     routeName,
		Provider:      candidate.Provider,
		Model:         candidate.Model,
		Endpoint:      candidate.Endpoint,
		Attempt:       attempt,
		ConfigVersion: configVersion,
		Reason:        reason,
		Metadata:      candidate.Metadata,
	}
}

func modelEvent(eventType gopact.EventType, ids gopact.RuntimeIDs, route gopact.ModelRoute, message *gopact.Message, usage *gopact.Usage, err error) gopact.Event {
	return gopact.Event{
		Type:       eventType,
		IDs:        ids,
		RunID:      ids.RunID,
		ThreadID:   ids.ThreadID,
		ModelRoute: &route,
		Message:    message,
		Usage:      usage,
		CreatedAt:  time.Now(),
		Err:        err,
	}
}
