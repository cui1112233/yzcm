package screenplay

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func expectSessionScope(m sqlmock.Sqlmock, id, parent, creator, visibility, status string, rev int64) {
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(`{"members":[{"username":"alice","read":true,"edit":true,"generate":true}]}`, 7))
	rows := sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "revision", "archived", "request_status", "request_actor"})
	if id != "" {
		rows.AddRow(id, parent, creator, visibility, "confirm", "标题", rev, false, status, "alice")
	}
	m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("owner", "project").WillReturnRows(rows)
}

func TestSessionRepositoryDefaultParentAndPrivateDetail(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := MySQLAuthoringRepository{db: db}
	ctx := context.Background()
	expectSessionScope(m, "", "", "", "", "", 0)
	m.ExpectExec("INSERT INTO screenplay_assistant_sessions").WithArgs("owner", "project", sqlmock.AnyArg(), "default", "alice", "private", "confirm", "标题").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	item, err := r.CreateAssistantSession(ctx, "owner", "project", "alice", SessionCreate{ParentID: "default", Title: "标题"})
	if err != nil || item.ID == "default" || item.Visibility != "private" {
		t.Fatal(item, err)
	}
	expectSessionScope(m, item.ID, "default", "alice", "private", "", 1)
	m.ExpectRollback()
	if _, _, err = r.GetAssistantSession(ctx, "owner", "project", "owner", item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionReservationAndLateCompletion(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := MySQLAuthoringRepository{db: db}
	ctx := context.Background()
	expectSessionScope(m, "s", "default", "alice", "project", "", 1)
	m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("owner", "project", "s").WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(`{"messages":[{"role":"user","content":"selected session only"}]}`))
	m.ExpectExec("UPDATE screenplay_assistant_sessions SET request_actor=").WithArgs("alice", sqlmock.AnyArg(), "owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	req, err := r.BeginAssistantRequest(ctx, "owner", "project", "alice", "s", 1)
	if err != nil || req.Token == "" || req.Revision != 2 || req.Mode != "confirm" || len(req.History.Messages) != 1 || req.History.Messages[0].Content != "selected session only" {
		t.Fatal(req, err)
	}
	expectSessionScope(m, "s", "default", "alice", "project", "running", 2)
	m.ExpectRollback()
	if _, err = r.BeginAssistantRequest(ctx, "owner", "project", "alice", "s", 2); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	expectSessionScope(m, "s", "default", "alice", "project", "unknown", 2)
	m.ExpectRollback()
	if err = r.FinishAssistantRequest(ctx, req, &AssistantSessionHistory{}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	expectSessionScope(m, "s", "default", "alice", "project", "", 3)
	m.ExpectRollback()
	if err = r.FinishAssistantRequest(ctx, req, &AssistantSessionHistory{}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRevocationRollsBackGrantWhenTokenWriteFails(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := MySQLAuthoringRepository{db: db}
	st := AuthoringState{Owner: "owner", ProjectID: "project"}
	before := st
	before.Members = []MemberGrant{{Username: "alice", Read: true, Generate: true}}
	actors := revokedSessionActors(before, st)
	if len(actors) != 1 || actors[0] != "alice" {
		t.Fatal(actors)
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	m.ExpectExec("UPDATE screenplay_authoring SET body=").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO screenplay_assistant_request_history").WithArgs("unknown", "revoked", "owner", "project", "alice").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE screenplay_assistant_sessions SET request_actor=NULL").WithArgs("owner", "project", "alice").WillReturnError(sql.ErrConnDone)
	m.ExpectRollback()
	if _, err = r.saveAndStopRequests(context.Background(), st, 5, actors, false); !errors.Is(err, sql.ErrConnDone) {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionManagementMutations(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := MySQLAuthoringRepository{db: db}
	ctx := context.Background()
	expectSessionScope(m, "s", "default", "alice", "private", "", 1)
	m.ExpectExec("UPDATE screenplay_assistant_sessions SET title=").WithArgs("改名", "owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := repo.ChangeAssistantSession(ctx, "owner", "project", "alice", "s", "rename", "改名", 1); err != nil {
		t.Fatal(err)
	}
	expectSessionScope(m, "s", "default", "alice", "private", "unknown", 2)
	m.ExpectExec("INSERT INTO screenplay_assistant_request_history.*SELECT owner_username,project_id,session_id,request_token,request_actor").WithArgs("unknown", "stopped", "owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE screenplay_assistant_sessions SET request_actor=NULL").WithArgs("owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := repo.ChangeAssistantSession(ctx, "owner", "project", "alice", "s", "stop", "", 2); err != nil {
		t.Fatal(err)
	}
	expectSessionScope(m, "s", "default", "alice", "private", "", 3)
	m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("owner", "project", "s").WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(`{"messages":[],"proposals":[]}`))
	m.ExpectExec("UPDATE screenplay_assistant_sessions SET archived_at=IF").WithArgs(true, "owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := repo.ChangeAssistantSession(ctx, "owner", "project", "alice", "s", "archive", "", 3); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionParentArchiveRestoreStalesAllPending(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := MySQLAuthoringRepository{db: db}
	for _, restore := range []bool{false, true} {
		revision := int64(1)
		action := "archive"
		status := "pending"
		request := "running"
		if restore {
			revision = 2
			action = "restore"
			status = "stale"
			request = ""
		}
		m.ExpectBegin()
		m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
		m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow("{}", 4))
		m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "revision", "archived", "request_status", "request_actor"}).
			AddRow("a", "default", "owner", "private", "confirm", "父", revision, restore, "", "").
			AddRow("b", "a", "owner", "private", "confirm", "子", revision, false, request, "owner").
			AddRow("c", "a", "owner", "private", "confirm", "闲置子", revision, false, "", ""))
		for _, id := range []string{"a", "b", "c"} {
			body := `{"extra":"keep","proposals":[{"future":123,"status":"` + status + `"},{"status":"applied"}]}`
			m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("owner", "project", id).WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(body))
			if !restore {
				m.ExpectExec("UPDATE screenplay_assistant_sessions SET body=").WithArgs(`{"extra":"keep","proposals":[{"future":123,"status":"stale"},{"status":"applied"}]}`, "owner", "project", id).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if id == "b" && !restore {
				m.ExpectExec("INSERT INTO screenplay_assistant_request_history").WithArgs("unknown", "archived", "owner", "project", id).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if id != "a" {
				m.ExpectExec("UPDATE screenplay_assistant_sessions SET request_actor=NULL").WithArgs("owner", "project", id).WillReturnResult(sqlmock.NewResult(0, 1))
			}
		}
		m.ExpectExec("UPDATE screenplay_assistant_sessions SET archived_at=IF").WithArgs(!restore, "owner", "project", "a").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectCommit()
		if err = repo.ChangeAssistantSession(context.Background(), "owner", "project", "owner", "a", action, "", revision); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionGuardedFinishChecksAuthorityAfterProjectAndSessionLocks(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := MySQLAuthoringRepository{db: db}
	expectSessionScope(m, "s", "default", "alice", "project", "running", 2)
	// This current read occurs after the project/session locks. Revocation while
	// waiting for the project must therefore be observed before any history write.
	m.ExpectQuery("SELECT username FROM auth_sessions.*FOR UPDATE").WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	called := false
	guard := func(ctx context.Context, tx *sql.Tx) error {
		called = true
		var username string
		if err := tx.QueryRowContext(ctx, "SELECT username FROM auth_sessions WHERE token_hash=? FOR UPDATE", "original").Scan(&username); err != nil {
			return ErrAccessDenied
		}
		return nil
	}
	req := AssistantSessionRequest{Owner: "owner", Project: "project", Actor: "alice", SessionID: "s", Token: "request", Revision: 2}
	err = r.FinishAssistantRequestGuarded(context.Background(), req, &AssistantSessionHistory{Messages: []AssistantMessage{{Role: "assistant", Content: "late"}}}, guard)
	if !called || !errors.Is(err, ErrAccessDenied) {
		t.Fatal(called, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
