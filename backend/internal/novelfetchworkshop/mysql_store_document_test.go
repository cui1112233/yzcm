package novelfetchworkshop

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLStorePutDocumentConvertsJSONParameterToUTF8(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT updated_at FROM novel_fetch_workshop_documents").WithArgs("alice", "book-1").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(canonicalBodyRefsSQL)).WithArgs("alice", "book-1").WillReturnRows(sqlmock.NewRows([]string{"version", "revision", "hash", "chars", "state", "updated"}))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO novel_fetch_workshop_documents(owner_username,book_id,document_json,created_at,updated_at) VALUES(?,?,CAST(? AS CHAR CHARACTER SET utf8mb4),CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE document_json=VALUES(document_json), updated_at=GREATEST(CURRENT_TIMESTAMP(6), TIMESTAMPADD(MICROSECOND,1,updated_at))`)).
		WithArgs("alice", "book-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()
	if err := store.PutDocument(context.Background(), "alice", Document{BookID: "book-1", Original: "中文模型输出"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type documentSQLArgument func(Document) bool

func (f documentSQLArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var document Document
	if json.Unmarshal([]byte(raw), &document) != nil {
		return false
	}
	return f(document)
}

func TestMySQLDocumentWriteUsesSavedBodyMetadata(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	now := time.Now().UTC()
	old := Document{BookID: "123", Original: "obsolete inline", ProcessedChars: 4, Versions: map[string]any{"ai2": "untouched legacy"}, Meta: map[string]any{"status": "done"}}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT updated_at FROM novel_fetch_workshop_documents").WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectQuery(regexp.QuoteMeta(canonicalBodyRefsSQL)).WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"version", "revision", "hash", "chars", "state", "updated"}).AddRow("original", uint64(7), "actual-hash", int64(13), "ready", now))
	mock.ExpectExec("INSERT INTO novel_fetch_workshop_documents").WithArgs("alice", "123", documentSQLArgument(func(d Document) bool {
		return d.Original == "" && d.ProcessedChars == 13 && d.BodyRefs["original"].Revision == 7 && d.BodyRefs["original"].ContentHash == "actual-hash" && d.Versions["ai2"] == "untouched legacy" && d.Meta["status"] == "done"
	})).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.PutDocument(context.Background(), "alice", old); err != nil {
		t.Fatal(err)
	}
	assertBodyExpectations(t, mock)
}

func TestMySQLDocumentWriteRefReadFailureDoesNotReplaceTask(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT updated_at FROM novel_fetch_workshop_documents").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(canonicalBodyRefsSQL)).WillReturnError(context.DeadlineExceeded)
	mock.ExpectRollback()
	if err := s.PutDocument(context.Background(), "alice", Document{BookID: "123", Original: "stale"}); err == nil {
		t.Fatal("failed canonical ref read allowed task replacement")
	}
	assertBodyExpectations(t, mock)
}

func TestLegacyMigrationCannotReplaceExistingBodyReferenceWithInlineCandidate(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	now := time.Now().UTC()
	old := Document{BookID: "123", Original: "old"}
	raw, _ := json.Marshal(old)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT document_json FROM novel_fetch_workshop_documents").WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"document_json"}).AddRow(raw))
	mock.ExpectExec("INSERT IGNORE INTO novel_fetch_workshop_bodies").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(canonicalBodyRefsSQL)).WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"version", "revision", "hash", "chars", "state", "updated"}).AddRow("original", uint64(9), "actual-hash", int64(13), "ready", now))
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=CAST").WithArgs(documentSQLArgument(func(d Document) bool {
		return d.Original == "" && d.ProcessedChars == 13 && d.BodyRefs["original"].Revision == 9 && d.BodyRefs["original"].ContentHash == "actual-hash"
	}), "alice", "123").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.MigrateLegacyDocumentBodies(context.Background(), "alice", "123"); err != nil {
		t.Fatal(err)
	}
	assertBodyExpectations(t, mock)
}

func TestMySQLStaleDocumentCannotResurrectDeletedBody(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	now := time.Now().UTC()
	old := Document{BookID: "123", Original: "deleted original", ProcessedText: "deleted original", ProcessedChars: 16, BodyRefs: map[string]BodyRef{"original": {VersionID: "original", Revision: 4, ContentHash: "previous"}}, Versions: map[string]any{"ai2": "initial legacy text must survive"}}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT updated_at FROM novel_fetch_workshop_documents").WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectQuery(regexp.QuoteMeta(canonicalBodyRefsSQL)).WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"version", "revision", "hash", "chars", "state", "updated"}))
	mock.ExpectExec("INSERT INTO novel_fetch_workshop_documents").WithArgs("alice", "123", documentSQLArgument(func(d Document) bool {
		_, exists := d.BodyRefs["original"]
		return !exists && d.Original == "" && d.ProcessedText == "" && d.ProcessedChars == 0 && d.Versions["ai2"] == "initial legacy text must survive"
	})).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.PutDocument(context.Background(), "alice", old); err != nil {
		t.Fatal(err)
	}
	assertBodyExpectations(t, mock)
}
