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
	disconnected := path + ".offline"
	if err := os.Rename(path, disconnected); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ResumeLegacyModelCleanup(ctx); err == nil || !bytes.Equal(before, raw()) {
		t.Fatal("a temporarily missing legacy file must retain its pending cleanup")
	}
	if err := os.Rename(disconnected, path); err != nil {
		t.Fatal(err)
	}
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

func TestAPIConfigPolicyMySQLMigratesFileOnlyVideoBesideBoundText(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = NewUserConfigStore(dir).ModelConfigSecrets()
	if err := store.PutConfig(ctx, "fixture", map[string]any{"modelCatalog": []any{
		map[string]any{"id": "bound-text", "kind": "text", "modelId": "fixture-text", "baseUrl": "https://text.example.test/v1", "credential": "fixture-existing-text"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_REMOVE(settings_json, '$.videoProviderLegacyMigrated') WHERE owner_username='fixture'`); err != nil {
		t.Fatal(err)
	}
	store.LegacyConfigDir = dir
	if _, err := store.GetConfig(ctx, "fixture"); err != nil {
		t.Fatalf("reading the bound text model while its old file is absent: %v", err)
	}
	path := filepath.Join(dir, "fixture", "api-config.json")
	original := []byte(`{"settings":{"layout":{"removeEmptyLines":true},"videoProviders":{"personal_api":{"apiKey":"fixture-file-only-video","model":"fixture-video","createUrl":"https://video.example.test/create","tasksUrl":"https://video.example.test/tasks","resultUrl":"https://video.example.test/result/{id}"}}}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	readRaw := func() (string, string) {
		t.Helper()
		var textCipher, videoCipher string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.modelCatalog[0].credential')),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.modelCatalog[1].credential')),'') FROM novel_fetch_workshop_configs WHERE owner_username='fixture'`).Scan(&textCipher, &videoCipher); err != nil {
			t.Fatal(err)
		}
		return textCipher, videoCipher
	}
	textBefore, _ := readRaw()
	store.ReplaceLegacyConfig = func(string, map[string]any) error { return errors.New("fixture atomic replace failure") }
	if _, err := store.GetConfig(ctx, "fixture"); err == nil {
		t.Fatal("file replacement fault was not reported")
	}
	file, err := os.ReadFile(path)
	textAfter, videoCipher := readRaw()
	if err != nil || !bytes.Equal(file, original) || textAfter != textBefore || !bytes.HasPrefix([]byte(videoCipher), []byte("enc:v2:")) {
		t.Fatal("file-only video must have its own committed ciphertext before any scrub")
	}
	store.ReplaceLegacyConfig = nil
	if err := store.ResumeLegacyModelCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	config, err := store.GetConfig(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	rows := modelCatalog(config)
	file, err = os.ReadFile(path)
	textAgain, videoAgain := readRaw()
	if err != nil || len(rows) != 2 || rows[1]["id"] != "legacy-video:personal_api" || rows[1]["credential"] != "fixture-file-only-video" ||
		bytes.Contains(file, []byte("fixture-file-only-video")) || !bytes.Contains(file, []byte("removeEmptyLines")) ||
		textAgain != textBefore || videoAgain != videoCipher {
		t.Fatal("retry lost the expected model, ciphertext or preferences")
	}
}

func TestAPIConfigPolicyMySQLScrubsOnlyProvenLegacyEntries(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := NewUserConfigStore(dir)
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = config.ModelConfigSecrets()
	if err := store.PutConfig(ctx, "fixture", map[string]any{"modelCatalog": []any{
		map[string]any{"id": "conflicted", "kind": "text", "modelId": "current", "baseUrl": "https://active.example.test/v1", "credential": "fixture-current-key"},
	}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture", "api-config.json")
	original := []byte(`{"modelCatalog":[{"id":"conflicted","kind":"text","modelId":"old","baseUrl":"https://old.example.test/v1","credential":"fixture-conflicting-key"}],
 "settings":{"videoProviders":{"personal_api":{"apiKey":"fixture-importable-key","model":"fixture-video","createUrl":"https://video.example.test/create"}}},"layout":{"compact":true}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	store.LegacyConfigDir = dir
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_legacy_fixture BEFORE UPDATE ON novel_fetch_workshop_configs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture write fault'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConfig(ctx, "fixture"); err == nil {
		t.Fatal("expected a failed model transaction")
	}
	file, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(file, original) {
		t.Fatal("failed model transaction touched the only legacy keys")
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER fail_legacy_fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConfig(ctx, "fixture"); err == nil {
		t.Fatal("unverified scope conflict needs recovery")
	}
	partial, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(partial, []byte("fixture-conflicting-key")) || bytes.Contains(partial, []byte("fixture-importable-key")) ||
		!bytes.Contains(partial, []byte("modelCatalogLegacyScrubEntryDone")) || !bytes.Contains(partial, []byte("compact")) {
		t.Fatal("scrub should acknowledge only the proven video entry and leave the unresolved text and preferences")
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, "SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username='fixture'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("modelCatalogLegacyScrubEntries")) || !bytes.Contains(raw, []byte("legacy-video:personal_api")) ||
		bytes.Contains(raw, []byte("fixture-conflicting-key")) || bytes.Contains(raw, []byte("fixture-importable-key")) {
		t.Fatal("durable pending evidence or bound video is missing")
	}
	fresh := novelfetchworkshop.NewMySQLStore(db)
	fresh.ModelSecrets, fresh.LegacyConfigDir = config.ModelConfigSecrets(), dir
	if err := fresh.ResumeLegacyModelCleanup(ctx); err == nil {
		t.Fatal("cold recovery must keep the unresolved conflict visible")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, partial) {
		t.Fatal("replaying a partially scrubbed file should leave its evidence unchanged")
	}
}

func TestAPIConfigPolicyMySQLMigratesLegacyVideoSettings(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	old := `{"modelCatalogSecretVersion":"bound-v3","modelCatalog":[],"workflow":{"keep":true},
	"videoProviders":{"personal_api":{"apiKey":"old-fixture-video-key","model":"test-video","createUrl":"https://video.example.test/create",
	"tasksUrl":"https://video.example.test/jobs","resultUrl":"https://video.example.test/results/{id}"}}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username, settings_json, created_at, updated_at) VALUES ('fixture',?,NOW(6),NOW(6))`, old); err != nil {
		t.Fatal(err)
	}
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = NewUserConfigStore(t.TempDir()).ModelConfigSecrets()
	if err := store.ResumeLegacyModelCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username='fixture'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("old-fixture-video-key")) || bytes.Contains(raw, []byte("\"videoProviders\"")) || !bytes.Contains(raw, []byte("enc:v2:")) {
		t.Fatal("historical video configuration was not replaced with a bound row")
	}
	config, err := store.GetConfig(ctx, "fixture")
	rows := modelCatalog(config)
	if err != nil || len(rows) != 1 || rows[0]["id"] != "legacy-video:personal_api" || rows[0]["credential"] != "old-fixture-video-key" ||
		rows[0]["tasksEndpoint"] != "https://video.example.test/jobs" || config["workflow"].(map[string]any)["keep"] != true {
		t.Fatal("video migration lost the established model, scope or preferences")
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
