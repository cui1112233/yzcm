package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"qiantie/backend/internal/novelfetchworkshop"
)

const deferredSettingsFile = "deferred-settings.json"

type deferredSettingsEnvelope struct {
	Ciphertext  string `json:"ciphertext"`
	Fingerprint string `json:"fingerprint"`
}

func snapshotAssignments(path []string, values, defaults map[string]any) settingsEdits {
	var edits settingsEdits
	for key, value := range values {
		fallback := defaults[key]
		if nested, ok := value.(map[string]any); ok {
			child, _ := fallback.(map[string]any)
			edits = append(edits, snapshotAssignments(append(path, key), nested, child)...)
			continue
		}
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
		case []any:
			if len(v) == 0 {
				continue
			}
		case float64:
			if v == 0 {
				continue
			}
		}
		raw, _ := json.Marshal(value)
		base, _ := json.Marshal(fallback)
		if !bytes.Equal(raw, base) {
			edits = append(edits, fieldEdits(path, map[string]any{key: value})...)
		}
	}
	return edits
}
func snapshotEdits(old map[string]any) settingsEdits {
	edits := snapshotAssignments(nil, preferenceOnlyPatch(old), defaultUserConfig)
	section, _ := old[apiIntegrationsSettingKey].(map[string]any)
	channelDefaults := map[string]any{"enabled": true, "apiFormat": "openai_compatible", "model": defaultUserConfig["model"], "baseUrl": defaultUserConfig["baseUrl"]}
	channels, _ := section["customChannels"].([]any)
	for _, v := range channels {
		row, _ := v.(map[string]any)
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		delete(row, "id")
		edits = append(edits, snapshotAssignments([]string{apiIntegrationsSettingKey, "customChannels", id}, row, channelDefaults)...)
	}
	delete(section, "customChannels")
	return append(edits, snapshotAssignments([]string{apiIntegrationsSettingKey}, section, nil)...)
}

// Called with the configuration file lock held. The file remains in place
// until both the database commit and a separate authenticated read succeed.
func (p *configSecretProvider) ImportDeferredSettings(ctx context.Context, owner, path string, journal *novelfetchworkshop.MySQLStore) (bool, error) {
	return p.importDeferredSettings(ctx, owner, path, journal, false)
}
func (p *configSecretProvider) importDeferredSettings(ctx context.Context, owner, path string, journal *novelfetchworkshop.MySQLStore, discover bool) (bool, error) {
	legacyPath := filepath.Join(filepath.Dir(path), deferredSettingsFile)
	raw, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var envelope deferredSettingsEnvelope
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return false, err
	}
	plain, err := p.OpenLegacyConflict(owner, novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}, envelope.Ciphertext, envelope.Fingerprint)
	if err != nil {
		return false, err
	}
	var edits settingsEdits
	switch data := bytes.TrimSpace([]byte(plain)); {
	case len(data) > 0 && data[0] == '[':
		if err := json.Unmarshal(data, &edits); err != nil {
			return false, err
		}
	case len(data) > 0 && data[0] == '{':
		var old map[string]any
		if err := json.Unmarshal(data, &old); err != nil {
			return false, err
		}
		edits = snapshotEdits(old)
	default:
		return false, errors.New("invalid deferred settings document")
	}
	if journal == nil {
		return false, errors.New("legacy settings import requires migration storage")
	}
	if discover {
		if _, _, err := journal.DiscoverLegacyConfigMigration(ctx, owner); err != nil {
			return false, err
		}
	}
	if err := journal.ImportLegacySettingEdits(ctx, owner, edits); err != nil {
		return false, err
	}
	if _, _, active, err := journal.LoadLegacySettingEdits(ctx, owner); err != nil {
		return false, err
	} else if !active {
		return false, novelfetchworkshop.ErrConfigConflict
	}
	return true, os.Remove(legacyPath)
}

func (s *UserConfigStore) writeSettingsPatch(ctx context.Context, owner string, config map[string]any, changes settingsEdits) error {
	if s.journal != nil {
		state, found, err := s.journal.DiscoverLegacyConfigMigration(ctx, owner)
		if err != nil {
			return err
		}
		if found {
			if state.Phase != novelfetchworkshop.LegacyComplete {
				err = s.journal.AppendLegacySettingEdits(ctx, owner, changes)
				if !errors.Is(err, novelfetchworkshop.ErrConfigConflict) {
					return err
				}
			}
			// Completion may have published a newer revision since the form read.
			// Merge only this request into the verified standard document.
			path, err := s.configPath(owner)
			if err != nil {
				return err
			}
			current, err := s.readStandardFile(path)
			if err != nil {
				return err
			}
			if current == nil || current["ciphertext"] != nil || current["fingerprint"] != nil {
				return errors.New("completed standard configuration unavailable")
			}
			if err := changes.apply(current); err != nil {
				return err
			}
			return s.writeAuthenticatedConfig(owner, current)
		}
	}
	return s.writeAuthenticatedConfig(owner, config)
}

func (p *configSecretProvider) ApplyLegacySettingEdits(owner, path string, edits []novelfetchworkshop.LegacySettingEdit) error {
	s := &UserConfigStore{BaseDir: filepath.Dir(filepath.Dir(path)), secrets: p}
	config, err := s.readStandardFile(path)
	if err != nil {
		return err
	}
	if config == nil || config["ciphertext"] != nil || config["fingerprint"] != nil {
		return errors.New("standard configuration unavailable during replay")
	}
	if err := settingsEdits(edits).apply(config); err != nil {
		return err
	}
	if err := s.writeAuthenticatedConfig(owner, config); err != nil {
		return err
	}
	verified, err := s.readStandardFile(path)
	if err != nil {
		return err
	}
	expected, err := json.Marshal(config)
	if err != nil {
		return err
	}
	actual, err := json.Marshal(verified)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, actual) {
		return fmt.Errorf("standard configuration verification failed")
	}
	return nil
}
