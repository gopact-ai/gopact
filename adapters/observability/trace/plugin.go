// Package trace provides a small event-to-span observability plugin.
package trace

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gopact-ai/gopact"
)

const (
	// PluginName is the stable plugin name exposed through gopact.PluginHost.
	PluginName = "gopact.trace"

	defaultServiceName = "gopact"
)

// ErrExporterRequired is returned when trace export is requested without an exporter.
var ErrExporterRequired = errors.New("trace: exporter is required")

// SpanKind classifies the runtime boundary that produced a span record.
type SpanKind string

const (
	// SpanKind values identify the runtime boundary that produced a span.
	SpanKindRun        SpanKind = "run"
	SpanKindTurn       SpanKind = "turn"
	SpanKindNode       SpanKind = "node"
	SpanKindModel      SpanKind = "model"
	SpanKindTool       SpanKind = "tool"
	SpanKindPolicy     SpanKind = "policy"
	SpanKindCheckpoint SpanKind = "checkpoint"
	SpanKindA2A        SpanKind = "a2a"
	SpanKindMemory     SpanKind = "memory"
	SpanKindSandbox    SpanKind = "sandbox"
	SpanKindEvent      SpanKind = "event"
)

// SpanStatus is the event-derived lifecycle status for a span record.
type SpanStatus string

const (
	// SpanStatus values identify the lifecycle state inferred for a span.
	SpanStatusUnknown     SpanStatus = ""
	SpanStatusStarted     SpanStatus = "started"
	SpanStatusCompleted   SpanStatus = "completed"
	SpanStatusFailed      SpanStatus = "failed"
	SpanStatusCanceled    SpanStatus = "canceled"
	SpanStatusInterrupted SpanStatus = "interrupted"
)

// SpanRecord is a provider-neutral trace record that exporter adapters can translate.
type SpanRecord struct {
	ServiceName string            `json:"service_name,omitempty"`
	Kind        SpanKind          `json:"kind"`
	Name        string            `json:"name"`
	Status      SpanStatus        `json:"status,omitempty"`
	EventType   gopact.EventType  `json:"event_type"`
	IDs         gopact.RuntimeIDs `json:"ids,omitempty"`
	Node        string            `json:"node,omitempty"`
	Step        int               `json:"step,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
}

// Exporter receives provider-neutral span records.
type Exporter interface {
	ExportSpan(ctx context.Context, span SpanRecord) error
}

// ExporterFunc adapts a function into an Exporter.
type ExporterFunc func(ctx context.Context, span SpanRecord) error

// ExportSpan calls f.
func (f ExporterFunc) ExportSpan(ctx context.Context, span SpanRecord) error {
	if f == nil {
		return ErrExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f(ctx, copySpan(span))
}

// MemoryExporter records span records in memory for tests and local inspection.
type MemoryExporter struct {
	mu    sync.Mutex
	spans []SpanRecord
}

// NewMemoryExporter creates an empty memory exporter.
func NewMemoryExporter() *MemoryExporter {
	return &MemoryExporter{}
}

// ExportSpan records a copied span.
func (e *MemoryExporter) ExportSpan(ctx context.Context, span SpanRecord) error {
	if e == nil {
		return ErrExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, copySpan(span))
	return nil
}

// Spans returns a copy of exported spans.
func (e *MemoryExporter) Spans() []SpanRecord {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]SpanRecord, len(e.spans))
	for i, span := range e.spans {
		out[i] = copySpan(span)
	}
	return out
}

type config struct {
	serviceName string
}

// Option configures the trace plugin.
type Option func(*config)

// WithServiceName sets the service name attached to exported spans.
func WithServiceName(serviceName string) Option {
	return func(cfg *config) {
		if serviceName != "" {
			cfg.serviceName = serviceName
		}
	}
}

// Plugin exports runtime events as trace span records.
type Plugin struct {
	exporter    Exporter
	serviceName string
}

var _ gopact.Plugin = (*Plugin)(nil)
var _ gopact.PluginDescriber = (*Plugin)(nil)

// NewPlugin creates a trace plugin backed by exporter.
func NewPlugin(exporter Exporter, opts ...Option) *Plugin {
	cfg := config{serviceName: defaultServiceName}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Plugin{
		exporter:    exporter,
		serviceName: cfg.serviceName,
	}
}

// Name returns the stable plugin name.
func (p *Plugin) Name() string {
	return PluginName
}

// Descriptor declares trace plugin capabilities.
func (p *Plugin) Descriptor() gopact.PluginDescriptor {
	return gopact.PluginDescriptor{
		Name:         PluginName,
		Version:      "v0.1.0",
		Capabilities: []gopact.PluginCapability{gopact.PluginCapabilityTelemetry},
		Metadata: map[string]string{
			"format":  "span_record",
			"service": p.serviceNameOrDefault(),
		},
	}
}

// Setup registers the event subscriber.
func (p *Plugin) Setup(ctx context.Context, host *gopact.PluginHost) error {
	if p == nil || p.exporter == nil {
		return ErrExporterRequired
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	host.Subscribe(p.exportEvent)
	return nil
}

// Close releases plugin resources.
func (p *Plugin) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	return ctx.Err()
}

func (p *Plugin) exportEvent(ctx context.Context, event gopact.Event) error {
	if p == nil || p.exporter == nil {
		return ErrExporterRequired
	}
	span := spanFromEvent(event)
	span.ServiceName = p.serviceNameOrDefault()
	if err := p.exporter.ExportSpan(ctx, span); err != nil {
		return fmt.Errorf("trace: export span: %w", err)
	}
	return nil
}

func (p *Plugin) serviceNameOrDefault() string {
	if p == nil || p.serviceName == "" {
		return defaultServiceName
	}
	return p.serviceName
}

func spanFromEvent(event gopact.Event) SpanRecord {
	kind := spanKind(event.Type)
	name := spanName(kind, event)
	attributes := spanAttributes(event)
	return SpanRecord{
		Kind:       kind,
		Name:       name,
		Status:     spanStatus(event.Type),
		EventType:  event.Type,
		IDs:        event.RuntimeIDs(),
		Node:       event.Node,
		Step:       event.Step,
		Attributes: attributes,
		Error:      event.Error(),
		CreatedAt:  event.CreatedAt,
	}
}

func spanKind(eventType gopact.EventType) SpanKind {
	switch eventType {
	case gopact.EventRunStarted,
		gopact.EventRunCompleted,
		gopact.EventRunFailed,
		gopact.EventRunCanceled,
		gopact.EventRunInterrupted:
		return SpanKindRun
	case gopact.EventTurnStarted,
		gopact.EventTurnInputReceived,
		gopact.EventTurnInputMerged,
		gopact.EventTurnResumed,
		gopact.EventTurnPreempted,
		gopact.EventTurnCompleted,
		gopact.EventTurnCanceled,
		gopact.EventTurnInterrupted,
		gopact.EventTurnFailed:
		return SpanKindTurn
	case gopact.EventNodeStarted,
		gopact.EventNodeResumed,
		gopact.EventNodeCompleted,
		gopact.EventNodeFailed:
		return SpanKindNode
	case gopact.EventModelRoutePlanned,
		gopact.EventModelProviderAttemptStarted,
		gopact.EventModelProviderAttemptCompleted,
		gopact.EventModelProviderAttemptFailed,
		gopact.EventModelProviderFallbackStarted,
		gopact.EventModelMessage:
		return SpanKindModel
	case gopact.EventToolRegistered,
		gopact.EventToolVisibleListed,
		gopact.EventToolDeferredListed,
		gopact.EventToolSearched,
		gopact.EventToolPromoted,
		gopact.EventToolVisibilityChanged,
		gopact.EventToolCall,
		gopact.EventToolResult:
		return SpanKindTool
	case gopact.EventPolicyRequested,
		gopact.EventPolicyDecided:
		return SpanKindPolicy
	case gopact.EventCheckpoint,
		gopact.EventCheckpointLoaded,
		gopact.EventStepImported,
		gopact.EventResumeReceived,
		gopact.EventInterrupted:
		return SpanKindCheckpoint
	case gopact.EventA2AAgentRegistered,
		gopact.EventA2ATaskSent,
		gopact.EventA2ATaskCompleted,
		gopact.EventA2ATaskFailed:
		return SpanKindA2A
	case gopact.EventMemoryPut,
		gopact.EventMemorySearched,
		gopact.EventMemoryDeleted:
		return SpanKindMemory
	case gopact.EventSandboxCreated,
		gopact.EventSandboxExecStarted,
		gopact.EventSandboxExecCompleted,
		gopact.EventSandboxExecFailed,
		gopact.EventSandboxFileRead,
		gopact.EventSandboxFileWritten,
		gopact.EventSandboxClosed:
		return SpanKindSandbox
	default:
		return SpanKindEvent
	}
}

func spanStatus(eventType gopact.EventType) SpanStatus {
	switch eventType {
	case gopact.EventRunStarted,
		gopact.EventTurnStarted,
		gopact.EventNodeStarted,
		gopact.EventModelProviderAttemptStarted,
		gopact.EventToolCall,
		gopact.EventPolicyRequested,
		gopact.EventSandboxCreated,
		gopact.EventSandboxExecStarted,
		gopact.EventA2ATaskSent:
		return SpanStatusStarted
	case gopact.EventRunCompleted,
		gopact.EventTurnCompleted,
		gopact.EventNodeCompleted,
		gopact.EventModelProviderAttemptCompleted,
		gopact.EventToolResult,
		gopact.EventPolicyDecided,
		gopact.EventSandboxExecCompleted,
		gopact.EventSandboxClosed,
		gopact.EventA2ATaskCompleted:
		return SpanStatusCompleted
	case gopact.EventRunFailed,
		gopact.EventTurnFailed,
		gopact.EventNodeFailed,
		gopact.EventModelProviderAttemptFailed,
		gopact.EventSandboxExecFailed,
		gopact.EventA2ATaskFailed:
		return SpanStatusFailed
	case gopact.EventRunCanceled,
		gopact.EventTurnCanceled:
		return SpanStatusCanceled
	case gopact.EventRunInterrupted,
		gopact.EventTurnInterrupted,
		gopact.EventInterrupted:
		return SpanStatusInterrupted
	default:
		return SpanStatusUnknown
	}
}

func spanName(kind SpanKind, event gopact.Event) string {
	switch kind {
	case SpanKindRun:
		return "run"
	case SpanKindTurn:
		return "turn"
	case SpanKindNode:
		if event.Node != "" {
			return event.Node
		}
		return "node"
	case SpanKindModel:
		if event.ModelRoute != nil && event.ModelRoute.Provider != "" {
			return "model/" + event.ModelRoute.Provider
		}
		return "model"
	case SpanKindTool:
		if event.ToolCall != nil && event.ToolCall.Name != "" {
			return event.ToolCall.Name
		}
		return "tool"
	case SpanKindPolicy:
		if event.PolicyRequest != nil && event.PolicyRequest.Boundary != "" {
			return "policy/" + string(event.PolicyRequest.Boundary)
		}
		return "policy"
	case SpanKindCheckpoint:
		return "checkpoint"
	case SpanKindA2A:
		return "a2a"
	case SpanKindMemory:
		return "memory"
	case SpanKindSandbox:
		return "sandbox"
	default:
		if event.Type != "" {
			return string(event.Type)
		}
		return "event"
	}
}

func spanAttributes(event gopact.Event) map[string]string {
	attributes := map[string]string{
		"event.type": string(event.Type),
	}
	if event.Node != "" {
		attributes["node"] = event.Node
	}
	if event.ModelRoute != nil {
		if event.ModelRoute.RouteName != "" {
			attributes["model.route"] = event.ModelRoute.RouteName
		}
		if event.ModelRoute.Provider != "" {
			attributes["model.provider"] = event.ModelRoute.Provider
		}
		if event.ModelRoute.Model != "" {
			attributes["model.name"] = event.ModelRoute.Model
		}
		if event.ModelRoute.Attempt != 0 {
			attributes["model.attempt"] = strconv.Itoa(event.ModelRoute.Attempt)
		}
	}
	if event.ToolCall != nil && event.ToolCall.Name != "" {
		attributes["tool.name"] = event.ToolCall.Name
	}
	if event.PolicyRequest != nil {
		if event.PolicyRequest.Boundary != "" {
			attributes["policy.boundary"] = string(event.PolicyRequest.Boundary)
		}
		if event.PolicyRequest.Action != "" {
			attributes["policy.action"] = string(event.PolicyRequest.Action)
		}
	}
	if event.PolicyDecision != nil && event.PolicyDecision.Action != "" {
		attributes["policy.decision"] = string(event.PolicyDecision.Action)
	}
	if event.Redaction.Applied {
		attributes["redaction.applied"] = "true"
		attributes["redaction.field_count"] = strconv.Itoa(len(event.Redaction.Fields))
	}
	return attributes
}

func copySpan(in SpanRecord) SpanRecord {
	out := in
	if len(in.Attributes) > 0 {
		out.Attributes = make(map[string]string, len(in.Attributes))
		for key, value := range in.Attributes {
			out.Attributes[key] = value
		}
	}
	return out
}
