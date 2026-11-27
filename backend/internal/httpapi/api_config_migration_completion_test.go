package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/storage"
)

// Older tests use this shorthand only for fixtures known to be readable.
func (s *UserConfigStore) read(username string) map[string]any {
	config, err := s.GetConfig(context.Background(), username)
	if err != nil {
		panic(err)
	}
	return config
}

func TestAPIIntegrationDeferredChannelEdits(t *testing.T) {
	previous := []any{map[string]any{"id": "remove"}, map[string]any{"id": "keep", "apiKey": "previous-fixture"}}
	input := map[string]any{"id": "keep", "name": "renamed", "apiKey": "  ", "params": map[string]any{}}
	channel, ok := sanitizeIntegrationChannel(input, previous[1].(map[string]any))
	if !ok {
		t.Fatal("valid patch rejected")
	}
	edits := integrationChannelEdits(previous, []map[string]any{input}, []any{channel})
	config := map[string]any{apiIntegrationsSettingKey: map[string]any{"customChannels": []any{
		map[string]any{"id": "keep", "apiKey": "original-fixture", "baseUrl": "preserved", "params": map[string]any{"quality": "hd"}},
		map[string]any{"id": "remove"},
		map[string]any{"id": "previously-offline"},
	}}}
	edits.apply(config)
	edits.apply(config)
	channels := apiIntegrationChannels(apiIntegrationStoredMap(config))
	if len(channels) != 2 || channels[1].(map[string]any)["id"] != "previously-offline" {
		t.Fatal("channel removal did not preserve the unseen channel")
	}
	row := channels[0].(map[string]any)
	if row["name"] != "renamed" || row["apiKey"] != "original-fixture" || row["baseUrl"] != "preserved" || row["params"] != nil {
		t.Fatal("channel did not preserve omitted fields or clear explicitly emptied params")
	}
}

func TestAPIConfigPolicyMySQLDeferredMigrationCompletion(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	migrations := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	if err := storage.RunMigrations(ctx, db, append(migrations, storage.NovelFetchWorkshopMigrations()...)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := NewUserConfigStore(dir)
	store := novelfetchworkshop.NewMySQLStore(db)
	store.LegacyConfigDir, store.ModelSecrets = dir, config.ModelConfigSecrets()
	store.LegacyConfigLocker = config.LegacyModelFileLocker()
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	t.Logf("disposable MySQL schema=%s", schema)
	login := auth.NewMySQLStore(db)
	now := time.Now().UTC()
	roleFixture := &impersonationTestStore{identities: map[string]accountcenter.Identity{}}
	router := NewRouter(RouterOptions{AuthStore: login, AccountCenterStore: roleFixture, NovelFetchStore: store, UserConfigStore: config, ScriptHistoryStore: NewScriptHistoryStore(t.TempDir()), Now: func() time.Time { return now }})
	insert := func(t *testing.T, owner string) string {
		t.Helper()
		raw := `{"modelCatalogSecretVersion":"bound-v3","modelCatalog":[],"videoProviders":{"personal_api":{"model":"expected-video","apiKey":"fixture-A","createUrl":"https://expected.example.test/video"}}}`
		if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, owner, raw); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, owner, "api-config.json")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write := func(t *testing.T, path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pending := func(t *testing.T, owner string) bool {
		t.Helper()
		result, err := store.HasPendingLegacyVideos(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	t.Run("database-only", func(t *testing.T) {
		insert(t, "database-only")
		single := novelfetchworkshop.NewMySQLStore(db)
		single.ModelSecrets = config.ModelConfigSecrets()
		deferPreferences, err := single.HasPendingLegacyVideos(ctx, "database-only")
		if err != nil || deferPreferences {
			t.Fatal("a database-only import should save preferences in the standard file", err)
		}
		settings, err := single.GetConfig(ctx, "database-only")
		rows := modelCatalog(settings)
		if err != nil || len(rows) != 1 || rows[0]["credential"] != "fixture-A" {
			t.Fatal("database-only import did not retain its single source", err)
		}
		if _, found, err := single.LegacyConfigMigrationState(ctx, "database-only"); err != nil || found {
			t.Fatal("database-only import created migration state", err)
		}
	})
	for _, tc := range []struct {
		owner, document string
		preFirst        bool
	}{
		{"first-ordinary", `{"notifications":{"soundVolume":34}}`, true},
		{"first-empty", `{}`, true},
		{"first-envelope", `{"ciphertext":"fixture-envelope","fingerprint":"fixture-fingerprint"}`, true},
		{"wrong-model", `{"videoProviders":{"personal_api":{"model":"other-video","apiKey":"fixture-B","createUrl":"https://expected.example.test/video"}}}`, false},
		{"wrong-address", `{"videoProviders":{"personal_api":{"model":"expected-video","apiKey":"fixture-B","createUrl":"https://other.example.test/video"}}}`, false},
		{"wrong-workflow", `{"videoProviders":{"personal_api":{"model":"expected-video","apiKey":"fixture-B","createUrl":"https://expected.example.test/video","workflowId":"other-workflow"}}}`, false},
	} {
		t.Run(tc.owner, func(t *testing.T) {
			path := insert(t, tc.owner)
			if !tc.preFirst {
				if _, err := store.GetConfig(ctx, tc.owner); err != nil {
					t.Fatal(err)
				}
			}
			write(t, path, tc.document)
			settings, err := store.GetConfig(ctx, tc.owner)
			if err != nil {
				t.Fatal(err)
			}
			rows, _, err := store.ListLegacyModelConflicts(ctx, tc.owner, "")
			if err != nil || len(rows) != 0 || !pending(t, tc.owner) || len(modelCatalog(settings)) != 0 {
				t.Error("unverified file prematurely consumed the protected database source")
			}
			write(t, path, `{"modelCatalogLegacyDefaultsFile":true,"videoProviders":{"personal_api":{"model":"expected-video","apiKey":"fixture-B","createUrl":"https://expected.example.test/video"}}}`)
			if _, err := store.GetConfig(ctx, tc.owner); err != nil {
				t.Fatal(err)
			}
			rows, _, err = store.ListLegacyModelConflicts(ctx, tc.owner, "")
			if err != nil || len(rows) != 2 || pending(t, tc.owner) {
				t.Error("matching original with a stale file flag did not reconcile both sources")
			}
		})
	}
	for _, interrupted := range []bool{false, true} {
		name := "journal-merge"
		if interrupted {
			name = "journal-retry"
		}
		t.Run(name, func(t *testing.T) {
			path := insert(t, name)
			if err := login.CreateAccount(ctx, auth.Account{Username: name, PasswordHash: "fixture", PasswordAlgorithm: "fixture", Active: true, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			roleFixture.identities[name] = accountcenter.Identity{Username: name, Role: accountcenter.RoleFounder, Active: true}
			if err := login.CreateSession(ctx, auth.Session{Username: name, Token: name, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			const video = `{"id":"v","kind":"video","provider":"personal_api","modelId":"expected-video","endpoint":"https://expected.example.test/video","credential":"fixture-A"}`
			if _, err := db.Exec("UPDATE novel_fetch_workshop_configs SET settings_json=? WHERE owner_username=?", `{"modelCatalog":[`+video+`]}`, name); err != nil {
				t.Fatal(err)
			}
			reply := impersonationRequest(router, "POST", "/api/config", name, `{"notifications":{"soundVolume":42,"soundEnabled":false}}`, nil)
			if reply.Code != http.StatusOK {
				t.Fatalf("first preference save: %d", reply.Code)
			}
			state, found, err := store.LegacyConfigMigrationState(ctx, name)
			if err != nil || !found || state.PatchEnvelope == "" {
				t.Fatal("first preference save did not create the encrypted MySQL journal", err)
			}
			reply = impersonationRequest(router, "POST", "/api/settings/api-integrations", name, `{"pinned":["openrouterApiKey"],"values":{"kimiOfficialApiKey":null},"customChannels":[{"id":"old","name":"renamed"}]}`, nil)
			if reply.Code != http.StatusOK {
				t.Fatalf("integration patch: %d", reply.Code)
			}
			journal := filepath.Join(filepath.Dir(path), deferredSettingsFile)
			if _, err := os.Stat(journal); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("new save created the old deferred file")
			}
			write(t, path, `{"ciphertext":"synthetic-only","fingerprint":"synthetic-only"}`)
			reply = impersonationRequest(router, "GET", "/api/config", name, "", nil)
			if reply.Code != http.StatusOK || strings.Contains(reply.Body.String(), "ciphertext") || strings.Contains(reply.Body.String(), "fingerprint") {
				t.Fatal("public view exposed envelope")
			}
			write(t, path, `{"modelCatalog":[`+strings.Replace(video, "fixture-A", "fixture-B", 1)+`],"notifications":{"soundVolume":60},"apiIntegrations":{"values":{"deepseekOfficialApiKey":"fixture-preserved","kimiOfficialApiKey":"fixture-clear"},"customChannels":[{"id":"old","name":"old","model":"original-model","apiKey":"fixture-channel"},{"id":"untouched","name":"untouched"}],"channelSelection":{"text":{"enabled":["old"]}}},"unrelated":"keep"}`)
			cold := novelfetchworkshop.NewMySQLStore(db)
			cold.LegacyConfigDir, cold.ModelSecrets, cold.LegacyConfigLocker = dir, config.ModelConfigSecrets(), config.LegacyModelFileLocker()
			if interrupted {
				if err := os.Mkdir(path+".tmp", 0700); err != nil {
					t.Fatal(err)
				}
				if _, err := store.GetConfig(ctx, name); err == nil {
					t.Fatal("write interruption acknowledged")
				}
				state, _, err := cold.LegacyConfigMigrationState(ctx, name)
				if err != nil || state.Phase != novelfetchworkshop.LegacyFinalizing || state.PatchEnvelope == "" {
					t.Fatal("lost journal on interruption", err)
				}
				if err := os.Remove(path + ".tmp"); err != nil {
					t.Fatal(err)
				}
				cold.ModelSecrets = &concurrentPatchWriter{configSecretProvider: config.secrets, store: cold}
			}
			if _, err := cold.GetConfig(ctx, name); err != nil {
				t.Fatal(err)
			}
			state, _, err = cold.LegacyConfigMigrationState(ctx, name)
			if err != nil || state.Phase != novelfetchworkshop.LegacyComplete || state.PatchEnvelope != "" {
				t.Fatal("latest revision not completed", err)
			}
			records, _, err := cold.ListLegacyModelConflicts(ctx, name, "")
			if err != nil || len(records) != 2 {
				t.Fatal("video conflict did not retain both origins", err)
			}
			saved := config.read(name)
			notifications := saved["notifications"].(map[string]any)
			if notifications["soundVolume"] != float64(42) || notifications["soundEnabled"] != false || interrupted && notifications["petVisible"] != false {
				t.Fatal("lost explicit preference edits during replay")
			}
			integration := apiIntegrationStoredMap(saved)
			values := apiIntegrationValues(integration)
			channels := apiIntegrationChannels(integration)
			if values["deepseekOfficialApiKey"] != "fixture-preserved" || values["kimiOfficialApiKey"] != nil || len(channels) != 2 || channels[0].(map[string]any)["name"] != "renamed" || channels[0].(map[string]any)["apiKey"] != "fixture-channel" || channels[0].(map[string]any)["model"] != "original-model" || len(apiIntegrationChannelSelection(integration)) != 1 || len(apiIntegrationPinned(integration)) != 1 || saved["unrelated"] != "keep" {
				t.Fatal("standard settings lost or overwritten")
			}
			reply = impersonationRequest(router, "POST", "/api/config", name, `{"notifications":{"soundVolume":51}}`, nil)
			if reply.Code != http.StatusOK {
				t.Fatal("ordinary post-migration preference write failed")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var out struct{ Notifications struct{ SoundVolume int } }
			if json.Unmarshal(raw, &out) != nil || out.Notifications.SoundVolume != 51 || bytes.Contains(raw, []byte("fixture-preserved")) {
				t.Error("post-migration write did not use the encrypted standard configuration")
			}
			write(t, journal, `{"ciphertext":"invalid","fingerprint":"invalid"}`)
			reply = impersonationRequest(router, "GET", "/api/storage/list", name, "", nil)
			if reply.Code != http.StatusServiceUnavailable {
				t.Errorf("storage scan status %d, want configuration verification error", reply.Code)
			}
			called := false
			_, err = YZCMUsageCredentialResolver(config, func(context.Context, string) (map[string]any, error) { called = true; return nil, nil })(ctx, name, "fixture")
			if err == nil || called {
				t.Error("usage resolver masked settings verification failure with another source")
			}
		})
	}
}

func stateTestStore(t *testing.T) *novelfetchworkshop.MySQLStore {
	t.Helper()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(context.Background(), db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := novelfetchworkshop.NewMySQLStore(db)
	store.LegacyConfigDir, store.ModelSecrets = dir, NewUserConfigStore(dir).ModelConfigSecrets()
	return store
}

func TestLegacyConfigMigrationStateCoversAllVideoCandidates(t *testing.T) {
	ctx := context.Background()
	store := stateTestStore(t)
	row := `{"id":"v","kind":"video","provider":"personal_api","modelId":"expected-video","endpoint":"https://expected.example.test/video"}`
	for _, tc := range []struct {
		name, raw string
		container int
		provider  bool
	}{
		{"top-level", `{"modelCatalog":[` + row + `]}`, 0, false},
		{"providers", `{"videoProviders":{"personal_api":{"model":"expected-video","createUrl":"https://expected.example.test/video"}}}`, 0, true},
		{"nested", `{"settings":{"appConfig":{"modelCatalog":[` + row + `]}}}`, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.DB.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, tc.name, tc.raw); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(store.LegacyConfigDir, tc.name)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "api-config.json"), []byte(`{"notifications":{"soundVolume":34}}`), 0600); err != nil {
				t.Fatal(err)
			}
			settings, err := store.GetConfig(ctx, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := store.HasPendingLegacyVideos(ctx, tc.name)
			if err != nil || !pending || len(modelCatalog(settings)) != 0 {
				t.Fatalf("ordinary preferences released keyless video: pending=%v rows=%d err=%v", pending, len(modelCatalog(settings)), err)
			}
			state, ok, err := store.LegacyConfigMigrationState(ctx, tc.name)
			if err != nil || !ok || state.Phase != "awaiting_source" {
				t.Fatalf("phase=%s found=%v err=%v", state.Phase, ok, err)
			}
			binding := novelfetchworkshop.LegacyConflictSource{Document: "db:legacy_config_migrations", Field: "video_candidates"}
			plain, err := store.ModelSecrets.OpenLegacyConflict(tc.name, binding, state.CandidateEnvelope, state.CandidateFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			var candidates struct {
				Sources []struct {
					Container int
					Video     bool
					Raw       map[string]any
				}
			}
			if json.Unmarshal([]byte(plain), &candidates) != nil || len(candidates.Sources) != 1 || candidates.Sources[0].Container != tc.container || candidates.Sources[0].Video != tc.provider || len(candidates.Sources[0].Raw) == 0 {
				t.Fatal("original video location missing")
			}
			restored := strings.Replace(tc.raw, `"expected-video"`, `"expected-video","apiKey":"fixture-restored"`, 1)
			if err := os.WriteFile(filepath.Join(dir, "api-config.json"), []byte(restored), 0600); err != nil {
				t.Fatal(err)
			}
			settings, err = store.GetConfig(ctx, tc.name)
			rows := modelCatalog(settings)
			if err != nil || len(rows) != 1 || rows[0]["credential"] != "fixture-restored" {
				t.Fatal("restoring a matching key did not release original candidate", err)
			}
			state, found, err := store.LegacyConfigMigrationState(ctx, tc.name)
			if err != nil || !found || state.Phase != "complete" || state.Revision != 4 || state.CandidateEnvelope != "" {
				t.Fatal("verified completion did not retire candidate", err)
			}
		})
	}
}

func TestLegacyConfigMigrationStateIsAccountAndPurposeBound(t *testing.T) {
	ctx := context.Background()
	store := stateTestStore(t)
	const owner = "sealed-owner"
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?, '{"videoProviders":{"personal_api":{"model":"test-video","apiKey":"synthetic-state-key"}}}',NOW(6),NOW(6))`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConfig(ctx, owner); err != nil {
		t.Fatal(err)
	}
	state, found, err := store.LegacyConfigMigrationState(ctx, owner)
	if err != nil || !found || strings.Contains(state.CandidateEnvelope, "synthetic-state-key") {
		t.Fatal("candidate not sealed", err)
	}
	for _, tc := range []struct {
		owner, field string
		valid        bool
	}{{owner, "video_candidates", true}, {"other", "video_candidates", false}, {owner, "settings_patch", false}} {
		plain, err := store.ModelSecrets.OpenLegacyConflict(tc.owner, novelfetchworkshop.LegacyConflictSource{Document: "db:legacy_config_migrations", Field: tc.field}, state.CandidateEnvelope, state.CandidateFingerprint)
		if (err == nil) != tc.valid || strings.Contains(plain, "synthetic-state-key") != tc.valid {
			t.Fatal("candidate account/purpose binding failed")
		}
	}
}

func TestLegacyConfigMigrationStateUpgradesPendingEnvelope(t *testing.T) {
	ctx := context.Background()
	store := stateTestStore(t)
	const owner = "upgrade-video"
	row := map[string]any{"id": "video", "kind": "video", "modelId": "old-model", "provider": "personal_api", "endpoint": "https://fixture.example.test/video", "credential": "fixture-original"}
	pending := map[string]any{"Rows": []any{row}, "Sources": []any{map[string]any{"container": 0, "index": 1, "Raw": row}}}
	seedPendingEnvelope(t, store, owner, pending)
	offline := novelfetchworkshop.NewMySQLStore(store.DB)
	offline.ModelSecrets = store.ModelSecrets
	if _, err := offline.GetConfig(ctx, owner); err == nil {
		t.Fatal("reader without original directory acknowledged old pending video")
	}
	var retained bool
	if err := store.DB.QueryRowContext(ctx, `SELECT JSON_CONTAINS_PATH(settings_json,'one','$.modelCatalogLegacyVideoPending') FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&retained); err != nil || !retained {
		t.Fatal("database-only reader lost a staged two-source import", err)
	}
	if _, err := store.GetConfig(ctx, owner); err != nil {
		t.Fatal(err)
	}
	state, found, err := store.LegacyConfigMigrationState(ctx, owner)
	if err != nil || !found || state.Phase != "awaiting_source" {
		t.Fatal("old envelope was not imported", err)
	}
	path := filepath.Join(store.LegacyConfigDir, owner, "api-config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, _ := json.Marshal(map[string]any{"modelCatalog": []any{row}})
	if err := os.WriteFile(path, file, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.GetConfig(ctx, owner)
	rows := modelCatalog(settings)
	if err != nil || len(rows) != 1 || rows[0]["credential"] != "fixture-original" {
		t.Fatal("old envelope did not resume", err)
	}
}

func TestLegacyConfigMigrationStateNoCodecReaderDuringDiscovery(t *testing.T) {
	store := stateTestStore(t)
	const owner = "first-preference"
	ctx := context.Background()
	if _, err := store.DB.Exec(`INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?, '{"modelCatalog":[{"id":"v","kind":"video","provider":"personal_api","modelId":"fixture","endpoint":"https://fixture.invalid/video"}]}',NOW(6),NOW(6))`, owner); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.DiscoverLegacyConfigMigration(ctx, owner); err != nil || !found {
		t.Fatal("preference-first discovery", err)
	}
	cfg, err := novelfetchworkshop.NewMySQLStore(store.DB).GetConfig(ctx, owner)
	if err == nil || len(modelCatalog(cfg)) != 0 {
		t.Fatal("unconfigured reader exposed staged video")
	}
}

func seedPendingEnvelope(t *testing.T, store *novelfetchworkshop.MySQLStore, owner string, pending map[string]any) {
	t.Helper()
	ctx := context.Background()
	plain, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, fp, err := store.ModelSecrets.SealLegacyConflict(owner, novelfetchworkshop.LegacyConflictSource{Document: "db:novel_fetch_workshop_configs", Field: "modelCatalogLegacyVideoPending"}, string(plain))
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"modelCatalogSecretVersion": "bound-v3", "modelCatalog": []any{}, "modelCatalogLegacyVideoPending": map[string]any{"ciphertext": ciphertext, "fingerprint": fp}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, owner, raw); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyConfigMigrationStateOldProviderEnvelopeRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := stateTestStore(t)
	owner := "keyless-upgrade"
	provider := map[string]any{"model": "fixture-video", "createUrl": "https://fixture.invalid/create"}
	row := map[string]any{"id": "legacy-video:personal_api", "kind": "video", "provider": "personal_api", "modelId": "fixture-video", "displayName": "fixture-video", "endpoint": "https://fixture.invalid/create", "tasksEndpoint": "", "resultEndpoint": "", "workflowId": "", "enabled": true, "credentialStatus": "needs_reentry", "credentialRecoveryReason": "legacy_missing"}
	seedPendingEnvelope(t, store, owner, map[string]any{"Rows": []any{row}, "Sources": []any{map[string]any{"video": true, "provider": "personal_api", "container": 2, "Raw": provider}}})
	if _, err := store.GetConfig(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.LegacyConfigDir, owner), 0700); err != nil {
		t.Fatal(err)
	}
	provider["apiKey"] = "synthetic-restored"
	raw, err := json.Marshal(map[string]any{"videoProviders": map[string]any{"personal_api": provider}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.LegacyConfigDir, owner, "api-config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.GetConfig(ctx, owner)
	rows := modelCatalog(settings)
	if err != nil || len(rows) != 1 || rows[0]["credential"] != "synthetic-restored" || firstText(rows[0]["tasksEndpoint"]) == "" || firstText(rows[0]["resultEndpoint"]) == "" {
		t.Fatal("old generated row did not restore to one verified model", err)
	}
	state, found, err := store.LegacyConfigMigrationState(ctx, owner)
	if err != nil || !found || state.Phase != "complete" {
		t.Fatal("upgraded state did not complete", err)
	}
}

func TestLegacyConfigMigrationStateEvidenceIsConsumedOnce(t *testing.T) {
	ctx := context.Background()
	store := stateTestStore(t)
	for _, mode := range []string{"provider-only", "two-provider-records", "wrong-independent-row", "provider-plus-catalog", "fresh-provider-only", "fresh-provider-plus-catalog"} {
		t.Run(mode, func(t *testing.T) {
			const oldID = "legacy-video:personal_api"
			scenario := strings.TrimPrefix(mode, "fresh-")
			provider := map[string]any{"model": "fixture-video", "createUrl": "https://fixture.invalid/create", "tasksUrl": "https://fixture.invalid/tasks", "resultUrl": "https://fixture.invalid/result"}
			generated := map[string]any{"kind": "video", "id": oldID, "modelId": "fixture-video", "displayName": "fixture-video", "provider": "personal_api", "endpoint": provider["createUrl"], "tasksEndpoint": provider["tasksUrl"], "resultEndpoint": provider["resultUrl"], "workflowId": "", "enabled": true, "credentialStatus": "needs_reentry", "credentialRecoveryReason": "legacy_missing"}
			independent := map[string]any{"kind": "video", "id": oldID, "modelId": "fixture-video", "displayName": "user-disabled", "provider": "personal_api", "endpoint": provider["createUrl"], "tasksEndpoint": provider["tasksUrl"], "resultEndpoint": provider["resultUrl"], "workflowId": "", "enabled": false}
			if mode != scenario {
				initial, err := json.Marshal(map[string]any{"modelCatalog": []any{independent}, "videoProviders": map[string]any{"personal_api": provider}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = store.DB.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, mode, initial); err != nil {
					t.Fatal(err)
				}
			} else {
				seedPendingEnvelope(t, store, mode, map[string]any{"Rows": []any{independent, generated}, "Sources": []any{map[string]any{"video": true, "provider": "personal_api", "container": 0, "Raw": provider}}})
			}
			if _, err := store.GetConfig(ctx, mode); err != nil {
				t.Fatal(err)
			}
			provider["apiKey"] = "synthetic-provider"
			providers := map[string]any{"personal_api": provider}
			doc := map[string]any{"videoProviders": providers}
			if mode == "two-provider-records" {
				doc["settings"] = map[string]any{"videoProviders": providers}
			}
			if scenario == "provider-plus-catalog" || mode == "wrong-independent-row" {
				independent["credential"] = "synthetic-independent"
				if mode == "wrong-independent-row" {
					independent["displayName"] = "unrelated-entry"
					independent["enabled"] = true
				}
				doc["modelCatalog"] = []any{independent}
			}
			dir := filepath.Join(store.LegacyConfigDir, mode)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "api-config.json")
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var savedID string
			if scenario == "provider-plus-catalog" {
				stop := errors.New("synthetic file write interruption")
				store.ReplaceLegacyConfig = func(string, map[string]any) error { return stop }
				_, err := store.GetConfig(ctx, mode)
				store.ReplaceLegacyConfig = nil
				if !errors.Is(err, stop) {
					t.Fatal("expected retryable failure after model commit", err)
				}
				committed, err := store.ReadBoundModelConfig(ctx, mode)
				if err != nil {
					t.Fatal(err)
				}
				for _, model := range modelCatalog(committed) {
					if model["displayName"] == "user-disabled" {
						savedID = firstText(model["id"])
					}
				}
				if savedID == "" || savedID == oldID {
					t.Fatal("distinct ID must already exist before file retry")
				}
			}
			cfg, err := store.GetConfig(ctx, mode)
			if err != nil {
				t.Fatal(err)
			}
			state, found, err := store.LegacyConfigMigrationState(ctx, mode)
			if err != nil || !found {
				t.Fatal("missing migration", err)
			}
			rows := modelCatalog(cfg)
			if scenario != "provider-plus-catalog" {
				retained, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, retained) || len(rows) != 0 || state.Phase != "awaiting_source" || state.CandidateEnvelope == "" {
					t.Fatal("independent catalog row released without its own file evidence", err)
				}
				return
			}
			check := func(cfg map[string]any) string {
				rows := modelCatalog(cfg)
				if len(rows) != 2 {
					t.Fatalf("expected separate provider and disabled catalog models, got %d", len(rows))
				}
				got := map[string]map[string]any{}
				for _, row := range rows {
					got[firstText(row["displayName"])] = row
				}
				a, b := got["fixture-video"], got["user-disabled"]
				id := firstText(b["id"])
				if a["id"] != oldID || a["enabled"] != true || a["credential"] != "synthetic-provider" || id == "" || id == oldID || strings.Contains(id, "synthetic") || b["enabled"] != false || b["credential"] != "synthetic-independent" {
					t.Fatal("model identity, enabled state or credentials crossed")
				}
				return id
			}
			id := check(cfg)
			if id != savedID {
				t.Fatal("retry changed independent row ID")
			}
			if state.Phase != "complete" {
				t.Fatal("two proven records did not complete")
			}
			scrubbed, err := os.ReadFile(path)
			if err != nil || bytes.Contains(scrubbed, []byte("synthetic-")) {
				t.Fatal("file still contains a migrated key")
			}
			conflicts, _, err := store.ListLegacyModelConflicts(ctx, mode, "")
			if err != nil || len(conflicts) != 0 {
				t.Fatal("independently proven keys were treated as disputed")
			}
			cold := novelfetchworkshop.NewMySQLStore(store.DB)
			cold.LegacyConfigDir, cold.ModelSecrets = store.LegacyConfigDir, store.ModelSecrets
			again, err := cold.GetConfig(ctx, mode)
			if err != nil || check(again) != id {
				t.Fatal("cold read changed the independent model ID", err)
			}
		})
	}
}

func TestLegacyConfigMigrationStateVerifiedPositionsKeepSeparateCredentials(t *testing.T) {
	useTempConfigSecretKey(t)
	ctx := context.Background()
	store := stateTestStore(t)
	const providerID = "legacy-video:personal_api"
	const providerKey = "synthetic-provider"
	var catalogID string
	for _, tc := range []struct {
		owner, originalKey, returnedKey string
	}{
		{"only-catalog-key-changed", providerKey, "synthetic-changed"},
		{"all-keys-equal", providerKey, providerKey},
		{"distinct-original-catalog-key", "synthetic-independent", "synthetic-independent"},
	} {
		t.Run(tc.owner, func(t *testing.T) {
			// Both positions describe the same enabled model, down to its original ID and display name.
			provider := map[string]any{"model": "fixture-video", "createUrl": "https://fixture.invalid/create", "tasksUrl": "https://fixture.invalid/tasks", "resultUrl": "https://fixture.invalid/result", "apiKey": providerKey}
			catalog := map[string]any{"kind": "video", "id": providerID, "modelId": "fixture-video", "displayName": "fixture-video", "enabled": true, "provider": "personal_api", "endpoint": provider["createUrl"], "tasksEndpoint": provider["tasksUrl"], "resultEndpoint": provider["resultUrl"], "workflowId": "", "credential": tc.originalKey}
			doc := map[string]any{"modelCatalog": []any{catalog}, "videoProviders": map[string]any{"personal_api": provider}}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, tc.owner, raw); err != nil {
				t.Fatal(err)
			}
			cfg, err := store.GetConfig(ctx, tc.owner)
			if err != nil || len(modelCatalog(cfg)) != 0 {
				t.Fatal("videos released before their file returned", err)
			}
			catalog["credential"] = tc.returnedKey
			raw, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			writeRawConfigFile(t, store.LegacyConfigDir, tc.owner, string(raw))
			stop := errors.New("synthetic file write interruption")
			store.ReplaceLegacyConfig = func(string, map[string]any) error { return stop }
			_, err = store.GetConfig(ctx, tc.owner)
			store.ReplaceLegacyConfig = nil
			if !errors.Is(err, stop) {
				t.Fatal("expected file write failure after committing the catalog", err)
			}

			conflict := tc.originalKey != tc.returnedKey
			check := func(settings map[string]any) {
				t.Helper()
				rows := modelCatalog(settings)
				if len(rows) != 2 {
					t.Fatalf("two independently verified source positions produced %d models", len(rows))
				}
				var providerRow, catalogRow map[string]any
				for _, row := range rows {
					if row["id"] == providerID {
						providerRow = row
					} else {
						catalogRow = row
					}
				}
				if providerRow["credential"] != providerKey || providerRow["credentialStatus"] == "needs_reentry" {
					t.Fatal("provider lost its verified credential")
				}
				id := firstText(catalogRow["id"])
				if id == "" || catalogID != "" && catalogID != id {
					t.Fatal("catalog ID missing or changed across credentials or retries")
				}
				catalogID = id
				if conflict {
					if catalogRow["credentialStatus"] != "needs_reentry" || catalogRow["credentialRecoveryReason"] != "legacy_video_source_conflict" || firstText(catalogRow["credential"], catalogRow["apiKey"], catalogRow["credentialRef"]) != "" {
						t.Fatal("independent row did not retain its own credential conflict")
					}
				} else if catalogRow["credential"] != tc.originalKey || catalogRow["credentialStatus"] == "needs_reentry" {
					t.Fatal("independent row lost its verified credential")
				}
			}
			committed, err := store.ReadBoundModelConfig(ctx, tc.owner)
			if err != nil {
				t.Fatal(err)
			}
			check(committed)
			cold := novelfetchworkshop.NewMySQLStore(store.DB)
			cold.LegacyConfigDir, cold.ModelSecrets = store.LegacyConfigDir, store.ModelSecrets
			for range 2 {
				cfg, err := cold.GetConfig(ctx, tc.owner)
				if err != nil {
					t.Fatal(err)
				}
				check(cfg)
			}
			state, found, err := cold.LegacyConfigMigrationState(ctx, tc.owner)
			if err != nil || !found || state.Phase != "complete" {
				t.Fatal("file retry did not complete migration", err)
			}
			records, _, err := cold.ListLegacyModelConflicts(ctx, tc.owner, "")
			if err != nil || conflict && len(records) != 2 || !conflict && len(records) != 0 {
				t.Fatalf("unexpected credential archive size %d: %v", len(records), err)
			}
			for _, record := range records {
				if record.Source.Video || record.Source.Field != "credential" {
					t.Fatal("provider credential included in catalog conflict archive")
				}
			}
		})
	}
}

type concurrentPatchWriter struct {
	*configSecretProvider
	store    *novelfetchworkshop.MySQLStore
	injected bool
}

func (w *concurrentPatchWriter) ApplyLegacySettingEdits(owner, path string, edits []novelfetchworkshop.LegacySettingEdit) error {
	if err := w.configSecretProvider.ApplyLegacySettingEdits(owner, path, edits); err != nil {
		return err
	}
	if !w.injected {
		w.injected = true
		return w.store.AppendLegacySettingEdits(context.Background(), owner, fieldEdits(nil, map[string]any{"notifications": map[string]any{"petVisible": false}}))
	}
	return nil
}
func patchTestStore(t *testing.T) *novelfetchworkshop.MySQLStore {
	t.Helper()
	s := stateTestStore(t)
	if _, err := s.DB.Exec("INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES('a','finalizing'),('b','finalizing')"); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestLegacySettingPatchJournalCompactsExplicitEdits(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	for _, patch := range []map[string]any{{"x": 1, "y": true}, {"x": nil}} {
		if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, patch)); err != nil {
			t.Fatal(err)
		}
	}
	edits, rev, active, err := s.LoadLegacySettingEdits(ctx, "a")
	if err != nil || !active || len(edits) != 2 || rev != 3 {
		t.Fatal("journal not compacted", rev, err)
	}
	doc := map[string]any{"x": "prior"}
	settingsEdits(edits).apply(doc)
	if _, ok := doc["x"]; ok || doc["y"] != true {
		t.Fatal("explicit removal lost")
	}
}
func TestLegacySettingPatchJournalCASPreservesConcurrentEdit(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	_, old, _, err := s.LoadLegacySettingEdits(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, map[string]any{"x": "new"})); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CompleteLegacySettingEdits(ctx, "a", old); ok || err != nil {
		t.Fatal("stale completion", err)
	}
	edits, rev, active, err := s.LoadLegacySettingEdits(ctx, "a")
	if !active || len(edits) != 1 || err != nil {
		t.Fatal("lost concurrent edit", err)
	}
	if ok, err := s.CompleteLegacySettingEdits(ctx, "a", rev); !ok || err != nil {
		t.Fatal("completion failed", err)
	}
}
func TestLegacySettingPatchJournalRejectsCrossAccountAndPurpose(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, map[string]any{"x": "synthetic-only"})); err != nil {
		t.Fatal(err)
	}
	state, _, err := s.LegacyConfigMigrationState(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ModelSecrets.OpenLegacyConflict("a", novelfetchworkshop.LegacyConflictSource{Document: "db:legacy_config_migrations", Field: "video_candidates"}, state.PatchEnvelope, state.PatchFingerprint); err == nil {
		t.Fatal("accepted other purpose")
	}
	if _, err := s.DB.Exec("UPDATE novel_fetch_legacy_config_migrations SET patch_ciphertext=?,patch_fingerprint=? WHERE owner_username='b'", state.PatchEnvelope, state.PatchFingerprint); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.LoadLegacySettingEdits(ctx, "b"); err == nil {
		t.Fatal("accepted other account")
	}
}
func TestLegacyDeferredSnapshotConversionIsConservative(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.journal = s
	raw := `{"notifications":{"soundVolume":60,"soundEnabled":false},"apiIntegrations":{"values":{"kimiOfficialApiKey":null},"pinned":[],"customChannels":[{"id":"v","name":"renamed","enabled":true,"apiFormat":"openai_compatible","group":"","model":"","baseUrl":"","concurrency":0,"params":{},"capabilities":null}]}}`
	binding := novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}
	ciphertext, fingerprint, err := c.secrets.SealLegacyConflict("a", binding, raw)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := UserScopedPath(c.BaseDir, "a", deferredSettingsFile)
	if err := writeUserConfigDocument(path, deferredSettingsEnvelope{ciphertext, fingerprint}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetConfig(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	edits, _, _, err := s.LoadLegacySettingEdits(ctx, "a")
	if err != nil || len(edits) != 2 {
		t.Fatalf("snapshot should import only the rename and mute, got %d edits: %v", len(edits), err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("verified old file not retired")
	}
	for _, edit := range edits {
		if edit.Delete {
			t.Fatal("empty snapshot field became a tombstone")
		}
	}
	// Recreate the old file after import and a subsequent acknowledged save.
	if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits([]string{apiIntegrationsSettingKey, "customChannels", "v"}, map[string]any{"name": "newer"})); err != nil {
		t.Fatal(err)
	}
	if err := writeUserConfigDocument(path, deferredSettingsEnvelope{ciphertext, fingerprint}); err != nil {
		t.Fatal(err)
	}
	doc, err := c.GetConfig(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if apiIntegrationChannels(apiIntegrationStoredMap(doc))[0].(map[string]any)["name"] != "newer" {
		t.Fatal("retry overwrote newer edit")
	}
}

func TestLegacySettingPatchImportsExplicitArrayAndRetainsUnverifiedFile(t *testing.T) {
	ctx := context.Background()
	store := patchTestStore(t)
	config := NewUserConfigStore(store.LegacyConfigDir)
	config.journal = store
	file, _ := UserScopedPath(config.BaseDir, "a", deferredSettingsFile)
	binding := novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}
	save := func(owner string) {
		t.Helper()
		ciphertext, fingerprint, err := config.secrets.SealLegacyConflict(owner, binding, `[{"path":["notifications","soundVolume"],"value":0},{"path":["apiIntegrations","values","kimiOfficialApiKey"],"delete":true}]`)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeUserConfigDocument(file, deferredSettingsEnvelope{ciphertext, fingerprint}); err != nil {
			t.Fatal(err)
		}
	}
	save("b")
	if _, err := config.GetConfig(ctx, "a"); err == nil {
		t.Fatal("accepted another account's file")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("unverified file removed", err)
	}
	save("a")
	if _, err := config.GetConfig(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	edits, _, active, err := store.LoadLegacySettingEdits(ctx, "a")
	if err != nil || !active || len(edits) != 2 || string(edits[0].Value) != "0" || !edits[1].Delete {
		t.Fatal("explicit array did not preserve zero and explicit deletion", err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("verified old file still present")
	}
}

func TestLegacySettingPatchRejectsGeneratedValuesFromInvalidChannelFields(t *testing.T) {
	for key, value := range map[string]any{"model": 42, "baseUrl": false, "enabled": "no", "clearApiKey": "yes"} {
		if _, ok := sanitizeIntegrationChannel(map[string]any{"id": "v", "name": "rename", key: value}, nil); ok {
			t.Errorf("invalid %s became a generated assignment", key)
		}
	}
}

func TestLegacySettingPatchPublicViewOmitsInternalFields(t *testing.T) {
	doc := map[string]any{"revision": 7, "configRevision": "8", "modelCatalogLegacyDefaultsFile": true, "notifications": map[string]any{"migrationPhase": "finalizing", "patchCiphertext": "opaque", "candidateFingerprint": "fingerprint", "soundVolume": 42}}
	view := publicUserConfig(doc)
	raw, _ := json.Marshal(view)
	for _, field := range []string{"revision", "Revision", "Legacy", "migrationPhase", "patchCiphertext", "candidateFingerprint"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("public field leaked: %s", field)
		}
	}
}
