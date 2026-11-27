package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	top := map[string]any{}
	for key, value := range old {
		if key != apiIntegrationsSettingKey && !isManagedModelField(key) && !internalPreferenceField(key) {
			top[key] = value
		}
	}
	edits := snapshotAssignments(nil, top, defaultUserConfig)
	stored, _ := old[apiIntegrationsSettingKey].(map[string]any)
	section := maps.Clone(stored)
	channelDefaults := map[string]any{"enabled": true, "apiFormat": "openai_compatible", "model": defaultUserConfig["model"], "baseUrl": defaultUserConfig["baseUrl"]}
	channels, _ := section["customChannels"].([]any)
	for _, v := range channels {
		row, _ := v.(map[string]any)
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		row = maps.Clone(row)
		delete(row, "id")
		edits = append(edits, snapshotAssignments([]string{apiIntegrationsSettingKey, "customChannels", id}, row, channelDefaults)...)
	}
	delete(section, "customChannels")
	edits = append(edits, snapshotAssignments([]string{apiIntegrationsSettingKey}, section, nil)...)
	for i := range edits {
		edits[i].OnlyIfMissing = true
	}
	return edits
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
	if journal == nil {
		return false, errors.New("legacy settings import requires migration storage")
	}
	convertMap := func(old map[string]any) []novelfetchworkshop.LegacySettingEdit { return snapshotEdits(old) }
	if err := journal.ImportLegacySettingFile(ctx, owner, envelope.Ciphertext, envelope.Fingerprint, discover, convertMap); err != nil {
		return false, err
	}
	_, _, active, err := journal.LoadLegacySettingEdits(ctx, owner)
	if err != nil {
		return false, err
	}
	current, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return active, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(raw, current) {
		return false, novelfetchworkshop.ErrConfigConflict
	}
	if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return active, nil
}

func (s *UserConfigStore) writeSettingsPatch(ctx context.Context, owner string, config map[string]any, changes settingsEdits) error {
	if s.journal == nil {
		if err := validateEditedChannelNames(config, changes); err != nil {
			return err
		}
		return s.writeAuthenticatedConfig(owner, config)
	}
	state, found, err := s.journal.DiscoverLegacyConfigMigration(ctx, owner)
	if err != nil {
		return err
	}
	pending := found && state.Phase != novelfetchworkshop.LegacyComplete
	write := func(completed bool) error {
		path, err := s.configPath(owner)
		if err != nil {
			return err
		}
		current, err := s.readStandardFile(path)
		if err != nil {
			return err
		}
		if completed && current == nil || current["ciphertext"] != nil || current["fingerprint"] != nil {
			return errors.New("standard configuration unavailable")
		}
		if !completed {
			current = mergeConfigMap(defaultUserConfig, current)
		}
		if err := changes.apply(current); err != nil {
			return err
		}
		if err := validateEditedChannelNames(current, changes); err != nil {
			return err
		}
		return s.writeAuthenticatedConfig(owner, current)
	}
	for {
		if pending {
			err = s.journal.AppendLegacySettingEdits(ctx, owner, changes)
			if !errors.Is(err, novelfetchworkshop.ErrConfigConflict) {
				return err
			}
		}
		written, err := s.journal.WriteStandardSettingsIfReady(ctx, owner, write)
		if err != nil || written {
			return err
		}
		pending = true
	}
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
	if len(edits) == 0 {
		return nil
	}
	before, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := settingsEdits(edits).apply(config); err != nil {
		return err
	}
	if err := validateEditedChannelNames(config, edits); err != nil {
		return err
	}
	expected, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if bytes.Equal(before, expected) {
		return nil
	}
	if err := s.writeAuthenticatedConfig(owner, config); err != nil {
		return err
	}
	verified, err := s.readStandardFile(path)
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
