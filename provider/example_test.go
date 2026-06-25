package provider_test

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/provider"
)

func ExampleRouter_Generate() {
	ctx := context.Background()
	registry := provider.NewRegistry()
	_ = registry.Register(ctx, &provider.Fake{
		NameValue: "fake",
		ModelsValue: []provider.ModelInfo{
			{Name: "fast", Capabilities: []provider.Capability{provider.CapabilityToolCalling}},
		},
		Response: provider.ResponseText("hello"),
	})

	router, err := provider.NewRouter(registry, provider.RouteSet{
		Default: "coding",
		Routes: []provider.Route{
			{
				Name: "coding",
				Candidates: []provider.Candidate{
					{Provider: "fake", Model: "fast"},
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	response, err := router.Generate(ctx, gopact.ModelRequest{
		IDs:      gopact.RuntimeIDs{RunID: "run-1"},
		Messages: []gopact.Message{{Role: gopact.RoleUser, Content: "hi"}},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(response.Route.Provider, response.Route.Model)
	fmt.Println(response.Message.Text())
	// Output:
	// fake fast
	// hello
}
