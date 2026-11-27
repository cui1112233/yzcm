package novelfetchworkshop

import (
	"context"
	"encoding/json"
	"fmt"

	"qiantie/backend/internal/batchfactoryv11"
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

func (source pendingVideoSource) model() map[string]any {
	if !source.Video {
		return source.Raw
	}
	group := map[string]any{"videoProviders": map[string]any{source.Provider: source.Raw}}
	return legacyVideoProviderRows(group)[0].(map[string]any)
}

func (s *MySQLStore) openPendingVideos(owner string, stored any) (pendingLegacyVideos, error) {
	var pending pendingLegacyVideos
	envelope, _ := stored.(map[string]any)
	binding := LegacyConflictSource{Document: "db:novel_fetch_workshop_configs", Field: legacyVideoPendingKey}
	plain, err := s.ModelSecrets.OpenLegacyConflict(owner, binding, ModelConfigText(envelope, "ciphertext"), ModelConfigText(envelope, "fingerprint"))
	if err != nil {
		return pending, err
	}
	if err := json.Unmarshal([]byte(plain), &pending); err != nil {
		return pending, fmt.Errorf("decode pending database video sources: %w", err)
	}
	return pending, nil
}

func (s *MySQLStore) preparePendingVideos(owner string, settings map[string]any, rows, boundRows []any, migration *legacyConfigMigration, waiting bool) ([]any, error) {
	var pending pendingLegacyVideos
	if stored, exists := settings[legacyVideoPendingKey]; exists {
		var err error
		pending, err = s.openPendingVideos(owner, stored)
		if err != nil {
			return nil, err
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
			migration.database = append(migration.database, legacySourceEntry{legacyScrubEntry: saved.legacyScrubEntry, raw: saved.Raw, model: saved.model()})
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
		binding := LegacyConflictSource{Document: "db:novel_fetch_workshop_configs", Field: legacyVideoPendingKey}
		ciphertext, fingerprint, err := s.ModelSecrets.SealLegacyConflict(owner, binding, string(raw))
		if err != nil {
			return nil, err
		}
		settings[legacyVideoPendingKey] = map[string]any{"ciphertext": ciphertext, "fingerprint": fingerprint}
	}
	return active, nil
}

// A returning file is evidence for a staged database credential only when it
// contains a credential for that video. The reconcile step then compares both
// origins. A preferences-only file cannot release any staged row.
func (s *MySQLStore) pendingVideoFileReady(owner string, stored any, file map[string]any) (bool, error) {
	pending, err := s.openPendingVideos(owner, stored)
	if err != nil {
		return false, err
	}
	sources := collectLegacySources(file)
	matches := func(model map[string]any) bool {
		for _, source := range sources {
			keys, valid := legacySourceCredentials(source)
			local := ModelConfigText(model, "provider") == batchfactoryv11.VideoProviderDoubaoLocal &&
				!hasLegacyCredentialValue(model) && sameLegacyVideoTarget(model, source.model)
			if valid && (len(keys) > 0 || local) && ModelConfigText(source.model, "kind") == "video" &&
				(ModelConfigText(model, "id") != "" && ModelConfigText(source.model, "id") == ModelConfigText(model, "id") ||
					ModelConfigText(source.model, "id") == "" && sameLegacyVideoTarget(model, source.model)) {
				return true
			}
		}
		return false
	}
	for _, row := range pending.Rows {
		model, _ := row.(map[string]any)
		if !matches(model) {
			return false, nil
		}
	}
	for _, saved := range pending.Sources {
		if !matches(saved.model()) {
			return false, nil
		}
	}
	return len(pending.Sources)+len(pending.Rows) > 0, nil
}

func (s *MySQLStore) HasPendingLegacyVideos(ctx context.Context, owner string) (bool, error) {
	var pending bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM novel_fetch_workshop_configs
WHERE owner_username=? AND JSON_CONTAINS_PATH(settings_json,'one','$.modelCatalogLegacyVideoPending','$.modelCatalogLegacyScrubPending',
'$.videoProviders','$.settings.videoProviders','$.appConfig.videoProviders','$.settings.appConfig.videoProviders',
'$.settings.modelCatalog','$.appConfig.modelCatalog','$.settings.appConfig.modelCatalog'))`, owner).Scan(&pending)
	return pending, err
}
