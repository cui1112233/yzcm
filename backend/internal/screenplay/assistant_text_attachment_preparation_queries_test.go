package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func preparationRows(a AssistantTextAttachment) *sqlmock.Rows {
	b, _ := json.Marshal(a)
	return sqlmock.NewRows([]string{"body", "revision", "status", "verified", "published", "token", "deadline"}).AddRow(string(b), a.StateRevision, a.Status, a.Verified, a.Published, "", nil)
}
func TestAssistantTextAttachmentPreparationIntentRead(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AssistantTextAttachments()
	s := attachmentSQLScope(t)
	a := attachmentSQLRecord(t)
	g := func(context.Context, *sql.Tx) error { return nil }
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", "default", "alice", "actual", 101, 0).WillReturnRows(preparationRows(a))
	m.ExpectCommit()
	items, e := r.ListPreparationEntries(context.Background(), s, 0, 100, g)
	if e != nil || len(items) != 1 || items[0].Attachment.Status != "preparing" {
		t.Fatal(items, e)
	}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", a.ID).WillReturnRows(preparationRows(a))
	m.ExpectCommit()
	v, e := r.InspectOwnIntentByID(context.Background(), s, a.ID, g)
	if e != nil || v.Attachment.ID != a.ID {
		t.Fatal(v, e)
	}
	weak, _ := NewAssistantReferenceScope("owner", "project", "default", "alice", "")
	if _, e = r.InspectOwnIntentByID(context.Background(), weak, a.ID, g); !errors.Is(e, ErrNotFound) {
		t.Fatal("weak draft", e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
