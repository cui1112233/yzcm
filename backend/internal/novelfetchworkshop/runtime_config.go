package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The source has been discovered but has never been published as a bound catalog.
var ErrCatalogAwaitingMigration = fmt.Errorf("legacy catalog awaits migration: %w", ErrConfigConflict)

// ReadWorkflowConfig reads preferences without importing or cleaning historical
// model credentials. Store.GetConfig remains the strict migration/admin path.
func ReadWorkflowConfig(ctx context.Context, store Store, owner string) (map[string]any, error) {
	if reader, ok := store.(interface {
		GetWorkflowConfig(context.Context, string) (map[string]any, error)
	}); ok {
		return reader.GetWorkflowConfig(ctx, owner)
	}
	return store.GetConfig(ctx, owner)
}

// ReadRuntimeConfig is a read-only execution view, never a configuration to save.
// Stores without the optional view retain their existing behavior.
func ReadRuntimeConfig(ctx context.Context, store Store, owner string) (map[string]any, error) {
	if reader, ok := store.(interface {
		ReadRuntimeConfig(context.Context, string) (map[string]any, error)
	}); ok {
		return reader.ReadRuntimeConfig(ctx, owner)
	}
	return store.GetConfig(ctx, owner)
}

// ReadRuntimeConfig keeps workflow, formal catalog and pending-video state in
// one SQL snapshot. Bound-key verification is mandatory; legacy finalization is
// deliberately absent, so a retained cleanup source cannot break plain reads.
func (s *MySQLStore) ReadRuntimeConfig(ctx context.Context, owner string) (map[string]any, error) {
	var raw []byte
	var pending bool
	err := s.DB.QueryRowContext(ctx, `SELECT c.settings_json,EXISTS(
SELECT 1 FROM novel_fetch_legacy_config_migrations m WHERE m.owner_username=? AND m.phase!='complete')
FROM novel_fetch_workshop_configs c WHERE c.owner_username=?`, owner, owner).Scan(&raw, &pending)
	if errors.Is(err, sql.ErrNoRows) {
		return EnsureWorkshopDefaults(nil), nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	var catalog any
	if _, exists := snapshot["modelCatalog"]; exists {
		if _, versioned := snapshot[ModelSecretVersionKey]; pending && !versioned && s.ModelSecrets != nil {
			if err := validateLegacyVideoDocument(snapshot); err != nil {
				return nil, err
			}
			for _, row := range runtimeCatalogRows(snapshot["modelCatalog"]) {
				if ModelConfigText(row, "kind") == "video" {
					return nil, ErrCatalogAwaitingMigration
				}
			}
		}
		if s.ModelSecrets == nil || snapshot[ModelSecretVersionKey] != ModelSecretVersion {
			return nil, ErrConfigConflict
		}
		if err := s.ModelSecrets.OpenModelConfig(owner, snapshot); err != nil {
			return nil, err
		}
		catalog = snapshot["modelCatalog"]
		if pending {
			for _, item := range runtimeCatalogRows(catalog) {
				if strings.EqualFold(ModelConfigText(item, "kind"), "video") {
					item["enabled"], item["runtimeAvailable"] = false, false
					item["runtimeReason"] = "legacy_video_migration_pending"
					delete(item, "credential")
					delete(item, "apiKey")
				}
			}
		}
	}
	workflow := runtimeWorkflowPreferences(snapshot)
	if catalog != nil {
		workflow["modelCatalog"] = catalog
	}
	return EnsureWorkshopDefaults(workflow), nil
}

func runtimeCatalogRows(value any) []map[string]any {
	result := []map[string]any{}
	switch rows := value.(type) {
	case []any:
		for _, row := range rows {
			if model, ok := row.(map[string]any); ok {
				result = append(result, model)
			}
		}
	case []map[string]any:
		result = rows
	}
	return result
}

func runtimeWorkflowPreferences(snapshot map[string]any) map[string]any {
	settings := workflowPreferencesForWrite(cloneMap(snapshot))
	if version, exists := snapshot["modelCatalogVersion"]; exists {
		settings["modelCatalogVersion"] = version
	}
	return settings
}
