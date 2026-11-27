package novelfetchworkshop

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newBodySQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *MySQLStore) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock, NewMySQLStore(db)
}

func TestMySQLStorePutBody(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(lockBodySQL)).WithArgs("alice", "123", "ai3").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO novel_fetch_workshop_bodies").WithArgs("alice", "123", "ai3", uint64(1), "gzip", sqlmock.AnyArg(), sqlmock.AnyArg(), int64(2), "ready", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	expectBodyMetadataWrite(mock, "alice", "123")
	mock.ExpectCommit()

	ref, err := store.PutBody(ctx, "alice", BodyRecord{
		BookID:  "123",
		BodyRef: BodyRef{VersionID: "ai3", State: "ready"},
		Content: "正文",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.VersionID != "ai3" || ref.Revision != 1 || ref.CharCount != 2 || ref.State != "ready" {
		t.Fatalf("ref=%+v", ref)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreGetBody(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	ctx := context.Background()
	blob, hash, chars, err := encodeBody("正文内容")
	if err != nil {
		t.Fatal(err)
	}
	updated := time.Date(2026, 9, 2, 13, 1, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT revision, content_encoding, content_blob, content_hash, char_count, state, updated_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? AND version_id=?`)).WithArgs("alice", "123", "ai3").WillReturnRows(sqlmock.NewRows([]string{"revision", "content_encoding", "content_blob", "content_hash", "char_count", "state", "updated_at"}).AddRow(uint64(3), "gzip", blob, hash, chars, "ready", updated))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE novel_fetch_workshop_bodies SET last_needed_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND book_id=? AND version_id=?`)).WithArgs("alice", "123", "ai3").WillReturnResult(sqlmock.NewResult(0, 1))

	body, err := store.GetBody(ctx, "alice", "123", "ai3")
	if err != nil {
		t.Fatal(err)
	}
	if body.BookID != "123" || body.VersionID != "ai3" || body.Content != "正文内容" || body.Revision != 3 || body.ContentHash != hash {
		t.Fatalf("body=%+v", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreGetBodyNotFound(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT revision, content_encoding, content_blob, content_hash, char_count, state, updated_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? AND version_id=?`)).WithArgs("alice", "123", "missing").WillReturnError(sql.ErrNoRows)

	_, err := store.GetBody(context.Background(), "alice", "123", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreListBodyRefs(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	updated := time.Date(2026, 9, 2, 13, 2, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"version_id", "revision", "content_hash", "char_count", "state", "updated_at"}).AddRow("ai1", uint64(1), "h1", int64(10), "ready", updated).AddRow("ai3", uint64(2), "h3", int64(12), "ready", updated)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT version_id, revision, content_hash, char_count, state, updated_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? ORDER BY version_id ASC`)).WithArgs("alice", "123").WillReturnRows(rows)

	refs, err := store.ListBodyRefs(context.Background(), "alice", "123")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].VersionID != "ai1" || refs[1].VersionID != "ai3" || refs[1].Revision != 2 {
		t.Fatalf("refs=%+v", refs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreDeleteBody(t *testing.T) {
	_, mock, store := newBodySQLMock(t)

	mock.ExpectBegin()
	expectLockedBody(mock, "alice", "123", "ai3", 1, "gzip", []byte("old"), "ready", nil, nil)
	expectBodyRemoval(mock, "alice", "123", "ai3", 1, "gzip", []byte("old"))
	mock.ExpectCommit()

	deleted, err := store.DeleteBody(context.Background(), "alice", "123", "ai3")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected body deletion")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
