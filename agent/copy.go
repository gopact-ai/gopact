package agent

import "github.com/gopact-ai/gopact"

func cloneMessages(values []gopact.Message) []gopact.Message {
	if values == nil {
		return nil
	}
	out := make([]gopact.Message, len(values))
	for i, value := range values {
		out[i] = cloneMessage(value)
	}
	return out
}

func cloneMessage(value gopact.Message) gopact.Message {
	if value.Parts == nil {
		return value
	}
	value.Parts = append([]gopact.MessagePart(nil), value.Parts...)
	for i := range value.Parts {
		if value.Parts[i].Ref != nil {
			ref := *value.Parts[i].Ref
			value.Parts[i].Ref = &ref
		}
	}
	return value
}

func cloneRefs(values []gopact.ArtifactRef) []gopact.ArtifactRef {
	return append([]gopact.ArtifactRef(nil), values...)
}

func cloneRetryHint(value *gopact.RetryHint) *gopact.RetryHint {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	clone := make(map[string]string, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}
