package gopacttest

import "sort"

func mergeSupplementalMetadata(metadata map[string]any, supplemental map[string]any, reserved func(string) bool) {
	for key, value := range supplemental {
		if reserved != nil && reserved(key) {
			continue
		}
		metadata[key] = value
	}
}

func sortedSupplementalMetadataKeys(supplemental map[string]any, reserved func(string) bool) []string {
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
