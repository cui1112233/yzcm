package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/apisecrets"
	"qiantie/backend/internal/novelfetchworkshop"
)

func textRepairCipherFixture(t *testing.T, owner string) (*configSecretProvider, []byte, string) {
	t.Helper()
	users := filepath.Join(t.TempDir(), "users")
	secrets := newConfigSecretProvider(users)
	config := map[string]any{novelfetchworkshop.ModelSecretVersionKey: novelfetchworkshop.ModelSecretVersion, "configRevision": "7", "modelCatalogLegacyScrubPending": "retained-proof", "modelCatalogLegacyScrubEntries": []any{map[string]any{"proof": "retained"}}, "modelCatalog": []any{
		map[string]any{"id": "text-main", "kind": "text", "displayName": "Text fixture", "modelId": "text-model", "baseUrl": "https://text.invalid/v1", "enabled": true, "credential": "", "credentialStatus": "needs_reentry"},
		map[string]any{"id": "video-kept", "kind": "video", "modelId": "video-model", "baseUrl": "https://video.invalid/v1", "provider": "personal_api", "credential": "other-fixture-only", "enabled": true},
	}}
	if err := secrets.SealModelConfig(owner, config, nil); err != nil {
		t.Fatal("seal fixture", err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["opaqueBigNumber"] = json.RawMessage(`9007199254740993`)
	var rows []json.RawMessage
	if err := json.Unmarshal(fields["modelCatalog"], &rows); err != nil {
		t.Fatal(err)
	}
	var other map[string]json.RawMessage
	if err := json.Unmarshal(rows[1], &other); err != nil {
		t.Fatal(err)
	}
	other["unknownBigNumber"] = json.RawMessage(`9007199254740993`)
	rows[1], _ = json.Marshal(other)
	fields["modelCatalog"], _ = json.Marshal(rows)
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(users, owner, "api-config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"legacyCredential":"retained-fixture-only"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return secrets, raw, legacy
}

type boundCipherArgument struct{ value string }

func (a *boundCipherArgument) Match(raw driver.Value) bool {
	value, ok := raw.(string)
	if !ok || !strings.HasPrefix(value, apisecrets.BoundPrefix) {
		return false
	}
	a.value = value
	return true
}

func TestBoundTextRepairUsesRealOwnerModelDestinationCipher(t *testing.T) {
	secrets, raw, legacy := textRepairCipherFixture(t, "alice")
	before, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = secrets
	store.LegacyConfigDir = filepath.Dir(filepath.Dir(legacy))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?")).WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(raw))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE")).WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(raw))
	capture := &boundCipherArgument{}
	// Exact leaf whitelist: no whole JSON replacement and no migration SQL.
	update := `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET(JSON_REMOVE(settings_json,?,?,?),?,?,'$.configRevision',?),updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.configRevision')),'0')=?`
	mock.ExpectExec(regexp.QuoteMeta(update)).WithArgs("$.modelCatalog[0].apiKey", "$.modelCatalog[0].credentialStatus", "$.modelCatalog[0].credentialRecoveryReason", "$.modelCatalog[0].credential", capture, "8", "alice", "7").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	model, handled, err := store.TryPatchBoundTextCredential(context.Background(), "alice", "text-main", map[string]any{"credential": "new-fixture-only", "displayName": "Text fixture", "name": "Text fixture", "modelId": "text-model", "baseUrl": "https://text.invalid/v1"})
	if err != nil || !handled || capture.value == "" {
		t.Fatalf("real cipher repair failed: %v", err)
	}
	verified := map[string]any{novelfetchworkshop.ModelSecretVersionKey: novelfetchworkshop.ModelSecretVersion, "modelCatalog": []any{cloneAnyMap(model)}}
	if err := secrets.OpenModelConfig("alice", verified); err != nil || modelCatalog(verified)[0]["credential"] != "new-fixture-only" {
		t.Fatal("new credential was not bound to original owner/model/target", err)
	}
	for _, tamper := range []string{"owner", "model", "destination"} {
		altered := cloneAnyMap(model)
		owner := "alice"
		switch tamper {
		case "owner":
			owner = "other-owner"
		case "model":
			altered["modelId"] = "other-model"
		case "destination":
			altered["baseUrl"] = "https://different.invalid/v1"
		}
		cfg := map[string]any{novelfetchworkshop.ModelSecretVersionKey: novelfetchworkshop.ModelSecretVersion, "modelCatalog": []any{altered}}
		if err := secrets.OpenModelConfig(owner, cfg); err == nil {
			t.Fatalf("cipher accepted changed %s", tamper)
		}
	}
	after, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("repair touched retained legacy file")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBodySafetyMySQLBoundTextRepairPreservesOtherRawAndMigration(t *testing.T) {
	dsn := os.Getenv("QIANTIE_BODY_SAFETY_TEST_DSN")
	if dsn == "" {
		t.Skip("explicit isolated MySQL DSN not configured")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(config.DBName, "nfw_body_safety_") {
		t.Fatal("requires isolated nfw_body_safety_ database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated database failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var selected string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selected); err != nil || selected != config.DBName {
		t.Fatal("refusing non-test database")
	}
	owner := fmt.Sprintf("bound-text-repair-%d", time.Now().UnixNano())
	secrets, raw, legacy := textRepairCipherFixture(t, owner)
	beforeFile, _ := os.ReadFile(legacy)
	defer func() {
		for _, table := range []string{"novel_fetch_workshop_configs", "novel_fetch_legacy_config_migrations"} {
			if _, err := db.Exec("DELETE FROM "+table+" WHERE owner_username=?", owner); err != nil {
				t.Error("fixture cleanup failed", err)
			}
		}
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))`, owner, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES(?,'finalizing')`, owner); err != nil {
		t.Fatal(err)
	}
	const snapshot = `SELECT JSON_EXTRACT(settings_json,'$.modelCatalog[1]'),JSON_EXTRACT(settings_json,'$.modelCatalogLegacyScrubPending'),JSON_EXTRACT(settings_json,'$.modelCatalogLegacyScrubEntries'),JSON_EXTRACT(settings_json,'$.opaqueBigNumber') FROM novel_fetch_workshop_configs WHERE owner_username=?`
	var other, proof, entries, big []byte
	if err := db.QueryRowContext(ctx, snapshot, owner).Scan(&other, &proof, &entries, &big); err != nil {
		t.Fatal(err)
	}
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = secrets
	store.LegacyConfigDir = filepath.Dir(filepath.Dir(legacy))
	if _, handled, err := store.TryPatchBoundTextCredential(ctx, owner, "text-main", map[string]any{"credential": "new-fixture-only", "enabled": true}); err != nil || !handled {
		t.Fatal("isolated repair failed", err)
	}
	var afterOther, afterProof, afterEntries, afterBig []byte
	if err := db.QueryRowContext(ctx, snapshot, owner).Scan(&afterOther, &afterProof, &afterEntries, &afterBig); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(other, afterOther) || !bytes.Equal(proof, afterProof) || !bytes.Equal(entries, afterEntries) || !bytes.Equal(big, afterBig) || string(afterBig) != "9007199254740993" {
		t.Fatal("repair rewrote unrelated JSON, large integer, or recovery evidence")
	}
	var phase string
	var revision uint64
	if err := db.QueryRowContext(ctx, "SELECT phase,revision FROM novel_fetch_legacy_config_migrations WHERE owner_username=?", owner).Scan(&phase, &revision); err != nil || phase != "finalizing" || revision != 1 {
		t.Fatal("repair changed migration state", err)
	}
	runtime, err := store.ReadRuntimeConfig(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	models := modelCatalog(runtime)
	if models[0]["credential"] != "new-fixture-only" || models[0]["credentialStatus"] == "needs_reentry" || models[1]["enabled"] != false || models[1]["credential"] != nil {
		t.Fatal("text repair enabled an unverified video or did not repair text")
	}
	afterFile, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(beforeFile, afterFile) {
		t.Fatal("repair modified legacy fixture file")
	}
}
