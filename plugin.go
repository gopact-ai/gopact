package gopact

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrPluginExists            = errors.New("gopact: plugin already exists")
	ErrPluginHostClosed        = errors.New("gopact: plugin host closed")
	ErrInvalidPluginDescriptor = errors.New("gopact: invalid plugin descriptor")
)

const (
	// EventMetadataPluginSubscriberErrors records non-fatal subscriber errors on fallback.
	EventMetadataPluginSubscriberErrors = "plugin_subscriber_errors"
)

// Plugin installs cross-cutting runtime behavior.
type Plugin interface {
	Name() string
	Setup(ctx context.Context, host *PluginHost) error
	Close(ctx context.Context) error
}

// PluginHostOption configures a plugin host.
type PluginHostOption func(*PluginHost)

// PluginFailurePolicy controls non-lifecycle plugin callback failures.
type PluginFailurePolicy string

const (
	PluginFailureStrict   PluginFailurePolicy = "strict"
	PluginFailureFallback PluginFailurePolicy = "fallback"
)

// WithPluginFailurePolicy sets the host failure policy for event subscribers.
func WithPluginFailurePolicy(policy PluginFailurePolicy) PluginHostOption {
	return func(host *PluginHost) {
		switch policy {
		case PluginFailureStrict, PluginFailureFallback:
			host.failurePolicy = policy
		}
	}
}

// WithPluginFailureStrict makes subscriber failures stop event publication.
func WithPluginFailureStrict() PluginHostOption {
	return WithPluginFailurePolicy(PluginFailureStrict)
}

// WithPluginFailureFallback records subscriber failures on event metadata and keeps publishing.
func WithPluginFailureFallback() PluginHostOption {
	return WithPluginFailurePolicy(PluginFailureFallback)
}

// PluginDescriber optionally lets a plugin declare its stable capabilities.
type PluginDescriber interface {
	Descriptor() PluginDescriptor
}

// PluginCapability describes a host-visible capability provided by a plugin.
type PluginCapability string

const (
	PluginCapabilityNodeMiddleware  PluginCapability = "node_middleware"
	PluginCapabilityEventMiddleware PluginCapability = "event_middleware"
	PluginCapabilityModelMiddleware PluginCapability = "model_middleware"
	PluginCapabilityToolMiddleware  PluginCapability = "tool_middleware"
	PluginCapabilityEventSubscriber PluginCapability = "event_subscriber"
	PluginCapabilityTelemetry       PluginCapability = "telemetry"
	PluginCapabilityPolicy          PluginCapability = "policy"
	PluginCapabilityReplay          PluginCapability = "replay"
	PluginCapabilityTransfer        PluginCapability = "transfer"
	PluginCapabilityChannel         PluginCapability = "channel"
	PluginCapabilityEvaluation      PluginCapability = "evaluation"
	PluginCapabilityApproval        PluginCapability = "approval"
)

// PluginDescriptor is stable metadata the host exposes for an installed plugin.
type PluginDescriptor struct {
	Name         string             `json:"name"`
	Version      string             `json:"version,omitempty"`
	Capabilities []PluginCapability `json:"capabilities,omitempty"`
	Metadata     map[string]string  `json:"metadata,omitempty"`
}

// Validate checks whether a plugin descriptor can be exposed by a host.
func (d PluginDescriptor) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidPluginDescriptor)
	}
	for _, capability := range d.Capabilities {
		if capability == "" {
			return fmt.Errorf("%w: capability is required", ErrInvalidPluginDescriptor)
		}
	}
	return nil
}

// HasCapability reports whether the descriptor declares a capability.
func (d PluginDescriptor) HasCapability(capability PluginCapability) bool {
	for _, declared := range d.Capabilities {
		if declared == capability {
			return true
		}
	}
	return false
}

// EventSubscriber receives runtime events.
type EventSubscriber func(ctx context.Context, event Event) error

// PluginHost stores middleware and event subscribers installed by plugins.
type PluginHost struct {
	mu                sync.RWMutex
	lifecycleMu       sync.Mutex
	isClosing         bool
	isClosed          bool
	activeRuns        int
	idle              chan struct{}
	closeDone         chan struct{}
	closeErr          error
	failurePolicy     PluginFailurePolicy
	plugins           map[string]Plugin
	pluginDescriptors map[string]PluginDescriptor
	pluginOrder       []string
	nodeMiddlewares   []NodeHandler
	eventMiddlewares  []EventHandler
	modelMiddlewares  []ModelHandler
	toolMiddlewares   []ToolHandler
	subscribers       []EventSubscriber
}

// NewPluginHost creates an empty plugin host.
func NewPluginHost(opts ...PluginHostOption) *PluginHost {
	host := &PluginHost{
		failurePolicy:     PluginFailureStrict,
		plugins:           make(map[string]Plugin),
		pluginDescriptors: make(map[string]PluginDescriptor),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(host)
		}
	}
	return host
}

// Install installs a plugin once.
func (h *PluginHost) Install(ctx context.Context, plugin Plugin) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if plugin == nil {
		return errors.New("gopact: plugin is nil")
	}
	name := plugin.Name()
	if name == "" {
		return errors.New("gopact: plugin name is required")
	}
	descriptor, err := describePlugin(plugin)
	if err != nil {
		return err
	}

	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.isClosing || h.isClosed {
		return ErrPluginHostClosed
	}

	h.mu.Lock()
	if h.plugins == nil {
		h.plugins = make(map[string]Plugin)
	}
	if h.pluginDescriptors == nil {
		h.pluginDescriptors = make(map[string]PluginDescriptor)
	}
	if _, ok := h.plugins[name]; ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrPluginExists, name)
	}
	h.plugins[name] = plugin
	h.pluginOrder = append(h.pluginOrder, name)
	nodeLen := len(h.nodeMiddlewares)
	eventLen := len(h.eventMiddlewares)
	modelLen := len(h.modelMiddlewares)
	toolLen := len(h.toolMiddlewares)
	subscriberLen := len(h.subscribers)
	h.mu.Unlock()

	if err := plugin.Setup(ctx, h); err != nil {
		h.mu.Lock()
		delete(h.plugins, name)
		delete(h.pluginDescriptors, name)
		h.pluginOrder = removeString(h.pluginOrder, name)
		h.nodeMiddlewares = h.nodeMiddlewares[:nodeLen]
		h.eventMiddlewares = h.eventMiddlewares[:eventLen]
		h.modelMiddlewares = h.modelMiddlewares[:modelLen]
		h.toolMiddlewares = h.toolMiddlewares[:toolLen]
		h.subscribers = h.subscribers[:subscriberLen]
		h.mu.Unlock()
		return err
	}
	h.mu.Lock()
	descriptor.Capabilities = mergePluginCapabilities(descriptor.Capabilities, inferPluginCapabilities(
		len(h.nodeMiddlewares) > nodeLen,
		len(h.eventMiddlewares) > eventLen,
		len(h.modelMiddlewares) > modelLen,
		len(h.toolMiddlewares) > toolLen,
		len(h.subscribers) > subscriberLen,
	)...)
	h.pluginDescriptors[name] = descriptor
	h.mu.Unlock()
	return nil
}

// Close closes installed plugins in reverse install order.
func (h *PluginHost) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if wait, ok, err := h.startClose(ctx); !ok {
		if wait == nil {
			return err
		}
		return h.waitClosed(ctx, wait)
	}

	h.mu.RLock()
	plugins := make([]Plugin, 0, len(h.pluginOrder))
	for i := len(h.pluginOrder) - 1; i >= 0; i-- {
		if plugin := h.plugins[h.pluginOrder[i]]; plugin != nil {
			plugins = append(plugins, plugin)
		}
	}
	h.mu.RUnlock()

	var errs []error
	for _, plugin := range plugins {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := plugin.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("gopact: close plugin %q: %w", plugin.Name(), err))
		}
	}

	h.mu.Lock()
	h.plugins = make(map[string]Plugin)
	h.pluginDescriptors = make(map[string]PluginDescriptor)
	h.pluginOrder = nil
	h.nodeMiddlewares = nil
	h.eventMiddlewares = nil
	h.modelMiddlewares = nil
	h.toolMiddlewares = nil
	h.subscribers = nil
	h.mu.Unlock()

	err := errors.Join(errs...)
	h.finishClose(err)
	return err
}

// PluginDescriptors returns descriptors for installed plugins in install order.
func (h *PluginHost) PluginDescriptors() []PluginDescriptor {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	descriptors := make([]PluginDescriptor, 0, len(h.pluginOrder))
	for _, name := range h.pluginOrder {
		descriptor, ok := h.pluginDescriptors[name]
		if !ok {
			descriptor = PluginDescriptor{Name: name}
		}
		descriptors = append(descriptors, copyPluginDescriptor(descriptor))
	}
	return descriptors
}

// UseNodeMiddleware registers a node middleware.
func (h *PluginHost) UseNodeMiddleware(middleware NodeHandler) {
	if h == nil || middleware == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodeMiddlewares = append(h.nodeMiddlewares, middleware)
}

// NodeMiddlewares returns installed node middleware in registration order.
func (h *PluginHost) NodeMiddlewares() []NodeHandler {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]NodeHandler(nil), h.nodeMiddlewares...)
}

// UseEventMiddleware registers an event middleware.
func (h *PluginHost) UseEventMiddleware(middleware EventHandler) {
	if h == nil || middleware == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.eventMiddlewares = append(h.eventMiddlewares, middleware)
}

// EventMiddlewares returns installed event middleware in registration order.
func (h *PluginHost) EventMiddlewares() []EventHandler {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]EventHandler(nil), h.eventMiddlewares...)
}

// UseModelMiddleware registers a model middleware.
func (h *PluginHost) UseModelMiddleware(middleware ModelHandler) {
	if h == nil || middleware == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.modelMiddlewares = append(h.modelMiddlewares, middleware)
}

// ModelMiddlewares returns installed model middleware in registration order.
func (h *PluginHost) ModelMiddlewares() []ModelHandler {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]ModelHandler(nil), h.modelMiddlewares...)
}

// UseToolMiddleware registers a tool middleware.
func (h *PluginHost) UseToolMiddleware(middleware ToolHandler) {
	if h == nil || middleware == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolMiddlewares = append(h.toolMiddlewares, middleware)
}

// ToolMiddlewares returns installed tool middleware in registration order.
func (h *PluginHost) ToolMiddlewares() []ToolHandler {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]ToolHandler(nil), h.toolMiddlewares...)
}

// Subscribe registers an event subscriber.
func (h *PluginHost) Subscribe(subscriber EventSubscriber) {
	if h == nil || subscriber == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers = append(h.subscribers, subscriber)
}

// Publish sends an event to subscribers in registration order.
func (h *PluginHost) Publish(ctx context.Context, event Event) error {
	_, err := h.publish(ctx, event)
	return err
}

func (h *PluginHost) publish(ctx context.Context, event Event) (Event, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return event, err
	}
	if h == nil {
		return event, nil
	}
	h.mu.RLock()
	subscribers := append([]EventSubscriber(nil), h.subscribers...)
	failurePolicy := h.failurePolicy
	h.mu.RUnlock()
	if failurePolicy == "" {
		failurePolicy = PluginFailureStrict
	}
	var failures []string
	for _, subscriber := range subscribers {
		if err := subscriber(ctx, event); err != nil {
			if failurePolicy == PluginFailureFallback {
				failures = append(failures, err.Error())
				continue
			}
			return event, fmt.Errorf("gopact: plugin subscriber: %w", err)
		}
	}
	if len(failures) > 0 {
		if event.Metadata == nil {
			event.Metadata = make(map[string]any)
		} else {
			metadata := make(map[string]any, len(event.Metadata)+1)
			for key, value := range event.Metadata {
				metadata[key] = value
			}
			event.Metadata = metadata
		}
		event.Metadata[EventMetadataPluginSubscriberErrors] = failures
	}
	return event, nil
}

func (h *PluginHost) beginRun(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.isClosing || h.isClosed {
		return ErrPluginHostClosed
	}
	h.activeRuns++
	return nil
}

func (h *PluginHost) endRun() {
	if h == nil {
		return
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.activeRuns > 0 {
		h.activeRuns--
	}
	if h.activeRuns == 0 && h.idle != nil {
		close(h.idle)
		h.idle = nil
	}
}

func (h *PluginHost) startClose(ctx context.Context) (<-chan struct{}, bool, error) {
	h.lifecycleMu.Lock()
	if h.isClosed {
		err := h.closeErr
		h.lifecycleMu.Unlock()
		return nil, false, err
	}
	if h.isClosing {
		done := h.closeDone
		h.lifecycleMu.Unlock()
		return done, false, nil
	}
	h.isClosing = true
	h.closeDone = make(chan struct{})
	done := h.closeDone
	for h.activeRuns > 0 {
		if h.idle == nil {
			h.idle = make(chan struct{})
		}
		idle := h.idle
		h.lifecycleMu.Unlock()
		select {
		case <-idle:
		case <-ctx.Done():
			h.cancelClose(done)
			return nil, false, ctx.Err()
		}
		h.lifecycleMu.Lock()
	}
	h.lifecycleMu.Unlock()
	return done, true, nil
}

func (h *PluginHost) cancelClose(done chan struct{}) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.isClosed || h.closeDone != done {
		return
	}
	h.isClosing = false
	h.closeDone = nil
	close(done)
}

func (h *PluginHost) finishClose(err error) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	h.closeErr = err
	h.isClosed = true
	h.isClosing = false
	if h.closeDone != nil {
		close(h.closeDone)
		h.closeDone = nil
	}
}

func (h *PluginHost) waitClosed(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.isClosed {
		return h.closeErr
	}
	return ErrPluginHostClosed
}

func removeString(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}

func describePlugin(plugin Plugin) (PluginDescriptor, error) {
	name := plugin.Name()
	descriptor := PluginDescriptor{Name: name}
	if describer, ok := plugin.(PluginDescriber); ok {
		descriptor = describer.Descriptor()
		if descriptor.Name == "" {
			descriptor.Name = name
		}
	}
	if descriptor.Name != name {
		return PluginDescriptor{}, fmt.Errorf("%w: descriptor name %q does not match plugin name %q", ErrInvalidPluginDescriptor, descriptor.Name, name)
	}
	descriptor = copyPluginDescriptor(descriptor)
	capabilities, err := normalizePluginCapabilities(descriptor.Capabilities)
	if err != nil {
		return PluginDescriptor{}, err
	}
	descriptor.Capabilities = capabilities
	if err := descriptor.Validate(); err != nil {
		return PluginDescriptor{}, err
	}
	return descriptor, nil
}

func inferPluginCapabilities(hasNodeMiddleware, hasEventMiddleware, hasModelMiddleware, hasToolMiddleware, hasSubscriber bool) []PluginCapability {
	var capabilities []PluginCapability
	if hasNodeMiddleware {
		capabilities = append(capabilities, PluginCapabilityNodeMiddleware)
	}
	if hasEventMiddleware {
		capabilities = append(capabilities, PluginCapabilityEventMiddleware)
	}
	if hasModelMiddleware {
		capabilities = append(capabilities, PluginCapabilityModelMiddleware)
	}
	if hasToolMiddleware {
		capabilities = append(capabilities, PluginCapabilityToolMiddleware)
	}
	if hasSubscriber {
		capabilities = append(capabilities, PluginCapabilityEventSubscriber)
	}
	return capabilities
}

func mergePluginCapabilities(base []PluginCapability, extra ...PluginCapability) []PluginCapability {
	capabilities, _ := normalizePluginCapabilities(base)
	seen := make(map[PluginCapability]struct{}, len(capabilities)+len(extra))
	for _, capability := range capabilities {
		seen[capability] = struct{}{}
	}
	for _, capability := range extra {
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	return capabilities
}

func normalizePluginCapabilities(values []PluginCapability) ([]PluginCapability, error) {
	capabilities := make([]PluginCapability, 0, len(values))
	seen := make(map[PluginCapability]struct{}, len(values))
	for _, capability := range values {
		if capability == "" {
			return nil, fmt.Errorf("%w: capability is required", ErrInvalidPluginDescriptor)
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

func copyPluginDescriptor(descriptor PluginDescriptor) PluginDescriptor {
	copied := descriptor
	copied.Capabilities = append([]PluginCapability(nil), descriptor.Capabilities...)
	if descriptor.Metadata != nil {
		copied.Metadata = make(map[string]string, len(descriptor.Metadata))
		for key, value := range descriptor.Metadata {
			copied.Metadata[key] = value
		}
	}
	return copied
}
