package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrInvalidIdentity reports an incomplete Agent identity.
	ErrInvalidIdentity = errors.New("agent: invalid identity")
	// ErrDuplicateAgent reports duplicate names in one Catalog.
	ErrDuplicateAgent = errors.New("agent: duplicate agent")
)

// Adapter maps the public Agent protocol to one typed Invokable boundary.
type Adapter[I, O any] struct {
	Input  func(context.Context, Request) (I, error)
	Output func(context.Context, O) (Response, error)
}

// Catalog is the build-time Agent directory builder.
type Catalog struct {
	mu       sync.Mutex
	bindings map[string]*directoryAgent
	order    []string
	compiled *Directory
	frozen   bool
}

// NewCatalog creates an empty directory builder.
func NewCatalog() *Catalog {
	return &Catalog{bindings: make(map[string]*directoryAgent)}
}

// Add registers one public ADK Agent.
func (catalog *Catalog) Add(target Agent) error {
	if catalog == nil {
		return errors.New("agent: catalog is nil")
	}
	if isNilValue(target) {
		return errors.New("agent: catalog target is nil")
	}
	identity := target.Identity()
	if err := validateCatalogIdentity(identity); err != nil {
		return err
	}
	binding := &directoryAgent{
		identity: identity,
		invoke:   target.Invoke,
	}
	return catalog.add(binding)
}

// AddInvokable registers one typed Invokable through an explicit protocol adapter.
func (catalog *Catalog) AddInvokable[I, O any](identity Identity, target gopact.Invokable[I, O], adapter Adapter[I, O]) error {
	if catalog == nil {
		return errors.New("agent: catalog is nil")
	}
	if err := validateCatalogIdentity(identity); err != nil {
		return err
	}
	if isNilValue(target) {
		return errors.New("agent: catalog target is nil")
	}
	if adapter.Input == nil || adapter.Output == nil {
		return errors.New("agent: catalog adapter input and output are required")
	}
	binding := &directoryAgent{
		identity: identity,
		invoke: func(ctx context.Context, request Request, options ...gopact.RunOption) (Response, error) {
			foreignOptions, err := foreignRunOptions(options)
			if err != nil {
				return Response{}, err
			}
			input, err := adapter.Input(ctx, cloneRequest(request))
			if err != nil {
				return Response{}, fmt.Errorf("agent: adapt %q input: %w", identity.Name, err)
			}
			output, err := target.Invoke(ctx, input, foreignOptions...)
			if err != nil {
				return Response{}, fmt.Errorf("agent: invoke %q: %w", identity.Name, err)
			}
			response, err := adapter.Output(ctx, output)
			if err != nil {
				return Response{}, fmt.Errorf("agent: adapt %q output: %w", identity.Name, err)
			}
			return cloneResponse(response), nil
		},
	}
	return catalog.add(binding)
}

func foreignRunOptions(options []gopact.RunOption) ([]gopact.RunOption, error) {
	config := gopact.ResolveRunOptions(options...)
	if err := config.RunConfigError(); err != nil {
		return nil, err
	}
	filtered := make([]gopact.RunOption, 0, len(config.EventSinks))
	if config.SessionID != "" {
		filtered = append(filtered, gopact.WithSessionID(config.SessionID))
	}
	if config.RunID != "" {
		filtered = append(filtered, gopact.WithRunID(config.RunID))
	}
	if config.Lineage != (gopact.RunLineage{}) {
		filtered = append(filtered, gopact.WithRunLineage(config.Lineage))
	}
	for _, sink := range config.EventSinks {
		filtered = append(filtered, gopact.WithEventSink(sink))
	}
	return filtered, nil
}

// Compile freezes this builder and returns its immutable Directory.
func (catalog *Catalog) Compile() (*Directory, error) {
	if catalog == nil {
		return nil, errors.New("agent: catalog is nil")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if catalog.compiled != nil {
		return catalog.compiled, nil
	}
	bindings := make(map[string]Agent, len(catalog.bindings))
	identities := make([]Identity, 0, len(catalog.order))
	for _, name := range catalog.order {
		binding := catalog.bindings[name]
		bindings[name] = binding
		identities = append(identities, binding.identity)
	}
	catalog.frozen = true
	catalog.compiled = &Directory{bindings: bindings, identities: identities}
	return catalog.compiled, nil
}

func (catalog *Catalog) add(binding *directoryAgent) error {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if catalog.frozen {
		panic("agent: catalog already compiled")
	}
	if catalog.bindings == nil {
		catalog.bindings = make(map[string]*directoryAgent)
	}
	if _, exists := catalog.bindings[binding.identity.Name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateAgent, binding.identity.Name)
	}
	catalog.bindings[binding.identity.Name] = binding
	catalog.order = append(catalog.order, binding.identity.Name)
	return nil
}

// Directory is an immutable runtime Agent lookup artifact.
type Directory struct {
	bindings   map[string]Agent
	identities []Identity
}

// Lookup returns the Agent binding for name.
func (directory *Directory) Lookup(name string) (Agent, bool) {
	if directory == nil || name == "" {
		return nil, false
	}
	binding, ok := directory.bindings[name]
	return binding, ok
}

// List returns identities in Catalog insertion order.
func (directory *Directory) List() []Identity {
	if directory == nil {
		return nil
	}
	return append([]Identity(nil), directory.identities...)
}

type directoryAgent struct {
	identity Identity
	invoke   func(context.Context, Request, ...gopact.RunOption) (Response, error)
}

func (agent *directoryAgent) Identity() Identity {
	if agent == nil {
		return Identity{}
	}
	return agent.identity
}

func (agent *directoryAgent) Invoke(ctx context.Context, request Request, options ...gopact.RunOption) (Response, error) {
	if agent == nil || agent.invoke == nil {
		return Response{}, errors.New("agent: directory binding is nil")
	}
	response, err := agent.invoke(ctx, cloneRequest(request), options...)
	return cloneResponse(response), err
}

func validateCatalogIdentity(identity Identity) error {
	if identity.Name == "" || identity.Description == "" || identity.Version == "" {
		return fmt.Errorf("%w: name, description, and version are required", ErrInvalidIdentity)
	}
	return nil
}

func cloneRequest(request Request) Request {
	request.Messages = cloneMessages(request.Messages)
	request.Artifacts = cloneRefs(request.Artifacts)
	request.Metadata = cloneStringMap(request.Metadata)
	return request
}

func cloneResponse(response Response) Response {
	response.Message = cloneMessage(response.Message)
	response.Artifacts = cloneRefs(response.Artifacts)
	response.Metadata = cloneStringMap(response.Metadata)
	return response
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
