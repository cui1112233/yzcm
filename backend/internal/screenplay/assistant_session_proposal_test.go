package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSessionAssetAndReceiptUseSameTransaction(t *testing.T) {
	for _, fail := range []bool{true, false} {
		db, m, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		repo := &MySQLAuthoringRepository{db: db}
		expectSessionScope(m, "s", "default", "alice", "private", "", 9)
		a := AuthoringAction{Kind: "asset", AssetPrepared: true, AssetOperation: "add", TargetID: "new", Asset: &Asset{ID: "new", Type: AssetProp, Name: "cup", Degree: DegreeStandard}}
		h := AssistantSessionHistory{Proposals: []AssistantProposal{{ID: "proposal", Status: "pending", Revision: 3, Actions: []AuthoringAction{a}}}}
		data, _ := json.Marshal(h)
		m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("owner", "project", "s").WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(data))
		m.ExpectQuery("SELECT COUNT").WithArgs("project", "owner", "prop", "cup").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		m.ExpectQuery("SELECT COALESCE").WithArgs("project", "owner").WillReturnRows(sqlmock.NewRows([]string{"sort"}).AddRow(1))
		m.ExpectExec("INSERT INTO screenplay_assets").WillReturnResult(sqlmock.NewResult(1, 1))
		save := m.ExpectExec("UPDATE screenplay_assistant_sessions SET body=").WithArgs(sqlmock.AnyArg(), "owner", "project", "s", int64(9))
		fault := errors.New("receipt write failed")
		if fail {
			save.WillReturnError(fault)
			m.ExpectRollback()
		} else {
			save.WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
		}
		out, err := repo.ApplySessionProposal(context.Background(), "owner", "project", "alice", "s", "proposal", 9)
		if fail {
			if !errors.Is(err, fault) || len(out.Proposals) != 0 {
				t.Fatal(err, out)
			}
		} else if err != nil || len(out.Proposals) != 1 || out.Proposals[0].Status != "applied" {
			t.Fatal(err, out)
		}
		if err = m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestSessionChapterChecksCollectionInsteadOfCommonChatRevision(t *testing.T) {
	project := Project{ID: "project", OwnerUsername: "owner", SourceText: "shared source"}
	st := AuthoringState{Owner: "owner", ProjectID: "project", Revision: 3, Chapters: []Chapter{{ID: "c", Title: "initial", SourceText: "source"}}}
	action, _, err := PrepareSessionContent(project, st, Document{}, json.RawMessage(`{"kind":"chapter","chapter":{"id":"c","title":"revised"}}`))
	if err != nil {
		t.Fatal(err)
	}
	h := AssistantSessionHistory{Proposals: []AssistantProposal{{ID: "q", Status: "pending", Revision: 4, Actions: []AuthoringAction{action}}}}
	body, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []bool{false, true} {
		db, m, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		live := st
		live.Revision = 97
		live.Messages = []AssistantMessage{{Role: "user", Content: "legacy history must survive"}}
		if changed {
			live.Chapters = append(append([]Chapter(nil), live.Chapters...), Chapter{ID: "new", Title: "new chapter"})
		}
		shared, _ := json.Marshal(live)
		m.ExpectBegin()
		m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
		m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(shared, 97))
		m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "revision", "archived", "status", "actor"}).AddRow("s", "default", "owner", "private", "confirm", "session", 12, false, "", ""))
		m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("owner", "project", "s").WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(body))
		if changed {
			m.ExpectRollback()
		} else {
			m.ExpectQuery("SELECT source_text FROM screenplay_projects").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"source_text"}).AddRow("shared source"))
			m.ExpectExec("UPDATE screenplay_authoring SET body=JSON_SET").WithArgs(sqlmock.AnyArg(), "owner", "project", int64(97)).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("UPDATE screenplay_assistant_sessions SET body=").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
		}
		out, err := (&MySQLAuthoringRepository{db: db}).ApplySessionProposal(context.Background(), "owner", "project", "owner", "s", "q", 12)
		if changed {
			if !errors.Is(err, ErrRevisionConflict) {
				t.Fatal(err)
			}
		} else if err != nil || len(out.Proposals) != 1 || out.Proposals[0].Status != "applied" {
			t.Fatal(err, out)
		}
		if err = m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
