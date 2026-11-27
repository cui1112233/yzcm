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

func TestLegacySettingPatchRoundTwoHistoricalChannelEcho(t *testing.T) {
	ctx := context.Background()
	s := patchTestStore(t)
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.UseLegacySettingJournal(s)
	path, _ := c.configPath("a")
	if err := writeUserConfigDocument(path, json.RawMessage(`{"apiIntegrations":{"customChannels":[{"id":"v","name":"restored","model":"keep","apiKey":"synthetic-current","concurrency":7}]}}`)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(filepath.Dir(path), deferredSettingsFile)
	save := func(id string) {
		t.Helper()
		cipher, fp, err := c.secrets.SealLegacyConflict("a", novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}, `[{"path":["apiIntegrations","customChannels","v","model"],"value":""},{"path":["apiIntegrations","customChannels","v","name"],"value":"snapshot"},{"path":["apiIntegrations","customChannels","v","id"],"value":"`+id+`"},{"path":["apiIntegrations","customChannels","v","concurrency"],"delete":true},{"path":["apiIntegrations","customChannels","v","enabled"],"value":true},{"path":["notifications","soundVolume"],"value":42},{"path":["apiIntegrations","customChannels","v","name"],"value":"explicit"}]`)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeUserConfigDocument(file, deferredSettingsEnvelope{cipher, fp}); err != nil {
			t.Fatal(err)
		}
	}
	save("other")
	if _, err := c.GetConfig(ctx, "a"); err == nil {
		t.Fatal("historical ID mutation accepted")
	}
	save("v")
	if _, err := s.GetConfig(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	saved, err := c.GetConfig(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	row := apiIntegrationChannels(apiIntegrationStoredMap(saved))[0].(map[string]any)
	if saved["notifications"].(map[string]any)["soundVolume"] != 42.0 || row["model"] != "keep" || row["apiKey"] != "synthetic-current" || row["concurrency"] != 7.0 || row["name"] != "explicit" || row["enabled"] != nil {
		t.Fatal("historical snapshot defaults erased restored settings or explicit edits")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("verified legacy file not retired")
	}
}
func TestLegacySettingPatchRoundTwoLongChannelID(t *testing.T) {
	c := NewUserConfigStore(t.TempDir())
	h, _, _ := newImpersonationTestRouter(t, func(o *RouterOptions) {
		o.AuthStore = &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
		o.UserConfigStore = c
	})
	id := strings.Repeat("z", 65)
	for _, tc := range []struct {
		payload string
		want    int
	}{
		{`{"customChannels":[{"id":"` + id + `","name":"short","model":"first"}]}`, 200},
		{`{"customChannels":[{"id":"` + id + `","model":"updated"}]}`, 200},
		{`{"channelSelection":{"text":{"enabled":["` + id + `"]}}}`, 400},
		{`{"deletedChannelIds":["` + id + `"]}`, 200},
	} {
		r := impersonationRequest(h, "POST", "/api/settings/api-integrations", "founder", tc.payload, nil)
		if r.Code != tc.want {
			t.Errorf("expected %d, got %d for %s", tc.want, r.Code, tc.payload)
		}
	}
	if len(apiIntegrationChannels(apiIntegrationStoredMap(c.read("founder")))) != 0 {
		t.Fatal("long channel ID remains after explicit removal")
	}
}
func TestLegacySettingPatchRoundTwoNoopDeletionPreservesBytes(t *testing.T) {
	c := NewUserConfigStore(t.TempDir())
	path, _ := c.configPath("a")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, original := range []string{
		`{ "apiIntegrations": { "values": {"kimiOfficialApiKey":"synthetic"} } }`,
		`{ "apiIntegrations": { "customChannels": null } }`,
		`{ "apiIntegrations": { "customChannels": [] } }`,
	} {
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		if err := c.secrets.ApplyLegacySettingEdits("a", path, settingsEdits{{Path: []string{apiIntegrationsSettingKey, "customChannels", "no-such-channel"}, Delete: true}}); err != nil {
			t.Error(err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != original {
			t.Fatal("deleting an absent channel rewrote the standard file", err)
		}
	}
}
func TestLegacySettingPatchRoundTwoNormalSaveAndReopenedReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	replay := patchTestStore(t)
	owner := "founder"
	if _, err := replay.DB.Exec("INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES(?,'complete')", owner); err != nil {
		t.Fatal(err)
	}
	normal := novelfetchworkshop.NewMySQLStore(replay.DB)
	normal.LegacyConfigDir = replay.LegacyConfigDir
	c := NewUserConfigStore(normal.LegacyConfigDir)
	normal.ModelSecrets = c.secrets
	c.UseLegacySettingJournal(normal)
	path, _ := c.configPath(owner)
	if err := writeUserConfigDocument(path, json.RawMessage(`{"storageRoot":"old","notifications":{"soundVolume":60}}`)); err != nil {
		t.Fatal(err)
	}
	h, _, _ := newImpersonationTestRouter(t, func(o *RouterOptions) {
		o.AuthStore = &authTestStore{account: auth.Account{Username: owner, Active: true, IsOwner: true}}
		o.UserConfigStore = c
		o.NovelFetchStore = normal
	})
	// Occupy the same writer row used by a second service during replay.
	guard, err := replay.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Rollback()
	if _, err = guard.ExecContext(ctx, "INSERT INTO novel_fetch_legacy_setting_files(owner_username,fingerprint) VALUES(?,'') ON DUPLICATE KEY UPDATE fingerprint=fingerprint", owner); err != nil {
		t.Fatal(err)
	}
	normalDone := make(chan int, 1)
	go func() {
		normalDone <- impersonationRequest(h, "POST", "/api/config", owner, `{"storageRoot":"new-root"}`, nil).Code
	}()
	finished := false
	select {
	case status := <-normalDone:
		finished = true
		t.Errorf("ordinary HTTP save bypassed the owner writer lock: %d", status)
	case <-time.After(250 * time.Millisecond):
	}
	importer := NewUserConfigStore(replay.LegacyConfigDir)
	importer.UseLegacySettingJournal(replay)
	cipher, fp, err := importer.secrets.SealLegacyConflict(owner, novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}, `[{"path":["notifications","soundVolume"],"value":42}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeUserConfigDocument(filepath.Join(filepath.Dir(path), deferredSettingsFile), deferredSettingsEnvelope{cipher, fp}); err != nil {
		t.Fatal(err)
	}
	if _, err = importer.GetConfig(ctx, owner); err != nil {
		t.Fatal(err)
	}
	replayDone := make(chan error, 1)
	go func() { _, err := replay.GetConfig(ctx, owner); replayDone <- err }()
	if err = guard.Commit(); err != nil {
		t.Fatal(err)
	}
	if !finished {
		if status := <-normalDone; status != 200 {
			t.Errorf("ordinary save status %d", status)
		}
	}
	if err := <-replayDone; err != nil {
		t.Fatal(err)
	}
	file, err := importer.readStandardFile(path)
	if err != nil || file["storageRoot"] != "new-root" || file["notifications"].(map[string]any)["soundVolume"] != 42.0 {
		t.Fatal("standard settings lost storageRoot or volume", err)
	}
}

func TestLegacySettingPatchRoundTwoIdentityEchoAloneIsNoop(t *testing.T) {
	target := map[string]any{}
	echo := settingsEdit{Path: []string{apiIntegrationsSettingKey, "customChannels", "v", "id"}, Value: json.RawMessage(`"v"`)}
	if err := echo.Validate(); err != nil {
		t.Error(err)
	}
	if err := (settingsEdits{echo}).apply(target); err != nil || len(target) != 0 {
		t.Fatalf("historical identity-only row has no assignment: %v", err)
	}
}

func TestLegacySettingPatchRoundTwoPreferencesOnlyFileIsNotCatalogInput(t *testing.T) {
	ctx := context.Background()
	s := patchTestStore(t)
	c := NewUserConfigStore(s.LegacyConfigDir)
	path, _ := c.configPath("a")
	if err := writeUserConfigDocument(path, map[string]any{"storageRoot": "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConfig(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM novel_fetch_workshop_configs WHERE owner_username='a'").Scan(&count); err != nil || count != 0 {
		t.Fatal("replaying ordinary settings created a competing catalog revision", count, err)
	}
}
