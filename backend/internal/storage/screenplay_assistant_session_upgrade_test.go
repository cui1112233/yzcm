package storage

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestSessionUpgradeOriginalAndRetry(t *testing.T) {
	for _, state := range []string{"original", "after_add_before_drop", "completed"} {
		db, m, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		m.ExpectBegin()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		deadline := sqlmock.NewRows([]string{"column_type", "is_nullable"})
		fk := 1
		if state == "completed" {
			fk = 0
		}
		if state != "original" {
			deadline.AddRow("datetime(6)", "YES")
		}
		m.ExpectQuery("SELECT column_type").WillReturnRows(deadline)
		m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(fk))
		if state == "original" {
			m.ExpectExec("ALTER TABLE screenplay_assistant_sessions ADD COLUMN").WillReturnResult(sqlmock.NewResult(0, 0))
		}
		if fk == 1 {
			m.ExpectExec("ALTER TABLE screenplay_assistant_sessions DROP FOREIGN KEY").WillReturnResult(sqlmock.NewResult(0, 0))
		}
		m.ExpectExec("UPDATE screenplay_assistant_sessions SET request_status='unknown'").WillReturnResult(sqlmock.NewResult(0, 0))
		m.ExpectExec("CREATE TABLE IF NOT EXISTS screenplay_assistant_request_history").WillReturnResult(sqlmock.NewResult(0, 0))
		m.ExpectCommit()
		ok, err := screenplayAssistantSessionUpgrade().Adopt(context.Background(), tx)
		if !ok || err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestSessionBaseChecksumCompatibility(t *testing.T) {
	migration := screenplayAssistantSessionMigration()
	if checksumFor(migration) != "f3169d7cd3c357860698d7c400b2a5b01a8a22f76c72f97c807ca24333f99906" {
		t.Fatal(checksumFor(migration))
	}
	if !checksumMatches(migration, "eef36b6dca1b92299cf598f0db544a065060318917d1f3a8d55c438b32c51da6", checksumFor(migration)) {
		t.Fatal("intermediate migration checksum rejected")
	}
}
