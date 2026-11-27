package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"qiantie/backend/internal/novelfetchworkshop"
)

// Only explicit user updates belong in this file. Merging defaults or a copy
// of the old catalog here would hide unrelated values when the file returns.
const deferredSettingsFile = "deferred-settings.json"

type deferredSettingsEnvelope struct {
	Ciphertext  string `json:"ciphertext"`
	Fingerprint string `json:"fingerprint"`
}

func (s *UserConfigStore) deferredSettings(owner string) (map[string]any, error) {
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
	var saved map[string]any
	if err := json.Unmarshal([]byte(plain), &saved); err != nil {
		return nil, err
	}
	return saved, nil
}

func (s *UserConfigStore) writeSettingsPatch(ctx context.Context, owner string, config, patch map[string]any, catalog novelfetchworkshop.Store) error {
	saved, err := s.deferredSettings(owner)
	if err != nil {
		return err
	}
	if saved == nil {
		lookup, ok := catalog.(interface {
			HasPendingLegacyVideos(context.Context, string) (bool, error)
		})
		if !ok {
			return s.writeAuthenticatedConfig(owner, config)
		}
		pending, err := lookup.HasPendingLegacyVideos(ctx, owner)
		if err != nil {
			return err
		}
		if !pending {
			return s.writeAuthenticatedConfig(owner, config)
		}
	}
	next := mergeConfigMap(saved, patch)
	if _, updated := patch[apiIntegrationsSettingKey]; updated {
		next[apiIntegrationsSettingKey] = config[apiIntegrationsSettingKey]
	}
	raw, err := json.Marshal(next)
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
