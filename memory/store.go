// Package memory provides long-term memory store contracts and in-memory defaults.
package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNotFound = errors.New("memory: not found")
	ErrInvalid  = errors.New("memory: invalid memory")
)

// ID is a stable memory identifier.
type ID string

// Type classifies memory content.
type Type string

const (
	TypeSemantic   Type = "semantic"
	TypeEpisodic   Type = "episodic"
	TypeProcedural Type = "procedural"
	TypeProfile    Type = "profile"
)

// Scope controls where memory can be recalled.
type Scope struct {
	UserID    string
	SessionID string
	ThreadID  string
	AgentID   string
	AppID     string
}

// Memory is a recallable long-term memory item.
type Memory struct {
	ID        ID
	Scope     Scope
	Type      Type
	Content   string
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Query searches memory by scope, type, and text.
type Query struct {
	Scope Scope
	Types []Type
	Text  string
	Limit int
}

// SearchResult is the result of a memory search.
type SearchResult struct {
	Memories []Memory
}

// Store is the memory store contract.
type Store interface {
	Put(ctx context.Context, memory Memory) (ID, error)
	Get(ctx context.Context, id ID) (Memory, error)
	Search(ctx context.Context, query Query) (SearchResult, error)
	Delete(ctx context.Context, id ID) error
}

// InMemory is a process-local memory store for tests and local development.
type InMemory struct {
	mu    sync.RWMutex
	seq   atomic.Uint64
	items map[ID]Memory
}

// New creates an empty in-memory store.
func New() *InMemory {
	return &InMemory{items: make(map[ID]Memory)}
}

func (s *InMemory) Put(ctx context.Context, memory Memory) (ID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validate(memory); err != nil {
		return "", err
	}

	now := time.Now()
	if memory.ID == "" {
		memory.ID = ID(fmt.Sprintf("mem-%d", s.seq.Add(1)))
	}
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	memory.UpdatedAt = now
	memory.Metadata = copyMetadata(memory.Metadata)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.items == nil {
		s.items = make(map[ID]Memory)
	}
	s.items[memory.ID] = memory
	return memory.ID, nil
}

func (s *InMemory) Get(ctx context.Context, id ID) (Memory, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	if s == nil {
		return Memory{}, ErrNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	memory, ok := s.items[id]
	if !ok {
		return Memory{}, ErrNotFound
	}
	return copyMemory(memory), nil
}

func (s *InMemory) Search(ctx context.Context, query Query) (SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return SearchResult{}, err
	}
	if s == nil {
		return SearchResult{}, nil
	}

	typeSet := make(map[Type]struct{}, len(query.Types))
	for _, typ := range query.Types {
		typeSet[typ] = struct{}{}
	}
	text := strings.ToLower(strings.TrimSpace(query.Text))

	s.mu.RLock()
	memories := make([]Memory, 0, len(s.items))
	for _, memory := range s.items {
		if !scopeMatches(query.Scope, memory.Scope) {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[memory.Type]; !ok {
				continue
			}
		}
		if text != "" && !strings.Contains(strings.ToLower(memory.Content), text) {
			continue
		}
		memories = append(memories, copyMemory(memory))
	}
	s.mu.RUnlock()

	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt.Before(memories[j].CreatedAt)
	})
	if query.Limit > 0 && len(memories) > query.Limit {
		memories = memories[:query.Limit]
	}
	return SearchResult{Memories: memories}, nil
}

func (s *InMemory) Delete(ctx context.Context, id ID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}

func validate(memory Memory) error {
	if !memory.Type.valid() {
		return fmt.Errorf("%w: invalid type %q", ErrInvalid, memory.Type)
	}
	if strings.TrimSpace(memory.Content) == "" {
		return fmt.Errorf("%w: content is required", ErrInvalid)
	}
	return nil
}

func (t Type) valid() bool {
	switch t {
	case TypeSemantic, TypeEpisodic, TypeProcedural, TypeProfile:
		return true
	default:
		return false
	}
}

func scopeMatches(query Scope, scope Scope) bool {
	if query.UserID != "" && query.UserID != scope.UserID {
		return false
	}
	if query.SessionID != "" && query.SessionID != scope.SessionID {
		return false
	}
	if query.ThreadID != "" && query.ThreadID != scope.ThreadID {
		return false
	}
	if query.AgentID != "" && query.AgentID != scope.AgentID {
		return false
	}
	if query.AppID != "" && query.AppID != scope.AppID {
		return false
	}
	return true
}

func copyMemory(memory Memory) Memory {
	memory.Metadata = copyMetadata(memory.Metadata)
	return memory
}

func copyMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
