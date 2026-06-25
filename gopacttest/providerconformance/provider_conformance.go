// Package providerconformance provides reusable provider contract tests.
package providerconformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/provider"
)

var ErrProviderConformanceFailed = errors.New("gopacttest: provider conformance failed")

// ProviderConformanceHarness describes one Provider implementation under test.
type ProviderConformanceHarness struct {
	Provider provider.Provider
	Request  gopact.ModelRequest
}

// ProviderConformanceResult is the observed result for one provider contract case.
type ProviderConformanceResult struct {
	Case   string
	Passed bool
	Err    error
}

// CheckProviderConformance runs reusable provider contract cases for model adapters.
func CheckProviderConformance(ctx context.Context, harness ProviderConformanceHarness) []ProviderConformanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	request := harness.Request
	if len(request.Messages) == 0 {
		request = defaultProviderRequest()
	}
	if request.Model == "" {
		request.Model = firstConformanceModelName(ctx, harness.Provider)
	}

	return []ProviderConformanceResult{
		checkProviderName(harness.Provider),
		checkProviderListsModels(ctx, harness.Provider),
		checkProviderModelsCanceledContext(harness.Provider),
		checkProviderGenerateCanceledContext(harness.Provider, copyModelRequestForConformance(request)),
		checkProviderGeneratesResponse(ctx, harness.Provider, copyModelRequestForConformance(request)),
		checkProviderDoesNotMutateRequest(ctx, harness.Provider, copyModelRequestForConformance(request)),
		checkProviderStreamsEvents(ctx, harness.Provider, copyModelRequestForConformance(request)),
		checkProviderStreamCanceledContext(harness.Provider, copyModelRequestForConformance(request)),
	}
}

// RequireProviderConformance fails the test unless provider satisfies the provider contract.
func RequireProviderConformance(t testing.TB, harness ProviderConformanceHarness) {
	t.Helper()

	for _, result := range CheckProviderConformance(context.Background(), harness) {
		if !result.Passed {
			t.Fatalf("provider conformance case %q failed: %v", result.Case, result.Err)
		}
	}
}

func checkProviderName(modelProvider provider.Provider) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("has-name", errors.New("provider is nil"))
	}
	if modelProvider.Name() == "" {
		return failedProviderConformance("has-name", errors.New("provider name is empty"))
	}
	return passedProviderConformance("has-name")
}

func checkProviderListsModels(ctx context.Context, modelProvider provider.Provider) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("lists-models", errors.New("provider is nil"))
	}
	models, err := modelProvider.Models(ctx)
	if err != nil {
		return failedProviderConformance("lists-models", err)
	}
	if len(models) == 0 {
		return failedProviderConformance("lists-models", errors.New("provider returned no models"))
	}
	for i, model := range models {
		if model.Name == "" {
			return failedProviderConformance("lists-models", fmt.Errorf("model[%d] name is empty", i))
		}
		if model.Provider != "" && model.Provider != modelProvider.Name() {
			return failedProviderConformance("lists-models", fmt.Errorf("model[%d] provider = %q, want empty or %q", i, model.Provider, modelProvider.Name()))
		}
	}
	return passedProviderConformance("lists-models")
}

func checkProviderModelsCanceledContext(modelProvider provider.Provider) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("models-respects-canceled-context", errors.New("provider is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := modelProvider.Models(ctx)
	if !errors.Is(err, context.Canceled) {
		return failedProviderConformance("models-respects-canceled-context", fmt.Errorf("Models canceled context error = %v, want context.Canceled", err))
	}
	return passedProviderConformance("models-respects-canceled-context")
}

func checkProviderGenerateCanceledContext(modelProvider provider.Provider, request gopact.ModelRequest) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("generate-respects-canceled-context", errors.New("provider is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := modelProvider.Generate(ctx, request)
	if !errors.Is(err, context.Canceled) {
		return failedProviderConformance("generate-respects-canceled-context", fmt.Errorf("Generate canceled context error = %v, want context.Canceled", err))
	}
	return passedProviderConformance("generate-respects-canceled-context")
}

func checkProviderGeneratesResponse(ctx context.Context, modelProvider provider.Provider, request gopact.ModelRequest) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("generates-response", errors.New("provider is nil"))
	}
	response, err := modelProvider.Generate(ctx, request)
	if err != nil {
		return failedProviderConformance("generates-response", err)
	}
	if response.Message.Role != gopact.RoleAssistant {
		return failedProviderConformance("generates-response", fmt.Errorf("response role = %q, want %q", response.Message.Role, gopact.RoleAssistant))
	}
	if response.Message.Text() == "" && len(response.Message.ToolCalls) == 0 && len(response.Message.Parts) == 0 {
		return failedProviderConformance("generates-response", errors.New("response message is empty"))
	}
	return passedProviderConformance("generates-response")
}

func checkProviderDoesNotMutateRequest(ctx context.Context, modelProvider provider.Provider, request gopact.ModelRequest) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("does-not-mutate-request", errors.New("provider is nil"))
	}
	before := copyModelRequestForConformance(request)
	_, err := modelProvider.Generate(ctx, request)
	if err != nil {
		return failedProviderConformance("does-not-mutate-request", err)
	}
	if !reflect.DeepEqual(request, before) {
		return failedProviderConformance("does-not-mutate-request", errors.New("provider mutated input request"))
	}
	return passedProviderConformance("does-not-mutate-request")
}

func checkProviderStreamsEvents(ctx context.Context, modelProvider provider.Provider, request gopact.ModelRequest) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("streams-events", errors.New("provider is nil"))
	}
	for event, err := range modelProvider.Stream(ctx, request) {
		if err != nil {
			return failedProviderConformance("streams-events", err)
		}
		if event.Type == "" {
			return failedProviderConformance("streams-events", errors.New("stream event type is empty"))
		}
		return passedProviderConformance("streams-events")
	}
	return failedProviderConformance("streams-events", errors.New("provider stream ended without events"))
}

func checkProviderStreamCanceledContext(modelProvider provider.Provider, request gopact.ModelRequest) ProviderConformanceResult {
	if modelProvider == nil {
		return failedProviderConformance("stream-respects-canceled-context", errors.New("provider is nil"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range modelProvider.Stream(ctx, request) {
		if errors.Is(err, context.Canceled) {
			return passedProviderConformance("stream-respects-canceled-context")
		}
		if err != nil {
			return failedProviderConformance("stream-respects-canceled-context", fmt.Errorf("Stream canceled context error = %v, want context.Canceled", err))
		}
		return failedProviderConformance("stream-respects-canceled-context", errors.New("Stream yielded an event instead of context.Canceled"))
	}
	return failedProviderConformance("stream-respects-canceled-context", errors.New("Stream ended without context.Canceled"))
}

func passedProviderConformance(name string) ProviderConformanceResult {
	return ProviderConformanceResult{Case: name, Passed: true}
}

func failedProviderConformance(name string, err error) ProviderConformanceResult {
	return ProviderConformanceResult{
		Case:   name,
		Passed: false,
		Err:    errors.Join(ErrProviderConformanceFailed, err),
	}
}

func firstConformanceModelName(ctx context.Context, modelProvider provider.Provider) string {
	if modelProvider == nil {
		return ""
	}
	models, err := modelProvider.Models(ctx)
	if err != nil || len(models) == 0 {
		return ""
	}
	return models[0].Name
}

func defaultProviderRequest() gopact.ModelRequest {
	return gopact.ModelRequest{
		IDs:   gopact.RuntimeIDs{RunID: "gopact-conformance-run", AgentID: "gopact-conformance-agent", CallID: "gopact-conformance-call"},
		Model: "gopact-conformance-model",
		Messages: []gopact.Message{
			{Role: gopact.RoleUser, Content: "gopact conformance"},
		},
		Metadata: map[string]any{"conformance": "provider"},
	}
}

func copyModelRequestForConformance(in gopact.ModelRequest) gopact.ModelRequest {
	out := in
	out.Messages = copyConformanceMessages(in.Messages)
	out.Tools = copyConformanceToolSpecs(in.Tools)
	out.ResponseSchema = copyConformanceJSONSchema(in.ResponseSchema)
	out.Capabilities = append([]gopact.Capability(nil), in.Capabilities...)
	out.Metadata = copyConformanceAnyMap(in.Metadata)
	return out
}

func copyConformanceMessages(in []gopact.Message) []gopact.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.Message, len(in))
	for i, message := range in {
		out[i] = message
		out[i].Parts = append([]gopact.ContentPart(nil), message.Parts...)
		for partIndex := range out[i].Parts {
			out[i].Parts[partIndex].Metadata = copyConformanceAnyMap(message.Parts[partIndex].Metadata)
		}
		out[i].ToolCalls = append([]gopact.ToolCall(nil), message.ToolCalls...)
		for callIndex := range out[i].ToolCalls {
			out[i].ToolCalls[callIndex].Arguments = append([]byte(nil), message.ToolCalls[callIndex].Arguments...)
		}
	}
	return out
}

func copyConformanceToolSpecs(in []gopact.ToolSpec) []gopact.ToolSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ToolSpec, len(in))
	for i, spec := range in {
		out[i] = spec
		out[i].InputSchema = copyConformanceJSONSchema(spec.InputSchema)
	}
	return out
}

func copyConformanceJSONSchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(in))
	for key, value := range in {
		out[key] = copyConformanceAnyValue(value)
	}
	return out
}

func copyConformanceAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = copyConformanceAnyValue(value)
	}
	return out
}

func copyConformanceAnyValue(in any) any {
	switch value := in.(type) {
	case gopact.JSONSchema:
		return copyConformanceJSONSchema(value)
	case map[string]any:
		return copyConformanceAnyMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = copyConformanceAnyValue(item)
		}
		return out
	case []string:
		return append([]string(nil), value...)
	case []int:
		return append([]int(nil), value...)
	case []float64:
		return append([]float64(nil), value...)
	default:
		return value
	}
}
