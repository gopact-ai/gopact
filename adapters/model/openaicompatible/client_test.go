package openaicompatible

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/provider"
)

func TestClientGeneratePostsChatCompletion(t *testing.T) {
	var gotAuth string
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotModel = body.Model

		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "hello"}}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3}
		}`))
	}))
	defer server.Close()

	client, err := New(Options{
		Provider: "openrouter",
		BaseURL:  server.URL,
		APIKey:   "token",
		Models:   []provider.ModelInfo{{Name: "test-model"}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := client.Generate(context.Background(), gopact.ModelRequest{
		Model:    "test-model",
		Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotModel != "test-model" {
		t.Fatalf("model = %q, want test-model", gotModel)
	}
	if response.Message.Text() != "hello" {
		t.Fatalf("Message.Text() = %q, want hello", response.Message.Text())
	}
	if response.Usage.TotalTokens != 3 {
		t.Fatalf("Usage.TotalTokens = %d, want 3", response.Usage.TotalTokens)
	}
}

func TestClientRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{name: "missing provider", opts: Options{BaseURL: "https://example.com", APIKey: "token"}},
		{name: "missing base url", opts: Options{Provider: "openai", APIKey: "token"}},
		{name: "missing api key", opts: Options{Provider: "openai", BaseURL: "https://example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.opts); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}
}

func TestClientGenerateReturnsProviderErrorForNonOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := New(Options{
		Provider: "openrouter",
		BaseURL:  server.URL,
		APIKey:   "token",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.Generate(context.Background(), gopact.ModelRequest{Model: "test-model"})
	if provider.Classify(err) != provider.ErrorRateLimited {
		t.Fatalf("Classify() = %q, want rate_limited; err = %v", provider.Classify(err), err)
	}
}
