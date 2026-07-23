package agent

import (
	"slices"

	"github.com/gopact-ai/gopact"
)

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
	return value.Clone()
}

func cloneRefs(values []gopact.ArtifactRef) []gopact.ArtifactRef {
	return slices.Clone(values)
}

func cloneRetryHint(value *gopact.RetryHint) *gopact.RetryHint {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
