package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func attachmentMessageCommit(t *testing.T) AssistantAttachmentMessageCommit {
	t.Helper()
	a := attachmentSQLRecord(t)
	snapshot, e := a.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	refs := []AssistantReferenceSnapshot{snapshot}
	old := []AssistantMessage{{Role: "user", Content: "old user"}, {Role: "assistant", Content: "old reply"}}
	candidate := append(append([]AssistantMessage(nil), old...), AssistantMessage{Role: "user", Content: "read file", References: refs, ReferenceOperationID: "server-op"}, AssistantMessage{Role: "assistant", Content: "file read"})
	return AssistantAttachmentMessageCommit{OperationID: "server-op", UserContent: "read file", PreviousMessages: old, CandidateMessages: candidate, References: refs}
}
func cloneAttachmentCommit(t *testing.T, c AssistantAttachmentMessageCommit) AssistantAttachmentMessageCommit {
	t.Helper()
	raw, _ := json.Marshal(c)
	var out AssistantAttachmentMessageCommit
	if e := json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestAssistantAttachmentMessageCommitStrictBinding(t *testing.T) {
	scope := attachmentSQLScope(t)
	valid := attachmentMessageCommit(t)
	if e := ValidateAssistantAttachmentMessageCommit(scope, valid); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"prefix_content", "old_user_resigned", "old_op_reused", "assistant_op", "extra_user", "wrong_user_content", "wrong_user_refs", "missing_user", "no_reply", "empty_operation", "wrong_operation", "no_file", "duplicate_file"} {
		t.Run(name, func(t *testing.T) {
			c := cloneAttachmentCommit(t, valid)
			switch name {
			case "prefix_content":
				c.CandidateMessages[0].Content = "changed"
			case "old_user_resigned":
				c.CandidateMessages[0].ReferenceOperationID = c.OperationID
			case "old_op_reused":
				c.PreviousMessages[0].ReferenceOperationID = c.OperationID
				c.CandidateMessages[0].ReferenceOperationID = c.OperationID
			case "assistant_op":
				c.CandidateMessages[3].ReferenceOperationID = c.OperationID
			case "extra_user":
				c.CandidateMessages = append(c.CandidateMessages, AssistantMessage{Role: "user", Content: "extra"})
			case "wrong_user_content":
				c.CandidateMessages[2].Content = "different"
			case "wrong_user_refs":
				c.CandidateMessages[2].References = nil
			case "missing_user":
				c.CandidateMessages = c.CandidateMessages[:2]
			case "no_reply":
				c.CandidateMessages = c.CandidateMessages[:3]
			case "empty_operation":
				c.OperationID = ""
			case "wrong_operation":
				c.CandidateMessages[2].ReferenceOperationID = "other"
			case "no_file":
				c.References = nil
				c.CandidateMessages[2].References = nil
			case "duplicate_file":
				c.References = append(c.References, c.References[0])
				c.CandidateMessages[2].References = c.References
			}
			if e := ValidateAssistantAttachmentMessageCommit(scope, c); e == nil {
				t.Fatal("forged operation accepted")
			}
		})
	}
	weak, _ := NewAssistantReferenceScope("owner", "project", "default", "alice", "")
	if !errors.Is(ValidateAssistantAttachmentMessageCommit(weak, valid), ErrAccessDenied) {
		t.Fatal("weak send")
	}
	c := cloneAttachmentCommit(t, valid)
	c.PreviousMessages = append([]AssistantMessage(nil), valid.CandidateMessages...)
	c.OperationID = "server-op-second"
	c.CandidateMessages = append(append([]AssistantMessage(nil), c.PreviousMessages...), AssistantMessage{Role: "user", Content: c.UserContent, References: c.References, ReferenceOperationID: c.OperationID}, AssistantMessage{Role: "assistant", Content: "read again"})
	if e := ValidateAssistantAttachmentMessageCommit(scope, c); e != nil {
		t.Fatal("same file second message", e)
	}
}
func TestAssistantAttachmentScopedSaveSQLRollback(t *testing.T) {
	for _, finalFails := range []bool{false, true} {
		db, m, _ := sqlmock.New()
		r := NewMySQLStore(db).AuthoringRepository().(*MySQLAuthoringRepository)
		scope := attachmentSQLScope(t)
		a := attachmentSQLRecord(t)
		a.Status = "ready"
		a.Verified = true
		ref, _ := a.Snapshot()
		refs := []AssistantReferenceSnapshot{ref}
		st := AuthoringState{Owner: "owner", ProjectID: "project", Revision: 7, Messages: []AssistantMessage{{Role: "user", Content: "read file", References: refs, ReferenceOperationID: "server-op"}, {Role: "assistant", Content: "file read"}}}
		expectSessionScope(m, "", "", "", "", "", 0)
		m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "project", a.ID).WillReturnRows(attachmentRows(a))
		m.ExpectQuery("SELECT operation_id FROM screenplay_assistant_text_attachment_messages").WithArgs("owner", "project", "default", "server-op").WillReturnError(sql.ErrNoRows)
		m.ExpectExec("INSERT INTO screenplay_assistant_text_attachment_messages").WithArgs("owner", "project", "default", "server-op", a.ID, ref.Revision, ref.Digest, "alice", "actual").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET first_published_at").WithArgs("owner", "project", a.ID, a.StateRevision).WillReturnResult(sqlmock.NewResult(0, 1))
		q := m.ExpectExec("UPDATE screenplay_authoring SET body")
		if finalFails {
			q.WillReturnError(errors.New("forced final failure"))
			m.ExpectRollback()
		} else {
			q.WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
		}
		calls := 0
		out, e := r.SaveAssistantReferencesScoped(context.Background(), st, scope, 7, "server-op", "read file", refs, func(context.Context, *sql.Tx) error { calls++; return nil })
		if (e != nil) != finalFails || calls != 1 || !finalFails && out.Revision != 8 {
			t.Fatal(out, e, calls)
		}
		if e := m.ExpectationsWereMet(); e != nil {
			t.Fatal(e)
		}
		db.Close()
	}
}
func TestAssistantAttachmentScopedSaveOriginalGuardFirst(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	r := NewMySQLStore(db).AuthoringRepository().(*MySQLAuthoringRepository)
	c := attachmentMessageCommit(t)
	st := AuthoringState{Owner: "owner", ProjectID: "project", Revision: 7, Messages: c.CandidateMessages}
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectRollback()
	if _, e := r.SaveAssistantReferencesScoped(context.Background(), st, attachmentSQLScope(t), 7, c.OperationID, c.UserContent, c.References, func(context.Context, *sql.Tx) error { return ErrAccessDenied }); !errors.Is(e, ErrAccessDenied) {
		t.Fatal(e)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
