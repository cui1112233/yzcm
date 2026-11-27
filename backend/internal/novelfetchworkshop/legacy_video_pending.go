package novelfetchworkshop

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

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

func (p pendingLegacyVideos) any() bool { return len(p.Rows) != 0 || len(p.Sources) != 0 }

func hasBoundCatalog(settings map[string]any) bool {
	return settings[ModelSecretVersionKey] == ModelSecretVersion || settings[ModelSecretVersionKey] == "bound-v2"
}

func (s *MySQLStore) databaseVideoCandidates(owner string, settings map[string]any) (pendingLegacyVideos, error) {
	var pending pendingLegacyVideos
	if old := settings[legacyVideoPendingKey]; old != nil {
		if s.ModelSecrets == nil {
			return pending, fmt.Errorf("encrypted legacy candidate requires model configuration encryption")
		}
		var err error
		pending, err = s.openPendingVideos(owner, old)
		if err != nil {
			return pending, err
		}
	}
	bound := hasBoundCatalog(settings)
	for _, source := range collectLegacyVideoCandidates(settings) {
		if bound && !source.Video && source.Container == 0 {
			continue
		}
		pending.Sources = append(pending.Sources, pendingVideoSource{source.legacyScrubEntry, source.raw})
	}
	return pending, nil
}

func (s *MySQLStore) migrationCandidates(state LegacyConfigMigrationState) (pendingLegacyVideos, error) {
	var pending pendingLegacyVideos
	raw, err := s.ModelSecrets.OpenLegacyConflict(state.Owner, videoCandidatesBinding(), state.CandidateEnvelope, state.CandidateFingerprint)
	if err != nil {
		return pending, err
	}
	err = json.Unmarshal([]byte(raw), &pending)
	return pending, err
}

// A missing identity field can use a provider default. Explicit nulls, numbers
// and objects have no trustworthy interpretation as destinations or model IDs.
func validLegacyVideoFields(raw map[string]any) bool {
	if raw == nil {
		return false
	}
	for _, field := range []string{"id", "kind", "model", "modelId", "provider", "adapterKind", "catalogSource", "createUrl", "tasksUrl", "resultUrl", "baseUrl", "endpoint", "tasksEndpoint", "resultEndpoint", "workflowId"} {
		if value, exists := raw[field]; exists {
			if _, ok := value.(string); !ok {
				return false
			}
		}
	}
	return true
}

func validLegacyProviderIdentity(name string, raw map[string]any) bool {
	if !validLegacyVideoFields(raw) {
		return false
	}
	provider := ModelConfigText(raw, "provider")
	return provider == "" || batchfactoryv11.NormalizeVideoProviderForHTTP(provider) == batchfactoryv11.NormalizeVideoProviderForHTTP(name)
}

func validateLegacyVideoDocument(doc map[string]any) error {
	for _, container := range legacyCatalogContainers(doc) {
		if value, exists := container["videoProviders"]; exists {
			providers, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid legacy video provider list")
			}
			for name, item := range providers {
				raw, _ := item.(map[string]any)
				if !validLegacyProviderIdentity(name, raw) {
					return fmt.Errorf("invalid legacy video provider identity")
				}
			}
		}
		catalog, _ := container["modelCatalog"].([]any)
		for _, item := range catalog {
			raw, _ := item.(map[string]any)
			if ModelConfigText(raw, "kind") == "video" && !validLegacyVideoFields(raw) {
				return fmt.Errorf("invalid legacy video catalog identity")
			}
		}
	}
	return nil
}

// The previous writer stopped normalizing nonlocal provider rows at a
// missing key. Recognize that exact generated identity using its original
// provider fields; an independently stored row still needs file evidence.
func (source pendingVideoSource) producedRow(row map[string]any) bool {
	if !validLegacyVideoFields(row) || !validLegacyVideoFields(source.Raw) {
		return false
	}
	canonical := source.model()
	if reflect.DeepEqual(row, canonical) {
		return true
	}
	if !source.Video || hasLegacyCredentialValue(row) || hasLegacyCredentialValue(source.Raw) {
		return false
	}
	provider := ModelConfigText(canonical, "provider")
	if provider == batchfactoryv11.VideoProviderDoubaoLocal {
		return false
	}
	old := cloneMap(canonical)
	old["endpoint"] = ModelConfigText(source.Raw, "createUrl")
	old["tasksEndpoint"] = ModelConfigText(source.Raw, "tasksUrl")
	old["resultEndpoint"] = ModelConfigText(source.Raw, "resultUrl")
	if provider != batchfactoryv11.VideoProviderPersonalAPI {
		old["modelId"] = ModelConfigText(source.Raw, "model")
	}
	delete(old, "adapterKind")
	delete(old, "catalogSource")
	for _, field := range []string{"id", "kind", "modelId", "provider", "baseUrl", "endpoint", "tasksEndpoint", "resultEndpoint", "workflowId", "adapterKind", "catalogSource"} {
		if ModelConfigText(old, field) != ModelConfigText(row, field) {
			return false
		}
	}
	return true
}

func (p pendingLegacyVideos) orphanRows() []map[string]any {
	var orphan []map[string]any
	for _, item := range p.Rows {
		row, _ := item.(map[string]any)
		if !slices.ContainsFunc(p.Sources, func(source pendingVideoSource) bool { return source.producedRow(row) }) {
			orphan = append(orphan, row)
		}
	}
	return orphan
}

func (p pendingLegacyVideos) fileReady(file map[string]any) bool {
	if validateLegacyVideoDocument(file) != nil {
		return false
	}
	sources := collectLegacyVideoCandidates(file)
	matches := func(model map[string]any) bool {
		for _, source := range sources {
			keys, valid := legacySourceCredentials(source)
			local := ModelConfigText(model, "provider") == batchfactoryv11.VideoProviderDoubaoLocal && !hasLegacyCredentialValue(model)
			fileID, databaseID := ModelConfigText(source.model, "id"), ModelConfigText(model, "id")
			if valid && (len(keys) > 0 || local) && sameLegacyVideoTarget(model, source.model) && (fileID == "" || databaseID == "" || fileID == databaseID) {
				return true
			}
		}
		return false
	}
	for _, row := range p.orphanRows() {
		if !validLegacyVideoFields(row) || !matches(row) {
			return false
		}
	}
	for _, saved := range p.Sources {
		if !validLegacyVideoFields(saved.Raw) || (saved.Video && !validLegacyProviderIdentity(saved.Provider, saved.Raw)) || !matches(saved.model()) {
			return false
		}
	}
	return p.any()
}

// Keep formal rows and non-video rows in place. Restore the original database
// videos only after the returning source passes the same identity checks.
func (p pendingLegacyVideos) prepareRows(rows, boundRows []any, migration *legacyConfigMigration) []any {
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
	// Older pending envelopes also stored a derived Rows list. The source
	// entries retain the original fields; use a derived row only when the
	// previous writer omitted its source (for example a keyless catalog row).
	var restored []any
	for _, saved := range p.Sources {
		source := legacySourceEntry{legacyScrubEntry: saved.legacyScrubEntry, raw: saved.Raw, model: saved.model()}
		migration.database = append(migration.database, source)
		if !slices.ContainsFunc(restored, func(prior any) bool { return reflect.DeepEqual(prior, source.model) }) {
			restored = append(restored, source.model)
		}
	}
	for _, row := range p.orphanRows() {
		restored = append(restored, row)
	}
	existing := active
	for _, item := range restored {
		row, _ := item.(map[string]any)
		if _, found := findLegacyTarget(row, existing); !found {
			active = append(active, cloneMap(row))
		}
	}
	return active
}
