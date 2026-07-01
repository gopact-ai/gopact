package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	// ErrDiscoveryFileRequired is returned when a file discoverer has no file path.
	ErrDiscoveryFileRequired = errors.New("a2a: discovery file is required")
)

// FileDiscoverer looks up agent cards from a local JSON file.
type FileDiscoverer struct {
	path string
}

var _ Discoverer = (*FileDiscoverer)(nil)
var _ CardLister = (*FileDiscoverer)(nil)

// NewFileDiscoverer creates a local file-backed agent card discoverer.
func NewFileDiscoverer(path string) (*FileDiscoverer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrDiscoveryFileRequired
	}
	return &FileDiscoverer{path: path}, nil
}

// ListCards returns all file-backed cards in file order.
func (d *FileDiscoverer) ListCards(ctx context.Context) ([]AgentCard, error) {
	doc, err := d.readDocument(ctx)
	if err != nil {
		return nil, err
	}
	cards := make([]AgentCard, 0, len(doc.Agents))
	for _, card := range doc.Agents {
		if card.Name == "" {
			return nil, ErrCardNameRequired
		}
		cards = append(cards, copyAgentCard(card))
	}
	return cards, nil
}

// Discover returns the first file entry matching the discovery query.
func (d *FileDiscoverer) Discover(ctx context.Context, query DiscoveryQuery) (DiscoveryResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	if d == nil || d.path == "" {
		return DiscoveryResult{}, ErrDiscoveryFileRequired
	}
	if !hasDiscoveryCriteria(query) {
		return DiscoveryResult{}, ErrDiscoveryRequired
	}
	doc, err := d.readDocument(ctx)
	if err != nil {
		return DiscoveryResult{}, err
	}
	for _, card := range doc.Agents {
		if !matchesDiscoveryQuery(card, query) {
			continue
		}
		if card.Name == "" {
			return DiscoveryResult{}, ErrCardNameRequired
		}
		return DiscoveryResult{
			Card:     copyAgentCard(card),
			Metadata: map[string]any{"source": "file"},
		}, nil
	}
	return DiscoveryResult{}, ErrAgentNotFound
}

func (d *FileDiscoverer) readDocument(ctx context.Context) (fileDiscoveryDocument, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return fileDiscoveryDocument{}, err
	}
	if d == nil || d.path == "" {
		return fileDiscoveryDocument{}, ErrDiscoveryFileRequired
	}
	raw, err := os.ReadFile(d.path)
	if err != nil {
		return fileDiscoveryDocument{}, fmt.Errorf("a2a: read discovery file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fileDiscoveryDocument{}, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		var cards []AgentCard
		if err := json.Unmarshal(raw, &cards); err != nil {
			return fileDiscoveryDocument{}, fmt.Errorf("a2a: decode discovery file: %w", err)
		}
		return fileDiscoveryDocument{Agents: cards}, nil
	}
	var doc fileDiscoveryDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fileDiscoveryDocument{}, fmt.Errorf("a2a: decode discovery file: %w", err)
	}
	return doc, nil
}

type fileDiscoveryDocument struct {
	Agents []AgentCard `json:"agents"`
}

func matchesDiscoveryQuery(card AgentCard, query DiscoveryQuery) bool {
	if query.Name != "" && card.Name != query.Name {
		return false
	}
	if query.URL != "" && card.URL != query.URL {
		return false
	}
	if !hasCapabilities(card.Capabilities, query.Require) {
		return false
	}
	if !hasMetadata(card.Metadata, query.Metadata) {
		return false
	}
	return true
}
