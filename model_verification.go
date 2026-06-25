package gopact

import (
	"errors"
	"fmt"
)

var (
	// ErrModelCallFailed is returned when model verification evidence records a failed call.
	ErrModelCallFailed = errors.New("gopact: model call failed")
	// ErrModelCallRequired is returned when model verification has no call snapshot.
	ErrModelCallRequired = errors.New("gopact: model call evidence is required")
)

const (
	// VerificationCheckModelCall is the standard check ID prefix for observed model calls.
	VerificationCheckModelCall = "model-call"

	// VerificationEvidenceTypeModelCall is the evidence type for observed model calls.
	VerificationEvidenceTypeModelCall = "model_call"
)

// ModelCallSnapshot is an already-observed model call summary. It intentionally
// records request/response shape, route, usage, and error metadata, not raw prompt
// or raw response text.
type ModelCallSnapshot struct {
	ID       string
	Name     string
	Ref      string
	Request  ModelRequest
	Response ModelResponse
	Err      error
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordModelCallCheck records an already-observed model call as verification evidence.
func RecordModelCallCheck(recorder *VerificationRecorder, snapshot ModelCallSnapshot) error {
	if recorder == nil {
		return errors.New("gopact: verification recorder is nil")
	}
	if !snapshot.Skipped && !modelCallHasRef(snapshot) {
		return ErrModelCallRequired
	}

	check := modelCallCheck(snapshot)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == VerificationStatusFailed {
		if snapshot.Err != nil {
			return errors.Join(ErrModelCallFailed, snapshot.Err)
		}
		return ErrModelCallFailed
	}
	return nil
}

func modelCallCheck(snapshot ModelCallSnapshot) VerificationCheck {
	ref := modelCallRef(snapshot)
	id := snapshot.ID
	if id == "" {
		id = VerificationCheckModelCall + ":" + ref
	}
	name := snapshot.Name
	if name == "" {
		name = "model call"
	}
	status := modelCallStatus(snapshot)
	summary := snapshot.Summary
	if summary == "" {
		summary = modelCallSummary(status, snapshot)
	}
	return VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeModelCall,
				Ref:      ref,
				Summary:  modelCallEvidenceSummary(status, snapshot),
				Metadata: modelCallEvidenceMetadata(snapshot),
			},
		},
		Metadata: modelCallCheckMetadata(snapshot),
	}
}

func modelCallStatus(snapshot ModelCallSnapshot) VerificationStatus {
	if snapshot.Skipped {
		return VerificationStatusSkipped
	}
	if snapshot.Err != nil {
		return VerificationStatusFailed
	}
	return VerificationStatusPassed
}

func modelCallSummary(status VerificationStatus, snapshot ModelCallSnapshot) string {
	switch status {
	case VerificationStatusSkipped:
		return "model call skipped"
	case VerificationStatusFailed:
		if snapshot.Err != nil {
			return "model call failed: " + snapshot.Err.Error()
		}
		return "model call failed"
	default:
		if route := modelRouteLabel(snapshot.Response.Route); route != "" {
			return "model call completed via " + route
		}
		if snapshot.Request.Model != "" {
			return "model call completed via " + snapshot.Request.Model
		}
		return "model call completed"
	}
}

func modelCallEvidenceSummary(status VerificationStatus, snapshot ModelCallSnapshot) string {
	if status == VerificationStatusSkipped {
		return "skipped"
	}
	if snapshot.Err != nil {
		return snapshot.Err.Error()
	}
	usage := snapshot.Response.Usage
	if usage.TotalTokens > 0 {
		return fmt.Sprintf("%d total tokens", usage.TotalTokens)
	}
	if route := modelRouteLabel(snapshot.Response.Route); route != "" {
		return route
	}
	return "model call captured"
}

func modelCallCheckMetadata(snapshot ModelCallSnapshot) map[string]any {
	metadata := modelCallBaseMetadata(snapshot)
	mergeSupplementalVerificationMetadata(metadata, snapshot.Metadata, modelCallReservedMetadataKey)
	return metadata
}

func modelCallEvidenceMetadata(snapshot ModelCallSnapshot) map[string]any {
	return modelCallBaseMetadata(snapshot)
}

func modelCallBaseMetadata(snapshot ModelCallSnapshot) map[string]any {
	request := snapshot.Request
	response := snapshot.Response
	metadata := map[string]any{
		"ref":              modelCallRef(snapshot),
		"message_count":    len(request.Messages),
		"tool_count":       len(request.Tools),
		"capability_count": len(request.Capabilities),
	}
	addRunExportRuntimeIDMetadata(metadata, request.IDs)
	if request.Model != "" {
		metadata["request_model"] = request.Model
	}
	if request.RouteHint != "" {
		metadata["route_hint"] = request.RouteHint
	}
	if roles := messageRoles(request.Messages); len(roles) > 0 {
		metadata["message_roles"] = roles
	}
	if names := toolSpecNames(request.Tools); len(names) > 0 {
		metadata["tool_names"] = names
	}
	if capabilities := capabilityNames(request.Capabilities); len(capabilities) > 0 {
		metadata["capabilities"] = capabilities
	}
	if len(request.ResponseSchema) > 0 {
		metadata["response_schema"] = true
	}
	addBudgetMetadata(metadata, request.Budget)
	addModelRouteMetadata(metadata, response.Route)
	addUsageMetadata(metadata, response.Usage)
	if response.Message.Role != "" {
		metadata["output_role"] = string(response.Message.Role)
	}
	if len(response.Message.ToolCalls) > 0 {
		metadata["output_tool_call_count"] = len(response.Message.ToolCalls)
		metadata["output_tool_names"] = toolCallNames(response.Message.ToolCalls)
	}
	if len(response.Events) > 0 {
		metadata["model_event_count"] = len(response.Events)
	}
	if len(request.Metadata) > 0 {
		metadata["request_metadata"] = copyAnyMap(request.Metadata)
	}
	if len(response.Metadata) > 0 {
		metadata["response_metadata"] = copyAnyMap(response.Metadata)
	}
	if snapshot.Err != nil {
		metadata["error"] = snapshot.Err.Error()
	}
	if snapshot.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func modelCallReservedMetadataKey(key string) bool {
	if runtimeIDVerificationMetadataKey(key) {
		return true
	}
	switch key {
	case "ref",
		"message_count",
		"tool_count",
		"capability_count",
		"request_model",
		"route_hint",
		"message_roles",
		"tool_names",
		"capabilities",
		"response_schema",
		"max_input_tokens",
		"max_output_tokens",
		"max_cost_usd",
		"route_name",
		"provider",
		"route_model",
		"endpoint",
		"attempt",
		"config_version",
		"route_reason",
		"route_metadata",
		"input_tokens",
		"output_tokens",
		"total_tokens",
		"cost_usd",
		"output_role",
		"output_tool_call_count",
		"output_tool_names",
		"model_event_count",
		"request_metadata",
		"response_metadata",
		"error",
		"skipped":
		return true
	default:
		return false
	}
}

func modelCallHasRef(snapshot ModelCallSnapshot) bool {
	if snapshot.Ref != "" || snapshot.ID != "" {
		return true
	}
	ids := snapshot.Request.IDs
	if ids.CallID != "" || ids.RunID != "" {
		return true
	}
	if snapshot.Request.Model != "" {
		return true
	}
	route := snapshot.Response.Route
	return route.Provider != "" || route.Model != "" || route.RouteName != ""
}

func modelCallRef(snapshot ModelCallSnapshot) string {
	if snapshot.Ref != "" {
		return snapshot.Ref
	}
	if snapshot.Request.IDs.CallID != "" {
		return snapshot.Request.IDs.CallID
	}
	if snapshot.Request.IDs.RunID != "" {
		return snapshot.Request.IDs.RunID + ":model"
	}
	if snapshot.Request.Model != "" {
		return snapshot.Request.Model
	}
	if route := modelRouteLabel(snapshot.Response.Route); route != "" {
		return route
	}
	if snapshot.ID != "" {
		return snapshot.ID
	}
	return VerificationCheckModelCall
}

func modelRouteLabel(route ModelRoute) string {
	if route.Provider != "" && route.Model != "" {
		return route.Provider + "/" + route.Model
	}
	if route.Model != "" {
		return route.Model
	}
	if route.Provider != "" {
		return route.Provider
	}
	return route.RouteName
}

func messageRoles(messages []Message) []string {
	if len(messages) == 0 {
		return nil
	}
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, string(message.Role))
	}
	return roles
}

func toolSpecNames(tools []ToolSpec) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	return names
}

func toolCallNames(calls []ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Name != "" {
			names = append(names, call.Name)
		}
	}
	return names
}

func capabilityNames(capabilities []Capability) []string {
	if len(capabilities) == 0 {
		return nil
	}
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability != "" {
			names = append(names, string(capability))
		}
	}
	return names
}

func addBudgetMetadata(metadata map[string]any, budget Budget) {
	if budget.MaxInputTokens > 0 {
		metadata["max_input_tokens"] = budget.MaxInputTokens
	}
	if budget.MaxOutputTokens > 0 {
		metadata["max_output_tokens"] = budget.MaxOutputTokens
	}
	if budget.MaxCostUSD > 0 {
		metadata["max_cost_usd"] = budget.MaxCostUSD
	}
}

func addModelRouteMetadata(metadata map[string]any, route ModelRoute) {
	if route.RouteName != "" {
		metadata["route_name"] = route.RouteName
	}
	if route.Provider != "" {
		metadata["provider"] = route.Provider
	}
	if route.Model != "" {
		metadata["route_model"] = route.Model
	}
	if route.Endpoint != "" {
		metadata["endpoint"] = route.Endpoint
	}
	if route.Attempt > 0 {
		metadata["attempt"] = route.Attempt
	}
	if route.ConfigVersion != "" {
		metadata["config_version"] = route.ConfigVersion
	}
	if route.Reason != "" {
		metadata["route_reason"] = route.Reason
	}
	if len(route.Metadata) > 0 {
		metadata["route_metadata"] = copyAnyMap(route.Metadata)
	}
}

func addUsageMetadata(metadata map[string]any, usage Usage) {
	if usage.InputTokens > 0 {
		metadata["input_tokens"] = usage.InputTokens
	}
	if usage.OutputTokens > 0 {
		metadata["output_tokens"] = usage.OutputTokens
	}
	if usage.TotalTokens > 0 {
		metadata["total_tokens"] = usage.TotalTokens
	}
	if usage.CostUSD > 0 {
		metadata["cost_usd"] = usage.CostUSD
	}
}
