package skill

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRegistryRegisterSearchAndActivate(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	skill := Skill{
		Name:        "repo-review",
		Description: "reviews repository changes",
		Version:     "v1",
		Resources:   []Resource{{Name: "guide", URI: "file://guide.md"}},
	}

	if err := registry.Register(ctx, skill); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	results, err := registry.Search(ctx, Query{Text: "review"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := skillNames(results); !reflect.DeepEqual(got, []string{"repo-review"}) {
		t.Fatalf("Search() names = %v, want [repo-review]", got)
	}

	activation, err := registry.Activate(ctx, "repo-review")
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if activation.Skill.Name != "repo-review" || activation.ActivatedAt.IsZero() {
		t.Fatalf("Activation = %+v", activation)
	}
}

func TestRegistryRejectsInvalidSkill(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	if err := registry.Register(ctx, Skill{}); err == nil {
		t.Fatal("Register() error = nil, want missing name error")
	}
}

func TestRegistryRejectsDuplicateSkill(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	skill := Skill{Name: "repo-review"}

	if err := registry.Register(ctx, skill); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	err := registry.Register(ctx, skill)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("Register() error = %v, want %v", err, ErrExists)
	}
}

func TestRegistryActivateRejectsMissingSkill(t *testing.T) {
	_, err := NewRegistry().Activate(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Activate() error = %v, want %v", err, ErrNotFound)
	}
}

func skillNames(skills []Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}
