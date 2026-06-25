package gopacttest

import "github.com/gopact-ai/gopact"

func copyConformanceMessages(in []gopact.Message) []gopact.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.Message, len(in))
	for i, message := range in {
		out[i] = message
		out[i].Parts = append([]gopact.ContentPart(nil), message.Parts...)
		for partIndex := range out[i].Parts {
			out[i].Parts[partIndex].Metadata = copyConformanceAnyMap(message.Parts[partIndex].Metadata)
		}
		out[i].ToolCalls = append([]gopact.ToolCall(nil), message.ToolCalls...)
		for callIndex := range out[i].ToolCalls {
			out[i].ToolCalls[callIndex].Arguments = append([]byte(nil), message.ToolCalls[callIndex].Arguments...)
		}
	}
	return out
}

func copyConformanceJSONSchema(in gopact.JSONSchema) gopact.JSONSchema {
	if len(in) == 0 {
		return nil
	}
	out := make(gopact.JSONSchema, len(in))
	for key, value := range in {
		out[key] = copyConformanceAnyValue(value)
	}
	return out
}

func copyConformanceAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = copyConformanceAnyValue(value)
	}
	return out
}

func copyConformanceAnyValue(in any) any {
	switch value := in.(type) {
	case gopact.JSONSchema:
		return copyConformanceJSONSchema(value)
	case map[string]any:
		return copyConformanceAnyMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = copyConformanceAnyValue(item)
		}
		return out
	case []string:
		return append([]string(nil), value...)
	case []int:
		return append([]int(nil), value...)
	case []float64:
		return append([]float64(nil), value...)
	default:
		return value
	}
}

func copyInterruptRecordForConformance(in *gopact.InterruptRecord) *gopact.InterruptRecord {
	if in == nil {
		return nil
	}
	out := *in
	out.Prompt = copyConformanceMessages([]gopact.Message{in.Prompt})[0]
	out.ResumeSchema = copyConformanceJSONSchema(in.ResumeSchema)
	out.Metadata = copyConformanceAnyMap(in.Metadata)
	return &out
}

func copyArtifactRefsForConformance(in []gopact.ArtifactRef) []gopact.ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]gopact.ArtifactRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Metadata = copyConformanceAnyMap(ref.Metadata)
	}
	return out
}
