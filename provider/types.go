// Package provider contains provider-neutral model routing contracts and defaults.
package provider

import (
	"context"
	"iter"
	"time"

	"github.com/gopact-ai/gopact"
)

// Capability aliases the root model capability contract for convenient provider package use.
type Capability = gopact.Capability

const (
	// Provider capability aliases mirror the root gopact capability constants.
	CapabilityToolCalling      = gopact.CapabilityToolCalling
	CapabilityStreaming        = gopact.CapabilityStreaming
	CapabilityJSONSchema       = gopact.CapabilityJSONSchema
	CapabilityVision           = gopact.CapabilityVision
	CapabilityReasoning        = gopact.CapabilityReasoning
	CapabilityStructuredOutput = gopact.CapabilityStructuredOutput
)

// Provider is the core model provider adapter contract.
type Provider interface {
	Name() string
	Models(ctx context.Context) ([]ModelInfo, error)
	Generate(ctx context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error)
	Stream(ctx context.Context, req gopact.ModelRequest) iter.Seq2[gopact.Event, error]
}

// Info describes a registered provider.
type Info struct {
	Name     string         `json:"name"`
	Models   []ModelInfo    `json:"models,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModelInfo describes one model exposed by a provider.
type ModelInfo struct {
	Name          string         `json:"name"`
	Provider      string         `json:"provider,omitempty"`
	Capabilities  []Capability   `json:"capabilities,omitempty"`
	ContextWindow int            `json:"context_window,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// Candidate identifies one provider/model endpoint candidate.
type Candidate struct {
	Provider string         `json:"provider"`
	Model    string         `json:"model"`
	Endpoint string         `json:"endpoint,omitempty"`
	Weight   float64        `json:"weight,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Route maps a request class to ordered provider/model candidates.
type Route struct {
	Name       string         `json:"name"`
	Require    []Capability   `json:"require,omitempty"`
	Candidates []Candidate    `json:"candidates"`
	Fallback   FallbackPolicy `json:"fallback,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// RouteSet is the typed route configuration injected by the application.
type RouteSet struct {
	Default       string         `json:"default,omitempty"`
	Routes        []Route        `json:"routes"`
	ConfigVersion string         `json:"config_version,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// FallbackPolicy controls when the router may switch candidates.
type FallbackPolicy struct {
	OnErrors    []ErrorClass   `json:"on_errors,omitempty"`
	MaxAttempts int            `json:"max_attempts,omitempty"`
	Backoff     time.Duration  `json:"backoff,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// RouteRequest is the input for planning one route attempt chain.
type RouteRequest struct {
	IDs       gopact.RuntimeIDs   `json:"ids,omitempty"`
	Request   gopact.ModelRequest `json:"request"`
	Hints     Hints               `json:"hints,omitempty"`
	Attempt   int                 `json:"attempt,omitempty"`
	LastError error               `json:"-"`
	Metadata  map[string]any      `json:"metadata,omitempty"`
}

// Hints carries application-level route selection hints.
type Hints struct {
	Route string         `json:"route,omitempty"`
	Task  string         `json:"task,omitempty"`
	Tier  string         `json:"tier,omitempty"`
	Needs []Capability   `json:"needs,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

// RoutePlan is an auditable route decision.
type RoutePlan struct {
	RouteName     string         `json:"route_name,omitempty"`
	Primary       Candidate      `json:"primary"`
	Fallbacks     []Candidate    `json:"fallbacks,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	ConfigVersion string         `json:"config_version,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// RetryDecision is an explicit decision to retry the same candidate.
type RetryDecision struct {
	Retry       bool                 `json:"retry"`
	Backoff     time.Duration        `json:"backoff,omitempty"`
	NextRequest *gopact.ModelRequest `json:"next_request,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	Metadata    map[string]any       `json:"metadata,omitempty"`
}

// FailoverDecision is an explicit decision to switch to another candidate.
type FailoverDecision struct {
	UseFallback bool                 `json:"use_fallback"`
	Candidate   Candidate            `json:"candidate,omitempty"`
	NextRequest *gopact.ModelRequest `json:"next_request,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	Metadata    map[string]any       `json:"metadata,omitempty"`
}
