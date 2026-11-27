package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/storage"
)

func migrationStateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("requires disposable MySQL")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := fmt.Sprintf("legacy_state_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName, cfg.ParseTime = name, true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLegacyConfigMigrationStateRevisionCAS(t *testing.T) {
	db := migrationStateTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec("INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES('alice','awaiting_source')"); err != nil {
		t.Fatal(err)
	}
	store := NewMySQLStore(db)
	advance := func(revision uint64, phase LegacyConfigMigrationPhase) (bool, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		ok, err := store.advanceLegacyConfigMigration(ctx, tx, "alice", revision, phase)
		if err == nil {
			err = tx.Commit()
		}
		return ok, err
	}
	if ok, err := advance(1, "complete"); ok || err == nil {
		t.Fatal("skipped phases")
	}
	if ok, err := advance(1, "reconciling"); err != nil || !ok {
		t.Fatal("valid transition failed", err)
	}
	if ok, err := advance(1, "finalizing"); err != nil || ok {
		t.Fatal("stale revision changed state", err)
	}
	state, found, err := store.LegacyConfigMigrationState(ctx, "alice")
	if err != nil || !found || state.Revision != 2 || state.Phase != "reconciling" {
		t.Fatal("unexpected state", err)
	}
}

type migrationFinalizerProbe struct {
	ModelConfigSecrets
	finalized bool
}

func (*migrationFinalizerProbe) OpenModelConfig(string, map[string]any) error { return nil }
func (p *migrationFinalizerProbe) FinalizeLegacySettings(string, string) error {
	p.finalized = true
	return nil
}

func TestLegacyConfigMigrationStateStaleFinalization(t *testing.T) {
	db := migrationStateTestDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		revision string
		plan     bool
	}{{"before-plan", "2", false}, {"missing-plan-at-same-config-revision", "3", false}, {"different-plan-same-digest", "3", true}} {
		t.Run(tc.name, func(t *testing.T) {
			current := map[string]any{ModelSecretVersionKey: ModelSecretVersion, ConfigRevisionKey: "3", "modelCatalog": []any{}, modelLegacyScrubPendingKey: "verified-plan", legacyModelMigrationVersionKey: legacyModelMigrationVersion, legacyModelScrubEntriesKey: []any{map[string]any{"container": 0, "index": 1, "digest": "original-entry"}}}
			raw, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, tc.name, raw); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase,candidate_ciphertext,revision) VALUES(?,'finalizing','synthetic-sentinel',3)`, tc.name); err != nil {
				t.Fatal(err)
			}
			snap := cloneMap(current)
			snap[ConfigRevisionKey] = tc.revision
			if tc.plan {
				snap[legacyModelScrubEntriesKey] = []any{}
			} else {
				delete(snap, modelLegacyScrubPendingKey)
				delete(snap, legacyModelScrubEntriesKey)
			}
			store := NewMySQLStore(db)
			probe := &migrationFinalizerProbe{}
			store.ModelSecrets, store.LegacyConfigDir = probe, t.TempDir()
			_, err = store.openConfig(ctx, tc.name, snap)
			if !errors.Is(err, ErrConfigConflict) || probe.finalized {
				t.Errorf("outdated snapshot must be rejected before file finalization: finalized=%t error=%v", probe.finalized, err)
			}
			state, found, err := store.LegacyConfigMigrationState(ctx, tc.name)
			if err != nil || !found || state.Phase != LegacyFinalizing || state.Revision != 3 || state.CandidateEnvelope == "" {
				t.Fatal("pending state and evidence must survive stale reader")
			}
		})
	}
}

func TestLegacyConfigMigrationStateUpgradesKeylessGeneratedRows(t *testing.T) {
	raw := map[string]any{"model": "fixture-video", "createUrl": "https://test.invalid/create"}
	source := pendingVideoSource{legacyScrubEntry: legacyScrubEntry{Video: true, Provider: "personal_api"}, Raw: raw}
	old := cloneMap(source.model())
	old["tasksEndpoint"], old["resultEndpoint"] = "", ""
	file := map[string]any{"videoProviders": map[string]any{"personal_api": map[string]any{"model": "fixture-video", "createUrl": "https://test.invalid/create", "apiKey": "synthetic"}}}
	p := pendingLegacyVideos{Rows: []any{old}, Sources: []pendingVideoSource{source}}
	if !p.fileReady(file) {
		t.Error("old generated row blocked the verified original provider")
	}
	orphan := cloneMap(old)
	orphan["tasksEndpoint"] = "https://other.invalid/tasks"
	p.Rows = append(p.Rows, orphan)
	if p.fileReady(file) {
		t.Error("an orphan generated row still needs matching file evidence")
	}
}

func TestLegacyConfigMigrationStateRejectsMalformedTargetTypes(t *testing.T) {
	for _, field := range []string{"model", "createUrl", "tasksUrl", "resultUrl", "workflowId", "adapterKind", "provider"} {
		for _, side := range []string{"database", "file"} {
			t.Run(side+"-"+field, func(t *testing.T) {
				raw := map[string]any{"model": "fixture"}
				returning := cloneMap(raw)
				if field == "model" {
					delete(raw, field)
					delete(returning, field)
				}
				returning["apiKey"] = "synthetic"
				if side == "database" {
					raw[field] = 17
				} else {
					returning[field] = nil
				}
				p := pendingLegacyVideos{Sources: []pendingVideoSource{{legacyScrubEntry: legacyScrubEntry{Video: true, Provider: "personal_api"}, Raw: raw}}}
				if p.fileReady(map[string]any{"videoProviders": map[string]any{"personal_api": returning}}) {
					t.Fatal("malformed target accepted")
				}
			})
		}
	}
}
