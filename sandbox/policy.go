package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrManagerRequired is returned when a policy manager is created without a manager.
	ErrManagerRequired = errors.New("sandbox: manager is required")
	// ErrPolicyRequired is returned when a policy manager is created without a policy.
	ErrPolicyRequired = errors.New("sandbox: policy is required")
)

// FileInfo is payload-free file metadata passed to policy checks.
type FileInfo struct {
	Path     string         `json:"path,omitempty"`
	MIMEType string         `json:"mime_type,omitempty"`
	Size     int64          `json:"size,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PolicyInput is the stable policy input for sandbox operations.
type PolicyInput struct {
	SessionID  string         `json:"session_id,omitempty"`
	Spec       Spec           `json:"spec,omitempty"`
	Command    []string       `json:"command,omitempty"`
	StdinBytes int            `json:"stdin_bytes,omitempty"`
	Timeout    time.Duration  `json:"timeout,omitempty"`
	Path       string         `json:"path,omitempty"`
	File       FileInfo       `json:"file,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type policyConfig struct {
	ids      gopact.RuntimeIDs
	metadata map[string]any
	sink     gopact.EventSubscriber
}

// PolicyOption configures a policy-wrapped sandbox manager.
type PolicyOption func(*policyConfig)

// WithPolicyIDs sets the runtime ids used in policy requests and events.
func WithPolicyIDs(ids gopact.RuntimeIDs) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.ids = ids
	}
}

// WithPolicyMetadata sets metadata copied into every policy request.
func WithPolicyMetadata(metadata map[string]any) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.metadata = copyAnyMap(metadata)
	}
}

// WithPolicyEventSink publishes policy requested/decided events to sink.
func WithPolicyEventSink(sink gopact.EventSubscriber) PolicyOption {
	return func(cfg *policyConfig) {
		cfg.sink = sink
	}
}

// PolicyManager authorizes sandbox session creation and operations.
type PolicyManager struct {
	next   Manager
	policy gopact.Policy
	cfg    policyConfig
}

// NewPolicyManager wraps a sandbox manager with policy checks.
func NewPolicyManager(next Manager, policy gopact.Policy, opts ...PolicyOption) (*PolicyManager, error) {
	if next == nil {
		return nil, ErrManagerRequired
	}
	if policy == nil {
		return nil, ErrPolicyRequired
	}
	cfg := policyConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PolicyManager{next: next, policy: policy, cfg: cfg}, nil
}

// Create authorizes and starts a sandbox session.
func (m *PolicyManager) Create(ctx context.Context, spec Spec) (Session, error) {
	if err := m.authorize(ctx, gopact.PolicyActionCreate, PolicyInput{Spec: copySpec(spec)}); err != nil {
		return nil, err
	}
	session, err := m.next.Create(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &policySession{next: session, manager: m}, nil
}

func (m *PolicyManager) authorize(ctx context.Context, action gopact.PolicyRequestAction, input PolicyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := gopact.PolicyRequest{
		IDs:      m.cfg.ids,
		Boundary: gopact.PolicyBoundarySandbox,
		Action:   action,
		Input:    input,
		Metadata: copyAnyMap(m.cfg.metadata),
	}
	if err := m.publish(ctx, gopact.NewPolicyRequestedEvent(req)); err != nil {
		return err
	}
	decision, err := m.policy.Decide(ctx, req)
	if err != nil {
		return fmt.Errorf("sandbox: policy: %w", err)
	}
	if err := m.publish(ctx, gopact.NewPolicyDecidedEvent(req, decision)); err != nil {
		return err
	}
	if decision.Action == gopact.PolicyReview {
		return gopact.NewPolicyReviewInterrupt(req, decision)
	}
	if !decision.Allowed() {
		return &gopact.PolicyDeniedError{Decision: decision, Request: req}
	}
	return nil
}

func (m *PolicyManager) publish(ctx context.Context, event gopact.Event) error {
	if m.cfg.sink == nil {
		return nil
	}
	if err := m.cfg.sink(ctx, event); err != nil {
		return fmt.Errorf("sandbox: policy event sink: %w", err)
	}
	return nil
}

type policySession struct {
	next    Session
	manager *PolicyManager
}

func (s *policySession) ID() string {
	return s.next.ID()
}

func (s *policySession) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	input := PolicyInput{
		SessionID:  s.ID(),
		Command:    append([]string(nil), req.Command...),
		StdinBytes: len(req.Stdin),
		Timeout:    req.Timeout,
		Metadata:   copyAnyMap(req.Metadata),
	}
	if err := s.manager.authorize(ctx, gopact.PolicyActionExec, input); err != nil {
		return ExecResult{}, err
	}
	return s.next.Exec(ctx, req)
}

func (s *policySession) ReadFile(ctx context.Context, path string) (File, error) {
	if err := s.manager.authorize(ctx, gopact.PolicyActionRead, PolicyInput{SessionID: s.ID(), Path: path}); err != nil {
		return File{}, err
	}
	return s.next.ReadFile(ctx, path)
}

func (s *policySession) WriteFile(ctx context.Context, file File) error {
	input := PolicyInput{SessionID: s.ID(), File: fileInfo(file), Path: file.Path}
	if err := s.manager.authorize(ctx, gopact.PolicyActionWrite, input); err != nil {
		return err
	}
	return s.next.WriteFile(ctx, file)
}

func (s *policySession) Close(ctx context.Context) error {
	return s.next.Close(ctx)
}

func fileInfo(file File) FileInfo {
	return FileInfo{
		Path:     file.Path,
		MIMEType: file.MIMEType,
		Size:     int64(len(file.Content)),
		Metadata: copyAnyMap(file.Metadata),
	}
}
