package screenplay

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
)

func attachmentSQLScope(t *testing.T) AssistantReferenceScope {
	t.Helper()
	s, e := NewAssistantReferenceScope("owner", "project", "default", "alice", "actual")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func attachmentSQLRecord(t *testing.T) AssistantTextAttachment {
	t.Helper()
	a, e := PrepareAssistantTextAttachment(attachmentSQLScope(t), "attachment1", "x.md", strings.Repeat("a", 64), []byte("# 真正文\r\n"))
	if e != nil {
		t.Fatal(e)
	}
	a.RequestKeyHash = contentDigest("request")
	a.ObjectKey = "assistant-text/attachment1"
	return a
}
func attachmentRows(a AssistantTextAttachment) *sqlmock.Rows {
	b, _ := json.Marshal(a)
	return sqlmock.NewRows([]string{"body", "state_revision", "status", "verified", "published", "work_token"}).AddRow(b, a.StateRevision, a.Status, a.Verified, a.Published, "")
}
func TestAssistantTextAttachmentIntentSQL(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	a := attachmentSQLRecord(t)
	guardCalls := 0
	guard := func(context.Context, *sql.Tx) error { guardCalls++; return nil }
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("alice", "actual", a.RequestKeyHash).WillReturnError(sql.ErrNoRows)
	m.ExpectExec("INSERT INTO screenplay_assistant_text_attachments").WithArgs("owner", "project", a.ID, "default", "alice", "actual", a.RequestKeyHash, a.ObjectKey, a.Namespace, a.ContentVersion, attachmentJSONArgument{}, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	got, token, e := r.BeginIntent(context.Background(), s, a, guard)
	if e != nil || got.ID != a.ID || token == "" || guardCalls != 1 {
		t.Fatal(got, token, e, guardCalls)
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("alice", "actual", a.RequestKeyHash).WillReturnRows(attachmentRows(a))
	m.ExpectCommit()
	got, token, e = r.BeginIntent(context.Background(), s, a, guard)
	if e != nil || got.ID != a.ID || token != "" {
		t.Fatal(got, token, e)
	}
	altered := a
	altered.Session = "other"
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentRows(altered))
	m.ExpectRollback()
	if _, _, e = r.BeginIntent(context.Background(), s, a, guard); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("cross scope key", e)
	}
	if _, _, e = r.BeginIntent(context.Background(), s, a, nil); !errors.Is(e, ErrAccessDenied) {
		t.Fatal("missing guard", e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestAssistantTextAttachmentReadySQL(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	a := attachmentSQLRecord(t)
	guard := func(context.Context, *sql.Tx) error { return nil }
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentRows(a))
	m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET status='ready'").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectRollback()
	if e := r.MarkReady(context.Background(), s, a.ID, "old-token", a.RawSHA256, guard); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal(e)
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentRows(a))
	m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET status='ready'").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if e := r.MarkReady(context.Background(), s, a.ID, "token", a.RawSHA256, guard); e != nil {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if e := r.MarkReady(cancelled, s, a.ID, "token", a.RawSHA256, guard); e == nil {
		t.Fatal("cancelled ready")
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

type attachmentJSONArgument struct{}

func (attachmentJSONArgument) Match(v driver.Value) bool { _, ok := v.(string); return ok }
func TestAssistantTextAttachmentReadAbsence(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	guard := func(context.Context, *sql.Tx) error { return nil }
	for _, missing := range []bool{true, false} {
		expectSessionScope(m, "", "", "", "", "", 0)
		q := m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", "guess")
		if missing {
			q.WillReturnError(sql.ErrNoRows)
		} else {
			a := attachmentSQLRecord(t)
			a.Uploader = "other"
			a.Status = "ready"
			a.Verified = true
			q.WillReturnRows(attachmentRows(a))
		}
		m.ExpectRollback()
		if _, e := r.GetVisible(context.Background(), s, "guess", guard); !errors.Is(e, ErrNotFound) {
			t.Fatal("absence differs", e)
		}
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("alice", "actual", contentDigest("request")).WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if _, e := r.InspectIntent(context.Background(), s, contentDigest("request"), guard); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestAssistantTextAttachmentListAndRetrySQL(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	a := attachmentSQLRecord(t)
	guard := func(context.Context, *sql.Tx) error { return nil }
	a.Status = "ready"
	a.Verified = true
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", "default", "alice", "actual", 100, 0).WillReturnRows(attachmentRows(a))
	m.ExpectCommit()
	items, e := r.ListVisible(context.Background(), s, 0, 100, guard)
	if e != nil || len(items) != 1 || items[0].Text != "" {
		t.Fatal(items, e)
	}
	a.Status = "preparing"
	a.Verified = false
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentRows(a))
	m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET status='preparing'").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectRollback()
	if _, _, e = r.ClaimRetry(context.Background(), s, a.ID, a.Namespace, a.StateRevision, guard); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("active lease reclaimed", e)
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentRows(a))
	m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET status='preparing'").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	got, token, e := r.ClaimRetry(context.Background(), s, a.ID, a.Namespace, a.StateRevision, guard)
	if e != nil || got.ID != a.ID || got.ObjectKey != a.ObjectKey || token == "" || got.StateRevision != a.StateRevision+1 {
		t.Fatal(got, token, e)
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentRows(a))
	m.ExpectRollback()
	if _, _, e = r.ClaimRetry(context.Background(), s, a.ID, strings.Repeat("b", 64), a.StateRevision, guard); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("namespace retry", e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
