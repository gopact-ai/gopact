package provider

// MissingCapabilities returns required capabilities that available does not include.
func MissingCapabilities(required []Capability, available []Capability) []Capability {
	availableSet := make(map[Capability]struct{}, len(available))
	for _, capability := range available {
		availableSet[capability] = struct{}{}
	}

	var missing []Capability
	for _, capability := range required {
		if _, ok := availableSet[capability]; !ok {
			missing = append(missing, capability)
		}
	}
	return missing
}
