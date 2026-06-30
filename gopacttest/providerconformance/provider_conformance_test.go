package providerconformance

import (
	"context"
	"iter"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/provider"
)

func TestCheckProviderConformancePassesWellBehavedProvider(t *testing.T) {
	modelProvider := &provider.Fake{
		NameValue: "fake-provider",
		ModelsValue: []provider.ModelInfo{
			{Name: "fake-model", Provider: "fake-provider", Capabilities: []provider.Capability{provider.CapabilityStreaming}},
		},
		Response: provider.ResponseText("hello"),
	}
	harness := ProviderConformanceHarness{
		Provider: modelProvider,
		Request: gopact.ModelRequest{
			IDs:   gopact.RuntimeIDs{RunID: "run-1", AgentID: "agent-1", CallID: "call-1"},
			Model: "fake-model",
			Messages: []gopact.Message{
				{Role: gopact.RoleUser, Content: "hi"},
			},
			Tools: []gopact.ToolSpec{
				{Name: "lookup", InputSchema: gopact.JSONSchema{"type": "object"}},
			},
			Metadata: map[string]any{"keep": "original"},
		},
	}

	results := CheckProviderConformance(context.Background(), harness)
	if failed := failedProviderConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckProviderConformance() failed cases: %v", failed)
	}
	RequireProviderConformance(t, harness)
}

func TestCheckProviderConformanceReportsEmptyModelList(t *testing.T) {
	harness := ProviderConformanceHarness{
		Provider: &provider.Fake{NameValue: "empty-provider", Response: provider.ResponseText("hello")},
		Request:  gopact.ModelRequest{Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hi"}}},
	}

	results := CheckProviderConformance(context.Background(), harness)
	if !hasFailedProviderConformanceCase(results, "lists-models") {
		t.Fatalf("CheckProviderConformance() did not report lists-models failure: %+v", results)
	}
}

func TestCheckProviderConformanceReportsRequestMutation(t *testing.T) {
	harness := ProviderConformanceHarness{
		Provider: mutatingProvider{},
		Request: gopact.ModelRequest{
			Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hi"}},
			Metadata: map[string]any{"keep": "original"},
		},
	}

	results := CheckProviderConformance(context.Background(), harness)
	if !hasFailedProviderConformanceCase(results, "does-not-mutate-request") {
		t.Fatalf("CheckProviderConformance() did not report mutation failure: %+v", results)
	}
}

func TestCheckProviderConformanceReportsMissingStreamEvent(t *testing.T) {
	harness := ProviderConformanceHarness{
		Provider: silentStreamProvider{},
		Request:  gopact.ModelRequest{Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hi"}}},
	}

	results := CheckProviderConformance(context.Background(), harness)
	if !hasFailedProviderConformanceCase(results, "streams-events") {
		t.Fatalf("CheckProviderConformance() did not report streams-events failure: %+v", results)
	}
}

func TestCheckProviderConformanceReportsMissingStreamRuntimeIDs(t *testing.T) {
	harness := ProviderConformanceHarness{
		Provider: missingStreamRuntimeIDsProvider{},
		Request:  gopact.ModelRequest{Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hi"}}},
	}

	results := CheckProviderConformance(context.Background(), harness)
	if !hasFailedProviderConformanceCase(results, "streams-events") {
		t.Fatalf("CheckProviderConformance() did not report stream runtime id failure: %+v", results)
	}
}

func failedProviderConformanceCases(results []ProviderConformanceResult) []string {
	var failed []string
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedProviderConformanceCase(results []ProviderConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}

type mutatingProvider struct{}

func (mutatingProvider) Name() string {
	return "mutating-provider"
}

func (mutatingProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []provider.ModelInfo{{Name: "mutating-model", Provider: "mutating-provider"}}, nil
}

func (mutatingProvider) Generate(ctx context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ModelResponse{}, err
	}
	req.Metadata["keep"] = "changed"
	return gopact.ModelResponse{Message: gopact.Message{Role: gopact.RoleAssistant, Content: "hello"}}, nil
}

func (mutatingProvider) Stream(ctx context.Context, req gopact.ModelRequest) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(gopact.Event{Type: gopact.EventModelProviderAttemptFailed, IDs: req.IDs, Err: err}, err)
			return
		}
		message := gopact.Message{Role: gopact.RoleAssistant, Content: "hello"}
		yield(gopact.Event{Type: gopact.EventModelMessage, IDs: req.IDs, Message: &message}, nil)
	}
}

type silentStreamProvider struct{}

func (silentStreamProvider) Name() string {
	return "silent-provider"
}

func (silentStreamProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []provider.ModelInfo{{Name: "silent-model", Provider: "silent-provider"}}, nil
}

func (silentStreamProvider) Generate(ctx context.Context, _ gopact.ModelRequest) (gopact.ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ModelResponse{}, err
	}
	return gopact.ModelResponse{Message: gopact.Message{Role: gopact.RoleAssistant, Content: "hello"}}, nil
}

func (silentStreamProvider) Stream(ctx context.Context, req gopact.ModelRequest) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(gopact.Event{Type: gopact.EventModelProviderAttemptFailed, IDs: req.IDs, Err: err}, err)
		}
	}
}

type missingStreamRuntimeIDsProvider struct{}

func (missingStreamRuntimeIDsProvider) Name() string {
	return "missing-stream-runtime-ids-provider"
}

func (missingStreamRuntimeIDsProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []provider.ModelInfo{{Name: "missing-stream-runtime-ids-model", Provider: "missing-stream-runtime-ids-provider"}}, nil
}

func (missingStreamRuntimeIDsProvider) Generate(ctx context.Context, _ gopact.ModelRequest) (gopact.ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ModelResponse{}, err
	}
	return gopact.ModelResponse{Message: gopact.Message{Role: gopact.RoleAssistant, Content: "hello"}}, nil
}

func (missingStreamRuntimeIDsProvider) Stream(ctx context.Context, _ gopact.ModelRequest) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(gopact.Event{Type: gopact.EventModelProviderAttemptFailed, Err: err}, err)
			return
		}
		message := gopact.Message{Role: gopact.RoleAssistant, Content: "hello"}
		yield(gopact.Event{Type: gopact.EventModelMessage, Message: &message}, nil)
	}
}
