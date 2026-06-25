package provider

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRegistryRegisterResolveAndList(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	fake := &Fake{
		NameValue: "primary",
		ModelsValue: []ModelInfo{
			{Name: "fast", Capabilities: []Capability{CapabilityToolCalling}},
		},
	}

	if err := registry.Register(ctx, fake); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, ok := registry.Resolve(ctx, "primary")
	if !ok {
		t.Fatal("Resolve() ok = false, want true")
	}
	if got.Name() != "primary" {
		t.Fatalf("resolved provider = %q, want primary", got.Name())
	}

	infos, err := registry.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "primary" {
		t.Fatalf("List() = %+v", infos)
	}
}

func TestRegistryRejectsInvalidProviders(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	tests := []struct {
		name     string
		provider Provider
	}{
		{name: "nil provider", provider: nil},
		{name: "empty name", provider: &Fake{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := registry.Register(ctx, tt.provider); err == nil {
				t.Fatal("Register() error = nil, want validation error")
			}
		})
	}
}

func TestRegistryRejectsDuplicateProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	fake := &Fake{NameValue: "primary"}

	if err := registry.Register(ctx, fake); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	err := registry.Register(ctx, fake)
	if !errors.Is(err, ErrProviderExists) {
		t.Fatalf("Register() error = %v, want %v", err, ErrProviderExists)
	}
}

func TestFakeProviderRecordsRequests(t *testing.T) {
	ctx := context.Background()
	fake := &Fake{
		NameValue: "primary",
		Response:  ResponseText("hello"),
	}

	got, err := fake.Generate(ctx, modelRequest("fast"))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Message.Text() != "hello" {
		t.Fatalf("Message.Text() = %q, want hello", got.Message.Text())
	}
	requests := fake.Requests()
	if len(requests) != 1 || requests[0].Model != "fast" {
		t.Fatalf("Requests() = %+v", requests)
	}

	requests[0].Model = "mutated"
	again := fake.Requests()
	if reflect.DeepEqual(requests, again) {
		t.Fatal("Requests() returned mutable backing storage")
	}
}
