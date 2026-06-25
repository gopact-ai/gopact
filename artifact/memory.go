// Package artifact provides artifact storage implementations.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/gopact-ai/gopact"
)

// ErrNotFound is returned when an artifact id is not present in the store.
var ErrNotFound = errors.New("artifact: not found")

// Memory stores artifacts in process memory for tests and local development.
type Memory struct {
	mu    sync.RWMutex
	items map[string]gopact.Artifact
	order []string
}

// NewMemory creates an empty in-memory artifact store.
func NewMemory() *Memory {
	return &Memory{items: make(map[string]gopact.Artifact)}
}

// Put stores an artifact and returns its stable reference.
func (m *Memory) Put(ctx context.Context, artifact gopact.Artifact) (gopact.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return gopact.ArtifactRef{}, err
	}
	sum := sha256.Sum256(artifact.Content)
	sha := hex.EncodeToString(sum[:])
	ref := artifact.Ref
	if ref.ID == "" {
		ref.ID = sha
	}
	ref.Size = int64(len(artifact.Content))
	ref.SHA256 = sha
	artifact.Ref = ref
	artifact.Content = append([]byte(nil), artifact.Content...)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.items == nil {
		m.items = make(map[string]gopact.Artifact)
	}
	if _, ok := m.items[ref.ID]; !ok {
		m.order = append(m.order, ref.ID)
	}
	m.items[ref.ID] = artifact
	return ref, nil
}

// Get returns one artifact by id.
func (m *Memory) Get(ctx context.Context, id string) (gopact.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return gopact.Artifact{}, err
	}
	if m == nil {
		return gopact.Artifact{}, ErrNotFound
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	artifact, ok := m.items[id]
	if !ok {
		return gopact.Artifact{}, ErrNotFound
	}
	artifact.Content = append([]byte(nil), artifact.Content...)
	return artifact, nil
}

// List returns artifact refs in insertion order.
func (m *Memory) List(ctx context.Context) ([]gopact.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	refs := make([]gopact.ArtifactRef, 0, len(m.order))
	for _, id := range m.order {
		refs = append(refs, m.items[id].Ref)
	}
	return refs, nil
}
