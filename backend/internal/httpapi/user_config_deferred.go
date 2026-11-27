package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"qiantie/backend/internal/novelfetchworkshop"
)

// Only explicit user updates belong in this file. Merging defaults or a copy
// of the old catalog here would hide unrelated values when the file returns.
const deferredSettingsFile = "deferred-settings.json"

type deferredSettingsEnvelope struct {
	Ciphertext  string `json:"ciphertext"`
	Fingerprint string `json:"fingerprint"`
}

func (s *UserConfigStore) deferredSettings(owner string) (settingsEdits, error) {
	if s == nil || s.BaseDir == "" {
		return nil, nil
	}
	path, err := UserScopedPath(s.BaseDir, owner, deferredSettingsFile)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored deferredSettingsEnvelope
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	binding := novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}
	plain, err := s.secrets.OpenLegacyConflict(owner, binding, stored.Ciphertext, stored.Fingerprint)
	if err != nil {
		return nil, err
	}
	var edits settingsEdits
	if err := json.Unmarshal([]byte(plain), &edits); err != nil {
		// Older encrypted files contained maps. Empty generated sections had
		// no delete intent. Preserve restored fields which those sections hid.
		var old map[string]any
		if err := json.Unmarshal([]byte(plain), &old); err != nil {
			return nil, err
		}
		if section, ok := old[apiIntegrationsSettingKey].(map[string]any); ok {
			if channels, ok := section["customChannels"].([]any); ok {
				delete(section, "customChannels")
				for _, v := range channels {
					row, _ := v.(map[string]any)
					if id, _ := row["id"].(string); id != "" {
						edits = append(edits, fieldEdits([]string{apiIntegrationsSettingKey, "customChannels", id}, row)...)
					}
				}
			}
			if pins, ok := section["pinned"].([]any); ok && len(pins) == 0 {
				delete(section, "pinned")
			}
		}
		edits = append(edits, fieldEdits(nil, old)...)
	}
	for _, edit := range edits {
		if len(edit.Path) == 0 {
			return nil, fmt.Errorf("invalid deferred settings path")
		}
	}
	if edits == nil {
		edits = settingsEdits{}
	}
	return edits, nil
}

func (s *UserConfigStore) writeSettingsPatch(ctx context.Context, owner string, config map[string]any, changes settingsEdits, catalog novelfetchworkshop.Store) error {
	saved, err := s.deferredSettings(owner)
	if err != nil {
		return err
	}
	lookup, ok := catalog.(interface {
		HasPendingLegacyVideos(context.Context, string) (bool, error)
	})
	if !ok && saved == nil {
		return s.writeAuthenticatedConfig(owner, config)
	}
	if ok {
		if discover, ok := catalog.(interface {
			DiscoverLegacyConfigMigration(context.Context, string) (novelfetchworkshop.LegacyConfigMigrationState, bool, error)
		}); ok {
			if _, _, err := discover.DiscoverLegacyConfigMigration(ctx, owner); err != nil {
				return err
			}
		}
		pending, err := lookup.HasPendingLegacyVideos(ctx, owner)
		if err != nil {
			return err
		}
		if !pending {
			if saved != nil {
				path, err := s.configPath(owner)
				if err != nil {
					return err
				}
				if err := s.secrets.FinalizeLegacySettings(owner, path); err != nil {
					return err
				}
				config = s.readLegacyConfig(owner)
				changes.apply(config)
			}
			return s.writeAuthenticatedConfig(owner, config)
		}
	}
	raw, err := json.Marshal(append(saved, changes...))
	if err != nil {
		return err
	}
	binding := novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}
	ciphertext, fingerprint, err := s.secrets.SealLegacyConflict(owner, binding, string(raw))
	if err != nil {
		return err
	}
	path, err := UserScopedPath(s.BaseDir, owner, deferredSettingsFile)
	if err != nil {
		return err
	}
	return writeUserConfigDocument(path, deferredSettingsEnvelope{ciphertext, fingerprint})
}

// FinalizeLegacySettings is called with the owner's configuration writer
// locked. Assignments and tombstones can be replayed if removal is interrupted.
func (p *configSecretProvider) FinalizeLegacySettings(owner, path string) error {
	s := &UserConfigStore{BaseDir: filepath.Dir(filepath.Dir(path)), secrets: p}
	edits, err := s.deferredSettings(owner)
	if err != nil || edits == nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	if config == nil || config["ciphertext"] != nil && config["fingerprint"] != nil {
		return fmt.Errorf("standard configuration is unavailable for deferred settings")
	}
	edits.apply(config)
	delete(config, novelfetchworkshop.LegacyModelDefaultsFileKey)
	if err := s.writeAuthenticatedConfig(owner, config); err != nil {
		return err
	}
	return os.Remove(filepath.Join(filepath.Dir(path), deferredSettingsFile))
}
