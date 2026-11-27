package novelfetchworkshop

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"qiantie/backend/internal/apisecrets"
)

type boundTextSnapshot struct {
	revision string
	index    int
	model    map[string]any
	rawModel json.RawMessage
}

// Decode only the formal row being repaired. Unrelated models and legacy
// recovery evidence never pass through a float64 or a rewritten JSON document.
func readBoundTextSnapshot(raw []byte, id string) (boundTextSnapshot, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return boundTextSnapshot{}, false, err
	}
	var version string
	if err := json.Unmarshal(fields[ModelSecretVersionKey], &version); err != nil || version != ModelSecretVersion {
		return boundTextSnapshot{}, false, nil
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(fields["modelCatalog"], &rows); err != nil {
		return boundTextSnapshot{}, true, err
	}
	found := -1
	for i, row := range rows {
		var identity struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(row, &identity); err != nil {
			return boundTextSnapshot{}, true, err
		}
		if identity.ID != id {
			continue
		}
		if found >= 0 {
			return boundTextSnapshot{}, true, ErrConfigConflict
		}
		found = i
		if identity.Kind != "text" {
			return boundTextSnapshot{}, false, nil
		}
	}
	if found < 0 {
		return boundTextSnapshot{}, false, nil
	}
	var model map[string]any
	decoder := json.NewDecoder(bytes.NewReader(rows[found]))
	decoder.UseNumber()
	if err := decoder.Decode(&model); err != nil {
		return boundTextSnapshot{}, true, err
	}
	meta := map[string]any{}
	if rawRevision := fields[ConfigRevisionKey]; len(rawRevision) > 0 {
		var revision any
		decoder := json.NewDecoder(bytes.NewReader(rawRevision))
		decoder.UseNumber()
		if err := decoder.Decode(&revision); err != nil {
			return boundTextSnapshot{}, true, err
		}
		meta[ConfigRevisionKey] = revision
	}
	return boundTextSnapshot{revision: configRevision(meta), index: found, model: model, rawModel: rows[found]}, true, nil
}

func boundTextCredentialInput(patch map[string]any) (credential string, clear, eligible bool, err error) {
	for key := range patch {
		switch key {
		case "credential", "clearCredential", "enabled", "name", "displayName", "modelId", "baseUrl", "endpoint", "kind", "id", ConfigRevisionKey:
		default:
			return "", false, false, nil
		}
	}
	if raw, present := patch["credential"]; present {
		value, ok := raw.(string)
		if !ok || len(value) > 8192 || apisecrets.IsEncrypted(strings.TrimSpace(value)) {
			return "", false, true, ErrConfigConflict
		}
		credential = strings.TrimSpace(value)
	}
	if raw, present := patch["clearCredential"]; present {
		value, ok := raw.(bool)
		if !ok {
			return "", false, true, ErrConfigConflict
		}
		clear = value
	}
	if clear && credential != "" {
		return "", false, true, ErrConfigConflict
	}
	return credential, clear, clear || credential != "", nil
}

// Full custom-model forms echo their unchanged label and target fields. They
// may enter the repair path only when no model metadata would actually change.
func boundTextCredentialEchoMatches(patch, model map[string]any) bool {
	label := ModelConfigText(model, "displayName")
	if label == "" {
		label = ModelConfigText(model, "name")
	}
	if label == "" {
		label = ModelConfigText(model, "modelId")
	}
	for key, value := range patch {
		switch key {
		case "credential", "clearCredential", ConfigRevisionKey:
			continue
		case "name", "displayName":
			text, ok := value.(string)
			if !ok || text != label {
				return false
			}
		case "enabled":
			enabled, ok := value.(bool)
			if !ok {
				return false
			}
			current := true
			if raw, present := model[key]; present {
				current, _ = raw.(bool)
			}
			if enabled != current {
				return false
			}
		default:
			text, ok := value.(string)
			if !ok || text != ModelConfigText(model, key) {
				return false
			}
		}
	}
	return true
}

// TryPatchBoundTextCredential repairs only an existing bound-v3 text row. A
// false handled result leaves creation/target edits/video changes on the old
// strict administrative path. No legacy discovery, replay or cleanup runs here.
func (s *MySQLStore) TryPatchBoundTextCredential(ctx context.Context, owner, id string, patch map[string]any) (map[string]any, bool, error) {
	credential, clear, eligible, err := boundTextCredentialInput(patch)
	if err != nil || !eligible {
		return nil, eligible, err
	}
	var raw []byte
	err = s.DB.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	before, eligible, err := readBoundTextSnapshot(raw, id)
	if err != nil || !eligible {
		return nil, eligible, err
	}
	if !boundTextCredentialEchoMatches(patch, before.model) {
		return nil, false, nil
	}
	if expected, present := patch[ConfigRevisionKey]; present && fmt.Sprint(expected) != before.revision {
		return nil, true, ErrConfigConflict
	}
	if s.ModelSecrets == nil {
		return nil, true, ErrConfigConflict
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, true, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&raw); err != nil {
		return nil, true, err
	}
	current, eligible, err := readBoundTextSnapshot(raw, id)
	if err != nil {
		return nil, true, err
	}
	if !eligible || current.revision != before.revision || current.index != before.index || !bytes.Equal(current.rawModel, before.rawModel) {
		return nil, true, ErrConfigConflict
	}
	nextRevision, err := checkedConfigRevision(map[string]any{ConfigRevisionKey: current.revision}, map[string]any{ConfigRevisionKey: before.revision})
	if err != nil {
		return nil, true, err
	}
	previous := map[string]any{ModelSecretVersionKey: ModelSecretVersion, "modelCatalog": []any{current.model}}
	opened := cloneMap(previous)
	if err := s.ModelSecrets.OpenModelConfig(owner, opened); err != nil {
		return nil, true, err
	}
	models, ok := opened["modelCatalog"].([]any)
	if !ok || len(models) != 1 {
		return nil, true, ErrConfigConflict
	}
	updated, ok := models[0].(map[string]any)
	if !ok {
		return nil, true, ErrConfigConflict
	}
	updated["credential"] = credential
	delete(updated, "apiKey")
	if !clear {
		delete(updated, "credentialStatus")
		delete(updated, "credentialRecoveryReason")
	}
	if err := s.ModelSecrets.SealModelConfig(owner, opened, previous); err != nil {
		return nil, true, err
	}
	encrypted, ok := updated["credential"].(string)
	if !ok {
		return nil, true, ErrConfigConflict
	}
	if credential != "" && encrypted == credential {
		return nil, true, ErrConfigConflict
	}
	path := fmt.Sprintf("$.modelCatalog[%d]", current.index)
	query := `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET(JSON_REMOVE(settings_json,?,?,?),?,?,'$.configRevision',?),updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.configRevision')),'0')=?`
	args := []any{path + ".apiKey", path + ".credentialStatus", path + ".credentialRecoveryReason", path + ".credential", encrypted, nextRevision, owner, current.revision}
	if clear {
		query = `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET(JSON_REMOVE(settings_json,?),?,?,'$.configRevision',?),updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.configRevision')),'0')=?`
		args = []any{path + ".apiKey", path + ".credential", "", nextRevision, owner, current.revision}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, true, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, true, err
	}
	if affected != 1 {
		return nil, true, ErrConfigConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, true, err
	}
	return updated, true, nil
}
