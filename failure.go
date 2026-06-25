package gopact

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// FailureKind classifies the boundary most responsible for a failure.
type FailureKind string

const (
	FailureRuntime      FailureKind = "runtime"
	FailureUnknown      FailureKind = "unknown"
	FailureContext      FailureKind = "context"
	FailureModel        FailureKind = "model"
	FailureTool         FailureKind = "tool"
	FailureFeedback     FailureKind = "feedback"
	FailurePolicy       FailureKind = "policy"
	FailureVerification FailureKind = "verification"
	FailureRecovery     FailureKind = "recovery"
	FailureEntropy      FailureKind = "entropy"
	FailureSandbox      FailureKind = "sandbox"
	FailureExternal     FailureKind = "external"
)

const (
	// EventMetadataFailureKind lets a host or template explicitly classify a RunFailed event.
	EventMetadataFailureKind = "failure_kind"
)

// FailureAttribution records a structured explanation for a failed run or step.
type FailureAttribution struct {
	ID        string                 `json:"id"`
	Kind      FailureKind            `json:"kind"`
	IDs       RuntimeIDs             `json:"ids,omitempty"`
	Node      string                 `json:"node,omitempty"`
	Step      int                    `json:"step,omitempty"`
	Summary   string                 `json:"summary,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Evidence  []VerificationEvidence `json:"evidence,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	Metadata  map[string]any         `json:"metadata,omitempty"`
}

// Validate checks whether the failure attribution has stable identity and a cause.
func (a FailureAttribution) Validate() error {
	if a.ID == "" {
		return errors.New("gopact: failure attribution id is required")
	}
	if !a.Kind.valid() {
		return fmt.Errorf("gopact: failure attribution kind %q is invalid", a.Kind)
	}
	if a.Summary == "" && a.Error == "" && len(a.Evidence) == 0 {
		return errors.New("gopact: failure attribution summary, error, or evidence is required")
	}
	for i, evidence := range a.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("gopact: invalid failure attribution evidence %d: %w", i, err)
		}
	}
	return nil
}

func (k FailureKind) valid() bool {
	switch k {
	case FailureRuntime, FailureUnknown, FailureContext, FailureModel, FailureTool, FailureFeedback,
		FailurePolicy, FailureVerification, FailureRecovery, FailureEntropy, FailureSandbox, FailureExternal:
		return true
	default:
		return false
	}
}

func failureAttributionFromFailedEvent(event Event, reports []VerificationReport) (FailureAttribution, bool) {
	if event.Type != EventRunFailed {
		return FailureAttribution{}, false
	}
	ids := event.RuntimeIDs()
	errText := event.Error()
	if errText == "" && event.StepSnapshot != nil {
		errText = event.StepSnapshot.Error
	}
	if errText == "" {
		errText = "run failed"
	}
	node := failureEventNode(event)
	step := failureEventStep(event)
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = now()
	}
	attribution := FailureAttribution{
		ID:      failureAttributionID(ids, node, step),
		Kind:    failureKindFromFailedEvent(event),
		IDs:     ids,
		Node:    node,
		Step:    step,
		Summary: "run failed",
		Error:   errText,
		Evidence: []VerificationEvidence{
			{
				Type:    "event",
				Ref:     failureEventRef(ids, node, step, event.Type),
				Summary: errText,
			},
		},
		CreatedAt: createdAt,
	}
	if report, ok := latestFailedVerificationReport(reports); ok {
		attribution.Kind = FailureVerification
		attribution.Summary = "verification failed"
		attribution.Evidence = append(attribution.Evidence, verificationReportEvidence(report))
		attribution.Metadata = verificationFailureMetadata(report)
	}
	return attribution, true
}

func failureEventNode(event Event) string {
	if event.Node != "" {
		return event.Node
	}
	if event.StepSnapshot != nil {
		return event.StepSnapshot.Node
	}
	return ""
}

func failureEventStep(event Event) int {
	if event.Step > 0 {
		return event.Step
	}
	if event.StepSnapshot != nil {
		return event.StepSnapshot.Step
	}
	return 0
}

func failureKindFromFailedEvent(event Event) FailureKind {
	if kind, ok := failureKindFromMetadata(event.Metadata); ok {
		return kind
	}
	if failureEventHasPolicySignal(event) {
		return FailurePolicy
	}
	if kind, ok := failureKindFromNode(failureEventNode(event)); ok {
		return kind
	}
	if event.ToolCall != nil || event.Result != nil {
		return FailureTool
	}
	if kind, ok := failureKindFromEventType(event.Type); ok {
		return kind
	}
	return FailureRuntime
}

func failureKindFromMetadata(metadata map[string]any) (FailureKind, bool) {
	if len(metadata) == 0 {
		return "", false
	}
	value, ok := metadata[EventMetadataFailureKind]
	if !ok {
		return "", false
	}
	switch kind := value.(type) {
	case FailureKind:
		if kind.valid() {
			return kind, true
		}
	case string:
		normalized := FailureKind(strings.ToLower(strings.TrimSpace(kind)))
		if normalized.valid() {
			return normalized, true
		}
	}
	return "", false
}

func failureEventHasPolicySignal(event Event) bool {
	if errors.Is(event.Err, ErrPolicyDenied) {
		return true
	}
	var policyErr *PolicyDeniedError
	if errors.As(event.Err, &policyErr) {
		return true
	}
	if event.PolicyRequest != nil {
		return true
	}
	if event.PolicyDecision != nil && !event.PolicyDecision.Allowed() {
		return true
	}
	if _, ok := event.Metadata["policy_boundary"]; ok {
		return true
	}
	return false
}

func failureKindFromNode(node string) (FailureKind, bool) {
	switch strings.ToLower(strings.TrimSpace(node)) {
	case "run", "runtime", "runner":
		return FailureRuntime, true
	case "unknown":
		return FailureUnknown, true
	case "build_context", "context", "context_pack", "compact_context", "compress_context":
		return FailureContext, true
	case "call_model", "model":
		return FailureModel, true
	case "call_tool", "tool":
		return FailureTool, true
	case "feedback", "review", "reviewer":
		return FailureFeedback, true
	case "policy":
		return FailurePolicy, true
	case "verify", "verification":
		return FailureVerification, true
	case "resume", "recovery", "recover", "checkpoint_resume", "effect_replay", "run_effect_replay", "memory_replay":
		return FailureRecovery, true
	case "entropy", "entropy_audit":
		return FailureEntropy, true
	case "sandbox", "sandbox_exec":
		return FailureSandbox, true
	case "a2a", "channel", "mcp", "skill", "external":
		return FailureExternal, true
	default:
		return "", false
	}
}

func failureKindFromEventType(eventType EventType) (FailureKind, bool) {
	switch eventType {
	case EventModelProviderAttemptFailed:
		return FailureModel, true
	case EventToolCall, EventToolResult:
		return FailureTool, true
	case EventSandboxExecFailed:
		return FailureSandbox, true
	case EventA2ATaskFailed, EventChannelTransferFailed, EventChannelSendFailed:
		return FailureExternal, true
	default:
		return "", false
	}
}

func failureAttributionID(ids RuntimeIDs, node string, step int) string {
	prefix := ids.RunID
	if prefix == "" {
		prefix = "run"
	}
	if node == "" {
		node = "run"
	}
	if step <= 0 {
		return fmt.Sprintf("%s:failure:%s", prefix, node)
	}
	return fmt.Sprintf("%s:failure:%s:%d", prefix, node, step)
}

func failureEventRef(ids RuntimeIDs, node string, step int, eventType EventType) string {
	prefix := ids.RunID
	if prefix == "" {
		prefix = "run"
	}
	if node == "" {
		node = "run"
	}
	if step <= 0 {
		return fmt.Sprintf("%s:%s:%s", prefix, node, eventType)
	}
	return fmt.Sprintf("%s:%s:%d:%s", prefix, node, step, eventType)
}

func latestFailedVerificationReport(reports []VerificationReport) (VerificationReport, bool) {
	for i := len(reports) - 1; i >= 0; i-- {
		if reports[i].Status == VerificationStatusFailed || reports[i].FailedCount > 0 {
			return copyVerificationReport(reports[i]), true
		}
	}
	return VerificationReport{}, false
}

func verificationReportEvidence(report VerificationReport) VerificationEvidence {
	return VerificationEvidence{
		Type:    "verification_report",
		Ref:     verificationReportRef(report),
		Summary: "verification report failed",
		Metadata: map[string]any{
			"status":        string(report.Status),
			"passed_count":  report.PassedCount,
			"failed_count":  report.FailedCount,
			"skipped_count": report.SkippedCount,
		},
	}
}

func verificationReportRef(report VerificationReport) string {
	prefix := report.IDs.RunID
	if prefix == "" {
		prefix = "run"
	}
	status := string(report.Status)
	if status == "" {
		status = "unknown"
	}
	return fmt.Sprintf("%s:verification_report:%s", prefix, status)
}

func verificationFailureMetadata(report VerificationReport) map[string]any {
	return map[string]any{
		"verification_status":        string(report.Status),
		"verification_passed_count":  report.PassedCount,
		"verification_failed_count":  report.FailedCount,
		"verification_skipped_count": report.SkippedCount,
	}
}

func copyFailureAttributions(in []FailureAttribution) []FailureAttribution {
	if len(in) == 0 {
		return nil
	}
	out := make([]FailureAttribution, len(in))
	for i, attribution := range in {
		out[i] = copyFailureAttribution(attribution)
	}
	return out
}

func copyFailureAttribution(in FailureAttribution) FailureAttribution {
	out := in
	out.Evidence = copyVerificationEvidence(in.Evidence)
	out.Metadata = copyAnyMap(in.Metadata)
	return out
}
