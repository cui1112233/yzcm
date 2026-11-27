package novelfetchworkshop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBoundTextCredentialRepairLeavesTargetEditsAndVideosOnStrictPath(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		patch map[string]any
	}{
		{boundTextFixture, map[string]any{"credential": "new-fixture", "baseUrl": "https://changed.invalid"}},
		{boundTextFixture, map[string]any{"clearCredential": true, "enabled": false}},
		{strings.Replace(boundTextFixture, `"kind":"text"`, `"kind":"video"`, 1), map[string]any{"credential": "new-fixture"}},
		{strings.Replace(boundTextFixture, `"bound-v3"`, `"bound-v2"`, 1), map[string]any{"credential": "new-fixture"}},
	} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		s := NewMySQLStore(db)
		s.ModelSecrets = &boundTextSecretProbe{}
		mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\?").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(tc.raw))
		_, handled, err := s.TryPatchBoundTextCredential(context.Background(), "alice", "text-main", tc.patch)
		if err != nil || handled {
			t.Fatal("target/video/unbound edit entered narrow repair", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

type boundTextRepairer interface {
	TryPatchBoundTextCredential(context.Context, string, string, map[string]any) (map[string]any, bool, error)
}
type boundTextSecretProbe struct {
	ModelConfigSecrets
	opened, sealed int
	failure        error
}

func (p *boundTextSecretProbe) OpenModelConfig(owner string, settings map[string]any) error {
	p.opened++
	if p.failure != nil {
		return p.failure
	}
	models := settings["modelCatalog"].([]any)
	if owner != "alice" || len(models) != 1 || models[0].(map[string]any)["id"] != "text-main" {
		return errors.New("wrong credential binding scope")
	}
	return nil
}
func (p *boundTextSecretProbe) SealModelConfig(owner string, next, previous map[string]any) error {
	p.sealed++
	if owner != "alice" || len(previous["modelCatalog"].([]any)) != 1 {
		return errors.New("wrong seal binding scope")
	}
	model := next["modelCatalog"].([]any)[0].(map[string]any)
	if model["credential"] != "" {
		model["credential"] = "sealed-test"
	}
	return nil
}

const boundTextFixture = `{"configRevision":"7","modelCatalogSecretVersion":"bound-v3","modelCatalogLegacyScrubPending":"keep","modelCatalogLegacyScrubEntries":[{"opaque":9007199254740993}],"modelCatalog":[{"id":"text-main","kind":"text","displayName":"Text fixture","modelId":"text-model","baseUrl":"https://text.invalid/v1","enabled":true,"credential":"","credentialStatus":"needs_reentry","opaque":9007199254740993},{"id":"video","kind":"video","credential":"unchanged","opaque":9007199254740993}]}`

func TestBoundTextCredentialRepairChangesOnlyBoundCredentialLeaves(t *testing.T) {
	for _, clear := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		s := NewMySQLStore(db)
		probe := &boundTextSecretProbe{}
		s.ModelSecrets = probe
		repair, ok := any(s).(boundTextRepairer)
		if !ok {
			t.Fatal("missing narrow bound-text credential repair")
		}
		mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\?").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(boundTextFixture))
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\? FOR UPDATE").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(boundTextFixture))
		update := mock.ExpectExec("UPDATE novel_fetch_workshop_configs SET settings_json=JSON_SET\\(JSON_REMOVE")
		patch := map[string]any{"credential": "new-fixture", "enabled": true, "name": "Text fixture", "displayName": "Text fixture", "modelId": "text-model", "baseUrl": "https://text.invalid/v1"}
		if clear {
			patch = map[string]any{"clearCredential": true}
			update.WithArgs("$.modelCatalog[0].apiKey", "$.modelCatalog[0].credential", "", "8", "alice", "7")
		} else {
			update.WithArgs("$.modelCatalog[0].apiKey", "$.modelCatalog[0].credentialStatus", "$.modelCatalog[0].credentialRecoveryReason", "$.modelCatalog[0].credential", "sealed-test", "8", "alice", "7")
		}
		update.WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		model, handled, err := repair.TryPatchBoundTextCredential(context.Background(), "alice", "text-main", patch)
		if err != nil || !handled || model["id"] != "text-main" || probe.opened != 1 || probe.sealed != 1 {
			t.Fatalf("repair failed: handled=%v err=%v", handled, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBoundTextCredentialRepairRejectsConcurrentChangeAndBadBinding(t *testing.T) {
	for _, kind := range []string{"revision", "target", "binding"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := NewMySQLStore(db)
			probe := &boundTextSecretProbe{}
			s.ModelSecrets = probe
			repair, ok := any(s).(boundTextRepairer)
			if !ok {
				t.Fatal("missing narrow bound-text credential repair")
			}
			mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\?").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(boundTextFixture))
			mock.ExpectBegin()
			latest := boundTextFixture
			if kind == "revision" {
				latest = `{"configRevision":"8","modelCatalogSecretVersion":"bound-v3","modelCatalog":[]}`
			}
			if kind == "target" {
				latest = `{"configRevision":"7","modelCatalogSecretVersion":"bound-v3","modelCatalog":[{"id":"text-main","kind":"text","baseUrl":"https://changed.invalid"}]}`
			}
			if kind == "binding" {
				probe.failure = errors.New("binding rejected")
			}
			mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\? FOR UPDATE").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(latest))
			mock.ExpectRollback()
			_, handled, err := repair.TryPatchBoundTextCredential(context.Background(), "alice", "text-main", map[string]any{"credential": "new-fixture"})
			if err == nil || !handled || probe.sealed != 0 {
				t.Fatal("concurrent/unverified repair reached sealing")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
