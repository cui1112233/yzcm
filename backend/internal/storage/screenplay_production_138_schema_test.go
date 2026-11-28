package storage

import (
	"context"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

const production138ColumnProbe = `SELECT table_name,column_name,data_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema=DATABASE() AND ((table_name='screenplay_batches' AND column_name='production_authorization') OR (table_name='screenplay_batch_episodes' AND column_name='storyboard_generation'))`

func production138Migration(t *testing.T) Migration {
	t.Helper()
	for _, m := range ScreenplayMigrations() {
		if m.Version == 8800124 {
			return m
		}
	}
	t.Fatal("missing 138 production migration 8800124")
	return Migration{}
}
func production138Columns(count int) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"table_name", "column_name", "data_type", "is_nullable", "column_default"})
	if count > 0 {
		rows.AddRow("screenplay_batches", "production_authorization", "json", "YES", nil)
	}
	if count > 1 {
		rows.AddRow("screenplay_batch_episodes", "storyboard_generation", "json", "YES", nil)
	}
	return rows
}
func TestProduction138MigrationResumesAndVerifiesExactColumns(t *testing.T) {
	for completed := 0; completed <= 2; completed++ {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			m := production138Migration(t)
			if m.Adopt == nil || m.Reconcile == nil || len(m.SQL) != 2 {
				t.Fatal("missing safe schema adoption")
			}
			db, mock, e := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(production138ColumnProbe).WillReturnRows(production138Columns(completed))
			for i := completed; i < 2; i++ {
				mock.ExpectExec(m.SQL[i]).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectQuery(production138ColumnProbe).WillReturnRows(production138Columns(2))
			mock.ExpectCommit()
			tx, _ := db.Begin()
			adopted, e := m.Adopt(context.Background(), tx)
			if e != nil || !adopted {
				t.Fatalf("adopt %v %v", adopted, e)
			}
			if e = tx.Commit(); e != nil {
				t.Fatal(e)
			}
			mock.ExpectBegin()
			mock.ExpectQuery(production138ColumnProbe).WillReturnRows(production138Columns(2))
			mock.ExpectCommit()
			tx, _ = db.Begin()
			if e = m.Reconcile(context.Background(), tx); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(); e != nil {
				t.Fatal(e)
			}
			if e = mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestProduction138MigrationRejectsWrongTypeNullabilityOrDefaultBeforeDDL(t *testing.T) {
	for _, value := range []struct {
		kind, nullable string
		def            any
	}{{"longtext", "YES", nil}, {"json", "NO", nil}, {"json", "YES", "{}"}} {
		t.Run(value.kind+value.nullable, func(t *testing.T) {
			m := production138Migration(t)
			db, mock, e := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectQuery(production138ColumnProbe).WillReturnRows(sqlmock.NewRows([]string{"table_name", "column_name", "data_type", "is_nullable", "column_default"}).AddRow("screenplay_batches", "production_authorization", value.kind, value.nullable, value.def))
			mock.ExpectRollback()
			tx, _ := db.Begin()
			if _, e = m.Adopt(context.Background(), tx); e == nil {
				t.Fatal("invalid existing column adopted")
			}
			_ = tx.Rollback()
			if e = mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestProduction138MigrationDoesNotIgnoreAlterFailure(t *testing.T) {
	m := production138Migration(t)
	db, mock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(production138ColumnProbe).WillReturnRows(production138Columns(1))
	mock.ExpectExec(m.SQL[1]).WillReturnError(fmt.Errorf("ALTER refused"))
	mock.ExpectRollback()
	tx, _ := db.Begin()
	if _, e := m.Adopt(context.Background(), tx); e == nil {
		t.Fatal("ALTER failure ignored")
	}
	_ = tx.Rollback()
	if e := mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestProduction138MigrationResumesWhenOnlyEpisodeColumnExists(t *testing.T) {
	m := production138Migration(t)
	db, mock, _ := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(production138ColumnProbe).WillReturnRows(sqlmock.NewRows([]string{"table_name", "column_name", "data_type", "is_nullable", "column_default"}).AddRow("screenplay_batch_episodes", "storyboard_generation", "json", "YES", nil))
	mock.ExpectExec(m.SQL[0]).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(production138ColumnProbe).WillReturnRows(production138Columns(2))
	mock.ExpectCommit()
	tx, _ := db.Begin()
	if ok, err := m.Adopt(context.Background(), tx); err != nil || !ok {
		t.Fatal("reverse partial migration failed", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
