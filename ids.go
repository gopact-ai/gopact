package gopact

// RuntimeIDs carries stable identity across users, sessions, runs, agents, and nested calls.
type RuntimeIDs struct {
	UserID       string `json:"user_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	AppID        string `json:"app_id,omitempty"`
	CallID       string `json:"call_id,omitempty"`
	ParentCallID string `json:"parent_call_id,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
}

// WithDefaults returns ids with empty fields filled from defaults.
func (ids RuntimeIDs) WithDefaults(defaults RuntimeIDs) RuntimeIDs {
	if ids.UserID == "" {
		ids.UserID = defaults.UserID
	}
	if ids.SessionID == "" {
		ids.SessionID = defaults.SessionID
	}
	if ids.ThreadID == "" {
		ids.ThreadID = defaults.ThreadID
	}
	if ids.RunID == "" {
		ids.RunID = defaults.RunID
	}
	if ids.AgentID == "" {
		ids.AgentID = defaults.AgentID
	}
	if ids.AppID == "" {
		ids.AppID = defaults.AppID
	}
	if ids.CallID == "" {
		ids.CallID = defaults.CallID
	}
	if ids.ParentCallID == "" {
		ids.ParentCallID = defaults.ParentCallID
	}
	if ids.TraceID == "" {
		ids.TraceID = defaults.TraceID
	}
	return ids
}

// IsZero reports whether no identity field is set.
func (ids RuntimeIDs) IsZero() bool {
	return ids == RuntimeIDs{}
}
