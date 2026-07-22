// Package provider contains provider-neutral registry and routing helpers.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrProviderNotFound reports a missing provider.
	ErrProviderNotFound = errors.New("provider: not found")
	// ErrNoCandidates reports an empty provider candidate list.
	ErrNoCandidates = errors.New("provider: no candidates")
)

// NamedModel is a model with a stable provider name.
type NamedModel struct {
	Name  string
	Model gopact.Model
}

// Registry stores provider models by name.
type Registry struct {
	mu     sync.RWMutex
	models map[string]gopact.Model
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{models: map[string]gopact.Model{}}
}

// Register stores a provider model.
func (r *Registry) Register(name string, model gopact.Model) error {
	if r == nil {
		return errors.New("provider: registry is nil")
	}
	if name == "" {
		return errors.New("provider: name is required")
	}
	if model == nil {
		return errors.New("provider: model is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[name] = model
	return nil
}

// Get returns a registered provider model.
func (r *Registry) Get(name string) (gopact.Model, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, ok := r.models[name]
	return model, ok
}

// Router selects the first available candidate.
type Router struct {
	registry *Registry
}

// NewRouter creates a router over registry.
func NewRouter(registry *Registry) *Router {
	return &Router{registry: registry}
}

// Invoke tries candidate providers in order.
func (r *Router) Invoke(ctx context.Context, req gopact.ModelRequest, candidates []string, opts ...gopact.ModelCallOption) (gopact.ModelResponse, error) {
	if len(candidates) == 0 {
		return gopact.ModelResponse{}, ErrNoCandidates
	}
	var lastErr error
	for _, name := range candidates {
		model, ok := r.registry.Get(name)
		if !ok {
			lastErr = fmt.Errorf("%w: %s", ErrProviderNotFound, name)
			continue
		}
		resp, err := model.Invoke(ctx, req, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrNoCandidates
	}
	return gopact.ModelResponse{}, lastErr
}

// BasicProvider is a small model implementation for adapters and tests.
type BasicProvider struct {
	defaultRequest gopact.ModelRequest
	invoke         InvokeFunc
}

// InvokeFunc adapts a function to the BasicProvider invoke protocol.
type InvokeFunc func(context.Context, gopact.ModelRequest, ...gopact.ModelCallOption) (gopact.ModelResponse, error)

// NewBasicProvider creates a provider from a default request and invoke function.
func NewBasicProvider(defaults gopact.ModelRequest, invoke InvokeFunc) *BasicProvider {
	return &BasicProvider{defaultRequest: defaults, invoke: invoke}
}

// NewRequest materializes a request from provider defaults.
func (p *BasicProvider) NewRequest(messages ...gopact.Message) gopact.ModelRequest {
	req := p.defaultRequest
	req.Messages = cloneMessages(messages)
	req.Tools = append([]gopact.ToolSpec(nil), p.defaultRequest.Tools...)
	req.Modalities = append([]gopact.Modality(nil), p.defaultRequest.Modalities...)
	req.Stop = append([]string(nil), p.defaultRequest.Stop...)
	req.OutputProtocols = append([]gopact.OutputProtocol(nil), p.defaultRequest.OutputProtocols...)
	if p.defaultRequest.Metadata != nil {
		req.Metadata = map[string]string{}
		for k, v := range p.defaultRequest.Metadata {
			req.Metadata[k] = v
		}
	}
	if p.defaultRequest.Extensions != nil {
		req.Extensions = map[string]any{}
		for k, v := range p.defaultRequest.Extensions {
			req.Extensions[k] = v
		}
	}
	return req
}

func cloneMessages(messages []gopact.Message) []gopact.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]gopact.Message, len(messages))
	for index, message := range messages {
		cloned[index] = message.Clone()
	}
	return cloned
}

// Invoke calls the configured provider function.
func (p *BasicProvider) Invoke(ctx context.Context, req gopact.ModelRequest, opts ...gopact.ModelCallOption) (gopact.ModelResponse, error) {
	if p == nil || p.invoke == nil {
		return gopact.ModelResponse{}, errors.New("provider: invoke function is nil")
	}
	return p.invoke(ctx, req, opts...)
}
