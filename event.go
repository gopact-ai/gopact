package gopact

import "time"

// EventType 标识 agent run 中的重要事件类型。
type EventType string

const (
	EventRunStarted   EventType = "run_started"
	EventModelMessage EventType = "model_message"
	EventToolCall     EventType = "tool_call"
	EventToolResult   EventType = "tool_result"
	EventCheckpoint   EventType = "checkpoint"
	EventInterrupted  EventType = "interrupted"
	EventRunCompleted EventType = "run_completed"
	EventRunFailed    EventType = "run_failed"
)

// Event 是 SDK 级别的执行流记录。
type Event struct {
	Type      EventType      `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	ThreadID  string         `json:"thread_id,omitempty"`
	Node      string         `json:"node,omitempty"`
	Message   *Message       `json:"message,omitempty"`
	ToolCall  *ToolCall      `json:"tool_call,omitempty"`
	Result    *ToolResult    `json:"result,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	Err       error          `json:"-"`
}
