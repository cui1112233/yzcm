package app

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"qiantie/backend/internal/novelfetchworkshop"
)

func TestNovelFetchBodyCleanupSurvivesStartupDeadline(t *testing.T) {
	t.Setenv("NOVEL_FETCH_BODY_REAPER_DISABLED", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT owner_username, book_id, version_id, revision, content_encoding, content_blob, releasable_at").WillReturnRows(sqlmock.NewRows([]string{"owner", "book", "version", "revision", "encoding", "blob", "released"}))
	mock.ExpectQuery("SELECT object_key FROM novel_fetch_body_object_retirements").WillReturnRows(sqlmock.NewRows([]string{"object_key"}))
	startup, cancelStartup := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelStartup()
	stop := startNovelFetchBodyCleanupWithTiming(startup, novelfetchworkshop.NewMySQLStore(db), time.Millisecond, time.Hour)
	defer stop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mock.ExpectationsWereMet() == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("startup deadline stopped permanent body maintenance: %v", err)
	}
}

func TestNovelFetchBodyCleanupExplicitStopCancelsRunningSQL(t *testing.T) {
	t.Setenv("NOVEL_FETCH_BODY_REAPER_DISABLED", "true")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT owner_username, book_id, version_id, revision, content_encoding, content_blob, releasable_at").WillDelayFor(5 * time.Second).WillReturnRows(sqlmock.NewRows([]string{"owner", "book", "version", "revision", "encoding", "blob", "released"}))
	stop := startNovelFetchBodyCleanupWithTiming(context.Background(), novelfetchworkshop.NewMySQLStore(db), time.Millisecond, time.Hour)
	defer stop()
	deadline := time.Now().Add(time.Second)
	for db.Stats().InUse == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if db.Stats().InUse == 0 {
		t.Fatal("maintenance did not begin SQL")
	}
	stop()
	deadline = time.Now().Add(time.Second)
	for db.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if db.Stats().InUse != 0 {
		t.Fatal("explicit maintenance stop did not cancel SQL")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
