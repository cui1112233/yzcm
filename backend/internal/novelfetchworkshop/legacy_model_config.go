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
	"sort"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/safepath"
)

const modelLegacyScrubPendingKey = "modelCatalogLegacyScrubPending"
const modelLegacyScrubDoneKey = "modelCatalogLegacyScrubDone"
const legacyVideoMigratedKey = "videoProviderLegacyMigrated"

func hasLegacyVideoProviders(document map[string]any) bool {
	for _, container := range legacyCatalogContainers(document) {
		if _, exists := container["videoProviders"]; exists {
			return true
		}
	}
	return false
}

func legacyVideoProviderRows(container map[string]any) []any {
	providers, _ := container["videoProviders"].(map[string]any)
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]any, 0, len(names))
	for _, name := range names {
		fields, _ := providers[name].(map[string]any)
		provider := batchfactoryv11.NormalizeVideoProviderForHTTP(name)
		candidate := batchfactoryv11.VideoProviderConfig{Provider: provider, Model: ModelConfigText(fields, "model"),
			APIKey: ModelConfigText(fields, "apiKey"), CreateURL: ModelConfigText(fields, "createUrl"),
			TasksURL: ModelConfigText(fields, "tasksUrl"), ResultURL: ModelConfigText(fields, "resultUrl"), WorkflowID: ModelConfigText(fields, "workflowId")}
		row, err := batchfactoryv11.NormalizeVideoProviderConfig(candidate)
		model := map[string]any{"id": "legacy-video:" + provider, "kind": "video", "modelId": row.Model,
			"displayName": row.Model, "provider": provider, "endpoint": row.CreateURL, "tasksEndpoint": row.TasksURL,
			"resultEndpoint": row.ResultURL, "workflowId": row.WorkflowID, "credential": row.APIKey, "enabled": true}
		if err != nil {
			markLegacyKeyReentry(model, "legacy_video_configuration_unusable")
		}
		rows = append(rows, model)
	}
	return rows
}

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
		catalog = append(append([]any(nil), catalog...), legacyVideoProviderRows(container)...)
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
	hasSecret := hasLegacyVideoProviders(document)
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
	if !hasLegacyVideoProviders(document) {
		rows, _ := json.Marshal(legacyCatalogRows(document))
		sum := sha256.Sum256(rows)
		return hex.EncodeToString(sum[:])
	}
	parts := []any{legacyCatalogRows(document)}
	for _, container := range legacyCatalogContainers(document) {
		parts = append(parts, container["videoProviders"])
	}
	rows, _ := json.Marshal(parts)
	sum := sha256.Sum256(rows)
	return hex.EncodeToString(sum[:])
}

func priorLegacyModelDigest(document map[string]any) string {
	copy := cloneMap(document)
	for _, container := range legacyCatalogContainers(copy) {
		delete(container, "videoProviders")
	}
	return legacyCatalogSecretDigest(copy)
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
	if document == nil {
		return fmt.Errorf("legacy model file is unavailable during credential cleanup")
	}
	if current := legacyCatalogSecretDigest(document); current != "" {
		if current != expected {
			return fmt.Errorf("legacy model file changed before credential cleanup")
		}
		for _, container := range legacyCatalogContainers(document) {
			delete(container, "videoProviders")
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
		document[modelLegacyScrubDoneKey] = expected
		replace := s.ReplaceLegacyConfig
		if replace == nil {
			replace = replaceLegacyModelDocument
		}
		if err := replace(path, document); err != nil {
			return err
		}
	} else if ModelConfigText(document, modelLegacyScrubDoneKey) != expected {
		return fmt.Errorf("legacy credential cleanup has no matching completion record")
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
FROM novel_fetch_workshop_configs WHERE JSON_CONTAINS_PATH(settings_json, 'one', '$.modelCatalogLegacyScrubPending',
'$.videoProviders', '$.settings.videoProviders', '$.appConfig.videoProviders', '$.settings.appConfig.videoProviders')`)
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
	indices, compatible := legacyModelCredentialCandidates(current, legacy)
	if !compatible || len(indices) != 1 {
		return nil
	}
	result, _ := legacy[indices[0]].(map[string]any)
	return result
}

func legacyModelCredentialCandidates(current map[string]any, legacy []any) ([]int, bool) {
	id := ModelConfigText(current, "id")
	modelID := ModelConfigText(current, "modelId")
	var matches []int
	compatible := true
	if id != "" {
		for i, item := range legacy {
			candidate, _ := item.(map[string]any)
			if ModelConfigText(candidate, "id") != id {
				continue
			}
			matches = append(matches, i)
			compatible = compatible && sameLegacyModelScope(current, candidate) && modelID != "" && modelID == ModelConfigText(candidate, "modelId")
		}
		if len(matches) != 0 {
			return matches, compatible
		}
	}
	for i, item := range legacy {
		candidate, _ := item.(map[string]any)
		candidateID := ModelConfigText(candidate, "id")
		if !sameLegacyModelScope(current, candidate) || modelID == "" || modelID != ModelConfigText(candidate, "modelId") {
			continue
		}
		matches = append(matches, i)
		compatible = compatible && (id == "" || candidateID == "")
	}
	return matches, compatible
}

func mergeLegacyModelKeys(current, legacy []any) {
	type claim struct {
		model      map[string]any
		candidates []int
		compatible bool
	}
	claims := make([]claim, 0, len(current))
	claimed := make([]int, len(legacy))
	for _, item := range current {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidates, compatible := legacyModelCredentialCandidates(model, legacy)
		for _, candidate := range candidates {
			claimed[candidate]++
		}
		claims = append(claims, claim{model, candidates, compatible})
	}
	for _, claim := range claims {
		model := claim.model
		if len(claim.candidates) == 0 {
			if ModelConfigText(model, "credential") == "" && ModelConfigText(model, "apiKey") == "" {
				markLegacyKeyReentry(model, "legacy_missing")
			}
			continue
		}
		if !claim.compatible || len(claim.candidates) != 1 || claimed[claim.candidates[0]] != 1 {
			markLegacyKeyReentry(model, "legacy_scope_or_mapping_conflict")
			continue
		}
		match, _ := legacy[claim.candidates[0]].(map[string]any)
		keys := map[string]struct{}{}
		for _, row := range []map[string]any{model, match} {
			for _, key := range []string{"credential", "apiKey"} {
				if value := ModelConfigText(row, key); value != "" {
					keys[value] = struct{}{}
				}
			}
		}
		if len(keys) != 1 {
			markLegacyKeyReentry(model, "legacy_credential_conflict")
			continue
		}
		for _, key := range []string{"credential", "apiKey"} {
			if value := ModelConfigText(match, key); value != "" {
				model[key] = value
			}
		}
	}
}

func markLegacyKeyReentry(model map[string]any, reason string) {
	delete(model, "credential")
	delete(model, "apiKey")
	delete(model, "credentialRef")
	model["credentialStatus"] = "needs_reentry"
	model["credentialRecoveryReason"] = reason
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
			markLegacyKeyReentry(prior, "legacy_duplicate_row")
			continue
		}
		byID[id] = model
		catalog = append(catalog, model)
	}
	return catalog
}
