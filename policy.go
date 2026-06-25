package gopact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// PolicyAction is the outcome of a policy check.
type PolicyAction string

const (
	// PolicyAction values describe policy decisions.
	PolicyAllow  PolicyAction = "allow"
	PolicyDeny   PolicyAction = "deny"
	PolicyReview PolicyAction = "review"
)

// PolicyDecision records whether an operation is allowed, denied, or needs review.
type PolicyDecision struct {
	Action   PolicyAction   `json:"action"`
	Reason   string         `json:"reason,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Allowed reports whether the operation can continue without human review.
func (d PolicyDecision) Allowed() bool {
	return d.Action == PolicyAllow
}

// ErrPolicyDenied is returned when a policy decision blocks an operation.
var ErrPolicyDenied = errors.New("gopact: policy denied")

// PolicyBoundary identifies the execution boundary being checked.
type PolicyBoundary string

const (
	// PolicyBoundary values identify protected runtime boundaries.
	PolicyBoundaryNode     PolicyBoundary = "node"
	PolicyBoundaryModel    PolicyBoundary = "model"
	PolicyBoundaryTool     PolicyBoundary = "tool"
	PolicyBoundaryEvent    PolicyBoundary = "event"
	PolicyBoundaryMemory   PolicyBoundary = "memory"
	PolicyBoundarySandbox  PolicyBoundary = "sandbox"
	PolicyBoundaryArtifact PolicyBoundary = "artifact"
	PolicyBoundaryA2A      PolicyBoundary = "a2a"
	PolicyBoundaryChannel  PolicyBoundary = "channel"
	PolicyBoundaryMCP      PolicyBoundary = "mcp"
	PolicyBoundarySkill    PolicyBoundary = "skill"
	PolicyBoundaryExporter PolicyBoundary = "exporter"
	PolicyBoundaryTurn     PolicyBoundary = "turn"
)

// PolicyRequestAction identifies the operation being authorized.
type PolicyRequestAction string

const (
	// PolicyRequestAction values identify protected runtime operations.
	PolicyActionRun      PolicyRequestAction = "run"
	PolicyActionGenerate PolicyRequestAction = "generate"
	PolicyActionInvoke   PolicyRequestAction = "invoke"
	PolicyActionEmit     PolicyRequestAction = "emit"
	PolicyActionSend     PolicyRequestAction = "send"
	PolicyActionReceive  PolicyRequestAction = "receive"
	PolicyActionConnect  PolicyRequestAction = "connect"
	PolicyActionCancel   PolicyRequestAction = "cancel"
	PolicyActionActivate PolicyRequestAction = "activate"
	PolicyActionExport   PolicyRequestAction = "export"
	PolicyActionCreate   PolicyRequestAction = "create"
	PolicyActionExec     PolicyRequestAction = "exec"
	PolicyActionRead     PolicyRequestAction = "read"
	PolicyActionWrite    PolicyRequestAction = "write"
	PolicyActionPut      PolicyRequestAction = "put"
	PolicyActionGet      PolicyRequestAction = "get"
	PolicyActionSearch   PolicyRequestAction = "search"
	PolicyActionDelete   PolicyRequestAction = "delete"
	PolicyActionList     PolicyRequestAction = "list"
	PolicyActionResume   PolicyRequestAction = "resume"
)

// PolicyRequest is the common input passed to policy checks.
type PolicyRequest struct {
	IDs      RuntimeIDs          `json:"ids,omitempty"`
	Boundary PolicyBoundary      `json:"boundary"`
	Action   PolicyRequestAction `json:"action"`
	Input    any                 `json:"input,omitempty"`
	Metadata map[string]any      `json:"metadata,omitempty"`
}

// Policy authorizes SDK execution boundaries.
type Policy interface {
	Decide(ctx context.Context, req PolicyRequest) (PolicyDecision, error)
}

// PolicyFunc adapts a function into a Policy.
type PolicyFunc func(ctx context.Context, req PolicyRequest) (PolicyDecision, error)

// Decide calls f.
func (f PolicyFunc) Decide(ctx context.Context, req PolicyRequest) (PolicyDecision, error) {
	if f == nil {
		return PolicyDecision{}, errors.New("gopact: policy function is nil")
	}
	return f(ctx, req)
}

// PolicyDeniedError carries the denied decision and request context.
type PolicyDeniedError struct {
	Decision PolicyDecision
	Request  PolicyRequest
}

// Error describes the policy denial.
func (e *PolicyDeniedError) Error() string {
	if e == nil {
		return ErrPolicyDenied.Error()
	}
	if e.Decision.Reason == "" {
		return ErrPolicyDenied.Error()
	}
	return fmt.Sprintf("%s: %s", ErrPolicyDenied, e.Decision.Reason)
}

// Unwrap makes errors.Is(err, ErrPolicyDenied) work.
func (e *PolicyDeniedError) Unwrap() error {
	return ErrPolicyDenied
}

// ToolPolicyInput is the stable policy input for a tool invocation.
type ToolPolicyInput struct {
	Name string          `json:"name"`
	Spec ToolSpec        `json:"spec,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ModelPolicyMiddleware authorizes model generation before calling the provider.
func ModelPolicyMiddleware(policy Policy) ModelHandler {
	return func(c *ModelContext) error {
		if policy == nil {
			return errors.New("gopact: policy is nil")
		}
		if c == nil {
			c = NewModelContext(context.TODO(), ModelContextOptions{})
		}
		req := PolicyRequest{
			IDs:      c.Request.IDs,
			Boundary: PolicyBoundaryModel,
			Action:   PolicyActionGenerate,
			Input:    c.Request,
			Metadata: copyAnyMap(c.Metadata),
		}
		c.AddEvent(policyRequestedEvent(req))
		decision, err := policy.Decide(c.Context, req)
		if err != nil {
			return fmt.Errorf("gopact: model policy: %w", err)
		}
		c.AddEvent(policyDecidedEvent(req, decision))
		if decision.Action == PolicyReview {
			return Interrupt(policyApprovalRecord(req, decision))
		}
		if !decision.Allowed() {
			return &PolicyDeniedError{Decision: decision, Request: req}
		}
		return c.Next()
	}
}

// ToolPolicyMiddleware authorizes a tool invocation before calling the tool.
func ToolPolicyMiddleware(policy Policy) ToolHandler {
	return func(c *ToolContext) error {
		if policy == nil {
			return errors.New("gopact: policy is nil")
		}
		if c == nil {
			c = NewToolContext(context.TODO(), ToolContextOptions{})
		}
		req := PolicyRequest{
			IDs:      c.IDs,
			Boundary: PolicyBoundaryTool,
			Action:   PolicyActionInvoke,
			Input: ToolPolicyInput{
				Name: c.Name,
				Spec: c.Spec,
				Args: append(json.RawMessage(nil), c.Args...),
			},
			Metadata: copyAnyMap(c.Metadata),
		}
		c.AddEvent(policyRequestedEvent(req))
		decision, err := policy.Decide(c.Context, req)
		if err != nil {
			return fmt.Errorf("gopact: tool policy: %w", err)
		}
		c.AddEvent(policyDecidedEvent(req, decision))
		if decision.Action == PolicyReview {
			return Interrupt(policyApprovalRecord(req, decision))
		}
		if !decision.Allowed() {
			return &PolicyDeniedError{Decision: decision, Request: req}
		}
		return c.Next()
	}
}

// NewPolicyRequestedEvent creates the standard policy-requested event.
func NewPolicyRequestedEvent(req PolicyRequest) Event {
	return policyRequestedEvent(req)
}

// NewPolicyDecidedEvent creates the standard policy-decided event.
func NewPolicyDecidedEvent(req PolicyRequest, decision PolicyDecision) Event {
	return policyDecidedEvent(req, decision)
}

// NewPolicyReviewInterrupt creates the standard approval interrupt for a review decision.
func NewPolicyReviewInterrupt(req PolicyRequest, decision PolicyDecision) *InterruptError {
	return Interrupt(policyApprovalRecord(req, decision))
}

func policyRequestedEvent(req PolicyRequest) Event {
	request := copyPolicyRequest(req)
	return Event{
		Type:          EventPolicyRequested,
		IDs:           req.IDs,
		RunID:         req.IDs.RunID,
		ThreadID:      req.IDs.ThreadID,
		PolicyRequest: &request,
		CreatedAt:     now(),
	}
}

func policyDecidedEvent(req PolicyRequest, decision PolicyDecision) Event {
	request := copyPolicyRequest(req)
	decisionCopy := copyPolicyDecision(decision)
	return Event{
		Type:           EventPolicyDecided,
		IDs:            req.IDs,
		RunID:          req.IDs.RunID,
		ThreadID:       req.IDs.ThreadID,
		PolicyRequest:  &request,
		PolicyDecision: &decisionCopy,
		CreatedAt:      now(),
	}
}

func policyApprovalRecord(req PolicyRequest, decision PolicyDecision) InterruptRecord {
	metadata := map[string]any{
		"policy_action":         decision.Action,
		"policy_boundary":       req.Boundary,
		"policy_request_action": req.Action,
	}
	if decision.Reason != "" {
		metadata["policy_reason"] = decision.Reason
	}
	if len(decision.Metadata) > 0 {
		metadata["policy_metadata"] = copyAnyMap(decision.Metadata)
	}
	reason := decision.Reason
	if reason == "" {
		reason = "policy review required"
	}
	return InterruptRecord{
		ID:         policyApprovalID(req),
		Type:       InterruptApproval,
		Reason:     reason,
		Prompt:     Message{Role: RoleAssistant, Content: reason},
		RequiredBy: string(req.Boundary),
		ResumeSchema: JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"approved": map[string]any{"type": "boolean"},
			},
			"required": []string{"approved"},
		},
		Metadata: metadata,
	}
}

func policyApprovalID(req PolicyRequest) string {
	if req.IDs.CallID != "" {
		return "policy:" + req.IDs.CallID
	}
	if req.IDs.RunID != "" {
		return "policy:" + req.IDs.RunID + ":" + string(req.Boundary) + ":" + string(req.Action)
	}
	return "policy:" + string(req.Boundary) + ":" + string(req.Action)
}
