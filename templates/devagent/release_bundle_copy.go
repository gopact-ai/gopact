package devagent

import "github.com/gopact-ai/gopact"

func copyReleaseRunExport(in gopact.RunExport) gopact.RunExport {
	out := in
	out.Events = copyReleaseEvents(in.Events)
	out.Steps = copyReleaseStepSnapshots(in.Steps)
	out.Tasks = copyReleaseTaskRecords(in.Tasks)
	out.Inputs = copyReleaseInputRecords(in.Inputs)
	out.Interventions = copyReleaseInterventionRecords(in.Interventions)
	out.Failures = copyReleaseFailureAttributions(in.Failures)
	out.EntropyAudits = copyReviewEntropyAudits(in.EntropyAudits)
	out.VerificationReports = copyReleaseVerificationReports(in.VerificationReports)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseProcessRecords(in ProcessRecords) ProcessRecords {
	return ProcessRecords{
		Task:          copyReleaseTaskRecord(in.Task),
		Inputs:        copyReleaseInputRecords(in.Inputs),
		Interventions: copyReleaseInterventionRecords(in.Interventions),
	}
}

func copyReleaseEvents(in []gopact.Event) []gopact.Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.Event, len(in))
	for i, event := range in {
		out[i] = copyReleaseEvent(event)
	}
	return out
}

func copyReleaseEvent(in gopact.Event) gopact.Event {
	out := in
	if in.StepSnapshot != nil {
		snapshot := copyReleaseStepSnapshot(*in.StepSnapshot)
		out.StepSnapshot = &snapshot
	}
	if in.ModelRoute != nil {
		route := *in.ModelRoute
		route.Metadata = copyReleaseMap(in.ModelRoute.Metadata)
		out.ModelRoute = &route
	}
	if in.Message != nil {
		message := copyReleaseMessage(*in.Message)
		out.Message = &message
	}
	if in.ToolCall != nil {
		toolCall := copyReleaseToolCall(*in.ToolCall)
		out.ToolCall = &toolCall
	}
	if in.Result != nil {
		result := copyReleaseToolResult(*in.Result)
		out.Result = &result
	}
	if in.Usage != nil {
		usage := *in.Usage
		out.Usage = &usage
	}
	out.Artifacts = copyReleaseArtifactRefs(in.Artifacts)
	if in.PolicyRequest != nil {
		request := copyReleasePolicyRequest(*in.PolicyRequest)
		out.PolicyRequest = &request
	}
	if in.PolicyDecision != nil {
		decision := copyReleasePolicyDecision(*in.PolicyDecision)
		out.PolicyDecision = &decision
	}
	out.Redaction.Fields = append([]string(nil), in.Redaction.Fields...)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseStepSnapshots(in []gopact.StepSnapshot) []gopact.StepSnapshot {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.StepSnapshot, len(in))
	for i, snapshot := range in {
		out[i] = copyReleaseStepSnapshot(snapshot)
	}
	return out
}

func copyReleaseStepSnapshot(in gopact.StepSnapshot) gopact.StepSnapshot {
	out := in
	out.Input = copyReleaseValue(in.Input)
	out.Output = copyReleaseValue(in.Output)
	out.Queue = append([]string(nil), in.Queue...)
	if in.Pending != nil {
		pending := copyReleaseInterruptRecord(*in.Pending)
		out.Pending = &pending
	}
	out.Effects = copyReleaseEffectRecords(in.Effects)
	out.Artifacts = copyReleaseArtifactRefs(in.Artifacts)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseTaskRecords(in []gopact.TaskRecord) []gopact.TaskRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.TaskRecord, len(in))
	for i, record := range in {
		out[i] = copyReleaseTaskRecord(record)
	}
	return out
}

func copyReleaseTaskRecord(in gopact.TaskRecord) gopact.TaskRecord {
	out := in
	out.Input = copyReleaseValue(in.Input)
	out.Output = copyReleaseValue(in.Output)
	out.Artifacts = copyReleaseArtifactRefs(in.Artifacts)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseInputRecords(in []gopact.InputRecord) []gopact.InputRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.InputRecord, len(in))
	for i, record := range in {
		out[i] = copyReleaseInputRecord(record)
	}
	return out
}

func copyReleaseInputRecord(in gopact.InputRecord) gopact.InputRecord {
	out := in
	out.Value = copyReleaseValue(in.Value)
	if in.Resume != nil {
		resume := copyReleaseResumeRequest(*in.Resume)
		out.Resume = &resume
	}
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseInterventionRecords(in []gopact.InterventionRecord) []gopact.InterventionRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.InterventionRecord, len(in))
	for i, record := range in {
		out[i] = copyReleaseInterventionRecord(record)
	}
	return out
}

func copyReleaseInterventionRecord(in gopact.InterventionRecord) gopact.InterventionRecord {
	out := in
	if in.Request != nil {
		request := copyReleaseInterruptRecord(*in.Request)
		out.Request = &request
	}
	if in.Resume != nil {
		resume := copyReleaseResumeRequest(*in.Resume)
		out.Resume = &resume
	}
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseFailureAttributions(in []gopact.FailureAttribution) []gopact.FailureAttribution {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.FailureAttribution, len(in))
	for i, attribution := range in {
		out[i] = attribution
		out[i].Evidence = copyReviewVerificationEvidence(attribution.Evidence)
		out[i].Metadata = copyReleaseMap(attribution.Metadata)
	}
	return out
}

func copyReleaseVerificationReports(in []gopact.VerificationReport) []gopact.VerificationReport {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.VerificationReport, len(in))
	for i, report := range in {
		out[i] = copyVerificationReport(report)
	}
	return out
}

func copyReleaseInterruptRecord(in gopact.InterruptRecord) gopact.InterruptRecord {
	out := in
	out.Prompt = copyReleaseMessage(in.Prompt)
	out.ResumeSchema = copyReleaseJSONSchema(in.ResumeSchema)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseResumeRequest(in gopact.ResumeRequest) gopact.ResumeRequest {
	out := in
	out.Payload = copyReleaseValue(in.Payload)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseToolResult(in gopact.ToolResult) gopact.ToolResult {
	out := in
	out.Artifacts = copyReleaseArtifactRefs(in.Artifacts)
	out.Effects = copyReleaseEffectRecords(in.Effects)
	out.Events = copyReleaseEvents(in.Events)
	if in.Commit != nil {
		commit := *in.Commit
		commit.Metadata = copyReleaseMap(in.Commit.Metadata)
		out.Commit = &commit
	}
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseEffectRecords(in []gopact.EffectRecord) []gopact.EffectRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.EffectRecord, len(in))
	for i, effect := range in {
		out[i] = effect
		out[i].DependsOn = append([]string(nil), effect.DependsOn...)
		out[i].Artifacts = copyReleaseArtifactRefs(effect.Artifacts)
		if effect.Sandbox != nil {
			sandbox := *effect.Sandbox
			sandbox.Command = append([]string(nil), effect.Sandbox.Command...)
			sandbox.Metadata = copyReleaseMap(effect.Sandbox.Metadata)
			out[i].Sandbox = &sandbox
		}
		out[i].Metadata = copyReleaseMap(effect.Metadata)
	}
	return out
}

func copyReleaseArtifactRefs(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Metadata = copyReleaseMap(ref.Metadata)
	}
	return out
}

func copyReleasePolicyRequest(in gopact.PolicyRequest) gopact.PolicyRequest {
	out := in
	out.Input = copyReleaseValue(in.Input)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleasePolicyDecision(in gopact.PolicyDecision) gopact.PolicyDecision {
	out := in
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseModelRequest(in gopact.ModelRequest) gopact.ModelRequest {
	out := in
	out.Messages = copyReleaseMessages(in.Messages)
	out.Tools = copyReleaseToolSpecs(in.Tools)
	out.ResponseSchema = copyReleaseJSONSchema(in.ResponseSchema)
	out.Capabilities = append([]gopact.Capability(nil), in.Capabilities...)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseModelResponse(in gopact.ModelResponse) gopact.ModelResponse {
	out := in
	out.Message = copyReleaseMessage(in.Message)
	out.Route.Metadata = copyReleaseMap(in.Route.Metadata)
	out.Events = copyReleaseEvents(in.Events)
	out.Metadata = copyReleaseMap(in.Metadata)
	return out
}

func copyReleaseMessages(in []gopact.Message) []gopact.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.Message, len(in))
	for i, message := range in {
		out[i] = copyReleaseMessage(message)
	}
	return out
}

func copyReleaseMessage(in gopact.Message) gopact.Message {
	out := in
	out.Parts = copyReleaseContentParts(in.Parts)
	out.ToolCalls = copyReleaseToolCalls(in.ToolCalls)
	return out
}

func copyReleaseContentParts(in []gopact.ContentPart) []gopact.ContentPart {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ContentPart, len(in))
	for i, part := range in {
		out[i] = part
		out[i].Metadata = copyReleaseMap(part.Metadata)
	}
	return out
}

func copyReleaseToolSpecs(in []gopact.ToolSpec) []gopact.ToolSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ToolSpec, len(in))
	for i, spec := range in {
		out[i] = spec
		out[i].InputSchema = copyReleaseJSONSchema(spec.InputSchema)
	}
	return out
}

func copyReleaseToolCalls(in []gopact.ToolCall) []gopact.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ToolCall, len(in))
	for i, toolCall := range in {
		out[i] = copyReleaseToolCall(toolCall)
	}
	return out
}

func copyReleaseToolCall(in gopact.ToolCall) gopact.ToolCall {
	out := in
	out.Arguments = append([]byte(nil), in.Arguments...)
	return out
}

func copyReleaseJSONSchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(in))
	for key, value := range in {
		out[key] = copyReleaseValue(value)
	}
	return out
}

func copyReleaseMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = copyReleaseValue(value)
	}
	return out
}

func copyReleaseValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return copyReleaseMap(typed)
	case gopact.JSONSchema:
		return copyReleaseJSONSchema(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = copyReleaseValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for i, item := range typed {
			out[i] = copyReleaseMap(item)
		}
		return out
	case []string:
		return copyStringSlice(typed)
	case []gopact.Message:
		return copyReleaseMessages(typed)
	case gopact.Message:
		return copyReleaseMessage(typed)
	case []gopact.ToolCall:
		return copyReleaseToolCalls(typed)
	case gopact.ToolCall:
		return copyReleaseToolCall(typed)
	case []gopact.ArtifactRef:
		return copyReleaseArtifactRefs(typed)
	case gopact.ToolResult:
		return copyReleaseToolResult(typed)
	case gopact.ModelRequest:
		return copyReleaseModelRequest(typed)
	case *gopact.ModelRequest:
		if typed == nil {
			return (*gopact.ModelRequest)(nil)
		}
		out := copyReleaseModelRequest(*typed)
		return &out
	case gopact.ModelResponse:
		return copyReleaseModelResponse(typed)
	case *gopact.ModelResponse:
		if typed == nil {
			return (*gopact.ModelResponse)(nil)
		}
		out := copyReleaseModelResponse(*typed)
		return &out
	case gopact.PolicyRequest:
		return copyReleasePolicyRequest(typed)
	case *gopact.PolicyRequest:
		if typed == nil {
			return (*gopact.PolicyRequest)(nil)
		}
		out := copyReleasePolicyRequest(*typed)
		return &out
	case gopact.PolicyDecision:
		return copyReleasePolicyDecision(typed)
	case *gopact.PolicyDecision:
		if typed == nil {
			return (*gopact.PolicyDecision)(nil)
		}
		out := copyReleasePolicyDecision(*typed)
		return &out
	case gopact.ResumeRequest:
		return copyReleaseResumeRequest(typed)
	case *gopact.ResumeRequest:
		if typed == nil {
			return (*gopact.ResumeRequest)(nil)
		}
		out := copyReleaseResumeRequest(*typed)
		return &out
	case gopact.InterruptRecord:
		return copyReleaseInterruptRecord(typed)
	case *gopact.InterruptRecord:
		if typed == nil {
			return (*gopact.InterruptRecord)(nil)
		}
		out := copyReleaseInterruptRecord(*typed)
		return &out
	default:
		return value
	}
}
