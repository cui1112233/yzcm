package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

const raceMigrationDDL = "ALTER TABLE race_target ADD COLUMN source_updated_at_ns BIGINT UNSIGNED NOT NULL DEFAULT 0"
const raceMigrationShape = "SELECT column_type FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='race_target' AND column_name='source_updated_at_ns'"
const raceLedgerCreate = "CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT NOT NULL PRIMARY KEY, checksum CHAR(64) NOT NULL, applied_at DATETIME(6) NOT NULL) ENGINE=InnoDB"
const raceLedgerInsert = "INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (?, ?, CURRENT_TIMESTAMP(6))"

func TestMigrationDuplicateDDLReadoptsExactShapeOnlyOnce(t *testing.T) {
	for _, path := range []string{"adopt", "sql"} {
		for _, code := range []uint16{1050, 1060} {
			for _, shape := range []string{"valid", "false", "error", "true-with-error", "duplicate-again"} {
				t.Run(fmt.Sprintf("%s/%d/%s", path, code, shape), func(t *testing.T) {
					db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
					if err != nil {
						t.Fatal(err)
					}
					defer db.Close()
					duplicate := fmt.Errorf("concurrent DDL: %w", &mysql.MySQLError{Number: code, Message: "duplicate schema object"})
					shapeError := errors.New("existing column shape mismatch")
					calls := 0
					m := Migration{Version: 8800414, SQL: []string{raceMigrationDDL}}
					m.Adopt = func(ctx context.Context, tx *sql.Tx) (bool, error) {
						calls++
						if calls == 1 {
							if path == "sql" {
								return false, nil
							}
							_, err := tx.ExecContext(ctx, raceMigrationDDL)
							return false, err
						}
						if calls > 2 {
							t.Fatal("unbounded adoption retry")
						}
						var column string
						if err := tx.QueryRowContext(ctx, raceMigrationShape).Scan(&column); err != nil {
							return false, err
						}
						switch column {
						case "bigint unsigned":
							return true, nil
						case "missing":
							return false, nil
						case "bigint-with-error":
							return true, shapeError
						default:
							return false, shapeError
						}
					}
					mock.ExpectExec(raceLedgerCreate).WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE version = ?").WithArgs(m.Version).WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
					mock.ExpectBegin()
					if path == "adopt" {
						mock.ExpectExec(raceMigrationDDL).WillReturnError(duplicate)
					}
					mock.ExpectRollback()
					if path == "sql" {
						mock.ExpectBegin()
						mock.ExpectExec(raceMigrationDDL).WillReturnError(duplicate)
						mock.ExpectRollback()
					}
					mock.ExpectBegin()
					probe := mock.ExpectQuery(raceMigrationShape)
					switch shape {
					case "valid":
						probe.WillReturnRows(sqlmock.NewRows([]string{"column_type"}).AddRow("bigint unsigned"))
					case "false":
						probe.WillReturnRows(sqlmock.NewRows([]string{"column_type"}).AddRow("missing"))
					case "error":
						probe.WillReturnRows(sqlmock.NewRows([]string{"column_type"}).AddRow("varchar(10)"))
					case "true-with-error":
						probe.WillReturnRows(sqlmock.NewRows([]string{"column_type"}).AddRow("bigint-with-error"))
					default:
						probe.WillReturnError(duplicate)
					}
					if shape == "valid" {
						mock.ExpectExec(raceLedgerInsert).WithArgs(m.Version, checksumFor(m)).WillReturnResult(sqlmock.NewResult(1, 1))
						mock.ExpectCommit()
					} else {
						mock.ExpectRollback()
					}
					err = RunMigrations(context.Background(), db, []Migration{m})
					if shape == "valid" {
						if err != nil {
							t.Fatal(err)
						}
					} else {
						var original *mysql.MySQLError
						if err == nil || !errors.As(err, &original) || original.Number != code {
							t.Fatalf("lost original duplicate error: %v", err)
						}
						if (shape == "error" || shape == "true-with-error") && !errors.Is(err, shapeError) {
							t.Fatalf("lost exact-shape rejection: %v", err)
						}
						if shape == "false" && errors.Unwrap(err) != duplicate {
							t.Fatalf("false adoption must return original cause without attempting a ledger write: %v", err)
						}
					}
					if calls != 2 {
						t.Fatalf("adopt calls=%d, want exactly 2", calls)
					}
					if err := mock.ExpectationsWereMet(); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestMigrationDuplicateDDLDoesNotReadoptWithoutAuthorization(t *testing.T) {
	for _, kind := range []string{"non-duplicate", "text-only", "no-adopter", "expired"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := error(&mysql.MySQLError{Number: 1060, Message: "duplicate column"})
			if kind == "non-duplicate" {
				cause = &mysql.MySQLError{Number: 1062, Message: "duplicate key"}
			}
			if kind == "text-only" {
				cause = errors.New("Error 1060 duplicate column")
			}
			calls := 0
			m := Migration{Version: 8800414, SQL: []string{raceMigrationDDL}}
			if kind != "no-adopter" {
				m.Adopt = func(context.Context, *sql.Tx) (bool, error) {
					calls++
					if kind == "expired" {
						cancel()
					}
					return false, cause
				}
			}
			mock.ExpectExec(raceLedgerCreate).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE version = ?").WithArgs(m.Version).WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
			mock.ExpectBegin()
			if kind == "no-adopter" {
				mock.ExpectExec(raceMigrationDDL).WillReturnError(cause)
			}
			mock.ExpectRollback()
			err = RunMigrations(ctx, db, []Migration{m})
			if err == nil || !errors.Is(err, cause) {
				t.Fatalf("failure not retained: %v", err)
			}
			expected := 1
			if kind == "no-adopter" {
				expected = 0
			}
			if calls != expected {
				t.Fatalf("unauthorized adoption retry: %d", calls)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
