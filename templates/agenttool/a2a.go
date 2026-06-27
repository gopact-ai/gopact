package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/a2a"
)

// A2ATool exposes a remote A2A agent as a gopact.Tool.
type A2ATool struct {
	agent       a2a.Agent
	card        a2a.AgentCard
	inputSchema gopact.JSONSchema
	policy      gopact.Policy
	auth        a2a.Authenticator
	timeout     time.Duration
}

// A2AOption configures an A2A tool adapter.
type A2AOption func(*A2ATool) error

// A2APolicyInput is the stable policy input for an A2A task send.
type A2APolicyInput struct {
	AgentName string        `json:"agent_name"`
	Card      a2a.AgentCard `json:"card"`
	Task      a2a.Task      `json:"task"`
}

// A2ACancelPolicyInput is the stable policy input for an A2A task cancel.
type A2ACancelPolicyInput struct {
	AgentName string        `json:"agent_name"`
	Card      a2a.AgentCard `json:"card"`
	TaskID    string        `json:"task_id"`
}

// NewA2A creates a tool adapter for an A2A agent.
func NewA2A(agent a2a.Agent, opts ...A2AOption) (*A2ATool, error) {
	if agent == nil {
		return nil, ErrAgentRequired
	}
	card := agent.Card()
	if card.Name == "" {
		return nil, ErrNameRequired
	}
	tool := &A2ATool{
		agent:       agent,
		card:        card,
		inputSchema: defaultInputSchema(),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(tool); err != nil {
			return nil, err
		}
	}
	return tool, nil
}

// WithPolicy authorizes each remote A2A send before the task leaves the process.
func WithPolicy(policy gopact.Policy) A2AOption {
	return func(tool *A2ATool) error {
		if policy == nil {
			return errors.New("agenttool: policy is required")
		}
		tool.policy = policy
		return nil
	}
}

// WithCard overrides the agent card, for example with a card discovered by a2a.Registry.
func WithCard(card a2a.AgentCard) A2AOption {
	return func(tool *A2ATool) error {
		if card.Name == "" {
			return ErrNameRequired
		}
		tool.card = copyAgentCard(card)
		return nil
	}
}

// WithAuth injects sanitized authentication context before remote A2A operations.
func WithAuth(auth a2a.Authenticator) A2AOption {
	return func(tool *A2ATool) error {
		if auth == nil {
			return errors.New("agenttool: auth is required")
		}
		tool.auth = auth
		return nil
	}
}

// WithTimeout bounds each remote A2A send.
func WithTimeout(timeout time.Duration) A2AOption {
	return func(tool *A2ATool) error {
		if timeout <= 0 {
			return errors.New("agenttool: timeout must be positive")
		}
		tool.timeout = timeout
		return nil
	}
}

// Spec returns the model-visible tool spec for the remote agent.
func (t *A2ATool) Spec(_ context.Context) (gopact.ToolSpec, error) {
	if t == nil || t.agent == nil {
		return gopact.ToolSpec{}, ErrAgentRequired
	}
	spec, err := SpecFromCard(t.card)
	if err != nil {
		return gopact.ToolSpec{}, err
	}
	spec.InputSchema = copySchema(t.inputSchema)
	return spec, nil
}

// Invoke sends one task to the remote A2A agent and returns the result as a tool result.
func (t *A2ATool) Invoke(ctx context.Context, args json.RawMessage) (gopact.ToolResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if t == nil || t.agent == nil {
		return gopact.ToolResult{}, ErrAgentRequired
	}
	input, err := decodeInput(args)
	if err != nil {
		return gopact.ToolResult{}, err
	}

	ids := childRuntimeIDs(ctx, t.card.Name)
	task := a2a.Task{
		ID:       ids.CallID,
		IDs:      ids,
		Input:    inputText(input),
		Metadata: a2aMetadata(t.card.Name, ids, ids.CallID, input.Metadata),
	}
	sendCtx, _, err := t.authenticate(ctx, gopact.PolicyActionSend, &task, "")
	if err != nil {
		return gopact.ToolResult{
			Metadata: a2aMetadata(t.card.Name, ids, task.ID, nil),
		}, err
	}
	var events []gopact.Event
	policyEvents, policyErr := t.authorize(sendCtx, task)
	events = append(events, policyEvents...)
	if policyErr != nil {
		return gopact.ToolResult{
			Events:   events,
			Metadata: a2aMetadata(t.card.Name, ids, task.ID, nil),
		}, policyErr
	}

	sent := a2aEvent(gopact.EventA2ATaskSent, ids, task.ID, t.card.Name, task.Metadata, nil, nil)
	events = append(events, sent)

	var cancel context.CancelFunc
	if t.timeout > 0 {
		sendCtx, cancel = context.WithTimeout(sendCtx, t.timeout)
		defer cancel()
	}
	result, sendErr := t.agent.Send(sendCtx, task)
	if sendErr != nil {
		failed := a2aEvent(gopact.EventA2ATaskFailed, ids, task.ID, t.card.Name, nil, nil, sendErr)
		events = append(events, failed)
		toolResult := gopact.ToolResult{
			Events:   events,
			Metadata: a2aMetadata(t.card.Name, ids, task.ID, nil),
		}
		return toolResult, fmt.Errorf("agenttool: send a2a task %q to agent %q: %w", task.ID, t.card.Name, sendErr)
	}

	taskID := result.TaskID
	if taskID == "" {
		taskID = task.ID
	}
	artifacts := copyArtifactRefs(result.Artifacts)
	completed := a2aEvent(gopact.EventA2ATaskCompleted, ids, taskID, t.card.Name, result.Metadata, artifacts, nil)
	events = append(events, completed)
	return gopact.ToolResult{
		Content:   result.Output,
		Artifacts: artifacts,
		Events:    events,
		Metadata:  a2aMetadata(t.card.Name, ids, taskID, result.Metadata),
	}, nil
}

// Stream sends one task to a streaming A2A agent and yields status/result events.
func (t *A2ATool) Stream(ctx context.Context, args json.RawMessage) iter.Seq2[gopact.Event, error] {
	return func(yield func(gopact.Event, error) bool) {
		if ctx == nil {
			ctx = context.TODO()
		}
		if t == nil || t.agent == nil {
			yield(gopact.Event{}, ErrAgentRequired)
			return
		}
		input, err := decodeInput(args)
		if err != nil {
			yield(gopact.Event{}, err)
			return
		}

		ids := childRuntimeIDs(ctx, t.card.Name)
		task := a2a.Task{
			ID:       ids.CallID,
			IDs:      ids,
			Input:    inputText(input),
			Metadata: a2aMetadata(t.card.Name, ids, ids.CallID, input.Metadata),
		}
		streamCtx, _, err := t.authenticate(ctx, gopact.PolicyActionSend, &task, "")
		if err != nil {
			yield(gopact.Event{}, err)
			return
		}
		policyEvents, policyErr := t.authorize(streamCtx, task)
		for i, event := range policyEvents {
			eventErr := error(nil)
			if policyErr != nil && i == len(policyEvents)-1 {
				eventErr = policyErr
			}
			if !yield(event, eventErr) || eventErr != nil {
				return
			}
		}
		if policyErr != nil {
			yield(gopact.Event{}, policyErr)
			return
		}

		if !yield(a2aEvent(gopact.EventA2ATaskSent, ids, task.ID, t.card.Name, task.Metadata, nil, nil), nil) {
			return
		}
		streamer, ok := t.agent.(a2a.StreamingAgent)
		if !ok {
			err := a2a.ErrStreamNotSupported
			yield(a2aEvent(gopact.EventA2ATaskFailed, ids, task.ID, t.card.Name, nil, nil, err), err)
			return
		}

		var cancel context.CancelFunc
		if t.timeout > 0 {
			streamCtx, cancel = context.WithTimeout(streamCtx, t.timeout)
			defer cancel()
		}
		for taskEvent, streamErr := range streamer.Stream(streamCtx, task) {
			event := a2aTaskEvent(taskEvent.WithDefaults(task), t.card.Name, ids, streamErr)
			if !yield(event, streamErr) || streamErr != nil {
				return
			}
		}
	}
}

// Cancel cancels one remote A2A task and returns the resulting event evidence.
func (t *A2ATool) Cancel(ctx context.Context, taskID string) (gopact.ToolResult, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if t == nil || t.agent == nil {
		return gopact.ToolResult{}, ErrAgentRequired
	}
	if taskID == "" {
		return gopact.ToolResult{}, a2a.ErrTaskIDRequired
	}

	ids := childRuntimeIDs(ctx, t.card.Name)
	cancelCtx, authMetadata, err := t.authenticate(ctx, gopact.PolicyActionCancel, nil, taskID)
	if err != nil {
		return gopact.ToolResult{
			Metadata: a2aMetadata(t.card.Name, ids, taskID, nil),
		}, err
	}
	var events []gopact.Event
	policyEvents, policyErr := t.authorizeCancel(cancelCtx, taskID, ids)
	events = append(events, policyEvents...)
	if policyErr != nil {
		return gopact.ToolResult{
			Events:   events,
			Metadata: a2aMetadata(t.card.Name, ids, taskID, authMetadata),
		}, policyErr
	}

	if err := t.agent.Cancel(cancelCtx, taskID); err != nil {
		failed := a2aEvent(gopact.EventA2ATaskFailed, ids, taskID, t.card.Name, authMetadata, nil, err)
		events = append(events, failed)
		return gopact.ToolResult{
			Events:   events,
			Metadata: a2aMetadata(t.card.Name, ids, taskID, authMetadata),
		}, fmt.Errorf("agenttool: cancel a2a task %q on agent %q: %w", taskID, t.card.Name, err)
	}

	canceled := a2aEvent(gopact.EventA2ATaskCanceled, ids, taskID, t.card.Name, authMetadata, nil, nil)
	events = append(events, canceled)
	return gopact.ToolResult{
		Events:   events,
		Metadata: a2aMetadata(t.card.Name, ids, taskID, authMetadata),
	}, nil
}

func (t *A2ATool) authorize(ctx context.Context, task a2a.Task) ([]gopact.Event, error) {
	if t.policy == nil {
		return nil, nil
	}
	req := gopact.PolicyRequest{
		IDs:      task.IDs,
		Boundary: gopact.PolicyBoundaryA2A,
		Action:   gopact.PolicyActionSend,
		Input: A2APolicyInput{
			AgentName: t.card.Name,
			Card:      copyAgentCard(t.card),
			Task:      copyTask(task),
		},
		Metadata: a2aMetadata(t.card.Name, task.IDs, task.ID, nil),
	}
	requested := a2aPolicyRequestedEvent(req)
	decision, err := t.policy.Decide(ctx, req)
	if err != nil {
		return []gopact.Event{requested}, fmt.Errorf("agenttool: a2a policy: %w", err)
	}
	decided := a2aPolicyDecidedEvent(req, decision)
	events := []gopact.Event{requested, decided}
	if decision.Action == gopact.PolicyReview {
		return events, gopact.Interrupt(a2aPolicyApprovalRecord(req, decision))
	}
	if !decision.Allowed() {
		return events, &gopact.PolicyDeniedError{Decision: decision, Request: req}
	}
	return events, nil
}

func (t *A2ATool) authenticate(
	ctx context.Context,
	action gopact.PolicyRequestAction,
	task *a2a.Task,
	taskID string,
) (context.Context, map[string]any, error) {
	if t.auth == nil {
		return ctx, nil, nil
	}
	req := a2a.AuthRequest{
		IDs:       childRuntimeIDs(ctx, t.card.Name),
		AgentName: t.card.Name,
		Card:      copyAgentCard(t.card),
		Action:    action,
		TaskID:    taskID,
	}
	if task != nil {
		taskCopy := copyTask(*task)
		req.IDs = task.IDs
		req.Task = &taskCopy
	}
	auth, err := t.auth.Authenticate(ctx, req)
	if err != nil {
		return ctx, nil, fmt.Errorf("agenttool: a2a auth: %w", err)
	}
	if auth.IsZero() {
		return ctx, nil, nil
	}
	auth = copyAuth(auth)
	auditMetadata := authAuditMetadata(auth)
	if task != nil {
		task.Auth = &auth
		task.Metadata = mergeAnyMap(task.Metadata, auditMetadata)
	}
	return a2a.ContextWithAuth(ctx, auth), auditMetadata, nil
}

func (t *A2ATool) authorizeCancel(ctx context.Context, taskID string, ids gopact.RuntimeIDs) ([]gopact.Event, error) {
	if t.policy == nil {
		return nil, nil
	}
	req := gopact.PolicyRequest{
		IDs:      ids,
		Boundary: gopact.PolicyBoundaryA2A,
		Action:   gopact.PolicyActionCancel,
		Input: A2ACancelPolicyInput{
			AgentName: t.card.Name,
			Card:      copyAgentCard(t.card),
			TaskID:    taskID,
		},
		Metadata: a2aMetadata(t.card.Name, ids, taskID, nil),
	}
	requested := a2aPolicyRequestedEvent(req)
	decision, err := t.policy.Decide(ctx, req)
	if err != nil {
		return []gopact.Event{requested}, fmt.Errorf("agenttool: a2a policy: %w", err)
	}
	decided := a2aPolicyDecidedEvent(req, decision)
	events := []gopact.Event{requested, decided}
	if decision.Action == gopact.PolicyReview {
		return events, gopact.Interrupt(a2aPolicyApprovalRecord(req, decision))
	}
	if !decision.Allowed() {
		return events, &gopact.PolicyDeniedError{Decision: decision, Request: req}
	}
	return events, nil
}

func inputText(input Input) string {
	if input.Input != "" {
		return input.Input
	}
	for i := len(input.Messages) - 1; i >= 0; i-- {
		if text := input.Messages[i].Text(); text != "" {
			return text
		}
	}
	return ""
}

func a2aEvent(
	eventType gopact.EventType,
	ids gopact.RuntimeIDs,
	taskID string,
	agentName string,
	metadata map[string]any,
	artifacts []gopact.ArtifactRef,
	err error,
) gopact.Event {
	return gopact.Event{
		Type:      eventType,
		IDs:       ids,
		Artifacts: copyArtifactRefs(artifacts),
		Metadata:  a2aMetadata(agentName, ids, taskID, metadata),
		Err:       err,
	}.WithRuntimeDefaults(ids)
}

func a2aTaskEvent(taskEvent a2a.TaskEvent, agentName string, ids gopact.RuntimeIDs, streamErr error) gopact.Event {
	eventType := gopact.EventA2ATaskStatusUpdated
	artifacts := copyArtifactRefs(taskEvent.Artifacts)
	var result *gopact.ToolResult
	err := taskEvent.Err
	if err == nil {
		err = streamErr
	}
	switch taskEvent.Status {
	case a2a.TaskStatusCompleted:
		eventType = gopact.EventA2ATaskCompleted
		if taskEvent.Result != nil {
			artifacts = copyArtifactRefs(taskEvent.Result.Artifacts)
			result = &gopact.ToolResult{
				Content:   taskEvent.Result.Output,
				Artifacts: artifacts,
				Metadata:  copyAnyMap(taskEvent.Result.Metadata),
			}
		} else {
			artifacts = copyArtifactRefs(taskEvent.Artifacts)
		}
	case a2a.TaskStatusFailed:
		eventType = gopact.EventA2ATaskFailed
	case a2a.TaskStatusCanceled:
		eventType = gopact.EventA2ATaskCanceled
	default:
		if taskEvent.Status == "" {
			switch {
			case len(artifacts) > 0:
				eventType = gopact.EventA2AArtifactUpdated
			case taskEvent.Message != "":
				eventType = gopact.EventA2AMessageReceived
			}
		}
	}
	metadata := a2aStatusMetadata(agentName, taskEvent, ids)
	event := a2aEvent(eventType, taskEvent.IDs.WithDefaults(ids), taskEvent.TaskID, agentName, metadata, artifacts, err)
	event.Result = result
	return event
}

func a2aStatusMetadata(agentName string, taskEvent a2a.TaskEvent, ids gopact.RuntimeIDs) map[string]any {
	metadata := copyAnyMap(taskEvent.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if taskEvent.Result != nil {
		for key, value := range taskEvent.Result.Metadata {
			metadata[key] = value
		}
	}
	if taskEvent.Status != "" {
		metadata["a2a_status"] = string(taskEvent.Status)
	}
	if taskEvent.Message != "" {
		metadata["a2a_message"] = taskEvent.Message
	}
	return a2aMetadata(agentName, ids, taskEvent.TaskID, metadata)
}

func a2aMetadata(agentName string, ids gopact.RuntimeIDs, taskID string, extra map[string]any) map[string]any {
	metadata := copyAnyMap(extra)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["agent_name"] = agentName
	metadata["a2a_task_id"] = taskID
	metadata["child_call_id"] = ids.CallID
	metadata["parent_call_id"] = ids.ParentCallID
	return metadata
}

func a2aPolicyRequestedEvent(req gopact.PolicyRequest) gopact.Event {
	request := copyPolicyRequest(req)
	return gopact.Event{
		Type:          gopact.EventPolicyRequested,
		IDs:           req.IDs,
		RunID:         req.IDs.RunID,
		ThreadID:      req.IDs.ThreadID,
		PolicyRequest: &request,
	}.WithRuntimeDefaults(req.IDs)
}

func a2aPolicyDecidedEvent(req gopact.PolicyRequest, decision gopact.PolicyDecision) gopact.Event {
	request := copyPolicyRequest(req)
	decisionCopy := copyPolicyDecision(decision)
	return gopact.Event{
		Type:           gopact.EventPolicyDecided,
		IDs:            req.IDs,
		RunID:          req.IDs.RunID,
		ThreadID:       req.IDs.ThreadID,
		PolicyRequest:  &request,
		PolicyDecision: &decisionCopy,
	}.WithRuntimeDefaults(req.IDs)
}

func a2aPolicyApprovalRecord(req gopact.PolicyRequest, decision gopact.PolicyDecision) gopact.InterruptRecord {
	reason := decision.Reason
	if reason == "" {
		reason = "a2a policy review required"
	}
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
	return gopact.InterruptRecord{
		ID:         a2aPolicyApprovalID(req),
		Type:       gopact.InterruptApproval,
		Reason:     reason,
		Prompt:     gopact.Message{Role: gopact.RoleAssistant, Content: reason},
		RequiredBy: string(gopact.PolicyBoundaryA2A),
		ResumeSchema: gopact.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"approved": map[string]any{"type": "boolean"},
			},
			"required": []string{"approved"},
		},
		Metadata: metadata,
	}
}

func a2aPolicyApprovalID(req gopact.PolicyRequest) string {
	action := string(req.Action)
	if action == "" {
		action = "review"
	}
	if req.IDs.CallID != "" {
		return "policy:" + req.IDs.CallID + ":a2a:" + action
	}
	if req.IDs.RunID != "" {
		return "policy:" + req.IDs.RunID + ":a2a:" + action
	}
	return "policy:a2a:" + action
}

func copyPolicyRequest(req gopact.PolicyRequest) gopact.PolicyRequest {
	req.Metadata = copyAnyMap(req.Metadata)
	if input, ok := req.Input.(A2APolicyInput); ok {
		req.Input = copyA2APolicyInput(input)
	}
	if input, ok := req.Input.(A2ACancelPolicyInput); ok {
		req.Input = copyA2ACancelPolicyInput(input)
	}
	return req
}

func copyPolicyDecision(decision gopact.PolicyDecision) gopact.PolicyDecision {
	decision.Metadata = copyAnyMap(decision.Metadata)
	return decision
}

func copyA2APolicyInput(input A2APolicyInput) A2APolicyInput {
	return A2APolicyInput{
		AgentName: input.AgentName,
		Card:      copyAgentCard(input.Card),
		Task:      copyTask(input.Task),
	}
}

func copyA2ACancelPolicyInput(input A2ACancelPolicyInput) A2ACancelPolicyInput {
	return A2ACancelPolicyInput{
		AgentName: input.AgentName,
		Card:      copyAgentCard(input.Card),
		TaskID:    input.TaskID,
	}
}

func copyAgentCard(card a2a.AgentCard) a2a.AgentCard {
	card.Capabilities = append([]string(nil), card.Capabilities...)
	card.Metadata = copyAnyMap(card.Metadata)
	return card
}

func copyTask(task a2a.Task) a2a.Task {
	task.Metadata = copyAnyMap(task.Metadata)
	if task.Auth != nil {
		auth := copyAuth(*task.Auth)
		task.Auth = &auth
	}
	return task
}

func copyAuth(auth a2a.Auth) a2a.Auth {
	auth.Metadata = copyAnyMap(auth.Metadata)
	return auth
}

func authAuditMetadata(auth a2a.Auth) map[string]any {
	metadata := make(map[string]any)
	if auth.Scheme != "" {
		metadata["auth_scheme"] = auth.Scheme
	}
	if auth.Principal != "" {
		metadata["auth_principal"] = auth.Principal
	}
	if auth.CredentialRef != "" {
		metadata["auth_credential_ref"] = auth.CredentialRef
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func mergeAnyMap(base map[string]any, overlays ...map[string]any) map[string]any {
	out := copyAnyMap(base)
	for _, overlay := range overlays {
		if len(overlay) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(overlay))
		}
		for key, value := range overlay {
			out[key] = value
		}
	}
	return out
}
