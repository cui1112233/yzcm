package novelfetchworkshop

import (
	"encoding/json"
	"fmt"
)

const legacyVideoPendingKey = "modelCatalogLegacyVideoPending"

// Pending rows live outside modelCatalog so neither a read nor a subsequent
// save can turn a database import into an established, executable video row.
// Keep the original locations and fields for the existing conflict archive.
type pendingVideoSource struct {
	legacyScrubEntry
	Raw map[string]any
}

type pendingLegacyVideos struct {
	Rows    []any
	Sources []pendingVideoSource
}

func (s *MySQLStore) preparePendingVideos(owner string, settings map[string]any, rows, boundRows []any, migration *legacyConfigMigration, waiting bool) ([]any, error) {
	var pending pendingLegacyVideos
	binding := LegacyConflictSource{Document: "db:novel_fetch_workshop_configs", Field: legacyVideoPendingKey}
	if stored, exists := settings[legacyVideoPendingKey]; exists {
		envelope, _ := stored.(map[string]any)
		plain, err := s.ModelSecrets.OpenLegacyConflict(owner, binding, ModelConfigText(envelope, "ciphertext"), ModelConfigText(envelope, "fingerprint"))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(plain), &pending); err != nil {
			return nil, fmt.Errorf("decode pending database video sources: %w", err)
		}
	} else if !waiting {
		return rows, nil
	}
	if !waiting {
		existing := rows
		for _, item := range pending.Rows {
			row, _ := item.(map[string]any)
			if _, found := findLegacyTarget(row, existing); !found {
				rows = append(rows, row)
			}
		}
		for _, saved := range pending.Sources {
			model := saved.Raw
			if saved.Video {
				group := map[string]any{"videoProviders": map[string]any{saved.Provider: saved.Raw}}
				model = legacyVideoProviderRows(group)[0].(map[string]any)
			}
			migration.database = append(migration.database, legacySourceEntry{legacyScrubEntry: saved.legacyScrubEntry, raw: saved.Raw, model: model})
		}
		delete(settings, legacyVideoPendingKey)
		return rows, nil
	}
	formal := map[string]bool{}
	for _, item := range boundRows {
		row, _ := item.(map[string]any)
		formal[ModelConfigText(row, "id")] = true
	}
	var active []any
	for _, item := range rows {
		row, _ := item.(map[string]any)
		if ModelConfigText(row, "kind") == "video" && !formal[ModelConfigText(row, "id")] {
			pending.Rows = append(pending.Rows, item)
		} else {
			active = append(active, item)
		}
	}
	var immediate []legacySourceEntry
	for _, source := range migration.database {
		if ModelConfigText(source.model, "kind") == "video" {
			pending.Sources = append(pending.Sources, pendingVideoSource{source.legacyScrubEntry, source.raw})
		} else {
			immediate = append(immediate, source)
		}
	}
	migration.database = immediate
	if len(pending.Rows)+len(pending.Sources) > 0 {
		raw, err := json.Marshal(pending)
		if err != nil {
			return nil, err
		}
		ciphertext, fingerprint, err := s.ModelSecrets.SealLegacyConflict(owner, binding, string(raw))
		if err != nil {
			return nil, err
		}
		settings[legacyVideoPendingKey] = map[string]any{"ciphertext": ciphertext, "fingerprint": fingerprint}
	}
	return active, nil
}
