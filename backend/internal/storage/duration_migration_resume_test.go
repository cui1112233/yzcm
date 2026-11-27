package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

const durationColumnProbe = `SELECT column_name,column_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name IN ('target_duration_seconds','requested_duration_seconds','actual_duration_seconds')`

func TestDurationMigrationKeepsHistoricalChecksums(t *testing.T) {
	for version, want := range map[int]string{
		1100017: "702d151e2ed6317ebff5920a6988a37a9e2ed082b185e0cd6d52dff251508bfb",
		1100018: "cdab1946509745fa0e66a1114b73cd01c2638464741ad49b77a126cd2b4b983a",
	} {
		if got := checksumFor(durationMigration(t, version)); got != want {
			t.Fatalf("historical migration %d checksum changed: %s", version, got)
		}
	}
}

func durationMigration(t *testing.T, version int) Migration {
	t.Helper()
	for _, m := range V11Migrations() {
		if m.Version == version {
			return m
		}
	}
	t.Fatalf("missing duration migration %d", version)
	return Migration{}
}

func TestBodySafetyMySQLDurationResume(t *testing.T) {
	dsn := os.Getenv("QIANTIE_BODY_SAFETY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL DSN not configured")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "nfw_body_safety_") {
		t.Fatal("requires isolated nfw_body_safety_ database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, version := range []int{1100017, 1100018} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			m := durationMigration(t, version)
			table := fmt.Sprintf("duration_resume_%d", time.Now().UnixNano())
			baseColumn, oldTable := "downgraded_asset_ids", "batch_factory_v11_production_tasks"
			if version == 1100018 {
				baseColumn, oldTable = "media_url", "batch_factory_v11_merge_sources"
			}
			m.Version = int(time.Now().UnixNano())
			m.SQL = append([]string(nil), m.SQL...)
			for i := range m.SQL {
				m.SQL[i] = strings.ReplaceAll(m.SQL[i], oldTable, table)
			}
			m.Adopt = adoptDurationColumns(table, m.SQL)
			defer func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 2*time.Minute)
				defer stop()
				if _, err := db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+table); err != nil {
					t.Error(err)
				}
				if _, err := db.ExecContext(cleanupCtx, "DELETE FROM schema_migrations WHERE version=?", m.Version); err != nil {
					t.Error(err)
				}
			}()
			if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY,"+baseColumn+" TEXT NULL)"); err != nil {
				t.Fatal(err)
			}
			// Simulate the first ALTER having committed before the old process died.
			if _, err := db.ExecContext(ctx, m.SQL[0]); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := RunMigrations(ctx, db, []Migration{m}); err != nil {
					t.Fatal(err)
				}
			}
			var columns int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name IN ('target_duration_seconds','requested_duration_seconds','actual_duration_seconds')", table).Scan(&columns); err != nil || columns != 3 {
				t.Fatalf("columns=%d error=%v", columns, err)
			}
		})
	}
}

func TestDurationMigrationResumesPartialDDL(t *testing.T) {
	for _, version := range []int{1100017, 1100018} {
		for completed := 1; completed <= 3; completed++ {
			t.Run(fmt.Sprintf("%d/%d-columns", version, completed), func(t *testing.T) {
				m := durationMigration(t, version)
				table := "batch_factory_v11_production_tasks"
				if version == 1100018 {
					table = "batch_factory_v11_merge_sources"
				}
				db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT NOT NULL PRIMARY KEY, checksum CHAR(64) NOT NULL, applied_at DATETIME(6) NOT NULL) ENGINE=InnoDB`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery(`SELECT checksum FROM schema_migrations WHERE version = ?`).WithArgs(version).WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"column_name", "column_type", "is_nullable", "column_default"})
				for _, name := range []string{"target_duration_seconds", "requested_duration_seconds", "actual_duration_seconds"}[:completed] {
					rows.AddRow(name, "decimal(10,3)", "NO", "0.000")
				}
				mock.ExpectQuery(durationColumnProbe).WithArgs(table).WillReturnRows(rows)
				for _, statement := range m.SQL[completed:] {
					mock.ExpectExec(statement).WillReturnResult(sqlmock.NewResult(0, 0))
				}
				mock.ExpectExec(`INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (?, ?, CURRENT_TIMESTAMP(6))`).WithArgs(version, checksumFor(m)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
				if err := RunMigrations(context.Background(), db, []Migration{m}); err != nil {
					t.Fatalf("partial DDL must resume without replaying existing columns: %v", err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDurationMigrationRejectsConflictingColumn(t *testing.T) {
	m := durationMigration(t, 1100017)
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(durationColumnProbe).WithArgs("batch_factory_v11_production_tasks").WillReturnRows(sqlmock.NewRows([]string{"column_name", "column_type", "is_nullable", "column_default"}).AddRow("target_duration_seconds", "varchar(32)", "NO", "0"))
	mock.ExpectRollback()
	if m.Adopt == nil {
		t.Fatal("missing safe partial duration adoption")
	}
	adopted, err := m.Adopt(context.Background(), tx)
	_ = tx.Rollback()
	if adopted || err == nil || !strings.Contains(err.Error(), "target_duration_seconds") {
		t.Fatalf("conflict incorrectly accepted: adopted=%v err=%v", adopted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
