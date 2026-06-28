package gopact

import "sort"

func mergeSupplementalVerificationMetadata(metadata map[string]any, supplemental map[string]any, reserved func(string) bool) {
	for key, value := range supplemental {
		if reserved != nil && reserved(key) {
			continue
		}
		metadata[key] = value
	}
}

func sortedSupplementalVerificationMetadataKeys(supplemental map[string]any, reserved func(string) bool) []string {
	if len(supplemental) == 0 {
		return nil
	}
	keys := make([]string, 0, len(supplemental))
	for key := range supplemental {
		if reserved != nil && reserved(key) {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func runtimeIDVerificationMetadataKey(key string) bool {
	switch key {
	case "user_id",
		"session_id",
		"thread_id",
		"run_id",
		"agent_id",
		"app_id",
		"call_id",
		"parent_call_id",
		"trace_id":
		return true
	default:
		return false
	}
}
