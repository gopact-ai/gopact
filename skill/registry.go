// Package skill provides a registry for reusable agent skills.
package skill

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrExists is returned when registering a duplicate skill name.
	ErrExists = errors.New("skill: already exists")
	// ErrNotFound is returned when a requested skill does not exist.
	ErrNotFound = errors.New("skill: not found")
)

// Skill describes reusable procedural knowledge and resources.
type Skill struct {
	Name        string
	Description string
	Version     string
	Resources   []Resource
	Scripts     []Script
	Metadata    map[string]any
}

// Resource is a skill resource reference.
type Resource struct {
	Name     string
	URI      string
	MIMEType string
}

// Script is a declared script. Execution belongs to sandbox integration.
type Script struct {
	Name    string
	Command []string
}

// Query searches skills.
type Query struct {
	Text  string
	Limit int
}

// Activation records a skill activation.
type Activation struct {
	Skill       Skill
	ActivatedAt time.Time
}

// Registry stores skills by name.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Register adds skill to the registry.
func (r *Registry) Register(ctx context.Context, skill Skill) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if skill.Name == "" {
		return errors.New("skill: name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skills == nil {
		r.skills = make(map[string]Skill)
	}
	if _, ok := r.skills[skill.Name]; ok {
		return fmt.Errorf("%w: %s", ErrExists, skill.Name)
	}
	r.skills[skill.Name] = copySkill(skill)
	return nil
}

// Get returns a copy of the registered skill by name.
func (r *Registry) Get(ctx context.Context, name string) (Skill, error) {
	if err := ctx.Err(); err != nil {
		return Skill{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	skill, ok := r.skills[name]
	if !ok {
		return Skill{}, ErrNotFound
	}
	return copySkill(skill), nil
}

// Search returns skills matching query text in stable name order.
func (r *Registry) Search(ctx context.Context, query Query) ([]Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text := strings.ToLower(strings.TrimSpace(query.Text))

	r.mu.RLock()
	skills := make([]Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		if text == "" ||
			strings.Contains(strings.ToLower(skill.Name), text) ||
			strings.Contains(strings.ToLower(skill.Description), text) {
			skills = append(skills, copySkill(skill))
		}
	}
	r.mu.RUnlock()

	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	if query.Limit > 0 && len(skills) > query.Limit {
		skills = skills[:query.Limit]
	}
	return skills, nil
}

// Activate returns a skill activation record for name.
func (r *Registry) Activate(ctx context.Context, name string) (Activation, error) {
	skill, err := r.Get(ctx, name)
	if err != nil {
		return Activation{}, err
	}
	return Activation{Skill: skill, ActivatedAt: time.Now()}, nil
}

func copySkill(skill Skill) Skill {
	skill.Resources = append([]Resource(nil), skill.Resources...)
	skill.Scripts = append([]Script(nil), skill.Scripts...)
	if len(skill.Metadata) > 0 {
		metadata := make(map[string]any, len(skill.Metadata))
		for key, value := range skill.Metadata {
			metadata[key] = value
		}
		skill.Metadata = metadata
	}
	return skill
}
