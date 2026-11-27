package novelfetchworkshop

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const runtimeSnapshotQuery = `SELECT c.settings_json,EXISTS(
SELECT 1 FROM novel_fetch_legacy_config_migrations m WHERE m.owner_username=? AND m.phase!='complete')
FROM novel_fetch_workshop_configs c WHERE c.owner_username=?`

type runtimeSecretProbe struct {
	ModelConfigSecrets
	calls   int
	owner   string
	failure error
}

func (p *runtimeSecretProbe) OpenModelConfig(owner string, settings map[string]any) error {
	p.calls++
	p.owner = owner
	if p.failure != nil {
		return p.failure
	}
	for _, item := range settings["modelCatalog"].([]any) {
		item.(map[string]any)["credential"] = "verified-test-key"
	}
	return nil
}

func TestRuntimeConfigReadsVerifiedSnapshotWithoutLegacyCleanup(t *testing.T) {
	for _, pending := range []bool{false, true} {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		if err != nil {
			t.Fatal(err)
		}
		store := NewMySQLStore(db)
		probe := &runtimeSecretProbe{}
		store.ModelSecrets = probe
		reader, ok := any(store).(interface {
			ReadRuntimeConfig(context.Context, string) (map[string]any, error)
		})
		if !ok {
			db.Close()
			t.Fatal("missing read-only runtime configuration view")
		}
		raw := `{"modelCatalogSecretVersion":"bound-v3","modelCatalogLegacyScrubPending":"held","modelCatalogLegacyMigrationVersion":"entry-v2","platforms":[{"id":"2","name":"fixture"}],"modelCatalogVersion":7,"modelCatalog":[{"id":"text","kind":"text","enabled":true,"credential":"bound-test"},{"id":"video","kind":"video","enabled":true,"credential":"bound-video"}],"appConfig":{"videoProviders":{"legacy":{"apiKey":"must-not-return"}},"maxTxt":4000}}`
		mock.ExpectQuery(runtimeSnapshotQuery).WithArgs("alice", "alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json", "pending"}).AddRow(raw, pending))
		config, err := reader.ReadRuntimeConfig(context.Background(), "alice")
		if err != nil {
			t.Fatal(err)
		}
		if probe.calls != 1 || probe.owner != "alice" {
			t.Fatal("formal model keys were not verified for this owner")
		}
		rows := config["modelCatalog"].([]any)
		text := rows[0].(map[string]any)
		video := rows[1].(map[string]any)
		if text["credential"] != "verified-test-key" || text["enabled"] != true {
			t.Fatal("verified text model became unavailable")
		}
		if pending {
			if video["enabled"] != false || video["credential"] != nil || video["apiKey"] != nil || video["id"] != "video" {
				t.Fatalf("pending video activated: %#v", video)
			}
		} else if video["credential"] != "verified-test-key" || video["enabled"] != true {
			t.Fatal("completed catalog video changed")
		}
		if config[modelLegacyScrubPendingKey] != nil || config[ModelSecretVersionKey] != nil || config["appConfig"].(map[string]any)["videoProviders"] != nil {
			t.Fatal("legacy cleanup material leaked into runtime preferences")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestRuntimeConfigPreservesSnapshotAndBindingFailures(t *testing.T) {
	for _, kind := range []string{"read", "binding", "unbound"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store := NewMySQLStore(db)
			cause := errors.New("verified binding rejected")
			probe := &runtimeSecretProbe{}
			store.ModelSecrets = probe
			reader, ok := any(store).(interface {
				ReadRuntimeConfig(context.Context, string) (map[string]any, error)
			})
			if !ok {
				t.Fatal("missing read-only runtime view")
			}
			expected := mock.ExpectQuery(runtimeSnapshotQuery).WithArgs("alice", "alice")
			if kind == "read" {
				expected.WillReturnError(cause)
			} else {
				version := "bound-v3"
				if kind == "unbound" {
					version = "bound-v2"
				}
				if kind == "binding" {
					probe.failure = cause
				}
				expected.WillReturnRows(sqlmock.NewRows([]string{"settings_json", "pending"}).AddRow(`{"modelCatalogSecretVersion":"`+version+`","modelCatalog":[]}`, true))
			}
			_, err = reader.ReadRuntimeConfig(context.Background(), "alice")
			if err == nil {
				t.Fatal("unsafe runtime read accepted")
			}
			if kind != "unbound" && !errors.Is(err, cause) {
				t.Fatalf("original storage/binding error lost: %v", err)
			}
			if kind == "unbound" && probe.calls != 0 {
				t.Fatal("unbound keys were opened")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
