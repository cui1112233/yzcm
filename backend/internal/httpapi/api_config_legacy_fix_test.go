package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/storage"
)

func TestAPIConfigPolicyMySQLScrubsLegacyFilesAfterCommittedMigration(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	previous := []byte(`{
 "notifications":{"soundEnabled":false},
 "modelCatalog":[{"id":"row-a","kind":"text","modelId":"shared","baseUrl":"https://a.example.test/v1","credential":"fixture-a"}],
 "settings":{"layout":{"removeEmptyLines":true},"modelCatalog":[
  {"id":"row-b","kind":"text","modelId":"shared","baseUrl":"https://b.example.test/v1","apiKey":"fixture-b"},
  {"kind":"audio","modelId":"ambiguous","endpoint":"https://audio.example.test/v1","credential":"fixture-ambiguous-one"},
  {"kind":"audio","modelId":"ambiguous","endpoint":"https://audio.example.test/v1","credential":"fixture-ambiguous-two"}]}
}`)
	path := filepath.Join(dir, "fixture", "api-config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	exec := func(owner, settings string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at)
VALUES (?,?,NOW(6),NOW(6))`, owner, settings); err != nil {
			t.Fatal(err)
		}
	}
	exec("fixture", `{"modelCatalog":[
 {"id":"row-a","kind":"text","modelId":"shared","baseUrl":"https://a.example.test/v1"}],
 "settings":{"modelCatalog":[
 {"id":"row-b","kind":"text","modelId":"shared","baseUrl":"https://b.example.test/v1"},
 {"id":"row-c","kind":"audio","modelId":"ambiguous","endpoint":"https://audio.example.test/v1"}]}}`)
	config := NewUserConfigStore(dir)
	makeStore := func() *novelfetchworkshop.MySQLStore {
		store := novelfetchworkshop.NewMySQLStore(db)
		store.LegacyConfigDir = dir
		store.LegacyConfigLocker = config.LegacyModelFileLocker()
		store.ModelSecrets = config.ModelConfigSecrets()
		return store
	}
	store := makeStore()
	var replacements int
	store.ReplaceLegacyConfig = func(string, map[string]any) error {
		replacements++
		return os.ErrPermission
	}
	if _, err := store.GetConfig(ctx, "fixture"); !errors.Is(err, os.ErrPermission) || replacements != 1 {
		t.Fatalf("committed import must surface file replacement failure: %v, calls %d", err, replacements)
	}
	raw := func() []byte {
		t.Helper()
		var saved []byte
		if err := db.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username='fixture'`).Scan(&saved); err != nil {
			t.Fatal(err)
		}
		return saved
	}
	before := raw()
	if bytes.Contains(before, []byte("fixture-")) || !bytes.Contains(before, []byte("modelCatalogLegacyScrubPending")) || !bytes.Contains(before, []byte("bound-v3")) {
		t.Fatal("database must commit only cipher and pending cleanup before replacement")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(previous, got) {
		t.Fatal("failed file replacement changed the original")
	}
	restarted := makeStore()
	if err := restarted.ResumeLegacyModelCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	after := raw()
	if bytes.Contains(after, []byte("modelCatalogLegacyScrubPending")) || bytes.Contains(after, []byte("fixture-")) {
		t.Fatal("restart did not acknowledge clean file")
	}
	var beforeConfig, afterConfig map[string]any
	_ = json.Unmarshal(before, &beforeConfig)
	_ = json.Unmarshal(after, &afterConfig)
	for index, first := range modelCatalog(beforeConfig) {
		second := modelCatalog(afterConfig)[index]
		if first["credential"] != second["credential"] || first["apiKey"] != second["apiKey"] {
			t.Fatal("file retry rewrote an already committed cipher")
		}
	}
	result, err := restarted.GetConfig(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	models := modelCatalog(result)
	if len(models) != 3 || models[0]["credential"] != "fixture-a" || models[1]["credential"] != "fixture-b" || firstText(models[2]["credential"]) != "" || models[2]["credentialStatus"] != "needs_reentry" {
		t.Fatal("legacy key assignment did not respect row identities and ambiguity")
	}
	scrubbed, err := os.ReadFile(path)
	if err != nil || bytes.Contains(scrubbed, []byte("fixture-")) || !bytes.Contains(scrubbed, []byte("soundEnabled")) || !bytes.Contains(scrubbed, []byte("removeEmptyLines")) {
		t.Fatal("legacy file cleanup kept credentials or removed unrelated preferences")
	}
}

func TestAPIConfigPolicyMySQLUpgradesV2OnlyForProvableTarget(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	config := NewUserConfigStore(t.TempDir())
	cipher, err := config.secrets.secrets.WriteCipher()
	if err != nil {
		t.Fatal(err)
	}
	models := []map[string]any{}
	for _, row := range []struct{ id, endpoint string }{
		{"same", "https://audio.example.test/v1"}, {"different", "https://different.example.test/v1"},
	} {
		base := "https://audio.example.test/v1"
		binding, _ := json.Marshal([]string{"qiantie:v88:model:bound-v2", "fixture", "audio", row.id, "", row.id, base, "credential"})
		key, err := cipher.SealBound("fixture-previous-key", binding)
		if err != nil {
			t.Fatal(err)
		}
		models = append(models, map[string]any{"id": row.id, "modelId": row.id, "kind": "audio", "baseUrl": base, "endpoint": row.endpoint, "credential": key})
	}
	old, _ := json.Marshal(map[string]any{"modelCatalog": models, novelfetchworkshop.ModelSecretVersionKey: "bound-v2"})
	if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at)
VALUES ('fixture',?,NOW(6),NOW(6))`, string(old)); err != nil {
		t.Fatal(err)
	}
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = config.ModelConfigSecrets()
	migrated, err := store.GetConfig(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	result := modelCatalog(migrated)
	if migrated[novelfetchworkshop.ModelSecretVersionKey] != "bound-v3" || len(result) != 2 || result[0]["credential"] != "fixture-previous-key" ||
		firstText(result[1]["credential"]) != "" || result[1]["credentialStatus"] != "needs_reentry" {
		t.Fatal("v2 migration must preserve only the known effective destination")
	}
}
