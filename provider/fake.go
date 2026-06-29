package provider

import (
	"context"
	"iter"
	"sync"

	"github.com/gopact-ai/gopact"
)

// Fake is an in-memory provider for tests, examples, and local development.
type Fake struct {
	NameValue     string
	ModelsValue   []ModelInfo
	Response      gopact.ModelResponse
	GenerateError error
	GenerateFunc  func(ctx context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error)

	mu       sync.Mutex
	requests []gopact.ModelRequest
}

// ResponseText creates a simple assistant text response.
func ResponseText(text string) gopact.ModelResponse {
	return gopact.ModelResponse{
		Message: gopact.Message{Role: gopact.RoleAssistant, Content: text},
		Usage:   gopact.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}
}

// Name returns the configured fake provider name.
func (f *Fake) Name() string {
	if f == nil {
		return ""
	}
	return f.NameValue
}

// Models returns a copy of the configured fake model list.
func (f *Fake) Models(ctx context.Context) ([]ModelInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}
	return append([]ModelInfo(nil), f.ModelsValue...), nil
}

// Generate records req and returns the configured fake response.
func (f *Fake) Generate(ctx context.Context, req gopact.ModelRequest, opts ...gopact.ModelOption) (gopact.ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ModelResponse{}, err
	}
	req = gopact.ApplyModelOptions(req, opts...)
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()

	if f.GenerateFunc != nil {
		return f.GenerateFunc(ctx, req)
	}
	if f.GenerateError != nil {
		return gopact.ModelResponse{}, f.GenerateError
	}
	return f.Response, nil
}

// Stream emits the generated fake response as model events.
func (f *Fake) Stream(ctx context.Context, req gopact.ModelRequest, opts ...gopact.ModelOption) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		req = gopact.ApplyModelOptions(req, opts...)
		response, err := f.Generate(ctx, req)
		if err != nil {
			yield(gopact.Event{Type: gopact.EventModelProviderAttemptFailed, IDs: req.IDs, Err: err}, err)
			return
		}
		yield(gopact.Event{Type: gopact.EventModelMessage, IDs: req.IDs, Message: &response.Message, Usage: &response.Usage}, nil)
	}
}

// Requests returns a copy of requests received by this fake.
func (f *Fake) Requests() []gopact.ModelRequest {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gopact.ModelRequest(nil), f.requests...)
}
