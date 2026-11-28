package screenplay

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSessionAttemptClaimChecksActiveTokenAndOneRow(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := MySQLAuthoringRepository{db: db}
	ctx := context.Background()
	req := AssistantSessionRequest{Owner: "owner", Project: "project", Actor: "alice", SessionID: "s", Token: "token", Revision: 2}
	for _, stage := range []string{"initial", "read_reply"} {
		expectSessionScope(m, "s", "default", "alice", "project", "running", 2)
		m.ExpectQuery("SELECT 1 FROM screenplay_assistant_sessions").WithArgs("owner", "project", "s", "token", "alice").WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(1))
		m.ExpectExec("INSERT INTO screenplay_session_usage_attempts").WithArgs(sqlmock.AnyArg(), "owner", "project", "s", "token", stage, "alice", "team", "founder", "hash").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectCommit()
		id, err := repo.ReserveSessionAttempt(ctx, req, stage, "team", "founder", "hash")
		if err != nil || id == "" {
			t.Fatal(err)
		}
		for _, n := range []int64{1, 0} {
			expectSessionScope(m, "s", "default", "alice", "project", "running", 2)
			m.ExpectQuery("SELECT 1 FROM screenplay_assistant_sessions").WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(1))
			m.ExpectExec("UPDATE screenplay_session_usage_attempts SET state='unknown'").WithArgs(id, "owner", "project", "s", "token", "alice", "team", "founder", "hash").WillReturnResult(sqlmock.NewResult(0, n))
			if n == 1 {
				m.ExpectCommit()
			} else {
				m.ExpectRollback()
			}
			err := repo.ClaimSessionAttempt(ctx, req, id, "team", "founder", "hash")
			if n == 1 && err != nil || n == 0 && !errors.Is(err, ErrRevisionConflict) {
				t.Fatal(n, err)
			}
		}
	}
	expectSessionScope(m, "s", "default", "alice", "project", "running", 2)
	m.ExpectQuery("SELECT 1 FROM screenplay_assistant_sessions").WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if _, err := repo.ReserveSessionAttempt(ctx, req, "read_reply", "team", "founder", "hash"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
