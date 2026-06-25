package gopacttest

func mergeSupplementalMetadata(metadata map[string]any, supplemental map[string]any, reserved func(string) bool) {
	for key, value := range supplemental {
		if reserved != nil && reserved(key) {
			continue
		}
		metadata[key] = value
	}
}
