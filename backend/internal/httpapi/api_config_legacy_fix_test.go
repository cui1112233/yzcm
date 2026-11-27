package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
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
	original := []byte(`{"modelCatalog":[{"id":"conflicted","kind":"text","modelId":"old","baseUrl":"https://old.example.test/v1","credential":["fixture-conflicting-key"]}],
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

func TestAPIConfigPolicyMySQLRecoversEachConflictingVideoKey(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	migrations := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	migrations = append(migrations, storage.NovelFetchWorkshopMigrations()...)
	if err := storage.RunMigrations(ctx, db, migrations); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := NewUserConfigStore(dir)
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets, store.LegacyConfigDir = config.ModelConfigSecrets(), dir
	if err := store.PutConfig(ctx, "manager", map[string]any{"modelCatalog": []any{
		map[string]any{"id": "unrelated", "kind": "text", "modelId": "text", "baseUrl": "https://text.example.test/v1", "credential": "fixture-unrelated"},
	}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manager", "api-config.json")
	original := []byte(`{"videoProviders":{"personal_api":{"apiKey":"fixture-top-video-key","model":"fixture-video","createUrl":"https://video.example.test/create"}},
 "settings":{"videoProviders":{"personal_api":{"apiKey":"fixture-nested-video-key","model":"fixture-video","createUrl":"https://video.example.test/create"}}},"layout":{"compact":true}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET(settings_json,'$.modelCatalogLegacyMigrationVersion','entry-v1') WHERE owner_username='manager'`); err != nil {
		t.Fatal(err)
	}
	lookupCiphertexts := func() map[string]string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT id,ciphertext FROM novel_fetch_legacy_model_conflicts WHERE owner_username='manager'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		found := map[string]string{}
		for rows.Next() {
			var id, cipher string
			if err := rows.Scan(&id, &cipher); err != nil {
				t.Fatal(err)
			}
			found[id] = cipher
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return found
	}
	var beforeText string
	if err := db.QueryRowContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.modelCatalog[0].credential')) FROM novel_fetch_workshop_configs WHERE owner_username='manager'`).Scan(&beforeText); err != nil {
		t.Fatal(err)
	}
	// A failure to update the plan rolls back the conflict rows too.
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER stop_conflict_plan BEFORE UPDATE ON novel_fetch_workshop_configs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture plan failure'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConfig(ctx, "manager"); err == nil || len(lookupCiphertexts()) != 0 {
		t.Fatal("the archive must commit in the same transaction as its cleanup plan")
	}
	if file, err := os.ReadFile(path); err != nil || !bytes.Equal(file, original) {
		t.Fatal("database interruption touched the unique old keys")
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER stop_conflict_plan"); err != nil {
		t.Fatal(err)
	}
	store.ReplaceLegacyConfig = func(string, map[string]any) error { return errors.New("fixture file failure") }
	if _, err := store.GetConfig(ctx, "manager"); err == nil {
		t.Fatal("expected file replacement fault")
	}
	committed := lookupCiphertexts()
	if len(committed) != 2 {
		t.Fatal("both conflicts must be written before the file replacement")
	}
	for _, cipher := range committed {
		if !strings.HasPrefix(cipher, "enc:v2:") || strings.Contains(cipher, "fixture-") {
			t.Fatal("archived key is not bound ciphertext")
		}
	}
	if file, err := os.ReadFile(path); err != nil || !bytes.Equal(file, original) {
		t.Fatal("failed atomic replacement changed the legacy file")
	}
	fresh := novelfetchworkshop.NewMySQLStore(db)
	fresh.ModelSecrets, fresh.LegacyConfigDir = config.ModelConfigSecrets(), dir
	records, _, err := fresh.ListLegacyModelConflicts(ctx, "manager", "")
	if err != nil || len(records) != 2 {
		t.Fatalf("both records should exist before cleanup: %v", err)
	}
	var topID, nestedID string
	for _, record := range records {
		if record.Source.Container == 0 {
			topID = record.ID
		} else if record.Source.Container == 1 {
			nestedID = record.ID
		}
	}
	if topID == "" || nestedID == "" {
		t.Fatal("the two entries must have independent source locations")
	}
	if _, err := db.ExecContext(ctx, `UPDATE novel_fetch_legacy_model_conflicts SET ciphertext=? WHERE owner_username='manager' AND id=?`, committed[nestedID], topID); err != nil {
		t.Fatal(err)
	}
	if err := fresh.ResumeLegacyModelCleanup(ctx); err == nil {
		t.Fatal("ciphertext from another source must not validate for cleanup")
	}
	partial, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(partial, []byte("fixture-top-video-key")) || bytes.Contains(partial, []byte("fixture-nested-video-key")) {
		t.Fatal("a mismatched source must stay intact while its verified sibling is scrubbed")
	}
	if _, err := db.ExecContext(ctx, `UPDATE novel_fetch_legacy_model_conflicts SET ciphertext=? WHERE owner_username='manager' AND id=?`, committed[topID], topID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER stop_conflict_ack BEFORE UPDATE ON novel_fetch_workshop_configs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture acknowledgement failure'`); err != nil {
		t.Fatal(err)
	}
	if err := fresh.ResumeLegacyModelCleanup(ctx); err == nil {
		t.Fatal("post-replacement acknowledgement fault should remain retryable")
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER stop_conflict_ack"); err != nil {
		t.Fatal(err)
	}
	if err := fresh.ResumeLegacyModelCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	store = fresh
	if _, err := store.GetConfig(ctx, "manager"); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(path)
	if err != nil || bytes.Contains(file, []byte("fixture-top-video-key")) || bytes.Contains(file, []byte("fixture-nested-video-key")) ||
		!bytes.Contains(file, []byte("compact")) {
		t.Fatal("cold recovery did not scrub both original entries and retain preferences")
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username='manager'`).Scan(&raw); err != nil ||
		!strings.Contains(raw, beforeText) || strings.Contains(raw, "modelCatalogLegacyScrubPending") || strings.Contains(raw, "fixture-top-video-key") {
		t.Fatal("pending marker or unrelated ciphertext was changed incorrectly")
	}
	for id, cipher := range lookupCiphertexts() {
		if committed[id] != cipher {
			t.Fatal("restarting cleanup replaced an existing ciphertext")
		}
	}
	authStore := auth.NewMySQLStore(db)
	center := accountcenter.NewMySQLStore(db)
	now := time.Now().UTC()
	for username, role := range map[string]string{"founder": accountcenter.RoleFounder, "manager": accountcenter.RoleManager, "manager2": accountcenter.RoleManager, "member": accountcenter.RoleMember} {
		if err := authStore.CreateAccount(ctx, auth.Account{Username: username, PasswordHash: "fixture", PasswordAlgorithm: "fixture",
			Active: true, IsOwner: role == accountcenter.RoleFounder, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO account_center_profiles(username,display_name,role) VALUES(?,?,?)`, username, username, role); err != nil {
			t.Fatal(err)
		}
		if err := authStore.CreateSession(ctx, auth.Session{Username: username, Token: username, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	h := NewRouter(RouterOptions{BridgeSecret: "fixture-local-signature", AuthStore: authStore, AccountCenterStore: center,
		NovelFetchStore: store, UserConfigStore: config, Now: func() time.Time { return now }})
	const list = "/api/config/model-conflicts"
	response := impersonationRequest(h, "GET", list, "manager", "", nil)
	var result struct {
		Conflicts []struct {
			ID string `json:"id"`
		} `json:"conflicts"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Conflicts) != 2 {
		t.Fatalf("expected two independent recoverable records, status=%d count=%d", response.Code, len(result.Conflicts))
	}
	if strings.Contains(response.Body.String(), "fixture-video") || strings.Contains(response.Body.String(), "https://") || strings.Contains(response.Body.String(), "fingerprint") {
		t.Fatal("restricted recovery listing disclosed private metadata")
	}
	first := list + "/" + result.Conflicts[0].ID
	for _, identity := range []string{"member", "manager2"} {
		if got := impersonationRequest(h, "POST", first+"/reveal?owner=manager", identity, "", nil); got.Code != http.StatusForbidden {
			t.Fatalf("cross-account recovery was accepted for %s: %d", identity, got.Code)
		}
	}
	bridge := httptest.NewRequest("POST", first+"/reveal", nil)
	SignBridgeRequest(bridge, "manager", false, now, "fixture-local-signature")
	bridgeResponse := httptest.NewRecorder()
	h.ServeHTTP(bridgeResponse, bridge)
	if bridgeResponse.Code != http.StatusUnauthorized {
		t.Fatal("bridge credentials cannot reveal archived keys")
	}
	cookie := startImpersonationForTest(t, h, "founder", "manager")
	if got := impersonationRequest(h, "POST", first+"/reveal", "founder", "", cookie); got.Code != http.StatusForbidden {
		t.Fatal("a switched session cannot reveal archived keys")
	}
	keys := map[string]bool{}
	for _, item := range result.Conflicts {
		response := impersonationRequest(h, "POST", list+"/"+item.ID+"/reveal", "manager", "", nil)
		var revealed struct {
			Value string `json:"value"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &revealed) != nil {
			t.Fatalf("reveal failed: %d", response.Code)
		}
		keys[revealed.Value] = true
	}
	if !keys["fixture-top-video-key"] || !keys["fixture-nested-video-key"] {
		t.Fatal("the two original keys must be independently recoverable")
	}
	if got := impersonationRequest(h, "POST", first+"/reveal?owner=manager", "founder", "", nil); got.Code != http.StatusOK {
		t.Fatalf("founder recovery in a normal login: %d", got.Code)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER stop_conflict_audit BEFORE INSERT ON account_center_audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture audit failure'`); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"reveal", "confirm", "discard"} {
		if got := impersonationRequest(h, "POST", first+"/"+action, "manager", "", nil); got.Code != http.StatusServiceUnavailable || strings.Contains(got.Body.String(), "fixture-") {
			t.Fatalf("audit failure must block %s: %d", action, got.Code)
		}
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state FROM novel_fetch_legacy_model_conflicts WHERE owner_username='manager' AND id=?`, result.Conflicts[0].ID).Scan(&state); err != nil || state != "pending" {
		t.Fatal("audit failure changed the recovery state")
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER stop_conflict_audit"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"confirm", "discard"} {
		if got := impersonationRequest(h, "POST", first+"/"+action, "manager", "", nil); got.Code != http.StatusOK {
			t.Fatalf("normal %s failed: %d", action, got.Code)
		}
	}
	if got := impersonationRequest(h, "POST", first+"/reveal", "manager", "", nil); got.Code != http.StatusForbidden {
		t.Fatal("discarded credential is still readable")
	}
	var events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_center_audit_events WHERE subject_username='manager' AND action LIKE 'legacy_key_%'`).Scan(&events); err != nil || events != 5 {
		t.Fatalf("expected 3 reveals and two audited decisions; audit count %d, err %v", events, err)
	}
	video := NewCatalogVideoProviderRegistry(center, store)
	if _, err := video.ResolveModel(ctx, "manager", batchfactoryv11.VideoProviderPersonalAPI, ""); err == nil {
		t.Fatal("recovery entries must never become selectable video credentials")
	}
	for _, path := range []string{"/api/config/models?kind=video", "/api/models?kind=video"} {
		if got := impersonationRequest(h, "GET", path, "manager", "", nil); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(`"models":[]`)) {
			t.Fatalf("ordinary model listing exposed recovery data: %d", got.Code)
		}
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

func TestAPIConfigPolicyMySQLSeparatesDatabaseVideoConflictsAndMergesExactCopies(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = NewUserConfigStore(t.TempDir()).ModelConfigSecrets()
	for _, tc := range []struct {
		owner, nestedKey string
		wantConflicts    int
		preserveBound    bool
	}{
		{owner: "db-conflict", nestedKey: "fixture-other-db-key", wantConflicts: 2},
		{owner: "db-exact-copy", nestedKey: "fixture-first-db-key", wantConflicts: 0},
		{owner: "db-bound-conflict", nestedKey: "fixture-other-db-key", wantConflicts: 2, preserveBound: true},
	} {
		build := func(key string) map[string]any {
			return map[string]any{"personal_api": map[string]any{"apiKey": key, "model": "fixture-video", "createUrl": "https://video.example.test/create"}}
		}
		doc, err := json.Marshal(map[string]any{"modelCatalogSecretVersion": "bound-v3", "modelCatalog": []any{},
			"videoProviders": build("fixture-first-db-key"), "settings": map[string]any{"videoProviders": build(tc.nestedKey)}})
		if err != nil {
			t.Fatal(err)
		}
		var originalCipher string
		if tc.preserveBound {
			cfg := map[string]any{"modelCatalog": []any{
				map[string]any{"id": "legacy-video:personal_api", "kind": "video", "modelId": "fixture-video", "provider": "personal_api",
					"endpoint": "https://video.example.test/create", "credential": "fixture-established"},
			}}
			if err := store.PutConfig(ctx, tc.owner, cfg); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.modelCatalog[0].credential')) FROM novel_fetch_workshop_configs WHERE owner_username=?`, tc.owner).Scan(&originalCipher); err != nil {
				t.Fatal(err)
			}
			top, _ := json.Marshal(build("fixture-first-db-key"))
			nested, _ := json.Marshal(build(tc.nestedKey))
			if _, err := db.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET(settings_json,'$.videoProviders',CAST(? AS JSON),'$.settings',JSON_OBJECT('videoProviders',CAST(? AS JSON))) WHERE owner_username=?`, string(top), string(nested), tc.owner); err != nil {
				t.Fatal(err)
			}
		} else if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs (owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, tc.owner, string(doc)); err != nil {
			t.Fatal(err)
		}
		settings, err := store.GetConfig(ctx, tc.owner)
		if err != nil {
			t.Fatal(err)
		}
		records, _, err := store.ListLegacyModelConflicts(ctx, tc.owner, "")
		if err != nil || len(records) != tc.wantConflicts {
			t.Fatalf("%s: recovery records=%d, err=%v", tc.owner, len(records), err)
		}
		rows := modelCatalog(settings)
		if len(rows) != 1 || tc.wantConflicts == 0 && rows[0]["credential"] != "fixture-first-db-key" ||
			tc.preserveBound && rows[0]["credential"] != "fixture-established" ||
			!tc.preserveBound && tc.wantConflicts != 0 && rows[0]["credentialStatus"] != "needs_reentry" {
			t.Fatalf("%s: expected a usable exact duplicate or an inactive conflict", tc.owner)
		}
		var raw []byte
		if err := db.QueryRowContext(ctx, "SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?", tc.owner).Scan(&raw); err != nil ||
			bytes.Contains(raw, []byte("videoProviders")) || bytes.Contains(raw, []byte("-db-key")) {
			t.Fatalf("%s: legacy database bytes were not scrubbed", tc.owner)
		}
		if tc.preserveBound && !bytes.Contains(raw, []byte(originalCipher)) {
			t.Fatal("existing formal video ciphertext changed during legacy recovery")
		}
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

func TestAPIConfigPolicyMySQLArbitratesMixedLegacyVideoSources(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	migrations := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	migrations = append(migrations, storage.NovelFetchWorkshopMigrations()...)
	if err := storage.RunMigrations(ctx, db, migrations); err != nil {
		t.Fatal(err)
	}
	const (
		model       = "legacy-video:personal_api"
		first       = "fixture-database-key"
		second      = "fixture-file-key"
		established = "fixture-established-key"
	)
	dir := t.TempDir()
	config := NewUserConfigStore(dir)
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = config.ModelConfigSecrets()
	center, login := accountcenter.NewMySQLStore(db), auth.NewMySQLStore(db)
	now := time.Now().UTC()
	writeSources := func(t *testing.T, owner, fileKey string, existing map[string]any) string {
		t.Helper()
		rows := []any{map[string]any{"id": "unrelated-text", "kind": "text", "modelId": "text", "baseUrl": "https://text.example.test/v1", "credential": "fixture-text"}}
		if existing != nil {
			rows = append(rows, existing)
		}
		if err := store.PutConfig(ctx, owner, map[string]any{"modelCatalog": rows}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET(settings_json,'$.videoProviders',
JSON_OBJECT('personal_api',JSON_OBJECT('apiKey',?,'model','fixture-video'))) WHERE owner_username=?`, first, owner); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, owner, "api-config.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"settings": map[string]any{"videoProviders": map[string]any{
			"personal_api": map[string]any{"apiKey": fileKey, "model": "fixture-video"},
		}}, "layout": map[string]any{"compact": true}})
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	official := func(fields map[string]any) map[string]any {
		fields["id"], fields["kind"], fields["modelId"], fields["provider"] = model, "video", "fixture-video", "personal_api"
		fields["endpoint"], fields["tasksEndpoint"] = batchfactoryv11.DefaultPersonalVideoCreateURL, batchfactoryv11.DefaultPersonalVideoTasksURL
		fields["resultEndpoint"] = batchfactoryv11.DefaultPersonalVideoTasksURL + "/{id}/result"
		return fields
	}
	rawConfig := func(t *testing.T, owner string) string {
		t.Helper()
		var raw string
		if err := db.QueryRowContext(ctx, "SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?", owner).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cipherAt := func(t *testing.T, owner string, index int) string {
		t.Helper()
		locator := fmt.Sprintf("$.modelCatalog[%d].credential", index)
		var value string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,?)),'') FROM novel_fetch_workshop_configs WHERE owner_username=?`, locator, owner).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, tc := range []struct {
		owner, fileKey, runtime string
		existing                map[string]any
		faults                  bool
		delayed                 bool
		savePreference          bool
		rewritePreferenceFile   bool
		preferenceFirst         bool
	}{
		{owner: "mixed-conflict", fileKey: second, faults: true},
		{owner: "delayed-conflict", fileKey: second, delayed: true},
		{owner: "delayed-write-failure", fileKey: second, delayed: true, faults: true},
		{owner: "delayed-preference-save", fileKey: second, delayed: true, savePreference: true},
		{owner: "delayed-preference-copy", fileKey: second, delayed: true, savePreference: true, rewritePreferenceFile: true},
		{owner: "delayed-preference-first", fileKey: second, delayed: true, savePreference: true, preferenceFirst: true},
		{owner: "delayed-identical", fileKey: first, runtime: first, delayed: true},
		{owner: "mixed-identical", fileKey: first, runtime: first},
		{owner: "mixed-established", fileKey: second, runtime: established, existing: official(map[string]any{"credential": established})},
		{owner: "mixed-blank", fileKey: second, existing: official(map[string]any{"credentialStatus": "needs_reentry"})},
	} {
		t.Run(tc.owner, func(t *testing.T) {
			if err := login.CreateAccount(ctx, auth.Account{Username: tc.owner, PasswordHash: "fixture", PasswordAlgorithm: "fixture",
				Active: true, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if err := center.CreateIdentity(ctx, accountcenter.Identity{Username: tc.owner, Role: accountcenter.RoleManager}); err != nil {
				t.Fatal(err)
			}
			if err := login.CreateSession(ctx, auth.Session{Username: tc.owner, Token: tc.owner, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			path := writeSources(t, tc.owner, tc.fileKey, tc.existing)
			store.LegacyConfigDir = dir
			textCipher, videoCipher := cipherAt(t, tc.owner, 0), cipherAt(t, tc.owner, 1)
			coldStore := func() *novelfetchworkshop.MySQLStore {
				fresh := novelfetchworkshop.NewMySQLStore(db)
				fresh.ModelSecrets = NewUserConfigStore(dir).ModelConfigSecrets()
				fresh.LegacyConfigDir = dir
				return fresh
			}
			if tc.delayed {
				if err := os.Rename(path, path+".offline"); err != nil {
					t.Fatal(err)
				}
				var public map[string]any
				if !tc.preferenceFirst {
					var err error
					public, err = store.GetConfig(ctx, tc.owner)
					if err != nil {
						t.Fatal(err)
					}
					if raw := rawConfig(t, tc.owner); strings.Contains(raw, first) || strings.Contains(raw, "entry-v2") {
						t.Error("unchecked database source was stored in plaintext or marked complete")
					}
					if _, exposed := public["modelCatalogLegacyVideoPending"]; exposed {
						t.Error("internal video provenance reached the returned configuration")
					}
				}
				if tc.savePreference {
					var schema string
					if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
						t.Fatal(err)
					}
					t.Logf("disposable MySQL schema=%s", schema)
					router := NewRouter(RouterOptions{AuthStore: login, AccountCenterStore: center, NovelFetchStore: store, UserConfigStore: config, Now: func() time.Time { return now }})
					for _, volume := range []int{37, 42} {
						body := fmt.Sprintf(`{"notifications":{"soundVolume":%d}}`, volume)
						if volume == 37 {
							body = `{"notifications":{"soundVolume":37,"soundEnabled":false}}`
						}
						response := impersonationRequest(router, "POST", "/api/config", tc.owner, body, nil)
						if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "modelCatalogLegacy") {
							t.Fatal("ordinary preference save failed or returned internal migration metadata")
						}
						fresh := NewRouter(RouterOptions{AuthStore: login, AccountCenterStore: center, NovelFetchStore: coldStore(), UserConfigStore: NewUserConfigStore(dir), Now: func() time.Time { return now }})
						got := impersonationRequest(fresh, "GET", "/api/config", tc.owner, "", nil)
						var saved struct{ Notifications struct{ SoundVolume int } }
						if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &saved) != nil || saved.Notifications.SoundVolume != volume {
							t.Fatal("ordinary preference save did not survive a fresh store")
						}
					}
					if tc.rewritePreferenceFile {
						replacements := []string{`{"notifications":{"soundVolume":42}}`, `{"modelCatalog":[],"modelCatalogLegacyDefaultsFile":false}`, `{"settings":{"videoProviders":{"personal_api":{"model":"fixture-video","apiKey":""}}}}`}
						changes, err := os.ReadFile(filepath.Join(filepath.Dir(path), "deferred-settings.json"))
						if err != nil || !bytes.Contains(changes, []byte("enc:v2:")) || bytes.Contains(changes, []byte("soundVolume")) {
							t.Error("deferred preference changes are not independently encrypted")
						} else {
							replacements = append(replacements, string(changes))
						}
						for _, doc := range replacements {
							if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
								t.Fatal(err)
							}
							resolved, err := NewCatalogVideoProviderRegistry(center, coldStore()).ResolveExactVideoModel(ctx, tc.owner, model)
							if err == nil || resolved.APIKey != "" || !strings.Contains(rawConfig(t, tc.owner), "modelCatalogLegacyVideoPending") {
								t.Error("a replacement without matching video credential evidence completed the migration")
							}
						}
					}
				} else {
					public["layoutDensity"] = "compact"
					if err := store.PutConfig(ctx, tc.owner, public); err != nil {
						t.Fatal(err)
					}
				}
				pending, err := NewCatalogVideoProviderRegistry(center, coldStore()).ResolveExactVideoModel(ctx, tc.owner, model)
				if err == nil || pending.APIKey != "" {
					t.Errorf("unchecked video resolved before file returned (database fixture selected=%t)", pending.APIKey == first)
				}
				if raw := rawConfig(t, tc.owner); !strings.Contains(raw, "modelCatalogLegacyVideoPending") || strings.Contains(raw, "entry-v2") {
					t.Error("source verification ended while original file was offline")
				}
				if err := os.Rename(path+".offline", path); err != nil {
					t.Fatal(err)
				}
				store = coldStore()
			}
			wantConflicts := 2
			if tc.fileKey == first {
				wantConflicts = 0
			}
			wantState := func(t *testing.T) {
				t.Helper()
				records, _, err := store.ListLegacyModelConflicts(ctx, tc.owner, "")
				if err != nil || len(records) != wantConflicts {
					t.Fatalf("conflict rows=%d want=%d err=%v", len(records), wantConflicts, err)
				}
			}
			if tc.faults {
				before := rawConfig(t, tc.owner)
				file, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, `CREATE TRIGGER stop_mixed_plan BEFORE UPDATE ON novel_fetch_workshop_configs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture update failure'`); err != nil {
					t.Fatal(err)
				}
				_, migrateErr := store.GetConfig(ctx, tc.owner)
				if _, err := db.ExecContext(ctx, "DROP TRIGGER stop_mixed_plan"); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(path)
				if err != nil || migrateErr == nil || rawConfig(t, tc.owner) != before || !bytes.Equal(file, after) {
					t.Fatal("DB failure did not retain both original sources")
				}
				store.ReplaceLegacyConfig = func(string, map[string]any) error { return errors.New("fixture write failure") }
				if _, err := store.GetConfig(ctx, tc.owner); err == nil {
					t.Fatal("expected file failure")
				}
				store.ReplaceLegacyConfig = nil
				wantState(t)
				if raw := rawConfig(t, tc.owner); !strings.Contains(raw, "modelCatalogLegacyScrubPending") || strings.Contains(raw, first) {
					t.Fatal("file failure should leave a pending plan and a committed database-source archive")
				}
				after, err = os.ReadFile(path)
				if err != nil || !bytes.Equal(file, after) {
					t.Fatal("failed file write damaged original evidence")
				}
			}
			result, err := store.GetConfig(ctx, tc.owner)
			if err != nil {
				t.Fatal(err)
			}
			if cipherAt(t, tc.owner, 0) != textCipher || tc.runtime == established && cipherAt(t, tc.owner, 1) != videoCipher {
				t.Fatal("migration changed a previously bound ciphertext")
			}
			video := NewCatalogVideoProviderRegistry(center, store)
			chosen, err := video.ResolveExactVideoModel(ctx, tc.owner, model)
			if tc.delayed && tc.runtime == "" && (err == nil || chosen.APIKey != "") {
				t.Errorf("disputed video resolved after restart (database fixture selected=%t)", chosen.APIKey == first)
			}
			if tc.runtime == "" {
				rows := modelCatalog(result)
				if err == nil || len(rows) != 2 || rows[1]["credentialStatus"] != "needs_reentry" || firstText(rows[1]["credential"]) != "" {
					t.Fatal("runtime returned a legacy key from a disputed group")
				}
			} else if err != nil || chosen.APIKey != tc.runtime {
				t.Fatalf("runtime failed to preserve the one proven key: %v", err)
			}
			wantState(t)
			if tc.delayed {
				store = coldStore()
				restored, err := NewCatalogVideoProviderRegistry(center, store).ResolveExactVideoModel(ctx, tc.owner, model)
				if (err == nil) != (tc.runtime != "") || restored.APIKey != tc.runtime {
					t.Fatal("another restart changed the reconciled video")
				}
				wantState(t)
				if tc.savePreference {
					router := NewRouter(RouterOptions{AuthStore: login, AccountCenterStore: center, NovelFetchStore: store, UserConfigStore: NewUserConfigStore(dir), Now: func() time.Time { return now }})
					reply := impersonationRequest(router, "GET", "/api/config", tc.owner, "", nil)
					var prefs struct {
						Notifications struct {
							SoundVolume  int
							SoundEnabled *bool
						}
					}
					if reply.Code != http.StatusOK || json.Unmarshal(reply.Body.Bytes(), &prefs) != nil || prefs.Notifications.SoundVolume != 42 {
						t.Errorf("restoring legacy credentials lost the saved sound volume: got %d, want 42", prefs.Notifications.SoundVolume)
					}
					if prefs.Notifications.SoundEnabled == nil || *prefs.Notifications.SoundEnabled {
						t.Error("restoring the file lost the earlier mute preference")
					}
				}
			}
			file, err := os.ReadFile(path)
			raw := rawConfig(t, tc.owner)
			if err != nil || bytes.Contains(file, []byte(tc.fileKey)) || !bytes.Contains(file, []byte("compact")) ||
				strings.Contains(raw, first) || strings.Contains(raw, "modelCatalogLegacyScrubPending") || strings.Contains(raw, "modelCatalogLegacyVideoPending") || strings.Contains(raw, "videoProviders") {
				t.Fatal("successful migration did not scrub and acknowledge both sources")
			}
			if wantConflicts == 0 {
				return
			}
			records, _, err := store.ListLegacyModelConflicts(ctx, tc.owner, "")
			if err != nil {
				t.Fatal(err)
			}
			h := NewRouter(RouterOptions{AuthStore: login, AccountCenterStore: center, NovelFetchStore: store, UserConfigStore: config, Now: func() time.Time { return now }})
			origins := map[string]string{}
			for _, item := range records {
				var sealed string
				if err := db.QueryRowContext(ctx, "SELECT ciphertext FROM novel_fetch_legacy_model_conflicts WHERE owner_username=? AND id=?", tc.owner, item.ID).Scan(&sealed); err != nil ||
					!strings.HasPrefix(sealed, "enc:v2:") || strings.Contains(sealed, first) || strings.Contains(sealed, second) {
					t.Fatal("conflict evidence is not encrypted")
				}
				got := impersonationRequest(h, "POST", "/api/config/model-conflicts/"+item.ID+"/reveal", tc.owner, "", nil)
				var found struct{ Value string }
				if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &found) != nil {
					t.Fatalf("audited reveal failed: %d", got.Code)
				}
				origin, _, _ := strings.Cut(item.Source.Document, ":")
				origins[origin] = found.Value
			}
			var reveals int
			if origins["db"] != first || origins["file"] != second ||
				db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_center_audit_events WHERE subject_username=? AND action LIKE 'legacy_key_reveal:%'`, tc.owner).Scan(&reveals) != nil || reveals != 2 {
				t.Fatal("the two source keys are not independently recoverable and audited")
			}
		})
	}
}
