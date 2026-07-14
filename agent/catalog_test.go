package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestCatalogCompilesImmutableDirectory(t *testing.T) {
	catalog := NewCatalog()
	planner := &catalogTestAgent{identity: Identity{
		Name: "planner", Description: "plans work", Version: "v1",
	}}
	if err := catalog.Add(planner); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	reviewTarget := gopact.InvokableFunc[string, int](func(_ context.Context, input string, _ ...gopact.RunOption) (int, error) {
		return len(input), nil
	})
	if err := catalog.AddInvokable(
		Identity{Name: "reviewer", Description: "reviews drafts", Version: "v1"},
		reviewTarget,
		Adapter[string, int]{
			Input: func(_ context.Context, request Request) (string, error) {
				return request.Messages[0].Parts[0].Text, nil
			},
			Output: func(_ context.Context, output int) (Response, error) {
				return Response{Message: gopact.UserMessage(string(rune('0' + output)))}, nil
			},
		},
	); err != nil {
		t.Fatalf("AddInvokable() error = %v", err)
	}
	planner.identity.Name = "mutated"

	directory, err := catalog.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	want := []Identity{
		{Name: "planner", Description: "plans work", Version: "v1"},
		{Name: "reviewer", Description: "reviews drafts", Version: "v1"},
	}
	identities := directory.List()
	if !reflect.DeepEqual(identities, want) {
		t.Fatalf("List() = %+v, want %+v", identities, want)
	}
	identities[0].Name = "caller mutation"
	if directory.List()[0].Name != "planner" {
		t.Fatal("Directory.List() returned aliased identity storage")
	}
	reviewer, ok := directory.Lookup("reviewer")
	if !ok || reviewer.Identity().Name != "reviewer" {
		t.Fatalf("Lookup() = (%+v, %v)", reviewer, ok)
	}
	response, err := reviewer.Invoke(context.Background(), Request{Messages: []gopact.Message{gopact.UserMessage("draft")}})
	if err != nil {
		t.Fatalf("reviewer.Invoke() error = %v", err)
	}
	if response.Message.Parts[0].Text != "5" {
		t.Fatalf("response = %+v, want adapted length", response)
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("Add() after Compile() did not panic")
		}
	}()
	_ = catalog.Add(&catalogTestAgent{identity: Identity{
		Name: "late", Description: "late agent", Version: "v1",
	}})
}

func TestCatalogAdapterForwardsSessionIDAndFiltersExtensions(t *testing.T) {
	var received gopact.RunConfig
	target := gopact.InvokableFunc[string, string](func(_ context.Context, input string, options ...gopact.RunOption) (string, error) {
		received = gopact.ResolveRunOptions(options...)
		return input, nil
	})
	catalog := NewCatalog()
	if err := catalog.AddInvokable(
		Identity{Name: "reviewer", Description: "reviews", Version: "v1"},
		target,
		Adapter[string, string]{
			Input:  func(context.Context, Request) (string, error) { return "input", nil },
			Output: func(context.Context, string) (Response, error) { return Response{}, nil },
		},
	); err != nil {
		t.Fatalf("AddInvokable() error = %v", err)
	}
	directory, err := catalog.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	reviewer, ok := directory.Lookup("reviewer")
	if !ok {
		t.Fatal("Lookup() reviewer = missing")
	}
	sink := gopact.EventSinkFunc(func(context.Context, gopact.Event) error { return nil })
	lineage := gopact.RunLineage{ParentRunID: "parent-run", Depth: 2}
	customIDKind := gopact.IDKind("catalog-custom")
	_, err = reviewer.Invoke(
		context.Background(),
		Request{},
		gopact.WithSessionID("session-explicit"),
		gopact.WithRunID("child-run"),
		gopact.WithRunLineage(lineage),
		gopact.WithIDGenerator(customIDKind, func() (string, error) { return "custom-id", nil }),
		gopact.WithEventSink(sink),
		catalogRunOptionFunc(func(cfg *gopact.RunConfig) {
			cfg.Extensions = map[string]any{"private": true}
		}),
	)
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if received.SessionID != "session-explicit" || received.RunID != "child-run" ||
		received.Lineage != lineage || len(received.EventSinks) != 1 || len(received.Extensions) != 0 {
		t.Fatalf("forwarded config = %+v", received)
	}
	if _, ok := received.IDGenerator(customIDKind); !ok {
		t.Fatal("typed adapter did not forward the custom ID generator")
	}
}

func TestCatalogRejectsInvalidAndDuplicateBindings(t *testing.T) {
	catalog := NewCatalog()
	if err := catalog.Add(&catalogTestAgent{identity: Identity{
		Name: "planner", Description: "plans", Version: "v1",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Add(&catalogTestAgent{identity: Identity{
		Name: "planner", Description: "duplicate", Version: "v2",
	}}); !errors.Is(err, ErrDuplicateAgent) {
		t.Fatalf("duplicate Add() error = %v, want ErrDuplicateAgent", err)
	}
	if err := NewCatalog().Add(&catalogTestAgent{identity: Identity{Name: "invalid"}}); err == nil {
		t.Fatal("invalid Add() error = nil")
	}
	var nilAgent *catalogTestAgent
	if err := NewCatalog().Add(nilAgent); err == nil {
		t.Fatal("nil Add() error = nil")
	}
	if err := NewCatalog().AddInvokable(
		Identity{Name: "reviewer", Description: "reviews", Version: "v1"},
		gopact.InvokableFunc[string, string](nil),
		Adapter[string, string]{},
	); err == nil {
		t.Fatal("invalid AddInvokable() error = nil")
	}
}

func TestDirectoryCompileIsIdempotent(t *testing.T) {
	catalog := NewCatalog()
	if err := catalog.Add(&catalogTestAgent{identity: testIdentity()}); err != nil {
		t.Fatal(err)
	}
	first, err := catalog.Compile()
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Compile() returned different immutable artifacts")
	}
}

type catalogTestAgent struct {
	identity Identity
}

type catalogRunOptionFunc func(*gopact.RunConfig)

func (f catalogRunOptionFunc) ApplyRunOption(config *gopact.RunConfig) { f(config) }

func (agent *catalogTestAgent) Identity() Identity { return agent.identity }

func (agent *catalogTestAgent) Invoke(_ context.Context, request Request, _ ...gopact.RunOption) (Response, error) {
	return Response{Message: request.Messages[0]}, nil
}
