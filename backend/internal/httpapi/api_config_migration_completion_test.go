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
		var result bool
		if err := db.QueryRowContext(ctx, `SELECT JSON_CONTAINS_PATH(settings_json,'one','$.modelCatalogLegacyVideoPending') FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&result); err != nil {
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
			if _, err := store.GetConfig(ctx, name); err != nil {
				t.Fatal(err)
			}
			reply := impersonationRequest(router, "POST", "/api/settings/api-integrations", name, `{"pinned":["openrouterApiKey"],"values":{"kimiOfficialApiKey":null}}`, nil)
			if reply.Code != http.StatusOK {
				t.Fatalf("integration patch status %d", reply.Code)
			}
			reply = impersonationRequest(router, "POST", "/api/config", name, `{"notifications":{"soundVolume":42}}`, nil)
			if reply.Code != http.StatusOK {
				t.Fatalf("preference patch status %d", reply.Code)
			}
			journal := filepath.Join(filepath.Dir(path), "deferred-settings.json")
			copyOfJournal, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			write(t, path, string(copyOfJournal))
			reply = impersonationRequest(router, "GET", "/api/config", name, "", nil)
			if reply.Code != http.StatusOK || strings.Contains(reply.Body.String(), "fingerprint") || strings.Contains(reply.Body.String(), "ciphertext") {
				t.Error("internal envelope metadata reached an unrestricted configuration view")
			}
			write(t, path, `{"notifications":{"soundVolume":60},"videoProviders":{"personal_api":{"model":"expected-video","apiKey":"fixture-B","createUrl":"https://expected.example.test/video"}},"apiIntegrations":{"values":{"deepseekOfficialApiKey":"fixture-preserved","kimiOfficialApiKey":"fixture-clear"},"customChannels":[{"id":"old","name":"old","apiKey":"fixture-channel"}],"channelSelection":{"text":{"enabled":["old"]}}},"unrelated":"keep"}`)
			if interrupted {
				if err := os.Mkdir(path+".tmp", 0700); err != nil {
					t.Fatal(err)
				}
				if _, err := store.GetConfig(ctx, name); err == nil {
					t.Error("standard file write interruption was acknowledged as completion")
				}
				if _, err := os.Stat(journal); err != nil {
					t.Fatal("failed merge lost the journal")
				}
				if err := os.Remove(path + ".tmp"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.GetConfig(ctx, name); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(journal); !errors.Is(err, os.ErrNotExist) {
				t.Error("verified settings journal not retired after migration completion")
			}
			if interrupted {
				// Recreate the durable state after standard config was written
				// but before journal removal. A cold reader must finish it.
				write(t, journal, string(copyOfJournal))
				cold := novelfetchworkshop.NewMySQLStore(db)
				cold.LegacyConfigDir, cold.ModelSecrets, cold.LegacyConfigLocker = dir, config.ModelConfigSecrets(), config.LegacyModelFileLocker()
				if _, err := cold.GetConfig(ctx, name); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(journal); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("replayed journal was not retired")
				}
			}
			saved := config.readLegacyConfig(name)
			if saved["notifications"].(map[string]any)["soundVolume"] != float64(42) {
				t.Error("standard file did not retain the acknowledged volume after replay")
			}
			integration := apiIntegrationStoredMap(saved)
			values := apiIntegrationValues(integration)
			if values["deepseekOfficialApiKey"] != "fixture-preserved" || values["kimiOfficialApiKey"] != nil || len(apiIntegrationChannels(integration)) != 1 || len(apiIntegrationChannelSelection(integration)) != 1 || len(apiIntegrationPinned(integration)) != 1 || saved["unrelated"] != "keep" {
				t.Error("standard file did not preserve untouched integration fields and apply explicit edits")
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
