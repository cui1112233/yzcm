package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
)

func TestLegacySettingPatchArchivedSnapshotOrderAndEvidence(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"clear-before-name", "name-before-clear", "generated-empty", "ambiguous", "ambiguous-key"} {
		t.Run(mode, func(t *testing.T) {
			s := patchTestStore(t)
			cfg := NewUserConfigStore(s.LegacyConfigDir)
			cfg.UseLegacySettingJournal(s)
			path, _ := cfg.configPath("a")
			if err := writeUserConfigDocument(path, json.RawMessage(`{"apiIntegrations":{"customChannels":[{"id":"v","name":"fresh","concurrency":7,"params":{"keep":true},"apiKey":"synthetic-restored"}]}}`)); err != nil {
				t.Fatal(err)
			}
			channel := func(key string, v any) settingsEdit {
				return fieldEdits([]string{apiIntegrationsSettingKey, "customChannels", "v"}, map[string]any{key: v})[0]
			}
			row := settingsEdits{channel("id", "v"), channel("name", "snapshot"), channel("baseUrl", ""), channel("model", ""), channel("group", ""), channel("enabled", true), channel("apiFormat", "openai_compatible"), channel("concurrency", nil)}
			switch mode {
			case "clear-before-name":
				row = append(row, channel("apiKey", nil), channel("name", "new"))
			case "name-before-clear":
				row = append(row, channel("name", "new"), channel("apiKey", nil))
			case "generated-empty":
				row = append(row, channel("apiKey", ""), channel("name", "new"))
			case "ambiguous-key":
				row = append(row[:2], channel("apiKey", nil), channel("name", "new"))
			case "ambiguous":
				row = append(row, channel("params", nil))
			}
			row = append(row, fieldEdits([]string{"notifications"}, map[string]any{"soundVolume": 42})...)
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			binding := novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}
			cipher, fp, err := cfg.secrets.SealLegacyConflict("a", binding, string(raw))
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(filepath.Dir(path), deferredSettingsFile)
			if err = writeUserConfigDocument(source, deferredSettingsEnvelope{cipher, fp}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.GetConfig(ctx, "a"); err != nil {
				t.Fatal(err)
			}
			got, err := cfg.readStandardFile(path)
			if err != nil {
				t.Fatal(err)
			}
			saved := apiIntegrationChannels(apiIntegrationStoredMap(got))[0].(map[string]any)
			name := "new"
			if mode == "ambiguous" {
				name = "fresh"
			}
			if saved["name"] != name || saved["concurrency"] != 7.0 || got["notifications"].(map[string]any)["soundVolume"] != 42.0 {
				t.Error("lost a restored field or explicit edit")
			}
			if mode == "generated-empty" || mode == "ambiguous" || mode == "ambiguous-key" {
				if saved["apiKey"] != "synthetic-restored" {
					t.Error("generated field erased restored key")
				}
			} else if _, ok := saved["apiKey"]; ok {
				t.Error("discarded explicit key clear")
			}
			if mode == "ambiguous" || mode == "ambiguous-key" {
				if !reflect.DeepEqual(saved["params"], map[string]any{"keep": true}) {
					t.Error("ambiguous delete erased restored params")
				}
				var evidence string
				if err := s.DB.QueryRow("SELECT source_ciphertext FROM novel_fetch_legacy_setting_files WHERE owner_username=? AND fingerprint=?", "a", fp).Scan(&evidence); err != nil {
					t.Fatal("missing recoverable evidence", err)
				}
				plain, err := cfg.secrets.OpenLegacyConflict("a", binding, evidence, fp)
				if err != nil || plain != string(raw) {
					t.Fatal("retained source differs", err)
				}
				if _, err = cfg.secrets.OpenLegacyConflict("b", binding, evidence, fp); err == nil {
					t.Error("source accepted under another account")
				}
			}
			if _, err = os.Stat(source); !os.IsNotExist(err) {
				t.Fatal("active source still present", err)
			}
		})
	}
}

func TestLegacySettingPatchArchivedIntegrationSections(t *testing.T) {
	snap := fieldEdits([]string{apiIntegrationsSettingKey, "customChannels", "v"}, map[string]any{"id": "v", "name": "snapshot"})
	var old, doc map[string]any
	json.Unmarshal([]byte(`{"apiIntegrations":{"pinned":["old","shared"],"values":{"a":"old","missing":"carried","later":"snapshot"},"channelSelection":{"text":{"enabled":[]}}}}`), &old)
	json.Unmarshal([]byte(`{"apiIntegrations":{"pinned":["shared","keep"],"values":{"a":"restored","later":"restored"},"channelSelection":{"text":{"enabled":["keep"]}}}}`), &doc)
	snap = append(snap, fieldEdits(nil, old)...)
	snap = append(snap, fieldEdits([]string{"notifications"}, map[string]any{"soundVolume": 42})...)
	snap = append(snap, fieldEdits([]string{apiIntegrationsSettingKey, "values"}, map[string]any{"later": "explicit"})...)
	if err := snap.apply(doc); err != nil {
		t.Fatal(err)
	}
	section := apiIntegrationStoredMap(doc)
	if !reflect.DeepEqual(section["pinned"], []any{"shared", "keep", "old"}) || !reflect.DeepEqual(section["values"], map[string]any{"a": "restored", "missing": "carried", "later": "explicit"}) || !reflect.DeepEqual(section["channelSelection"], map[string]any{"text": map[string]any{"enabled": []any{"keep"}}}) || doc["notifications"].(map[string]any)["soundVolume"] != 42.0 {
		t.Error("snapshot overwrote or discarded restored settings or a confirmed subsequent edit")
	}
}

func TestLegacySettingPatchPartialChannelRestoresName(t *testing.T) {
	ctx := context.Background()
	s := patchTestStore(t)
	if _, err := s.DB.Exec("INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES('founder',?,NOW(6),NOW(6))", `{"modelCatalog":[{"id":"video","kind":"video","provider":"personal_api","modelId":"synthetic-video","endpoint":"https://service.invalid/video","credential":"synthetic-key"}]}`); err != nil {
		t.Fatal(err)
	}
	cfg := NewUserConfigStore(s.LegacyConfigDir)
	cfg.UseLegacySettingJournal(s)
	h, _, _ := newImpersonationTestRouter(t, func(o *RouterOptions) {
		o.AuthStore = &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
		o.UserConfigStore = cfg
		o.NovelFetchStore = s
	})
	for _, tc := range []struct {
		endpoint, body string
		code           int
	}{
		{"/api/settings/api-integrations", `{"customChannels":[{"id":"v","model":"model-new"}]}`, 200},
		{"/api/config", `{"apiIntegrations":{"customChannels":[{"id":"v","baseUrl":"https://service.invalid"}]}}`, 200},
		{"/api/settings/api-integrations", `{"customChannels":[{"id":"v","name":" "}]}`, 400},
		{"/api/settings/api-integrations", `{"customChannels":[{"model":"unnamed"}]}`, 400},
	} {
		if r := impersonationRequest(h, "POST", tc.endpoint, "founder", tc.body, nil); r.Code != tc.code {
			t.Errorf("%s got %d, want %d (%s)", tc.endpoint, r.Code, tc.code, r.Body.String())
		}
	}
	edits, _, _, err := s.LoadLegacySettingEdits(ctx, "founder")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := cfg.configPath("founder")
	if err = writeUserConfigDocument(path, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err = cfg.secrets.ApplyLegacySettingEdits("founder", path, edits); err == nil {
		t.Error("finalized an incomplete channel without a restored name")
	}
	if err = writeUserConfigDocument(path, json.RawMessage(`{"apiIntegrations":{"customChannels":[{"id":"v","name":"restored"}]}}`)); err != nil {
		t.Fatal(err)
	}
	if err = cfg.secrets.ApplyLegacySettingEdits("founder", path, edits); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.readStandardFile(path)
	if err != nil {
		t.Fatal(err)
	}
	saved := apiIntegrationChannels(apiIntegrationStoredMap(got))[0].(map[string]any)
	if saved["name"] != "restored" || saved["model"] != "model-new" || saved["baseUrl"] != "https://service.invalid" {
		t.Error("partial saves did not recover together")
	}
}
