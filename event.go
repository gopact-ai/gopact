package gopact

import "time"

// EventType 标识 agent run 中的重要事件类型。
type EventType string

const (
	EventRunStarted                    EventType = "run_started"
	EventTurnStarted                   EventType = "turn_started"
	EventTurnInputReceived             EventType = "turn_input_received"
	EventTurnInputMerged               EventType = "turn_input_merged"
	EventTurnResumed                   EventType = "turn_resumed"
	EventTurnPreempted                 EventType = "turn_preempted"
	EventTurnCompleted                 EventType = "turn_completed"
	EventTurnCanceled                  EventType = "turn_canceled"
	EventTurnInterrupted               EventType = "turn_interrupted"
	EventTurnFailed                    EventType = "turn_failed"
	EventNodeStarted                   EventType = "node_started"
	EventNodeResumed                   EventType = "node_resumed"
	EventNodeCompleted                 EventType = "node_completed"
	EventNodeFailed                    EventType = "node_failed"
	EventModelRoutePlanned             EventType = "model_route_planned"
	EventModelProviderAttemptStarted   EventType = "model_provider_attempt_started"
	EventModelProviderAttemptCompleted EventType = "model_provider_attempt_completed"
	EventModelProviderAttemptFailed    EventType = "model_provider_attempt_failed"
	EventModelProviderFallbackStarted  EventType = "model_provider_fallback_started"
	EventModelMessage                  EventType = "model_message"
	EventPolicyRequested               EventType = "policy_requested"
	EventPolicyDecided                 EventType = "policy_decided"
	EventToolRegistered                EventType = "tool_registered"
	EventToolVisibleListed             EventType = "tool_visible_listed"
	EventToolDeferredListed            EventType = "tool_deferred_listed"
	EventToolSearched                  EventType = "tool_searched"
	EventToolPromoted                  EventType = "tool_promoted"
	EventToolVisibilityChanged         EventType = "tool_visibility_changed"
	EventToolCall                      EventType = "tool_call"
	EventToolResult                    EventType = "tool_result"
	EventSandboxCreated                EventType = "sandbox_created"
	EventSandboxExecStarted            EventType = "sandbox_exec_started"
	EventSandboxExecCompleted          EventType = "sandbox_exec_completed"
	EventSandboxExecFailed             EventType = "sandbox_exec_failed"
	EventSandboxFileRead               EventType = "sandbox_file_read"
	EventSandboxFileWritten            EventType = "sandbox_file_written"
	EventSandboxClosed                 EventType = "sandbox_closed"
	EventMemoryPut                     EventType = "memory_put"
	EventMemorySearched                EventType = "memory_searched"
	EventMemoryDeleted                 EventType = "memory_deleted"
	EventSkillRegistered               EventType = "skill_registered"
	EventSkillActivated                EventType = "skill_activated"
	EventMCPServerConnected            EventType = "mcp_server_connected"
	EventMCPToolsListed                EventType = "mcp_tools_listed"
	EventMCPResourcesListed            EventType = "mcp_resources_listed"
	EventMCPPromptsListed              EventType = "mcp_prompts_listed"
	EventA2AAgentRegistered            EventType = "a2a_agent_registered"
	EventA2AAgentCardFetched           EventType = "a2a_agent_card_fetched"
	EventA2AAgentHeartbeat             EventType = "a2a_agent_heartbeat"
	EventA2AAgentEvicted               EventType = "a2a_agent_evicted"
	EventA2ATaskSent                   EventType = "a2a_task_sent"
	EventA2AMessageReceived            EventType = "a2a_message_received"
	EventA2AArtifactUpdated            EventType = "a2a_artifact_updated"
	EventA2ATaskStatusUpdated          EventType = "a2a_task_status_updated"
	EventA2ATaskCompleted              EventType = "a2a_task_completed"
	EventA2ATaskFailed                 EventType = "a2a_task_failed"
	EventA2ATaskCanceled               EventType = "a2a_task_canceled"
	EventSurfaceMessageProjected       EventType = "surface_message_projected"
	EventChannelTransferStarted        EventType = "channel_transfer_started"
	EventChannelTransferCompleted      EventType = "channel_transfer_completed"
	EventChannelTransferFailed         EventType = "channel_transfer_failed"
	EventChannelSendStarted            EventType = "channel_send_started"
	EventChannelSendCompleted          EventType = "channel_send_completed"
	EventChannelSendFailed             EventType = "channel_send_failed"
	EventChannelActionReceived         EventType = "channel_action_received"
	EventChannelActionRejected         EventType = "channel_action_rejected"
	EventCheckpoint                    EventType = "checkpoint"
	EventCheckpointLoaded              EventType = "checkpoint_loaded"
	EventStepImported                  EventType = "step_imported"
	EventInterrupted                   EventType = "interrupted"
	EventResumeReceived                EventType = "resume_received"
	EventRunInterrupted                EventType = "run_interrupted"
	EventRunCompleted                  EventType = "run_completed"
	EventRunFailed                     EventType = "run_failed"
	EventRunCanceled                   EventType = "run_canceled"
)

// Event 是 SDK 级别的执行流记录。
type Event struct {
	Type           EventType       `json:"type"`
	IDs            RuntimeIDs      `json:"ids,omitempty"`
	RunID          string          `json:"run_id,omitempty"`
	ThreadID       string          `json:"thread_id,omitempty"`
	Node           string          `json:"node,omitempty"`
	Step           int             `json:"step,omitempty"`
	StepSnapshot   *StepSnapshot   `json:"step_snapshot,omitempty"`
	ModelRoute     *ModelRoute     `json:"model_route,omitempty"`
	Message        *Message        `json:"message,omitempty"`
	ToolCall       *ToolCall       `json:"tool_call,omitempty"`
	Result         *ToolResult     `json:"result,omitempty"`
	Usage          *Usage          `json:"usage,omitempty"`
	Artifacts      []ArtifactRef   `json:"artifacts,omitempty"`
	PolicyRequest  *PolicyRequest  `json:"policy_request,omitempty"`
	PolicyDecision *PolicyDecision `json:"policy_decision,omitempty"`
	Redaction      RedactionState  `json:"redaction,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	Err            error           `json:"-"`
}

// RuntimeIDs 返回事件身份，旧版 RunID/ThreadID 字段也会作为输入参与合并。
func (e Event) RuntimeIDs() RuntimeIDs {
	ids := e.IDs
	if ids.RunID == "" {
		ids.RunID = e.RunID
	}
	if ids.ThreadID == "" {
		ids.ThreadID = e.ThreadID
	}
	return ids
}

// WithRuntimeDefaults 填充事件缺失身份，并同步旧版 RunID/ThreadID 字段。
func (e Event) WithRuntimeDefaults(defaults RuntimeIDs) Event {
	ids := e.RuntimeIDs().WithDefaults(defaults)
	e.IDs = ids
	e.RunID = ids.RunID
	e.ThreadID = ids.ThreadID
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now()
	}
	return e
}

// Error 返回事件携带的错误文本。
func (e Event) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func now() time.Time {
	return time.Now()
}
