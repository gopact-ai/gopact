package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

// Mesh is the RPC-like A2A entry point for local and discovered agents.
type Mesh struct {
	registry       *Registry
	ids            gopact.RuntimeIDs
	policy         gopact.Policy
	authenticator  Authenticator
	metadata       map[string]any
	sink           gopact.EventSubscriber
	timeout        time.Duration
	retry          MeshRetryPolicy
	httpOptions    []HTTPAgentOption
	jsonrpcOptions []JSONRPCAgentOption
}

// MeshOption configures an A2A Mesh.
type MeshOption func(*Mesh) error

// MeshRetryPolicy configures explicit A2A mesh call retries.
type MeshRetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

// NewMesh creates an A2A Mesh backed by a registry.
func NewMesh(opts ...MeshOption) (*Mesh, error) {
	mesh := &Mesh{registry: NewRegistry()}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(mesh); err != nil {
			return nil, err
		}
	}
	if mesh.registry == nil {
		return nil, errors.New("a2a: mesh registry is required")
	}
	return mesh, nil
}

// WithMeshRegistry sets the registry used by a Mesh.
func WithMeshRegistry(registry *Registry) MeshOption {
	return func(mesh *Mesh) error {
		if registry == nil {
			return errors.New("a2a: mesh registry is required")
		}
		mesh.registry = registry
		return nil
	}
}

// WithMeshRuntimeIDs sets fallback runtime ids for mesh evidence and policies.
func WithMeshRuntimeIDs(ids gopact.RuntimeIDs) MeshOption {
	return func(mesh *Mesh) error {
		mesh.ids = ids
		return nil
	}
}

// WithMeshPolicy wraps registered and discovered agents with an A2A policy gate.
func WithMeshPolicy(policy gopact.Policy) MeshOption {
	return func(mesh *Mesh) error {
		if policy == nil {
			return ErrPolicyRequired
		}
		mesh.policy = policy
		return nil
	}
}

// WithMeshAuthenticator wraps registered and discovered agents with auth injection.
func WithMeshAuthenticator(authenticator Authenticator) MeshOption {
	return func(mesh *Mesh) error {
		if authenticator == nil {
			return ErrAuthenticatorRequired
		}
		mesh.authenticator = authenticator
		return nil
	}
}

// WithMeshMetadata sets metadata copied into mesh policy and auth requests.
func WithMeshMetadata(metadata map[string]any) MeshOption {
	return func(mesh *Mesh) error {
		mesh.metadata = copyAnyMap(metadata)
		return nil
	}
}

// WithMeshEventSink publishes mesh evidence events.
func WithMeshEventSink(sink gopact.EventSubscriber) MeshOption {
	return func(mesh *Mesh) error {
		mesh.sink = sink
		return nil
	}
}

// WithMeshOperationTimeout sets a default deadline for mesh discovery, calls, streams, and cancellations.
func WithMeshOperationTimeout(timeout time.Duration) MeshOption {
	return func(mesh *Mesh) error {
		if timeout <= 0 {
			return errors.New("a2a: mesh operation timeout must be positive")
		}
		mesh.timeout = timeout
		return nil
	}
}

// WithMeshRetryPolicy retries failed Call and Route operations with the same stable task id.
func WithMeshRetryPolicy(policy MeshRetryPolicy) MeshOption {
	return func(mesh *Mesh) error {
		if policy.Backoff < 0 {
			policy.Backoff = 0
		}
		mesh.retry = policy
		return nil
	}
}

// WithMeshHTTPAgentOptions applies options to HTTP agents auto-registered from bootstrapped cards.
func WithMeshHTTPAgentOptions(opts ...HTTPAgentOption) MeshOption {
	return func(mesh *Mesh) error {
		mesh.httpOptions = append(mesh.httpOptions, opts...)
		return nil
	}
}

// WithMeshJSONRPCAgentOptions applies options to JSON-RPC agents auto-registered from bootstrapped cards.
func WithMeshJSONRPCAgentOptions(opts ...JSONRPCAgentOption) MeshOption {
	return func(mesh *Mesh) error {
		mesh.jsonrpcOptions = append(mesh.jsonrpcOptions, opts...)
		return nil
	}
}

// Register adds an agent to the mesh and returns registration evidence.
func (m *Mesh) Register(ctx context.Context, agent Agent) (RegistrationResult, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return RegistrationResult{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ids := m.idsWithContext(ctx)
	ctx = contextWithRuntimeIDs(ctx, ids)
	agent, err = m.wrapAgent(agent)
	if err != nil {
		return RegistrationResult{}, err
	}
	result, err := registry.RegisterWithEvidence(ctx, agent, ids)
	if err != nil {
		return result, err
	}
	if err := m.publishEvents(ctx, result.Events); err != nil {
		return result, err
	}
	return result, nil
}

// RegisterWithLease adds an agent to the mesh with a bounded registry lease.
func (m *Mesh) RegisterWithLease(ctx context.Context, agent Agent, ttl time.Duration) (RegistrationResult, error) {
	if ttl <= 0 {
		return RegistrationResult{}, ErrLeaseTTLRequired
	}
	registry, err := m.requireRegistry()
	if err != nil {
		return RegistrationResult{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ids := m.idsWithContext(ctx)
	ctx = contextWithRuntimeIDs(ctx, ids)
	agent, err = m.wrapAgent(agent)
	if err != nil {
		return RegistrationResult{}, err
	}
	result, err := registry.registerWithEvidence(ctx, agent, ids, time.Now().Add(ttl))
	if err != nil {
		return result, err
	}
	if err := m.publishEvents(ctx, result.Events); err != nil {
		return result, err
	}
	return result, nil
}

// Discover fetches an agent card, caches it, and registers a callable agent when possible.
func (m *Mesh) Discover(ctx context.Context, discoverer Discoverer, query DiscoveryQuery) (DiscoveryResult, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return DiscoveryResult{}, err
	}
	if discoverer == nil {
		return DiscoveryResult{}, ErrDiscovererRequired
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	query = copyDiscoveryQuery(query)
	query.IDs = query.IDs.WithDefaults(m.idsWithContext(ctx))
	ctx = contextWithRuntimeIDs(ctx, query.IDs)

	result, err := registry.Discover(ctx, discovererOnly{discoverer: discoverer}, query)
	if err != nil {
		return result, err
	}
	if agent, ok := discoverer.(Agent); ok {
		wrapped, err := m.wrapAgent(discoveredAgent{agent: agent, card: result.Card})
		if err != nil {
			return result, err
		}
		registration, err := registry.RegisterWithEvidence(ctx, wrapped, query.IDs)
		if err != nil && !errors.Is(err, ErrAgentExists) {
			return result, err
		}
		if err == nil {
			result.Events = append(result.Events, registration.Events...)
		}
	}
	if err := m.publishEvents(ctx, result.Events); err != nil {
		return result, err
	}
	return result, nil
}

// Bootstrap imports agent cards from discovery sources and publishes fetch evidence.
func (m *Mesh) Bootstrap(ctx context.Context, listers ...CardLister) (BootstrapResult, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return BootstrapResult{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ids := m.idsWithContext(ctx)
	ctx = contextWithRuntimeIDs(ctx, ids)
	result, err := registry.Bootstrap(ctx, ids, listers...)
	if err != nil {
		return result, err
	}
	events, err := m.registerBootstrapHTTPAgents(ctx, registry, result.Cards)
	if err != nil {
		return result, err
	}
	jsonRPCEvents, err := m.registerBootstrapJSONRPCAgents(ctx, registry, result.Cards)
	if err != nil {
		return result, err
	}
	result.Events = append(result.Events, events...)
	result.Events = append(result.Events, jsonRPCEvents...)
	if err := m.publishEvents(ctx, result.Events); err != nil {
		return result, err
	}
	return result, nil
}

// Cards returns known agent cards in first-seen mesh order.
func (m *Mesh) Cards(ctx context.Context) ([]AgentCard, error) {
	return m.ListCards(ctx)
}

// ListCards returns known agent cards in first-seen mesh order.
func (m *Mesh) ListCards(ctx context.Context) ([]AgentCard, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return nil, err
	}
	return registry.ListCards(ctx)
}

// Card returns one known agent card.
func (m *Mesh) Card(ctx context.Context, name string) (AgentCard, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return AgentCard{}, err
	}
	return registry.Card(ctx, name)
}

// Heartbeat renews a registered agent lease and returns the updated card snapshot.
func (m *Mesh) Heartbeat(ctx context.Context, name string, ttl time.Duration) (AgentCard, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return AgentCard{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ids := m.idsWithContext(ctx)
	ctx = contextWithRuntimeIDs(ctx, ids)
	card, err := registry.Heartbeat(ctx, name, ttl)
	if err != nil {
		return card, err
	}
	if err := m.publishEvents(ctx, []gopact.Event{agentHeartbeatEvent(card, ids)}); err != nil {
		return card, err
	}
	return card, nil
}

// Call sends one task to an agent by name and publishes call evidence.
func (m *Mesh) Call(ctx context.Context, name string, task Task) (Result, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ctx, task = m.taskContext(ctx, task)
	result, callErr := m.runWithRetry(ctx, task, func() (Result, error) {
		return registry.Send(ctx, name, task)
	})
	if publishErr := m.publishOperationEvents(ctx, result.Events); publishErr != nil && callErr == nil {
		return result, publishErr
	}
	return result, callErr
}

// Route sends one task to the first matching agent and publishes call evidence.
func (m *Mesh) Route(ctx context.Context, query RouteQuery) (Result, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ctx, query = m.routeQueryContext(ctx, query)
	result, routeErr := m.runWithRetry(ctx, query.Task, func() (Result, error) {
		return registry.Route(ctx, query)
	})
	if publishErr := m.publishOperationEvents(ctx, result.Events); publishErr != nil && routeErr == nil {
		return result, publishErr
	}
	return result, routeErr
}

// Stream sends one streaming task to an agent by name and publishes stream evidence.
func (m *Mesh) Stream(ctx context.Context, name string, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		registry, err := m.requireRegistry()
		if err != nil {
			yield(failedTaskEvent(task, err), err)
			return
		}
		ctx, cancel := m.operationContext(ctx)
		defer cancel()
		ctx, task = m.taskContext(ctx, task)
		card, _ := registry.Card(ctx, name)
		sent := false
		for event, streamErr := range registry.Stream(ctx, name, task) {
			event = event.WithDefaults(task)
			if card.Name == "" {
				card = cardFromTaskEvent(event, name)
			}
			if !sent && card.Name != "" && !isLocalPolicyBlock(streamErr) {
				if err := m.publishEvents(ctx, []gopact.Event{task.SentEvent(card)}); err != nil {
					yield(failedTaskEvent(task, err), err)
					return
				}
				sent = true
			}
			if err := m.publishEvents(ctx, []gopact.Event{event.RuntimeEvent(card)}); err != nil {
				yield(event, err)
				return
			}
			if !yield(event, streamErr) || streamErr != nil {
				return
			}
		}
	}
}

// RouteStream streams one task from the first matching agent and publishes stream evidence.
func (m *Mesh) RouteStream(ctx context.Context, query RouteQuery) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		registry, err := m.requireRegistry()
		if err != nil {
			yield(failedTaskEvent(query.Task, err), err)
			return
		}
		ctx, cancel := m.operationContext(ctx)
		defer cancel()
		ctx, query = m.routeQueryContext(ctx, query)
		var sent bool
		for event, streamErr := range registry.RouteStream(ctx, query) {
			event = event.WithDefaults(query.Task)
			card := cardFromTaskEvent(event, "")
			if !sent && card.Name != "" && !isLocalPolicyBlock(streamErr) {
				if err := m.publishEvents(ctx, []gopact.Event{query.Task.SentEvent(card)}); err != nil {
					yield(event, err)
					return
				}
				sent = true
			}
			if err := m.publishEvents(ctx, []gopact.Event{event.RuntimeEvent(card)}); err != nil {
				yield(event, err)
				return
			}
			if !yield(event, streamErr) || streamErr != nil {
				return
			}
		}
	}
}

// Cancel cancels one task by agent name and publishes terminal cancel evidence.
func (m *Mesh) Cancel(ctx context.Context, name string, taskID string) (CancelResult, error) {
	registry, err := m.requireRegistry()
	if err != nil {
		return CancelResult{}, err
	}
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	ids := m.idsWithContext(ctx)
	ctx = contextWithRuntimeIDs(ctx, ids)
	result, cancelErr := registry.CancelWithEvidence(ctx, name, taskID, ids)
	if publishErr := m.publishEvents(ctx, result.Events); publishErr != nil && cancelErr == nil {
		return result, publishErr
	}
	return result, cancelErr
}

func (m *Mesh) requireRegistry() (*Registry, error) {
	if m == nil || m.registry == nil {
		return nil, ErrAgentNotFound
	}
	return m.registry, nil
}

func (m *Mesh) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if m == nil || m.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, m.timeout)
}

func (m *Mesh) wrapAgent(agent Agent) (Agent, error) {
	if agent == nil {
		return nil, errors.New("a2a: agent is nil")
	}
	wrapped := agent
	if m.policy != nil {
		policyAgent, err := NewPolicyAgent(wrapped, m.policy,
			WithPolicyIDs(m.ids),
			WithPolicyMetadata(m.metadata),
			WithPolicyEventSink(m.sink),
		)
		if err != nil {
			return nil, err
		}
		wrapped = policyAgent
	}
	if m.authenticator != nil {
		authAgent, err := NewAuthAgent(wrapped, m.authenticator,
			WithAuthIDs(m.ids),
			WithAuthMetadata(m.metadata),
		)
		if err != nil {
			return nil, err
		}
		wrapped = authAgent
	}
	return wrapped, nil
}

func (m *Mesh) idsWithContext(ctx context.Context) gopact.RuntimeIDs {
	ids := gopact.RuntimeIDs{}
	if m != nil {
		ids = m.ids
	}
	return runtimeIDsWithContext(ctx, gopact.RuntimeIDs{}, ids)
}

func runtimeIDsWithContext(ctx context.Context, ids, defaults gopact.RuntimeIDs) gopact.RuntimeIDs {
	if contextIDs, ok := gopact.RuntimeIDsFromContext(ctx); ok && !contextIDs.IsZero() {
		ids = ids.WithDefaults(contextIDs)
	}
	return ids.WithDefaults(defaults)
}

func contextWithRuntimeIDs(ctx context.Context, ids gopact.RuntimeIDs) context.Context {
	if ctx == nil {
		ctx = context.TODO()
	}
	if ids.IsZero() {
		return ctx
	}
	return gopact.ContextWithRuntimeIDs(ctx, ids)
}

func (m *Mesh) taskContext(ctx context.Context, task Task) (context.Context, Task) {
	task = copyTask(task)
	task.IDs = task.IDs.WithDefaults(m.idsWithContext(ctx))
	return contextWithRuntimeIDs(ctx, task.IDs), task
}

func (m *Mesh) routeQueryContext(ctx context.Context, query RouteQuery) (context.Context, RouteQuery) {
	query.Require = append([]string(nil), query.Require...)
	query.Tags = append([]string(nil), query.Tags...)
	query.Metadata = copyAnyMap(query.Metadata)
	ctx, query.Task = m.taskContext(ctx, query.Task)
	return ctx, query
}

func (m *Mesh) registerBootstrapHTTPAgents(ctx context.Context, registry *Registry, cards []AgentCard) ([]gopact.Event, error) {
	events := []gopact.Event{}
	ids := m.idsWithContext(ctx)
	for _, card := range cards {
		endpoint := httpCardURL(card)
		if endpoint == "" {
			continue
		}
		opts := append([]HTTPAgentOption(nil), m.httpOptions...)
		opts = append(opts, WithHTTPAgentCard(card))
		agent, err := NewHTTPAgent(endpoint, opts...)
		if err != nil {
			return events, err
		}
		wrapped, err := m.wrapAgent(agent)
		if err != nil {
			return events, err
		}
		registration, err := registry.RegisterWithEvidence(ctx, wrapped, ids)
		if errors.Is(err, ErrAgentExists) {
			continue
		}
		if err != nil {
			return events, err
		}
		events = append(events, registration.Events...)
	}
	return events, nil
}

func (m *Mesh) registerBootstrapJSONRPCAgents(ctx context.Context, registry *Registry, cards []AgentCard) ([]gopact.Event, error) {
	events := []gopact.Event{}
	ids := m.idsWithContext(ctx)
	for _, card := range cards {
		endpoint := jsonRPCCardURL(card)
		if endpoint == "" {
			continue
		}
		opts := append([]JSONRPCAgentOption(nil), m.jsonrpcOptions...)
		opts = append(opts, WithJSONRPCAgentCard(card))
		agent, err := NewJSONRPCAgent(endpoint, opts...)
		if err != nil {
			return events, err
		}
		wrapped, err := m.wrapAgent(agent)
		if err != nil {
			return events, err
		}
		registration, err := registry.RegisterWithEvidence(ctx, wrapped, ids)
		if errors.Is(err, ErrAgentExists) {
			continue
		}
		if err != nil {
			return events, err
		}
		events = append(events, registration.Events...)
	}
	return events, nil
}

func (m *Mesh) runWithRetry(ctx context.Context, task Task, run func() (Result, error)) (Result, error) {
	if m == nil || m.retry.MaxAttempts <= 1 {
		return run()
	}
	if task.ID == "" {
		return Result{}, ErrMeshRetryTaskIDRequired
	}
	var allEvents []gopact.Event
	var last Result
	var lastErr error
	for attempt := 1; attempt <= m.retry.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			last.Events = copyEvents(allEvents)
			return last, err
		}
		result, err := run()
		result.Events = markMeshRetryAttempt(result.Events, attempt)
		allEvents = append(allEvents, copyEvents(result.Events)...)
		result.Events = copyEvents(allEvents)
		last = result
		lastErr = err
		if err == nil {
			return result, nil
		}
		if !m.shouldRetry(ctx, attempt, err) {
			return result, err
		}
		if err := waitMeshRetryDelay(ctx, m.retry.Backoff); err != nil {
			last.Events = copyEvents(allEvents)
			return last, err
		}
	}
	return last, lastErr
}

func (m *Mesh) shouldRetry(ctx context.Context, attempt int, err error) bool {
	if err == nil || attempt >= m.retry.MaxAttempts {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if isLocalPolicyBlock(err) || errors.Is(err, ErrAgentNotFound) {
		return false
	}
	return true
}

func waitMeshRetryDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func markMeshRetryAttempt(events []gopact.Event, attempt int) []gopact.Event {
	out := copyEvents(events)
	for i := range out {
		out[i].Metadata = copyAnyMap(out[i].Metadata)
		if out[i].Metadata == nil {
			out[i].Metadata = make(map[string]any)
		}
		out[i].Metadata["a2a_attempt"] = attempt
	}
	return out
}

func httpCardURL(card AgentCard) string {
	if isHTTPURL(card.URL) {
		return card.URL
	}
	for _, protocol := range card.Protocols {
		if strings.EqualFold(protocol.Transport, "http") && isHTTPURL(protocol.URL) {
			return protocol.URL
		}
	}
	return ""
}

func jsonRPCCardURL(card AgentCard) string {
	for _, protocol := range card.Protocols {
		if strings.EqualFold(protocol.Transport, "jsonrpc") && isHTTPURL(protocol.URL) {
			return protocol.URL
		}
	}
	return ""
}

func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

func (m *Mesh) publishEvents(ctx context.Context, events []gopact.Event) error {
	if m == nil || m.sink == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	for _, event := range events {
		if err := m.sink(ctx, event); err != nil {
			return fmt.Errorf("a2a: mesh event sink: %w", err)
		}
	}
	return nil
}

func (m *Mesh) publishOperationEvents(ctx context.Context, events []gopact.Event) error {
	if m != nil && m.policy != nil && m.sink != nil {
		events = withoutPolicyEvents(events)
	}
	return m.publishEvents(ctx, events)
}

func withoutPolicyEvents(events []gopact.Event) []gopact.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]gopact.Event, 0, len(events))
	for _, event := range events {
		if event.Type == gopact.EventPolicyRequested || event.Type == gopact.EventPolicyDecided {
			continue
		}
		out = append(out, event)
	}
	return out
}

type discovererOnly struct {
	discoverer Discoverer
}

func (d discovererOnly) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if d.discoverer == nil {
		return DiscoveryResult{}, ErrDiscovererRequired
	}
	return d.discoverer.Discover(ctx, query)
}

func cardFromTaskEvent(event TaskEvent, fallbackName string) AgentCard {
	card := AgentCard{Name: fallbackName}
	if event.Metadata == nil {
		return card
	}
	if name, ok := event.Metadata[metadataAgentName].(string); ok {
		card.Name = name
	}
	if url, ok := event.Metadata[metadataAgentURL].(string); ok {
		card.URL = url
	}
	return card
}
