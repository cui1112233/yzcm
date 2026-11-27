package screenplay

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const contentOldScript = "# 第1集：旧稿\n## 1-1：日 内 家\n人物：小明\n小明：旧稿"
const contentNewScript = "# 第1集：新稿\n## 1-1：日 内 家\n人物：小明\n小明：新稿"

func contentEditFixture(t *testing.T, kind string) (Project, AuthoringState, Document) {
	t.Helper()
	p := Project{ID: "p", OwnerUsername: "alice", SourceText: "原文"}
	active := Document{ID: "old", Version: 3, Revision: 1, Content: contentOldScript}
	st := AuthoringState{ProjectID: p.ID, Owner: p.OwnerUsername, Revision: 5,
		Chapters: []Chapter{{ID: "c", Title: "旧章", SourceText: "章节原文", ScriptText: contentOldScript, RawResult: "旧回包", PlanningPromptID: "138"}},
		Members:  []MemberGrant{{Username: "editor", Read: true, Edit: true}}}
	if err := addPreparedContentEdit(&st, p, active, kind); err != nil {
		t.Fatal(err)
	}
	st.Revision++
	return p, st, active
}

func addPreparedContentEdit(st *AuthoringState, p Project, active Document, kind string) error {
	input := map[string]any{"kind": kind, "expected_revision": st.Revision}
	if kind == "document" {
		input["content"] = contentNewScript
	} else {
		input["chapter"] = map[string]string{"id": "c", "script_text": contentNewScript}
	}
	raw, _ := json.Marshal(input)
	action, title, err := PrepareAssistantContentEdit(p, *st, active, raw, false)
	if err != nil {
		return err
	}
	st.Proposals = append(st.Proposals, AssistantProposal{ID: "change", Summary: title, Actions: []AuthoringAction{action}, Status: "pending", Revision: st.Revision + 1})
	return nil
}

type appliedContentBody struct{ chapter bool }

func (m appliedContentBody) Match(v driver.Value) bool {
	raw, ok := v.(string)
	var latest AuthoringState
	if !ok || json.Unmarshal([]byte(raw), &latest) != nil || latest.Revision != 7 || len(latest.Proposals) != 1 || latest.Proposals[0].Status != "applied" {
		return false
	}
	chapter := latest.Chapters[0]
	if m.chapter {
		return chapter.ScriptText == contentNewScript && chapter.RawResult == "" && chapter.SourceText == "章节原文" && chapter.PlanningPromptID == "138"
	}
	return chapter.ScriptText == contentOldScript && chapter.RawResult == "旧回包"
}

func TestMySQLAuthoringContentAtomicRollback(t *testing.T) {
	for _, kind := range []string{"document", "chapter"} {
		t.Run(kind, func(t *testing.T) {
			for _, failAt := range []string{"", "begin", "project", "state", "active", "max", "supersede", "document", "clear-shots", "clear-characters", "clear-scenes", "shot", "character", "scene", "receipt", "commit", "missing-active", "revoked", "concurrent-document", "changed-digest"} {
				t.Run(failAt, func(t *testing.T) {
					db, mock, err := sqlmock.New()
					if err != nil {
						t.Fatal(err)
					}
					defer db.Close()
					p, st, active := contentEditFixture(t, kind)
					injected := errors.New("injected " + failAt)
					var sentinel error
					begin := mock.ExpectBegin()
					if failAt == "begin" {
						begin.WillReturnError(injected)
						sentinel = injected
					} else {
						sentinel = expectContentTransaction(mock, p, st, active, kind, failAt, injected)
					}
					s := NewMySQLStore(db)
					out, err := s.CommitAuthoringContent(context.Background(), s.AuthoringRepository(), "editor", p.OwnerUsername, p.ID, "change", 6)
					if sentinel != nil && !errors.Is(err, sentinel) || sentinel == nil && (err != nil || out.Revision != 7 || out.Proposals[0].Status != "applied") {
						t.Fatalf("state=%+v err=%v, want %v", out, err, sentinel)
					}
					if err := mock.ExpectationsWereMet(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func expectContentTransaction(mock sqlmock.Sqlmock, p Project, st AuthoringState, active Document, kind, failAt string, injected error) error {
	project := mock.ExpectQuery("SELECT source_text FROM screenplay_projects").WithArgs(p.ID, p.OwnerUsername)
	if failAt == "project" {
		project.WillReturnError(injected)
		mock.ExpectRollback()
		return injected
	}
	project.WillReturnRows(sqlmock.NewRows([]string{"source_text"}).AddRow(p.SourceText))
	state := mock.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs(p.OwnerUsername, p.ID)
	if failAt == "state" {
		state.WillReturnError(injected)
		mock.ExpectRollback()
		return injected
	}
	if failAt == "revoked" {
		st.Members[0].Edit = false
	}
	raw, _ := json.Marshal(st)
	state.WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(raw, st.Revision))
	if failAt == "revoked" {
		mock.ExpectRollback()
		return ErrAccessDenied
	}
	document := mock.ExpectQuery("SELECT id,version,revision,content FROM screenplay_documents").WithArgs(p.ID, p.OwnerUsername)
	rows := sqlmock.NewRows([]string{"id", "version", "revision", "content"})
	switch failAt {
	case "active":
		document.WillReturnError(injected)
		mock.ExpectRollback()
		return injected
	case "missing-active":
		document.WillReturnRows(rows)
		mock.ExpectRollback()
		return ErrRevisionConflict
	case "concurrent-document":
		document.WillReturnRows(rows.AddRow("different-id", active.Version, active.Revision, active.Content))
		mock.ExpectRollback()
		return ErrRevisionConflict
	case "changed-digest":
		document.WillReturnRows(rows.AddRow(active.ID, active.Version, active.Revision, "同一个记录里的意外修改"))
		mock.ExpectRollback()
		return ErrRevisionConflict
	default:
		document.WillReturnRows(rows.AddRow(active.ID, active.Version, active.Revision, active.Content))
	}
	max := mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(version), 0)")).WithArgs(p.ID, p.OwnerUsername)
	if failAt == "max" {
		max.WillReturnError(injected)
		mock.ExpectRollback()
		return injected
	}
	max.WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(active.Version))
	for _, step := range []struct{ name, sql string }{
		{"supersede", "UPDATE screenplay_documents SET status"},
		{"document", "INSERT INTO screenplay_documents"},
		{"clear-shots", "DELETE FROM screenplay_shots"},
		{"clear-characters", "DELETE FROM screenplay_characters"},
		{"clear-scenes", "DELETE FROM screenplay_scenes"},
		{"shot", "INSERT INTO screenplay_shots"},
		{"character", "INSERT INTO screenplay_characters"},
		{"scene", "INSERT INTO screenplay_scenes"},
		{"receipt", "UPDATE screenplay_authoring"},
	} {
		exec := mock.ExpectExec(step.sql)
		if step.name == "receipt" {
			exec.WithArgs(appliedContentBody{chapter: kind == "chapter"}, int64(7), p.ID, p.OwnerUsername, int64(6))
		}
		if step.name == failAt {
			exec.WillReturnError(injected)
			mock.ExpectRollback()
			return injected
		}
		exec.WillReturnResult(sqlmock.NewResult(0, 1))
	}
	if failAt == "commit" {
		mock.ExpectCommit().WillReturnError(injected)
		return injected
	}
	mock.ExpectCommit()
	return nil
}

func TestAuthoringContentChecksCurrentSourceAndChapter(t *testing.T) {
	for _, changed := range []string{"source", "chapter", "collection", "missing", "unauthorized"} {
		t.Run(changed, func(t *testing.T) {
			ctx := context.Background()
			m := NewMemoryStore()
			r := NewMemoryAuthoringRepository()
			p := Project{ID: "p", OwnerUsername: "alice", SourceText: "原文"}
			m.CreateProject(ctx, p)
			started, err := PublishChapterScripts(ctx, m, p.OwnerUsername, p.ID, contentOldScript)
			if err != nil {
				t.Fatal(err)
			}
			st := AuthoringState{ProjectID: p.ID, Owner: p.OwnerUsername, Chapters: []Chapter{{ID: "c", Title: "旧章", ScriptText: contentOldScript}, {ID: "second", Title: "第二章"}}}
			if err := addPreparedContentEdit(&st, p, started, "chapter"); err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "source":
				saved, err := m.SaveProjectSource(ctx, p.OwnerUsername, p.ID, "换过的原文")
				if err != nil || saved.Revision != p.Revision {
					t.Fatal(saved, err)
				}
			case "chapter":
				st.Chapters[0].Title = "别人改的标题"
			case "collection":
				st.Chapters[1].Title = "后来改的另一章"
			case "missing":
				st.Chapters = st.Chapters[1:]
			}
			if _, err := r.Save(ctx, st, 0); err != nil {
				t.Fatal(err)
			}
			actor, expected := p.OwnerUsername, ErrRevisionConflict
			if changed == "unauthorized" {
				actor, expected = "other", ErrAccessDenied
			}
			if _, err := m.CommitAuthoringContent(ctx, r, actor, p.OwnerUsername, p.ID, "change", 1); !errors.Is(err, expected) {
				t.Fatal(err)
			}
			active, _ := m.ActiveDocument(ctx, p.OwnerUsername, p.ID)
			current, _ := r.Load(ctx, p.OwnerUsername, p.ID)
			if active.ID != started.ID || current.Revision != 1 || current.Proposals[0].Status != "pending" {
				t.Fatal(active, current)
			}
		})
	}
}

func TestAuthoringContentEditPreservesUnfinishedBatches(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	r := NewMemoryAuthoringRepository()
	p := Project{ID: "p", OwnerUsername: "alice", SourceText: "原文"}
	m.CreateProject(ctx, p)
	st := AuthoringState{ProjectID: "p", Owner: "alice", Chapters: []Chapter{{ID: "c", Title: "旧章", ScriptText: contentOldScript, RawResult: "之前的回包", PlanningPromptID: "138",
		ScriptGeneration: &ScriptGeneration{ID: "real-batch", Status: "awaiting_continue"}}}}
	if err := addPreparedContentEdit(&st, p, Document{}, "chapter"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Save(ctx, st, 0); err != nil {
		t.Fatal(err)
	}
	svc := AuthoringService{Store: m, Repository: r}
	current, err := svc.Apply(ctx, "alice", "p", "change", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	chapter := current.Chapters[0]
	if chapter.ScriptGeneration.ID != "real-batch" || chapter.ScriptGeneration.Status != "stale" || chapter.RawResult != "" || chapter.PlanningPromptID != "138" || chapter.ScriptText != contentNewScript {
		t.Fatal(chapter)
	}
	if _, err := m.ActiveDocument(ctx, "alice", "p"); !errors.Is(err, ErrNoActiveScript) {
		t.Fatal(err)
	}
}

func TestAuthoringContentConcurrentConfirmationWritesOnce(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	r := NewMemoryAuthoringRepository()
	p := Project{ID: "p", OwnerUsername: "alice"}
	m.CreateProject(ctx, p)
	active, err := PublishChapterScripts(ctx, m, p.OwnerUsername, p.ID, contentOldScript)
	if err != nil {
		t.Fatal(err)
	}
	st := AuthoringState{ProjectID: p.ID, Owner: p.OwnerUsername}
	if err := addPreparedContentEdit(&st, p, active, "document"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Save(ctx, st, 0); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := m.CommitAuthoringContent(ctx, r, p.OwnerUsername, p.OwnerUsername, p.ID, "change", 1)
			results <- err
		}()
	}
	a, b := <-results, <-results
	if a != nil {
		a, b = b, a
	}
	if a != nil || !errors.Is(b, ErrRevisionConflict) {
		t.Fatal(a, b)
	}
	docs, _ := m.ListDocuments(ctx, p.OwnerUsername, p.ID)
	saved, _ := r.Load(ctx, p.OwnerUsername, p.ID)
	if len(docs) != 2 || docs[0].ID == active.ID || saved.Proposals[0].Status != "applied" || saved.Revision != 2 {
		t.Fatal(docs, saved)
	}
}
