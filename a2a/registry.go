// Package a2a provides minimal agent-to-agent contracts.
package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
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
	// ErrDiscoveryRequired is returned when a discovery query has no name or URL.
	ErrDiscoveryRequired = errors.New("a2a: discovery name or url is required")
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
	Name         string
	Description  string
	URL          string
	Capabilities []string
	Metadata     map[string]any
}

// DiscoveryQuery identifies an agent card lookup.
type DiscoveryQuery struct {
	Name     string            `json:"name,omitempty"`
	URL      string            `json:"url,omitempty"`
	IDs      gopact.RuntimeIDs `json:"ids,omitempty"`
	Metadata map[string]any    `json:"metadata,omitempty"`
}

// DiscoveryResult records a discovered agent card and fetch evidence.
type DiscoveryResult struct {
	Card     AgentCard      `json:"card"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Events   []gopact.Event `json:"events,omitempty"`
}

// Discoverer looks up remote agent cards.
type Discoverer interface {
	Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error)
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
	ID       string
	IDs      gopact.RuntimeIDs
	Input    string
	Auth     *Auth
	Metadata map[string]any
}

// Result is an A2A task result.
type Result struct {
	TaskID    string
	Output    string
	Artifacts []gopact.ArtifactRef
	Metadata  map[string]any
}

// TaskEvent is one streaming status or result update for an A2A task.
type TaskEvent struct {
	TaskID    string
	IDs       gopact.RuntimeIDs
	Status    TaskStatus
	Message   string
	Result    *Result
	Artifacts []gopact.ArtifactRef
	Metadata  map[string]any
	Err       error
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if agent == nil {
		return errors.New("a2a: agent is nil")
	}
	card := agent.Card()
	if card.Name == "" {
		return ErrCardNameRequired
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
		return fmt.Errorf("%w: %s", ErrAgentExists, card.Name)
	}
	r.agents[card.Name] = agent
	r.cards[card.Name] = copyAgentCard(card)
	return nil
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
	if query.Name == "" && query.URL == "" {
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

	r.mu.Lock()
	if r.cards == nil {
		r.cards = make(map[string]AgentCard)
	}
	r.cards[result.Card.Name] = copyAgentCard(result.Card)
	r.mu.Unlock()

	return result, nil
}

// Send sends one task to the named registered agent.
func (r *Registry) Send(ctx context.Context, name string, task Task) (Result, error) {
	agent, err := r.resolve(ctx, name)
	if err != nil {
		return Result{}, err
	}
	return agent.Send(ctx, task)
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
		for event, err := range streamer.Stream(ctx, task) {
			event = event.WithDefaults(task)
			if !yield(event, err) || err != nil {
				return
			}
		}
	}
}

// Cancel cancels one task on the named registered agent.
func (r *Registry) Cancel(ctx context.Context, name string, taskID string) error {
	if taskID == "" {
		return ErrTaskIDRequired
	}
	agent, err := r.resolve(ctx, name)
	if err != nil {
		return err
	}
	return agent.Cancel(ctx, taskID)
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

func agentCardFetchedEvent(query DiscoveryQuery, result DiscoveryResult) gopact.Event {
	metadata := copyAnyMap(result.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["agent_name"] = result.Card.Name
	if result.Card.URL != "" {
		metadata["agent_url"] = result.Card.URL
	} else if query.URL != "" {
		metadata["agent_url"] = query.URL
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
	query.Metadata = copyAnyMap(query.Metadata)
	return query
}

func copyAgentCard(card AgentCard) AgentCard {
	card.Capabilities = append([]string(nil), card.Capabilities...)
	card.Metadata = copyAnyMap(card.Metadata)
	return card
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
		out[key] = value
	}
	return out
}
