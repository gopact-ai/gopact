package agent

import (
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

const (
	maxObservationSummaryRunes = 512
	maxObservationRefs         = 64
)

var (
	// ErrToolOutcomeNotObservable reports a tool fact that must terminate or interrupt the run.
	ErrToolOutcomeNotObservable = errors.New("agent: tool outcome is not model feedback")
	// ErrInvalidObservation reports missing identity or feedback required by an observation.
	ErrInvalidObservation = errors.New("agent: invalid observation")
)

// ObserveToolOutcome converts a tool execution fact into model-facing feedback when allowed.
func ObserveToolOutcome(outcome gopact.ToolOutcome) (Observation, error) {
	switch value := outcome.(type) {
	case gopact.ToolResultOutcome:
		return observeToolResult(value)
	case *gopact.ToolResultOutcome:
		return observeToolResultPointer(value)
	case gopact.ToolRejectedOutcome:
		return observeToolRejection(value)
	case *gopact.ToolRejectedOutcome:
		return observeToolRejectionPointer(value)
	case gopact.ToolErrorOutcome:
		return observeToolError(value)
	case *gopact.ToolErrorOutcome:
		return observeToolErrorPointer(value)
	case gopact.ToolInterruptOutcome, *gopact.ToolInterruptOutcome:
		return Observation{}, fmt.Errorf("%w: tool interrupt", ErrToolOutcomeNotObservable)
	case nil:
		return Observation{}, fmt.Errorf("%w: nil tool outcome", ErrInvalidObservation)
	default:
		return Observation{}, fmt.Errorf("%w: unsupported tool outcome %T", ErrInvalidObservation, outcome)
	}
}

func observeToolResultPointer(outcome *gopact.ToolResultOutcome) (Observation, error) {
	if outcome == nil {
		return Observation{}, fmt.Errorf("%w: nil tool result", ErrInvalidObservation)
	}
	return observeToolResult(*outcome)
}

func observeToolRejectionPointer(outcome *gopact.ToolRejectedOutcome) (Observation, error) {
	if outcome == nil {
		return Observation{}, fmt.Errorf("%w: nil tool rejection", ErrInvalidObservation)
	}
	return observeToolRejection(*outcome)
}

func observeToolErrorPointer(outcome *gopact.ToolErrorOutcome) (Observation, error) {
	if outcome == nil {
		return Observation{}, fmt.Errorf("%w: nil tool error", ErrInvalidObservation)
	}
	return observeToolError(*outcome)
}

// ObserveGuardRejection converts a guard rejection into model-facing feedback.
func ObserveGuardRejection(rejection gopact.GuardRejection) (Observation, error) {
	if rejection.ID == "" || rejection.GuardName == "" {
		return Observation{}, fmt.Errorf("%w: guard rejection identity is required", ErrInvalidObservation)
	}
	text := rejection.Message
	if text == "" {
		text = rejection.Reason
	}
	if text == "" {
		return Observation{}, fmt.Errorf("%w: guard rejection feedback is required", ErrInvalidObservation)
	}
	return Observation{
		ID:     rejection.ID,
		Kind:   ObservationGuardRejected,
		Source: ObservationSource{Kind: ObservationSourceGuardRejection, ID: rejection.ID},
		Subject: ObservationSubject{
			GuardName:  rejection.GuardName,
			SubjectRef: rejection.SubjectRef,
		},
		Message:   feedbackMessage("system", text),
		Summary:   boundedSummary(text),
		RetryHint: cloneRetryHint(rejection.RetryHint),
	}, nil
}

// ObserveRepairRequest converts a model repair request into feedback for a later turn.
func ObserveRepairRequest(id string, request gopact.RepairRequest) (Observation, error) {
	if id == "" {
		return Observation{}, fmt.Errorf("%w: repair observation id is required", ErrInvalidObservation)
	}
	message := cloneMessage(request.Message)
	if len(message.Parts) == 0 {
		if request.Reason == "" {
			return Observation{}, fmt.Errorf("%w: repair feedback is required", ErrInvalidObservation)
		}
		message = feedbackMessage("system", request.Reason)
	}
	return Observation{
		ID:      id,
		Kind:    ObservationModelFeedback,
		Source:  ObservationSource{Kind: ObservationSourceModelFeedback, ID: id},
		Subject: ObservationSubject{SubjectRef: request.Ref},
		Message: message,
		Summary: boundedSummary(request.Reason),
	}, nil
}

func observeToolResult(outcome gopact.ToolResultOutcome) (Observation, error) {
	if err := validateToolIdentity(outcome.CallID, outcome.Name); err != nil {
		return Observation{}, err
	}
	text := outcome.Result.Preview
	if text == "" {
		text = "tool " + outcome.Name + " completed"
	}
	refs := appendRefs(outcome.Result.ArtifactRefs, outcome.Result.EffectRefs)
	return (toolObservationRequest{callID: outcome.CallID, name: outcome.Name, kind: ObservationToolResult, text: text, refs: refs}).build(), nil
}

func observeToolRejection(outcome gopact.ToolRejectedOutcome) (Observation, error) {
	if err := validateToolIdentity(outcome.CallID, outcome.Name); err != nil {
		return Observation{}, err
	}
	text := outcome.Rejection.Message
	if text == "" {
		text = outcome.Rejection.Reason
	}
	if text == "" {
		return Observation{}, fmt.Errorf("%w: tool rejection feedback is required", ErrInvalidObservation)
	}
	return (toolObservationRequest{callID: outcome.CallID, name: outcome.Name, kind: ObservationToolRejected, text: text, retry: outcome.Rejection.RetryHint}).build(), nil
}

func observeToolError(outcome gopact.ToolErrorOutcome) (Observation, error) {
	if err := validateToolIdentity(outcome.CallID, outcome.Name); err != nil {
		return Observation{}, err
	}
	if !outcome.Error.RetryableForModel && outcome.Error.Feedback == "" {
		return Observation{}, fmt.Errorf("%w: %s", ErrToolOutcomeNotObservable, outcome.Error.Kind)
	}
	text := outcome.Error.Feedback
	if text == "" {
		text = outcome.Error.Message
	}
	if text == "" {
		return Observation{}, fmt.Errorf("%w: tool error feedback is required", ErrInvalidObservation)
	}
	retry := &gopact.RetryHint{Retryable: outcome.Error.RetryableForModel, Message: text}
	return (toolObservationRequest{callID: outcome.CallID, name: outcome.Name, kind: ObservationToolError, text: text, refs: outcome.Error.PartialRefs, retry: retry}).build(), nil
}

type toolObservationRequest struct {
	callID string
	name   string
	kind   ObservationKind
	text   string
	refs   []gopact.ArtifactRef
	retry  *gopact.RetryHint
}

func (request toolObservationRequest) build() Observation {
	return Observation{
		ID:        request.callID,
		Kind:      request.kind,
		Source:    ObservationSource{Kind: ObservationSourceToolOutcome, ID: request.callID},
		Subject:   ObservationSubject{ToolCallID: request.callID, ToolName: request.name},
		Message:   feedbackMessage("tool", request.text),
		Summary:   boundedSummary(request.text),
		Refs:      boundedRefs(request.refs),
		RetryHint: cloneRetryHint(request.retry),
	}
}

func validateToolIdentity(callID, name string) error {
	if callID == "" || name == "" {
		return fmt.Errorf("%w: tool call id and name are required", ErrInvalidObservation)
	}
	return nil
}

func feedbackMessage(role, text string) gopact.Message {
	return gopact.Message{Role: role, Parts: []gopact.MessagePart{{Type: "text", Text: text}}}
}

func boundedSummary(value string) string {
	runes := []rune(value)
	if len(runes) <= maxObservationSummaryRunes {
		return value
	}
	return string(runes[:maxObservationSummaryRunes])
}

func appendRefs(groups ...[]gopact.ArtifactRef) []gopact.ArtifactRef {
	var refs []gopact.ArtifactRef
	for _, group := range groups {
		remaining := maxObservationRefs - len(refs)
		if remaining <= 0 {
			break
		}
		if len(group) > remaining {
			group = group[:remaining]
		}
		refs = append(refs, group...)
	}
	return refs
}

func boundedRefs(values []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(values) > maxObservationRefs {
		values = values[:maxObservationRefs]
	}
	return cloneRefs(values)
}
