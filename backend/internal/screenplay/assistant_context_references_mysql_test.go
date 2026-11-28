package screenplay

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAssistantReferencesSameTransactionRejectsChangedSource(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same", true: "changed"}[changed], func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			v := AssistantReferenceView{Project: Project{ID: "p", SourceText: "original"}}
			ref, e := v.Describe(AssistantReferenceSelector{Kind: "project_source", ID: "p"})
			if e != nil {
				t.Fatal(e)
			}
			m.ExpectBegin()
			tx, e := db.Begin()
			if e != nil {
				t.Fatal(e)
			}
			source := "original"
			if changed {
				source = "updated"
			}
			m.ExpectQuery("SELECT COALESCE\\(source_text,''\\).*FOR UPDATE").WithArgs("o", "p").WillReturnRows(sqlmock.NewRows([]string{"source"}).AddRow(source))
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WithArgs("o", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(`{}`, 1))
			e = CheckAssistantReferencesInTx(context.Background(), tx, "o", "p", []AssistantReferenceSnapshot{ref})
			if changed && !errors.Is(e, ErrRevisionConflict) || !changed && e != nil {
				t.Fatal(e)
			}
			m.ExpectRollback()
			tx.Rollback()
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestAssistantReferencesDefaultSaveAuthorityAndDependencyRollback(t *testing.T) {
	for _, reason := range []string{"authority", "dependency", "revoked"} {
		t.Run(reason, func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			r := MySQLAuthoringRepository{db: db}
			m.ExpectBegin()
			m.ExpectQuery("SELECT 1 FROM screenplay_projects.*FOR UPDATE").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
			body := `{"members":[{"username":"a","read":true,"generate":true}]}`
			if reason == "revoked" {
				body = `{}`
			}
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WithArgs("owner", "project").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(body, 2))
			guardCalled := false
			guard := func(context.Context, *sql.Tx) error {
				guardCalled = true
				if reason == "authority" {
					return ErrAccessDenied
				}
				return nil
			}
			v := AssistantReferenceView{Project: Project{ID: "project", SourceText: "before"}}
			ref, _ := v.Describe(AssistantReferenceSelector{Kind: "project_source", ID: "project"})
			if reason == "dependency" {
				m.ExpectQuery("SELECT COALESCE\\(source_text,''\\).*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"source"}).AddRow("after"))
				m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(body, 2))
			}
			m.ExpectRollback()
			_, e = r.SaveAssistantReferences(context.Background(), AuthoringState{Owner: "owner", ProjectID: "project"}, "a", 2, []AssistantReferenceSnapshot{ref}, guard)
			want := ErrAccessDenied
			if reason == "dependency" {
				want = ErrRevisionConflict
			}
			if !errors.Is(e, want) {
				t.Fatal(e)
			}
			if guardCalled != (reason != "revoked") {
				t.Fatal("guard ordering")
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestAssistantReferencesUnsentReleasesTokenWithKnownReason(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	r := MySQLAuthoringRepository{db: db}
	expectSessionScope(m, "s", "default", "alice", "private", "running", 2)
	m.ExpectQuery("SELECT request_token FROM screenplay_assistant_sessions").WithArgs("owner", "project", "s").WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("token"))
	m.ExpectExec("INSERT INTO screenplay_assistant_request_history").WithArgs("completed", "unsent", "owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE screenplay_assistant_sessions SET request_actor=NULL").WithArgs("owner", "project", "s").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	e = r.ReleaseAssistantReferencesUnsent(context.Background(), AssistantSessionRequest{Owner: "owner", Project: "project", Actor: "alice", SessionID: "s", Token: "token", Revision: 2})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestAssistantReferencesSQLChecksEachKind(t *testing.T) {
	for _, kind := range []string{"document", "chapter_script", "asset_text", "storyboard"} {
		t.Run(kind, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			view := AssistantReferenceView{Project: Project{ID: "p", SourceText: "source"}, Chapters: []Chapter{{ID: "c", Title: "chapter", ScriptText: "script"}}, Documents: []Document{{ID: "d", Status: "active", Version: 2, Revision: 3, Content: "doc"}}, Assets: []Asset{{ID: "a", Name: "asset", Type: AssetCharacter, Identity: "identity", Description: "desc", Prompt: "prompt", Revision: 4}}, Boards: []Storyboard{{ID: "b", EpisodeNo: 2, Engine: "seedance", Revision: 5, Shots: []StoryboardShot{{ID: "s", Seq: 1, Characters: "person", Scene: "scene", Props: "props", VideoDesc: "video", ImagePrompt: "image", Line: "line"}}}}}
			ids := map[string]string{"document": "d", "chapter_script": "c", "asset_text": "a", "storyboard": "b"}
			ref, err := view.Describe(AssistantReferenceSelector{Kind: kind, ID: ids[kind]})
			if err != nil {
				t.Fatal(err)
			}
			m.ExpectBegin()
			tx, _ := db.Begin()
			m.ExpectQuery("SELECT COALESCE\\(source_text,''\\).*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"source"}).AddRow("source"))
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"body", "rev"}).AddRow(`{"chapters":[{"id":"c","title":"chapter","script_text":"script"}]}`, 8))
			switch kind {
			case "document":
				m.ExpectQuery("SELECT id,status,version,revision,content FROM screenplay_documents.*FOR UPDATE").WithArgs("o", "p", "d").WillReturnRows(sqlmock.NewRows([]string{"id", "status", "version", "rev", "content"}).AddRow("d", "active", 2, 3, "doc"))
			case "asset_text":
				m.ExpectQuery("SELECT id,name,asset_type.*FROM screenplay_assets.*FOR UPDATE").WithArgs("o", "p", "a").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "type", "identity", "desc", "prompt", "rev"}).AddRow("a", "asset", "character", "identity", "desc", "prompt", 4))
			case "storyboard":
				m.ExpectQuery("SELECT id,episode_no,engine,revision FROM screenplay_storyboards.*FOR UPDATE").WithArgs("o", "p", "b").WillReturnRows(sqlmock.NewRows([]string{"id", "episode", "engine", "rev"}).AddRow("b", 2, "seedance", 5))
				m.ExpectQuery("SELECT id,seq,characters_text.*FROM screenplay_storyboard_shots.*FOR UPDATE").WithArgs("o", "p", "b").WillReturnRows(sqlmock.NewRows([]string{"id", "seq", "characters", "scene", "props", "video", "protocol"}).AddRow("s", 1, "person", "scene", "props", "video", `{"image_prompt":"image","line":"line","protocol_fields":{"secret":"never_sent"}}`))
			}
			if err = CheckAssistantReferencesInTx(context.Background(), tx, "o", "p", []AssistantReferenceSnapshot{ref}); err != nil {
				t.Fatal(err)
			}
			m.ExpectCommit()
			tx.Commit()
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssistantReferencesDefaultMessageCommitsOnlyAfterGuard(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	repo := MySQLAuthoringRepository{db: db}
	view := AssistantReferenceView{Project: Project{ID: "p", SourceText: "source"}}
	ref, _ := view.Describe(AssistantReferenceSelector{Kind: "project_source", ID: "p"})
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"body", "rev"}).AddRow(`{}`, 2))
	called := false
	guard := func(context.Context, *sql.Tx) error { called = true; return nil }
	m.ExpectQuery("SELECT COALESCE\\(source_text,''\\).*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"source"}).AddRow("source"))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"body", "rev"}).AddRow(`{}`, 2))
	m.ExpectExec("UPDATE screenplay_authoring SET body=").WithArgs(sqlmock.AnyArg(), int64(3), "p", "o", int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	out, e := repo.SaveAssistantReferences(context.Background(), AuthoringState{Owner: "o", ProjectID: "p", Messages: []AssistantMessage{{Role: "user", Content: "q", References: []AssistantReferenceSnapshot{ref}}}}, "o", 2, []AssistantReferenceSnapshot{ref}, guard)
	if e != nil || !called || len(out.Messages[0].References) != 1 || out.Revision != 3 {
		t.Fatal(out, e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
