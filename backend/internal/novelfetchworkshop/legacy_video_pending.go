package novelfetchworkshop

import (
	"encoding/json"
	"fmt"
	"reflect"

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
	for _, container := range LegacyCatalogContainers(doc) {
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

// Rebuild precisely the projection written into Rows by the old pending
// envelope writer. The untouched Sources entry remains the authority for
// matching a returning file, including its original adapter and catalog.
func (source pendingVideoSource) historicalRow() map[string]any {
	if !source.Video {
		return cloneMap(source.Raw)
	}
	old := source.model()
	delete(old, "adapterKind")
	delete(old, "catalogSource")
	provider := ModelConfigText(old, "provider")
	if ModelConfigText(source.Raw, "apiKey") == "" && provider != batchfactoryv11.VideoProviderDoubaoLocal {
		// Missing credentials used to stop normalization before URL defaults.
		old["endpoint"] = ModelConfigText(source.Raw, "createUrl")
		old["tasksEndpoint"] = ModelConfigText(source.Raw, "tasksUrl")
		old["resultEndpoint"] = ModelConfigText(source.Raw, "resultUrl")
		if provider != batchfactoryv11.VideoProviderPersonalAPI {
			old["modelId"] = ModelConfigText(source.Raw, "model")
			old["displayName"] = old["modelId"]
		}
	}
	return old
}

func (p pendingLegacyVideos) orphanIndices() []int {
	if len(p.Rows) == 0 {
		return nil
	}
	claimed := make([]bool, len(p.Rows))
	for _, source := range p.Sources {
		if !validLegacyVideoFields(source.Raw) {
			continue
		}
		projected := source.historicalRow()
		var missingKey map[string]any
		if ModelConfigText(projected, "credential") == "" && ModelConfigText(projected, "apiKey") == "" {
			// For an unbound catalog the old writer ran mergeLegacyModelKeys
			// with no file rows before saving this otherwise identical record.
			missingKey = cloneMap(projected)
			markLegacyKeyReentry(missingKey, "legacy_missing")
		}
		for i, item := range p.Rows {
			row, ok := item.(map[string]any)
			if !claimed[i] && ok && (reflect.DeepEqual(row, projected) || missingKey != nil && reflect.DeepEqual(row, missingKey)) {
				claimed[i] = true
				break
			}
		}
	}
	var orphan []int
	for i := range p.Rows {
		if !claimed[i] {
			orphan = append(orphan, i)
		}
	}
	return orphan
}
