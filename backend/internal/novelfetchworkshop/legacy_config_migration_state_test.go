package novelfetchworkshop

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/storage"
)

func TestLegacyConfigMigrationStateRevisionCAS(t *testing.T) {
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
	defer admin.Close()
	name := fmt.Sprintf("legacy_state_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
	}()
	cfg.DBName, cfg.ParseTime = name, true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
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
