package novelfetchworkshop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"qiantie/backend/internal/safepath"
)

const modelLegacyScrubPendingKey = "modelCatalogLegacyScrubPending"

func legacyCatalogContainers(document map[string]any) []map[string]any {
	nested, _ := document["settings"].(map[string]any)
	app, _ := document["appConfig"].(map[string]any)
	nestedApp, _ := nested["appConfig"].(map[string]any)
	return []map[string]any{document, nested, app, nestedApp}
}

func legacyCatalogRows(document map[string]any) []any {
	var rows []any
	for _, container := range legacyCatalogContainers(document) {
		catalog, _ := container["modelCatalog"].([]any)
		for _, item := range catalog {
			duplicate := false
			for _, previous := range rows {
				if reflect.DeepEqual(item, previous) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				rows = append(rows, item)
			}
		}
	}
	return rows
}

func legacyCatalogSecretDigest(document map[string]any) string {
	var hasSecret bool
	for _, item := range legacyCatalogRows(document) {
		model, _ := item.(map[string]any)
		for _, field := range []string{"credential", "apiKey", "credentialRef"} {
			if _, present := model[field]; present {
				hasSecret = true
			}
		}
	}
	if !hasSecret {
		return ""
	}
	rows, _ := json.Marshal(legacyCatalogRows(document))
	sum := sha256.Sum256(rows)
	return hex.EncodeToString(sum[:])
}

func (s *MySQLStore) legacyModelFilePath(owner string) (string, error) {
	if s.LegacyConfigDir == "" {
		return "", nil
	}
	return safepath.UserPath(s.LegacyConfigDir, owner, "api-config.json")
}

func readLegacyModelDocument(path string) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	for _, container := range legacyCatalogContainers(document) {
		if raw := container["modelCatalog"]; raw != nil {
			if _, ok := raw.([]any); !ok {
				return nil, fmt.Errorf("legacy model catalog is not a list")
			}
		}
	}
	return document, nil
}

func (s *MySQLStore) scrubLegacyModelFile(ctx context.Context, owner, expected string) error {
	if s.LegacyConfigLocker != nil {
		s.LegacyConfigLocker.Lock()
		defer s.LegacyConfigLocker.Unlock()
	}
	path, err := s.legacyModelFilePath(owner)
	if err != nil {
		return err
	}
	document, err := readLegacyModelDocument(path)
	if err != nil {
		return err
	}
	if current := legacyCatalogSecretDigest(document); current != "" {
		if current != expected {
			return fmt.Errorf("legacy model file changed before credential cleanup")
		}
		for _, container := range legacyCatalogContainers(document) {
			models, _ := container["modelCatalog"].([]any)
			for _, item := range models {
				model, ok := item.(map[string]any)
				if !ok {
					continue
				}
				removed := false
				for _, field := range []string{"credential", "apiKey", "credentialRef"} {
					if _, exists := model[field]; exists {
						delete(model, field)
						removed = true
					}
				}
				if removed {
					model["credentialStatus"] = "imported_or_needs_reentry"
				}
			}
		}
		replace := s.ReplaceLegacyConfig
		if replace == nil {
			replace = replaceLegacyModelDocument
		}
		if err := replace(path, document); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs
SET settings_json=JSON_REMOVE(settings_json, '$.modelCatalogLegacyScrubPending')
WHERE owner_username=? AND JSON_UNQUOTE(JSON_EXTRACT(settings_json, '$.modelCatalogLegacyScrubPending'))=?`, owner, expected)
	return err
}

func replaceLegacyModelDocument(path string, document map[string]any) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".api-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(document); err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return os.Rename(temporary.Name(), path)
}

// ResumeLegacyModelCleanup retries previously committed file cleanup after a restart.
func (s *MySQLStore) ResumeLegacyModelCleanup(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT owner_username
FROM novel_fetch_workshop_configs WHERE JSON_CONTAINS_PATH(settings_json, 'one', '$.modelCatalogLegacyScrubPending')`)
	if err != nil {
		return err
	}
	var pending []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, owner)
	}
	err = rows.Err()
	rows.Close()
	for _, owner := range pending {
		if _, e := s.GetConfig(ctx, owner); e != nil {
			err = errors.Join(err, fmt.Errorf("clean legacy model configuration: %w", e))
		}
	}
	return err
}

func sameLegacyModelScope(left, right map[string]any) bool {
	target := ModelCredentialDestination(left)
	kind := ModelConfigText(left, "kind")
	return kind != "" && target.Endpoint != "" && kind == ModelConfigText(right, "kind") &&
		ModelCredentialSource(left) == ModelCredentialSource(right) && target == ModelCredentialDestination(right)
}

func legacyModelCredentialCandidate(current map[string]any, legacy []any) map[string]any {
	id := ModelConfigText(current, "id")
	modelID := ModelConfigText(current, "modelId")
	var match map[string]any
	if id != "" {
		for _, item := range legacy {
			candidate, _ := item.(map[string]any)
			if ModelConfigText(candidate, "id") != id {
				continue
			}
			if match != nil || !sameLegacyModelScope(current, candidate) {
				return nil
			}
			match = candidate
		}
		if match != nil {
			return match
		}
	}
	for _, item := range legacy {
		candidate, _ := item.(map[string]any)
		candidateID := ModelConfigText(candidate, "id")
		if !sameLegacyModelScope(current, candidate) || modelID == "" || modelID != ModelConfigText(candidate, "modelId") {
			continue
		}
		if match != nil || id != "" && candidateID != "" {
			return nil
		}
		match = candidate
	}
	return match
}

func mergeLegacyModelKeys(current, legacy []any) {
	for _, item := range current {
		model, ok := item.(map[string]any)
		if !ok || ModelConfigText(model, "credential") != "" || ModelConfigText(model, "apiKey") != "" {
			continue
		}
		match := legacyModelCredentialCandidate(model, legacy)
		for _, key := range []string{"credential", "apiKey"} {
			if value := ModelConfigText(match, key); value != "" {
				model[key] = value
			}
		}
		if ModelConfigText(model, "credential") == "" && ModelConfigText(model, "apiKey") == "" {
			model["credentialStatus"] = "needs_reentry"
		}
	}
}

func normalizeLegacyModelRows(rows []any) []any {
	catalog := make([]any, 0, len(rows))
	byID := make(map[string]map[string]any, len(rows))
	for _, item := range rows {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := ModelConfigText(model, "id")
		if id == "" {
			raw, _ := json.Marshal([]any{ModelConfigText(model, "kind"), ModelConfigText(model, "modelId"), ModelCredentialSource(model), ModelCredentialDestination(model)})
			sum := sha256.Sum256(raw)
			id = "legacy:" + hex.EncodeToString(sum[:16])
			model["id"] = id
		}
		if prior := byID[id]; prior != nil {
			delete(prior, "credential")
			delete(prior, "apiKey")
			delete(prior, "credentialRef")
			prior["credentialStatus"] = "needs_reentry"
			continue
		}
		byID[id] = model
		catalog = append(catalog, model)
	}
	return catalog
}
