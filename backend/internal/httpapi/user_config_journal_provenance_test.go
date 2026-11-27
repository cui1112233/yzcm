package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
)

var deferredFilePurpose = novelfetchworkshop.LegacyConflictSource{Document: deferredSettingsFile, Field: "settings"}
var dbPatchPurpose = novelfetchworkshop.LegacyConflictSource{Document: "db:legacy_config_migrations", Field: "setting_patch"}

func TestLegacySettingPatchFilePurposeAlwaysMeansSnapshot(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"ordinary-fields", `{"schemaVersion":3,"batches":["first"],"snapshots":{"folder":"archive"},"notifications":{"soundVolume":42}}`},
		{"same-shape", `{"schemaVersion":1,"batches":[{"batchId":"old-data","edits":[{"path":["keep"],"delete":true},{"path":["count"],"value":9}]}]}`},
		{"malformed-version", `{"schemaVersion":"release label","batches":{"color":"blue"},"snapshots":"photo roll"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := patchTestStore(t)
			c := NewUserConfigStore(s.LegacyConfigDir)
			c.UseLegacySettingJournal(s)
			path, _ := c.configPath("a")
			if err := writeUserConfigDocument(path, map[string]any{"keep": "original", "count": 7, "notifications": map[string]any{"soundVolume": 61}}); err != nil {
				t.Fatal(err)
			}
			source := writeSealedSettingFixture(t, c, "a", tc.raw)
			if _, err := s.GetConfig(ctx, "a"); err != nil {
				t.Fatal("old preference names blocked import", err)
			}
			got, err := c.readStandardFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got["keep"] != "original" || got["count"] != 7.0 || got["notifications"].(map[string]any)["soundVolume"] != 61.0 {
				t.Error("old file received permission to delete or overwrite restored fields")
			}
			var old map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &old); err != nil {
				t.Fatal(err)
			}
			for _, k := range []string{"schemaVersion", "batches", "snapshots"} {
				if !reflect.DeepEqual(got[k], old[k]) {
					t.Errorf("lost ordinary old preference %s", k)
				}
			}
			assertSettingSource(t, s, source, deferredFilePurpose, tc.raw)
		})
	}
}

func TestUserConfigPendingGETFiltersEveryLegacyContainer(t *testing.T) {
	s := stateTestStore(t)
	ctx := context.Background()
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.UseLegacySettingJournal(s)
	const owner = "founder"
	dbSource := `{"modelCatalog":[{"kind":"video","id":"old","modelId":"legacy","credential":"synthetic","endpoint":"https://example.invalid"}]}`
	if _, err := s.DB.Exec("INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))", owner, dbSource); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.DiscoverLegacyConfigMigration(ctx, owner); err != nil || !found {
		t.Fatal("pending state", err)
	}
	fixture := func(name string) map[string]any {
		return map[string]any{
			"modelCatalog":        []any{map[string]any{"id": "private-" + name, "kind": "video"}},
			"modelCatalogVersion": 17, "modelCatalogSecretVersion": "old-mark", "modelCatalogLegacyVideoPending": "old-pending",
			"videoProviders": map[string]any{"p": "private-provider-" + name}, "videoProviderLegacyMigrated": true,
			"migrationPhase": "old-phase", "configRevision": "private-revision", "localPreference": "keep-" + name,
		}
	}
	disk := fixture("root")
	settings := fixture("settings")
	settings["appConfig"] = fixture("settings.appConfig")
	disk["settings"] = settings
	disk["appConfig"] = fixture("appConfig")
	disk["apiIntegrations"] = map[string]any{"customChannels": []any{
		map[string]any{"id": "test", "name": "test", "params": map[string]any{"modelCatalog": "provider-option"}},
	}}
	path, _ := c.configPath(owner)
	if err := writeUserConfigDocument(path, disk); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h, _, _ := newImpersonationTestRouter(t, func(o *RouterOptions) {
		o.AuthStore = &authTestStore{account: auth.Account{Username: owner, Active: true, IsOwner: true}}
		o.NovelFetchStore = s
		o.UserConfigStore = c
	})
	r := impersonationRequest(h, "GET", "/api/config", owner, "", nil)
	if r.Code != 200 {
		t.Fatalf("pending preference GET: %d", r.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	channels := apiIntegrationChannels(apiIntegrationStoredMap(got))
	if len(channels) != 1 || mapOrEmpty(channels[0].(map[string]any)["params"])["modelCatalog"] != "provider-option" {
		t.Error("pending GET removed an ordinary custom provider parameter outside the model containers")
	}
	for _, name := range []string{"root", "settings", "appConfig", "settings.appConfig"} {
		container := got
		if name != "root" {
			for _, k := range strings.Split(name, ".") {
				container, _ = container[k].(map[string]any)
			}
		}
		if container["localPreference"] != "keep-"+name {
			t.Errorf("lost adjacent preference in %s", name)
		}
		for k := range fixture(name) {
			if _, ok := container[k]; ok && k != "localPreference" {
				t.Errorf("pending GET exposed %s in %s", k, name)
			}
		}
	}
	formal := map[string]any{novelfetchworkshop.ModelSecretVersionKey: novelfetchworkshop.ModelSecretVersion, "modelCatalog": []any{
		map[string]any{"kind": "video", "id": "formal", "modelId": "formal-video", "credential": "synthetic-bound-key"},
	}}
	if err := c.secrets.SealModelConfig("another-owner", formal, nil); err != nil {
		t.Fatal(err)
	}
	badCatalog, err := json.Marshal(formal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE novel_fetch_workshop_configs SET settings_json=? WHERE owner_username=?", badCatalog, owner); err != nil {
		t.Fatal(err)
	}
	r = impersonationRequest(h, "GET", "/api/config", owner, "", nil)
	if r.Code != 503 {
		t.Errorf("formal catalog binding failure returned %d", r.Code)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preference GET modified the source file", err)
	}
}

func writeSealedSettingFixture(t *testing.T, c *UserConfigStore, owner, plain string) deferredSettingsEnvelope {
	t.Helper()
	cipher, fp, err := c.secrets.SealLegacyConflict(owner, deferredFilePurpose, plain)
	if err != nil {
		t.Fatal(err)
	}
	path, err := UserScopedPath(c.BaseDir, owner, deferredSettingsFile)
	if err != nil {
		t.Fatal(err)
	}
	file := deferredSettingsEnvelope{cipher, fp}
	if err := writeUserConfigDocument(path, file); err != nil {
		t.Fatal(err)
	}
	return file
}

func assertSettingSource(t *testing.T, s *novelfetchworkshop.MySQLStore, source deferredSettingsEnvelope, purpose novelfetchworkshop.LegacyConflictSource, want string) {
	t.Helper()
	var retained string
	if err := s.DB.QueryRow("SELECT source_ciphertext FROM novel_fetch_legacy_setting_files WHERE owner_username='a' AND fingerprint=?", source.Fingerprint).Scan(&retained); err != nil {
		t.Fatal("missing original ciphertext", err)
	}
	if retained != source.Ciphertext {
		t.Fatal("receipt changed the original ciphertext")
	}
	plain, err := s.ModelSecrets.OpenLegacyConflict("a", purpose, retained, source.Fingerprint)
	if err != nil || plain != want {
		t.Fatal("receipt no longer restores the exact source", err)
	}
}

func TestLegacySettingPatchProvenanceRequestOrders(t *testing.T) {
	const naturalOrders = 1000
	const permutations = 120 // all permutations of five request fields
	const samples = naturalOrders + permutations
	ctx := context.Background()
	s := patchTestStore(t)
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.UseLegacySettingJournal(s)
	channels := make([]any, 0, samples)
	var requests, historical settingsEdits
	orders := map[string]bool{}
	for i := 0; i < samples; i++ {
		id := fmt.Sprintf("v-%d", i)
		row := map[string]any{"id": id, "name": "restored", "apiKey": "synthetic-restored"}
		channels = append(channels, row)
		request, err := integrationRequestEdits(nil, map[string]any{"customChannels": []any{map[string]any{"id": id, "name": "new", "group": "text", "model": "model-new", "fullUrl": true, "clearApiKey": true}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(request) != 5 {
			t.Fatalf("request writer returned %d of five submitted fields", len(request))
		}
		if i >= naturalOrders {
			slices.SortFunc(request, func(a, b settingsEdit) int { return strings.Compare(a.Path[3], b.Path[3]) })
			// Decode the permutation index by selecting one of the remaining fields at each position.
			n := i - naturalOrders
			for j := range request {
				remaining := len(request) - j
				k := j + n%remaining
				request[j], request[k] = request[k], request[j]
				n /= remaining
			}
		}
		var order []string
		for _, e := range request {
			order = append(order, e.Path[3])
		}
		orders[fmt.Sprint(order)] = true
		requests = append(requests, request...)
		historical = append(historical, fieldEdits([]string{apiIntegrationsSettingKey, "customChannels", id}, map[string]any{"id": id, "name": "snapshot", "model": "", "group": "", "apiFormat": "openai_compatible", "baseUrl": "", "enabled": true, "concurrency": nil})...)
		historical = append(historical, request...)
	}
	if len(orders) != permutations {
		t.Fatalf("exercised %d of %d possible field orders", len(orders), permutations)
	}
	initial := map[string]any{apiIntegrationsSettingKey: map[string]any{"customChannels": channels}}
	for _, owner := range []string{"a", "b"} {
		path, _ := c.configPath(owner)
		if err := writeUserConfigDocument(path, initial); err != nil {
			t.Fatal(err)
		}
	}
	old, err := json.Marshal(historical)
	if err != nil {
		t.Fatal(err)
	}
	source := writeSealedSettingFixture(t, c, "a", string(old))
	if err := s.AppendLegacySettingEdits(ctx, "b", requests); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLegacySettingEdits(ctx, "b", fieldEdits([]string{apiIntegrationsSettingKey, "customChannels", "v-0"}, map[string]any{"name": "later"})); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"a", "b"} {
		if _, err := s.GetConfig(ctx, owner); err != nil {
			t.Fatal(err)
		}
		path, _ := c.configPath(owner)
		doc, err := c.readStandardFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rows := apiIntegrationChannels(apiIntegrationStoredMap(doc))
		if len(rows) != samples {
			t.Fatalf("%s: got %d rows", owner, len(rows))
		}
		bad := 0
		for i, v := range rows {
			row := v.(map[string]any)
			name := "new"
			if i == 0 {
				name = "later"
			}
			if owner == "a" {
				if row["apiKey"] != "synthetic-restored" || row["name"] != "restored" {
					bad++
				}
			} else if _, hasKey := row["apiKey"]; hasKey || row["name"] != name {
				bad++
			}
		}
		if bad != 0 {
			t.Errorf("%s: %d/%d requests replayed with the wrong clear or name", owner, bad, samples)
		}
	}
	assertSettingSource(t, s, source, deferredFilePurpose, string(old))
	t.Logf("verified %d requests per format, %d distinct field orders", samples, len(orders))
}

func TestLegacySettingPatchProvenanceNoChannels(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.UseLegacySettingJournal(s)
	path, _ := c.configPath("a")
	var restored, archived map[string]any
	json.Unmarshal([]byte(`{"notifications":{"soundVolume":51},"apiIntegrations":{"pinned":["keep"],"values":{"existing":"restored"},"channelSelection":{"text":{"enabled":["keep"]}}}}`), &restored)
	json.Unmarshal([]byte(`{"notifications":{"soundVolume":42},"apiIntegrations":{"pinned":["old"],"values":{"existing":"old","missing":"fill-me"},"channelSelection":{"text":{"enabled":[]}}}}`), &archived)
	if err := writeUserConfigDocument(path, restored); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(fieldEdits(nil, archived))
	if err != nil {
		t.Fatal(err)
	}
	source := writeSealedSettingFixture(t, c, "a", string(raw))
	if _, err := c.secrets.ImportDeferredSettings(ctx, "a", path, s); err != nil {
		t.Fatal(err)
	}
	for _, val := range []int{1, 2} {
		if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, map[string]any{"counter": val})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GetConfig(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	doc, err := c.readStandardFile(path)
	if err != nil {
		t.Fatal(err)
	}
	section := apiIntegrationStoredMap(doc)
	if doc["notifications"].(map[string]any)["soundVolume"] != 51.0 || doc["counter"] != 2.0 || !reflect.DeepEqual(section["pinned"], []any{"keep", "old"}) || !reflect.DeepEqual(section["values"], map[string]any{"existing": "restored", "missing": "fill-me"}) || !reflect.DeepEqual(section["channelSelection"], map[string]any{"text": map[string]any{"enabled": []any{"keep"}}}) {
		t.Error("array without channels overwrote restored settings or lost a missing field")
	}
	assertSettingSource(t, s, source, deferredFilePurpose, string(raw))
}

func TestLegacySettingPatchProvenancePreviewFillsBeforeDefaults(t *testing.T) {
	s := patchTestStore(t)
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.UseLegacySettingJournal(s)
	source := `[{"path":["notifications","soundVolume"],"value":42}]`
	writeSealedSettingFixture(t, c, "a", source)
	got, err := c.GetConfig(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if got["notifications"].(map[string]any)["soundVolume"] != 42.0 {
		t.Fatal("a UI default masked an imported field that was absent on disk")
	}
}

func TestLegacySettingPatchProvenanceProtectedBatches(t *testing.T) {
	s := patchTestStore(t)
	ctx := context.Background()
	for _, row := range []map[string]any{{"x": "old", "preserve": true}, {"x": nil, "count": 2}} {
		if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, row)); err != nil {
			t.Fatal(err)
		}
	}
	state, _, err := s.LegacyConfigMigrationState(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.ModelSecrets.OpenLegacyConflict("a", dbPatchPurpose, state.PatchEnvelope, state.PatchFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	var journal struct {
		Version int `json:"schemaVersion"`
		Batches []struct {
			ID    string        `json:"batchId"`
			Edits settingsEdits `json:"edits"`
		} `json:"batches"`
	}
	if err := json.Unmarshal([]byte(plain), &journal); err != nil || journal.Version != 1 || len(journal.Batches) != 2 {
		t.Fatal("encrypted payload has no schema and append boundaries", err)
	}
	first, second := journal.Batches[0], journal.Batches[1]
	if first.ID == "" || second.ID == "" || first.ID == second.ID || len(first.Edits) != 1 || first.Edits[0].Path[0] != "preserve" || len(second.Edits) != 2 {
		t.Fatal("compaction lost request provenance")
	}
	c := NewUserConfigStore(s.LegacyConfigDir)
	c.UseLegacySettingJournal(s)
	path, _ := c.configPath("a")
	if err := writeUserConfigDocument(path, map[string]any{"x": "restore", "count": 99}); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportLegacySettingFile(ctx, "a", state.PatchEnvelope, state.PatchFingerprint, false,
		func(old map[string]any) []novelfetchworkshop.LegacySettingEdit { return snapshotEdits(old) }); err == nil {
		t.Fatal("a database batch was accepted with the old file purpose")
	}
	if _, err := s.GetConfig(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	doc, err := c.readStandardFile(path)
	if err != nil || doc["x"] != nil || doc["count"] != 2.0 || doc["preserve"] != true {
		t.Fatal("MySQL journal lost its request semantics", err)
	}

	journal.Batches[1].ID = first.ID
	invalid, _ := json.Marshal(journal)
	changed, _, err := s.ModelSecrets.SealLegacyConflict("a", dbPatchPurpose, string(invalid))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ModelSecrets.OpenLegacyConflict("a", dbPatchPurpose, changed, state.PatchFingerprint); err == nil {
		t.Error("modified batch authenticated with original fingerprint")
	}
	for _, raw := range []string{`{"schemaVersion":1}`, `{"schemaVersion":2,"batches":[]}`, string(invalid), plain + "{}"} {
		changed, fp, err := s.ModelSecrets.SealLegacyConflict("b", dbPatchPurpose, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.Exec("UPDATE novel_fetch_legacy_config_migrations SET patch_ciphertext=?,patch_fingerprint=? WHERE owner_username='b'", changed, fp); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err = s.LoadLegacySettingEdits(ctx, "b"); err == nil {
			t.Error("accepted malformed authenticated MySQL journal")
		}
	}
}

func TestLegacySettingPatchProvenanceOldDatabaseEvidence(t *testing.T) {
	for _, action := range []string{"compact", "complete", "finalize"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			s := patchTestStore(t)
			c := NewUserConfigStore(s.LegacyConfigDir)
			path, _ := c.configPath("a")
			if err := writeUserConfigDocument(path, map[string]any{"x": "restored"}); err != nil {
				t.Fatal(err)
			}
			raw := `[{"path":["x"],"delete":true},{"path":["missing"],"value":"filled"}]`
			cipher, fp, err := s.ModelSecrets.SealLegacyConflict("a", dbPatchPurpose, raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec("UPDATE novel_fetch_legacy_config_migrations SET patch_ciphertext=?,patch_fingerprint=? WHERE owner_username='a'", cipher, fp); err != nil {
				t.Fatal(err)
			}
			edits, rev, _, err := s.LoadLegacySettingEdits(ctx, "a")
			if err != nil {
				t.Fatal(err)
			}
			testDoc := map[string]any{"x": "restored"}
			if err = settingsEdits(edits).apply(testDoc); err != nil || testDoc["x"] != "restored" || testDoc["missing"] != "filled" {
				t.Error("old database array cleared restored data", err)
			}
			switch action {
			case "compact":
				if err := s.AppendLegacySettingEdits(ctx, "a", fieldEdits(nil, map[string]any{"missing": "new"})); err != nil {
					t.Fatal(err)
				}
				fallthrough
			case "finalize":
				if _, err := s.GetConfig(ctx, "a"); err != nil {
					t.Fatal(err)
				}
			case "complete":
				if ok, err := s.CompleteLegacySettingEdits(ctx, "a", rev); !ok || err != nil {
					t.Fatal(err)
				}
			}
			assertSettingSource(t, s, deferredSettingsEnvelope{cipher, fp}, dbPatchPurpose, raw)
		})
	}
}
