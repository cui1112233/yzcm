package httpapi

// The book's confirmed platform wins over a shared layout profile, matching
// the reference system's apply_task_overrides_to_profile. Older recovered
// documents may keep their input platform only in the execution snapshot.
func webSubmitTaskPlatform(meta, profile map[string]any) string {
	snapshot := mapOrEmpty(meta["processSnapshot"])
	return firstText(meta["platformId"], meta["platform_id"], snapshot["platform_id"], profile["platform_id"], profile["platformId"])
}
