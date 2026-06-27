package gopact

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrChannelEventFailed   = errors.New("gopact: channel event failed")
	ErrChannelEventRequired = errors.New("gopact: channel event evidence is required")
)

const (
	// VerificationCheckChannelEvent is the standard check ID prefix for observed channel events.
	VerificationCheckChannelEvent = "channel-event"

	// VerificationEvidenceTypeChannelEvent is the evidence type for observed channel events.
	VerificationEvidenceTypeChannelEvent = "channel_event"
)

// ChannelEventSnapshot is an already-observed channel event summary. It records
// event identity, action shape, runtime IDs, and payload shape, not raw text or
// raw payload content.
type ChannelEventSnapshot struct {
	ID       string
	Name     string
	Ref      string
	IDs      RuntimeIDs
	Event    ChannelEvent
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordChannelEventCheck records an already-observed channel event as verification evidence.
func RecordChannelEventCheck(recorder *VerificationRecorder, snapshot ChannelEventSnapshot) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if !snapshot.Skipped && !channelEventHasRef(snapshot) {
		return ErrChannelEventRequired
	}

	check := channelEventCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrChannelEventFailed, snapshot.Err)
		}
		return ErrChannelEventFailed
	}
	return nil
}

func channelEventCheck(snapshot ChannelEventSnapshot) VerificationCheck {
	ref := channelEventRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckChannelEvent + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "channel event"
	}
	status := channelEventStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = channelEventSummary(status, snapshot)
	}
	return VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeChannelEvent,
				Ref:      ref,
				Summary:  channelEventEvidenceSummary(status, snapshot),
				Metadata: channelEventEvidenceMetadata(snapshot),
			},
		},
		Metadata: channelEventCheckMetadata(snapshot),
	}
}

func channelEventStatus(snapshot ChannelEventSnapshot) VerificationStatus {
	if snapshot.Skipped {
		return VerificationStatusSkipped
	}
	if snapshot.Err != nil {
		return VerificationStatusFailed
	}
	return VerificationStatusPassed
}

func channelEventSummary(status VerificationStatus, snapshot ChannelEventSnapshot) string {
	switch status {
	case VerificationStatusSkipped:
		return "channel event skipped"
	case VerificationStatusFailed:
		if snapshot.Err != nil {
			return "channel event failed: " + snapshot.Err.Error()
		}
		return "channel event failed"
	default:
		event := snapshot.Event
		switch event.Type {
		case ChannelEventAction:
			if event.Action.Type != "" {
				return "channel action received: " + string(event.Action.Type)
			}
			return "channel action received"
		case ChannelEventCancel:
			return "channel cancel received"
		case ChannelEventMessage:
			return "channel message received"
		default:
			return "channel event received"
		}
	}
}

func channelEventEvidenceSummary(status VerificationStatus, snapshot ChannelEventSnapshot) string {
	if status == VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	event := snapshot.Event
	if event.Channel != "" && event.Type != "" {
		return fmt.Sprintf("%s %s event captured", event.Channel, event.Type)
	}
	if event.Type != "" {
		return fmt.Sprintf("%s event captured", event.Type)
	}
	return "channel event captured"
}

func channelEventCheckMetadata(snapshot ChannelEventSnapshot) map[string]any {
	metadata := channelEventBaseMetadata(snapshot)
	mergeSupplementalVerificationMetadata(metadata, snapshot.Metadata, channelEventReservedMetadataKey)
	return metadata
}

func channelEventEvidenceMetadata(snapshot ChannelEventSnapshot) map[string]any {
	return channelEventCheckMetadata(snapshot)
}

func channelEventBaseMetadata(snapshot ChannelEventSnapshot) map[string]any {
	event := snapshot.Event
	metadata := map[string]any{
		"ref":              channelEventRef(snapshot),
		"text_bytes":       len(event.Text),
		"payload_present":  event.Payload != nil,
		"action_present":   event.Action.Type != "",
		"metadata_key_cnt": len(event.Metadata),
	}
	addRunExportRuntimeIDMetadata(metadata, channelEventRuntimeIDs(snapshot))
	if event.ID != "" {
		metadata["event_id"] = event.ID
	}
	if event.Channel != "" {
		metadata["channel"] = string(event.Channel)
	}
	if event.Type != "" {
		metadata["event_type"] = string(event.Type)
	}
	if event.Payload != nil {
		metadata["payload_type"] = fmt.Sprintf("%T", event.Payload)
	}
	if keys := sortedAnyMapKeys(event.Metadata); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	if !event.CreatedAt.IsZero() {
		metadata["created_at"] = event.CreatedAt.Format(time.RFC3339Nano)
	}
	addChannelActionMetadata(metadata, event.Action)
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func addChannelActionMetadata(metadata map[string]any, action SurfaceAction) {
	if action.ID != "" {
		metadata["action_id"] = action.ID
	}
	if action.Type != "" {
		metadata["action_type"] = string(action.Type)
	}
	if action.InterruptID != "" {
		metadata["interrupt_id"] = action.InterruptID
	}
	if action.CallID != "" {
		metadata["action_call_id"] = action.CallID
	}
	if action.Payload != nil {
		metadata["action_payload_present"] = true
		metadata["action_payload_type"] = fmt.Sprintf("%T", action.Payload)
	}
	if keys := sortedAnyMapKeys(action.Metadata); len(keys) > 0 {
		metadata["action_metadata_keys"] = keys
	}
}

func channelEventReservedMetadataKey(key string) bool {
	if runtimeIDVerificationMetadataKey(key) {
		return true
	}
	switch key {
	case "ref",
		"text_bytes",
		"payload_present",
		"action_present",
		"metadata_key_cnt",
		"event_id",
		"channel",
		"event_type",
		"payload_type",
		"metadata_keys",
		"created_at",
		"error",
		"skipped",
		"action_id",
		"action_type",
		"interrupt_id",
		"action_call_id",
		"action_payload_present",
		"action_payload_type",
		"action_metadata_keys":
		return true
	default:
		return false
	}
}

func channelEventRuntimeIDs(snapshot ChannelEventSnapshot) RuntimeIDs {
	return snapshot.Event.IDs.WithDefaults(snapshot.Event.Action.IDs).WithDefaults(snapshot.IDs)
}

func channelEventHasRef(snapshot ChannelEventSnapshot) bool {
	if snapshot.Ref != "" || snapshot.ID != "" {
		return true
	}
	event := snapshot.Event
	if event.ID != "" || event.Channel != "" || event.Type != "" {
		return true
	}
	if event.Action.ID != "" || event.Action.Type != "" || event.Action.InterruptID != "" || event.Action.CallID != "" {
		return true
	}
	ids := channelEventRuntimeIDs(snapshot)
	return ids.CallID != "" || ids.RunID != ""
}

func channelEventRef(snapshot ChannelEventSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	event := snapshot.Event
	if event.ID != "" {
		return event.ID
	}
	if event.Action.ID != "" {
		return event.Action.ID
	}
	if event.Action.InterruptID != "" {
		return event.Action.InterruptID
	}
	if event.Action.CallID != "" {
		return event.Action.CallID
	}
	ids := channelEventRuntimeIDs(snapshot)
	if ids.CallID != "" {
		return ids.CallID
	}
	if ids.RunID != "" {
		return ids.RunID + ":channel"
	}
	if event.Channel != "" && event.Type != "" {
		return string(event.Channel) + ":" + string(event.Type)
	}
	if event.Type != "" {
		return string(event.Type)
	}
	if event.Channel != "" {
		return string(event.Channel)
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckChannelEvent
}

func sortedAnyMapKeys(in map[string]any) []string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
