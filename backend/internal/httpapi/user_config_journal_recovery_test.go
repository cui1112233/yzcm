package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
	"testing"
	"time"
)

func TestLegacySettingPatchReviewRoutes(t *testing.T) {
	for _, endpoint := range []string{"/api/config", "/api/settings/api-integrations"} {
		t.Run(endpoint, func(t *testing.T) {
			s := patchTestStore(t)
			ctx := context.Background()
			if _, err := s.DB.Exec("INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES('founder',?,NOW(6),NOW(6))", `{"videoProviders":{"personal_api":{"model":"video-fixture","createUrl":"https://model.test/api"}}}`); err != nil {
				t.Fatal(err)
			}
			config := NewUserConfigStore(s.LegacyConfigDir)
			path, _ := config.configPath("founder")
			if err := writeUserConfigDocument(path, json.RawMessage(`{"apiIntegrations":{"pinned":["openrouterApiKey"],"channelSelection":{"text":{"enabled":["v"]}},"customChannels":[{"id":"v","name":"orig","model":"m","apiKey":"synthetic","params":{"revision":"internal","quality":"hd"}},{"id":"keep","name":"keep"}]}}`)); err != nil {
				t.Fatal(err)
			}
			h, _, _ := newImpersonationTestRouter(t, func(o *RouterOptions) {
				o.AuthStore = &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
				o.UserConfigStore = config
				o.NovelFetchStore = s
			})
			for _, tc := range []struct {
				input   string
				invalid bool
			}{{`{"customChannels":[{"id":"v","name":"new"}]}`, false}, {`{"pinned":["unknown"]}`, true}, {`{"channelSelection":{"text":{}}}`, true}, {`{"channelSelection":{"text":{"enabled":[4]}}}`, true}} {
				body := tc.input
				if endpoint == "/api/config" {
					body = `{"apiIntegrations":` + body + "}"
				}
				reply := impersonationRequest(h, "POST", endpoint, "founder", body, nil)
				if tc.invalid {
					if reply.Code != 400 {
						t.Errorf("invalid input %s returned %d", tc.input, reply.Code)
					}
					continue
				}
				if reply.Code != 200 {
					t.Fatalf("save returned %d", reply.Code)
				}
				if strings.Contains(reply.Body.String(), "internal") {
					t.Error("POST exposed internal parameter")
				}
			}
			got, err := config.GetConfig(ctx, "founder")
			if err != nil {
				t.Fatal(err)
			}
			section := apiIntegrationStoredMap(got)
			chs := apiIntegrationChannels(section)
			if len(chs) != 2 || chs[0].(map[string]any)["name"] != "new" || chs[0].(map[string]any)["model"] != "m" || chs[0].(map[string]any)["apiKey"] != "synthetic" {
				t.Error("rename replaced omitted fields/channels")
			}
			if len(apiIntegrationPinned(section)) != 1 || len(apiIntegrationChannelSelection(section)) != 1 {
				t.Error("invalid input erased existing choices")
			}
			reply := impersonationRequest(h, "GET", "/api/settings/api-integrations", "founder", "", nil)
			if strings.Contains(reply.Body.String(), "internal") || !strings.Contains(reply.Body.String(), "quality") {
				t.Error("GET nested parameter filtering failed")
			}
			clear := `{"pinned":[],"channelSelection":{"text":{"enabled":[]}},"deletedChannelIds":["keep"]}`
			if endpoint == "/api/config" {
				clear = `{"apiIntegrations":` + clear + "}"
			}
			reply = impersonationRequest(h, "POST", endpoint, "founder", clear, nil)
			if reply.Code != 200 {
				t.Fatalf("clear returned %d", reply.Code)
			}
			configAfter, err := config.GetConfig(ctx, "founder")
			if err != nil {
				t.Fatal(err)
			}
			after := apiIntegrationStoredMap(configAfter)
			choice := apiIntegrationChannelSelection(after)["text"]
			if len(apiIntegrationPinned(after)) != 0 || !isExplicitChannelSelection(choice) || len(normalizeChannelSelectionValue(choice)) != 0 || len(apiIntegrationChannels(after)) != 1 {
				t.Error("explicit clears or channel deletion were lost")
			}

		})
	}
}
func TestLegacySettingPatchReviewSnapshot(t *testing.T) {
	var old, restored map[string]any
	json.Unmarshal([]byte(`{"image":{"model":"explicit-model"},"baseUrl":"https://example.test/custom","notifications":{"soundVolume":42},"apiIntegrations":{"pinned":["openrouterApiKey"],"values":{"kimiOfficialApiKey":"stale","seedance2Api":"missing"},"customChannels":[{"id":"v","name":"stale","model":"stale","concurrency":3},{"id":"extra","name":"extra"}]}}`), &old)
	json.Unmarshal([]byte(`{"apiIntegrations":{"pinned":["deepseekOfficialApiKey"],"values":{"kimiOfficialApiKey":"fresh"},"customChannels":[{"id":"v","name":"fresh","model":"fresh","concurrency":6}]}}`), &restored)
	if err := snapshotEdits(old).apply(restored); err != nil {
		t.Fatal(err)
	}
	img, _ := restored["image"].(map[string]any)
	if img["model"] != "explicit-model" || restored["baseUrl"] != "https://example.test/custom" {
		t.Error("lost explicit top-level input")
	}
	section := apiIntegrationStoredMap(restored)
	chs := apiIntegrationChannels(section)
	if len(chs) != 2 || chs[0].(map[string]any)["model"] != "fresh" || chs[0].(map[string]any)["concurrency"] != 6.0 || apiIntegrationValues(section)["kimiOfficialApiKey"] != "fresh" || apiIntegrationValues(section)["seedance2Api"] != "missing" || len(apiIntegrationPinned(section)) != 2 {
		t.Error("snapshot replaced restored fields or lost missing entries")
	}
}
func TestLegacySettingPatchReviewUnsafeArrayAndEmptyFinalization(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	if err := s.AppendLegacySettingEdits(ctx, "a", []settingsEdit{{Path: []string{apiIntegrationsSettingKey, "customChannels"}, Value: json.RawMessage(`[]`)}}); err == nil {
		t.Error("accepted replacement channel array")
	}
	config := NewUserConfigStore(s.LegacyConfigDir)
	config.UseLegacySettingJournal(s)
	archived, _ := UserScopedPath(config.BaseDir, "a", deferredSettingsFile)
	cipher, fp, err := config.secrets.SealLegacyConflict("a", novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}, `[{"path":["apiIntegrations","customChannels"],"value":[]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeUserConfigDocument(archived, deferredSettingsEnvelope{cipher, fp}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.GetConfig(ctx, "a"); err == nil {
		t.Fatal("unsafe archived array accepted")
	}
	if _, err := os.Stat(archived); err != nil {
		t.Fatal("unsafe archived file removed")
	}
	if _, err := s.DB.Exec("UPDATE novel_fetch_legacy_config_migrations SET candidate_ciphertext='synthetic' WHERE owner_username='b'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConfig(ctx, "b"); err == nil {
		t.Error("absent standard file cleared candidates")
	}
	st, _, err := s.LegacyConfigMigrationState(ctx, "b")
	if err != nil || st.Phase != novelfetchworkshop.LegacyFinalizing || st.CandidateEnvelope == "" {
		t.Error("lost unverified migration")
	}
}
func TestLegacySettingPatchReviewImportAfterCompletion(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.journal = s
	path, _ := c.configPath("a")
	if err := writeUserConfigDocument(path, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(filepath.Dir(path), deferredSettingsFile)
	fixture := func(n string) deferredSettingsEnvelope {
		t.Helper()
		cipher, fp, err := c.secrets.SealLegacyConflict("a", novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}, `[{"path":["notifications","soundVolume"],"value":`+n+`}]`)
		if err != nil {
			t.Fatal(err)
		}
		return deferredSettingsEnvelope{cipher, fp}
	}
	first := fixture("42")
	upper := first
	upper.Fingerprint = strings.ToUpper(first.Fingerprint)
	for _, tc := range []struct {
		file deferredSettingsEnvelope
		want float64
	}{{first, 42}, {upper, 51}, {fixture("57"), 57}, {first, 51}} {
		if err := writeUserConfigDocument(old, tc.file); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetConfig(ctx, "a"); err != nil {
			t.Fatal(err)
		}
		doc, err := c.GetConfig(ctx, "a")
		if err != nil || doc["notifications"].(map[string]any)["soundVolume"] != tc.want {
			t.Fatalf("expected restored volume %v, %v", tc.want, err)
		}
		if err := writeUserConfigDocument(path, json.RawMessage(`{"notifications":{"soundVolume":51}}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("verified duplicate file was not retired")
		}
	}
}

type pausedPreferenceWriter struct {
	*configSecretProvider
	entered, release chan struct{}
}

func (p *pausedPreferenceWriter) ApplyLegacySettingEdits(owner, path string, edits []novelfetchworkshop.LegacySettingEdit) error {
	if p.entered != nil {
		close(p.entered)
		p.entered = nil
		<-p.release
	}
	return p.configSecretProvider.ApplyLegacySettingEdits(owner, path, edits)
}

type observedPreferenceWriter struct {
	*configSecretProvider
	started chan struct{}
}

func (p *observedPreferenceWriter) ImportDeferredSettings(ctx context.Context, owner, path string, s *novelfetchworkshop.MySQLStore) (bool, error) {
	if p.started != nil {
		close(p.started)
		p.started = nil
	}
	return p.configSecretProvider.ImportDeferredSettings(ctx, owner, path, s)
}

func TestLegacySettingPatchReviewTwoWritersWithoutWorkshopRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	old := patchTestStore(t)
	c := NewUserConfigStore(old.LegacyConfigDir)
	newer := novelfetchworkshop.NewMySQLStore(old.DB)
	newer.LegacyConfigDir = old.LegacyConfigDir
	started := make(chan struct{})
	newer.ModelSecrets = &observedPreferenceWriter{c.secrets, started}
	path, _ := c.configPath("a")
	if err := writeUserConfigDocument(path, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	old.ModelSecrets = &pausedPreferenceWriter{c.secrets, entered, release}
	appendVolume := func(v int) {
		t.Helper()
		if err := newer.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, map[string]any{"volume": v})); err != nil {
			t.Fatal(err)
		}
	}
	appendVolume(20)
	before, after := make(chan error, 1), make(chan error, 1)
	go func() { _, err := old.GetConfig(ctx, "a"); before <- err }()
	select {
	case <-entered:
	case err := <-before:
		t.Fatalf("old writer failed %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	appendVolume(42)
	go func() { _, err := newer.GetConfig(ctx, "a"); after <- err }()
	select {
	case <-started:
	case err := <-after:
		t.Fatalf("new writer failed %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	secondDone := false
	select {
	case err := <-after:
		secondDone = true
		t.Errorf("second writer finished while old writer could still overwrite: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	close(release)
	if err := <-before; err != nil {
		t.Error(err)
	}
	if !secondDone {
		if err := <-after; err != nil {
			t.Error(err)
		}
	}
	doc, err := c.readStandardFile(path)
	if err != nil || doc["volume"] != 42.0 {
		t.Error("stale writer replaced newer file", err)
	}
}
