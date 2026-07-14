package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// Plugin registers workflow extensions during Compile. Setup does not transfer
// resource ownership; the application or adapter that created a resource must
// also close it.
type Plugin interface {
	Name() string
	Setup(context.Context, *Registry) error
}

// EventTypeValidator validates a workflow-scoped emitted event.
type EventTypeValidator func(gopact.Event) error

// Registry is the per-Compile staging registry passed to plugins.
type Registry struct {
	eventTypes        map[string]EventTypeValidator
	nodeMiddlewares   []erasedNodeMiddleware
	routeMiddlewares  []erasedRouteMiddleware
	joinMiddlewares   []erasedJoinMiddleware
	eventSinkWrappers []EventSinkWrapper
	names             map[string]struct{}
	topologyNames     []string
}

// EventSinkWrapper wraps one event sink in the delivery chain.
type EventSinkWrapper func(gopact.EventSink) gopact.EventSink

// RegisterEventType registers a custom workflow event type validator.
func (r *Registry) RegisterEventType(name string, validator EventTypeValidator) error {
	if err := r.reserveName("event type", name); err != nil {
		return err
	}
	if validator == nil {
		return fmt.Errorf("workflow: event type %q validator is nil", name)
	}
	if r.eventTypes == nil {
		r.eventTypes = map[string]EventTypeValidator{}
	}
	r.eventTypes[name] = validator
	return nil
}

// RegisterNodeMiddleware registers a typed node middleware.
func (r *Registry) RegisterNodeMiddleware[I, O any](name string, mw NodeMiddleware[I, O]) error {
	if err := r.reserveName("node middleware", name); err != nil {
		return err
	}
	if mw == nil {
		return fmt.Errorf("workflow: node middleware %q is nil", name)
	}
	r.nodeMiddlewares = append(r.nodeMiddlewares, eraseNodeMiddleware(name, mw))
	return nil
}

// RegisterRouteMiddleware registers a typed route middleware.
func (r *Registry) RegisterRouteMiddleware[I, O any](name string, mw RouteMiddleware[I, O]) error {
	if err := r.reserveName("route middleware", name); err != nil {
		return err
	}
	if mw == nil {
		return fmt.Errorf("workflow: route middleware %q is nil", name)
	}
	r.routeMiddlewares = append(r.routeMiddlewares, eraseRouteMiddleware(name, mw))
	return nil
}

// RegisterJoinMiddleware registers a typed join middleware.
func (r *Registry) RegisterJoinMiddleware[I any](name string, mw JoinMiddleware[I]) error {
	if err := r.reserveName("join middleware", name); err != nil {
		return err
	}
	if mw == nil {
		return fmt.Errorf("workflow: join middleware %q is nil", name)
	}
	r.joinMiddlewares = append(r.joinMiddlewares, eraseJoinMiddleware(name, mw))
	return nil
}

// RegisterEventSinkWrapper registers an event sink delivery wrapper.
func (r *Registry) RegisterEventSinkWrapper(name string, wrapper EventSinkWrapper) error {
	if err := r.reserveName("event sink wrapper", name); err != nil {
		return err
	}
	if wrapper == nil {
		return fmt.Errorf("workflow: event sink wrapper %q is nil", name)
	}
	r.eventSinkWrappers = append(r.eventSinkWrappers, wrapper)
	return nil
}

func (r *Registry) reserveName(kind, name string) error {
	if r == nil {
		return errors.New("workflow: registry is nil")
	}
	if name == "" {
		return fmt.Errorf("workflow: %s name is required", kind)
	}
	if r.names == nil {
		r.names = map[string]struct{}{}
	}
	key := kind + "\x00" + name
	if _, ok := r.names[key]; ok {
		return fmt.Errorf("workflow: duplicate %s %q", kind, name)
	}
	r.names[key] = struct{}{}
	r.topologyNames = append(r.topologyNames, key)
	return nil
}

// WithPlugins configures workflow plugins.
func WithPlugins(plugins ...Plugin) BuildOption {
	return buildOptionFunc(func(cfg *buildConfig) {
		cfg.plugins = append(cfg.plugins, plugins...)
	})
}

type pluginSetup struct {
	eventTypes        map[string]EventTypeValidator
	nodeMiddlewares   []erasedNodeMiddleware
	routeMiddlewares  []erasedRouteMiddleware
	joinMiddlewares   []erasedJoinMiddleware
	eventSinkWrappers []EventSinkWrapper
	topologyNames     []string
}

func setupPlugins(ctx context.Context, plugins []Plugin) (pluginSetup, error) {
	seen := map[string]struct{}{}
	registry := &Registry{eventTypes: map[string]EventTypeValidator{}, names: map[string]struct{}{}}
	for _, plugin := range plugins {
		if plugin == nil {
			return pluginSetup{}, errors.New("workflow: plugin is nil")
		}
		name := plugin.Name()
		if name == "" {
			return pluginSetup{}, errors.New("workflow: plugin name is required")
		}
		if _, ok := seen[name]; ok {
			return pluginSetup{}, fmt.Errorf("workflow: duplicate plugin %q", name)
		}
		seen[name] = struct{}{}
		if err := plugin.Setup(ctx, registry); err != nil {
			return pluginSetup{}, fmt.Errorf("workflow: setup plugin %q: %w", name, err)
		}
	}
	return pluginSetup{
		eventTypes:        copyEventTypes(registry.eventTypes),
		nodeMiddlewares:   append([]erasedNodeMiddleware(nil), registry.nodeMiddlewares...),
		routeMiddlewares:  append([]erasedRouteMiddleware(nil), registry.routeMiddlewares...),
		joinMiddlewares:   append([]erasedJoinMiddleware(nil), registry.joinMiddlewares...),
		eventSinkWrappers: append([]EventSinkWrapper(nil), registry.eventSinkWrappers...),
		topologyNames:     append([]string(nil), registry.topologyNames...),
	}, nil
}

func copyEventTypes(in map[string]EventTypeValidator) map[string]EventTypeValidator {
	out := make(map[string]EventTypeValidator, len(in))
	for name, validator := range in {
		out[name] = validator
	}
	return out
}
