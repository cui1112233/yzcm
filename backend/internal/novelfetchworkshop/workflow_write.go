package novelfetchworkshop

import (
	"context"
	"encoding/json"
	"strings"
)

// WriteWorkflowConfig saves preferences, never a decrypted runtime view.
func WriteWorkflowConfig(ctx context.Context, store Store, owner string, settings map[string]any) error {
	if writer, ok := store.(interface {
		PutWorkflowConfig(context.Context, string, map[string]any) error
	}); ok {
		return writer.PutWorkflowConfig(ctx, owner, settings)
	}
	if _, sliced := store.(interface {
		GetWorkflowConfig(context.Context, string) (map[string]any, error)
	}); sliced {
		return ErrConfigConflict
	}
	return store.PutConfig(ctx, owner, settings)
}

func protectedWorkflowField(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	return strings.HasPrefix(key, "modelcatalog") || key == "videoproviders" || key == "videoproviderlegacymigrated"
}

func containsProtectedWorkflowField(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if protectedWorkflowField(key) || containsProtectedWorkflowField(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsProtectedWorkflowField(child) {
				return true
			}
		}
	}
	return false
}

// Strip every protected field, including historical aliases in nested forms.
// The returned read version is required by the workflow-only writer.
func workflowPreferencesForWrite(settings map[string]any) map[string]any {
	if settings == nil {
		settings = map[string]any{}
	}
	var strip func(any)
	strip = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if protectedWorkflowField(key) {
					delete(value, key)
				} else {
					strip(child)
				}
			}
		case []any:
			for _, child := range value {
				strip(child)
			}
		}
	}
	strip(settings)
	settings[ConfigRevisionKey] = configRevision(settings)
	return EnsureWorkshopDefaults(settings)
}

// Keep model subtrees opaque, including unknown fields and ciphertext.
func restoreProtectedWorkflowJSON(previous, current map[string]json.RawMessage) (bool, error) {
	restored := false
	for key, raw := range previous {
		if protectedWorkflowField(key) {
			current[key] = raw
			restored = true
			continue
		}
		var oldObject map[string]json.RawMessage
		if json.Unmarshal(raw, &oldObject) == nil && oldObject != nil {
			var nextObject map[string]json.RawMessage
			_ = json.Unmarshal(current[key], &nextObject)
			if nextObject == nil {
				nextObject = map[string]json.RawMessage{}
			}
			changed, err := restoreProtectedWorkflowJSON(oldObject, nextObject)
			if err != nil {
				return false, err
			}
			if changed {
				encoded, err := json.Marshal(nextObject)
				if err != nil {
					return false, err
				}
				current[key] = encoded
				restored = true
			}
		} else {
			var list []any
			if json.Unmarshal(raw, &list) == nil && containsProtectedWorkflowField(list) {
				current[key] = raw
				restored = true
			}
		}
	}
	return restored, nil
}

// PutWorkflowConfig retains private models and migration evidence. Revision
// validation and preference replacement share the same database row lock.
func (s *MySQLStore) PutWorkflowConfig(ctx context.Context, owner string, settings map[string]any) error {
	if settings == nil || settings[ConfigRevisionKey] == nil {
		return ErrConfigConflict
	}
	// Check the JSON that will actually be stored, so typed maps, RawMessage
	// and custom marshalers cannot hide a model subtree from the check.
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return err
	}
	if containsProtectedWorkflowField(normalized) {
		return ErrConfigConflict
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,'{}',CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))`, owner); err != nil {
		return err
	}
	var previousRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&previousRaw); err != nil {
		return err
	}
	var previous map[string]any
	if err = json.Unmarshal(previousRaw, &previous); err != nil {
		return err
	}
	next, err := checkedConfigRevision(previous, settings)
	if err != nil {
		return err
	}
	var oldJSON, nextJSON map[string]json.RawMessage
	if err = json.Unmarshal(previousRaw, &oldJSON); err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &nextJSON); err != nil {
		return err
	}
	if _, err = restoreProtectedWorkflowJSON(oldJSON, nextJSON); err != nil {
		return err
	}
	nextJSON[ConfigRevisionKey], err = json.Marshal(next)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(nextJSON)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=CAST(? AS CHAR CHARACTER SET utf8mb4), updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=?`, string(raw), owner); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	settings[ConfigRevisionKey] = next
	return nil
}
