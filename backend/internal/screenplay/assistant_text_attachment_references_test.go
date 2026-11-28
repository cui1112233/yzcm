package screenplay

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestAssistantTextAttachmentScopedResolve(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	a := attachmentSQLRecord(t)
	a.Status = "ready"
	a.Verified = true
	snapshot, _ := a.Snapshot()
	view := AssistantReferenceView{Project: Project{ID: "project", OwnerUsername: "owner", SourceText: "old source"}}
	old, _ := view.Describe(AssistantReferenceSelector{Kind: "project_source", ID: "project"})
	g := func(context.Context, *sql.Tx) error { return nil }
	if _, e := ResolveAssistantReferencesScoped(context.Background(), view, r, AssistantReferenceScope{}, []AssistantReferenceSelector{snapshot.Selector()}, g); !errors.Is(e, ErrAccessDenied) {
		t.Fatal(e)
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", a.ID).WillReturnRows(attachmentRows(a))
	m.ExpectCommit()
	refs, e := ResolveAssistantReferencesScoped(context.Background(), view, r, s, []AssistantReferenceSelector{old.Selector(), snapshot.Selector()}, g)
	if e != nil || len(refs) != 2 || refs[1].Text != a.Text {
		t.Fatal(refs, e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestAssistantTextAttachmentReferenceDirectory(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	a := attachmentSQLRecord(t)
	a.Status = "ready"
	a.Verified = true
	g := func(context.Context, *sql.Tx) error { return nil }
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT COUNT").WithArgs("owner", "project", "default", "alice", "actual").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(1))
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", "default", "alice", "actual", 100, 0).WillReturnRows(attachmentRows(a))
	m.ExpectCommit()
	refs, total, e := r.ListReferenceDirectory(context.Background(), s, 0, 100, g)
	if e != nil || total != 1 || len(refs) != 1 || refs[0].Text != "" || refs[0].Digest == "" {
		t.Fatal(refs, total, e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
