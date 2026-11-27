package novelfetchworkshop

import (
	"reflect"
	"slices"

	"qiantie/backend/internal/batchfactoryv11"
)

// Locations, not row IDs or keys, distinguish records in a source document.
type legacyVideoPosition struct {
	Container, Index int
	Provider         string
	Video            bool
}

func videoPosition(source legacySourceEntry) legacyVideoPosition {
	return legacyVideoPosition{Container: source.Container, Index: source.Index, Provider: source.Provider, Video: source.Video}
}

type legacyVideoMatch struct {
	database, file legacySourceEntry
	orphan         bool
}

func (p pendingLegacyVideos) matchFile(file map[string]any) ([]legacyVideoMatch, bool) {
	if !p.any() || validateLegacyVideoDocument(file) != nil {
		return nil, false
	}
	var matches []legacyVideoMatch
	for _, saved := range p.Sources {
		if !validLegacyVideoFields(saved.Raw) || saved.Video && !validLegacyProviderIdentity(saved.Provider, saved.Raw) {
			return nil, false
		}
		matches = append(matches, legacyVideoMatch{database: legacySourceEntry{legacyScrubEntry: saved.legacyScrubEntry, raw: saved.Raw, model: saved.model()}})
	}
	for _, i := range p.orphanIndices() {
		row, _ := p.Rows[i].(map[string]any)
		if !validLegacyVideoFields(row) {
			return nil, false
		}
		// Pre-1200007 omitted the original JSON position for some keyless
		// catalog rows. The index in the sealed Rows list is durable.
		matches = append(matches, legacyVideoMatch{database: legacySourceEntry{legacyScrubEntry: legacyScrubEntry{Container: -1, Index: i}, raw: row, model: row}, orphan: true})
	}

	fileSources := collectLegacyVideoCandidates(file)
	candidates := make([][]int, len(matches))
	uses := make([]int, len(fileSources))
	for i, match := range matches {
		for j, source := range fileSources {
			if eligibleVideoEvidence(match, source) {
				candidates[i] = append(candidates[i], j)
				uses[j]++
			}
		}
	}
	claimed := make([]bool, len(fileSources))
	for i, match := range matches {
		competing := slices.ContainsFunc(candidates[i], func(j int) bool { return uses[j] > 1 })
		var qualified []int
		for _, j := range candidates[i] {
			if !competing || sameVideoRecord(match.database, fileSources[j]) {
				qualified = append(qualified, j)
			}
		}
		if len(qualified) == 0 {
			return nil, false
		}
		selected := qualified[0]
		if competing && len(qualified) > 1 {
			// The original location distinguishes otherwise identical nested
			// records. An orphan has no corresponding document position.
			if match.orphan {
				return nil, false
			}
			selected = -1
			for _, j := range qualified {
				if videoPosition(match.database) == videoPosition(fileSources[j]) {
					selected = j
					break
				}
			}
		}
		if selected < 0 || claimed[selected] {
			return nil, false
		}
		claimed[selected] = true
		matches[i].file = fileSources[selected]
	}
	for j, n := range uses {
		if n > 1 && !claimed[j] {
			return nil, false
		}
	}
	return matches, true
}

func eligibleVideoEvidence(match legacyVideoMatch, file legacySourceEntry) bool {
	row := match.database.model
	left, right := ModelConfigText(row, "id"), ModelConfigText(file.model, "id")
	if left != "" && right != "" && left != right || !sameLegacyVideoTarget(row, file.model) {
		return false
	}
	keys, valid := legacySourceCredentials(file)
	if !valid {
		return false
	}
	if len(keys) == 0 && !(ModelConfigText(row, "provider") == batchfactoryv11.VideoProviderDoubaoLocal && !hasLegacyCredentialValue(row)) {
		return false
	}
	// An old orphan has no database source locator to archive a disputed
	// key against. Preserve its envelope if the returning key disagrees.
	if match.orphan {
		original, valid := legacySourceCredentials(match.database)
		if !valid {
			return false
		}
		for _, key := range original {
			if keys["credential"] != key && keys["apiKey"] != key {
				return false
			}
		}
	}
	return true
}

func sameVideoRecord(database, file legacySourceEntry) bool {
	if database.Video != file.Video {
		return false
	}
	if database.Video {
		return true
	}
	for _, row := range []map[string]any{database.model, file.model} {
		if v, exists := row["enabled"]; exists {
			if _, ok := v.(bool); !ok {
				return false
			}
		}
		if v, exists := row["displayName"]; exists {
			if _, ok := v.(string); !ok {
				return false
			}
		}
	}
	return reflect.DeepEqual(database.model["displayName"], file.model["displayName"]) &&
		reflect.DeepEqual(database.model["enabled"], file.model["enabled"])
}

// separateLegacyVideoID derives a catalog ID from the protected database
// position and public model identity.
func separateLegacyVideoID(source legacySourceEntry, occupied map[string]bool) string {
	row := source.model
	for attempt := 0; ; attempt++ {
		digest := legacyEntryDigest([]any{
			"independent-legacy-video-v1", videoPosition(source), ModelConfigText(row, "id"),
			ModelConfigText(row, "kind"), ModelConfigText(row, "modelId"), ModelCredentialSource(row),
			ModelCredentialDestination(row), ModelConfigText(row, "workflowId"), row["displayName"], row["enabled"], attempt,
		})
		id := "legacy:" + digest[:32]
		if !occupied[id] {
			return id
		}
	}
}

func (migration *legacyConfigMigration) prepareVideoRows(rows, boundRows []any, file map[string]any) []any {
	formal := map[string]bool{}
	for _, item := range boundRows {
		row, _ := item.(map[string]any)
		formal[ModelConfigText(row, "id")] = true
	}
	var active []any
	for _, item := range rows {
		row, _ := item.(map[string]any)
		if ModelConfigText(row, "kind") != "video" || formal[ModelConfigText(row, "id")] {
			active = append(active, item)
		}
	}
	if !migration.releaseVideos {
		return active
	}
	occupied := map[string]bool{}
	for _, items := range [][]any{rows, legacyCatalogRows(file)} {
		for _, item := range items {
			row, _ := item.(map[string]any)
			occupied[legacyModelRowID(row)] = true
		}
	}
	primary := map[string]int{}
	for i, match := range migration.videoMatches {
		id := legacyModelRowID(match.database.model)
		occupied[id] = true
		index, found := primary[id]
		if !found || match.database.Video && !migration.videoMatches[index].database.Video {
			primary[id] = i
		}
	}
	var restored []any
	migration.fileModelIDs = map[legacyVideoPosition]string{}
	for i, match := range migration.videoMatches {
		source := match.database
		row := cloneMap(source.model)
		id := legacyModelRowID(row)
		// Each match already proved a separate source position. The provider
		// keeps its ID when a catalog entry shares it.
		if primary[id] != i {
			id = separateLegacyVideoID(source, occupied)
			occupied[id] = true
			row["id"] = id
		}
		formal, collides := findLegacyTarget(row, active)
		if formal != nil {
			id = ModelConfigText(formal, "id")
		}
		if !collides || formal != nil {
			source.TargetID = id
			migration.fileModelIDs[videoPosition(match.file)] = id
		}
		if !match.orphan {
			migration.database = append(migration.database, source)
		}
		if !collides {
			restored = append(restored, row)
		}
	}
	return append(active, restored...)
}
