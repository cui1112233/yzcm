package novelfetchworkshop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

const configOwnerSnapshotSQL = `SELECT c.settings_json,EXISTS(
SELECT 1 FROM novel_fetch_legacy_config_migrations m WHERE m.owner_username=? AND m.phase!='complete')
FROM novel_fetch_workshop_configs c WHERE c.owner_username=?`

func TestMySQLGetConfigBindsOwnerIndependentlyInSameSnapshot(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery(configOwnerSnapshotSQL).WithArgs("alice", "alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json", "pending"}).AddRow(`{"label":"owner-specific"}`, pending))
			settings, err := NewMySQLStore(db).GetConfig(context.Background(), "alice")
			if pending {
				if err == nil || !strings.Contains(err.Error(), "requires model configuration encryption") {
					t.Fatalf("pending owner migration was ignored: %v", err)
				}
			} else if err != nil || settings["label"] != "owner-specific" {
				t.Fatalf("owner snapshot failed: settings=%v err=%v", settings, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Explicit isolated MySQL acceptance only. The supplied database is never
// modified: tables and rows exist exclusively in a uniquely created test DB.
func TestBodySafetyMySQLGetConfigMixedCollations(t *testing.T) {
	dsn := os.Getenv("QIANTIE_BODY_SAFETY_TEST_DSN")
	if dsn == "" {
		t.Skip("explicit isolated MySQL DSN not configured")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "nfw_body_safety_") {
		t.Fatal("requires an isolated nfw_body_safety_ database")
	}
	cfg.ParseTime = true
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("open isolated database failed")
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var selected string
	if err := admin.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&selected); err != nil || selected != cfg.DBName || !strings.HasPrefix(selected, "nfw_body_safety_") {
		t.Fatal("refusing non-test database")
	}
	name := fmt.Sprintf("nfw_body_safety_collation_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create unique collation fixture: %v", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Minute)
		defer stop()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE `"+name+"`"); err != nil {
			t.Errorf("remove unique collation fixture: %v", err)
		}
	}()
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("open unique collation fixture failed")
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE novel_fetch_workshop_configs (owner_username VARCHAR(191) NOT NULL PRIMARY KEY,settings_json JSON NOT NULL) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`,
		`CREATE TABLE novel_fetch_legacy_config_migrations (owner_username VARCHAR(191) NOT NULL PRIMARY KEY,phase VARCHAR(32) NOT NULL) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create mixed collation tables: %v", err)
		}
	}
	for _, owner := range []string{"alice", "bob"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json) VALUES(?,?)`, owner, `{"label":"mixed-collation-safe"}`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES('alice','complete'),('bob','awaiting_source')`); err != nil {
		t.Fatal(err)
	}
	// Reproduce the actual old query failure against these same tables before
	// asserting the new query, rather than merely checking a query string.
	var raw []byte
	var pending bool
	oldErr := db.QueryRowContext(ctx, `SELECT c.settings_json,EXISTS(SELECT 1 FROM novel_fetch_legacy_config_migrations m WHERE m.owner_username=c.owner_username AND m.phase!='complete') FROM novel_fetch_workshop_configs c WHERE c.owner_username=?`, "alice").Scan(&raw, &pending)
	var mysqlErr *mysql.MySQLError
	if !errors.As(oldErr, &mysqlErr) || mysqlErr.Number != 1267 {
		t.Fatalf("fixture did not reproduce old mixed-collation error 1267: %v", oldErr)
	}
	store := NewMySQLStore(db)
	settings, err := store.GetConfig(ctx, "alice")
	if err != nil || settings["label"] != "mixed-collation-safe" {
		t.Fatalf("safe owner snapshot failed: %v", err)
	}
	if _, err := store.GetConfig(ctx, "bob"); err == nil || !strings.Contains(err.Error(), "requires model configuration encryption") {
		t.Fatalf("pending owner protection changed: %v", err)
	}
}
