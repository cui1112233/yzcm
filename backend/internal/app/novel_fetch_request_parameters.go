package app

// Copy maps so a classifier or per-version request cannot mutate saved defaults.
func novelFetchExtraParameters(sources ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, source := range sources {
		for key, value := range source {
			out[key] = value
		}
	}
	return out
}
