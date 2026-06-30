// Package a2a provides minimal agent-to-agent contracts.
package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"sync"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrAgentExists is returned when registering a duplicate A2A agent name.
	ErrAgentExists = errors.New("a2a: agent already exists")
	// ErrAgentNotFound is returned when an A2A agent name is not registered.
	ErrAgentNotFound = errors.New("a2a: agent not found")
	// ErrCardNameRequired is returned when an agent card has no name.
	ErrCardNameRequired = errors.New("a2a: agent card name is required")
	// ErrDiscovererRequired is returned when discovery is requested without a discoverer.
	ErrDiscovererRequired = errors.New("a2a: discoverer is required")
	// ErrDiscoveryRequired is returned when a discovery query has no name, URL, or metadata.
	ErrDiscoveryRequired = errors.New("a2a: discovery name, url, or metadata is required")
	// ErrStreamNotSupported is returned when an agent does not implement streaming.
	ErrStreamNotSupported = errors.New("a2a: streaming is not supported")
	// ErrTaskIDRequired is returned when cancellation has no task id.
	ErrTaskIDRequired = errors.New("a2a: task id is required")
)

// TaskStatus identifies the current state of a remote task.
type TaskStatus string

const (
	// TaskStatus values describe the lifecycle state of a remote task.
	TaskStatusSubmitted TaskStatus = "submitted"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

const (
	metadataAgentName  = "agent_name"
	metadataAgentURL   = "agent_url"
	metadataA2ATaskID  = "a2a_task_id"
	metadataA2AStatus  = "a2a_status"
	metadataA2AMessage = "a2a_message"
)

// Auth carries sanitized authentication context for an A2A operation.
type Auth struct {
	Scheme        string         `json:"scheme,omitempty"`
	Principal     string         `json:"principal,omitempty"`
	CredentialRef string         `json:"credential_ref,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// IsZero reports whether the auth value carries no authentication context.
func (a Auth) IsZero() bool {
	return a.Scheme == "" && a.Principal == "" && a.CredentialRef == "" && len(a.Metadata) == 0
}

// AuthRequest is passed to an injected authenticator before an A2A operation.
type AuthRequest struct {
	IDs       gopact.RuntimeIDs          `json:"ids,omitempty"`
	AgentName string                     `json:"agent_name,omitempty"`
	Card      AgentCard                  `json:"card,omitempty"`
	Action    gopact.PolicyRequestAction `json:"action"`
	Task      *Task                      `json:"task,omitempty"`
	TaskID    string                     `json:"task_id,omitempty"`
	Metadata  map[string]any             `json:"metadata,omitempty"`
}

// Authenticator injects authentication context without letting the SDK own secrets.
type Authenticator interface {
	Authenticate(ctx context.Context, req AuthRequest) (Auth, error)
}

// AuthenticatorFunc adapts a function into an Authenticator.
type AuthenticatorFunc func(ctx context.Context, req AuthRequest) (Auth, error)

// Authenticate calls f.
func (f AuthenticatorFunc) Authenticate(ctx context.Context, req AuthRequest) (Auth, error) {
	if f == nil {
		return Auth{}, errors.New("a2a: authenticator function is nil")
	}
	return f(ctx, req)
}

type authContextKey struct{}

// ContextWithAuth returns a context carrying A2A auth for transport adapters.
func ContextWithAuth(ctx context.Context, auth Auth) context.Context {
	if ctx == nil {
		ctx = context.TODO()
	}
	return context.WithValue(ctx, authContextKey{}, copyAuth(auth))
}

// AuthFromContext returns A2A auth carried on ctx.
func AuthFromContext(ctx context.Context) (Auth, bool) {
	if ctx == nil {
		return Auth{}, false
	}
	auth, ok := ctx.Value(authContextKey{}).(Auth)
	if !ok {
		return Auth{}, false
	}
	return copyAuth(auth), true
}

// AgentCard describes a remote or local agent.
type AgentCard struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	URL          string            `json:"url,omitempty"`
	Protocols    []ProtocolBinding `json:"protocols,omitempty"`
	Skills       []AgentSkill      `json:"skills,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	InputSchema  gopact.JSONSchema `json:"input_schema,omitempty"`
	OutputSchema gopact.JSONSchema `json:"output_schema,omitempty"`
	Streaming    bool              `json:"streaming,omitempty"`
	Artifacts    bool              `json:"artifacts,omitempty"`
	Auth         *AuthRequirement  `json:"auth,omitempty"`
	Owner        string            `json:"owner,omitempty"`
	Version      string            `json:"version,omitempty"`
	Health       *HealthHints      `json:"health,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

// ProtocolBinding describes one transport binding exposed by an agent card.
type ProtocolBinding struct {
	Name      string `json:"name"`
	Transport string `json:"transport,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AgentSkill describes one skill exposed by an agent card.
type AgentSkill struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	InputSchema  gopact.JSONSchema `json:"input_schema,omitempty"`
	OutputSchema gopact.JSONSchema `json:"output_schema,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

// AuthRequirement describes how callers authenticate to an agent.
type AuthRequirement struct {
	Required bool           `json:"required,omitempty"`
	Schemes  []string       `json:"schemes,omitempty"`
	Scopes   []string       `json:"scopes,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// HealthHints describes optional health and readiness endpoints.
type HealthHints struct {
	HealthPath    string `json:"health_path,omitempty"`
	ReadinessPath string `json:"readiness_path,omitempty"`
}

// DiscoveryQuery identifies an agent card lookup.
type DiscoveryQuery struct {
	Name     string            `json:"name,omitempty"`
	URL      string            `json:"url,omitempty"`
	IDs      gopact.RuntimeIDs `json:"ids,omitempty"`
	Require  []string          `json:"require,omitempty"`
	Metadata map[string]any    `json:"metadata,omitempty"`
}

// DiscoveryResult records a discovered agent card and fetch evidence.
type DiscoveryResult struct {
	Card     AgentCard      `json:"card"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Events   []gopact.Event `json:"events,omitempty"`
}

// RouteQuery selects a registered agent by card capabilities and metadata.
type RouteQuery struct {
	Require  []string       `json:"require,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Fallback bool           `json:"fallback,omitempty"`
	Task     Task           `json:"task"`
}

// Discoverer looks up remote agent cards.
type Discoverer interface {
	Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error)
}

// CardLister lists agent cards from a registry or discoverer.
type CardLister interface {
	ListCards(ctx context.Context) ([]AgentCard, error)
}

// DiscovererFunc adapts a function into a Discoverer.
type DiscovererFunc func(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error)

// Discover calls f.
func (f DiscovererFunc) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if f == nil {
		return DiscoveryResult{}, errors.New("a2a: discoverer function is nil")
	}
	return f(ctx, query)
}

// Task is an A2A task request.
type Task struct {
	ID       string            `json:"id,omitempty"`
	IDs      gopact.RuntimeIDs `json:"ids,omitempty"`
	Input    string            `json:"input,omitempty"`
	Auth     *Auth             `json:"auth,omitempty"`
	Metadata map[string]any    `json:"metadata,omitempty"`
}

// SentEvent converts an outbound A2A task into a core runtime event.
func (t Task) SentEvent(card AgentCard) gopact.Event {
	metadata := copyAnyMap(t.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if card.Name != "" {
		metadata[metadataAgentName] = card.Name
	}
	if card.URL != "" {
		metadata[metadataAgentURL] = card.URL
	}
	if id := taskID(t); id != "" {
		metadata[metadataA2ATaskID] = id
	}

	var message *gopact.Message
	if t.Input != "" {
		msg := gopact.UserMessage(t.Input)
		message = &msg
	}

	return gopact.Event{
		Type:     gopact.EventA2ATaskSent,
		IDs:      t.IDs,
		Message:  message,
		Metadata: metadata,
	}.WithRuntimeDefaults(t.IDs)
}

// Result is an A2A task result.
type Result struct {
	TaskID    string               `json:"task_id,omitempty"`
	Output    string               `json:"output,omitempty"`
	Artifacts []gopact.ArtifactRef `json:"artifacts,omitempty"`
	Metadata  map[string]any       `json:"metadata,omitempty"`
	Events    []gopact.Event       `json:"events,omitempty"`
}

// CancelResult records evidence produced while canceling an A2A task.
type CancelResult struct {
	TaskID string         `json:"task_id,omitempty"`
	Events []gopact.Event `json:"events,omitempty"`
}

// RegistrationResult records a successful agent registration.
type RegistrationResult struct {
	Card   AgentCard      `json:"card"`
	Events []gopact.Event `json:"events,omitempty"`
}

// BootstrapResult records agent cards loaded during mesh bootstrap.
type BootstrapResult struct {
	Cards  []AgentCard    `json:"cards,omitempty"`
	Events []gopact.Event `json:"events,omitempty"`
}

// TaskEvent is one streaming status or result update for an A2A task.
type TaskEvent struct {
	TaskID    string               `json:"task_id,omitempty"`
	IDs       gopact.RuntimeIDs    `json:"ids,omitempty"`
	Status    TaskStatus           `json:"status,omitempty"`
	Message   string               `json:"message,omitempty"`
	Result    *Result              `json:"result,omitempty"`
	Artifacts []gopact.ArtifactRef `json:"artifacts,omitempty"`
	Metadata  map[string]any       `json:"metadata,omitempty"`
	Err       error                `json:"-"`
}

// Agent sends tasks to another agent boundary.
type Agent interface {
	Card() AgentCard
	Send(ctx context.Context, task Task) (Result, error)
	Cancel(ctx context.Context, taskID string) error
}

// StreamingAgent is an optional A2A capability for status/result streams.
type StreamingAgent interface {
	Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error]
}

// Registry stores A2A agents by card name.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]Agent
	cards  map[string]AgentCard
	order  []string
}

// NewRegistry creates an empty A2A registry.
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]Agent),
		cards:  make(map[string]AgentCard),
	}
}

// Register adds agent to the registry by its card name.
func (r *Registry) Register(ctx context.Context, agent Agent) error {
	_, err := r.RegisterWithEvidence(ctx, agent, gopact.RuntimeIDs{})
	return err
}

// RegisterWithEvidence adds agent and returns an A2A registration evidence event.
func (r *Registry) RegisterWithEvidence(ctx context.Context, agent Agent, ids gopact.RuntimeIDs) (RegistrationResult, error) {
	if err := ctx.Err(); err != nil {
		return RegistrationResult{}, err
	}
	if agent == nil {
		return RegistrationResult{}, errors.New("a2a: agent is nil")
	}
	card := agent.Card()
	if card.Name == "" {
		return RegistrationResult{}, ErrCardNameRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.agents == nil {
		r.agents = make(map[string]Agent)
	}
	if r.cards == nil {
		r.cards = make(map[string]AgentCard)
	}
	if _, ok := r.agents[card.Name]; ok {
		return RegistrationResult{}, fmt.Errorf("%w: %s", ErrAgentExists, card.Name)
	}
	r.agents[card.Name] = agent
	r.cards[card.Name] = copyAgentCard(card)
	r.appendOrderLocked(card.Name)
	card = copyAgentCard(card)
	return RegistrationResult{
		Card:   card,
		Events: []gopact.Event{agentRegisteredEvent(card, ids)},
	}, nil
}

// ListCards returns known agent cards in first-seen registration/discovery order.
func (r *Registry) ListCards(ctx context.Context) ([]AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrAgentNotFound
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	cards := make([]AgentCard, 0, len(r.order))
	for _, name := range r.order {
		card, ok := r.cards[name]
		if !ok {
			continue
		}
		cards = append(cards, copyAgentCard(card))
	}
	return cards, nil
}

// ImportCards stores cards listed by lister and preserves first-seen order.
func (r *Registry) ImportCards(ctx context.Context, lister CardLister) ([]AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrAgentNotFound
	}
	imported, err := listImportCards(ctx, lister)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.importCardsLocked(imported)
	return copyAgentCards(imported), nil
}

// Bootstrap imports agent cards from multiple mesh discovery sources and returns fetch evidence.
func (r *Registry) Bootstrap(ctx context.Context, ids gopact.RuntimeIDs, listers ...CardLister) (BootstrapResult, error) {
	if err := ctx.Err(); err != nil {
		return BootstrapResult{}, err
	}
	if r == nil {
		return BootstrapResult{}, ErrAgentNotFound
	}
	if len(listers) == 0 {
		return BootstrapResult{}, ErrDiscovererRequired
	}

	imported := make([]AgentCard, 0)
	sourceIndexes := make([]int, 0)
	sourceCardIndexes := make([]int, 0)
	for sourceIndex, lister := range listers {
		cards, err := listImportCards(ctx, lister)
		if err != nil {
			return BootstrapResult{}, err
		}
		for cardIndex := range cards {
			sourceIndexes = append(sourceIndexes, sourceIndex)
			sourceCardIndexes = append(sourceCardIndexes, cardIndex)
		}
		imported = append(imported, cards...)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.importCardsLocked(imported)

	events := make([]gopact.Event, 0, len(imported))
	for i, card := range imported {
		events = append(events, agentCardFetchedEvent(DiscoveryQuery{IDs: ids}, DiscoveryResult{
			Card: card,
			Metadata: map[string]any{
				"source_index":      sourceIndexes[i],
				"source_card_index": sourceCardIndexes[i],
			},
		}))
	}
	return BootstrapResult{Cards: copyAgentCards(imported), Events: events}, nil
}

// Card returns the latest known agent card for name.
func (r *Registry) Card(ctx context.Context, name string) (AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return AgentCard{}, err
	}
	if r == nil {
		return AgentCard{}, ErrAgentNotFound
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if agent, ok := r.agents[name]; ok {
		return copyAgentCard(agent.Card()), nil
	}
	if card, ok := r.cards[name]; ok {
		return copyAgentCard(card), nil
	}
	return AgentCard{}, ErrAgentNotFound
}

// Discover fetches an agent card and stores it in the registry.
func (r *Registry) Discover(ctx context.Context, discoverer Discoverer, query DiscoveryQuery) (DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	if r == nil {
		return DiscoveryResult{}, ErrAgentNotFound
	}
	if discoverer == nil {
		return DiscoveryResult{}, ErrDiscovererRequired
	}
	if !hasDiscoveryCriteria(query) {
		return DiscoveryResult{}, ErrDiscoveryRequired
	}
	result, err := discoverer.Discover(ctx, copyDiscoveryQuery(query))
	if err != nil {
		return DiscoveryResult{}, err
	}
	if result.Card.Name == "" {
		return DiscoveryResult{}, ErrCardNameRequired
	}
	result.Card = copyAgentCard(result.Card)
	result.Metadata = copyAnyMap(result.Metadata)
	result.Events = append(copyEvents(result.Events), agentCardFetchedEvent(query, result))
	agent, hasAgent := discoverer.(Agent)

	r.mu.Lock()
	if r.cards == nil {
		r.cards = make(map[string]AgentCard)
	}
	r.cards[result.Card.Name] = copyAgentCard(result.Card)
	r.appendOrderLocked(result.Card.Name)
	if hasAgent {
		if r.agents == nil {
			r.agents = make(map[string]Agent)
		}
		if _, ok := r.agents[result.Card.Name]; !ok {
			r.agents[result.Card.Name] = discoveredAgent{agent: agent, card: result.Card}
		}
	}
	r.mu.Unlock()

	return result, nil
}

// Send sends one task to the named registered agent.
func (r *Registry) Send(ctx context.Context, name string, task Task) (Result, error) {
	agent, err := r.resolve(ctx, name)
	if err != nil {
		return Result{}, err
	}
	return sendWithEvidence(ctx, agent, task)
}

// Route sends one task to a registered agent matching all route constraints.
func (r *Registry) Route(ctx context.Context, query RouteQuery) (Result, error) {
	if query.Fallback {
		return r.routeWithFallback(ctx, query)
	}
	agent, err := r.resolveRoute(ctx, query)
	if err != nil {
		return Result{}, err
	}
	return sendWithEvidence(ctx, agent, query.Task)
}

func (r *Registry) routeWithFallback(ctx context.Context, query RouteQuery) (Result, error) {
	agents, err := r.resolveRoutes(ctx, query)
	if err != nil {
		return Result{}, err
	}
	var events []gopact.Event
	var last Result
	var lastErr error
	for _, agent := range agents {
		result, err := sendWithEvidence(ctx, agent, query.Task)
		result.Events = append(events, copyEvents(result.Events)...)
		if err == nil {
			return result, nil
		}
		if isLocalPolicyBlock(err) {
			return result, err
		}
		events = copyEvents(result.Events)
		last = result
		lastErr = err
	}
	return last, lastErr
}

// RouteStream streams one task from a registered agent matching all route constraints.
func (r *Registry) RouteStream(ctx context.Context, query RouteQuery) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		if query.Fallback {
			r.routeStreamWithFallback(ctx, query, yield)
			return
		}
		agent, err := r.resolveRoute(ctx, query)
		if err != nil {
			yield(TaskEvent{TaskID: query.Task.ID, IDs: query.Task.IDs, Status: TaskStatusFailed, Err: err}, err)
			return
		}
		streamer, ok := agent.(StreamingAgent)
		if !ok {
			yield(TaskEvent{TaskID: query.Task.ID, IDs: query.Task.IDs, Status: TaskStatusFailed, Err: ErrStreamNotSupported}, ErrStreamNotSupported)
			return
		}
		card := agent.Card()
		for event, err := range streamer.Stream(ctx, query.Task) {
			event = event.WithDefaults(query.Task)
			event = eventWithAgentMetadata(event, card)
			if !yield(event, err) || err != nil {
				return
			}
		}
	}
}

func (r *Registry) routeStreamWithFallback(ctx context.Context, query RouteQuery, yield func(TaskEvent, error) bool) {
	agents, err := r.resolveRoutes(ctx, query)
	if err != nil {
		yield(TaskEvent{TaskID: query.Task.ID, IDs: query.Task.IDs, Status: TaskStatusFailed, Err: err}, err)
		return
	}
	for _, agent := range agents {
		streamer, ok := agent.(StreamingAgent)
		if !ok {
			continue
		}
		card := agent.Card()
		started := false
		unsupported := false
		for event, err := range streamer.Stream(ctx, query.Task) {
			if err != nil && !started && errors.Is(err, ErrStreamNotSupported) {
				unsupported = true
				break
			}
			started = true
			event = event.WithDefaults(query.Task)
			event = eventWithAgentMetadata(event, card)
			if !yield(event, err) || err != nil {
				return
			}
		}
		if unsupported {
			continue
		}
		return
	}
	yield(TaskEvent{TaskID: query.Task.ID, IDs: query.Task.IDs, Status: TaskStatusFailed, Err: ErrStreamNotSupported}, ErrStreamNotSupported)
}

// Stream streams task events from the named registered agent.
func (r *Registry) Stream(ctx context.Context, name string, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		agent, err := r.resolve(ctx, name)
		if err != nil {
			yield(TaskEvent{TaskID: task.ID, IDs: task.IDs, Status: TaskStatusFailed, Err: err}, err)
			return
		}
		streamer, ok := agent.(StreamingAgent)
		if !ok {
			yield(TaskEvent{TaskID: task.ID, IDs: task.IDs, Status: TaskStatusFailed, Err: ErrStreamNotSupported}, ErrStreamNotSupported)
			return
		}
		card := agent.Card()
		for event, err := range streamer.Stream(ctx, task) {
			event = event.WithDefaults(task)
			event = eventWithAgentMetadata(event, card)
			if !yield(event, err) || err != nil {
				return
			}
		}
	}
}

// Cancel cancels one task on the named registered agent.
func (r *Registry) Cancel(ctx context.Context, name string, taskID string) error {
	_, err := r.CancelWithEvidence(ctx, name, taskID, gopact.RuntimeIDs{})
	return err
}

// CancelWithEvidence cancels one task and returns a terminal A2A evidence event.
func (r *Registry) CancelWithEvidence(ctx context.Context, name string, taskID string, ids gopact.RuntimeIDs) (CancelResult, error) {
	if taskID == "" {
		return CancelResult{}, ErrTaskIDRequired
	}
	agent, err := r.resolve(ctx, name)
	if err != nil {
		return CancelResult{}, err
	}
	card := agent.Card()
	if err := agent.Cancel(ctx, taskID); err != nil {
		event := TaskEvent{TaskID: taskID, IDs: ids, Status: TaskStatusFailed, Err: err}.RuntimeEvent(card)
		return CancelResult{TaskID: taskID, Events: []gopact.Event{event}}, err
	}
	event := TaskEvent{TaskID: taskID, IDs: ids, Status: TaskStatusCanceled}.RuntimeEvent(card)
	return CancelResult{TaskID: taskID, Events: []gopact.Event{event}}, nil
}

func (r *Registry) resolve(ctx context.Context, name string) (Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrAgentNotFound
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	agent, ok := r.agents[name]
	if !ok {
		return nil, ErrAgentNotFound
	}
	return agent, nil
}

func (r *Registry) resolveRoute(ctx context.Context, query RouteQuery) (Agent, error) {
	agents, err := r.resolveRoutes(ctx, query)
	if err != nil {
		return nil, err
	}
	return agents[0], nil
}

func (r *Registry) resolveRoutes(ctx context.Context, query RouteQuery) ([]Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || (len(query.Require) == 0 && len(query.Metadata) == 0) {
		return nil, ErrAgentNotFound
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]Agent, 0, len(r.order))
	for _, name := range r.order {
		agent := r.agents[name]
		card := r.cards[name]
		if agent == nil {
			continue
		}
		if hasCapabilities(card.Capabilities, query.Require) && hasMetadata(card.Metadata, query.Metadata) {
			agents = append(agents, agent)
		}
	}
	if len(agents) == 0 {
		return nil, ErrAgentNotFound
	}
	return agents, nil
}

func (r *Registry) appendOrderLocked(name string) {
	for _, existing := range r.order {
		if existing == name {
			return
		}
	}
	r.order = append(r.order, name)
}

func (r *Registry) importCardsLocked(cards []AgentCard) {
	if r.cards == nil {
		r.cards = make(map[string]AgentCard)
	}
	for _, card := range cards {
		r.cards[card.Name] = copyAgentCard(card)
		r.appendOrderLocked(card.Name)
	}
}

func listImportCards(ctx context.Context, lister CardLister) ([]AgentCard, error) {
	if lister == nil {
		return nil, ErrDiscovererRequired
	}
	cards, err := lister.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	imported := make([]AgentCard, 0, len(cards))
	for _, card := range cards {
		if card.Name == "" {
			return nil, ErrCardNameRequired
		}
		imported = append(imported, copyAgentCard(card))
	}
	return imported, nil
}

func hasDiscoveryCriteria(query DiscoveryQuery) bool {
	return query.Name != "" || query.URL != "" || len(query.Require) > 0 || len(query.Metadata) > 0
}

func matchesRemoteDiscoveryQuery(card AgentCard, query DiscoveryQuery) bool {
	if query.Name != "" && card.Name != query.Name {
		return false
	}
	if !hasCapabilities(card.Capabilities, query.Require) {
		return false
	}
	return hasMetadata(card.Metadata, query.Metadata)
}

func hasCapabilities(got []string, require []string) bool {
	set := make(map[string]struct{}, len(got))
	for _, capability := range got {
		set[capability] = struct{}{}
	}
	for _, capability := range require {
		if _, ok := set[capability]; !ok {
			return false
		}
	}
	return true
}

func hasMetadata(got map[string]any, require map[string]any) bool {
	for key, expected := range require {
		actual, ok := got[key]
		if !ok || !reflect.DeepEqual(actual, expected) {
			return false
		}
	}
	return true
}

func eventWithAgentMetadata(event TaskEvent, card AgentCard) TaskEvent {
	event.Metadata = copyAnyMap(event.Metadata)
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	if card.Name != "" {
		event.Metadata[metadataAgentName] = card.Name
	}
	if card.URL != "" {
		event.Metadata[metadataAgentURL] = card.URL
	}
	return event
}

func sendWithEvidence(ctx context.Context, agent Agent, task Task) (Result, error) {
	card := agent.Card()
	result, err := agent.Send(ctx, task)
	policyEvents, otherEvents := splitPolicyEvents(result.Events)
	events := copyEvents(policyEvents)
	if !isLocalPolicyBlock(err) {
		events = append(events, task.SentEvent(card))
	}
	result.Events = append(events, copyEvents(otherEvents)...)
	if err != nil {
		failed := TaskEvent{
			TaskID: taskID(task),
			IDs:    task.IDs,
			Status: TaskStatusFailed,
			Err:    err,
		}.RuntimeEvent(card)
		result.Events = append(result.Events, failed)
		return result, err
	}
	completed := TaskEvent{
		TaskID:    firstNonEmpty(result.TaskID, taskID(task)),
		IDs:       task.IDs,
		Status:    TaskStatusCompleted,
		Result:    &result,
		Artifacts: result.Artifacts,
	}.RuntimeEvent(card)
	result.Events = append(result.Events, completed)
	return result, nil
}

func splitPolicyEvents(events []gopact.Event) ([]gopact.Event, []gopact.Event) {
	if len(events) == 0 {
		return nil, nil
	}
	policyEvents := make([]gopact.Event, 0, len(events))
	otherEvents := make([]gopact.Event, 0, len(events))
	for _, event := range events {
		if event.Type == gopact.EventPolicyRequested || event.Type == gopact.EventPolicyDecided {
			policyEvents = append(policyEvents, event)
			continue
		}
		otherEvents = append(otherEvents, event)
	}
	return policyEvents, otherEvents
}

func isLocalPolicyBlock(err error) bool {
	return errors.Is(err, gopact.ErrPolicyDenied) || errors.Is(err, gopact.ErrInterrupted)
}

type discoveredAgent struct {
	agent Agent
	card  AgentCard
}

func (a discoveredAgent) Card() AgentCard {
	return copyAgentCard(a.card)
}

func (a discoveredAgent) Send(ctx context.Context, task Task) (Result, error) {
	return a.agent.Send(ctx, task)
}

func (a discoveredAgent) Cancel(ctx context.Context, taskID string) error {
	return a.agent.Cancel(ctx, taskID)
}

func (a discoveredAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	streamer, ok := a.agent.(StreamingAgent)
	if !ok {
		return func(yield func(TaskEvent, error) bool) {
			yield(failedTaskEvent(task, ErrStreamNotSupported), ErrStreamNotSupported)
		}
	}
	return streamer.Stream(ctx, task)
}

// FakeAgent is an in-memory A2A agent for tests.
type FakeAgent struct {
	CardValue  AgentCard
	SendFunc   func(ctx context.Context, task Task) (Result, error)
	StreamFunc func(ctx context.Context, task Task) iter.Seq2[TaskEvent, error]
	CancelFunc func(ctx context.Context, taskID string) error
}

// Card returns the configured fake agent card.
func (a FakeAgent) Card() AgentCard {
	return a.CardValue
}

// Send calls SendFunc or returns a default result for task.
func (a FakeAgent) Send(ctx context.Context, task Task) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if a.SendFunc != nil {
		return a.SendFunc(ctx, task)
	}
	return Result{TaskID: task.ID}, nil
}

// Stream calls StreamFunc or yields ErrStreamNotSupported.
func (a FakeAgent) Stream(ctx context.Context, task Task) iter.Seq2[TaskEvent, error] {
	return func(yield func(TaskEvent, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(TaskEvent{TaskID: task.ID, IDs: task.IDs, Status: TaskStatusFailed, Err: err}, err)
			return
		}
		if a.StreamFunc == nil {
			yield(TaskEvent{TaskID: task.ID, IDs: task.IDs, Status: TaskStatusFailed, Err: ErrStreamNotSupported}, ErrStreamNotSupported)
			return
		}
		for event, err := range a.StreamFunc(ctx, task) {
			event = event.WithDefaults(task)
			if !yield(event, err) || err != nil {
				return
			}
		}
	}
}

// Cancel calls CancelFunc or accepts the cancellation.
func (a FakeAgent) Cancel(ctx context.Context, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if taskID == "" {
		return ErrTaskIDRequired
	}
	if a.CancelFunc != nil {
		return a.CancelFunc(ctx, taskID)
	}
	return nil
}

// WithDefaults fills missing task identity fields from task.
func (e TaskEvent) WithDefaults(task Task) TaskEvent {
	if e.TaskID == "" {
		e.TaskID = task.ID
	}
	if e.IDs.IsZero() {
		e.IDs = task.IDs
	} else {
		e.IDs = e.IDs.WithDefaults(task.IDs)
	}
	if e.Status == "" {
		if e.Err != nil {
			e.Status = TaskStatusFailed
		} else if e.Result != nil {
			e.Status = TaskStatusCompleted
		}
	}
	if len(e.Artifacts) == 0 && e.Result != nil {
		e.Artifacts = append([]gopact.ArtifactRef(nil), e.Result.Artifacts...)
	}
	return e
}

// RuntimeEvent converts a streamed A2A task update into a core runtime event.
func (e TaskEvent) RuntimeEvent(card AgentCard) gopact.Event {
	e = e.WithDefaults(Task{ID: e.TaskID, IDs: e.IDs})
	metadata := copyAnyMap(e.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if card.Name != "" {
		metadata[metadataAgentName] = card.Name
	}
	if card.URL != "" {
		metadata[metadataAgentURL] = card.URL
	}
	if e.TaskID != "" {
		metadata[metadataA2ATaskID] = e.TaskID
	}
	if e.Status != "" {
		metadata[metadataA2AStatus] = string(e.Status)
	}
	if e.Message != "" {
		metadata[metadataA2AMessage] = e.Message
	}

	artifacts := copyArtifactRefs(e.Artifacts)
	var result *gopact.ToolResult
	if e.Result != nil {
		taskResult := copyResult(*e.Result)
		artifacts = append(artifacts, copyArtifactRefs(taskResult.Artifacts)...)
		result = &gopact.ToolResult{
			Content:   taskResult.Output,
			Artifacts: taskResult.Artifacts,
			Metadata:  taskResult.Metadata,
		}
	}
	artifacts = dedupeArtifactRefs(artifacts)

	var message *gopact.Message
	if e.Message != "" {
		msg := gopact.AssistantMessage(e.Message)
		message = &msg
	}

	return gopact.Event{
		Type:      taskEventType(e),
		IDs:       e.IDs,
		Message:   message,
		Result:    result,
		Artifacts: artifacts,
		Metadata:  metadata,
		Err:       e.Err,
	}.WithRuntimeDefaults(e.IDs)
}

func taskEventType(e TaskEvent) gopact.EventType {
	switch e.Status {
	case TaskStatusCompleted:
		return gopact.EventA2ATaskCompleted
	case TaskStatusFailed:
		return gopact.EventA2ATaskFailed
	case TaskStatusCanceled:
		return gopact.EventA2ATaskCanceled
	case TaskStatusSubmitted, TaskStatusRunning:
		return gopact.EventA2ATaskStatusUpdated
	}
	if e.Err != nil {
		return gopact.EventA2ATaskFailed
	}
	if e.Result != nil {
		return gopact.EventA2ATaskCompleted
	}
	if len(e.Artifacts) > 0 {
		return gopact.EventA2AArtifactUpdated
	}
	if e.Message != "" {
		return gopact.EventA2AMessageReceived
	}
	return gopact.EventA2ATaskStatusUpdated
}

func agentRegisteredEvent(card AgentCard, ids gopact.RuntimeIDs) gopact.Event {
	metadata := copyAnyMap(card.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[metadataAgentName] = card.Name
	if card.URL != "" {
		metadata[metadataAgentURL] = card.URL
	}
	metadata["capability_count"] = len(card.Capabilities)
	return gopact.Event{
		Type:     gopact.EventA2AAgentRegistered,
		IDs:      ids,
		Metadata: metadata,
	}.WithRuntimeDefaults(ids)
}

func agentCardFetchedEvent(query DiscoveryQuery, result DiscoveryResult) gopact.Event {
	metadata := copyAnyMap(result.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[metadataAgentName] = result.Card.Name
	if result.Card.URL != "" {
		metadata[metadataAgentURL] = result.Card.URL
	} else if query.URL != "" {
		metadata[metadataAgentURL] = query.URL
	}
	if query.Name != "" {
		metadata["query_name"] = query.Name
	}
	return gopact.Event{
		Type:     gopact.EventA2AAgentCardFetched,
		IDs:      query.IDs,
		Metadata: metadata,
	}.WithRuntimeDefaults(query.IDs)
}

func copyDiscoveryQuery(query DiscoveryQuery) DiscoveryQuery {
	query.Require = append([]string(nil), query.Require...)
	query.Metadata = copyAnyMap(query.Metadata)
	return query
}

func copyAgentCard(card AgentCard) AgentCard {
	card.Protocols = append([]ProtocolBinding(nil), card.Protocols...)
	card.Skills = copyAgentSkills(card.Skills)
	card.Capabilities = append([]string(nil), card.Capabilities...)
	card.InputSchema = copyA2AJSONSchema(card.InputSchema)
	card.OutputSchema = copyA2AJSONSchema(card.OutputSchema)
	card.Auth = copyAuthRequirement(card.Auth)
	card.Health = copyHealthHints(card.Health)
	card.Metadata = copyAnyMap(card.Metadata)
	return card
}

func copyAgentCards(cards []AgentCard) []AgentCard {
	if len(cards) == 0 {
		return nil
	}
	out := make([]AgentCard, len(cards))
	for i, card := range cards {
		out[i] = copyAgentCard(card)
	}
	return out
}

func copyAgentSkills(skills []AgentSkill) []AgentSkill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]AgentSkill, len(skills))
	for i, skill := range skills {
		out[i] = skill
		out[i].InputSchema = copyA2AJSONSchema(skill.InputSchema)
		out[i].OutputSchema = copyA2AJSONSchema(skill.OutputSchema)
		out[i].Metadata = copyAnyMap(skill.Metadata)
	}
	return out
}

func copyAuthRequirement(auth *AuthRequirement) *AuthRequirement {
	if auth == nil {
		return nil
	}
	out := *auth
	out.Schemes = append([]string(nil), auth.Schemes...)
	out.Scopes = append([]string(nil), auth.Scopes...)
	out.Metadata = copyAnyMap(auth.Metadata)
	return &out
}

func copyHealthHints(hints *HealthHints) *HealthHints {
	if hints == nil {
		return nil
	}
	out := *hints
	return &out
}

func copyAuth(auth Auth) Auth {
	auth.Metadata = copyAnyMap(auth.Metadata)
	return auth
}

func copyEvents(events []gopact.Event) []gopact.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]gopact.Event, len(events))
	copy(out, events)
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = copyAnyValue(value)
	}
	return out
}

func copyA2AJSONSchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(in))
	for key, value := range in {
		out[key] = copyAnyValue(value)
	}
	return out
}

func copyAnyValue(value any) any {
	switch v := value.(type) {
	case gopact.JSONSchema:
		return copyA2AJSONSchema(v)
	case map[string]any:
		return copyAnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = copyAnyValue(item)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}
